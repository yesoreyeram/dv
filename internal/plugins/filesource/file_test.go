package filesource

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/fsroot"
	"github.com/yesoreyeram/dv/internal/plugin"
	"github.com/yesoreyeram/dv/internal/plugins/csvformat"
)

type fixture struct {
	src  *Source
	dir  string
	cfg  plugin.SourceConfig
	root *fsroot.Roots
}

func setup(t *testing.T) fixture {
	t.Helper()
	dir := t.TempDir()
	roots, err := fsroot.Open(map[string]string{"samples": dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = roots.Close() })
	reg := plugin.NewRegistry()
	for _, f := range []plugin.Format{csvformat.NewCSV(), csvformat.NewTSV()} {
		if err := reg.RegisterFormat(f); err != nil {
			t.Fatal(err)
		}
	}
	src, err := New(roots, reg, 1024)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{src: src, dir: dir, root: roots, cfg: plugin.SourceConfig{UID: "s1", Type: ID, JSONData: json.RawMessage(`{"root":"samples"}`)}}
}

func (f fixture) write(t *testing.T, name, content string) {
	t.Helper()
	p := filepath.Join(f.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) query(body string) (*frame.Result, error) {
	return f.src.Query(context.Background(), plugin.Query{RefID: "A", Source: f.cfg, Body: json.RawMessage(body)})
}

func TestNew(t *testing.T) {
	f := setup(t)
	if _, err := New(nil, plugin.NewRegistry(), 1); err == nil {
		t.Fatal("nil roots")
	}
	if _, err := New(f.root, nil, 1); err == nil {
		t.Fatal("nil formats")
	}
	if _, err := New(f.root, plugin.NewRegistry(), 0); err == nil {
		t.Fatal("zero limit")
	}
	if m := f.src.Meta(); m.ID != "file" || m.Name != "Local files" {
		t.Fatalf("%+v", m)
	}
}

func TestQueryCSV(t *testing.T) {
	f := setup(t)
	f.write(t, "dir/sales.csv", "region,amount\nemea,1\napac,2.5\n")
	res, err := f.query(`{"path":"dir/sales.csv"}`)
	if err != nil {
		t.Fatal(err)
	}
	fr := res.Frames[0]
	if fr.Name != "sales.csv" || fr.Rows() != 2 || fr.Meta.Source != "file" || fr.Meta.ExecutedQuery != "samples:dir/sales.csv" {
		t.Fatalf("frame %q rows %d meta %+v", fr.Name, fr.Rows(), fr.Meta)
	}
	if fr.Field("amount").Type != frame.TypeNumber {
		t.Fatal("amount type")
	}
}

func TestQueryFormatSelection(t *testing.T) {
	f := setup(t)
	f.write(t, "a.TAB", "x\ty\n1\t2\n")
	f.write(t, "data.txt", "x;y\n1;2\n")
	f.write(t, "noext", "x\n1\n")
	if res, err := f.query(`{"path":"a.TAB"}`); err != nil || len(res.Frames[0].Fields) != 2 {
		t.Fatalf("tab by extension: %v", err)
	}
	res, err := f.query(`{"path":"data.txt","format":"csv","options":{"delimiter":";"},"fieldTypes":{"y":"string"}}`)
	if err != nil || res.Frames[0].Field("y").Type != frame.TypeString {
		t.Fatalf("explicit format: %v", err)
	}
	for body, want := range map[string]error{
		`{"path":"data.txt"}`:                ErrUnsupportedFormat,
		`{"path":"noext"}`:                   ErrUnsupportedFormat,
		`{"path":"data.txt","format":"xls"}`: ErrUnsupportedFormat,
	} {
		if _, err := f.query(body); !errors.Is(err, want) {
			t.Errorf("%s: err = %v", body, err)
		}
	}
}

func TestQueryErrors(t *testing.T) {
	f := setup(t)
	f.write(t, "big.csv", "v\n"+strings.Repeat("1\n", 1000))
	f.write(t, "ragged.csv", "a,b\n1\n")
	if err := os.Mkdir(filepath.Join(f.dir, "folder.csv"), 0o750); err != nil {
		t.Fatal(err)
	}
	cases := map[string]error{
		`{"path":"../etc/passwd.csv"}`:      fsroot.ErrInvalidPath,
		`{"path":"/etc/passwd"}`:            fsroot.ErrInvalidPath,
		`{"path":""}`:                       fsroot.ErrInvalidPath,
		`{"path":"missing.csv"}`:            fsroot.ErrNotFound,
		`{"path":"folder.csv"}`:             fsroot.ErrNotRegular,
		`{"path":"big.csv"}`:                fsroot.ErrTooLarge,
		`{"path":"ragged.csv"}`:             csvformat.ErrDecode,
		`{"path":"a.csv","root":"other"}`:   ErrInvalidQuery,
		`{"path":"a.csv"} {"path":"b.csv"}`: ErrInvalidQuery,
		`not json`:                          ErrInvalidQuery,
	}
	for body, want := range cases {
		_, err := f.query(body)
		if !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", body, err, want)
			continue
		}
		if strings.Contains(err.Error(), f.dir) {
			t.Errorf("%s: error %q reveals the root directory", body, err)
		}
	}
}

func TestConfig(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if err := f.src.Validate(ctx, f.cfg); err != nil {
		t.Fatal(err)
	}
	if err := f.src.Health(ctx, f.cfg); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{``, `{}`, `{"root":""}`, `{"root":"elsewhere"}`, `{"root":"samples","path":"/"}`, `[]`} {
		cfg := f.cfg
		cfg.JSONData = json.RawMessage(raw)
		if err := f.src.Validate(ctx, cfg); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%q: err = %v", raw, err)
		}
		if _, err := f.src.Query(ctx, plugin.Query{Source: cfg, Body: json.RawMessage(`{"path":"a.csv"}`)}); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("query with %q: err = %v", raw, err)
		}
	}
}

func TestLimitsAreApplied(t *testing.T) {
	f := setup(t)
	f.write(t, "n.csv", "v\n1\n2\n3\n4\n")
	res, err := f.src.Query(context.Background(), plugin.Query{Source: f.cfg, Body: json.RawMessage(`{"path":"n.csv"}`), Limits: frame.Limits{MaxRows: 2}})
	if err != nil || res.Frames[0].Rows() != 2 || !res.Frames[0].Meta.Truncated {
		t.Fatalf("%v", err)
	}
}
