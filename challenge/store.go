package challenge

import (
	"context"
	"errors"
	"fmt"
	"time"

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
			 stake_points, fee_bps, pass_ratio, start_at, end_at, grace_hours, status, max_participants, is_charity, charity_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'open',$13,$14,$15)
		RETURNING id`,
		c.CreatorID, c.Title, c.Sport, c.GoalType, c.GoalValue, c.Source,
		c.StakePoints, c.FeeBps, c.PassRatio, c.StartAt, c.EndAt, c.GraceHours, c.MaxParticipants,
		c.IsCharity, c.CharityID,
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

	// Đọc + validate challenge. FOR UPDATE (không phải FOR SHARE) có chủ đích:
	// serialize các join song song trên cùng kèo để COUNT participants bên dưới
	// không bị race vượt max_participants, đồng thời vẫn chặn settlement job
	// đổi status trong lúc đang join.
	var c Challenge
	err = tx.QueryRow(ctx, `
		SELECT id, goal_type, goal_value, stake_points, start_at, end_at, status, max_participants, is_charity, charity_id
		FROM challenges WHERE id = $1 FOR UPDATE`,
		challengeID,
	).Scan(&c.ID, &c.GoalType, &c.GoalValue, &c.StakePoints, &c.StartAt, &c.EndAt, &c.Status, &c.MaxParticipants, &c.IsCharity, &c.CharityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("load challenge: %w", err)
	}

	// Đếm số người tham gia hiện tại
	var currentParticipants int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM enrollments WHERE challenge_id = $1`,
		challengeID,
	).Scan(&currentParticipants); err != nil {
		return 0, fmt.Errorf("count participants: %w", err)
	}

	// Kiểm tra xem user này đã tham gia chưa
	var alreadyJoined bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM enrollments WHERE challenge_id = $1 AND user_id = $2)`,
		challengeID, userID,
	).Scan(&alreadyJoined)
	if err != nil {
		return 0, fmt.Errorf("check enrollment exists: %w", err)
	}

	if !alreadyJoined && c.MaxParticipants > 0 && currentParticipants >= c.MaxParticipants {
		return 0, fmt.Errorf("%w: kèo đã đầy (%d/%d người)", ErrChallengeFull, currentParticipants, c.MaxParticipants)
	}

	// Chặn join nếu đã bước sang ngày thứ 2 của thử thách
	todayStr := time.Now().In(VNLocation).Format("2006-01-02")
	startStr := c.StartAt.In(VNLocation).Format("2006-01-02")
	if todayStr > startStr {
		return 0, fmt.Errorf("%w: kèo đã bắt đầu từ ngày %s, không thể tham gia thêm", ErrNotJoinable, startStr)
	}

	if c.Status != StatusOpen && c.Status != StatusActive {
		return 0, fmt.Errorf("%w: status=%s", ErrNotJoinable, c.Status)
	}

	enrollmentID, err := s.enroll(ctx, tx, c, userID)
	if err != nil {
		return 0, err // gồm cả ErrInsufficientBalance — tx đã abort, rollback sạch
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return enrollmentID, nil
}

// enroll khóa cược + tạo enrollment + sinh sẵn kỳ đánh giá cho (challenge, user)
// TRONG tx của caller — caller tự commit. Idempotent: đã join thì trả id cũ.
// c phải đã có ID + StakePoints + GoalType/GoalValue + StartAt/EndAt.
func (s *Store) enroll(ctx context.Context, tx pgx.Tx, c Challenge, userID int64) (int64, error) {
	// Khóa cược trước để lấy stake_txn_id (enrollment có FK tới nó).
	// Idempotent: user retry sẽ replay đúng txn cũ.
	res, err := s.ledger.PostTx(ctx, tx, ledger.StakeLockRequest(userID, c.ID, c.StakePoints))
	if err != nil {
		return 0, err
	}

	var enrollmentID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO enrollments (challenge_id, user_id, stake_txn_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (challenge_id, user_id) DO NOTHING
		RETURNING id`,
		c.ID, userID, res.TxnID,
	).Scan(&enrollmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Đã join từ trước (ledger txn cũng là replay) — idempotent, trả id cũ.
		if err := tx.QueryRow(ctx,
			`SELECT id FROM enrollments WHERE challenge_id = $1 AND user_id = $2`,
			c.ID, userID,
		).Scan(&enrollmentID); err != nil {
			return 0, fmt.Errorf("fetch existing enrollment: %w", err)
		}
		return enrollmentID, nil
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
	return enrollmentID, nil
}

// CreateWithCreator tạo kèo VÀ enroll người tạo trong MỘT transaction. Trước
// đây createChallenge gọi Create (commit) rồi Join (tx khác): nếu Join lỗi/crash
// giữa chừng thì kèo đã tồn tại nhưng chủ kèo chưa vào / chưa khóa cược (mồ côi).
func (s *Store) CreateWithCreator(ctx context.Context, c Challenge) (int64, int64, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var challengeID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO challenges
			(creator_id, title, sport, goal_type, goal_value, source,
			 stake_points, fee_bps, pass_ratio, start_at, end_at, grace_hours, status, max_participants, is_charity, charity_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'open',$13,$14,$15)
		RETURNING id`,
		c.CreatorID, c.Title, c.Sport, c.GoalType, c.GoalValue, c.Source,
		c.StakePoints, c.FeeBps, c.PassRatio, c.StartAt, c.EndAt, c.GraceHours, c.MaxParticipants,
		c.IsCharity, c.CharityID,
	).Scan(&challengeID)
	if err != nil {
		return 0, 0, fmt.Errorf("create challenge: %w", err)
	}
	c.ID = challengeID

	enrollmentID, err := s.enroll(ctx, tx, c, c.CreatorID)
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, fmt.Errorf("commit: %w", err)
	}
	return challengeID, enrollmentID, nil
}
