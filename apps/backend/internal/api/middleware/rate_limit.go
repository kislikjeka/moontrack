package middleware

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter implements a simple IP-based rate limiter
type RateLimiter struct {
	visitors map[string]*rate.Limiter
	mu       sync.RWMutex
	r        rate.Limit
	b        int
}

// NewRateLimiter creates a new rate limiter
// r: requests per second
// b: burst size
func NewRateLimiter(r rate.Limit, b int) *RateLimiter {
	return &RateLimiter{
		visitors: make(map[string]*rate.Limiter),
		r:        r,
		b:        b,
	}
}

// getVisitor retrieves or creates a limiter for an IP address
func (rl *RateLimiter) getVisitor(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limiter, exists := rl.visitors[ip]
	if !exists {
		limiter = rate.NewLimiter(rl.r, rl.b)
		rl.visitors[ip] = limiter
	}

	return limiter
}

// cleanupVisitors removes old visitors periodically
func (rl *RateLimiter) cleanupVisitors() {
	ticker := time.NewTicker(time.Minute)
	go func() {
		for range ticker.C {
			rl.mu.Lock()
			// Clear all visitors to prevent memory leak
			// In production, you'd want to track last access time
			rl.visitors = make(map[string]*rate.Limiter)
			rl.mu.Unlock()
		}
	}()
}

// Middleware returns the rate limiting middleware
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	// Start cleanup goroutine
	go rl.cleanupVisitors()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get IP address (handle X-Forwarded-For for proxies)
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		}

		limiter := rl.getVisitor(ip)
		if !limiter.Allow() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limit exceeded, please try again later"}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RateLimit returns a rate limiting middleware
// Default: 100 requests per second with burst of 20
func RateLimit() func(http.Handler) http.Handler {
	limiter := NewRateLimiter(100, 20)
	return limiter.Middleware
}
