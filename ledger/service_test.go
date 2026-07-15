package ledger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ===== Unit tests: công thức settlement (pure function, không cần DB) =====

func TestSettlementConservation(t *testing.T) {
	// Bất biến quan trọng nhất: tổng entries = 0 với MỌI tổ hợp input.
	cases := []SettlementParams{
		{ChallengeID: 1, StakePoints: 200, FeeBps: 1000, CompletedIDs: []int64{1, 2, 3}, FailedIDs: []int64{4, 5}},
		{ChallengeID: 2, StakePoints: 333, FeeBps: 1000, CompletedIDs: []int64{1, 2, 3, 4, 5, 6, 7}, FailedIDs: []int64{8}}, // chia không hết → có dư
		{ChallengeID: 3, StakePoints: 100, FeeBps: 1000, CompletedIDs: []int64{1, 2}, FailedIDs: nil},                       // không ai rớt
		{ChallengeID: 4, StakePoints: 100, FeeBps: 1000, CompletedIDs: nil, FailedIDs: []int64{1, 2, 3}},                    // không ai đậu
		{ChallengeID: 5, StakePoints: 500, FeeBps: 0, CompletedIDs: []int64{1}, FailedIDs: []int64{2}},                      // fee 0%
		{ChallengeID: 6, StakePoints: 100, FeeBps: 1000, CompletedIDs: []int64{1}, FailedIDs: nil},                          // kèo 1 người tự đậu
	}
	for _, p := range cases {
		t.Run(fmt.Sprintf("challenge_%d", p.ChallengeID), func(t *testing.T) {
			req, err := SettlementRequest(p)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if err := req.Validate(); err != nil {
				t.Fatalf("validate: %v", err)
			}
			// Pool phải về 0: tổng entries trên challenge_pool = 0.
			var poolSum int64
			for _, e := range req.Entries {
				if e.Account.Type == AccountChallengePool {
					poolSum += e.Amount
				}
			}
			if poolSum != 0 {
				t.Fatalf("challenge_pool không về 0, còn %d", poolSum)
			}
		})
	}
}

func TestSettlementRejectsDuplicateUser(t *testing.T) {
	_, err := SettlementRequest(SettlementParams{
		ChallengeID: 1, StakePoints: 100, FeeBps: 1000,
		CompletedIDs: []int64{1, 2}, FailedIDs: []int64{2, 3}, // user 2 ở cả hai
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestSettlementSharesMath(t *testing.T) {
	// 5 người rớt × 200 = pool 1000; fee 10% = 100; chia 3 người: 900/3 = 300/người.
	req, _ := SettlementRequest(SettlementParams{
		ChallengeID: 9, StakePoints: 200, FeeBps: 1000,
		CompletedIDs: []int64{1, 2, 3}, FailedIDs: []int64{4, 5, 6, 7, 8},
	})
	got := map[int64]int64{} // user → tổng nhận vào available
	for _, e := range req.Entries {
		if e.Account.Type == AccountUserAvailable {
			got[e.Account.UserID] += e.Amount
		}
	}
	for _, uid := range []int64{1, 2, 3} {
		if got[uid] != 500 { // 200 hoàn cược + 300 thưởng
			t.Fatalf("user %d nhận %d, expected 500", uid, got[uid])
		}
	}
}

// ===== Integration tests: race conditions (cần DB thật) =====
//
// Chạy:
//   docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=test postgres:16
//   psql ... < migrations/001_ledger.sql   (bỏ FK users(id) hoặc tạo bảng users tối giản)
//   LEDGER_TEST_DSN=postgres://postgres:test@localhost:5432/postgres go test ./ledger/ -run Integration -v

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

// newTestUser tạo một user MỚI cho mỗi lần chạy test. Bài học từ chính file
// này: dùng user ID cố định + assert số dư tuyệt đối khiến test chỉ đúng trên
// DB vừa reset — chạy lần hai là fail giả. Test phải tự chứa.
func newTestUser(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (display_name) VALUES ('ledger-test') RETURNING id`,
	).Scan(&id); err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return id
}

// TestIntegrationIdempotentReplay: N goroutine cùng post MỘT idempotency key —
// đúng một lần được apply, số dư tăng đúng một lần.
func TestIntegrationIdempotentReplay(t *testing.T) {
	pool := testPool(t)
	svc := NewService(NewPGStore(pool))
	ctx := context.Background()
	userID := newTestUser(t, pool)

	const goroutines = 20
	req := PurchaseRequest(userID, 100, "test", fmt.Sprintf("replay-%d-%d", os.Getpid(), time.Now().UnixNano()))

	var wg sync.WaitGroup
	applied, replayed := 0, 0
	var mu sync.Mutex
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := svc.Post(ctx, req)
			if err != nil {
				t.Errorf("post: %v", err)
				return
			}
			mu.Lock()
			if res.Replayed {
				replayed++
			} else {
				applied++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if applied != 1 {
		t.Fatalf("applied = %d, expected đúng 1 (replayed = %d)", applied, replayed)
	}
	bal, err := svc.Balance(ctx, UserAvailable(userID))
	if err != nil {
		t.Fatal(err)
	}
	if bal != 100 {
		t.Fatalf("balance = %d, expected 100 — điểm bị cộng %d lần", bal, bal/100)
	}
}

// TestIntegrationNoNegativeBalance: seed 100 điểm, 10 goroutine mỗi cái cố tiêu 30.
// Tối đa 3 request thành công; balance cuối không bao giờ âm.
// Đây là test chứng minh CHECK constraint làm đúng việc mà SELECT-then-UPDATE làm sai.
func TestIntegrationNoNegativeBalance(t *testing.T) {
	pool := testPool(t)
	svc := NewService(NewPGStore(pool))
	ctx := context.Background()
	userID := newTestUser(t, pool)

	if _, err := svc.Purchase(ctx, userID, 100, "test", fmt.Sprintf("seed-%d-%d", os.Getpid(), time.Now().UnixNano())); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const goroutines = 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	success, insufficient := 0, 0
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.Redeem(ctx, userID, 30, time.Now().UnixNano()+int64(i))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				success++
			case errors.Is(err, ErrInsufficientBalance):
				insufficient++
			default:
				t.Errorf("unexpected: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if success == 0 {
		// Có 100 điểm mà không request nào tiêu được 30 → hệ thống từ chối
		// giao dịch hợp lệ. Assertion này chống kiểu "pass trùng hợp":
		// bug ON CONFLICT + CHECK từng khiến mọi delta âm bị reject nhưng
		// test vẫn xanh vì balance khớp với success=0.
		t.Fatalf("không request nào thành công dù đủ số dư (insufficient=%d)", insufficient)
	}
	if success > 3 {
		t.Fatalf("success = %d > 3: đã chi quá số dư", success)
	}
	bal, _ := svc.Balance(ctx, UserAvailable(userID))
	if bal < 0 {
		t.Fatalf("balance âm: %d", bal)
	}
	if want := int64(100 - success*30); bal != want {
		t.Fatalf("balance = %d, expected %d (success=%d, insufficient=%d)", bal, want, success, insufficient)
	}
}

// TestIntegrationZeroSum: sau mọi thao tác, tổng toàn bộ ledger vẫn = 0.
func TestIntegrationZeroSum(t *testing.T) {
	pool := testPool(t)
	var sum int64
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(amount), 0) FROM ledger_entries`,
	).Scan(&sum); err != nil {
		t.Fatal(err)
	}
	if sum != 0 {
		t.Fatalf("tổng ledger = %d, bất biến double-entry đã vỡ", sum)
	}
}

func TestRewardPayoutRequest(t *testing.T) {
	req := RewardPayoutRequest(7, 2, "event=99")
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
	if req.Type != TxnRewardPayout {
		t.Fatalf("type = %s", req.Type)
	}
	// Cùng event → cùng idempotency key (replay vô hại); khác event → khác key.
	if req.IdempotencyKey != RewardPayoutRequest(7, 2, "event=99").IdempotencyKey {
		t.Fatal("idempotency key không ổn định")
	}
	if req.IdempotencyKey == RewardPayoutRequest(7, 2, "event=100").IdempotencyKey {
		t.Fatal("hai event khác nhau trùng idempotency key")
	}
	var user, expense int64
	for _, e := range req.Entries {
		switch e.Account.Type {
		case AccountUserAvailable:
			user = e.Amount
		case AccountRewardExpense:
			expense = e.Amount
		}
	}
	if user != 2 || expense != -2 {
		t.Fatalf("entries: user=%d expense=%d, want +2/-2", user, expense)
	}
}
