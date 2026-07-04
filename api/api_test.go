package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
// (httptest): mua điểm (callback ZaloPay giả với MAC đúng) → tạo kèo (tự
// động vào kèo) → xem ví/tiến độ → đổi thưởng → verify từng số dư.
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

	const key2 = "test-key2"
	ledgerStore := ledger.NewPGStore(pool)
	challengeStore := challenge.NewStore(pool, ledgerStore)
	paySvc := payment.NewService(pool, ledgerStore, fakeGateway{}, key2)
	apiSrv := NewServer(pool, ledgerStore, challengeStore, auth)

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

	// ===== 1. Mua gói 1000 điểm =====
	var order struct {
		OrderURL   string `json:"order_url"`
		AppTransID string `json:"app_trans_id"`
	}
	call("POST", "/v1/wallet/purchase", map[string]int64{"pack_points": 1000}, &order)
	if order.AppTransID == "" {
		t.Fatal("không nhận được app_trans_id")
	}

	// Ví chưa có điểm — chỉ callback mới là nguồn sự thật, app tự báo không tính.
	var wallet struct{ Available, Locked int64 }
	call("GET", "/v1/wallet", nil, &wallet)
	if wallet.Available != 0 {
		t.Fatalf("chưa callback mà đã có %d điểm", wallet.Available)
	}

	// ZaloPay bắn callback (MAC đúng với key2). Bắn HAI lần — lần hai phải
	// idempotent, vẫn return_code 1 và không cộng điểm lần nữa.
	data, _ := json.Marshal(map[string]any{
		"app_trans_id": order.AppTransID, "amount": 1_000_000, "zp_trans_id": 987,
	})
	mac := hmac.New(sha256.New, []byte(key2))
	mac.Write(data)
	cb := map[string]any{"data": string(data), "mac": hex.EncodeToString(mac.Sum(nil)), "type": 1}
	for i := 0; i < 2; i++ {
		var cbResp struct {
			ReturnCode int `json:"return_code"`
		}
		call("POST", "/callbacks/zalopay", cb, &cbResp)
		if cbResp.ReturnCode != 1 {
			t.Fatalf("callback lần %d: return_code = %d, want 1", i+1, cbResp.ReturnCode)
		}
	}
	// MAC sai phải bị từ chối.
	var badResp struct {
		ReturnCode int `json:"return_code"`
	}
	call("POST", "/callbacks/zalopay", map[string]any{"data": string(data), "mac": "deadbeef"}, &badResp)
	if badResp.ReturnCode == 1 {
		t.Fatal("callback MAC sai mà vẫn được chấp nhận")
	}

	call("GET", "/v1/wallet", nil, &wallet)
	if wallet.Available != 1120 { // 1000 + 120 bonus, cộng đúng MỘT lần
		t.Fatalf("available = %d, want 1120", wallet.Available)
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
	if wallet.Available != 920 || wallet.Locked != 200 {
		t.Fatalf("sau tạo kèo: available=%d locked=%d, want 920/200", wallet.Available, wallet.Locked)
	}

	// Join lại kèo của chính mình → idempotent, không khóa thêm.
	resp := call("POST", fmt.Sprintf("/v1/challenges/%d/join", created.ChallengeID), nil, nil)
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
	if wallet.Available != 320 { // 920 − 600
		t.Fatalf("sau đổi vé: available = %d, want 320", wallet.Available)
	}

	// Đổi món vượt số dư → 402, ví không đổi.
	resp = call("POST", "/v1/redemptions", map[string]string{"sku": "gear-trail-shoes"}, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("đổi thưởng vượt số dư: status = %d, want 402", resp.StatusCode)
	}
	call("GET", "/v1/wallet", nil, &wallet)
	if wallet.Available != 320 {
		t.Fatalf("ví bị đổi sau request thất bại: %d", wallet.Available)
	}
}

type fakeGateway struct{}

func (fakeGateway) CreateOrder(_ context.Context, appTransID string, _ int64, _ string) (string, error) {
	return "https://pay.zalopay.test/" + appTransID, nil
}
