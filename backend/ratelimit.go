package main

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type rateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	limit    int
	window   time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{attempts: make(map[string][]time.Time), limit: limit, window: window}
}

func (rl *rateLimiter) allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-rl.window)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	kept := rl.attempts[key][:0]
	for _, t := range rl.attempts[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= rl.limit {
		rl.attempts[key] = kept
		return false
	}
	rl.attempts[key] = append(kept, now)
	return true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (rl *rateLimiter) middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !rl.allow(clientIP(r)) {
			writeError(w, http.StatusTooManyRequests, "too many attempts, please wait before trying again")
			return
		}
		next(w, r)
	}
}
