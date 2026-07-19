package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StravaEvent là payload webhook của Strava (một event = một thay đổi).
// https://developers.strava.com/docs/webhooks/
type StravaEvent struct {
	ObjectType     string `json:"object_type"` // "activity" | "athlete"
	ObjectID       int64  `json:"object_id"`
	AspectType     string `json:"aspect_type"` // "create" | "update" | "delete"
	OwnerID        int64  `json:"owner_id"`    // athlete_id
	EventTime      int64  `json:"event_time"`
	SubscriptionID int64  `json:"subscription_id"` // của subscription do chính app tạo
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
		dedup, string(payload),
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

// RewardAccruer cộng điểm thưởng quãng đường trong CÙNG tx với upsert activity.
// Interface nhỏ để ingest không phụ thuộc cứng vào package reward; nil = tắt thưởng.
type RewardAccruer interface {
	AccrueActivity(ctx context.Context, tx pgx.Tx, userID int64,
		sport, source, externalID string, distanceM float64, manual bool) error
}

// StravaWorker poll inbox và xử lý từng event.
type StravaWorker struct {
	pool    *pgxpool.Pool
	client  StravaClient
	log     *slog.Logger
	rewards RewardAccruer // optional — nil thì bỏ qua thưởng
}

func NewStravaWorker(pool *pgxpool.Pool, client StravaClient, log *slog.Logger) *StravaWorker {
	return &StravaWorker{pool: pool, client: client, log: log}
}

// WithRewards bật cộng thưởng quãng đường cho hoạt động ingest được.
func (w *StravaWorker) WithRewards(r RewardAccruer) *StravaWorker {
	w.rewards = r
	return w
}

// RunLoop chạy tới khi ctx hủy. An toàn chạy nhiều instance: SKIP LOCKED.
func (w *StravaWorker) RunLoop(ctx context.Context, idle time.Duration) {
	// Dọn event kẹt 'processing' từ lần chạy trước (crash) ngay khi khởi động.
	if err := w.RequeueStuckProcessing(ctx, 5*time.Minute); err != nil {
		w.log.Error("requeue stuck processing", "err", err)
	}
	for ctx.Err() == nil {
		n, err := w.ProcessOnce(ctx)
		if err != nil {
			w.log.Error("strava worker", "err", err)
		}
		if n == 0 {
			// Không có event đến hạn — dọn stuck rồi nghỉ.
			if err := w.RequeueStuckProcessing(ctx, 5*time.Minute); err != nil {
				w.log.Error("requeue stuck processing", "err", err)
			}
			select {
			case <-ctx.Done():
			case <-time.After(idle):
			}
		}
	}
}

const maxStravaAttempts = 10

// ProcessOnce claim MỘT event đến hạn rồi xử lý theo claim-then-process:
//
//	tx1: đánh dấu 'processing' + commit  → nhả lock/connection
//	     fetch Strava (NGOÀI tx)
//	tx2: apply (upsert/recompute/reward) → mark 'processed'
//
// Nhờ vậy call HTTP KHÔNG giữ DB connection (trước đây fetch trong tx: Strava
// chậm = giữ conn tới hết timeout). Upsert/recompute/reward idempotent nên
// reprocess (crash giữa chừng → sweeper requeue) vô hại.
//
// Lỗi TẠM THỜI (timeout/429/5xx) giữ 'pending' + backoff để tự retry; lỗi VĨNH
// VIỄN (dữ liệu lạ, 4xx) hoặc hết maxStravaAttempts → 'failed'. Trả 0/1.
func (w *StravaWorker) ProcessOnce(ctx context.Context) (int, error) {
	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin claim: %w", err)
	}
	defer tx.Rollback(ctx)

	var inboxID int64
	var payload []byte
	var attempts int
	err = tx.QueryRow(ctx, `
		SELECT id, payload, attempts FROM webhook_inbox
		WHERE provider = 'strava' AND status = 'pending' AND next_attempt_at <= now()
		ORDER BY id LIMIT 1
		FOR UPDATE SKIP LOCKED`,
	).Scan(&inboxID, &payload, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("claim: %w", err)
	}
	attempts++
	if _, err := tx.Exec(ctx, `
		UPDATE webhook_inbox SET status = 'processing', claimed_at = now(), attempts = $2
		WHERE id = $1`, inboxID, attempts); err != nil {
		return 0, fmt.Errorf("mark processing: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit claim: %w", err)
	}

	// Xử lý NGOÀI tx claim (gồm cả HTTP Strava).
	applyErr := w.process(ctx, payload)

	// ctx xử lý có thể đã hết hạn (fetch Strava chậm vượt ngân sách serverless).
	// Dùng context RIÊNG cho lệnh cập nhật trạng thái cuối để luôn ghi được —
	// nếu tái dùng ctx đã chết, event kẹt vĩnh viễn ở 'processing' và cron 500.
	finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if applyErr == nil {
		if _, err := w.pool.Exec(finishCtx, `
			UPDATE webhook_inbox SET status = 'processed', processed_at = now(), error = NULL, claimed_at = NULL
			WHERE id = $1`, inboxID); err != nil {
			return 0, fmt.Errorf("mark processed: %w", err)
		}
		return 1, nil
	}

	// Tạm thời + chưa hết lượt → giữ 'pending' với backoff (30s..180s).
	if isTransient(applyErr) && attempts < maxStravaAttempts {
		backoffSec := min(attempts, 6) * 30
		if _, err := w.pool.Exec(finishCtx, `
			UPDATE webhook_inbox
			SET status = 'pending', claimed_at = NULL, error = $2,
			    next_attempt_at = now() + make_interval(secs => $3)
			WHERE id = $1`, inboxID, applyErr.Error(), backoffSec); err != nil {
			return 0, fmt.Errorf("requeue transient: %w", err)
		}
		w.log.Warn("strava event tạm lỗi — requeue", "inbox_id", inboxID, "attempt", attempts, "backoff_s", backoffSec, "err", applyErr)
		return 1, nil
	}

	// Vĩnh viễn / hết lượt → failed, chờ điều tra hoặc requeue tay.
	if _, err := w.pool.Exec(finishCtx, `
		UPDATE webhook_inbox SET status = 'failed', error = $2, processed_at = now(), claimed_at = NULL
		WHERE id = $1`, inboxID, applyErr.Error()); err != nil {
		return 0, fmt.Errorf("mark failed: %w", err)
	}
	w.log.Error("strava event failed", "inbox_id", inboxID, "attempts", attempts, "err", applyErr)
	return 1, nil
}

// RequeueStuckProcessing đưa event kẹt ở 'processing' quá olderThan (crash giữa
// tx1 và tx2, hoặc process quá lâu) về lại 'pending'. Apply idempotent nên vô hại.
func (w *StravaWorker) RequeueStuckProcessing(ctx context.Context, olderThan time.Duration) error {
	_, err := w.pool.Exec(ctx, `
		UPDATE webhook_inbox
		SET status = 'pending', claimed_at = NULL, next_attempt_at = now()
		WHERE provider = 'strava' AND status = 'processing'
		  AND claimed_at < now() - make_interval(secs => $1)`,
		int(olderThan.Seconds()),
	)
	return err
}

// process phân giải user + fetch Strava (NGOÀI tx) rồi apply trong tx riêng.
func (w *StravaWorker) process(ctx context.Context, payload []byte) error {
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
	err := w.pool.QueryRow(ctx, `
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
		return w.applyDelete(ctx, userID, externalID)
	}

	// create / update: fetch NGOÀI tx trước, rồi mới apply.
	sa, err := w.client.GetActivity(ctx, ev.OwnerID, ev.ObjectID)
	if err != nil {
		return fmt.Errorf("fetch activity %d: %w", ev.ObjectID, err)
	}
	sport := sportFromStrava(sa.Type)
	if sport == "" {
		return nil // bộ môn ngoài phạm vi app
	}
	return w.applyActivity(ctx, userID, externalID, sport, sa)
}

// applyDelete xóa hoạt động + recompute các kỳ từng chứa nó, trong 1 tx.
func (w *StravaWorker) applyDelete(ctx context.Context, userID int64, externalID string) error {
	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var sport string
	var date time.Time
	err = tx.QueryRow(ctx, `
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
	if err := recompute(ctx, tx, userID, sport, ProviderStrava, date); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// applyActivity upsert hoạt động + recompute (cả kỳ cũ) + thưởng, trong 1 tx.
func (w *StravaWorker) applyActivity(ctx context.Context, userID int64, externalID, sport string, sa StravaActivity) error {
	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

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
	stale, err := upsertActivity(ctx, tx, act)
	if err != nil {
		return err
	}
	// Bản ghi cũ bị thay thế (update đổi giờ/ngày/bộ môn, hoặc thuộc user cũ
	// khi tài khoản nguồn đổi chủ) → recompute cả kỳ cũ, theo đúng chủ cũ.
	for _, p := range stale {
		if err := recompute(ctx, tx, p.userID, p.sport, ProviderStrava, p.date); err != nil {
			return err
		}
	}
	if w.rewards != nil {
		if err := w.rewards.AccrueActivity(ctx, tx, userID,
			sport, ProviderStrava, externalID, sa.DistanceM, sa.Manual); err != nil {
			return fmt.Errorf("accrue reward: %w", err)
		}
	}
	if err := recompute(ctx, tx, userID, sport, ProviderStrava, vnDate(sa.StartDate)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// isTransient: lỗi tạm thời đáng retry (timeout, 429, 5xx, lỗi mạng) vs vĩnh
// viễn (dữ liệu lạ, 4xx) — cái sau retry vô ích, để 'failed' cho điều tra.
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	s := strings.ToLower(err.Error())
	for _, sub := range []string{"strava 429", "strava 5", "oauth 5", "timeout", "connection refused", "no such host", "eof", "reset by peer"} {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
