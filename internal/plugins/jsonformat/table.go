package jsonformat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/plugin"
)

// kind is the JSON kind of a non-null cell.
type kind uint8

const (
	kindNumber kind = 1 << iota
	kindBool
	kindString
	kindNested // object or array
)

// sizeNull is the estimated size of a null value (ADR-0003).
var sizeNull = int64(frame.EstimateValueSize(nil))

// column holds one field's retained cells and running size totals under every type the
// field can end up with, so the byte limit is checked in O(1) per cell whatever the
// final type (FR-012 AC4/AC6).
type column struct {
	name     string
	fixed    frame.FieldType // fieldTypes override, or ""
	cells    []any
	kinds    kind
	allTime  bool  // every string seen parses as a time
	native   int64 // size with values of their own kind (valid when one kind is seen)
	asTime   int64 // size of the strings converted to time
	asString int64 // size with every value rendered as a string
	asFixed  int64 // size under the override
	failed   int   // values that do not parse under the override
	nonNull  int
}

func (c *column) typ() frame.FieldType {
	if c.fixed != "" {
		return c.fixed
	}
	switch c.kinds {
	case 0:
		return frame.TypeString
	case kindNumber:
		return frame.TypeNumber
	case kindBool:
		return frame.TypeBool
	case kindNested:
		return frame.TypeJSON
	case kindString:
		if c.allTime {
			return frame.TypeTime
		}
		return frame.TypeString
	}
	return frame.TypeString // mixed kinds
}

// size is the estimated retained size of the column under its current type.
func (c *column) size(rows int) int64 {
	nulls := int64(rows-c.nonNull) * sizeNull
	switch t := c.typ(); {
	case c.fixed != "":
		return c.asFixed + nulls
	case t == frame.TypeTime:
		return c.asTime + nulls
	case t == frame.TypeString && c.kinds != kindString && c.kinds != 0:
		return c.asString + nulls
	default:
		return c.native + nulls
	}
}

// delta is the change to a column from one more non-null cell.
type delta struct {
	kind                              kind
	time                              bool
	native, asTime, asString, asFixed int64
	failed                            bool
}

func (c *column) measure(v any) delta {
	var d delta
	switch x := v.(type) {
	case json.Number:
		d.kind = kindNumber
		n, _ := toNumber(x)
		d.native = int64(frame.EstimateValueSize(n))
	case bool:
		d.kind = kindBool
		d.native = int64(frame.EstimateValueSize(x))
	case string:
		d.kind = kindString
		d.native = int64(frame.EstimateValueSize(x))
		if ts, ok := convert(frame.TypeTime, x); ok {
			d.time, d.asTime = true, int64(frame.EstimateValueSize(ts))
		}
	case json.RawMessage:
		d.kind = kindNested
		d.native = int64(frame.EstimateValueSize(x))
	}
	d.asString = int64(frame.EstimateValueSize(renderString(v)))
	if c.fixed != "" {
		cv, ok := convert(c.fixed, v)
		d.asFixed, d.failed = int64(frame.EstimateValueSize(cv)), !ok
	}
	return d
}

func (c *column) apply(v any, d delta) {
	c.cells = append(c.cells, v)
	if v == nil {
		return
	}
	if c.kinds&kindString == 0 && d.kind == kindString {
		c.allTime = true
	}
	if d.kind == kindString && !d.time {
		c.allTime = false
	}
	c.kinds |= d.kind
	c.native += d.native
	c.asTime += d.asTime
	c.asString += d.asString
	c.asFixed += d.asFixed
	c.nonNull++
	if d.failed {
		c.failed++
	}
}

// table accumulates rows column-wise.
type table struct {
	overrides map[string]frame.FieldType
	lim       frame.Limits
	cols      []*column
	byKey     map[string]int
	names     map[string]bool
	rows      int
	total     int64
	stopped   bool // the byte limit refused a row
	notices   []string
}

func newTable(overrides map[string]frame.FieldType, lim frame.Limits) *table {
	return &table{overrides: overrides, lim: lim, byKey: map[string]int{}, names: map[string]bool{}}
}

func (t *table) notice(s string) { t.notices = append(t.notices, s) }

// fieldName derives a unique, valid field name for key (FR-012 AC3).
func (t *table) fieldName(key string, index int) string {
	name := key
	if name == "" {
		name = "Column " + strconv.Itoa(index+1)
	}
	name = truncateUTF8(name, frame.MaxFieldNameBytes)
	base := name
	for n := 2; t.names[name]; n++ {
		suffix := fmt.Sprintf(" (%d)", n)
		name = truncateUTF8(base, frame.MaxFieldNameBytes-len(suffix)) + suffix
	}
	return name
}

// add appends one row, or sets stopped when it would exceed the byte limit.
func (t *table) add(cells []cell) error {
	if t.stopped {
		return nil
	}
	// Resolve fields; duplicate keys in one row keep the last value.
	idx := make(map[int]any, len(cells))
	var fresh []string
	freshAt := map[string]int{}
	for _, c := range cells {
		i, ok := t.byKey[c.key]
		if !ok {
			j, seen := freshAt[c.key]
			if !seen {
				if len(t.cols)+len(fresh) >= MaxFields {
					return fmt.Errorf("more than %d fields: %w", MaxFields, plugin.ErrDecode)
				}
				j = len(fresh)
				freshAt[c.key] = j
				fresh = append(fresh, c.key)
			}
			i = len(t.cols) + j
		}
		idx[i] = c.v
	}
	newCols := make([]*column, len(fresh))
	for j, key := range fresh {
		newCols[j] = &column{fixed: t.overrides[key]}
	}
	colAt := func(i int) *column {
		if i < len(t.cols) {
			return t.cols[i]
		}
		return newCols[i-len(t.cols)]
	}
	// Plan the size of the retained rows plus this one.
	deltas := make(map[int]delta, len(idx))
	var size int64
	n := len(t.cols) + len(newCols)
	for i := range n {
		c := colAt(i)
		v, present := idx[i]
		if !present || v == nil {
			size += c.size(t.rows + 1)
			continue
		}
		d := c.measure(v)
		deltas[i] = d
		probe := *c
		probe.cells = nil
		probe.apply(v, d)
		size += probe.size(t.rows + 1)
	}
	if t.lim.MaxBytes > 0 && size > t.lim.MaxBytes {
		t.stopped = true
		t.notice(fmt.Sprintf("Result truncated at the byte limit of %d bytes.", t.lim.MaxBytes))
		return nil
	}
	for j, key := range fresh {
		c := newCols[j]
		c.name = t.fieldName(key, len(t.cols))
		c.cells = make([]any, t.rows, t.rows+1)
		t.names[c.name] = true
		t.byKey[key] = len(t.cols)
		t.cols = append(t.cols, c)
	}
	for i, c := range t.cols {
		v := idx[i]
		c.apply(v, deltas[i])
	}
	t.rows++
	t.total = size
	return nil
}

// frame builds the result. Every retained row fits the limits by construction; the
// builder still enforces them.
func (t *table) frame(ctx context.Context, truncated bool) ([]*frame.Frame, error) {
	fields := make([]*frame.Field, len(t.cols))
	types := make([]frame.FieldType, len(t.cols))
	for i, c := range t.cols {
		types[i] = c.typ()
		f, err := frame.NewField(c.name, types[i])
		if err != nil {
			return nil, err
		}
		fields[i] = f
	}
	b, err := frame.NewBuilder("", t.lim, fields...)
	if err != nil {
		return nil, err
	}
	row := make([]any, len(t.cols))
	for n := range t.rows {
		if n%cancelCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		for i, c := range t.cols {
			row[i], _ = convert(types[i], c.cells[n])
		}
		if err := b.AppendRow(row...); errors.Is(err, frame.ErrLimitReached) {
			break
		} else if err != nil {
			return nil, err
		}
	}
	fr := b.Frame()
	if truncated && !fr.Meta.Truncated {
		fr.Meta.Truncated = true
		for _, s := range t.notices {
			fr.AddNotice(frame.SeverityWarning, s)
		}
	}
	for _, c := range t.cols {
		if c.fixed != "" && c.failed > 0 {
			noun := "values"
			if c.failed == 1 {
				noun = "value"
			}
			fr.AddNotice(frame.SeverityWarning, fmt.Sprintf("%d %s in field %q could not be read as %s and were set to null.", c.failed, noun, c.name, c.fixed))
		}
		if c.fixed == "" && c.kinds == kindNumber {
			if bad := countBadNumbers(c.cells); bad > 0 {
				fr.AddNotice(frame.SeverityWarning, fmt.Sprintf("%d numbers in field %q are out of range and were set to null.", bad, c.name))
			}
		}
	}
	return []*frame.Frame{fr}, nil
}

func countBadNumbers(cells []any) int {
	bad := 0
	for _, v := range cells {
		if n, ok := v.(json.Number); ok {
			if _, ok := toNumber(n); !ok {
				bad++
			}
		}
	}
	return bad
}
