package challenge

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hao/keo/ledger"
)

// Store gom nghiệp vụ challenge trên AlloyDB. Nhận thẳng *ledger.PGStore
// (không phải interface Store) vì cần PostTx — ghi ledger trong cùng
// transaction với enrollment.
type Store struct {
	pool   *pgxpool.Pool
	ledger *ledger.PGStore
}

func NewStore(pool *pgxpool.Pool, l *ledger.PGStore) *Store {
	return &Store{pool: pool, ledger: l}
}

func (s *Store) Create(ctx context.Context, c Challenge) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO challenges
			(creator_id, title, sport, goal_type, goal_value, source,
			 stake_points, fee_bps, pass_ratio, start_at, end_at, grace_hours, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'open')
		RETURNING id`,
		c.CreatorID, c.Title, c.Sport, c.GoalType, c.GoalValue, c.Source,
		c.StakePoints, c.FeeBps, c.PassRatio, c.StartAt, c.EndAt, c.GraceHours,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create challenge: %w", err)
	}
	return id, nil
}

// Join là luồng vào kèo. MỘT transaction cho cả ba việc:
//
//  1. Insert enrollment (UNIQUE(challenge_id,user_id) → double-tap vô hại)
//  2. Khóa cược qua ledger.PostTx (idempotency key derive từ challenge+user)
//  3. Sinh sẵn toàn bộ enrollment_periods
//
// Crash ở bất kỳ đâu → rollback trọn vẹn, không có trạng thái "đã trừ điểm
// nhưng chưa vào kèo" hay ngược lại. Đây là lý do ledger cần PostTx.
func (s *Store) Join(ctx context.Context, challengeID, userID int64) (int64, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// Đọc + validate challenge. FOR SHARE chặn settlement job đổi status
	// trong lúc mình đang join (join vs settle trên cùng challenge).
	var c Challenge
	err = tx.QueryRow(ctx, `
		SELECT id, goal_type, goal_value, stake_points, start_at, end_at, status
		FROM challenges WHERE id = $1 FOR SHARE`,
		challengeID,
	).Scan(&c.ID, &c.GoalType, &c.GoalValue, &c.StakePoints, &c.StartAt, &c.EndAt, &c.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("load challenge: %w", err)
	}
	if c.Status != StatusOpen && c.Status != StatusActive {
		return 0, fmt.Errorf("%w: status=%s", ErrNotJoinable, c.Status)
	}

	// Khóa cược trước để lấy stake_txn_id (enrollment có FK tới nó).
	// Idempotent: user retry sẽ replay đúng txn cũ.
	res, err := s.ledger.PostTx(ctx, tx, ledger.StakeLockRequest(userID, challengeID, c.StakePoints))
	if err != nil {
		return 0, err // gồm cả ErrInsufficientBalance — tx đã abort, rollback sạch
	}

	var enrollmentID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO enrollments (challenge_id, user_id, stake_txn_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (challenge_id, user_id) DO NOTHING
		RETURNING id`,
		challengeID, userID, res.TxnID,
	).Scan(&enrollmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Đã join từ trước (ledger txn cũng là replay) — idempotent, trả id cũ.
		if err := tx.QueryRow(ctx,
			`SELECT id FROM enrollments WHERE challenge_id = $1 AND user_id = $2`,
			challengeID, userID,
		).Scan(&enrollmentID); err != nil {
			return 0, fmt.Errorf("fetch existing enrollment: %w", err)
		}
		return enrollmentID, tx.Commit(ctx)
	}
	if err != nil {
		return 0, fmt.Errorf("insert enrollment: %w", err)
	}

	// Sinh sẵn toàn bộ kỳ đánh giá — ingestion về sau chỉ UPDATE achieved.
	periods, err := GeneratePeriods(c.GoalType, c.GoalValue, c.StartAt, c.EndAt, VNLocation)
	if err != nil {
		return 0, fmt.Errorf("generate periods: %w", err)
	}
	batch := &pgx.Batch{}
	for _, p := range periods {
		batch.Queue(`
			INSERT INTO enrollment_periods (enrollment_id, period_start, period_end, target)
			VALUES ($1, $2, $3, $4)`,
			enrollmentID, p.Start, p.End, p.Target,
		)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return 0, fmt.Errorf("insert periods: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return enrollmentID, nil
}
