package csvformat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/plugin"
)

func decode(t *testing.T, f *Format, in string, opts plugin.DecodeOptions) *frame.Frame {
	t.Helper()
	frames, err := f.Decode(context.Background(), strings.NewReader(in), opts)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("frames = %d", len(frames))
	}
	return frames[0]
}

func types(fr *frame.Frame) string {
	parts := make([]string, len(fr.Fields))
	for i, f := range fr.Fields {
		parts[i] = f.Name + ":" + string(f.Type)
	}
	return strings.Join(parts, ",")
}

func TestMeta(t *testing.T) {
	if m := NewCSV().Meta(); m.ID != "csv" || m.Extensions[0] != ".csv" {
		t.Fatalf("%+v", m)
	}
	if m := NewTSV().Meta(); m.ID != "tsv" || len(m.Extensions) != 2 {
		t.Fatalf("%+v", m)
	}
}

func TestInference(t *testing.T) {
	in := "\xef\xbb\xbftime,date,n,f,b,s,empty,mixed,nan\n" +
		"2026-11-01T10:00:00+02:00,2026-11-01,1,1.5,TRUE,x,,1,NaN\n" +
		"2026-11-02T10:00:00Z,2026-11-02 03:04:05,-2,2,false,y,,true,1\n" +
		",,,,,,,,\n"
	fr := decode(t, NewCSV(), in, plugin.DecodeOptions{})
	want := "time:time,date:time,n:number,f:number,b:bool,s:string,empty:string,mixed:string,nan:string"
	if got := types(fr); got != want {
		t.Fatalf("types\n got %s\nwant %s", got, want)
	}
	if fr.Rows() != 3 {
		t.Fatalf("rows = %d", fr.Rows())
	}
	if v := fr.Field("time").At(0).(time.Time); !v.Equal(time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)) || v.Location() != time.UTC {
		t.Fatalf("time = %v", v)
	}
	if v := fr.Field("n").At(1); v != int64(-2) {
		t.Fatalf("int = %#v", v)
	}
	if v := fr.Field("f").At(0); v != 1.5 {
		t.Fatalf("float = %#v", v)
	}
	if v := fr.Field("b").At(0); v != true {
		t.Fatalf("bool = %#v", v)
	}
	for _, name := range []string{"time", "n", "b", "empty"} {
		if fr.Field(name).At(2) != nil {
			t.Fatalf("%s: empty cell should be null", name)
		}
	}
}

func TestNumberEdgeCases(t *testing.T) {
	for _, c := range []string{"Inf", "-infinity", "0x10", "1_000", "1e999"} {
		if _, ok := convert(frame.TypeNumber, c); ok {
			t.Errorf("%q should not be a number", c)
		}
	}
	if v, ok := convert(frame.TypeNumber, "1e3"); !ok || v != 1000.0 {
		t.Errorf("1e3 = %v %v", v, ok)
	}
	if v, ok := convert(frame.TypeNumber, "99999999999999999999"); !ok || v != 1e20 {
		t.Errorf("big = %v %v", v, ok)
	}
	if v, ok := convert(frame.TypeJSON, `{"a":1}`); !ok || string(v.(json.RawMessage)) != `{"a":1}` {
		t.Errorf("json = %v %v", v, ok)
	}
	if _, ok := convert(frame.TypeJSON, `{`); ok {
		t.Error("invalid json accepted")
	}
	if v, ok := convert(frame.TypeString, "  padded "); !ok || v != "  padded " {
		t.Errorf("strings keep their whitespace: %q", v)
	}
}

func TestHeaderNames(t *testing.T) {
	long := strings.Repeat("é", 200)
	fr := decode(t, NewCSV(), "a, a ,,a,"+long+"\n1,2,3,4,5\n", plugin.DecodeOptions{})
	names := []string{}
	for _, f := range fr.Fields {
		names = append(names, f.Name)
	}
	got := strings.Join(names[:4], "|")
	if got != "a|a (2)|Column 3|a (3)" {
		t.Fatalf("names = %s", got)
	}
	if len(names[4]) > frame.MaxFieldNameBytes || !strings.HasPrefix(names[4], "é") {
		t.Fatalf("long name = %d bytes", len(names[4]))
	}
}

func TestNoHeaderAndOptions(t *testing.T) {
	opts := plugin.DecodeOptions{Options: json.RawMessage(`{"header":false,"delimiter":";","comment":"#"}`)}
	fr := decode(t, NewCSV(), "# comment\n1;x\n2;y\n", opts)
	if types(fr) != "Column 1:number,Column 2:string" || fr.Rows() != 2 {
		t.Fatalf("%s rows %d", types(fr), fr.Rows())
	}
	fr = decode(t, NewCSV(), "a\n1\n", plugin.DecodeOptions{Options: json.RawMessage(`null`)})
	if fr.Rows() != 1 {
		t.Fatal("null options")
	}
}

func TestTSVQuotesAreLiteral(t *testing.T) {
	fr := decode(t, NewTSV(), "name\tsize\n5\" disk\t1\n", plugin.DecodeOptions{})
	if fr.Field("name").At(0) != `5" disk` || fr.Field("size").At(0) != int64(1) {
		t.Fatalf("%v %v", fr.Field("name").At(0), fr.Field("size").At(0))
	}
}

func TestOverrides(t *testing.T) {
	opts := plugin.DecodeOptions{FieldTypes: map[string]frame.FieldType{"code": frame.TypeNumber, "id": frame.TypeString}}
	fr := decode(t, NewCSV(), "code,id\n10,1\nn/a,2\nx,3\n", opts)
	if types(fr) != "code:number,id:string" {
		t.Fatal(types(fr))
	}
	if fr.Field("code").At(1) != nil || fr.Field("id").At(0) != "1" {
		t.Fatal("override values")
	}
	if len(fr.Meta.Notices) != 1 || !strings.Contains(fr.Meta.Notices[0].Text, "2 values in field \"code\"") {
		t.Fatalf("notices = %+v", fr.Meta.Notices)
	}
	fr = decode(t, NewCSV(), "code\nx\n", opts)
	if !strings.Contains(fr.Meta.Notices[0].Text, "1 value in") {
		t.Fatalf("singular notice = %+v", fr.Meta.Notices)
	}
	_, err := NewCSV().Decode(context.Background(), strings.NewReader("a\n1\n"), plugin.DecodeOptions{FieldTypes: map[string]frame.FieldType{"a": "blob"}})
	if err == nil || !strings.Contains(err.Error(), "blob") {
		t.Fatalf("err = %v", err)
	}
}

func rows(n int) string {
	var b strings.Builder
	b.WriteString("v\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%d\n", i)
	}
	return b.String()
}

func TestRowLimit(t *testing.T) {
	fr := decode(t, NewCSV(), rows(50), plugin.DecodeOptions{Limits: frame.Limits{MaxRows: 10}})
	if fr.Rows() != 10 || !fr.Meta.Truncated || !strings.Contains(fr.Meta.Notices[0].Text, "row limit") {
		t.Fatalf("rows %d meta %+v", fr.Rows(), fr.Meta)
	}
	fr = decode(t, NewCSV(), rows(10), plugin.DecodeOptions{Limits: frame.Limits{MaxRows: 10}})
	if fr.Rows() != 10 || fr.Meta.Truncated {
		t.Fatalf("exactly at limit: rows %d truncated %v", fr.Rows(), fr.Meta.Truncated)
	}
}

func TestByteLimit(t *testing.T) {
	// Raw estimate per row is len+16 = 17..18 bytes, so 100 bytes holds 5 rows.
	fr := decode(t, NewCSV(), rows(50), plugin.DecodeOptions{Limits: frame.Limits{MaxBytes: 100}})
	if fr.Rows() != 5 || !fr.Meta.Truncated || !strings.Contains(fr.Meta.Notices[0].Text, "byte limit") {
		t.Fatalf("rows %d meta %+v", fr.Rows(), fr.Meta)
	}
	// Strings are estimated the same way by the builder, so it stops first.
	fr = decode(t, NewCSV(), "s\naaaa\nbbbb\ncccc\n", plugin.DecodeOptions{Limits: frame.Limits{MaxBytes: 41}})
	if fr.Rows() != 2 || len(fr.Meta.Notices) != 1 {
		t.Fatalf("rows %d notices %+v", fr.Rows(), fr.Meta.Notices)
	}
}

func TestDecodeErrors(t *testing.T) {
	cases := map[string]struct {
		in, opts, want string
	}{
		"ragged":          {"a,b\n1,2\n3\n", "", "line 3"},
		"bare quote":      {"a\nx\"y\n", "", "line 2"},
		"empty":           {"", "", "empty"},
		"unknown option":  {"a\n", `{"quote":"'"}`, "unknown field"},
		"bad delimiter":   {"a\n", `{"delimiter":"ab"}`, "delimiter"},
		"quote delimiter": {"a\n", `{"delimiter":"\""}`, "delimiter"},
		"bad comment":     {"a\n", `{"comment":"\n"}`, "comment"},
		"same chars":      {"a\n", `{"delimiter":"#","comment":"#"}`, "differ"},
		"too wide":        {strings.Repeat("a,", MaxColumns) + "a\n", "", "columns"},
	}
	for name, c := range cases {
		opts := plugin.DecodeOptions{}
		if c.opts != "" {
			opts.Options = json.RawMessage(c.opts)
		}
		_, err := NewCSV().Decode(context.Background(), strings.NewReader(c.in), opts)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
	_, err := NewCSV().Decode(context.Background(), strings.NewReader("a,b\n1\n"), plugin.DecodeOptions{})
	if !errors.Is(err, ErrDecode) || !strings.Contains(err.Error(), "wrong number of columns") {
		t.Fatalf("err = %v", err)
	}
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewCSV().Decode(ctx, strings.NewReader(rows(5)), plugin.DecodeOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("disk on fire") }

func TestReaderError(t *testing.T) {
	if _, err := NewCSV().Decode(context.Background(), failReader{}, plugin.DecodeOptions{}); err == nil || !strings.Contains(err.Error(), "disk on fire") {
		t.Fatalf("err = %v", err)
	}
}

func TestBadLimits(t *testing.T) {
	if _, err := NewCSV().Decode(context.Background(), strings.NewReader("a\n1\n"), plugin.DecodeOptions{Limits: frame.Limits{MaxRows: -1}}); err == nil {
		t.Fatal("negative limits accepted")
	}
}

func FuzzDecode(f *testing.F) {
	for _, s := range []string{"a,b\n1,2\n", "\"x\ny\",1\n", "a\n\"", ",,,\n,,,\n", "t\n2026-01-01\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		frames, err := NewCSV().Decode(context.Background(), strings.NewReader(in), plugin.DecodeOptions{Limits: frame.Limits{MaxRows: 100, MaxBytes: 1 << 16}})
		if err != nil {
			return
		}
		if err := frames[0].Validate(); err != nil {
			t.Fatalf("invalid frame: %v", err)
		}
		if _, err := json.Marshal(frames[0]); err != nil {
			t.Fatalf("marshal: %v", err)
		}
	})
}
