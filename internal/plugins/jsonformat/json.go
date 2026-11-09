// Package jsonformat implements the JSON and NDJSON format plugins (FR-012, ADR-0002).
//
// Decoding streams the input with encoding/json's token API: rows are decoded one at a
// time, values skipped on the way to options.rowsPath are consumed token by token, and
// input after the row-limit overflow point is never parsed. Each row is split into
// columns; types are inferred from the rows the frame keeps, and the byte limit is
// planned on converted values while rows are read, so held rows never exceed it.
package jsonformat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/plugin"
)

const (
	// MaxFields bounds the width of a frame (FR-012 AC3).
	MaxFields = 1024
	// MaxDepth bounds nesting anywhere in the input (FR-012 AC6).
	MaxDepth = 64
	// MaxFlattenDepth bounds how many object levels flatten expands (FR-012 AC5).
	MaxFlattenDepth = 8
	// maxPathSegments bounds options.rowsPath (FR-012 AC2).
	maxPathSegments = 16
	// cancelCheckEvery is how many rows are decoded between context checks.
	cancelCheckEvery = 256
)

// valueField is the field name for rows that are not objects (FR-012 AC2).
const valueField = "value"

var segmentRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Options are the format-specific settings in DecodeOptions.Options.
type Options struct {
	// RowsPath names the array holding the rows as dot-separated object keys (json only).
	RowsPath string `json:"rowsPath,omitempty"`
	// Flatten expands nested objects into "parent.child" fields.
	Flatten bool `json:"flatten,omitempty"`
}

// Format decodes JSON documents or NDJSON streams.
type Format struct {
	meta   plugin.FormatMeta
	ndjson bool
}

// NewJSON returns the JSON format plugin.
func NewJSON() *Format {
	return &Format{meta: plugin.FormatMeta{
		ID:          "json",
		Name:        "JSON",
		Description: "A JSON array of rows, optionally nested under rowsPath; nested objects can be flattened.",
		MediaTypes:  []string{"application/json"},
		Extensions:  []string{".json"},
	}}
}

// NewNDJSON returns the newline-delimited JSON (JSON Lines) format plugin.
func NewNDJSON() *Format {
	return &Format{ndjson: true, meta: plugin.FormatMeta{
		ID:          "ndjson",
		Name:        "NDJSON",
		Description: "Newline-delimited JSON (JSON Lines): one row per line.",
		MediaTypes:  []string{"application/x-ndjson"},
		Extensions:  []string{".ndjson", ".jsonl"},
	}}
}

// Meta implements plugin.Format.
func (f *Format) Meta() plugin.FormatMeta { return f.meta }

func (f *Format) options(raw json.RawMessage) (Options, []string, error) {
	var o Options
	if t := bytes.TrimSpace(raw); len(t) == 0 || string(t) == "null" {
		return o, nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		return o, nil, fmt.Errorf("%s options: %w", f.meta.ID, err)
	}
	if o.RowsPath == "" {
		return o, nil, nil
	}
	if f.ndjson {
		return o, nil, errors.New("rowsPath is not supported for ndjson")
	}
	segs := strings.Split(o.RowsPath, ".")
	if len(segs) > maxPathSegments {
		return o, nil, fmt.Errorf("rowsPath has more than %d segments", maxPathSegments)
	}
	for _, s := range segs {
		if !segmentRE.MatchString(s) {
			return o, nil, errors.New("rowsPath must be dot-separated keys of letters, digits, '_' or '-'")
		}
	}
	return o, segs, nil
}

// Decode implements plugin.Format.
func (f *Format) Decode(ctx context.Context, r io.Reader, opts plugin.DecodeOptions) ([]*frame.Frame, error) {
	o, path, err := f.options(opts.Options)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", plugin.ErrOptions, err)
	}
	for name, t := range opts.FieldTypes {
		if !t.Valid() {
			return nil, fmt.Errorf("fieldTypes: %q has unsupported type %q", name, t)
		}
	}
	br := bufio.NewReader(r)
	if b, err := br.Peek(3); err == nil && bytes.Equal(b, []byte{0xEF, 0xBB, 0xBF}) {
		_, _ = br.Discard(3)
	}
	d := &decoder{
		ctx: ctx, dec: json.NewDecoder(br), lim: opts.Limits, flatten: o.Flatten,
		tab: newTable(opts.FieldTypes, opts.Limits),
	}
	d.dec.UseNumber()
	if f.ndjson {
		err = d.ndjson()
	} else {
		err = d.document(br, path)
	}
	if err != nil {
		return nil, err
	}
	return d.tab.frame(ctx, d.truncated)
}

// decoder walks the input and feeds rows to a table.
type decoder struct {
	ctx       context.Context
	dec       *json.Decoder
	lim       frame.Limits
	flatten   bool
	tab       *table
	rows      int
	truncated bool
}

// full reports whether no further row may be added; it marks the result truncated when
// input remains.
func (d *decoder) full(more bool) bool {
	if d.tab.stopped {
		d.truncated = true
		return true
	}
	if d.lim.MaxRows > 0 && d.rows >= d.lim.MaxRows && more {
		d.truncated = true
		d.tab.notice(fmt.Sprintf("Result truncated at the row limit of %d rows.", d.lim.MaxRows))
		return true
	}
	return false
}

func (d *decoder) ndjson() error {
	for {
		if d.rows%cancelCheckEvery == 0 {
			if err := d.ctx.Err(); err != nil {
				return err
			}
		}
		if d.full(d.dec.More()) {
			return nil
		}
		var raw json.RawMessage
		err := d.dec.Decode(&raw)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return decodeError(fmt.Sprintf("row %d", d.rows+1), err)
		}
		if err := d.row(raw, fmt.Sprintf("row %d", d.rows+1)); err != nil {
			return err
		}
	}
}

func (d *decoder) document(br *bufio.Reader, path []string) error {
	first, err := firstByte(br)
	if err != nil {
		return err
	}
	switch {
	case len(path) > 0:
		if err := d.navigate(path); err != nil {
			return err
		}
	case first == '[':
		if _, err := d.dec.Token(); err != nil {
			return decodeError("", err)
		}
	default: // a single value is one row
		var raw json.RawMessage
		if err := d.dec.Decode(&raw); err != nil {
			return decodeError("", err)
		}
		if err := d.row(raw, "the document"); err != nil {
			return err
		}
		return d.expectEOF()
	}
	if err := d.array(); err != nil || d.truncated {
		return err
	}
	if _, err := d.dec.Token(); err != nil { // the closing ']'
		return decodeError("", err)
	}
	for range path { // finish every enclosing object so trailing syntax is checked
		if err := d.skipRest(); err != nil {
			return err
		}
	}
	return d.expectEOF()
}

// firstByte returns the first non-space byte without consuming it.
func firstByte(br *bufio.Reader) (byte, error) {
	for {
		b, err := br.Peek(1)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0, fmt.Errorf("input is empty: %w", plugin.ErrDecode)
			}
			return 0, err
		}
		switch b[0] {
		case ' ', '\t', '\r', '\n':
			_, _ = br.Discard(1)
		default:
			return b[0], nil
		}
	}
}

// array decodes elements of an array whose '[' was consumed, up to the limits.
func (d *decoder) array() error {
	for {
		if d.rows%cancelCheckEvery == 0 {
			if err := d.ctx.Err(); err != nil {
				return err
			}
		}
		more := d.dec.More()
		if d.full(more) || !more {
			return nil
		}
		var raw json.RawMessage
		if err := d.dec.Decode(&raw); err != nil {
			return decodeError("", err)
		}
		if err := d.row(raw, fmt.Sprintf("row %d", d.rows+1)); err != nil {
			return err
		}
	}
}

// navigate consumes tokens up to and including the '[' of the array at path.
func (d *decoder) navigate(path []string) error {
	for i, seg := range path {
		if err := d.expectDelim('{', path[:i]); err != nil {
			return err
		}
		found := false
		for d.dec.More() {
			tok, err := d.dec.Token()
			if err != nil {
				return decodeError("", err)
			}
			if key, _ := tok.(string); key == seg {
				found = true
				break
			}
			if err := d.skipValue(i + 1); err != nil {
				return err
			}
		}
		if !found {
			return fmt.Errorf("rowsPath %q: key %q not found: %w", strings.Join(path, "."), seg, plugin.ErrDecode)
		}
	}
	return d.expectDelim('[', path)
}

func (d *decoder) expectDelim(want json.Delim, at []string) error {
	tok, err := d.dec.Token()
	if err != nil {
		return decodeError("", err)
	}
	if tok != want {
		what := "an object"
		if want == '[' {
			what = "an array"
		}
		where := "the document"
		if len(at) > 0 {
			where = fmt.Sprintf("rowsPath %q", strings.Join(at, "."))
		}
		return fmt.Errorf("%s is not %s: %w", where, what, plugin.ErrDecode)
	}
	return nil
}

// skipValue consumes one value token by token; depth is the nesting already entered.
func (d *decoder) skipValue(depth int) error {
	open := 0
	for {
		tok, err := d.dec.Token()
		if err != nil {
			return decodeError("", err)
		}
		switch tok {
		case json.Delim('{'), json.Delim('['):
			open++
			if depth+open > MaxDepth {
				return nestingError()
			}
		case json.Delim('}'), json.Delim(']'):
			open--
		}
		if open == 0 {
			return nil
		}
	}
}

// skipRest consumes the remaining members of the current object and its closing '}'.
func (d *decoder) skipRest() error {
	for d.dec.More() {
		if _, err := d.dec.Token(); err != nil { // key
			return decodeError("", err)
		}
		if err := d.skipValue(1); err != nil {
			return err
		}
	}
	_, err := d.dec.Token()
	if err != nil {
		return decodeError("", err)
	}
	return nil
}

func (d *decoder) expectEOF() error {
	if _, err := d.dec.Token(); !errors.Is(err, io.EOF) {
		if err != nil && !isSyntax(err) {
			return err
		}
		return fmt.Errorf("trailing data after the JSON value at offset %d: %w", d.dec.InputOffset(), plugin.ErrDecode)
	}
	return nil
}

// row splits one decoded row into cells and hands it to the table.
func (d *decoder) row(raw json.RawMessage, where string) error {
	if depth(raw) > MaxDepth {
		return nestingError()
	}
	var cells []cell
	if t := bytes.TrimSpace(raw); len(t) > 0 && t[0] == '{' {
		var err error
		if cells, err = objectCells(t, "", 0, d.flatten); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
	} else {
		cells = []cell{{key: valueField, v: scalar(t)}}
	}
	if err := d.tab.add(cells); err != nil {
		return err
	}
	if !d.tab.stopped {
		d.rows++
	}
	return nil
}

type cell struct {
	key string
	v   any // nil, json.Number, bool, string or json.RawMessage (object/array)
}

// objectCells returns the members of an object in order; with flatten, nested objects
// up to MaxFlattenDepth become prefixed members. Duplicate keys keep the last value.
func objectCells(raw json.RawMessage, prefix string, level int, flatten bool) ([]cell, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if _, err := dec.Token(); err != nil { // '{'
		return nil, decodeError("", err)
	}
	var out []cell
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, decodeError("", err)
		}
		key := prefix + tok.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, decodeError("", err)
		}
		if flatten && level+1 < MaxFlattenDepth && len(v) > 0 && v[0] == '{' {
			nested, err := objectCells(v, key+".", level+1, flatten)
			if err != nil {
				return nil, err
			}
			out = append(out, nested...)
			continue
		}
		out = append(out, cell{key: key, v: scalar(v)})
	}
	return out, nil
}

// scalar converts a raw value to its cell form.
func scalar(raw json.RawMessage) any {
	switch {
	case len(raw) == 0 || string(raw) == "null":
		return nil
	case string(raw) == "true":
		return true
	case string(raw) == "false":
		return false
	case raw[0] == '"':
		var s string
		_ = json.Unmarshal(raw, &s) // already validated by the decoder
		return s
	case raw[0] == '{' || raw[0] == '[':
		var b bytes.Buffer
		if err := json.Compact(&b, raw); err != nil {
			return json.RawMessage(append([]byte(nil), raw...))
		}
		return json.RawMessage(b.Bytes())
	default:
		return json.Number(raw)
	}
}

// depth returns the maximum nesting of raw, ignoring brackets inside strings.
func depth(raw []byte) int {
	cur, maxd, inStr, esc := 0, 0, false, false
	for _, b := range raw {
		switch {
		case inStr:
			switch {
			case esc:
				esc = false
			case b == '\\':
				esc = true
			case b == '"':
				inStr = false
			}
		case b == '"':
			inStr = true
		case b == '{' || b == '[':
			cur++
			maxd = max(maxd, cur)
		case b == '}' || b == ']':
			cur--
		}
	}
	return maxd
}

func nestingError() error {
	return fmt.Errorf("nesting deeper than %d levels: %w", MaxDepth, plugin.ErrDecode)
}

func isSyntax(err error) bool {
	var se *json.SyntaxError
	var te *json.UnmarshalTypeError
	return errors.As(err, &se) || errors.As(err, &te) || errors.Is(err, io.ErrUnexpectedEOF)
}

// decodeError maps encoding/json errors to content-free decode errors (FR-012 AC7);
// other errors (cancellation, the file size cap) pass through unchanged.
func decodeError(where string, err error) error {
	if where != "" {
		where += ": "
	}
	var se *json.SyntaxError
	switch {
	case errors.As(err, &se) && strings.Contains(se.Error(), "unexpected end"):
		// Newer Go releases report truncated input as a SyntaxError.
		return fmt.Errorf("%sunexpected end of input: %w", where, plugin.ErrDecode)
	case errors.As(err, &se):
		return fmt.Errorf("%sinvalid JSON syntax at offset %d: %w", where, se.Offset, plugin.ErrDecode)
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return fmt.Errorf("%sunexpected end of input: %w", where, plugin.ErrDecode)
	case isSyntax(err):
		return fmt.Errorf("%sinvalid JSON value: %w", where, plugin.ErrDecode)
	}
	return err
}

// renderString renders a cell as text for string fields.
func renderString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return string(x)
	case bool:
		return strconv.FormatBool(x)
	case json.RawMessage:
		return string(x)
	}
	return ""
}

// toNumber converts a JSON number to int64 when integral and in range, else to a finite
// float64.
func toNumber(n json.Number) (any, bool) {
	if i, err := strconv.ParseInt(string(n), 10, 64); err == nil {
		return i, true
	}
	if f, err := strconv.ParseFloat(string(n), 64); err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) {
		return f, true
	}
	return nil, false
}

// convert converts a cell to type t. ok is false when a non-null cell does not fit t.
func convert(t frame.FieldType, v any) (any, bool) {
	if v == nil {
		return nil, true
	}
	switch t {
	case frame.TypeString:
		return renderString(v), true
	case frame.TypeNumber:
		switch x := v.(type) {
		case json.Number:
			return toNumber(x)
		case string:
			return toNumber(json.Number(strings.TrimSpace(x)))
		}
	case frame.TypeBool:
		switch x := v.(type) {
		case bool:
			return x, true
		case string:
			switch strings.ToLower(strings.TrimSpace(x)) {
			case "true":
				return true, true
			case "false":
				return false, true
			}
		}
	case frame.TypeTime:
		if s, ok := v.(string); ok {
			if ts, ok := frame.ParseTime(strings.TrimSpace(s)); ok {
				return ts, true
			}
		}
	case frame.TypeJSON:
		switch x := v.(type) {
		case json.RawMessage:
			return x, true
		case json.Number:
			return json.RawMessage(x), true
		default:
			b, err := json.Marshal(x)
			if err != nil {
				return nil, false
			}
			return json.RawMessage(b), true
		}
	}
	return nil, false
}

// truncateUTF8 shortens s to at most n bytes without splitting a rune.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
