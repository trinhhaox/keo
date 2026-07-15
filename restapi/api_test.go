package restapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hao/keo/challenge"
	"github.com/hao/keo/ledger"
	"github.com/hao/keo/payment"
)

// TestIntegrationUserJourney chạy trọn hành trình một user qua HTTP thật
// (httptest): mua điểm (webhook SePay giả với API key đúng) → tạo kèo (tự
// động vào kèo) → xem ví/tiến độ → đổi thưởng → check-in nhận thưởng →
// verify từng số dư.
func TestIntegrationUserJourney(t *testing.T) {
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

	// Auth giả: user id trong header. Bản thật thay bằng session/JWT.
	auth := func(r *http.Request) (int64, error) {
		return strconv.ParseInt(r.Header.Get("X-User-ID"), 10, 64)
	}

	const sepayKey = "test-sepay-key"
	ledgerStore := ledger.NewPGStore(pool)
	challengeStore := challenge.NewStore(pool, ledgerStore)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	paySvc := payment.NewService(pool, ledgerStore, sepayKey, "0000", "MB", quiet)
	apiSrv := NewServer(pool, ledgerStore, challengeStore, auth, []byte("test-jwt"))

	mux := http.NewServeMux()
	apiSrv.Routes(mux)
	paySvc.Routes(mux, auth)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	var userID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (display_name) VALUES ('journey-test') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatal(err)
	}

	call := func(method, path string, body any, out any) *http.Response {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			json.NewEncoder(&buf).Encode(body)
		}
		req, _ := http.NewRequest(method, ts.URL+path, &buf)
		req.Header.Set("X-User-ID", fmt.Sprint(userID))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if out != nil {
			defer resp.Body.Close()
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				t.Fatalf("%s %s: decode: %v", method, path, err)
			}
		}
		return resp
	}

	// ===== 1. Mua gói 1.000.000 điểm (1 điểm = 1 VNĐ) =====
	var order struct {
		OrderURL   string `json:"order_url"`
		AppTransID string `json:"app_trans_id"`
	}
	call("POST", "/v1/wallet/purchase", map[string]int64{"pack_points": 1_000_000}, &order)
	if order.AppTransID == "" {
		t.Fatal("không nhận được app_trans_id")
	}

	// Ví chưa có điểm — chỉ callback mới là nguồn sự thật, app tự báo không tính.
	var wallet struct{ Available, Locked int64 }
	call("GET", "/v1/wallet", nil, &wallet)
	if wallet.Available != 0 {
		t.Fatalf("chưa callback mà đã có %d điểm", wallet.Available)
	}

	// SePay bắn webhook (API key đúng). Bắn HAI lần — lần hai phải idempotent,
	// vẫn success và không cộng điểm lần nữa.
	webhook := func(apiKey string) *http.Response {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"gateway": "TestBank", "content": order.AppTransID,
			"transferType": "in", "transferAmount": 1_000_000, "referenceCode": "987",
		})
		req, _ := http.NewRequest("POST", ts.URL+"/webhooks/sepay", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Apikey "+apiKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	for i := 0; i < 2; i++ {
		resp := webhook(sepayKey)
		var cbResp struct {
			Success bool `json:"success"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&cbResp); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if !cbResp.Success {
			t.Fatalf("webhook lần %d: success = false, want true", i+1)
		}
	}
	// API key sai phải bị từ chối.
	resp := webhook("wrong-key")
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("webhook API key sai mà vẫn được chấp nhận")
	}

	call("GET", "/v1/wallet", nil, &wallet)
	if wallet.Available != 1_120_000 { // 1.000.000 + 120.000 bonus, cộng đúng MỘT lần
		t.Fatalf("available = %d, want 1120000", wallet.Available)
	}

	// ===== 2. Tạo kèo (tự động vào kèo, khóa cược) =====
	var created struct {
		ChallengeID int64 `json:"challenge_id"`
	}
	call("POST", "/v1/challenges", map[string]any{
		"title": "Chạy 20km/tuần (journey)", "sport": "run",
		"goal_type": "weekly_distance_km", "goal_value": 20,
		"source": "strava", "stake_points": 200, "duration_days": 30,
	}, &created)
	if created.ChallengeID == 0 {
		t.Fatal("tạo kèo thất bại")
	}

	call("GET", "/v1/wallet", nil, &wallet)
	if wallet.Available != 1_119_800 || wallet.Locked != 200 {
		t.Fatalf("sau tạo kèo: available=%d locked=%d, want 1119800/200", wallet.Available, wallet.Locked)
	}

	// Join lại kèo của chính mình → idempotent, không khóa thêm.
	resp = call("POST", fmt.Sprintf("/v1/challenges/%d/join", created.ChallengeID), nil, nil)
	resp.Body.Close()
	call("GET", "/v1/wallet", nil, &wallet)
	if wallet.Locked != 200 {
		t.Fatalf("join lại bị khóa thêm: locked = %d", wallet.Locked)
	}

	// ===== 3. Tiến độ =====
	var mine []struct {
		ChallengeID  int64  `json:"challenge_id"`
		Status       string `json:"status"`
		PeriodsTotal int    `json:"periods_total"`
	}
	call("GET", "/v1/me/challenges", nil, &mine)
	found := false
	for _, m := range mine {
		if m.ChallengeID == created.ChallengeID {
			found = true
			if m.Status != "active" || m.PeriodsTotal != 5 { // 30 ngày weekly = 5 kỳ
				t.Fatalf("kèo: status=%s periods=%d, want active/5", m.Status, m.PeriodsTotal)
			}
		}
	}
	if !found {
		t.Fatal("không thấy kèo vừa tạo trong /v1/me/challenges")
	}

	// ===== 4. Đổi thưởng =====
	call("POST", "/v1/redemptions", map[string]string{"sku": "ticket-sg-night-run"}, nil)
	call("GET", "/v1/wallet", nil, &wallet)
	if wallet.Available != 519_800 { // 1.119.800 − 600.000
		t.Fatalf("sau đổi vé: available = %d, want 519800", wallet.Available)
	}

	// Đổi món vượt số dư → 402, ví không đổi.
	resp = call("POST", "/v1/redemptions", map[string]string{"sku": "gear-trail-shoes"}, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("đổi thưởng vượt số dư: status = %d, want 402", resp.StatusCode)
	}
	call("GET", "/v1/wallet", nil, &wallet)
	if wallet.Available != 519_800 {
		t.Fatalf("ví bị đổi sau request thất bại: %d", wallet.Available)
	}

	// ===== 5. Check-in nhận thưởng =====
	var checkin struct {
		PointsGranted int64 `json:"points_granted"`
	}
	call("POST", "/v1/checkins", nil, &checkin)
	if checkin.PointsGranted != 1 {
		t.Fatalf("check-in: %+v, want points_granted=1", checkin)
	}
	// Check-in lần 2 trong ngày → 409, không cộng thêm.
	resp = call("POST", "/v1/checkins", nil, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("check-in lần 2: status = %d, want 409", resp.StatusCode)
	}
	var rewards struct {
		CheckedInToday bool  `json:"checked_in_today"`
		TotalPoints    int64 `json:"total_points"`
	}
	call("GET", "/v1/rewards", nil, &rewards)
	if !rewards.CheckedInToday || rewards.TotalPoints != 1 {
		t.Fatalf("rewards: %+v, want checked_in_today=true total=1", rewards)
	}
	// Ví +1 điểm — thưởng cộng thẳng.
	call("GET", "/v1/wallet", nil, &wallet)
	if wallet.Available != 519_801 {
		t.Fatalf("ví sau check-in: %d, want 519801", wallet.Available)
	}
}
