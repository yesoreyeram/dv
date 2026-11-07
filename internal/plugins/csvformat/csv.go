// Package csvformat implements the CSV and TSV format plugins (FR-011, ADR-0002).
//
// Decoding reads records up to the host's row and byte limits, infers a type per column
// (bool, number, time, else string) unless overridden, and builds one frame with the
// limit-enforcing frame.Builder.
package csvformat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/plugin"
)

// MaxColumns bounds the width of a file (FR-011 AC6).
const MaxColumns = 1024

// cancelCheckEvery is how many records are read between context checks.
const cancelCheckEvery = 1024

// ErrDecode is wrapped by every error caused by the input data (as opposed to options).
var ErrDecode = errors.New("decode failed")

// Options are the format-specific settings in DecodeOptions.Options.
type Options struct {
	// Delimiter is a single character; it defaults to ',' (csv) or '\t' (tsv).
	Delimiter string `json:"delimiter,omitempty"`
	// Comment, if set, is a single character that starts comment lines.
	Comment string `json:"comment,omitempty"`
	// Header reports whether the first record holds field names (default true).
	Header *bool `json:"header,omitempty"`
}

// Format decodes delimiter-separated values.
type Format struct {
	meta      plugin.FormatMeta
	delimiter rune
}

// NewCSV returns the comma-separated format plugin.
func NewCSV() *Format {
	return &Format{delimiter: ',', meta: plugin.FormatMeta{
		ID:          "csv",
		Name:        "CSV",
		Description: "Comma-separated values with a header row; column types are inferred.",
		MediaTypes:  []string{"text/csv"},
		Extensions:  []string{".csv"},
	}}
}

// NewTSV returns the tab-separated format plugin.
func NewTSV() *Format {
	return &Format{delimiter: '\t', meta: plugin.FormatMeta{
		ID:          "tsv",
		Name:        "TSV",
		Description: "Tab-separated values with a header row; column types are inferred.",
		MediaTypes:  []string{"text/tab-separated-values"},
		Extensions:  []string{".tsv", ".tab"},
	}}
}

// Meta implements plugin.Format.
func (f *Format) Meta() plugin.FormatMeta { return f.meta }

type settings struct {
	delimiter rune
	comment   rune
	header    bool
}

func (f *Format) settings(raw json.RawMessage) (settings, error) {
	s := settings{delimiter: f.delimiter, header: true}
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return s, nil
	}
	var o Options
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		return s, fmt.Errorf("%s options: %w", f.meta.ID, err)
	}
	if o.Delimiter != "" {
		d, err := singleRune("delimiter", o.Delimiter)
		if err != nil {
			return s, err
		}
		s.delimiter = d
	}
	if o.Comment != "" {
		c, err := singleRune("comment", o.Comment)
		if err != nil {
			return s, err
		}
		s.comment = c
	}
	if o.Header != nil {
		s.header = *o.Header
	}
	if s.comment == s.delimiter {
		return s, errors.New("comment and delimiter must differ")
	}
	return s, nil
}

func singleRune(name, v string) (rune, error) {
	r, size := utf8.DecodeRuneInString(v)
	if size != len(v) || r == utf8.RuneError || r == '"' || r == '\r' || r == '\n' {
		return 0, fmt.Errorf("%s must be a single character other than quote or newline", name)
	}
	return r, nil
}

// Decode implements plugin.Format.
func (f *Format) Decode(ctx context.Context, r io.Reader, opts plugin.DecodeOptions) ([]*frame.Frame, error) {
	s, err := f.settings(opts.Options)
	if err != nil {
		return nil, err
	}
	for name, t := range opts.FieldTypes {
		if !t.Valid() {
			return nil, fmt.Errorf("fieldTypes: %q has unsupported type %q", name, t)
		}
	}
	cr := csv.NewReader(stripBOM(r))
	cr.Comma = s.delimiter
	cr.Comment = s.comment
	cr.FieldsPerRecord = 0 // the first record fixes the width
	cr.ReuseRecord = false
	// TSV (IANA text/tab-separated-values) has no quoting, so quotes are literal there.
	cr.LazyQuotes = f.meta.ID == "tsv"

	names, records, byteStop, err := f.read(ctx, cr, s.header, opts.Limits)
	if err != nil {
		return nil, err
	}
	fr, err := build(names, records, opts)
	if err != nil {
		return nil, err
	}
	if byteStop && !fr.Meta.Truncated {
		fr.Meta.Truncated = true
		fr.AddNotice(frame.SeverityWarning, fmt.Sprintf("Result truncated at the byte limit of %d bytes.", opts.Limits.MaxBytes))
	}
	return []*frame.Frame{fr}, nil
}

// read returns the field names and up to MaxRows+1 records (the extra one lets the
// builder detect truncation). byteStop reports that reading stopped at the byte limit.
func (f *Format) read(ctx context.Context, cr *csv.Reader, header bool, lim frame.Limits) (names []string, records [][]string, byteStop bool, err error) {
	var bytesRead int64
	for n := 0; ; n++ {
		if n%cancelCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, false, err
			}
		}
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, false, parseError(err)
		}
		if len(rec) > MaxColumns {
			line, _ := cr.FieldPos(0)
			return nil, nil, false, fmt.Errorf("line %d has %d columns, limit is %d: %w", line, len(rec), MaxColumns, ErrDecode)
		}
		if names == nil {
			if header {
				names = uniqueNames(rec)
				continue
			}
			names = uniqueNames(make([]string, len(rec)))
		}
		if lim.MaxRows > 0 && len(records) > lim.MaxRows {
			break // already holds the overflow record
		}
		var size int64
		for _, c := range rec {
			size += int64(len(c)) + 16
		}
		if lim.MaxBytes > 0 && bytesRead+size > lim.MaxBytes {
			return names, records, true, nil
		}
		bytesRead += size
		records = append(records, rec)
	}
	if names == nil {
		return nil, nil, false, fmt.Errorf("input is empty: %w", ErrDecode)
	}
	return names, records, false, nil
}

func parseError(err error) error {
	var pe *csv.ParseError
	if errors.As(err, &pe) {
		msg := pe.Err.Error()
		if errors.Is(pe.Err, csv.ErrFieldCount) {
			msg = "wrong number of columns"
		}
		return fmt.Errorf("line %d, column %d: %s: %w", pe.Line, pe.Column, msg, ErrDecode)
	}
	return err
}

// stripBOM drops a leading UTF-8 byte-order mark.
func stripBOM(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	if b, err := br.Peek(3); err == nil && bytes.Equal(b, []byte{0xEF, 0xBB, 0xBF}) {
		_, _ = br.Discard(3)
	}
	return br
}

// uniqueNames trims header names, fills blanks with "Column N" and de-duplicates.
func uniqueNames(header []string) []string {
	out := make([]string, len(header))
	seen := make(map[string]int, len(header))
	for i, h := range header {
		name := strings.TrimSpace(h)
		if name == "" {
			name = "Column " + strconv.Itoa(i+1)
		}
		if len(name) > frame.MaxFieldNameBytes {
			name = truncateUTF8(name, frame.MaxFieldNameBytes-8)
		}
		base := name
		for seen[name] > 0 {
			seen[base]++
			name = fmt.Sprintf("%s (%d)", base, seen[base])
		}
		seen[name]++
		out[i] = name
	}
	return out
}

func truncateUTF8(s string, n int) string {
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func build(names []string, records [][]string, opts plugin.DecodeOptions) (*frame.Frame, error) {
	types := make([]frame.FieldType, len(names))
	fields := make([]*frame.Field, len(names))
	for i, name := range names {
		t, ok := opts.FieldTypes[name]
		if !ok {
			t = infer(records, i)
		}
		fld, err := frame.NewField(name, t)
		if err != nil {
			return nil, err
		}
		types[i], fields[i] = t, fld
	}
	b, err := frame.NewBuilder("", opts.Limits, fields...)
	if err != nil {
		return nil, err
	}
	failed := make([]int, len(names))
	row := make([]any, len(names))
	for _, rec := range records {
		for i, cell := range rec {
			v, ok := convert(types[i], cell)
			if !ok {
				failed[i]++
			}
			row[i] = v
		}
		if err := b.AppendRow(row...); errors.Is(err, frame.ErrLimitReached) {
			break
		} else if err != nil {
			return nil, err
		}
	}
	fr := b.Frame()
	for i, n := range failed {
		if n > 0 {
			noun := "values"
			if n == 1 {
				noun = "value"
			}
			fr.AddNotice(frame.SeverityWarning, fmt.Sprintf("%d %s in field %q could not be read as %s and were set to null.", n, noun, names[i], types[i]))
		}
	}
	return fr, nil
}

// infer picks the narrowest type that every non-empty cell of column i parses as.
func infer(records [][]string, i int) frame.FieldType {
	candidates := []frame.FieldType{frame.TypeBool, frame.TypeNumber, frame.TypeTime}
	seen := false
	for _, rec := range records {
		cell := strings.TrimSpace(rec[i])
		if cell == "" {
			continue
		}
		seen = true
		kept := candidates[:0]
		for _, t := range candidates {
			if _, ok := convert(t, cell); ok {
				kept = append(kept, t)
			}
		}
		candidates = kept
		if len(candidates) == 0 {
			return frame.TypeString
		}
	}
	if !seen {
		return frame.TypeString
	}
	return candidates[0]
}

// timeLayouts are accepted for time inference, all interpreted as UTC when no offset is
// present.
var timeLayouts = []string{time.RFC3339Nano, "2006-01-02", "2006-01-02 15:04:05", "2006-01-02T15:04:05"}

// convert parses cell as type t. Empty cells are null and ok. ok is false when a
// non-empty cell does not parse; the value is then null.
func convert(t frame.FieldType, cell string) (any, bool) {
	c := strings.TrimSpace(cell)
	if c == "" {
		return nil, true
	}
	switch t {
	case frame.TypeString:
		return cell, true
	case frame.TypeBool:
		switch strings.ToLower(c) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	case frame.TypeNumber:
		if n, err := strconv.ParseInt(c, 10, 64); err == nil {
			return n, true
		}
		if f, err := strconv.ParseFloat(c, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) && !isSpecialFloat(c) {
			return f, true
		}
	case frame.TypeTime:
		for _, l := range timeLayouts {
			if ts, err := time.Parse(l, c); err == nil {
				return ts.UTC(), true
			}
		}
	case frame.TypeJSON:
		if json.Valid([]byte(c)) {
			return json.RawMessage(c), true
		}
	}
	return nil, false
}

// isSpecialFloat rejects hex floats and underscores, which ParseFloat accepts but are
// not plain decimal numbers in data files.
func isSpecialFloat(s string) bool {
	return strings.ContainsAny(s, "xXpP_")
}
