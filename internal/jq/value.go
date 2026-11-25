// Package jq is a sandboxed, dependency-free evaluator for a documented subset of the jq
// 1.7 language (FR-073, NFR-SEC-021, ADR-0008).
//
// Values are JSON values: nil, bool, json.Number or float64 (numbers), string, []any and
// *Object (objects keep their key order). Evaluation has no I/O, no environment and no
// clock, and every program runs under step, output and depth budgets.
package jq

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Object is a JSON object that keeps the order in which keys were first set.
type Object struct {
	keys []string
	m    map[string]any
}

// NewObject returns an empty object with room for n keys.
func NewObject(n int) *Object { return &Object{keys: make([]string, 0, n), m: make(map[string]any, n)} }

// Set sets key to v, keeping the key's original position when it already exists.
func (o *Object) Set(key string, v any) {
	if _, ok := o.m[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.m[key] = v
}

// Get returns the value of key.
func (o *Object) Get(key string) (any, bool) {
	v, ok := o.m[key]
	return v, ok
}

// Keys returns the keys in insertion order. The slice must not be modified.
func (o *Object) Keys() []string { return o.keys }

// Len returns the number of keys.
func (o *Object) Len() int { return len(o.keys) }

func (o *Object) clone() *Object {
	c := NewObject(len(o.keys))
	for _, k := range o.keys {
		c.Set(k, o.m[k])
	}
	return c
}

func (o *Object) sortedKeys() []string {
	ks := append([]string(nil), o.keys...)
	sort.Strings(ks)
	return ks
}

// ErrDepth is returned when a value nests deeper than the allowed depth.
var ErrDepth = errors.New("value nests too deeply")

// Decode reads exactly one JSON value from r. Objects keep key order, numbers keep their
// text (json.Number), and nesting deeper than maxDepth is refused (NFR-SEC-021 AC5).
func Decode(r io.Reader, maxDepth int) (any, error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	v, err := decodeValue(dec, 0, maxDepth)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after the JSON value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder, depth, maxDepth int) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		if depth >= maxDepth {
			return nil, fmt.Errorf("%w (more than %d levels)", ErrDepth, maxDepth)
		}
		switch t {
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := decodeValue(dec, depth+1, maxDepth)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return arr, nil
		case '{':
			obj := NewObject(4)
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				v, err := decodeValue(dec, depth+1, maxDepth)
				if err != nil {
					return nil, err
				}
				obj.Set(k, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return obj, nil
		}
		return nil, fmt.Errorf("unexpected %q", t)
	default:
		return t, nil // nil, bool, string, json.Number
	}
}

// Marshal encodes v as compact JSON, refusing nesting deeper than maxDepth.
func Marshal(v any, maxDepth int) ([]byte, error) {
	var b bytes.Buffer
	if err := encode(&b, v, 0, maxDepth); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// AppendJSON appends the compact JSON encoding of v to b.
func AppendJSON(b *bytes.Buffer, v any, maxDepth int) error { return encode(b, v, 0, maxDepth) }

func encode(b *bytes.Buffer, v any, depth, maxDepth int) error {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case json.Number:
		b.WriteString(normalizeNumberText(t))
	case float64:
		b.WriteString(formatFloat(t))
	case string:
		writeString(b, t)
	case []any:
		if depth >= maxDepth {
			return fmt.Errorf("%w (more than %d levels)", ErrDepth, maxDepth)
		}
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := encode(b, e, depth+1, maxDepth); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case *Object:
		if depth >= maxDepth {
			return fmt.Errorf("%w (more than %d levels)", ErrDepth, maxDepth)
		}
		b.WriteByte('{')
		for i, k := range t.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeString(b, k)
			b.WriteByte(':')
			if err := encode(b, t.m[k], depth+1, maxDepth); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("unsupported value type %T", v)
	}
	return nil
}

// normalizeNumberText returns valid JSON for a json.Number (it is already JSON text when
// it came from the decoder; numbers built by tonumber are validated there).
func normalizeNumberText(n json.Number) string {
	if n == "" {
		return "0"
	}
	if _, err := strconv.ParseFloat(string(n), 64); err != nil {
		return formatFloat(toFloat(n)) // out of range: saturate like jq
	}
	return string(n)
}

func writeString(b *bytes.Buffer, s string) {
	var tmp bytes.Buffer
	enc := json.NewEncoder(&tmp)
	enc.SetEscapeHTML(false) // jq prints <, > and & as they are
	_ = enc.Encode(s)        // strings always encode
	b.Write(bytes.TrimSuffix(tmp.Bytes(), []byte("\n")))
}

// formatFloat prints numbers like jq 1.7: integers without a fraction, otherwise the
// shortest representation; NaN as null and infinities as the largest double.
func formatFloat(f float64) string {
	switch {
	case math.IsNaN(f):
		return "null"
	case math.IsInf(f, 1):
		return "1.7976931348623157e+308"
	case math.IsInf(f, -1):
		return "-1.7976931348623157e+308"
	case f == math.Trunc(f) && math.Abs(f) < 1e17:
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// typeName is jq's `type`.
func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case json.Number, float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case *Object:
		return "object"
	}
	return "unknown"
}

func isNumber(v any) bool {
	switch v.(type) {
	case json.Number, float64:
		return true
	}
	return false
}

// toFloat converts a number value to float64.
func toFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case json.Number:
		f, err := strconv.ParseFloat(string(t), 64)
		if err != nil {
			// Out-of-range literals saturate like jq.
			if strings.HasPrefix(string(t), "-") {
				return math.Inf(-1)
			}
			return math.Inf(1)
		}
		return f
	}
	return 0
}

func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	}
	return true
}

// typeOrder is jq's sort order of types.
func typeOrder(v any) int {
	switch t := v.(type) {
	case nil:
		return 0
	case bool:
		if t {
			return 2
		}
		return 1
	case json.Number, float64:
		return 3
	case string:
		return 4
	case []any:
		return 5
	case *Object:
		return 6
	}
	return 7
}

// compare orders values like jq: null < false < true < numbers < strings < arrays <
// objects; arrays lexicographically; objects by sorted key set, then values.
func compare(a, b any) int {
	ta, tb := typeOrder(a), typeOrder(b)
	if ta != tb {
		return cmpInt(ta, tb)
	}
	switch x := a.(type) {
	case json.Number, float64:
		if na, ok := a.(json.Number); ok {
			if nb, ok := b.(json.Number); ok && na == nb {
				return 0
			}
			if ia, err := na.Int64(); err == nil {
				if nb, ok := b.(json.Number); ok {
					if ib, err := nb.Int64(); err == nil {
						return cmpInt64(ia, ib)
					}
				}
			}
		}
		fa, fb := toFloat(a), toFloat(b)
		switch {
		case fa < fb:
			return -1
		case fa > fb:
			return 1
		}
		return 0
	case string:
		return strings.Compare(x, b.(string))
	case []any:
		y := b.([]any)
		for i := 0; i < len(x) && i < len(y); i++ {
			if c := compare(x[i], y[i]); c != 0 {
				return c
			}
		}
		return cmpInt(len(x), len(y))
	case *Object:
		y := b.(*Object)
		ka, kb := x.sortedKeys(), y.sortedKeys()
		if c := compare(stringsToAny(ka), stringsToAny(kb)); c != 0 {
			return c
		}
		for _, k := range ka {
			if c := compare(x.m[k], y.m[k]); c != 0 {
				return c
			}
		}
	}
	return 0
}

func stringsToAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func cmpInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// preview renders v for an error message, truncated to 40 bytes (FR-073 AC7).
func preview(v any) string {
	b, err := Marshal(v, 64)
	if err != nil {
		return typeName(v)
	}
	s := string(b)
	if len(s) > 40 {
		cut := 37
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + "..."
	}
	return s
}

// describe is jq's "<type> (<value>)" used in error messages.
func describe(v any) string { return typeName(v) + " (" + preview(v) + ")" }
