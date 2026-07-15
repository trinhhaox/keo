package handler

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hao/keo/migrations"
)

// registerMigrateRoute gắn POST /api/admin/migrate — chạy migration từ runtime
// Vercel, cùng bookkeeping schema_migrations với scripts/migrate.sh (mỗi file
// một transaction, idempotent, chạy theo thứ tự tên file).
//
// Bảo vệ: yêu cầu header X-Migrate-Key khớp env MIGRATE_KEY (so sánh
// constant-time); thiếu env = endpoint đóng hẳn. Body JSON:
//
//	{"action":"status"}                    → danh sách applied/pending
//	{"action":"baseline","through":"008"}  → đánh dấu đã áp tới prefix, KHÔNG chạy SQL
//	                                         (cho DB từng migrate tay, không có bookkeeping)
//	{"action":"apply"}                     → chạy lần lượt các migration pending
func registerMigrateRoute(mux *http.ServeMux, pool *pgxpool.Pool) {
	mux.HandleFunc("POST /api/admin/migrate", func(w http.ResponseWriter, r *http.Request) {
		key := os.Getenv("MIGRATE_KEY")
		if key == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Migrate-Key")), []byte(key)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var body struct {
			Action  string `json:"action"`
			Through string `json:"through"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		out, err := runMigrateAction(ctx, pool, body.Action, body.Through)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	})
}

func runMigrateAction(ctx context.Context, pool *pgxpool.Pool, action, through string) (map[string]any, error) {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return nil, fmt.Errorf("ensure schema_migrations: %w", err)
	}

	all, err := listMigrationFiles()
	if err != nil {
		return nil, err
	}
	applied, err := appliedVersions(ctx, pool)
	if err != nil {
		return nil, err
	}
	var pending []string
	for _, v := range all {
		if !applied[v] {
			pending = append(pending, v)
		}
	}

	switch action {
	case "status":
		return map[string]any{"applied": keys(applied), "pending": pending}, nil

	case "baseline":
		if through == "" {
			return nil, fmt.Errorf("baseline cần 'through'")
		}
		var marked []string
		for _, v := range pending {
			if !strings.HasPrefix(v, through) && v > through {
				break
			}
			if _, err := pool.Exec(ctx,
				`INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`, v,
			); err != nil {
				return nil, fmt.Errorf("baseline %s: %w", v, err)
			}
			marked = append(marked, v)
		}
		return map[string]any{"baselined": marked}, nil

	case "apply":
		var ran []string
		for _, v := range pending {
			sqlBytes, err := migrations.Files.ReadFile(v)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", v, err)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				return nil, fmt.Errorf("begin %s: %w", v, err)
			}
			if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
				tx.Rollback(ctx)
				return map[string]any{"ran": ran, "failed": v, "error": err.Error()}, nil
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO schema_migrations (version) VALUES ($1)`, v); err != nil {
				tx.Rollback(ctx)
				return nil, fmt.Errorf("record %s: %w", v, err)
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, fmt.Errorf("commit %s: %w", v, err)
			}
			ran = append(ran, v)
		}
		return map[string]any{"ran": ran}, nil
	}
	return nil, fmt.Errorf("action không hợp lệ: %q", action)
}

func listMigrationFiles() ([]string, error) {
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func appliedVersions(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read applied: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
