package ingest

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestIntegrationStravaClientRefresh: token trong DB đã hết hạn → GetActivity
// phải tự refresh, LƯU LẠI token mới (Strava xoay vòng refresh token — quên
// lưu là lần refresh sau chết), rồi fetch activity với token mới.
func TestIntegrationStravaClientRefresh(t *testing.T) {
	dsn := os.Getenv("LEDGER_TEST_DSN")
	if dsn == "" {
		t.Skip("set LEDGER_TEST_DSN để chạy integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	// Strava giả: /oauth/token cấp token mới; /api/v3/activities đòi đúng token mới.
	refreshCalls := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth/token":
			refreshCalls++
			if r.FormValue("grant_type") != "refresh_token" || r.FormValue("refresh_token") != "old-refresh" {
				http.Error(w, "bad grant", http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "new-access",
				"refresh_token": "new-refresh",
				"expires_at":    time.Now().Add(6 * time.Hour).Unix(),
			})
		case r.URL.Path == "/api/v3/activities/424242":
			if r.Header.Get("Authorization") != "Bearer new-access" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"id": 424242, "sport_type": "Run", "distance": 5230.5,
				"moving_time": 1810, "average_heartrate": 152.3,
				"manual": false, "start_date": time.Now().UTC().Format(time.RFC3339),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)

	key := make([]byte, 32)
	rand.Read(key)
	cph, err := NewAESGCMCipher(key)
	if err != nil {
		t.Fatal(err)
	}

	// Seed user + integration với token ĐÃ HẾT HẠN.
	var userID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (display_name) VALUES ('strava-client-test') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	athleteID := time.Now().UnixNano()
	oldAccess, _ := cph.Encrypt([]byte("old-access"))
	oldRefresh, _ := cph.Encrypt([]byte("old-refresh"))
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_integrations
			(user_id, provider, external_user_id, access_token_enc, refresh_token_enc, token_expires_at)
		VALUES ($1, 'strava', $2, $3, $4, now() - interval '1 hour')`,
		userID, fmt.Sprint(athleteID), oldAccess, oldRefresh); err != nil {
		t.Fatal(err)
	}

	client := &HTTPStravaClient{
		Pool: pool, Cipher: cph,
		ClientID: "cid", ClientSecret: "secret", BaseURL: fake.URL,
	}

	act, err := client.GetActivity(ctx, athleteID, 424242)
	if err != nil {
		t.Fatal(err)
	}
	if act.Type != "Run" || act.DistanceM != 5230.5 || act.MovingTimeS != 1810 {
		t.Fatalf("activity map sai: %+v", act)
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh được gọi %d lần, want 1", refreshCalls)
	}

	// Token mới phải đã được lưu (xoay vòng) — gọi lần 2 KHÔNG refresh nữa.
	if _, err := client.GetActivity(ctx, athleteID, 424242); err != nil {
		t.Fatal(err)
	}
	if refreshCalls != 1 {
		t.Fatalf("token mới không được lưu — refresh bị gọi lại (%d lần)", refreshCalls)
	}

	// Verify refresh token trong DB là bản mới, đã mã hóa.
	var refreshEnc []byte
	if err := pool.QueryRow(ctx, `
		SELECT refresh_token_enc FROM user_integrations
		WHERE provider = 'strava' AND external_user_id = $1`,
		fmt.Sprint(athleteID)).Scan(&refreshEnc); err != nil {
		t.Fatal(err)
	}
	dec, err := cph.Decrypt(refreshEnc)
	if err != nil {
		t.Fatal(err)
	}
	if string(dec) != "new-refresh" {
		t.Fatalf("refresh token trong DB = %q, want new-refresh", dec)
	}
}
