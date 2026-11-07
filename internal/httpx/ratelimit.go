package httpx

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimiter is a per-client token-bucket limiter (NFR-SEC-008). Clients are keyed by
// TCP peer IP; forwarded headers are deliberately ignored until a trusted-proxy list
// exists (threat model F-01, backlog B-021).
type RateLimiter struct {
	rate       float64 // tokens per second
	burst      float64
	maxClients int
	idleTTL    time.Duration
	now        func() time.Time

	mu        sync.Mutex
	clients   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter creates a limiter allowing rps sustained requests per second with the
// given burst. The client table is bounded by maxClients.
func NewRateLimiter(rps float64, burst, maxClients int, now func() time.Time) *RateLimiter {
	if now == nil {
		now = time.Now
	}
	return &RateLimiter{
		rate: rps, burst: float64(burst), maxClients: maxClients, idleTTL: 3 * time.Minute,
		now: now, clients: make(map[string]*bucket), lastSweep: now(),
	}
}

// Allow consumes a token for key. When denied it returns the wait until the next token.
func (l *RateLimiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.lastSweep) > l.idleTTL {
		l.sweep(now)
	}
	b, ok := l.clients[key]
	if !ok {
		if len(l.clients) >= l.maxClients {
			l.sweep(now)
			if len(l.clients) >= l.maxClients {
				// Table full of active clients: fail closed for newcomers.
				return false, time.Second
			}
		}
		b = &bucket{tokens: l.burst, last: now}
		l.clients[key] = b
	}
	b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
	return false, wait
}

func (l *RateLimiter) sweep(now time.Time) {
	for k, b := range l.clients {
		if now.Sub(b.last) > l.idleTTL {
			delete(l.clients, k)
		}
	}
	l.lastSweep = now
}

// Len returns the number of tracked clients.
func (l *RateLimiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.clients)
}

// ClientIP returns the peer IP of the request.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Middleware returns the HTTP middleware enforcing the limiter.
func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, wait := l.Allow(ClientIP(r))
		if !ok {
			secs := int(math.Ceil(wait.Seconds()))
			if secs < 1 {
				secs = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			WriteError(w, r, http.StatusTooManyRequests, "rate_limited", "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}
