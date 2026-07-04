// Package ingest nhận hoạt động thể thao từ các nguồn xác thực (Strava webhook,
// Apple Health / Health Connect sync từ mobile) và cộng vào tiến độ kèo.
//
// Nguyên tắc: mọi cập nhật tiến độ là RECOMPUTE từ bảng activities cho kỳ bị
// ảnh hưởng, không cộng dồn mù — nhờ vậy event update/delete từ Strava và
// sync đè từ Health/Fit đều cho kết quả đúng, chạy lại bao nhiêu lần cũng vậy.
package ingest

import (
	"time"

	"github.com/hao/keo/challenge"
)

const (
	ProviderStrava      = "strava"
	ProviderGoogleFit   = "google_fit"
	ProviderAppleHealth = "apple_health"
)

// Activity là một hoạt động đã chuẩn hóa, sẵn sàng ghi vào bảng activities.
type Activity struct {
	UserID       int64
	Source       string
	ExternalID   string
	Sport        string // walk|run|swim|bike|gym — trùng với challenges.sport
	DistanceM    float64
	DurationS    int
	Steps        int
	Sessions     int
	AvgHeartrate float64
	IsManual     bool
	StartedAt    time.Time
}

// sportFromStrava map activity type của Strava về sport nội bộ.
// Trả về "" nếu không quan tâm (yoga, golf...) — event sẽ được bỏ qua êm.
func sportFromStrava(t string) string {
	switch t {
	case "Run", "TrailRun", "VirtualRun":
		return "run"
	case "Ride", "VirtualRide", "GravelRide", "MountainBikeRide":
		return "bike"
	case "Swim":
		return "swim"
	case "Walk", "Hike":
		return "walk"
	case "WeightTraining", "Workout", "Crossfit":
		return "gym"
	default:
		return ""
	}
}

// vnDate quy một timestamp về ngày theo giờ VN — ranh giới kỳ của toàn hệ thống.
func vnDate(t time.Time) time.Time {
	y, m, d := t.In(challenge.VNLocation).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, challenge.VNLocation)
}
