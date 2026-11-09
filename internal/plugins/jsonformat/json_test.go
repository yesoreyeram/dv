package jsonformat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/plugin"
)

func decode(t *testing.T, f *Format, in string, opts plugin.DecodeOptions) (*frame.Frame, error) {
	t.Helper()
	frames, err := f.Decode(context.Background(), strings.NewReader(in), opts)
	if err != nil {
		return nil, err
	}
	if len(frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(frames))
	}
	if err := frames[0].Validate(); err != nil {
		t.Fatalf("invalid frame: %v", err)
	}
	return frames[0], nil
}

func mustDecode(t *testing.T, f *Format, in string, opts plugin.DecodeOptions) *frame.Frame {
	t.Helper()
	fr, err := decode(t, f, in, opts)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return fr
}

func withOptions(o string) plugin.DecodeOptions {
	return plugin.DecodeOptions{Options: json.RawMessage(o)}
}

func values(t *testing.T, fr *frame.Frame, name string) string {
	t.Helper()
	f := fr.Field(name)
	if f == nil {
		t.Fatalf("no field %q in %v", name, fieldNames(fr))
	}
	parts := make([]string, f.Len())
	for i := range parts {
		switch v := f.At(i).(type) {
		case nil:
			parts[i] = "null"
		case json.RawMessage:
			parts[i] = string(v)
		case time.Time:
			parts[i] = v.Format(time.RFC3339)
		default:
			parts[i] = fmt.Sprint(v)
		}
	}
	return strings.Join(parts, ",")
}

func fieldNames(fr *frame.Frame) []string {
	var out []string
	for _, f := range fr.Fields {
		out = append(out, f.Name+":"+string(f.Type))
	}
	return out
}

func TestMeta(t *testing.T) {
	if m := NewJSON().Meta(); m.ID != "json" || m.Extensions[0] != ".json" {
		t.Fatalf("json meta = %+v", m)
	}
	if m := NewNDJSON().Meta(); m.ID != "ndjson" || len(m.Extensions) != 2 {
		t.Fatalf("ndjson meta = %+v", m)
	}
}

func TestShapesAndTypes(t *testing.T) {
	tests := []struct {
		name, in, opts string
		want           []string
		vals           map[string]string
	}{
		{
			name: "array of objects",
			in:   `[{"t":"2026-11-01T00:00:00Z","h":"a","n":1,"f":0.5,"b":true,"o":{"x": 1},"a":[1, 2]}]`,
			want: []string{"t:time", "h:string", "n:number", "f:number", "b:bool", "o:json", "a:json"},
			vals: map[string]string{"o": `{"x":1}`, "a": "[1,2]", "t": "2026-11-01T00:00:00Z"},
		},
		{name: "single object", in: ` {"a": 1} `, want: []string{"a:number"}},
		{name: "scalars", in: `[1, "x", null]`, want: []string{"value:string"}, vals: map[string]string{"value": "1,x,null"}},
		{name: "top-level scalar", in: `42`, want: []string{"value:number"}},
		{name: "mixed object and scalar rows", in: `[{"a":1}, 2]`, want: []string{"a:number", "value:number"}, vals: map[string]string{"a": "1,null", "value": "null,2"}},
		{name: "empty array", in: `[]`, want: nil},
		{name: "all null", in: `[{"a":null}]`, want: []string{"a:string"}},
		{name: "time then text", in: `[{"a":"2026-01-01"},{"a":"soon"}]`, want: []string{"a:string"}},
		{name: "text then time", in: `[{"a":"soon"},{"a":"2026-01-01"}]`, want: []string{"a:string"}},
		{name: "mixed kinds", in: `[{"a":1},{"a":"x"},{"a":false},{"a":[1]}]`, want: []string{"a:string"}, vals: map[string]string{"a": "1,x,false,[1]"}},
		{name: "big int", in: `[{"a":9007199254740993}]`, want: []string{"a:number"}, vals: map[string]string{"a": "9007199254740993"}},
		{name: "duplicate key keeps last", in: `[{"a":1,"a":2}]`, want: []string{"a:number"}, vals: map[string]string{"a": "2"}},
		{name: "empty key", in: `[{"":1,"b":2}]`, want: []string{"Column 1:number", "b:number"}},
		{name: "BOM", in: "\xEF\xBB\xBF[{\"a\":1}]", want: []string{"a:number"}},
		{name: "unicode escapes", in: `[{"é":"é"}]`, want: []string{"é:string"}},
		{name: "rowsPath", in: `{"x":[[1],{"y":2}],"d":{"skip":true,"rows":[{"a":1}]},"z":3}`, opts: `{"rowsPath":"d.rows"}`, want: []string{"a:number"}},
		{name: "rowsPath first match", in: `{"r":[{"a":1}],"r":[{"b":1}]}`, opts: `{"rowsPath":"r"}`, want: []string{"a:number"}},
		{
			name: "flatten", in: `[{"h":{"n":"a","g":{"r":"emea"}},"l":[{"x":1}]}]`, opts: `{"flatten":true}`,
			want: []string{"h.n:string", "h.g.r:string", "l:json"},
		},
		{name: "null options", in: `[1]`, opts: `null`, want: []string{"value:number"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fr := mustDecode(t, NewJSON(), tc.in, withOptions(tc.opts))
			if got := fieldNames(fr); strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("fields = %v, want %v", got, tc.want)
			}
			for name, want := range tc.vals {
				if got := values(t, fr, name); got != want {
					t.Errorf("%s = %s, want %s", name, got, want)
				}
			}
		})
	}
}

func TestFlattenDepthIsBounded(t *testing.T) {
	in := `[` + strings.Repeat(`{"a":`, 12) + `1` + strings.Repeat(`}`, 12) + `]`
	fr := mustDecode(t, NewJSON(), in, withOptions(`{"flatten":true}`))
	if len(fr.Fields) != 1 {
		t.Fatalf("fields = %v", fieldNames(fr))
	}
	f := fr.Fields[0]
	if strings.Count(f.Name, ".") != MaxFlattenDepth-1 || f.Type != frame.TypeJSON {
		t.Fatalf("field = %s %s", f.Name, f.Type)
	}
}

func TestFieldNamesAreTruncatedAndUnique(t *testing.T) {
	long := strings.Repeat("é", frame.MaxFieldNameBytes)
	in := fmt.Sprintf(`[{%q:1,%q:2}]`, long, long+"x")
	fr := mustDecode(t, NewJSON(), in, plugin.DecodeOptions{})
	if len(fr.Fields) != 2 || fr.Fields[0].Name == fr.Fields[1].Name {
		t.Fatalf("fields = %v", fieldNames(fr))
	}
	for _, f := range fr.Fields {
		if len(f.Name) > frame.MaxFieldNameBytes || !strings.HasPrefix(long, strings.TrimSuffix(f.Name, " (2)")) {
			t.Fatalf("bad name %q", f.Name)
		}
	}
	if !strings.HasSuffix(fr.Fields[1].Name, " (2)") {
		t.Fatalf("second name = %q", fr.Fields[1].Name)
	}
	// A real key that equals a generated name is suffixed too.
	fr = mustDecode(t, NewJSON(), `[{"":1,"Column 1":2}]`, plugin.DecodeOptions{})
	if got := fieldNames(fr); strings.Join(got, " ") != "Column 1:number Column 1 (2):number" {
		t.Fatalf("fields = %v", got)
	}
}

func TestNDJSON(t *testing.T) {
	fr := mustDecode(t, NewNDJSON(), "{\"a\":1}\n\n\r\n{\"a\":2,\"b\":\"x\"}\n7\n", plugin.DecodeOptions{})
	if got := values(t, fr, "a"); got != "1,2,null" {
		t.Fatalf("a = %s", got)
	}
	if got := values(t, fr, "value"); got != "null,null,7" {
		t.Fatalf("value = %s", got)
	}
	fr = mustDecode(t, NewNDJSON(), "", plugin.DecodeOptions{})
	if len(fr.Fields) != 0 || fr.Rows() != 0 {
		t.Fatalf("empty ndjson = %v", fieldNames(fr))
	}
}

func TestOverrides(t *testing.T) {
	in := `[{"n":"12","b":"TRUE","t":"2026-01-02","j":"x","s":5},{"n":"x","b":1,"t":3,"j":{"k":1},"s":null}]`
	opts := plugin.DecodeOptions{FieldTypes: map[string]frame.FieldType{
		"n": frame.TypeNumber, "b": frame.TypeBool, "t": frame.TypeTime, "j": frame.TypeJSON, "s": frame.TypeString,
	}}
	fr := mustDecode(t, NewJSON(), in, opts)
	want := map[string]string{"n": "12,null", "b": "true,null", "t": "2026-01-02T00:00:00Z,null", "j": `"x",{"k":1}`, "s": "5,null"}
	for name, w := range want {
		if got := values(t, fr, name); got != w {
			t.Errorf("%s = %s, want %s", name, got, w)
		}
	}
	var texts []string
	for _, n := range fr.Meta.Notices {
		texts = append(texts, n.Text)
	}
	joined := strings.Join(texts, "|")
	for _, f := range []string{`"n"`, `"b"`, `"t"`} {
		if !strings.Contains(joined, "1 value in field "+f) {
			t.Errorf("no notice for %s in %q", f, joined)
		}
	}
	if strings.Contains(joined, `"j"`) || strings.Contains(joined, `"s"`) {
		t.Errorf("unexpected notice in %q", joined)
	}
	_, err := decode(t, NewJSON(), `[]`, plugin.DecodeOptions{FieldTypes: map[string]frame.FieldType{"a": "nope"}})
	if err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("err = %v", err)
	}
}

func TestOutOfRangeNumbersBecomeNull(t *testing.T) {
	fr := mustDecode(t, NewJSON(), `[{"a":1e999},{"a":2}]`, plugin.DecodeOptions{})
	if got := values(t, fr, "a"); got != "null,2" {
		t.Fatalf("a = %s", got)
	}
	if len(fr.Meta.Notices) != 1 || !strings.Contains(fr.Meta.Notices[0].Text, "out of range") {
		t.Fatalf("notices = %+v", fr.Meta.Notices)
	}
}

func TestRowLimitStopsParsing(t *testing.T) {
	opts := plugin.DecodeOptions{Limits: frame.Limits{MaxRows: 2}}
	for _, tc := range []struct {
		f  *Format
		in string
	}{
		{NewJSON(), `[{"a":1},{"a":2},{"a":"x"}, garbage`},
		{NewNDJSON(), "{\"a\":1}\n{\"a\":2}\n{\"a\":\"x\"}\ngarbage"},
		{NewJSON(), `{"d":[{"a":1},{"a":2},{"a":"x"}, garbage`},
	} {
		o := opts
		if strings.HasPrefix(tc.in, `{"d"`) {
			o.Options = json.RawMessage(`{"rowsPath":"d"}`)
		}
		fr := mustDecode(t, tc.f, tc.in, o)
		if fr.Rows() != 2 || !fr.Meta.Truncated || fr.Field("a").Type != frame.TypeNumber {
			t.Fatalf("%s: rows=%d truncated=%v type=%s", tc.f.Meta().ID, fr.Rows(), fr.Meta.Truncated, fr.Field("a").Type)
		}
		if len(fr.Meta.Notices) != 1 || !strings.Contains(fr.Meta.Notices[0].Text, "row limit of 2") {
			t.Fatalf("notices = %+v", fr.Meta.Notices)
		}
	}
	// Exactly MaxRows rows is not truncated.
	fr := mustDecode(t, NewJSON(), `[{"a":1},{"a":2}]`, opts)
	if fr.Meta.Truncated {
		t.Fatal("exact fit marked truncated")
	}
}

func TestByteLimitUsesKeptRowsOnly(t *testing.T) {
	// Two numbers cost 2 x 24 bytes; the third row ("x") would turn the field into a
	// string and does not fit, so it must not change the type.
	opts := plugin.DecodeOptions{Limits: frame.Limits{MaxBytes: 60}}
	fr := mustDecode(t, NewJSON(), `[{"v":1},{"v":2},{"v":"x"},{"v":3}]`, opts)
	if fr.Rows() != 2 || fr.Field("v").Type != frame.TypeNumber || !fr.Meta.Truncated {
		t.Fatalf("rows=%d type=%s truncated=%v", fr.Rows(), fr.Field("v").Type, fr.Meta.Truncated)
	}
	if !strings.Contains(fr.Meta.Notices[0].Text, "byte limit of 60") {
		t.Fatalf("notices = %+v", fr.Meta.Notices)
	}
	// Time strings are counted as times once inferred: three times (40 bytes each) fit
	// 130 bytes although the strings would not (3 x 52).
	opts = plugin.DecodeOptions{Limits: frame.Limits{MaxBytes: 130}}
	fr = mustDecode(t, NewJSON(), `["2026-01-01T00:00:00Z","2026-01-02T00:00:00Z","2026-01-03T00:00:00Z"]`, opts)
	if fr.Rows() != 3 || fr.Field("value").Type != frame.TypeTime || fr.Meta.Truncated {
		t.Fatalf("rows=%d type=%s truncated=%v", fr.Rows(), fr.Field("value").Type, fr.Meta.Truncated)
	}
	// New columns count the earlier rows as nulls.
	opts = plugin.DecodeOptions{Limits: frame.Limits{MaxBytes: 24 * 3}}
	fr = mustDecode(t, NewJSON(), `[{"a":1},{"a":2},{"b":3}]`, opts)
	if fr.Rows() != 2 || len(fr.Fields) != 1 {
		t.Fatalf("rows=%d fields=%v", fr.Rows(), fieldNames(fr))
	}
	// A first row that does not fit gives an empty, truncated frame.
	opts = plugin.DecodeOptions{Limits: frame.Limits{MaxBytes: 10}}
	fr = mustDecode(t, NewJSON(), `[{"a":1}]`, opts)
	if fr.Rows() != 0 || !fr.Meta.Truncated {
		t.Fatalf("rows=%d truncated=%v", fr.Rows(), fr.Meta.Truncated)
	}
	// Under an override the converted size counts.
	opts = plugin.DecodeOptions{Limits: frame.Limits{MaxBytes: 48}, FieldTypes: map[string]frame.FieldType{"v": frame.TypeNumber}}
	fr = mustDecode(t, NewJSON(), `[{"v":"1"},{"v":"2"},{"v":"3"}]`, opts)
	if fr.Rows() != 2 || values(t, fr, "v") != "1,2" {
		t.Fatalf("rows=%d v=%s", fr.Rows(), values(t, fr, "v"))
	}
	// Mixed kinds are sized as strings.
	opts = plugin.DecodeOptions{Limits: frame.Limits{MaxBytes: 33 * 2}}
	fr = mustDecode(t, NewJSON(), `[1,"x",true]`, opts)
	if fr.Rows() != 2 || fr.Field("value").Type != frame.TypeString {
		t.Fatalf("rows=%d type=%s", fr.Rows(), fr.Field("value").Type)
	}
}

func TestDecodeErrors(t *testing.T) {
	tests := []struct {
		name, in, opts, want string
		f                    *Format
	}{
		{name: "empty", in: "  ", want: "empty"},
		{name: "syntax", in: `[{"k": s3cr3t}]`, want: "offset"},
		{name: "syntax top", in: `s3cr3t`, want: "offset"},
		{name: "trailing", in: `[] {"k":"s3cr3t"}`, want: "trailing"},
		{name: "trailing after object", in: `{"a":1} x`, want: "offset"},
		{name: "trailing after rowsPath", in: `{"r":[]} x`, opts: `{"rowsPath":"r"}`, want: "trailing"},
		{name: "broken after rowsPath", in: `{"r":[], "s": s3cr3t}`, opts: `{"rowsPath":"r"}`, want: "offset"},
		{name: "truncated", in: `[{"a":1}`, want: "end of input"},
		{name: "rowsPath missing", in: `{"d":{}}`, opts: `{"rowsPath":"d.rows"}`, want: `"d.rows"`},
		{name: "rowsPath not array", in: `{"d":{"rows":{"s3cr3t":1}}}`, opts: `{"rowsPath":"d.rows"}`, want: "not an array"},
		{name: "rowsPath through non-object", in: `{"d":[1]}`, opts: `{"rowsPath":"d.rows"}`, want: "not an object"},
		{name: "rowsPath on array", in: `[1]`, opts: `{"rowsPath":"d"}`, want: "not an object"},
		{name: "deep row", in: strings.Repeat("[", 70) + strings.Repeat("]", 70), want: "nesting"},
		{name: "deep skipped value", in: `{"x":` + strings.Repeat("[", 70) + strings.Repeat("]", 70) + `,"r":[]}`, opts: `{"rowsPath":"r"}`, want: "nesting"},
		{name: "ndjson syntax", f: NewNDJSON(), in: "{\"ok\":1}\n{\"k\": s3cr3t}", want: "row 2"},
		{name: "ndjson truncated", f: NewNDJSON(), in: "{\"ok\":1}\n{\"k\":", want: "row 2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.f
			if f == nil {
				f = NewJSON()
			}
			_, err := decode(t, f, tc.in, withOptions(tc.opts))
			if !errors.Is(err, plugin.ErrDecode) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want decode error mentioning %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "s3cr3t") {
				t.Fatalf("error echoes content: %v", err)
			}
		})
	}
}

func TestTooManyFields(t *testing.T) {
	var b strings.Builder
	b.WriteString("[{")
	for i := range MaxFields + 1 {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"k%d":1`, i)
	}
	b.WriteString("}]")
	_, err := decode(t, NewJSON(), b.String(), plugin.DecodeOptions{})
	if !errors.Is(err, plugin.ErrDecode) || !strings.Contains(err.Error(), "1024") {
		t.Fatalf("err = %v", err)
	}
	// Exactly MaxFields across rows is fine; one more in a later row fails.
	rows := make([]string, 0, MaxFields+1)
	for i := range MaxFields {
		rows = append(rows, fmt.Sprintf(`{"k%d":1}`, i))
	}
	fr := mustDecode(t, NewJSON(), "["+strings.Join(rows, ",")+"]", plugin.DecodeOptions{})
	if len(fr.Fields) != MaxFields {
		t.Fatalf("fields = %d", len(fr.Fields))
	}
	rows = append(rows, `{"extra":1}`)
	if _, err := decode(t, NewJSON(), "["+strings.Join(rows, ",")+"]", plugin.DecodeOptions{}); !errors.Is(err, plugin.ErrDecode) {
		t.Fatalf("err = %v", err)
	}
}

func TestOptionErrors(t *testing.T) {
	for _, tc := range []struct {
		f    *Format
		opts string
	}{
		{NewJSON(), `{"rowsPath":"a..b"}`},
		{NewJSON(), `{"rowsPath":"a[0]"}`},
		{NewJSON(), `{"rowsPath":"$.a"}`},
		{NewJSON(), `{"rowsPath":"` + strings.Repeat("a.", maxPathSegments) + `a"}`},
		{NewJSON(), `{"rowsPath":"` + strings.Repeat("a", 129) + `"}`},
		{NewJSON(), `{"unknown":true}`},
		{NewJSON(), `{"flatten":"yes"}`},
		{NewJSON(), `[`},
		{NewNDJSON(), `{"rowsPath":"a"}`},
	} {
		_, err := decode(t, tc.f, `[]`, withOptions(tc.opts))
		if !errors.Is(err, plugin.ErrOptions) {
			t.Errorf("%s %s: err = %v", tc.f.Meta().ID, tc.opts, err)
		}
	}
}

var errRead = errors.New("read failed")

func TestReaderErrorsPassThrough(t *testing.T) {
	r := io.MultiReader(strings.NewReader(`[{"a":1},`), iotest.ErrReader(errRead))
	_, err := NewJSON().Decode(context.Background(), r, plugin.DecodeOptions{})
	if !errors.Is(err, errRead) || errors.Is(err, plugin.ErrDecode) {
		t.Fatalf("err = %v", err)
	}
	_, err = NewJSON().Decode(context.Background(), iotest.ErrReader(errRead), plugin.DecodeOptions{})
	if !errors.Is(err, errRead) {
		t.Fatalf("err = %v", err)
	}
	_, err = NewNDJSON().Decode(context.Background(), io.MultiReader(strings.NewReader("{}\n"), iotest.ErrReader(errRead)), plugin.DecodeOptions{})
	if !errors.Is(err, errRead) {
		t.Fatalf("ndjson err = %v", err)
	}
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, f := range []*Format{NewJSON(), NewNDJSON()} {
		_, err := f.Decode(ctx, strings.NewReader(`[1]`), plugin.DecodeOptions{})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%s: err = %v", f.Meta().ID, err)
		}
	}
}

func TestDepth(t *testing.T) {
	for in, want := range map[string]int{`1`: 0, `[]`: 1, `[{"a":[1]}]`: 3, `["[[[","\"[["]`: 1, `{"a\\\\":"]"}`: 1} {
		if got := depth([]byte(in)); got != want {
			t.Errorf("depth(%s) = %d, want %d", in, got, want)
		}
	}
}

func FuzzDecode(f *testing.F) {
	for _, s := range []string{`[{"a":1}]`, `{"d":{"r":[1,"x"]}}`, "{\"a\":1}\n{\"b\":[2]}", `[[[`, `"x"`} {
		f.Add(s, false)
		f.Add(s, true)
	}
	f.Fuzz(func(t *testing.T, in string, nd bool) {
		fm := NewJSON()
		if nd {
			fm = NewNDJSON()
		}
		frames, err := fm.Decode(context.Background(), strings.NewReader(in), plugin.DecodeOptions{
			Limits: frame.Limits{MaxRows: 50, MaxBytes: 1 << 16}, Options: json.RawMessage(`{"flatten":true}`),
		})
		if err != nil {
			if !errors.Is(err, plugin.ErrDecode) {
				t.Fatalf("non-decode error: %v", err)
			}
			return
		}
		if err := frames[0].Validate(); err != nil {
			t.Fatalf("invalid frame: %v", err)
		}
	})
}
