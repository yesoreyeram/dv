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

// singleRune accepts one printable ASCII character or tab. ASCII keeps the streaming
// column guard exact (see columnGuard).
func singleRune(name, v string) (rune, error) {
	if len(v) != 1 || v[0] == '"' || (v[0] != '\t' && (v[0] < 0x20 || v[0] > 0x7e)) {
		return 0, fmt.Errorf("%s must be a single printable ASCII character or tab, other than a quote", name)
	}
	return rune(v[0]), nil
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
	var rr recordReader
	if f.meta.ID == "tsv" {
		// TSV (IANA text/tab-separated-values) has no quoting: quotes are literal data.
		rr = &tsvReader{br: bufio.NewReader(stripBOM(r)), delim: string(s.delimiter), comment: s.comment}
	} else {
		guard := &columnGuard{r: stripBOM(r), delim: byte(s.delimiter), comment: byte(s.comment), atLineStart: true, line: 1}
		cr := csv.NewReader(guard)
		cr.Comma = s.delimiter
		cr.Comment = s.comment
		cr.FieldsPerRecord = 0 // the first record fixes the width
		rr = csvReader{cr}
	}

	names, records, byteStop, err := read(ctx, rr, s.header, opts.Limits)
	if err != nil {
		return nil, err
	}
	fr, err := build(ctx, names, records, opts)
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
func read(ctx context.Context, rr recordReader, header bool, lim frame.Limits) (names []string, records [][]string, byteStop bool, err error) {
	var bytesRead int64
	for n := 0; ; n++ {
		if names != nil && lim.MaxRows > 0 && len(records) > lim.MaxRows {
			break // already holds the overflow record; later input is never parsed
		}
		if n%cancelCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, false, err
			}
		}
		rec, err := rr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, false, err
		}
		if names == nil {
			if header {
				names = uniqueNames(rec)
				continue
			}
			names = uniqueNames(make([]string, len(rec)))
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

// recordReader yields records; errors caused by the input wrap ErrDecode and name the
// line. Implementations enforce MaxColumns and a constant record width.
type recordReader interface {
	Read() ([]string, error)
}

// csvReader adapts encoding/csv (RFC 4180 quoting).
type csvReader struct{ cr *csv.Reader }

func (c csvReader) Read() ([]string, error) {
	rec, err := c.cr.Read()
	if err != nil {
		return nil, parseError(err) // the column cap is enforced by columnGuard
	}
	return rec, nil
}

// columnGuard counts unquoted delimiters per record while bytes stream into
// encoding/csv, and fails as soon as a record would exceed MaxColumns. encoding/csv
// allocates every field of a record before returning it, so without this a single wide
// record could allocate many times the file size before the width check runs.
// It follows RFC 4180 quoting: '"' toggles quoted state (a doubled quote toggles twice),
// and delimiters and newlines inside quotes are data. Comment lines are skipped.
type columnGuard struct {
	r           io.Reader
	delim       byte
	comment     byte
	inQuote     bool
	inComment   bool
	atLineStart bool
	delims      int
	line        int
	err         error // deferred until the consumer reaches the offending record
}

func (g *columnGuard) Read(p []byte) (int, error) {
	if g.err != nil {
		return 0, g.err
	}
	n, err := g.r.Read(p)
	// recordStart is where the current record begins in p (0 if it began earlier).
	recordStart := 0
	for i, b := range p[:n] {
		switch {
		case g.inComment:
			if b == '\n' {
				g.inComment, g.atLineStart = false, true
				g.line++
				recordStart = i + 1
			}
			continue
		case g.atLineStart && !g.inQuote && g.comment != 0 && b == g.comment:
			g.inComment = true
			continue
		}
		g.atLineStart = false
		switch {
		case b == '"':
			g.inQuote = !g.inQuote
		case b == '\n':
			g.line++
			if !g.inQuote {
				g.delims, g.atLineStart = 0, true
				recordStart = i + 1
			}
		case b == g.delim && !g.inQuote:
			g.delims++
			if g.delims >= MaxColumns {
				// Hand over the complete records before this one; fail when the
				// consumer asks for more, so a row limit reached earlier still wins.
				g.err = fmt.Errorf("line %d has more than %d columns: %w", g.line, MaxColumns, ErrDecode)
				if recordStart > 0 {
					return recordStart, nil
				}
				return 0, g.err
			}
		}
	}
	return n, err
}

func tooWide(line, n int) error {
	return fmt.Errorf("line %d has %d columns, limit is %d: %w", line, n, MaxColumns, ErrDecode)
}

// tsvReader splits lines on the delimiter without any quote handling. Like
// encoding/csv it accepts \n or \r\n line endings and skips empty and comment lines.
type tsvReader struct {
	br      *bufio.Reader
	delim   string
	comment rune
	line    int
	width   int
}

func (t *tsvReader) Read() ([]string, error) {
	for {
		s, err := t.br.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if s == "" && err != nil {
			return nil, io.EOF
		}
		t.line++
		s = strings.TrimSuffix(strings.TrimSuffix(s, "\n"), "\r")
		if s == "" || (t.comment != 0 && strings.HasPrefix(s, string(t.comment))) {
			continue
		}
		n := strings.Count(s, t.delim) + 1
		if n > MaxColumns {
			return nil, tooWide(t.line, n)
		}
		if t.width == 0 {
			t.width = n
		} else if n != t.width {
			return nil, fmt.Errorf("line %d: wrong number of columns (%d, expected %d): %w", t.line, n, t.width, ErrDecode)
		}
		return strings.Split(s, t.delim), nil
	}
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
		name = truncateUTF8(name, frame.MaxFieldNameBytes)
		base := name
		for seen[name] > 0 {
			seen[base]++
			suffix := fmt.Sprintf(" (%d)", seen[base])
			// Shorten the base so the suffixed name still fits the field-name limit.
			name = truncateUTF8(base, frame.MaxFieldNameBytes-len(suffix)) + suffix
		}
		seen[name]++
		out[i] = name
	}
	return out
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

// build infers types and converts records into a frame. It checks ctx every
// cancelCheckEvery rows so a cancelled query stops consuming CPU.
//
// Types are inferred only from rows the frame keeps (FR-011 AC4/AC7). The byte limit is
// counted on converted values, so whether a row fits depends on the column types, and
// the types depend on which rows are kept. build therefore streams over column-wise
// state: each record narrows the per-column candidate types and is accepted only if the
// retained prefix plus the record fits under the types inferred from that prefix. When a
// record changes a column's type, only that column is re-converted over the prefix. A
// column's type changes at most four times (from string while only empty cells are seen,
// then only by dropping candidates among bool, number and time, finally to string), so
// the total work is linear in the retained data: at most five conversions per cell. The
// overflow record read past MaxRows never influences types; it only lets the builder flag
// the row limit.
func build(ctx context.Context, names []string, records [][]string, opts plugin.DecodeOptions) (*frame.Frame, error) {
	sample := records
	if m := opts.Limits.MaxRows; m > 0 && len(sample) > m {
		sample = sample[:m]
	}
	inf := newInferrer(names, opts.FieldTypes)
	types := inf.types()
	cols := make([]column, len(names))
	var total int64
	stopped := false
	row := make([]any, len(names))
	bad := make([]bool, len(names))
	changed := make(map[int]column)
	for n, rec := range sample {
		if n%cancelCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		next := inf.peek(rec)
		clear(changed)
		size := total
		for i := range next {
			if next[i] == types[i] {
				continue
			}
			c, err := convertColumn(ctx, next[i], sample[:n], i)
			if err != nil {
				return nil, err
			}
			changed[i] = c
			size += c.bytes - cols[i].bytes
		}
		for i, cell := range rec {
			v, ok := convert(next[i], cell)
			row[i], bad[i] = v, !ok
			size += int64(frame.EstimateValueSize(v))
		}
		if opts.Limits.MaxBytes > 0 && size > opts.Limits.MaxBytes {
			stopped = true
			break
		}
		for i, c := range changed {
			cols[i] = c
		}
		for i, v := range row {
			cols[i].vals = append(cols[i].vals, v)
			if bad[i] {
				cols[i].failed++
			}
		}
		for i, v := range row {
			cols[i].bytes += int64(frame.EstimateValueSize(v))
		}
		total, types = size, next
		inf.commit(rec)
	}
	return assemble(ctx, names, types, cols, records, len(sample), stopped, opts.Limits)
}

// column holds one column's converted values for the retained prefix, their estimated
// size and how many cells failed conversion (only possible under a fieldTypes override).
type column struct {
	vals   []any
	bytes  int64
	failed int
}

// convertColumn converts column i of rows under type t.
func convertColumn(ctx context.Context, t frame.FieldType, rows [][]string, i int) (column, error) {
	c := column{vals: make([]any, len(rows))}
	for n, rec := range rows {
		if n%cancelCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return column{}, err
			}
		}
		v, ok := convert(t, rec[i])
		if !ok {
			c.failed++
		}
		c.vals[n] = v
		c.bytes += int64(frame.EstimateValueSize(v))
	}
	return c, nil
}

// assemble builds the frame from the planned columns. Every planned row fits the limits
// by construction; the builder still enforces them. When the sample was complete and an
// overflow record exists, appending it lets the builder flag the row limit.
func assemble(ctx context.Context, names []string, types []frame.FieldType, cols []column, records [][]string, sampled int, stopped bool, lim frame.Limits) (*frame.Frame, error) {
	fields := make([]*frame.Field, len(names))
	for i, name := range names {
		fld, err := frame.NewField(name, types[i])
		if err != nil {
			return nil, err
		}
		fields[i] = fld
	}
	b, err := frame.NewBuilder("", lim, fields...)
	if err != nil {
		return nil, err
	}
	rows := 0
	if len(cols) > 0 {
		rows = len(cols[0].vals)
	}
	row := make([]any, len(names))
	for n := range rows {
		if n%cancelCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		for i := range cols {
			row[i] = cols[i].vals[n]
		}
		if err := b.AppendRow(row...); errors.Is(err, frame.ErrLimitReached) {
			break
		} else if err != nil {
			return nil, err
		}
	}
	fr := b.Frame()
	switch {
	case stopped && !fr.Meta.Truncated:
		fr.Meta.Truncated = true
		fr.AddNotice(frame.SeverityWarning, fmt.Sprintf("Result truncated at the byte limit of %d bytes.", lim.MaxBytes))
	case !stopped && len(records) > sampled:
		for i, cell := range records[sampled] {
			row[i], _ = convert(types[i], cell)
		}
		if err := b.AppendRow(row...); err != nil && !errors.Is(err, frame.ErrLimitReached) {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i, c := range cols {
		if c.failed > 0 {
			noun := "values"
			if c.failed == 1 {
				noun = "value"
			}
			fr.AddNotice(frame.SeverityWarning, fmt.Sprintf("%d %s in field %q could not be read as %s and were set to null.", c.failed, noun, names[i], types[i]))
		}
	}
	return fr, nil
}

// inferrer tracks, per column, the types every non-empty cell seen so far parses as.
// Columns named in overrides keep their override.
type inferrer struct {
	fixed []frame.FieldType // override, or "" when inferred
	cands [][]frame.FieldType
	seen  []bool
}

func newInferrer(names []string, overrides map[string]frame.FieldType) *inferrer {
	in := &inferrer{fixed: make([]frame.FieldType, len(names)), cands: make([][]frame.FieldType, len(names)), seen: make([]bool, len(names))}
	for i, name := range names {
		in.fixed[i] = overrides[name]
		in.cands[i] = []frame.FieldType{frame.TypeBool, frame.TypeNumber, frame.TypeTime}
	}
	return in
}

func pick(fixed frame.FieldType, cands []frame.FieldType, seen bool) frame.FieldType {
	switch {
	case fixed != "":
		return fixed
	case !seen || len(cands) == 0:
		return frame.TypeString
	default:
		return cands[0]
	}
}

func narrow(cands []frame.FieldType, cell string) []frame.FieldType {
	kept := make([]frame.FieldType, 0, len(cands))
	for _, t := range cands {
		if _, ok := convert(t, cell); ok {
			kept = append(kept, t)
		}
	}
	return kept
}

// types returns the current type of every column.
func (in *inferrer) types() []frame.FieldType {
	out := make([]frame.FieldType, len(in.cands))
	for i := range out {
		out[i] = pick(in.fixed[i], in.cands[i], in.seen[i])
	}
	return out
}

// peek returns the column types that would result from also seeing rec.
func (in *inferrer) peek(rec []string) []frame.FieldType {
	out := make([]frame.FieldType, len(in.cands))
	for i := range out {
		cell := strings.TrimSpace(rec[i])
		if in.fixed[i] != "" || cell == "" {
			out[i] = pick(in.fixed[i], in.cands[i], in.seen[i])
			continue
		}
		out[i] = pick("", narrow(in.cands[i], cell), true)
	}
	return out
}

// commit records rec as part of the retained sample.
func (in *inferrer) commit(rec []string) {
	for i := range in.cands {
		cell := strings.TrimSpace(rec[i])
		if in.fixed[i] != "" || cell == "" {
			continue
		}
		in.cands[i] = narrow(in.cands[i], cell)
		in.seen[i] = true
	}
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
