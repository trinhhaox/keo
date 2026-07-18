package challenge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hao/keo/ledger"
)

// SettlementJob chạy định kỳ (cron ~15 phút). An toàn khi chạy nhiều instance
// song song: pha 1 là các UPDATE nguyên tử, pha 2 dùng FOR UPDATE SKIP LOCKED.
type SettlementJob struct {
	store *Store
	log   *slog.Logger
}

func NewSettlementJob(store *Store, log *slog.Logger) *SettlementJob {
	return &SettlementJob{store: store, log: log}
}

// Run thực hiện một chu kỳ: chuyển trạng thái rồi chốt sổ tối đa batchSize kèo.
func (j *SettlementJob) Run(ctx context.Context, now time.Time, batchSize int) error {
	if err := j.transition(ctx, now); err != nil {
		return fmt.Errorf("transition: %w", err)
	}
	ids, err := j.pickSettling(ctx, batchSize)
	if err != nil {
		return fmt.Errorf("pick: %w", err)
	}
	for _, id := range ids {
		if err := j.settleOne(ctx, id); err != nil {
			// Một kèo lỗi không chặn các kèo khác — log và đi tiếp,
			// lần cron sau sẽ thử lại (status vẫn là settling).
			j.log.Error("settle failed", "challenge_id", id, "err", err)
		}
	}
	return nil
}

// transition — pha 1: mỗi câu là một optimistic lock, chạy song song an toàn.
func (j *SettlementJob) transition(ctx context.Context, now time.Time) error {
	pool := j.store.pool
	if _, err := pool.Exec(ctx,
		`UPDATE challenges SET status = 'active' WHERE status = 'open' AND start_at <= $1`, now); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx,
		`UPDATE challenges SET status = 'grace' WHERE status = 'active' AND end_at <= $1`, now); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `
		UPDATE challenges SET status = 'settling'
		WHERE status = 'grace'
		  AND end_at + make_interval(hours => grace_hours) <= $1`, now); err != nil {
		return err
	}
	return nil
}

func (j *SettlementJob) pickSettling(ctx context.Context, limit int) ([]int64, error) {
	rows, err := j.store.pool.Query(ctx,
		`SELECT id FROM challenges WHERE status = 'settling' ORDER BY end_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// settleOne chốt sổ MỘT kèo trong MỘT transaction:
// đọc kết quả từng enrollment → chia thưởng qua ledger → cập nhật trạng thái.
// Idempotent hai lớp: FOR UPDATE SKIP LOCKED + recheck status chống chạy trùng
// giữa các instance; idempotency key 'settle:challenge={id}' của ledger chống
// double-payout nếu vẫn lọt qua bằng cách nào đó.
func (j *SettlementJob) settleOne(ctx context.Context, challengeID int64) error {
	tx, err := j.store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var c Challenge
	err = tx.QueryRow(ctx, `
		SELECT id, stake_points, fee_bps, pass_ratio, status, is_charity, charity_id
		FROM challenges WHERE id = $1 AND status = 'settling'
		FOR UPDATE SKIP LOCKED`,
		challengeID,
	).Scan(&c.ID, &c.StakePoints, &c.FeeBps, &c.PassRatio, &c.Status, &c.IsCharity, &c.CharityID)
	if err == pgx.ErrNoRows {
		return nil // instance khác đang xử lý, hoặc đã settled — bỏ qua
	}
	if err != nil {
		return fmt.Errorf("lock challenge: %w", err)
	}

	// Gom kết quả mọi enrollment active trong một query.
	rows, err := tx.Query(ctx, `
		SELECT e.id, e.user_id,
		       COUNT(*)                        AS total,
		       COUNT(*) FILTER (WHERE p.passed) AS passed
		FROM enrollments e
		JOIN enrollment_periods p ON p.enrollment_id = e.id
		WHERE e.challenge_id = $1 AND e.status = 'active'
		GROUP BY e.id, e.user_id`,
		challengeID,
	)
	if err != nil {
		return fmt.Errorf("aggregate periods: %w", err)
	}
	type outcome struct {
		enrollmentID, userID int64
		total, passed        int
	}
	var completed, failed []outcome
	for rows.Next() {
		var o outcome
		if err := rows.Scan(&o.enrollmentID, &o.userID, &o.total, &o.passed); err != nil {
			rows.Close()
			return err
		}
		if Passed(o.passed, o.total, c.PassRatio) {
			completed = append(completed, o)
		} else {
			failed = append(failed, o)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Chia thưởng — một ledger transaction cho cả kèo.
	params := ledger.SettlementParams{
		ChallengeID: c.ID,
		StakePoints: c.StakePoints,
		FeeBps:      c.FeeBps,
	}
	for _, o := range completed {
		params.CompletedIDs = append(params.CompletedIDs, o.userID)
	}
	for _, o := range failed {
		params.FailedIDs = append(params.FailedIDs, o.userID)
	}
	if len(completed)+len(failed) > 0 {
		var req ledger.Request
		var err error
		if c.IsCharity {
			req, err = ledger.CharitySettlementRequest(params, c.CharityID)
		} else {
			req, err = ledger.SettlementRequest(params)
		}
		if err != nil {
			return fmt.Errorf("build settlement: %w", err)
		}
		if _, err := j.store.ledger.PostTx(ctx, tx, req); err != nil {
			return fmt.Errorf("post settlement: %w", err)
		}
	}

	// Cập nhật kết quả từng enrollment.
	batch := &pgx.Batch{}
	mark := func(o outcome, st EnrollStatus) {
		result, _ := json.Marshal(map[string]int{"periods_total": o.total, "periods_passed": o.passed})
		batch.Queue(`
			UPDATE enrollments SET status = $1, result = $2, settled_at = now()
			WHERE id = $3`,
			st, string(result), o.enrollmentID)
	}
	for _, o := range completed {
		mark(o, EnrollCompleted)
	}
	for _, o := range failed {
		mark(o, EnrollFailed)
	}
	batch.Queue(`UPDATE challenges SET status = 'settled' WHERE id = $1`, challengeID)
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("mark results: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	j.log.Info("challenge settled",
		"challenge_id", challengeID, "completed", len(completed), "failed", len(failed))
	return nil
}
