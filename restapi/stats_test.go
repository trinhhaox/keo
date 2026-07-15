package restapi

import (
	"testing"
	"time"
)

func d(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestComputeStreak(t *testing.T) {
	today := d("2026-07-08")
	cases := []struct {
		name string
		days []time.Time
		want int
	}{
		{"chưa có hoạt động nào", nil, 0},
		{"tập hôm nay, chuỗi 1", []time.Time{d("2026-07-08")}, 1},
		{"3 ngày liên tiếp tới hôm nay", []time.Time{d("2026-07-08"), d("2026-07-07"), d("2026-07-06")}, 3},
		{"hôm nay chưa tập — chuỗi từ hôm qua vẫn giữ", []time.Time{d("2026-07-07"), d("2026-07-06")}, 2},
		{"lỡ trọn một ngày — chuỗi đứt", []time.Time{d("2026-07-08"), d("2026-07-06"), d("2026-07-05")}, 1},
		{"chuỗi cũ đã đứt từ lâu", []time.Time{d("2026-07-01"), d("2026-06-30")}, 0},
		{"ngày trùng lặp không đếm đôi", []time.Time{d("2026-07-08"), d("2026-07-08"), d("2026-07-07")}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := computeStreak(tc.days, today); got != tc.want {
				t.Errorf("computeStreak = %d, want %d", got, tc.want)
			}
		})
	}
}
