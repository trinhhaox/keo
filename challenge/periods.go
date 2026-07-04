package challenge

import (
	"fmt"
	"math"
	"time"
)

// GeneratePeriods sinh toàn bộ kỳ đánh giá của một enrollment tại thời điểm
// vào kèo. Pure function — test không cần DB.
//
// Quy tắc:
//   - daily_*:  mỗi ngày (theo loc) một kỳ, target = goalValue.
//   - weekly_*: mỗi 7 ngày một kỳ tính từ ngày bắt đầu; tuần cuối lẻ ngày
//     thì target prorate theo số ngày (ví dụ kèo 30 ngày goal 20km/tuần
//     → 4 tuần đủ + kỳ 2 ngày target 20×2/7 ≈ 5.71km).
//
// Ranh giới kỳ là NGÀY theo loc (nửa đêm), không phải giờ start_at — user
// nghĩ theo "ngày" chứ không theo timestamp.
func GeneratePeriods(gt GoalType, goalValue float64, startAt, endAt time.Time, loc *time.Location) ([]Period, error) {
	if !endAt.After(startAt) {
		return nil, fmt.Errorf("end_at must be after start_at")
	}
	if goalValue <= 0 {
		return nil, fmt.Errorf("goal_value must be > 0")
	}

	// Kỳ phủ các ngày lịch trong khoảng NỬA MỞ [dateOf(start), dateOf(end)):
	// kèo start 01/07 06:00, end 31/07 06:00 → 30 kỳ ngày 01..30/07.
	// Phần lẻ sáng 31/07 không tính — app nên tạo kèo theo ranh giới nửa đêm,
	// còn nếu không thì bỏ phần lẻ cuối là lựa chọn đơn giản và nhất quán.
	start := dateOf(startAt, loc)
	end := dateOf(endAt, loc)
	totalDays := int(end.Sub(start).Hours() / 24)
	if totalDays == 0 {
		totalDays = 1 // kèo bắt đầu và kết thúc trong cùng một ngày
	}

	var periods []Period
	switch gt {
	case GoalDailySteps:
		for d := 0; d < totalDays; d++ {
			s := start.AddDate(0, 0, d)
			periods = append(periods, Period{Start: s, End: s.AddDate(0, 0, 1), Target: goalValue})
		}
	case GoalWeeklyDistanceKm, GoalWeeklySessions:
		for d := 0; d < totalDays; d += 7 {
			days := 7
			if d+7 > totalDays {
				days = totalDays - d
			}
			target := goalValue
			if days < 7 {
				target = math.Round(goalValue*float64(days)/7*100) / 100 // prorate, 2 chữ số
			}
			s := start.AddDate(0, 0, d)
			periods = append(periods, Period{Start: s, End: s.AddDate(0, 0, days), Target: target})
		}
	default:
		return nil, fmt.Errorf("unknown goal type %q", gt)
	}
	return periods, nil
}

// Passed quyết định đậu/rớt: passed/total >= passRatio.
// Epsilon xử lý sai số float khi passRatio đọc từ NUMERIC (0.8 → 4/5 phải đậu).
func Passed(passedPeriods, totalPeriods int, passRatio float64) bool {
	if totalPeriods == 0 {
		return false // không có kỳ nào = dữ liệu hỏng, không cho đậu mặc định
	}
	return float64(passedPeriods)/float64(totalPeriods)+1e-9 >= passRatio
}

func dateOf(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}
