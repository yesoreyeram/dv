// Package frame implements dv's common columnar data-frame model (ADR-0003, FR-010).
//
// Every data source normalises its result into one or more Frames so panels can render
// data from any source identically. A Frame is a named, ordered set of equally long
// Fields. Each Field has a FieldType and stores its values as the corresponding Go type
// (see FieldType) or nil for null.
package frame

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

// FieldType is the logical type of a field's values.
type FieldType string

// Supported field types and the Go type each stores.
const (
	TypeTime   FieldType = "time"   // time.Time, normalised to UTC
	TypeNumber FieldType = "number" // float64, or int64 when the source is integral
	TypeString FieldType = "string" // string
	TypeBool   FieldType = "bool"   // bool
	TypeJSON   FieldType = "json"   // json.RawMessage (valid JSON)
)

// Valid reports whether t is a supported field type.
func (t FieldType) Valid() bool {
	switch t {
	case TypeTime, TypeNumber, TypeString, TypeBool, TypeJSON:
		return true
	}
	return false
}

// MaxFieldNameBytes bounds field names (FR-010 AC6).
const MaxFieldNameBytes = 256

// ErrType is returned (wrapped) when a value does not match its field's type.
var ErrType = errors.New("value type does not match field type")

// FieldConfig carries display hints. All members are optional.
type FieldConfig struct {
	DisplayName string `json:"displayName,omitempty"`
	Unit        string `json:"unit,omitempty"`
	Decimals    *int   `json:"decimals,omitempty"`
}

// Field is a named, typed column. Use NewField to construct one.
type Field struct {
	Name   string
	Type   FieldType
	Config FieldConfig
	values []any
}

// NewField returns an empty field after validating its name and type.
func NewField(name string, typ FieldType) (*Field, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	if !typ.Valid() {
		return nil, fmt.Errorf("field %q: unsupported type %q", name, typ)
	}
	return &Field{Name: name, Type: typ}, nil
}

func validateName(name string) error {
	if name == "" {
		return errors.New("field name must not be empty")
	}
	if len(name) > MaxFieldNameBytes {
		return fmt.Errorf("field name exceeds %d bytes", MaxFieldNameBytes)
	}
	return nil
}

// Len returns the number of values in the field.
func (f *Field) Len() int { return len(f.values) }

// At returns the value at row i (nil for null). It panics if i is out of range, like a
// slice index.
func (f *Field) At(i int) any { return f.values[i] }

// Values returns a copy of the field's values.
func (f *Field) Values() []any { return append([]any(nil), f.values...) }

// Append adds one value after normalising it to the field's Go type.
func (f *Field) Append(v any) error {
	nv, err := f.normalize(v)
	if err != nil {
		return err
	}
	f.values = append(f.values, nv)
	return nil
}

// normalize converts v to the canonical Go type for f.Type, or returns an error wrapping
// ErrType. nil is always accepted as null.
func (f *Field) normalize(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch f.Type {
	case TypeTime:
		switch t := v.(type) {
		case time.Time:
			return t.UTC(), nil
		case *time.Time:
			if t == nil {
				return nil, nil
			}
			return t.UTC(), nil
		}
	case TypeNumber:
		if n, ok := normalizeNumber(v); ok {
			return n, nil
		}
	case TypeString:
		if s, ok := v.(string); ok {
			return s, nil
		}
	case TypeBool:
		if b, ok := v.(bool); ok {
			return b, nil
		}
	case TypeJSON:
		if r, ok := v.(json.RawMessage); ok {
			if !json.Valid(r) {
				return nil, fmt.Errorf("field %q: invalid JSON value: %w", f.Name, ErrType)
			}
			return append(json.RawMessage(nil), r...), nil
		}
	}
	return nil, fmt.Errorf("field %q: cannot store %T in a %s field: %w", f.Name, v, f.Type, ErrType)
}

// normalizeNumber maps Go numeric types to float64 or int64. NaN and ±Inf become null
// because JSON cannot represent them (FR-010 AC3).
func normalizeNumber(v any) (any, bool) {
	switch n := v.(type) {
	case float64:
		return finiteOrNil(n), true
	case float32:
		return finiteOrNil(float64(n)), true
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint:
		return normalizeNumber(uint64(n))
	case uint64:
		if n > math.MaxInt64 {
			return float64(n), true
		}
		return int64(n), true
	}
	return nil, false
}

func finiteOrNil(f float64) any {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return f
}

// Per-value memory estimates on 64-bit platforms. Values live in a []any, so every value
// costs a 16-byte interface slot plus whatever the interface points at.
const (
	sizeSlot      = 16 // interface header in []any
	sizeBoxedWord = 8  // heap box for int64/float64
	sizeStringHdr = 16 // string header (pointer + length)
	sizeSliceHdr  = 24 // slice header for json.RawMessage
	sizeTime      = 24 // time.Time (wall, ext, loc)
)

// EstimateValueSize returns the estimated retained size of one stored value, as counted
// against Limits.MaxBytes by Builder.AppendRow. v must already be of the canonical Go type
// for its field (int64, float64, bool, string, time.Time, json.RawMessage or nil). Decoders
// use it to plan which rows fit before building a frame.
func EstimateValueSize(v any) int { return estimateSize(v) }

// estimateSize approximates the retained memory of one value for byte limits
// (NFR-PERF-003). It errs high rather than low: a byte limit is a memory guard, so an
// estimate below the real cost lets results overshoot it. Growth slack in the backing
// []any (append doubling) is not counted; see ADR-0003.
func estimateSize(v any) int {
	switch x := v.(type) {
	case nil, bool:
		return sizeSlot
	case string:
		return sizeSlot + sizeStringHdr + len(x)
	case json.RawMessage:
		return sizeSlot + sizeSliceHdr + len(x)
	case []byte:
		return sizeSlot + sizeSliceHdr + len(x)
	case time.Time, *time.Time:
		return sizeSlot + sizeTime
	default:
		return sizeSlot + sizeBoxedWord
	}
}

// timeLayouts are the text forms decoders infer as time (FR-011 AC7), interpreted as UTC
// when no offset is present.
var timeLayouts = []string{time.RFC3339Nano, "2006-01-02", "2006-01-02 15:04:05", "2006-01-02T15:04:05"}

// ParseTime parses s under the layouts decoders infer as time and returns it in UTC.
func ParseTime(s string) (time.Time, bool) {
	for _, l := range timeLayouts {
		if ts, err := time.Parse(l, s); err == nil {
			return ts.UTC(), true
		}
	}
	return time.Time{}, false
}
