package httpx

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recoverer converts panics into a generic JSON 500 and logs the stack server-side
// (NFR-SEC-012). http.ErrAbortHandler is re-panicked as net/http expects.
func Recoverer(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer recoverPanic(log, w, r)
			next.ServeHTTP(w, r)
		})
	}
}

func recoverPanic(log *slog.Logger, w http.ResponseWriter, r *http.Request) {
	p := recover()
	if p == nil {
		return
	}
	if err, ok := p.(error); ok && errors.Is(err, http.ErrAbortHandler) {
		panic(p)
	}
	log.ErrorContext(r.Context(), "panic serving request", "panic", p, "stack", string(debug.Stack()))
	WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
}
