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
