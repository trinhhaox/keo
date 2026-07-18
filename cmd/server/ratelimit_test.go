package main

import (
	"net/http"
	"testing"
	"time"
)

func TestRateLimiterAllow(t *testing.T) {
	l := newRateLimiter(1, 3) // 1 token/s, burst 3
	t0 := time.Unix(1_700_000_000, 0)

	// 3 request đầu (burst) được phép, request thứ 4 bị chặn.
	for i := 0; i < 3; i++ {
		if !l.allow("1.2.3.4", t0) {
			t.Fatalf("request %d trong burst phải được phép", i+1)
		}
	}
	if l.allow("1.2.3.4", t0) {
		t.Fatal("request thứ 4 phải bị chặn")
	}

	// Sau 1 giây nạp lại 1 token → 1 request nữa được phép.
	if !l.allow("1.2.3.4", t0.Add(time.Second)) {
		t.Fatal("sau 1s phải được phép 1 request")
	}
	if l.allow("1.2.3.4", t0.Add(time.Second)) {
		t.Fatal("token đã hết, phải bị chặn")
	}

	// Key khác (IP khác) có bucket riêng.
	if !l.allow("9.9.9.9", t0) {
		t.Fatal("IP khác phải có hạn mức riêng")
	}
}

func TestRateLimiterCleanup(t *testing.T) {
	l := newRateLimiter(1, 3)
	t0 := time.Unix(1_700_000_000, 0)
	l.allow("1.2.3.4", t0)
	l.allow("9.9.9.9", t0)
	l.cleanup(t0.Add(10*time.Minute), 5*time.Minute)
	if len(l.buckets) != 0 {
		t.Fatalf("bucket cũ phải bị dọn, còn %d", len(l.buckets))
	}
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		remote  string
		want    string
	}{
		{"cf-connecting-ip", map[string]string{"CF-Connecting-IP": "5.5.5.5"}, "10.0.0.1:1234", "5.5.5.5"},
		{"xff first hop", map[string]string{"X-Forwarded-For": "6.6.6.6, 10.0.0.1"}, "10.0.0.1:1234", "6.6.6.6"},
		{"remoteaddr fallback", nil, "7.7.7.7:9999", "7.7.7.7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, _ := http.NewRequest("GET", "/", nil)
			r.RemoteAddr = c.remote
			for k, v := range c.headers {
				r.Header.Set(k, v)
			}
			if got := clientIP(r); got != c.want {
				t.Fatalf("clientIP = %q, want %q", got, c.want)
			}
		})
	}
}
