// Package httpx contains the hardened HTTP middleware chain shared by every route:
// request IDs, trace context, access logging, panic recovery, security headers,
// body limits and per-client rate limiting.
package httpx

import "net/http"

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middleware so that the first argument is the outermost wrapper.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
