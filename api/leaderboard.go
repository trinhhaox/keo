// Bảng xếp hạng một kèo: mọi người chơi + tiến độ theo kỳ. Read-only từ
// enrollments/enrollment_periods — không đụng ledger.
package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Server) leaderboard(w http.ResponseWriter, r *http.Request, userID int64) {
	challengeID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad challenge id")
		return
	}
	ctx := r.Context()

	type challengeInfo struct {
		ID          int64     `json:"id"`
		Title       string    `json:"title"`
		Sport       string    `json:"sport"`
		Source      string    `json:"source"`
		StakePoints int64     `json:"stake_points"`
		EndAt       time.Time `json:"end_at"`
		Status      string    `json:"status"`
	}
	var c challengeInfo
	err = s.pool.QueryRow(ctx, `
		SELECT id, title, sport, source, stake_points, end_at, status
		FROM challenges WHERE id = $1`, challengeID,
	).Scan(&c.ID, &c.Title, &c.Sport, &c.Source, &c.StakePoints, &c.EndAt, &c.Status)
	if err == pgx.ErrNoRows {
		httpError(w, http.StatusNotFound, "kèo không tồn tại")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, "query challenge")
		return
	}

	rows, err := s.pool.Query(ctx, `
		SELECT e.user_id, u.display_name, e.status,
		       COUNT(p.*)                       AS periods_total,
		       COUNT(*) FILTER (WHERE p.passed) AS periods_passed
		FROM enrollments e
		JOIN users u ON u.id = e.user_id
		LEFT JOIN enrollment_periods p ON p.enrollment_id = e.id
		WHERE e.challenge_id = $1
		GROUP BY e.user_id, u.display_name, e.status
		ORDER BY periods_passed DESC, u.display_name ASC`,
		challengeID,
	)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "query leaderboard")
		return
	}
	defer rows.Close()

	type entry struct {
		UserID        int64  `json:"user_id"`
		DisplayName   string `json:"display_name"`
		Status        string `json:"status"`
		PeriodsTotal  int    `json:"periods_total"`
		PeriodsPassed int    `json:"periods_passed"`
		IsMe          bool   `json:"is_me"`
	}
	entries := []entry{}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.UserID, &e.DisplayName, &e.Status,
			&e.PeriodsTotal, &e.PeriodsPassed); err != nil {
			httpError(w, http.StatusInternalServerError, "scan")
			return
		}
		e.IsMe = e.UserID == userID
		entries = append(entries, e)
	}
	writeJSON(w, map[string]any{
		"challenge": c,
		"pot":       c.StakePoints * int64(len(entries)),
		"entries":   entries,
	})
}
