package migrations

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestIntegrationRunMigrations tái hiện đúng đường migrate trên Vercel:
// DB trắng tinh, simple protocol (như Supabase pooler), apply toàn bộ chuỗi
// migration từ embed — gồm cả 005 partition nhiều statement.
func TestIntegrationRunMigrations(t *testing.T) {
	adminDSN := os.Getenv("LEDGER_TEST_DSN")
	if adminDSN == "" {
		t.Skip("set LEDGER_TEST_DSN để chạy integration test")
	}
	ctx := context.Background()

	// Tạo database mới toanh cho test này.
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	dbName := fmt.Sprintf("mig_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+dbName+" WITH (FORCE)")
		admin.Close(context.Background())
	})

	cfg, err := pgxpool.ParseConfig(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = dbName
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol // như prod
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	// status trên DB trắng: mọi migration đều pending.
	out, err := runMigrateAction(ctx, pool, "status", "")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(out["pending"].([]string)); n < 11 {
		t.Fatalf("pending = %d, want >= 11", n)
	}

	// apply toàn bộ.
	out, err = runMigrateAction(ctx, pool, "apply", "")
	if err != nil {
		t.Fatal(err)
	}
	if failed, ok := out["failed"]; ok {
		t.Fatalf("migration %v lỗi: %v", failed, out["error"])
	}
	if n := len(out["ran"].([]string)); n < 11 {
		t.Fatalf("ran = %d, want >= 11", n)
	}

	// Bảng của 009/010 phải tồn tại và apply lại phải là no-op.
	for _, tbl := range []string{"checkins", "reward_events", "reward_daily"} {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT to_regclass($1) IS NOT NULL`, tbl).Scan(&exists); err != nil || !exists {
			t.Fatalf("bảng %s chưa tồn tại (err=%v)", tbl, err)
		}
	}
	out, err = runMigrateAction(ctx, pool, "apply", "")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(out["ran"].([]string)); n != 0 {
		t.Fatalf("apply lần 2 chạy %d migration, want 0 (idempotent)", n)
	}

	// baseline trên DB đã áp hết: không có gì để đánh dấu.
	out, err = runMigrateAction(ctx, pool, "baseline", "008")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(out["baselined"].([]string)); n != 0 {
		t.Fatalf("baseline đánh dấu %d, want 0", n)
	}
}
