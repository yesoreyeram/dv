package frame

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func mustField(t *testing.T, name string, typ FieldType) *Field {
	t.Helper()
	f, err := NewField(name, typ)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestFieldTypeValid(t *testing.T) {
	for _, ft := range []FieldType{TypeTime, TypeNumber, TypeString, TypeBool, TypeJSON} {
		if !ft.Valid() {
			t.Errorf("%s should be valid", ft)
		}
	}
	if FieldType("int").Valid() {
		t.Error("int should be invalid")
	}
}

func TestNewFieldValidation(t *testing.T) {
	if _, err := NewField("", TypeString); err == nil {
		t.Error("empty name accepted")
	}
	if _, err := NewField(strings.Repeat("x", MaxFieldNameBytes+1), TypeString); err == nil {
		t.Error("long name accepted")
	}
	if _, err := NewField(strings.Repeat("x", MaxFieldNameBytes), TypeString); err != nil {
		t.Errorf("max-length name rejected: %v", err)
	}
	if _, err := NewField("a", "decimal"); err == nil {
		t.Error("bad type accepted")
	}
}

func TestFieldAppendNormalisation(t *testing.T) {
	loc := time.FixedZone("x", 3600)
	ts := time.Date(2026, 11, 7, 11, 0, 0, 0, loc)
	var nilTime *time.Time
	cases := []struct {
		typ  FieldType
		in   any
		want any
	}{
		{TypeTime, ts, ts.UTC()},
		{TypeTime, &ts, ts.UTC()},
		{TypeTime, nilTime, nil},
		{TypeNumber, 1.5, 1.5},
		{TypeNumber, float32(2.5), 2.5},
		{TypeNumber, int(3), int64(3)},
		{TypeNumber, int8(-4), int64(-4)},
		{TypeNumber, int16(5), int64(5)},
		{TypeNumber, int32(6), int64(6)},
		{TypeNumber, int64(7), int64(7)},
		{TypeNumber, uint8(8), int64(8)},
		{TypeNumber, uint16(9), int64(9)},
		{TypeNumber, uint32(10), int64(10)},
		{TypeNumber, uint(11), int64(11)},
		{TypeNumber, uint64(12), int64(12)},
		{TypeNumber, uint64(math.MaxUint64), float64(math.MaxUint64)},
		{TypeNumber, uint(math.MaxUint), float64(math.MaxUint)},
		{TypeNumber, math.NaN(), nil},
		{TypeNumber, math.Inf(1), nil},
		{TypeNumber, float32(math.Inf(-1)), nil},
		{TypeString, "s", "s"},
		{TypeBool, true, true},
		{TypeJSON, json.RawMessage(`{"a":1}`), json.RawMessage(`{"a":1}`)},
		{TypeString, nil, nil},
	}
	for _, c := range cases {
		f := mustField(t, "f", c.typ)
		if err := f.Append(c.in); err != nil {
			t.Fatalf("%s %T: %v", c.typ, c.in, err)
		}
		if got := f.At(0); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %T: got %#v want %#v", c.typ, c.in, got, c.want)
		}
	}
}

func TestFieldAppendRejectsWrongTypes(t *testing.T) {
	cases := []struct {
		typ FieldType
		in  any
	}{
		{TypeTime, "2026-01-01"},
		{TypeNumber, "1"},
		{TypeString, 1},
		{TypeBool, "true"},
		{TypeJSON, "{}"},
		{TypeJSON, json.RawMessage(`{bad`)},
	}
	for _, c := range cases {
		f := mustField(t, "col", c.typ)
		err := f.Append(c.in)
		if !errors.Is(err, ErrType) || !strings.Contains(err.Error(), `"col"`) {
			t.Errorf("%s %#v: want ErrType naming the field, got %v", c.typ, c.in, err)
		}
		if f.Len() != 0 {
			t.Error("rejected value was stored")
		}
	}
}

func TestJSONValueIsCopied(t *testing.T) {
	raw := json.RawMessage(`[1]`)
	f := mustField(t, "j", TypeJSON)
	if err := f.Append(raw); err != nil {
		t.Fatal(err)
	}
	raw[1] = '2'
	if string(f.At(0).(json.RawMessage)) != "[1]" {
		t.Error("JSON value aliases caller memory")
	}
	vals := f.Values()
	vals[0] = nil
	if f.At(0) == nil {
		t.Error("Values must return a copy")
	}
}

func TestFrameValidate(t *testing.T) {
	a, b := mustField(t, "a", TypeNumber), mustField(t, "b", TypeNumber)
	_ = a.Append(1)
	if _, err := New("x", a, b); !errors.Is(err, ErrLength) {
		t.Errorf("length mismatch: %v", err)
	}
	if _, err := New("x", mustField(t, "a", TypeNumber), mustField(t, "a", TypeString)); err == nil {
		t.Error("duplicate names accepted")
	}
	if _, err := New("x", nil); err == nil {
		t.Error("nil field accepted")
	}
	if _, err := New("x", &Field{Name: "", Type: TypeBool}); err == nil {
		t.Error("empty name accepted")
	}
	if _, err := New("x", &Field{Name: "a", Type: "nope"}); err == nil {
		t.Error("invalid type accepted")
	}
	empty, err := New("empty")
	if err != nil || empty.Rows() != 0 {
		t.Errorf("empty frame: %v", err)
	}
}

func TestAppendRowIsAtomic(t *testing.T) {
	fr, err := New("x", mustField(t, "n", TypeNumber), mustField(t, "s", TypeString))
	if err != nil {
		t.Fatal(err)
	}
	if err := fr.AppendRow(1, "a"); err != nil {
		t.Fatal(err)
	}
	if err := fr.AppendRow(2, 3); !errors.Is(err, ErrType) {
		t.Fatalf("want type error, got %v", err)
	}
	if err := fr.AppendRow(1); !errors.Is(err, ErrLength) {
		t.Fatalf("want length error, got %v", err)
	}
	if fr.Rows() != 1 || fr.Field("n").Len() != 1 || fr.Field("s").Len() != 1 {
		t.Fatal("failed append modified the frame")
	}
	if fr.Field("missing") != nil {
		t.Error("unknown field found")
	}
}

func TestBuilderRowLimit(t *testing.T) {
	b, err := NewBuilder("x", Limits{MaxRows: 2}, mustField(t, "n", TypeNumber))
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if err := b.AppendRow(i); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.AppendRow(3); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("want ErrLimitReached, got %v", err)
	}
	if err := b.AppendRow(4); !errors.Is(err, ErrLimitReached) {
		t.Fatal("builder must stay stopped")
	}
	fr := b.Frame()
	if fr.Rows() != 2 || !fr.Meta.Truncated || len(fr.Meta.Notices) != 1 ||
		fr.Meta.Notices[0].Severity != SeverityWarning || !strings.Contains(fr.Meta.Notices[0].Text, "row limit") {
		t.Fatalf("unexpected frame state: %+v", fr.Meta)
	}
}

func TestBuilderByteLimit(t *testing.T) {
	b, err := NewBuilder("x", Limits{MaxBytes: 40}, mustField(t, "s", TypeString))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AppendRow("0123"); err != nil { // 4+16 = 20 bytes
		t.Fatal(err)
	}
	if err := b.AppendRow("abcd"); err != nil { // 40 bytes total: at the limit, allowed
		t.Fatal(err)
	}
	if err := b.AppendRow("x"); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("want ErrLimitReached, got %v", err)
	}
	if b.Bytes() != 40 || b.Frame().Rows() != 2 || !strings.Contains(b.Frame().Meta.Notices[0].Text, "byte limit") {
		t.Fatalf("bytes=%d rows=%d", b.Bytes(), b.Frame().Rows())
	}
}

func TestBuilderTypeErrorDoesNotTruncate(t *testing.T) {
	b, _ := NewBuilder("x", Limits{MaxRows: 5}, mustField(t, "n", TypeNumber))
	if err := b.AppendRow("x"); !errors.Is(err, ErrType) {
		t.Fatal(err)
	}
	if b.Frame().Meta.Truncated || b.Bytes() != 0 {
		t.Fatal("type error must not truncate or count bytes")
	}
	if err := b.AppendRow(1); err != nil {
		t.Fatal(err)
	}
}

func TestNewBuilderValidation(t *testing.T) {
	if _, err := NewBuilder("x", Limits{MaxRows: -1}); err == nil {
		t.Error("negative limit accepted")
	}
	f := mustField(t, "n", TypeNumber)
	_ = f.Append(1)
	if _, err := NewBuilder("x", Limits{}, f); err == nil {
		t.Error("non-empty field accepted")
	}
	if _, err := NewBuilder("x", Limits{}, mustField(t, "a", TypeBool), mustField(t, "a", TypeBool)); err == nil {
		t.Error("duplicate field accepted")
	}
}

func sampleFrame(t *testing.T) *Frame {
	t.Helper()
	dec := 2
	tf := mustField(t, "time", TypeTime)
	nf := mustField(t, "value", TypeNumber)
	nf.Config = FieldConfig{DisplayName: "Value", Unit: "ms", Decimals: &dec}
	fr, err := New("cpu", tf, nf, mustField(t, "host", TypeString), mustField(t, "ok", TypeBool), mustField(t, "tags", TypeJSON))
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	rows := [][]any{
		{t0, 1.0, "a", true, json.RawMessage(`{"k":"v"}`)},
		{t0.Add(time.Minute), int64(1) << 60, nil, false, nil},
		{nil, -0.25, "c", nil, json.RawMessage(`[1,2]`)},
		{t0.Add(2 * time.Minute), 1e300, "d", true, json.RawMessage(`null`)},
	}
	for _, r := range rows {
		if err := fr.AppendRow(r...); err != nil {
			t.Fatal(err)
		}
	}
	fr.Meta = Meta{Source: "csv", ExecutedQuery: "SELECT 1", Truncated: true, Notices: []Notice{{SeverityInfo, "hi"}}}
	return fr
}

func TestWireEncoding(t *testing.T) {
	b, err := json.Marshal(sampleFrame(t))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema":{"name":"cpu","meta":{"source":"csv","executedQuery":"SELECT 1","truncated":true,"notices":[{"severity":"info","text":"hi"}]},` +
		`"fields":[{"name":"time","type":"time","config":{}},{"name":"value","type":"number","config":{"displayName":"Value","unit":"ms","decimals":2}},` +
		`{"name":"host","type":"string","config":{}},{"name":"ok","type":"bool","config":{}},{"name":"tags","type":"json","config":{}}]},` +
		`"data":{"values":[[1794045600000,1794045660000,null,1794045720000],[1.0,1152921504606846976,-0.25,1e+300],["a",null,"c","d"],[true,false,null,true],[{"k":"v"},null,[1,2],null]]}}`
	if string(b) != want {
		t.Fatalf("wire mismatch\n got: %s\nwant: %s", b, want)
	}
}

func TestWireEncodingOmitsEmptyMeta(t *testing.T) {
	fr, _ := New("x", mustField(t, "a", TypeBool))
	b, err := json.Marshal(fr)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"schema":{"name":"x","fields":[{"name":"a","type":"bool","config":{}}]},"data":{"values":[[]]}}` {
		t.Fatal(string(b))
	}
	bad := &Frame{Name: "x", Fields: []*Field{nil}}
	if _, err := json.Marshal(bad); err == nil {
		t.Fatal("invalid frame encoded")
	}
}

func TestWireRoundTrip(t *testing.T) {
	in := sampleFrame(t)
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Frame
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	// JSON null in a json field decodes as SQL-style null.
	in.Field("tags").values[3] = nil
	if !reflect.DeepEqual(in, &out) {
		t.Fatalf("round trip mismatch\n in: %#v\nout: %#v", in, &out)
	}
}

func TestWireDecodeRejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"length":        `{"schema":{"name":"x","fields":[{"name":"a","type":"number"},{"name":"b","type":"string"}]},"data":{"values":[[1,2],["x"]]}}`,
		"column count":  `{"schema":{"name":"x","fields":[{"name":"a","type":"number"}]},"data":{"values":[]}}`,
		"unknown key":   `{"schema":{"name":"x","fields":[]},"data":{"values":[]},"extra":1}`,
		"bad type":      `{"schema":{"name":"x","fields":[{"name":"a","type":"int"}]},"data":{"values":[[]]}}`,
		"not array":     `{"schema":{"name":"x","fields":[{"name":"a","type":"number"}]},"data":{"values":[{}]}}`,
		"time string":   `{"schema":{"name":"x","fields":[{"name":"a","type":"time"}]},"data":{"values":[["2026"]]}}`,
		"time float":    `{"schema":{"name":"x","fields":[{"name":"a","type":"time"}]},"data":{"values":[[1.5]]}}`,
		"number string": `{"schema":{"name":"x","fields":[{"name":"a","type":"number"}]},"data":{"values":[["1"]]}}`,
		"number bool":   `{"schema":{"name":"x","fields":[{"name":"a","type":"number"}]},"data":{"values":[[true]]}}`,
		"number huge":   `{"schema":{"name":"x","fields":[{"name":"a","type":"number"}]},"data":{"values":[[1e999]]}}`,
		"string num":    `{"schema":{"name":"x","fields":[{"name":"a","type":"string"}]},"data":{"values":[[1]]}}`,
		"bool str":      `{"schema":{"name":"x","fields":[{"name":"a","type":"bool"}]},"data":{"values":[["t"]]}}`,
		"duplicate":     `{"schema":{"name":"x","fields":[{"name":"a","type":"bool"},{"name":"a","type":"bool"}]},"data":{"values":[[],[]]}}`,
		"syntax":        `{"schema":`,
		"empty object":  `{}`,
		"null":          `null`,
		"null schema":   `{"schema":null,"data":{"values":[]}}`,
		"no data":       `{"schema":{"name":"x","fields":[]}}`,
		"null fields":   `{"schema":{"name":"x","fields":null},"data":{"values":[]}}`,
		"no values":     `{"schema":{"name":"x","fields":[]},"data":{}}`,
		"null column":   `{"schema":{"name":"x","fields":[{"name":"a","type":"number"}]},"data":{"values":[null]}}`,
		"null field":    `{"schema":{"name":"x","fields":[null]},"data":{"values":[[]]}}`,
	}
	for name, payload := range cases {
		var f Frame
		if err := json.Unmarshal([]byte(payload), &f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	var f Frame
	err := json.Unmarshal([]byte(cases["length"]), &f)
	if !errors.Is(err, ErrLength) || !strings.Contains(err.Error(), "length") {
		t.Errorf("length error should wrap ErrLength: %v", err)
	}
}

func TestWireDecodeJSONCompacts(t *testing.T) {
	var f Frame
	err := json.Unmarshal([]byte(`{"schema":{"name":"x","fields":[{"name":"j","type":"json"}]},"data":{"values":[[ { "a" : [ 1 , 2 ] } ]]}}`), &f)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(f.Fields[0].At(0).(json.RawMessage)); got != `{"a":[1,2]}` {
		t.Fatal(got)
	}
}
