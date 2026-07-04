package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hao/keo/challenge"
	"github.com/hao/keo/ledger"
)

// ===== Fakes =====

type fakeStrava struct {
	activities map[int64]StravaActivity
}

func (f *fakeStrava) GetActivity(_ context.Context, _, id int64) (StravaActivity, error) {
	a, ok := f.activities[id]
	if !ok {
		return StravaActivity{}, fmt.Errorf("activity %d not found", id)
	}
	return a, nil
}

type okVerifier struct{}

func (okVerifier) Verify(context.Context, int64, string) error { return nil }

// ===== Setup =====

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

// setupUserWithChallenge: user + điểm + kèo đang mở + đã join.
// Trả về (userID, enrollmentID).
func setupUserWithChallenge(t *testing.T, pool *pgxpool.Pool, tag string, c challenge.Challenge) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	ledgerStore := ledger.NewPGStore(pool)
	store := challenge.NewStore(pool, ledgerStore)

	var userID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (display_name) VALUES ($1) RETURNING id`,
		"ingest-test-"+tag,
	).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.NewService(ledgerStore).Purchase(ctx, userID, 1000, "test",
		fmt.Sprintf("ingest-%s-%d-%d", tag, os.Getpid(), time.Now().UnixNano())); err != nil {
		t.Fatal(err)
	}

	c.CreatorID = userID
	challengeID, err := store.Create(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	enrollmentID, err := store.Join(ctx, challengeID, userID)
	if err != nil {
		t.Fatal(err)
	}
	return userID, enrollmentID
}

func periodOf(t *testing.T, pool *pgxpool.Pool, enrollmentID int64, date time.Time) (achieved float64, passed bool) {
	t.Helper()
	err := pool.QueryRow(context.Background(), `
		SELECT achieved, passed FROM enrollment_periods
		WHERE enrollment_id = $1 AND period_start <= $2::date AND period_end > $2::date`,
		enrollmentID, date,
	).Scan(&achieved, &passed)
	if err != nil {
		t.Fatalf("read period: %v", err)
	}
	return achieved, passed
}

// ===== Tests =====

// TestIntegrationStravaFlow: webhook create → update → delete, verify recompute
// đúng ở từng bước (đè chứ không cộng dồn, xóa thì trừ về 0).
func TestIntegrationStravaFlow(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Now()

	userID, enrollmentID := setupUserWithChallenge(t, pool, "strava", challenge.Challenge{
		Title: "Chạy 20km/tuần (ingest test)", Sport: "run",
		GoalType: challenge.GoalWeeklyDistanceKm, GoalValue: 20, Source: "strava",
		StakePoints: 100, FeeBps: 1000, PassRatio: 0.8,
		StartAt: now.AddDate(0, 0, -2), EndAt: now.AddDate(0, 0, 12), GraceHours: 48,
	})

	// Gắn tài khoản Strava.
	athleteID := time.Now().UnixNano()
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_integrations (user_id, provider, external_user_id)
		VALUES ($1, 'strava', $2)`, userID, fmt.Sprint(athleteID)); err != nil {
		t.Fatal(err)
	}

	// Activity ID unique mỗi lần chạy — như Strava thật (ID toàn cục duy nhất).
	// Bài học: tái sử dụng ID cứng giữa các lần chạy khiến UNIQUE(source,
	// external_activity_id) giữ activity lại với user của lần chạy trước.
	actID := time.Now().UnixNano()
	strava := &fakeStrava{activities: map[int64]StravaActivity{
		actID: {ID: actID, Type: "Run", DistanceM: 5000, MovingTimeS: 1800, StartDate: now},
	}}
	worker := NewStravaWorker(pool, strava, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	push := func(aspect string, eventTime int64) {
		t.Helper()
		payload := fmt.Sprintf(
			`{"object_type":"activity","object_id":%d,"aspect_type":"%s","owner_id":%d,"event_time":%d}`,
			actID, aspect, athleteID, eventTime)
		if err := EnqueueStravaEvent(ctx, pool, []byte(payload)); err != nil {
			t.Fatal(err)
		}
		// Enqueue trùng phải vô hại (Strava retry).
		if err := EnqueueStravaEvent(ctx, pool, []byte(payload)); err != nil {
			t.Fatal(err)
		}
		n, err := worker.ProcessOnce(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("processed %d events, want 1", n)
		}
	}

	// 1. create: chạy 5km → achieved 5.
	push("create", 1)
	if achieved, _ := periodOf(t, pool, enrollmentID, now); achieved != 5 {
		t.Fatalf("sau create: achieved = %v, want 5", achieved)
	}

	// 2. update: Strava sửa thành 21km → achieved phải là 21 (ĐÈ, không phải 26).
	strava.activities[actID] = StravaActivity{ID: actID, Type: "Run", DistanceM: 21000, MovingTimeS: 7200, StartDate: now}
	push("update", 2)
	achieved, passed := periodOf(t, pool, enrollmentID, now)
	if achieved != 21 {
		t.Fatalf("sau update: achieved = %v, want 21 — recompute bị cộng dồn thay vì đè?", achieved)
	}
	if !passed {
		t.Fatalf("21km >= target 20km nhưng passed = false")
	}

	// 3. delete: user xóa hoạt động → achieved về 0, passed về false.
	push("delete", 3)
	achieved, passed = periodOf(t, pool, enrollmentID, now)
	if achieved != 0 || passed {
		t.Fatalf("sau delete: achieved = %v passed = %v, want 0/false", achieved, passed)
	}

	// Hoạt động nhập tay không được tính.
	strava.activities[actID] = StravaActivity{ID: actID, Type: "Run", DistanceM: 42000, Manual: true, StartDate: now}
	push("create", 4)
	if achieved, _ := periodOf(t, pool, enrollmentID, now); achieved != 0 {
		t.Fatalf("manual entry bị tính vào kèo: achieved = %v", achieved)
	}
}

// TestIntegrationHealthSyncOverwrite: sync bucket là ĐÈ theo (user,sport,ngày) —
// sync lại số thấp hơn phải kéo achieved xuống và lật passed.
func TestIntegrationHealthSyncOverwrite(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	now := time.Now()

	userID, enrollmentID := setupUserWithChallenge(t, pool, "health", challenge.Challenge{
		Title: "10k bước/ngày (ingest test)", Sport: "walk",
		GoalType: challenge.GoalDailySteps, GoalValue: 10000, Source: "google_fit",
		StakePoints: 100, FeeBps: 1000, PassRatio: 0.8,
		StartAt: now.AddDate(0, 0, -2), EndAt: now.AddDate(0, 0, 5), GraceHours: 48,
	})

	svc := NewHealthSyncService(pool, okVerifier{})
	today := now.In(challenge.VNLocation).Format("2006-01-02")

	// Sync 12k bước → kỳ hôm nay passed.
	if err := svc.Sync(ctx, userID, ProviderGoogleFit, "tok", []HealthBucket{
		{Date: today, Sport: "walk", Steps: 12000},
	}); err != nil {
		t.Fatal(err)
	}
	achieved, passed := periodOf(t, pool, enrollmentID, now)
	if achieved != 12000 || !passed {
		t.Fatalf("sau sync 12k: achieved = %v passed = %v", achieved, passed)
	}

	// Thiết bị sửa số liệu, sync lại 8k → phải ĐÈ xuống 8000 và passed = false.
	if err := svc.Sync(ctx, userID, ProviderGoogleFit, "tok", []HealthBucket{
		{Date: today, Sport: "walk", Steps: 8000},
	}); err != nil {
		t.Fatal(err)
	}
	achieved, passed = periodOf(t, pool, enrollmentID, now)
	if achieved != 8000 || passed {
		t.Fatalf("sau sync đè 8k: achieved = %v passed = %v, want 8000/false", achieved, passed)
	}

	// Nguồn strava không được đi qua endpoint này.
	if err := svc.Sync(ctx, userID, ProviderStrava, "tok", nil); err == nil {
		t.Fatal("sync source=strava phải bị từ chối")
	}
}
