package mcp

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/kees/atlas/internal/auth"
)

const (
	requestsPerMinute = 120
	writesPerHour     = 500
	// maxResponse keeps one tool result small enough to be useful in a model's context.
	maxResponse = 16 * 1024
)

// limiter counts events per key in a fixed window. State is in memory: a restart resets it.
// shortcut: fixed windows and process-local counters, switch to a table if Atlas runs more than one process.
type limiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	hits   map[string]*bucket
}

type bucket struct {
	start time.Time
	n     int
}

func newLimiter(limit int, window time.Duration) *limiter {
	return &limiter{limit: limit, window: window, now: time.Now, hits: map[string]*bucket{}}
}

// take records n events for key. When the window is full it records nothing and returns the wait.
func (l *limiter) take(key string, n int) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b := l.hits[key]
	if b == nil || now.Sub(b.start) >= l.window {
		for k, o := range l.hits { // drop expired windows so the map cannot grow without bound
			if now.Sub(o.start) >= l.window {
				delete(l.hits, k)
			}
		}
		b = &bucket{start: now}
		l.hits[key] = b
	}
	if b.n+n > l.limit {
		return b.start.Add(l.window).Sub(now), false
	}
	b.n += n
	return 0, true
}

// rateLimit answers 429 with Retry-After when a token sends too many requests.
func rateLimit(l *limiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, _ := auth.ActorFrom(r.Context())
		if wait, ok := l.take(a.String(), 1); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			http.Error(w, `{"error":{"code":"rate_limited","message":"too many requests"}}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
