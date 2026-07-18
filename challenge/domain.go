// Package challenge quản lý vòng đời kèo: tạo → vào kèo (khóa cược) →
// theo dõi tiến độ theo kỳ → settlement (chia thưởng qua ledger).
package challenge

import (
	"errors"
	"time"
)

type GoalType string

const (
	GoalDailySteps       GoalType = "daily_steps"
	GoalDailyDistanceKm  GoalType = "daily_distance_km"
	GoalWeeklyDistanceKm GoalType = "weekly_distance_km"
	GoalWeeklySessions   GoalType = "weekly_sessions"
)

type Status string

const (
	StatusOpen     Status = "open"
	StatusActive   Status = "active"
	StatusGrace    Status = "grace"
	StatusSettling Status = "settling"
	StatusSettled  Status = "settled"
)

type EnrollStatus string

const (
	EnrollActive    EnrollStatus = "active"
	EnrollCompleted EnrollStatus = "completed"
	EnrollFailed    EnrollStatus = "failed"
)

type Challenge struct {
	ID              int64
	CreatorID       int64
	Title           string
	Sport           string
	GoalType        GoalType
	GoalValue       float64
	Source          string
	StakePoints     int64
	FeeBps          int64
	PassRatio       float64
	StartAt         time.Time
	EndAt           time.Time
	GraceHours      int
	Status          Status
	MaxParticipants int
	IsCharity       bool
	CharityID       int64
}

type Enrollment struct {
	ID          int64
	ChallengeID int64
	UserID      int64
	Status      EnrollStatus
}

// Period là một kỳ đánh giá [Start, End) với chỉ tiêu Target.
type Period struct {
	Start  time.Time
	End    time.Time
	Target float64
}

var (
	ErrNotJoinable   = errors.New("challenge: not joinable")
	ErrNotFound      = errors.New("challenge: not found")
	ErrChallengeFull = errors.New("challenge: full")
)

// VNLocation là timezone chốt kỳ. Ranh giới ngày/tuần tính theo giờ VN —
// user chạy lúc 23:50 phải được tính vào đúng ngày họ nhìn thấy trên app.
var VNLocation = mustLoadLocation("Asia/Ho_Chi_Minh")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		// Fallback tĩnh: VN không có DST, UTC+7 cố định.
		return time.FixedZone("ICT", 7*3600)
	}
	return loc
}
