package restapi

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestIntegrationSupabaseUserSync khóa các bất biến chống chiếm tài khoản của
// syncSupabaseUser: match theo supabase_id, chỉ link email đã verify, không
// bao giờ gán đè supabase_id của user khác.
func TestIntegrationSupabaseUserSync(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()

	// UUID duy nhất mỗi lần chạy để test rerun-safe.
	tag := time.Now().UnixNano() % 1_000_000_000
	uuid := func(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%09d%03d", tag, n) }
	emailVictim := fmt.Sprintf("victim-%d@keo.test", tag)

	// Nạn nhân: user đã gắn Supabase, có email verified.
	victimID, err := syncSupabaseUser(ctx, pool, uuid(1), emailVictim, "Victim", true)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Attacker sub khác, CÙNG email nhưng chưa verify → không được đụng
	// vào user nạn nhân, tạo user mới (email không lưu).
	attackerID, err := syncSupabaseUser(ctx, pool, uuid(2), emailVictim, "Attacker", false)
	if err != nil {
		t.Fatal(err)
	}
	if attackerID == victimID {
		t.Fatal("email chưa verify mà vẫn link vào user nạn nhân")
	}

	// 2. Attacker sub khác nữa, cùng email ĐÃ verify — nhưng email đã thuộc
	// user gắn Supabase khác → vẫn không cướp, tạo user mới.
	attacker2ID, err := syncSupabaseUser(ctx, pool, uuid(3), emailVictim, "Attacker2", true)
	if err != nil {
		t.Fatal(err)
	}
	if attacker2ID == victimID {
		t.Fatal("email verified của user đã gắn Supabase khác mà vẫn bị cướp")
	}

	// Nạn nhân đăng nhập lại → vẫn về đúng row cũ, supabase_id không bị đè.
	again, err := syncSupabaseUser(ctx, pool, uuid(1), emailVictim, "Victim", true)
	if err != nil {
		t.Fatal(err)
	}
	if again != victimID {
		t.Fatalf("nạn nhân login lại ra user %d, want %d", again, victimID)
	}

	// 3. Link hợp lệ: user sẵn có từ kênh khác (email verified, CHƯA gắn
	// Supabase) → login Supabase đầu tiên phải nối đúng vào row đó.
	emailLegacy := fmt.Sprintf("legacy-%d@keo.test", tag)
	var legacyID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (display_name, email) VALUES ('Legacy', $1) RETURNING id`,
		emailLegacy,
	).Scan(&legacyID); err != nil {
		t.Fatal(err)
	}
	linked, err := syncSupabaseUser(ctx, pool, uuid(4), emailLegacy, "Legacy", true)
	if err != nil {
		t.Fatal(err)
	}
	if linked != legacyID {
		t.Fatalf("link email verified: ra user %d, want %d", linked, legacyID)
	}

	// 4. Email rỗng (login Zalo/phone qua Supabase) → tạo user bình thường,
	// nhiều user không email không va nhau trên UNIQUE(email).
	for i := 5; i <= 6; i++ {
		if _, err := syncSupabaseUser(ctx, pool, uuid(i), "", "NoEmail", false); err != nil {
			t.Fatalf("user không email thứ %d: %v", i-4, err)
		}
	}
}

func integrationPool(t *testing.T) *pgxpool.Pool {
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
