package frame

import (
	"errors"
	"fmt"
)

// Severity of a frame notice.
type Severity string

// Notice severities.
const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
)

// Notice is a human-readable message attached to a frame (e.g. truncation, coercion).
type Notice struct {
	Severity Severity `json:"severity"`
	Text     string   `json:"text"`
}

// Meta describes where a frame came from and how it was produced.
type Meta struct {
	// Source is the plugin ID that produced the frame.
	Source string `json:"source,omitempty"`
	// ExecutedQuery is the query as executed, for display (never contains secrets).
	ExecutedQuery string   `json:"executedQuery,omitempty"`
	Truncated     bool     `json:"truncated,omitempty"`
	Notices       []Notice `json:"notices,omitempty"`
}

// Frame is a named, columnar table. All fields have the same length (FR-010 AC2).
type Frame struct {
	Name   string
	Fields []*Field
	Meta   Meta
}

// Result is the outcome of a query: zero or more frames.
type Result struct {
	Frames []*Frame `json:"frames"`
}

// ErrLength is returned (wrapped) when field lengths would diverge.
var ErrLength = errors.New("field length mismatch")

// New builds a frame from fields, validating names and lengths.
func New(name string, fields ...*Field) (*Frame, error) {
	f := &Frame{Name: name, Fields: fields}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return f, nil
}

// Validate checks the frame invariants: valid, unique field names and types, and equal
// field lengths.
func (f *Frame) Validate() error {
	seen := make(map[string]struct{}, len(f.Fields))
	for i, fld := range f.Fields {
		if fld == nil {
			return fmt.Errorf("field %d is nil", i)
		}
		if err := validateName(fld.Name); err != nil {
			return fmt.Errorf("field %d: %w", i, err)
		}
		if !fld.Type.Valid() {
			return fmt.Errorf("field %q: unsupported type %q", fld.Name, fld.Type)
		}
		if _, dup := seen[fld.Name]; dup {
			return fmt.Errorf("duplicate field name %q", fld.Name)
		}
		seen[fld.Name] = struct{}{}
		if fld.Len() != f.Fields[0].Len() {
			return fmt.Errorf("field %q has %d values, field %q has %d: %w",
				fld.Name, fld.Len(), f.Fields[0].Name, f.Fields[0].Len(), ErrLength)
		}
	}
	return nil
}

// Rows returns the number of rows (0 for a frame without fields).
func (f *Frame) Rows() int {
	if len(f.Fields) == 0 {
		return 0
	}
	return f.Fields[0].Len()
}

// Field returns the field with the given name, or nil.
func (f *Frame) Field(name string) *Field {
	for _, fld := range f.Fields {
		if fld.Name == name {
			return fld
		}
	}
	return nil
}

// AppendRow appends one value per field atomically: if any value is rejected, no field
// is modified (FR-010 AC2/AC3).
func (f *Frame) AppendRow(vals ...any) error {
	if len(vals) != len(f.Fields) {
		return fmt.Errorf("row has %d values, frame has %d fields: %w", len(vals), len(f.Fields), ErrLength)
	}
	norm := make([]any, len(vals))
	for i, v := range vals {
		nv, err := f.Fields[i].normalize(v)
		if err != nil {
			return err
		}
		norm[i] = nv
	}
	for i, nv := range norm {
		f.Fields[i].values = append(f.Fields[i].values, nv)
	}
	return nil
}

// AddNotice attaches a notice to the frame.
func (f *Frame) AddNotice(sev Severity, text string) {
	f.Meta.Notices = append(f.Meta.Notices, Notice{Severity: sev, Text: text})
}
