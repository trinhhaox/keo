// Package reward quản lý điểm thưởng: check-in hàng ngày (+1 điểm) và thưởng
// quãng đường đi bộ/chạy bộ từ Strava (+1 điểm / km tròn).
//
// Tỷ giá hệ thống 1 điểm = 1 VNĐ, thưởng là điểm NGUYÊN nên cộng THẲNG vào ví
// qua ledger txn reward_payout — không có tầng tích lũy lẻ.
//
// Idempotency hai lớp, đều bằng UNIQUE constraint theo nguyên tắc chung của
// repo: checkins(user_id, vn_date) → reward_events(user_id, kind, ref_key);
// ledger dùng key reward:user=N:event=M nên một event chỉ phát điểm một lần.
package reward

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hao/keo/challenge"
	"github.com/hao/keo/ledger"
)

const (
	KindCheckin  = "checkin"
	KindDistance = "activity_distance"

	checkinPoints = 1 // mỗi check-in = 1 điểm

	// DailyCap là trần tổng điểm thưởng một user nhận được trong một ngày
	// (giờ VN) — chốt chặn anti-farm: GPS spoof / app giả 1000km cũng chỉ
	// lấy được tối đa chừng này mỗi ngày.
	DailyCap = 100
)

var ErrAlreadyCheckedIn = errors.New("reward: hôm nay đã check-in")

// Ledger là phần duy nhất của ledger store mà reward cần — post trong tx của caller.
type Ledger interface {
	PostTx(ctx context.Context, tx pgx.Tx, req ledger.Request) (ledger.Result, error)
}

type Service struct {
	pool   *pgxpool.Pool
	ledger Ledger
}

func NewService(pool *pgxpool.Pool, l Ledger) *Service {
	return &Service{pool: pool, ledger: l}
}

// Accrual là kết quả một lần cộng thưởng.
type Accrual struct {
	Granted bool  // false = sự kiện đã tồn tại (replay), không có gì thay đổi
	Points  int64 // số điểm THẬT vừa cộng vào ví (đã clamp theo trần ngày)
	Capped  bool  // true = bị trần ngày cắt bớt (Points < số đáng lẽ được nhận)
}

// AccrueTx cộng điểm thưởng trong transaction của CALLER (worker Strava dùng
// đường này để accrual nằm chung tx với upsert activity). Idempotent theo
// (userID, kind, refKey); tổng điểm cấp trong một ngày VN bị chặn ở DailyCap.
func (s *Service) AccrueTx(ctx context.Context, tx pgx.Tx, userID int64, kind, refKey string, points int64) (Accrual, error) {
	if points <= 0 {
		return Accrual{}, fmt.Errorf("reward: points phải > 0, nhận %d", points)
	}
	today := time.Now().In(challenge.VNLocation).Format("2006-01-02")

	// 1. Khóa counter ngày (tạo nếu chưa có). Row lock giữ tới hết tx —
	//    accrual song song của cùng user xếp hàng, không ai lách được trần.
	if _, err := tx.Exec(ctx, `
		INSERT INTO reward_daily (user_id, vn_date) VALUES ($1, $2::date)
		ON CONFLICT (user_id, vn_date) DO NOTHING`,
		userID, today,
	); err != nil {
		return Accrual{}, fmt.Errorf("ensure daily row: %w", err)
	}
	var grantedToday int64
	if err := tx.QueryRow(ctx, `
		SELECT points FROM reward_daily
		WHERE user_id = $1 AND vn_date = $2::date FOR UPDATE`,
		userID, today,
	).Scan(&grantedToday); err != nil {
		return Accrual{}, fmt.Errorf("lock daily counter: %w", err)
	}
	granted := min(points, max(DailyCap-grantedToday, 0))

	// 2. Ghi sự kiện với số ĐÃ clamp — điểm quyết định idempotency. Kịch trần
	//    (granted = 0) vẫn ghi để đốt ref_key: replay/update sau không cấp lại.
	var eventID int64
	err := tx.QueryRow(ctx, `
		INSERT INTO reward_events (user_id, kind, ref_key, points)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, kind, ref_key) DO NOTHING
		RETURNING id`,
		userID, kind, refKey, granted,
	).Scan(&eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Accrual{Granted: false}, nil // replay — không cộng lại
	}
	if err != nil {
		return Accrual{}, fmt.Errorf("insert reward event: %w", err)
	}
	if granted == 0 {
		return Accrual{Granted: true, Points: 0, Capped: true}, nil
	}

	// 3. Cập nhật counter ngày + phát điểm vào ví. Idempotency key theo event
	//    nên retry vô hại; tx rollback thì event cũng rollback — luôn nhất quán.
	if _, err := tx.Exec(ctx, `
		UPDATE reward_daily SET points = points + $3
		WHERE user_id = $1 AND vn_date = $2::date`,
		userID, today, granted,
	); err != nil {
		return Accrual{}, fmt.Errorf("update daily counter: %w", err)
	}
	req := ledger.RewardPayoutRequest(userID, granted, fmt.Sprintf("event=%d", eventID))
	if _, err := s.ledger.PostTx(ctx, tx, req); err != nil {
		return Accrual{}, fmt.Errorf("post reward payout: %w", err)
	}
	return Accrual{Granted: true, Points: granted, Capped: granted < points}, nil
}

// CheckIn ghi nhận check-in hôm nay (theo giờ VN) và cộng 1 điểm vào ví.
// Gọi lần 2 trong ngày trả ErrAlreadyCheckedIn.
func (s *Service) CheckIn(ctx context.Context, userID int64, now time.Time) (Accrual, error) {
	vnDate := now.In(challenge.VNLocation).Format("2006-01-02")

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Accrual{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var checkinID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO checkins (user_id, vn_date)
		VALUES ($1, $2::date)
		ON CONFLICT (user_id, vn_date) DO NOTHING
		RETURNING id`,
		userID, vnDate,
	).Scan(&checkinID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Accrual{}, ErrAlreadyCheckedIn
	}
	if err != nil {
		return Accrual{}, fmt.Errorf("insert checkin: %w", err)
	}

	acc, err := s.AccrueTx(ctx, tx, userID, KindCheckin, vnDate, checkinPoints)
	if err != nil {
		return Accrual{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Accrual{}, fmt.Errorf("commit: %w", err)
	}
	return acc, nil
}

// AccrueActivity cộng thưởng quãng đường cho một hoạt động Strava, trong tx
// của worker ingest. Chỉ đi bộ/chạy bộ, bỏ qua nhập tay; 1 điểm mỗi km TRÒN.
// Cấp MỘT lần theo (source, externalID): update tăng km sau đó không top-up
// (chống farm bằng sửa hoạt động), delete không thu hồi (điểm có thể đã tiêu).
// Nếu lần đầu floor(km) = 0 thì chưa có sự kiện — update sau vẫn cấp được.
func (s *Service) AccrueActivity(ctx context.Context, tx pgx.Tx, userID int64,
	sport, source, externalID string, distanceM float64, manual bool) error {
	if manual || (sport != "walk" && sport != "run") {
		return nil
	}
	points := int64(math.Floor(distanceM / 1000.0))
	if points <= 0 {
		return nil
	}
	refKey := fmt.Sprintf("%s:%s", source, externalID)
	_, err := s.AccrueTx(ctx, tx, userID, KindDistance, refKey, points)
	return err
}

// Summary là trạng thái thưởng hiển thị cho user.
type Summary struct {
	CheckedInToday bool  `json:"checked_in_today"` // hôm nay (giờ VN) đã check-in chưa
	TotalPoints    int64 `json:"total_points"`     // tổng điểm thưởng đã nhận từ trước tới nay
}

// GetSummary đọc trạng thái thưởng của user.
func (s *Service) GetSummary(ctx context.Context, userID int64, now time.Time) (Summary, error) {
	vnDate := now.In(challenge.VNLocation).Format("2006-01-02")
	var out Summary
	err := s.pool.QueryRow(ctx, `
		SELECT
			EXISTS (SELECT 1 FROM checkins WHERE user_id = $1 AND vn_date = $2::date),
			COALESCE((SELECT SUM(points) FROM reward_events WHERE user_id = $1), 0)`,
		userID, vnDate,
	).Scan(&out.CheckedInToday, &out.TotalPoints)
	if err != nil {
		return Summary{}, fmt.Errorf("read reward summary: %w", err)
	}
	return out, nil
}
