package plugin

import "errors"

// Stable error kinds shared by format plugins, so callers such as the query API map
// errors to statuses without importing concrete plugins (B-067, FR-016 AC4).
var (
	// ErrDecode is wrapped by every format error caused by the input data.
	ErrDecode = errors.New("decode failed")
	// ErrOptions is wrapped by every format error caused by invalid format options.
	ErrOptions = errors.New("invalid format options")
)

// Stable error kinds for connector (network) sources (FR-040 AC5). Every message a
// source wraps around them must be safe to return to the client: no secrets, no upstream
// bodies, no resolved addresses and no URLs with query strings.
var (
	// ErrInvalidQuery is wrapped by errors caused by the query body (400).
	ErrInvalidQuery = errors.New("invalid query")
	// ErrInvalidConfig is wrapped by errors in an instance's settings (400 at query
	// time; start-up failure in Validate).
	ErrInvalidConfig = errors.New("invalid source configuration")
	// ErrDestinationNotAllowed is wrapped when the SSRF guard refuses a destination (403).
	ErrDestinationNotAllowed = errors.New("destination not allowed")
	// ErrUpstreamAuth is wrapped when the upstream rejects the credentials (502).
	ErrUpstreamAuth = errors.New("upstream rejected the credentials")
	// ErrUpstreamNotFound is wrapped when the upstream answers 404 (404).
	ErrUpstreamNotFound = errors.New("upstream resource not found")
	// ErrUpstreamRateLimited is wrapped when the upstream answers 429 (429).
	ErrUpstreamRateLimited = errors.New("upstream rate limit")
	// ErrUpstream is wrapped by other upstream failures: unexpected status, a next-page
	// URL outside the base URL, an invalid body (502).
	ErrUpstream = errors.New("upstream error")
	// ErrUpstreamUnreachable is wrapped by connection, TLS and protocol failures (502).
	ErrUpstreamUnreachable = errors.New("upstream unreachable")
	// ErrResponseTooLarge is wrapped when an upstream body exceeds its size cap (413).
	ErrResponseTooLarge = errors.New("upstream response too large")
	// ErrUnsupportedMediaType is wrapped when the upstream answers a media type the
	// source cannot decode (415).
	ErrUnsupportedMediaType = errors.New("unsupported upstream media type")
)
