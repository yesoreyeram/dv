package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"
)

type ctxKey int

const (
	requestIDKey ctxKey = iota
	traceIDKey
)

// RequestIDHeader is the header used to propagate request IDs.
const RequestIDHeader = "X-Request-ID"

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// ValidRequestID reports whether s is acceptable as a client-supplied request ID.
func ValidRequestID(s string) bool { return validRequestID.MatchString(s) }

// NewRequestID returns a random 128-bit hex identifier.
func NewRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never returns an error on supported platforms.
	return hex.EncodeToString(b[:])
}

// RequestIDFrom returns the request ID stored in ctx, or "".
func RequestIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

// TraceIDFrom returns the W3C trace ID stored in ctx, or "".
func TraceIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(traceIDKey).(string)
	return v
}

// RequestID accepts a well-formed incoming X-Request-ID or generates one, stores it in
// the context and echoes it on the response (NFR-OBS-002). Malformed IDs are replaced
// to prevent log injection.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !ValidRequestID(id) {
			id = NewRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

// ParseTraceparent extracts the trace ID from a W3C traceparent header
// ("00-<32 hex trace id>-<16 hex parent id>-<2 hex flags>"). It returns "" if invalid.
func ParseTraceparent(v string) string {
	parts := strings.Split(strings.TrimSpace(v), "-")
	if len(parts) < 4 || len(parts[0]) != 2 || parts[0] == "ff" ||
		len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
		return ""
	}
	if parts[0] == "00" && len(parts) != 4 {
		return ""
	}
	for _, p := range parts[:4] {
		if !isLowerHex(p) {
			return ""
		}
	}
	if allZero(parts[1]) || allZero(parts[2]) {
		return ""
	}
	return parts[1]
}

func isLowerHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func allZero(s string) bool { return strings.Trim(s, "0") == "" }

// TraceContext stores the incoming W3C trace ID in the context so logs correlate with
// upstream traces (NFR-OBS-003). Spans are emitted once the OTel SDK lands (ADR-0005).
func TraceContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := ParseTraceparent(r.Header.Get("traceparent")); id != "" {
			r = r.WithContext(context.WithValue(r.Context(), traceIDKey, id))
		}
		next.ServeHTTP(w, r)
	})
}
