package http

import (
	"net/http"
	"sync"
	"time"

	"payment-demo/internal/shared/httputil"
)

// rateLimiter 简单的 per-key 令牌桶限流器（PCI Req 8: 防暴力枚举）
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    int           // 每个窗口允许的请求数
	window  time.Duration // 窗口时长
}

type bucket struct {
	tokens    int
	lastReset time.Time
}

func newRateLimiter(rate int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		buckets: make(map[string]*bucket),
		rate:    rate,
		window:  window,
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[key]
	if !ok || time.Since(b.lastReset) > rl.window {
		rl.buckets[key] = &bucket{tokens: rl.rate - 1, lastReset: time.Now()}
		return true
	}
	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}

// rateLimitMiddleware 包装 handler，按 userID 限流
func rateLimitMiddleware(rl *rateLimiter, keyFunc func(*http.Request) string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := keyFunc(r)
		if !rl.allow(key) {
			httputil.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}
