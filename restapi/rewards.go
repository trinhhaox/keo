package restapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/hao/keo/reward"
)

// ===== Điểm thưởng: check-in hàng ngày + thưởng km Strava =====

// postCheckin: POST /v1/checkins — check-in hôm nay (giờ VN), +1 điểm vào ví.
// Bấm lần 2 trong ngày trả 409.
func (s *Server) postCheckin(w http.ResponseWriter, r *http.Request, userID int64) {
	acc, err := s.rewards.CheckIn(r.Context(), userID, time.Now())
	if err != nil {
		if errors.Is(err, reward.ErrAlreadyCheckedIn) {
			httpError(w, http.StatusConflict, "hôm nay bạn đã check-in rồi — quay lại vào ngày mai nhé")
			return
		}
		httpError(w, http.StatusInternalServerError, "check-in failed")
		return
	}
	writeJSON(w, map[string]any{"points_granted": acc.Points, "capped": acc.Capped})
}

// getRewards: GET /v1/rewards — trạng thái thưởng (đã check-in hôm nay chưa,
// tổng điểm thưởng từng nhận).
func (s *Server) getRewards(w http.ResponseWriter, r *http.Request, userID int64) {
	sum, err := s.rewards.GetSummary(r.Context(), userID, time.Now())
	if err != nil {
		httpError(w, http.StatusInternalServerError, "read rewards")
		return
	}
	writeJSON(w, sum)
}
