// Package plugin defines dv's compiled-in data-source and format plugin model (ADR-0002,
// FR-015) and the registry that holds them.
//
// A Source fetches data and returns frames; a Format decodes a byte stream into frames.
// Byte-oriented sources (file, object storage, HTTP) delegate decoding to a Format.
// Plugins never open network connections or files on their own: the host passes them a
// policy-enforcing dialer/HTTP client and an os.Root (added with the first plugin that
// needs them), keeping security controls in one place.
package plugin

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/yesoreyeram/dv/internal/frame"
)

// SourceMeta is the public description of a data-source plugin type. It is returned to
// clients verbatim, so it must contain only static, non-sensitive information.
type SourceMeta struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Capabilities []string `json:"capabilities"`
}

// FormatMeta is the public description of a format plugin.
type FormatMeta struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	MediaTypes  []string `json:"mediaTypes"`
	Extensions  []string `json:"extensions"`
}

// SourceConfig is the configuration of one data-source instance.
type SourceConfig struct {
	UID  string `json:"uid"`
	Type string `json:"type"`
	Name string `json:"name"`
	// JSONData holds non-secret settings; it may be returned to clients.
	JSONData json.RawMessage `json:"jsonData,omitempty"`
	// SecureJSONData holds secrets. It is write-only: encoding exposes only which keys
	// are set, and logging prints [REDACTED] (FR-015 AC5, NFR-SEC-002).
	SecureJSONData SecureValues `json:"secureJsonFields,omitempty"`
}

// TimeRange is an absolute, UTC time range for a query.
type TimeRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// Query is one query against a source instance.
type Query struct {
	RefID     string          `json:"refId"`
	Source    SourceConfig    `json:"-"`
	Body      json.RawMessage `json:"body,omitempty"` // plugin-specific query model
	TimeRange TimeRange       `json:"timeRange"`
	Limits    frame.Limits    `json:"-"` // host-enforced row/byte limits (NFR-PERF-003)
}

// Source fetches data. Every method must honour ctx cancellation and deadlines.
type Source interface {
	Meta() SourceMeta
	// Validate checks an instance configuration statically and, where possible, for
	// connectivity.
	Validate(ctx context.Context, cfg SourceConfig) error
	// Query executes q and returns frames built with q.Limits.
	Query(ctx context.Context, q Query) (*frame.Result, error)
	// Health reports whether the configured instance is reachable.
	Health(ctx context.Context, cfg SourceConfig) error
}

// DecodeOptions control how a Format decodes input.
type DecodeOptions struct {
	Limits frame.Limits
	// FieldTypes overrides inferred types by field name.
	FieldTypes map[string]frame.FieldType
	// Options holds format-specific settings (e.g. CSV delimiter).
	Options json.RawMessage
}

// Format decodes a byte stream into frames.
type Format interface {
	Meta() FormatMeta
	Decode(ctx context.Context, r io.Reader, opts DecodeOptions) ([]*frame.Frame, error)
}
