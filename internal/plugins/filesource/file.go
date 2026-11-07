// Package filesource implements the "file" data-source plugin (FR-011, NFR-SEC-006).
//
// An instance is bound to one operator-configured root (jsonData.root). A query names a
// relative path inside that root; the file is opened through the host's fsroot.Roots,
// capped in size, and decoded by the format plugin chosen explicitly or by extension.
package filesource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/fsroot"
	"github.com/yesoreyeram/dv/internal/plugin"
)

// ID is the plugin ID.
const ID = "file"

// ErrUnsupportedFormat is returned when no format plugin matches the query.
var ErrUnsupportedFormat = errors.New("unsupported format")

// ErrInvalidQuery is returned for malformed query bodies.
var ErrInvalidQuery = errors.New("invalid query")

// ErrInvalidConfig is returned for malformed instance configurations.
var ErrInvalidConfig = errors.New("invalid source configuration")

// Formats looks up format plugins; *plugin.Registry implements it.
type Formats interface {
	Format(id string) (plugin.Format, error)
	Catalog() plugin.Catalog
}

// Settings is the instance configuration in SourceConfig.JSONData.
type Settings struct {
	Root string `json:"root"`
}

// QueryModel is the plugin-specific query in Query.Body.
type QueryModel struct {
	Path       string                     `json:"path"`
	Format     string                     `json:"format,omitempty"`
	Options    json.RawMessage            `json:"options,omitempty"`
	FieldTypes map[string]frame.FieldType `json:"fieldTypes,omitempty"`
}

// Default result limits applied when a query leaves a limit at zero ("unlimited"), so a
// caller that forgets to set limits cannot make a decode hold an unbounded amount of
// memory (FR-011 AC8, NFR-PERF-003).
const (
	DefaultMaxRows  = 1_000_000
	DefaultMaxBytes = 64 << 20
)

// Source is the file data-source plugin.
type Source struct {
	roots    *fsroot.Roots
	formats  Formats
	maxBytes int64
	defaults frame.Limits
}

// New returns a file source reading from roots, decoding with formats and refusing files
// larger than maxBytes. defaults are the result limits used for any limit a query leaves
// at zero; zero members of defaults fall back to DefaultMaxRows / DefaultMaxBytes.
func New(roots *fsroot.Roots, formats Formats, maxBytes int64, defaults frame.Limits) (*Source, error) {
	if roots == nil || formats == nil {
		return nil, errors.New("file source needs roots and formats")
	}
	if maxBytes <= 0 {
		return nil, errors.New("file source needs a positive size limit")
	}
	if defaults.MaxRows < 0 || defaults.MaxBytes < 0 {
		return nil, errors.New("file source default limits must not be negative")
	}
	if defaults.MaxRows == 0 {
		defaults.MaxRows = DefaultMaxRows
	}
	if defaults.MaxBytes == 0 {
		defaults.MaxBytes = DefaultMaxBytes
	}
	return &Source{roots: roots, formats: formats, maxBytes: maxBytes, defaults: defaults}, nil
}

// limits fills the zero members of q with the source defaults.
func (s *Source) limits(q frame.Limits) frame.Limits {
	if q.MaxRows == 0 {
		q.MaxRows = s.defaults.MaxRows
	}
	if q.MaxBytes == 0 {
		q.MaxBytes = s.defaults.MaxBytes
	}
	return q
}

// Meta implements plugin.Source.
func (s *Source) Meta() plugin.SourceMeta {
	return plugin.SourceMeta{
		ID:           ID,
		Name:         "Local files",
		Description:  "Files in directories the operator exposes to dv, decoded by a format plugin.",
		Capabilities: []string{"query", "formats"},
	}
}

func strictDecode(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("unexpected data after JSON object")
	}
	return nil
}

func (s *Source) settings(cfg plugin.SourceConfig) (Settings, error) {
	var st Settings
	if len(bytes.TrimSpace(cfg.JSONData)) == 0 {
		return st, fmt.Errorf("jsonData.root is required: %w", ErrInvalidConfig)
	}
	if err := strictDecode(cfg.JSONData, &st); err != nil {
		return st, fmt.Errorf("%w: jsonData: %w", ErrInvalidConfig, err)
	}
	if st.Root == "" {
		return st, fmt.Errorf("jsonData.root is required: %w", ErrInvalidConfig)
	}
	if !s.roots.Has(st.Root) {
		return st, fmt.Errorf("file root %q is not configured on this server: %w", st.Root, ErrInvalidConfig)
	}
	return st, nil
}

// Validate implements plugin.Source.
func (s *Source) Validate(_ context.Context, cfg plugin.SourceConfig) error {
	st, err := s.settings(cfg)
	if err != nil {
		return err
	}
	return s.roots.Check(st.Root)
}

// Health implements plugin.Source.
func (s *Source) Health(ctx context.Context, cfg plugin.SourceConfig) error {
	return s.Validate(ctx, cfg)
}

// Query implements plugin.Source.
func (s *Source) Query(ctx context.Context, q plugin.Query) (*frame.Result, error) {
	st, err := s.settings(q.Source)
	if err != nil {
		return nil, err
	}
	var m QueryModel
	if err := strictDecode(q.Body, &m); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidQuery, err)
	}
	clean, err := fsroot.CleanPath(m.Path)
	if err != nil {
		return nil, err
	}
	format, err := s.pickFormat(m.Format, clean)
	if err != nil {
		return nil, err
	}
	f, err := s.roots.Open(st.Root, clean, s.maxBytes)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	frames, err := format.Decode(ctx, io.Reader(f), plugin.DecodeOptions{
		Limits:     s.limits(q.Limits),
		FieldTypes: m.FieldTypes,
		Options:    m.Options,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", clean, err)
	}
	for _, fr := range frames {
		if fr.Name == "" {
			fr.Name = path.Base(clean)
		}
		fr.Meta.Source = ID
		fr.Meta.ExecutedQuery = st.Root + ":" + clean
	}
	return &frame.Result{Frames: frames}, nil
}

// pickFormat returns the explicitly requested format, or the one registered for the
// file's extension (case-insensitive).
func (s *Source) pickFormat(id, clean string) (plugin.Format, error) {
	if id != "" {
		f, err := s.formats.Format(id)
		if err != nil {
			return nil, fmt.Errorf("format %q: %w", id, ErrUnsupportedFormat)
		}
		return f, nil
	}
	ext := strings.ToLower(path.Ext(clean))
	if ext != "" {
		for _, fm := range s.formats.Catalog().Formats {
			for _, e := range fm.Extensions {
				if strings.ToLower(e) == ext {
					return s.formats.Format(fm.ID)
				}
			}
		}
	}
	return nil, fmt.Errorf("no format for %q; set \"format\" explicitly: %w", path.Base(clean), ErrUnsupportedFormat)
}
