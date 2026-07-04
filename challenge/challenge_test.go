package challenge

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hao/keo/ledger"
)

// ===== Unit tests: sinh kỳ (pure function) =====

func TestGeneratePeriodsDaily(t *testing.T) {
	start := time.Date(2026, 7, 1, 6, 0, 0, 0, VNLocation)
	end := start.AddDate(0, 0, 30)
	periods, err := GeneratePeriods(GoalDailySteps, 10000, start, end, VNLocation)
	if err != nil {
		t.Fatal(err)
	}
	if len(periods) != 30 {
		t.Fatalf("got %d periods, want 30", len(periods))
	}
	for _, p := range periods {
		if p.Target != 10000 {
			t.Fatalf("daily target = %v, want 10000", p.Target)
		}
	}
}

func TestGeneratePeriodsWeeklyProrated(t *testing.T) {
	// 30 ngày, goal 20km/tuần → 4 tuần đủ + 1 kỳ 2 ngày prorate 20×2/7 ≈ 5.71
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, VNLocation)
	end := start.AddDate(0, 0, 30)
	periods, err := GeneratePeriods(GoalWeeklyDistanceKm, 20, start, end, VNLocation)
	if err != nil {
		t.Fatal(err)
	}
	if len(periods) != 5 {
		t.Fatalf("got %d periods, want 5", len(periods))
	}
	for i := 0; i < 4; i++ {
		if periods[i].Target != 20 {
			t.Fatalf("week %d target = %v, want 20", i, periods[i].Target)
		}
	}
	if last := periods[4].Target; last != 5.71 {
		t.Fatalf("partial week target = %v, want 5.71", last)
	}
}

func TestPassedRatio(t *testing.T) {
	cases := []struct {
		passed, total int
		ratio         float64
		want          bool
	}{
		{4, 5, 0.8, true}, // đúng ngưỡng → đậu (float epsilon phải xử lý được)
		{3, 5, 0.8, false},
		{24, 30, 0.8, true},
		{23, 30, 0.8, false},
		{1, 1, 1.0, true},
		{0, 0, 0.8, false}, // dữ liệu hỏng → không đậu mặc định
	}
	for _, c := range cases {
		if got := Passed(c.passed, c.total, c.ratio); got != c.want {
			t.Fatalf("Passed(%d, %d, %v) = %v, want %v", c.passed, c.total, c.ratio, got, c.want)
		}
	}
}

// ===== Integration test: full lifecycle =====
//
// Kịch bản: 3 user mua điểm → vào kèo 100 điểm → user A đậu hết, user B đậu
// đúng 80%, user C rớt → settlement job chạy → verify từng số dư và bất biến.

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("LEDGER_TEST_DSN")
	if dsn == "" {
		t.Skip("set LEDGER_TEST_DSN để chạy integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestIntegrationFullLifecycle(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	ledgerStore := ledger.NewPGStore(pool)
	ledgerSvc := ledger.NewService(ledgerStore)
	store := NewStore(pool, ledgerStore)
	job := NewSettlementJob(store, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Seed users id riêng cho test.
	run := os.Getpid()
	userA, userB, userC := int64(930000+run%1000)*10+1, int64(930000+run%1000)*10+2, int64(930000+run%1000)*10+3
	for _, uid := range []int64{userA, userB, userC} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO users (id, display_name) OVERRIDING SYSTEM VALUE
			VALUES ($1, 'lifecycle-test') ON CONFLICT (id) DO NOTHING`, uid); err != nil {
			t.Fatal(err)
		}
		if _, err := ledgerSvc.Purchase(ctx, uid, 1000, "test", fmt.Sprintf("lc-%d-%d", run, uid)); err != nil {
			t.Fatal(err)
		}
	}

	// Kèo daily 5 ngày, đã hết hạn + qua grace → settle được ngay.
	now := time.Now()
	challengeID, err := store.Create(ctx, Challenge{
		CreatorID: userA, Title: "Đi bộ 10k bước (lifecycle test)", Sport: "walk",
		GoalType: GoalDailySteps, GoalValue: 10000, Source: "google_fit",
		StakePoints: 100, FeeBps: 1000, PassRatio: 0.8,
		StartAt: now.AddDate(0, 0, -8), EndAt: now.AddDate(0, 0, -3), GraceHours: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Cả 3 vào kèo; user A join 2 lần để kiểm idempotency.
	enrollA, err := store.Join(ctx, challengeID, userA)
	if err != nil {
		t.Fatal(err)
	}
	enrollA2, err := store.Join(ctx, challengeID, userA)
	if err != nil {
		t.Fatal(err)
	}
	if enrollA != enrollA2 {
		t.Fatalf("double join tạo 2 enrollment: %d vs %d", enrollA, enrollA2)
	}
	enrollB, err := store.Join(ctx, challengeID, userB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Join(ctx, challengeID, userC); err != nil {
		t.Fatal(err)
	}

	// Sau join: available = 900, locked = 100, và chỉ khóa MỘT lần dù join 2 lần.
	if bal, _ := ledgerSvc.Balance(ctx, ledger.UserLocked(userA)); bal != 100 {
		t.Fatalf("userA locked = %d, want 100", bal)
	}

	// Mô phỏng ingestion: A đậu mọi kỳ, B đậu đúng ceil(80%), C không kỳ nào.
	var total int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM enrollment_periods WHERE enrollment_id = $1`, enrollA,
	).Scan(&total); err != nil {
		t.Fatal(err)
	}
	needB := (total*8 + 9) / 10 // ceil(0.8 × total)
	if _, err := pool.Exec(ctx,
		`UPDATE enrollment_periods SET passed = true, achieved = target WHERE enrollment_id = $1`, enrollA); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE enrollment_periods SET passed = true, achieved = target
		WHERE enrollment_id = $1 AND period_start IN (
			SELECT period_start FROM enrollment_periods
			WHERE enrollment_id = $1 ORDER BY period_start LIMIT $2)`,
		enrollB, needB); err != nil {
		t.Fatal(err)
	}

	// Chạy job — và chạy LẦN THỨ HAI để chứng minh idempotent.
	if err := job.Run(ctx, now, 10); err != nil {
		t.Fatal(err)
	}
	if err := job.Run(ctx, now, 10); err != nil {
		t.Fatal(err)
	}

	// Verify trạng thái.
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM challenges WHERE id = $1`, challengeID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "settled" {
		t.Fatalf("challenge status = %s, want settled", status)
	}

	// Verify số dư. Pool = 100 (C rớt); fee 10%; share = 90/2 = 45.
	// A, B: 900 + 100 hoàn cược + 45 thưởng = 1045. C: 900. Locked tất cả = 0.
	check := func(uid, wantAvail int64) {
		t.Helper()
		avail, _ := ledgerSvc.Balance(ctx, ledger.UserAvailable(uid))
		locked, _ := ledgerSvc.Balance(ctx, ledger.UserLocked(uid))
		if avail != wantAvail || locked != 0 {
			t.Fatalf("user %d: avail=%d locked=%d, want avail=%d locked=0", uid, avail, locked, wantAvail)
		}
	}
	check(userA, 1045)
	check(userB, 1045)
	check(userC, 900)

	if fee, _ := ledgerSvc.Balance(ctx, ledger.PlatformFee()); fee < 10 {
		t.Fatalf("platform_fee = %d, want >= 10", fee)
	}
	if poolBal, _ := ledgerSvc.Balance(ctx, ledger.ChallengePool(challengeID)); poolBal != 0 {
		t.Fatalf("challenge_pool = %d, want 0 sau settlement", poolBal)
	}

	// Bất biến cuối: tổng toàn bộ ledger vẫn = 0.
	var sum int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(amount),0) FROM ledger_entries`).Scan(&sum); err != nil {
		t.Fatal(err)
	}
	if sum != 0 {
		t.Fatalf("tổng ledger = %d, bất biến double-entry đã vỡ", sum)
	}
}
