package costqueryrealtime

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// simpleIPLimiter is an in-memory fixed-window rate limiter per client IP.
type simpleIPLimiter struct {
	mu       sync.Mutex
	window   time.Duration
	limit    int
	hits     map[string]ipWindow
}

type ipWindow struct {
	start time.Time
	count int
}

func newIPLimiter(limit int, window time.Duration) *simpleIPLimiter {
	return &simpleIPLimiter{
		window: window,
		limit:  limit,
		hits:   make(map[string]ipWindow),
	}
}

func (l *simpleIPLimiter) allow(ip string) bool {
	if ip == "" {
		ip = "unknown"
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.hits[ip]
	if !ok || now.Sub(w.start) >= l.window {
		l.hits[ip] = ipWindow{start: now, count: 1}
		return true
	}
	if w.count >= l.limit {
		return false
	}
	w.count++
	l.hits[ip] = w
	return true
}

func (l *simpleIPLimiter) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !l.allow(ip) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code":    http.StatusTooManyRequests,
				"message": "too many requests",
			})
			return
		}
		c.Next()
	}
}

func shareNoReferrer() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Cache-Control", "no-store")
		c.Next()
	}
}
