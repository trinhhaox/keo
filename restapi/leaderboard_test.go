package restapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hao/keo/challenge"
	"github.com/hao/keo/ledger"
)

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	if v != nil {
		if err := json.NewEncoder(&buf).Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	return &buf
}

// TestIntegrationLeaderboardAndStats: hai user vào cùng một kèo, một user có
// hoạt động 3 ngày liên tiếp → leaderboard trả đủ 2 người (đúng cờ is_me),
// /v1/me/activities trả hoạt động, /v1/me/stats trả streak 3 ngày.
func TestIntegrationLeaderboardAndStats(t *testing.T) {
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

	ledgerStore := ledger.NewPGStore(pool)
	ledgerSvc := ledger.NewService(ledgerStore)
	challengeStore := challenge.NewStore(pool, ledgerStore)
	auth := func(r *http.Request) (int64, error) {
		return strconv.ParseInt(r.Header.Get("X-User-ID"), 10, 64)
	}
	apiSrv := NewServer(pool, ledgerStore, challengeStore, auth, auth, []byte("test-jwt"))
	mux := http.NewServeMux()
	apiSrv.Routes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	newUser := func(name string) int64 {
		var id int64
		if err := pool.QueryRow(ctx,
			`INSERT INTO users (display_name) VALUES ($1) RETURNING id`, name,
		).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := ledgerSvc.Purchase(ctx, id, 1000, "test",
			fmt.Sprintf("lb-%d-%d", os.Getpid(), id)); err != nil {
			t.Fatal(err)
		}
		return id
	}
	alice, bob := newUser("lb-alice"), newUser("lb-bob")

	call := func(uid int64, method, path string, body any, out any) int {
		t.Helper()
		req, _ := http.NewRequest(method, ts.URL+path, jsonBody(t, body))
		req.Header.Set("X-User-ID", fmt.Sprint(uid))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if out != nil && resp.StatusCode < 300 {
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				t.Fatalf("%s %s: decode: %v", method, path, err)
			}
		}
		return resp.StatusCode
	}

	// Alice tạo kèo (tự động vào), Bob vào theo.
	var created struct {
		ChallengeID int64 `json:"challenge_id"`
	}
	if code := call(alice, "POST", "/v1/challenges", map[string]any{
		"title": "Chạy 20 km/tuần (lb test)", "sport": "run",
		"goal_type": "weekly_distance_km", "goal_value": 20,
		"source": "strava", "stake_points": 100, "duration_days": 14,
	}, &created); code != 200 {
		t.Fatalf("create challenge: HTTP %d", code)
	}
	if code := call(bob, "POST", fmt.Sprintf("/v1/challenges/%d/join", created.ChallengeID), nil, nil); code != 200 {
		t.Fatalf("bob join: HTTP %d", code)
	}

	// Bob có hoạt động 3 ngày liên tiếp tới hôm nay (giờ VN).
	today := time.Now().In(challenge.VNLocation)
	for i := 0; i < 3; i++ {
		day := today.AddDate(0, 0, -i)
		if _, err := pool.Exec(ctx, `
			INSERT INTO activities (user_id, source, external_activity_id, sport,
			                        distance_m, duration_s, started_at, vn_date)
			VALUES ($1, 'strava', $2, 'run', 5000, 1800, $3, $4::date)
			ON CONFLICT (source, external_activity_id, started_at) DO NOTHING`,
			bob, fmt.Sprintf("lb-act-%d-%d", bob, i), day, day.Format("2006-01-02"),
		); err != nil {
			t.Fatal(err)
		}
	}

	// ===== Leaderboard =====
	var lb struct {
		Pot     int64 `json:"pot"`
		Entries []struct {
			UserID int64 `json:"user_id"`
			IsMe   bool  `json:"is_me"`
		} `json:"entries"`
	}
	if code := call(bob, "GET", fmt.Sprintf("/v1/challenges/%d/leaderboard", created.ChallengeID), nil, &lb); code != 200 {
		t.Fatalf("leaderboard: HTTP %d", code)
	}
	if len(lb.Entries) != 2 {
		t.Fatalf("leaderboard có %d người, want 2", len(lb.Entries))
	}
	if lb.Pot != 200 {
		t.Errorf("pot = %d, want 200", lb.Pot)
	}
	for _, e := range lb.Entries {
		if want := e.UserID == bob; e.IsMe != want {
			t.Errorf("user %d: is_me = %v, want %v", e.UserID, e.IsMe, want)
		}
	}
	if code := call(bob, "GET", "/v1/challenges/999999999/leaderboard", nil, nil); code != 404 {
		t.Errorf("leaderboard kèo ma: HTTP %d, want 404", code)
	}

	// ===== Hoạt động gần đây =====
	var acts []struct {
		Sport     string  `json:"sport"`
		DistanceM float64 `json:"distance_m"`
	}
	if code := call(bob, "GET", "/v1/me/activities", nil, &acts); code != 200 {
		t.Fatalf("activities: HTTP %d", code)
	}
	if len(acts) != 3 {
		t.Fatalf("activities trả %d bản ghi, want 3", len(acts))
	}

	// ===== Streak =====
	var stats struct {
		StreakDays     int     `json:"streak_days"`
		WeekDistanceM  float64 `json:"week_distance_m"`
		WeekActiveDays int     `json:"week_active_days"`
	}
	if code := call(bob, "GET", "/v1/me/stats", nil, &stats); code != 200 {
		t.Fatalf("stats: HTTP %d", code)
	}
	if stats.StreakDays != 3 {
		t.Errorf("streak = %d, want 3", stats.StreakDays)
	}
	// Alice chưa vận động gì — mọi số phải là 0, không lỗi.
	stats = struct {
		StreakDays     int     `json:"streak_days"`
		WeekDistanceM  float64 `json:"week_distance_m"`
		WeekActiveDays int     `json:"week_active_days"`
	}{}
	if code := call(alice, "GET", "/v1/me/stats", nil, &stats); code != 200 {
		t.Fatalf("stats alice: HTTP %d", code)
	}
	if stats.StreakDays != 0 || stats.WeekActiveDays != 0 {
		t.Errorf("alice stats = %+v, want toàn 0", stats)
	}
}
