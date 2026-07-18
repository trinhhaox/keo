package ingest

import (
	"context"
	"fmt"
	"testing"
)

// TestIsTransient khóa phân loại lỗi tạm thời (retry) vs vĩnh viễn (failed).
// Pure function — không cần DB, chạy trong mọi môi trường.
func TestIsTransient(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context deadline", context.DeadlineExceeded, true},
		{"context canceled", context.Canceled, true},
		{"strava 429 rate limit", fmt.Errorf("fetch activity 1: strava 429: too many requests"), true},
		{"strava 500", fmt.Errorf("strava 503: service unavailable"), true},
		{"oauth 5xx", fmt.Errorf("refresh token: oauth 500: internal"), true},
		{"timeout string", fmt.Errorf("fetch: Client.Timeout exceeded"), true},
		{"connection refused", fmt.Errorf("dial tcp: connection refused"), true},
		{"strava 404 permanent", fmt.Errorf("fetch activity 1: strava 404: not found"), false},
		{"strava 401 permanent", fmt.Errorf("strava 401: unauthorized"), false},
		{"parse error permanent", fmt.Errorf("parse: unexpected end of JSON"), false},
		{"unknown goal type permanent", fmt.Errorf("unknown goal type \"x\""), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isTransient(c.err); got != c.want {
				t.Fatalf("isTransient(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

// net.Error timeout được coi là transient.
func TestIsTransientNetError(t *testing.T) {
	if !isTransient(timeoutNetErr{}) {
		t.Fatal("net.Error phải là transient")
	}
}

type timeoutNetErr struct{}

func (timeoutNetErr) Error() string   { return "i/o timeout" }
func (timeoutNetErr) Timeout() bool   { return true }
func (timeoutNetErr) Temporary() bool { return true }
