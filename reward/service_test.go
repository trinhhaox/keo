package reward

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hao/keo/ledger"
)

// Integration tests — cần Postgres đã chạy migrations:
//
//	LEDGER_TEST_DSN=postgres://postgres:test@localhost:5432/keo go test ./reward/ -run Integration -v
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("LEDGER_TEST_DSN")
	if dsn == "" {
		t.Skip("set LEDGER_TEST_DSN để chạy integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func newUser(t *testing.T, pool *pgxpool.Pool, name string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (display_name) VALUES ($1) RETURNING id`, name,
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func available(t *testing.T, l *ledger.PGStore, userID int64) int64 {
	t.Helper()
	bal, err := l.Balance(context.Background(), ledger.UserAvailable(userID))
	if err != nil {
		t.Fatal(err)
	}
	return bal
}

// TestIntegrationCheckInOncePerDay: check-in cộng thẳng 1 điểm vào ví;
// lần 2 trong ngày bị chặn, không cộng nữa.
func TestIntegrationCheckInOncePerDay(t *testing.T) {
	pool := testPool(t)
	l := ledger.NewPGStore(pool)
	svc := NewService(pool, l)
	userID := newUser(t, pool, "checkin-once")
	now := time.Now()

	acc, err := svc.CheckIn(context.Background(), userID, now)
	if err != nil {
		t.Fatal(err)
	}
	if !acc.Granted || acc.Points != 1 {
		t.Fatalf("lần 1: %+v, want granted/+1", acc)
	}
	if got := available(t, l, userID); got != 1 {
		t.Fatalf("ví = %d sau check-in, want 1", got)
	}

	if _, err := svc.CheckIn(context.Background(), userID, now); !errors.Is(err, ErrAlreadyCheckedIn) {
		t.Fatalf("lần 2: err = %v, want ErrAlreadyCheckedIn", err)
	}
	if got := available(t, l, userID); got != 1 {
		t.Fatalf("ví = %d sau check-in trùng, want 1", got)
	}

	sum, err := svc.GetSummary(context.Background(), userID, now)
	if err != nil {
		t.Fatal(err)
	}
	if !sum.CheckedInToday || sum.TotalPoints != 1 {
		t.Fatalf("summary: %+v", sum)
	}
}

// TestIntegrationAccrueActivityRules: chỉ walk/run không-manual mới được thưởng,
// 1 điểm/km tròn cộng thẳng vào ví, replay theo (source, external_id) vô hại.
func TestIntegrationAccrueActivityRules(t *testing.T) {
	pool := testPool(t)
	l := ledger.NewPGStore(pool)
	svc := NewService(pool, l)
	userID := newUser(t, pool, "activity-rules")
	ctx := context.Background()

	accrue := func(sport, extID string, distanceM float64, manual bool) {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err := svc.AccrueActivity(ctx, tx, userID, sport, "strava", extID, distanceM, manual); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}

	accrue("run", "act-1", 5400, false) // 5.4km → +5 điểm
	if got := available(t, l, userID); got != 5 {
		t.Fatalf("run 5.4km: ví = %d, want 5", got)
	}
	accrue("run", "act-1", 5400, false) // replay webhook → không cộng thêm
	if got := available(t, l, userID); got != 5 {
		t.Fatalf("replay: ví = %d, want 5", got)
	}
	accrue("walk", "act-2", 900, false) // 0.9km → floor = 0, không có sự kiện
	accrue("bike", "act-3", 20000, false)
	accrue("run", "act-4", 3000, true) // manual → bỏ qua
	if got := available(t, l, userID); got != 5 {
		t.Fatalf("sau các case không thưởng: ví = %d, want 5", got)
	}
	accrue("walk", "act-5", 2100, false) // 2.1km walk → +2 điểm
	if got := available(t, l, userID); got != 7 {
		t.Fatalf("walk 2.1km: ví = %d, want 7", got)
	}

	sum, err := svc.GetSummary(ctx, userID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if sum.TotalPoints != 7 {
		t.Fatalf("total_points = %d, want 7", sum.TotalPoints)
	}
}

// TestIntegrationDailyCap: tổng thưởng một ngày bị chặn ở DailyCap; sự kiện
// đến khi kịch trần vẫn đốt ref_key; sang ngày mới trần reset.
func TestIntegrationDailyCap(t *testing.T) {
	pool := testPool(t)
	l := ledger.NewPGStore(pool)
	svc := NewService(pool, l)
	userID := newUser(t, pool, "daily-cap")
	ctx := context.Background()

	accrue := func(refKey string, points int64) Accrual {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		acc, err := svc.AccrueTx(ctx, tx, userID, KindDistance, refKey, points)
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		return acc
	}

	// 250km giả → chỉ nhận đúng DailyCap.
	acc := accrue("strava:mega-run", 250)
	if acc.Points != DailyCap || !acc.Capped {
		t.Fatalf("250 điểm: %+v, want points=%d capped=true", acc, DailyCap)
	}
	if got := available(t, l, userID); got != DailyCap {
		t.Fatalf("ví = %d, want %d", got, DailyCap)
	}

	// Kịch trần: sự kiện mới nhận 0 điểm nhưng vẫn được ghi (đốt ref_key)...
	acc = accrue("strava:after-cap", 5)
	if acc.Points != 0 || !acc.Capped || !acc.Granted {
		t.Fatalf("sau trần: %+v, want points=0 capped=true granted=true", acc)
	}
	// ...nên replay cùng ref_key là no-op, kể cả khi trần đã reset.
	if acc = accrue("strava:after-cap", 5); acc.Granted {
		t.Fatalf("replay ref_key đã đốt: %+v, want granted=false", acc)
	}

	// Check-in khi kịch trần: ghi nhận check-in nhưng +0 điểm.
	accCheckin, err := svc.CheckIn(ctx, userID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if accCheckin.Points != 0 || !accCheckin.Capped {
		t.Fatalf("check-in khi kịch trần: %+v, want points=0 capped=true", accCheckin)
	}

	// Giả lập sang ngày mới: dời counter hôm nay về hôm qua → trần reset.
	if _, err := pool.Exec(ctx, `
		UPDATE reward_daily SET vn_date = vn_date - 1 WHERE user_id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	acc = accrue("strava:next-day", 3)
	if acc.Points != 3 || acc.Capped {
		t.Fatalf("ngày mới: %+v, want points=3 capped=false", acc)
	}
	if got := available(t, l, userID); got != DailyCap+3 {
		t.Fatalf("ví = %d, want %d", got, DailyCap+3)
	}
}
