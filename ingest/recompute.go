package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// stalePeriod là (user, sport, ngày) của một bản ghi cũ vừa bị thay thế —
// caller phải recompute các kỳ chứa nó để tiến độ không kẹt số liệu cũ.
// Mang theo userID vì bản ghi stale có thể thuộc user KHÁC (tài khoản nguồn
// đổi chủ) — recompute bằng user của event hiện tại sẽ bỏ sót user cũ.
type stalePeriod struct {
	userID int64
	sport  string
	date   time.Time
}

// upsertActivity ghi/đè một hoạt động. ON CONFLICT DO UPDATE ở đây là an toàn
// (khác với balance cache của ledger): đây là dữ liệu thay thế, không phải delta.
//
// Bảng activities partition theo started_at (005) nên UNIQUE là (source,
// external_activity_id, started_at) — không có UNIQUE toàn cục 2 cột. Để giữ
// bất biến "một hoạt động nguồn = một row", xóa trước mọi bản ghi cùng hoạt
// động nhưng khác started_at (Strava update đổi giờ bắt đầu), trả về (sport,
// vn_date) của chúng để caller recompute cả các kỳ cũ.
//
// user_id nằm trong danh sách DO UPDATE có chủ đích: nếu một tài khoản nguồn
// đổi chủ (user A revoke Strava, user B kết nối đúng tài khoản đó), hoạt động
// re-sync phải thuộc về chủ hiện tại — bỏ sót cột này khiến activity kẹt lại
// với user cũ và recompute cho user mới luôn ra 0.
func upsertActivity(ctx context.Context, tx pgx.Tx, a Activity) ([]stalePeriod, error) {
	if a.Sessions == 0 {
		a.Sessions = 1
	}
	rows, err := tx.Query(ctx, `
		DELETE FROM activities
		WHERE source = $1 AND external_activity_id = $2 AND started_at <> $3
		RETURNING user_id, sport, vn_date`,
		a.Source, a.ExternalID, a.StartedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("delete stale activity %s/%s: %w", a.Source, a.ExternalID, err)
	}
	var stale []stalePeriod
	for rows.Next() {
		var p stalePeriod
		if err := rows.Scan(&p.userID, &p.sport, &p.date); err != nil {
			rows.Close()
			return nil, err
		}
		stale = append(stale, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO activities
			(user_id, source, external_activity_id, sport, distance_m, duration_s,
			 steps, sessions, avg_heartrate, is_manual_entry, started_at, vn_date)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,0),$10,$11,$12)
		ON CONFLICT (source, external_activity_id, started_at) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			sport = EXCLUDED.sport,
			distance_m = EXCLUDED.distance_m,
			duration_s = EXCLUDED.duration_s,
			steps = EXCLUDED.steps,
			sessions = EXCLUDED.sessions,
			avg_heartrate = EXCLUDED.avg_heartrate,
			is_manual_entry = EXCLUDED.is_manual_entry,
			vn_date = EXCLUDED.vn_date,
			ingested_at = now()`,
		a.UserID, a.Source, a.ExternalID, a.Sport, a.DistanceM, a.DurationS,
		a.Steps, a.Sessions, a.AvgHeartrate, a.IsManual, a.StartedAt, vnDate(a.StartedAt),
	)
	if err != nil {
		return nil, fmt.Errorf("upsert activity %s/%s: %w", a.Source, a.ExternalID, err)
	}
	return stale, nil
}

// recompute tính lại achieved/passed cho MỌI kỳ đang active của user bị ảnh
// hưởng bởi một hoạt động tại ngày date (sport + source khớp với kèo).
//
// Recompute-from-source thay vì cộng dồn: chậm hơn một chút nhưng đúng tuyệt
// đối với update/delete/re-sync, và idempotent — chạy lại N lần vẫn ra một
// kết quả. Query nằm trong cùng tx với upsert/delete activity.
func recompute(ctx context.Context, tx pgx.Tx, userID int64, sport, source string, date time.Time) error {
	rows, err := tx.Query(ctx, `
		SELECT p.enrollment_id, p.period_start, p.period_end, p.target, c.goal_type
		FROM enrollment_periods p
		JOIN enrollments e ON e.id = p.enrollment_id
		JOIN challenges c ON c.id = e.challenge_id
		WHERE e.user_id = $1 AND e.status = 'active'
		  AND c.sport = $2 AND c.source = $3
		  AND p.period_start <= $4::date AND p.period_end > $4::date`,
		userID, sport, source, date,
	)
	if err != nil {
		return fmt.Errorf("find affected periods: %w", err)
	}
	type period struct {
		enrollmentID int64
		start, end   time.Time
		target       float64
		goalType     string
	}
	var affected []period
	for rows.Next() {
		var p period
		if err := rows.Scan(&p.enrollmentID, &p.start, &p.end, &p.target, &p.goalType); err != nil {
			rows.Close()
			return err
		}
		affected = append(affected, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, p := range affected {
		var agg string
		switch p.goalType {
		case "daily_steps":
			agg = `COALESCE(SUM(steps), 0)`
		case "weekly_distance_km":
			agg = `COALESCE(SUM(distance_m), 0) / 1000.0`
		case "weekly_sessions":
			agg = `COALESCE(SUM(sessions), 0)`
		default:
			return fmt.Errorf("unknown goal type %q", p.goalType)
		}
		var achieved float64
		// is_manual_entry = false: hoạt động Strava nhập tay không được tính.
		// Các trust flag khác (pace_anomaly...) sẽ lọc thêm ở đây khi có.
		if err := tx.QueryRow(ctx, fmt.Sprintf(`
			SELECT %s FROM activities
			WHERE user_id = $1 AND sport = $2 AND source = $3
			  AND NOT is_manual_entry
			  AND vn_date >= $4::date AND vn_date < $5::date`, agg),
			userID, sport, source, p.start, p.end,
		).Scan(&achieved); err != nil {
			return fmt.Errorf("aggregate: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE enrollment_periods
			SET achieved = $1, passed = $2, updated_at = now()
			WHERE enrollment_id = $3 AND period_start = $4`,
			achieved, achieved+1e-9 >= p.target, p.enrollmentID, p.start,
		); err != nil {
			return fmt.Errorf("update period: %w", err)
		}
	}
	return nil
}
