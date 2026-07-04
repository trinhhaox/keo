package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StravaEvent là payload webhook của Strava (một event = một thay đổi).
// https://developers.strava.com/docs/webhooks/
type StravaEvent struct {
	ObjectType string `json:"object_type"` // "activity" | "athlete"
	ObjectID   int64  `json:"object_id"`
	AspectType string `json:"aspect_type"` // "create" | "update" | "delete"
	OwnerID    int64  `json:"owner_id"`    // athlete_id
	EventTime  int64  `json:"event_time"`
}

// EnqueueStravaEvent là toàn bộ việc webhook handler làm: parse tối thiểu,
// INSERT vào inbox, trả về ngay. Không gọi API, không đụng nghiệp vụ —
// Strava yêu cầu trả 200 trong 2 giây, và mọi lỗi xử lý về sau không được
// làm mất event.
func EnqueueStravaEvent(ctx context.Context, pool *pgxpool.Pool, payload []byte) error {
	var ev StravaEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return fmt.Errorf("parse strava event: %w", err)
	}
	dedup := fmt.Sprintf("%d:%d:%s:%d", ev.OwnerID, ev.ObjectID, ev.AspectType, ev.EventTime)
	_, err := pool.Exec(ctx, `
		INSERT INTO webhook_inbox (provider, dedup_key, payload)
		VALUES ('strava', $1, $2)
		ON CONFLICT (provider, dedup_key) DO NOTHING`,
		dedup, payload,
	)
	if err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}
	return nil
}

// StravaClient trừu tượng hóa Strava API — implementation thật lo OAuth
// refresh token + rate limit (200 req/15 phút với app mới); test dùng fake.
type StravaClient interface {
	// GetActivity fetch chi tiết hoạt động, dùng token của athlete tương ứng.
	GetActivity(ctx context.Context, athleteID, activityID int64) (StravaActivity, error)
}

type StravaActivity struct {
	ID           int64
	Type         string // "Run", "Ride", "Swim"...
	DistanceM    float64
	MovingTimeS  int
	AvgHeartrate float64
	Manual       bool // nhập tay — không tính vào kèo
	StartDate    time.Time
}

// StravaWorker poll inbox và xử lý từng event.
type StravaWorker struct {
	pool   *pgxpool.Pool
	client StravaClient
	log    *slog.Logger
}

func NewStravaWorker(pool *pgxpool.Pool, client StravaClient, log *slog.Logger) *StravaWorker {
	return &StravaWorker{pool: pool, client: client, log: log}
}

// RunLoop chạy tới khi ctx hủy. An toàn chạy nhiều instance: SKIP LOCKED.
func (w *StravaWorker) RunLoop(ctx context.Context, idle time.Duration) {
	for ctx.Err() == nil {
		n, err := w.ProcessOnce(ctx)
		if err != nil {
			w.log.Error("strava worker", "err", err)
		}
		if n == 0 {
			select {
			case <-ctx.Done():
			case <-time.After(idle):
			}
		}
	}
}

// ProcessOnce lấy và xử lý MỘT event pending. Trả về số event đã xử lý (0/1).
//
// Toàn bộ nằm trong một DB transaction: claim event (SKIP LOCKED) → fetch
// Strava → upsert activity → recompute → mark processed. Crash ở đâu cũng
// rollback về pending, lần sau xử lý lại; upsert + recompute idempotent nên
// xử lý lại vô hại.
//
// Trade-off có chủ đích: gọi HTTP API trong lúc giữ DB tx. Ở volume thấp
// điều này đơn giản và đúng; khi scale, chuyển sang claim-then-process
// (tx1 đánh dấu processing → gọi API ngoài tx → tx2 apply) kèm timeout
// requeue cho event kẹt ở processing.
func (w *StravaWorker) ProcessOnce(ctx context.Context) (int, error) {
	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var inboxID int64
	var payload []byte
	err = tx.QueryRow(ctx, `
		SELECT id, payload FROM webhook_inbox
		WHERE provider = 'strava' AND status = 'pending'
		ORDER BY id LIMIT 1
		FOR UPDATE SKIP LOCKED`,
	).Scan(&inboxID, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("claim: %w", err)
	}

	if err := w.handle(ctx, tx, payload); err != nil {
		// Lỗi xử lý (API down, dữ liệu lạ): đánh dấu failed kèm lý do để
		// điều tra, KHÔNG rollback về pending — tránh retry vô hạn chặn queue.
		// Requeue thủ công/định kỳ sau khi hiểu nguyên nhân.
		if _, mErr := w.pool.Exec(ctx, `
			UPDATE webhook_inbox SET status = 'failed', error = $1, processed_at = now()
			WHERE id = $2`, err.Error(), inboxID); mErr != nil {
			return 0, fmt.Errorf("mark failed: %v (original: %w)", mErr, err)
		}
		w.log.Error("strava event failed", "inbox_id", inboxID, "err", err)
		return 1, nil
	}

	if _, err := tx.Exec(ctx, `
		UPDATE webhook_inbox SET status = 'processed', processed_at = now()
		WHERE id = $1`, inboxID); err != nil {
		return 0, fmt.Errorf("mark processed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return 1, nil
}

func (w *StravaWorker) handle(ctx context.Context, tx pgx.Tx, payload []byte) error {
	var ev StravaEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if ev.ObjectType != "activity" {
		return nil // athlete event (revoke...) — xử lý riêng sau, giờ bỏ qua
	}

	// Map athlete → user. Không có integration = user chưa từng kết nối
	// hoặc đã revoke → bỏ qua êm.
	var userID int64
	err := tx.QueryRow(ctx, `
		SELECT user_id FROM user_integrations
		WHERE provider = 'strava' AND external_user_id = $1 AND revoked_at IS NULL`,
		fmt.Sprint(ev.OwnerID),
	).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve user: %w", err)
	}

	externalID := fmt.Sprint(ev.ObjectID)

	if ev.AspectType == "delete" {
		// Xóa hoạt động → recompute các kỳ từng chứa nó.
		var sport string
		var date time.Time
		err := tx.QueryRow(ctx, `
			DELETE FROM activities
			WHERE source = 'strava' AND external_activity_id = $1
			RETURNING sport, vn_date`, externalID,
		).Scan(&sport, &date)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // chưa từng ingest — không có gì để recompute
		}
		if err != nil {
			return fmt.Errorf("delete activity: %w", err)
		}
		return recompute(ctx, tx, userID, sport, ProviderStrava, date)
	}

	// create / update: fetch chi tiết rồi upsert + recompute.
	sa, err := w.client.GetActivity(ctx, ev.OwnerID, ev.ObjectID)
	if err != nil {
		return fmt.Errorf("fetch activity %d: %w", ev.ObjectID, err)
	}
	sport := sportFromStrava(sa.Type)
	if sport == "" {
		return nil // bộ môn ngoài phạm vi app
	}
	act := Activity{
		UserID:       userID,
		Source:       ProviderStrava,
		ExternalID:   externalID,
		Sport:        sport,
		DistanceM:    sa.DistanceM,
		DurationS:    sa.MovingTimeS,
		Sessions:     1,
		AvgHeartrate: sa.AvgHeartrate,
		IsManual:     sa.Manual,
		StartedAt:    sa.StartDate,
	}
	if err := upsertActivity(ctx, tx, act); err != nil {
		return err
	}
	return recompute(ctx, tx, userID, sport, ProviderStrava, vnDate(sa.StartDate))
}
