package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// rateLimiter: token-bucket per key (IP), chống brute-force endpoint nhạy cảm.
//
// Phạm vi: CHỈ hiệu quả cho binary chạy dài (VPS). Trên serverless mỗi invocation
// là process riêng nên state không chia sẻ — ở đó rate-limit phải đặt tại edge
// (Cloudflare). Đây là lớp phòng thủ bổ sung cho đường binary, không thay edge.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // token nạp mỗi giây
	burst   float64 // trần token (số request dồn cho phép)
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(ratePerSec, burst float64) *rateLimiter {
	return &rateLimiter{buckets: make(map[string]*bucket), rate: ratePerSec, burst: burst}
}

// allow trừ 1 token cho key tại thời điểm now; false = vượt hạn mức.
func (l *rateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// cleanup xóa bucket không hoạt động quá idle để map không phình vô hạn.
func (l *rateLimiter) cleanup(now time.Time, idle time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, b := range l.buckets {
		if now.Sub(b.last) > idle {
			delete(l.buckets, k)
		}
	}
}

// middleware rate-limit các request có path bắt đầu bằng một trong prefixes.
// Request khác đi thẳng.
func (l *rateLimiter) middleware(next http.Handler, prefixes ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limited := false
		for _, p := range prefixes {
			if strings.HasPrefix(r.URL.Path, p) {
				limited = true
				break
			}
		}
		if limited && !l.allow(clientIP(r), time.Now()) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "quá nhiều yêu cầu, thử lại sau", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP lấy IP thật của client sau Cloudflare tunnel/proxy: ưu tiên
// CF-Connecting-IP, rồi X-Forwarded-For (hop đầu), cuối cùng RemoteAddr.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
