// Thống kê cá nhân: hoạt động gần đây + chuỗi ngày tập (streak) + tổng tuần.
// Tất cả đọc từ bảng activities — nguồn sự thật duy nhất về vận động, nên số
// liệu ở đây tự khớp với tiến độ kèo (cùng nguồn recompute).
package api

import (
	"net/http"
	"time"

	"github.com/hao/keo/challenge"
)

func (s *Server) myActivities(w http.ResponseWriter, r *http.Request, userID int64) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT sport, source, distance_m, duration_s, steps, sessions, started_at, vn_date
		FROM activities
		WHERE user_id = $1 AND NOT is_manual_entry
		ORDER BY started_at DESC LIMIT 30`,
		userID,
	)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "query")
		return
	}
	defer rows.Close()

	type act struct {
		Sport     string    `json:"sport"`
		Source    string    `json:"source"`
		DistanceM float64   `json:"distance_m"`
		DurationS int       `json:"duration_s"`
		Steps     int       `json:"steps"`
		Sessions  int       `json:"sessions"`
		StartedAt time.Time `json:"started_at"`
		VNDate    time.Time `json:"vn_date"`
	}
	out := []act{}
	for rows.Next() {
		var a act
		if err := rows.Scan(&a.Sport, &a.Source, &a.DistanceM, &a.DurationS,
			&a.Steps, &a.Sessions, &a.StartedAt, &a.VNDate); err != nil {
			httpError(w, http.StatusInternalServerError, "scan")
			return
		}
		out = append(out, a)
	}
	writeJSON(w, out)
}

func (s *Server) myStats(w http.ResponseWriter, r *http.Request, userID int64) {
	ctx := r.Context()

	// Ngày có hoạt động, mới nhất trước — đủ cho chuỗi tối đa một năm.
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT vn_date FROM activities
		WHERE user_id = $1 AND NOT is_manual_entry
		ORDER BY vn_date DESC LIMIT 366`,
		userID,
	)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "query days")
		return
	}
	defer rows.Close()
	var days []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			httpError(w, http.StatusInternalServerError, "scan")
			return
		}
		days = append(days, d)
	}

	// Tổng tuần này (tuần ISO, thứ Hai) theo giờ Việt Nam.
	var weekDistanceM float64
	var weekSessions, weekActiveDays int
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(distance_m), 0), COALESCE(SUM(sessions), 0),
		       COUNT(DISTINCT vn_date)
		FROM activities
		WHERE user_id = $1 AND NOT is_manual_entry
		  AND vn_date >= date_trunc('week', (now() AT TIME ZONE 'Asia/Ho_Chi_Minh'))::date`,
		userID,
	).Scan(&weekDistanceM, &weekSessions, &weekActiveDays); err != nil {
		httpError(w, http.StatusInternalServerError, "query week")
		return
	}

	today := time.Now().In(challenge.VNLocation)
	writeJSON(w, map[string]any{
		"streak_days":      computeStreak(days, today),
		"week_distance_m":  weekDistanceM,
		"week_sessions":    weekSessions,
		"week_active_days": weekActiveDays,
	})
}

// computeStreak đếm số ngày liên tiếp có hoạt động, tính lùi từ hôm nay.
// Hôm nay chưa tập thì chuỗi chưa đứt — tính từ hôm qua; chuỗi chỉ đứt khi
// lỡ trọn một ngày.
func computeStreak(days []time.Time, today time.Time) int {
	const day = "2006-01-02"
	seen := make(map[string]bool, len(days))
	for _, d := range days {
		seen[d.Format(day)] = true
	}
	cur := today
	if !seen[cur.Format(day)] {
		cur = cur.AddDate(0, 0, -1)
	}
	streak := 0
	for seen[cur.Format(day)] {
		streak++
		cur = cur.AddDate(0, 0, -1)
	}
	return streak
}
