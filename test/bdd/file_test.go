package bdd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cucumber/godog"

	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/fsroot"
	"github.com/yesoreyeram/dv/internal/plugin"
	"github.com/yesoreyeram/dv/internal/plugins/builtin"
	"github.com/yesoreyeram/dv/internal/plugins/filesource"
)

// TestFileSourceFeatures executes features/sources against the real built-in plugins.
func TestFileSourceFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Name:                "sources",
		ScenarioInitializer: initializeFileScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"../../features/sources"},
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("sources feature suite failed")
	}
}

type fileWorld struct {
	base     string // parent of the root, for files "outside" it
	rootName string
	rootDir  string
	outside  string
	maxBytes int64
	defaults frame.Limits
	limits   frame.Limits
	cfg      plugin.SourceConfig
	result   *frame.Result
	err      error
}

// errorKinds maps the feature's wording to the stable error kinds (FR-011 AC9).
var errorKinds = map[string]error{
	"invalid path":       fsroot.ErrInvalidPath,
	"not found":          fsroot.ErrNotFound,
	"not a regular file": fsroot.ErrNotRegular,
	"too large":          fsroot.ErrTooLarge,
	"unsupported format": filesource.ErrUnsupportedFormat,
}

func (w *fileWorld) source() (plugin.Source, func(), error) {
	roots, err := fsroot.Open(map[string]string{w.rootName: w.rootDir})
	if err != nil {
		return nil, nil, err
	}
	reg := plugin.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Roots: roots, FileMaxBytes: w.maxBytes, QueryDefaults: w.defaults}); err != nil {
		_ = roots.Close()
		return nil, nil, err
	}
	reg.Freeze()
	src, err := reg.Source(filesource.ID)
	if err != nil {
		_ = roots.Close()
		return nil, nil, err
	}
	return src, func() { _ = roots.Close() }, nil
}

func (w *fileWorld) writeFile(name, content string) error {
	p := filepath.Join(w.rootDir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(content), 0o600)
}

func (w *fileWorld) query(body map[string]any) error {
	src, done, err := w.source()
	if err != nil {
		return err
	}
	defer done()
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	w.result, w.err = src.Query(context.Background(), plugin.Query{RefID: "A", Source: w.cfg, Body: b, Limits: w.limits})
	return nil
}

func (w *fileWorld) frame() (*frame.Frame, error) {
	if w.err != nil {
		return nil, fmt.Errorf("query failed: %w", w.err)
	}
	if w.result == nil || len(w.result.Frames) != 1 {
		return nil, errors.New("expected exactly one frame")
	}
	return w.result.Frames[0], nil
}

func (w *fileWorld) field(name string) (*frame.Field, error) {
	fr, err := w.frame()
	if err != nil {
		return nil, err
	}
	f := fr.Field(name)
	if f == nil {
		return nil, fmt.Errorf("no field %q", name)
	}
	return f, nil
}

func numericRows(n int) string {
	var b strings.Builder
	b.WriteString("value\n")
	for i := range n {
		fmt.Fprintf(&b, "%d\n", i)
	}
	return b.String()
}

func initializeFileScenario(sc *godog.ScenarioContext) {
	w := &fileWorld{}
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		base, err := os.MkdirTemp("", "dv-bdd-files-")
		if err != nil {
			return ctx, err
		}
		*w = fileWorld{base: base, maxBytes: 64 << 20}
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		return ctx, errors.Join(err, os.RemoveAll(w.base))
	})

	sc.Step(`^a file root "([^"]*)"$`, func(name string) error {
		w.rootName, w.rootDir = name, filepath.Join(w.base, "root")
		return os.Mkdir(w.rootDir, 0o750)
	})
	sc.Step(`^a file source instance using root "([^"]*)"$`, func(root string) error {
		w.cfg = plugin.SourceConfig{UID: "files", Type: filesource.ID, Name: "Files", JSONData: json.RawMessage(fmt.Sprintf(`{"root":%q}`, root))}
		return nil
	})
	sc.Step(`^the file "([^"]*)" contains:$`, func(name string, doc *godog.DocString) error {
		return w.writeFile(name, doc.Content+"\n")
	})
	sc.Step(`^the file "([^"]*)" contains the tab-separated rows:$`, func(name string, tbl *godog.Table) error {
		var b strings.Builder
		for _, row := range tbl.Rows {
			cells := make([]string, len(row.Cells))
			for i, c := range row.Cells {
				cells[i] = c.Value
			}
			b.WriteString(strings.Join(cells, "\t") + "\n")
		}
		return w.writeFile(name, b.String())
	})
	sc.Step(`^the file "([^"]*)" contains (\d+) numeric rows$`, func(name string, n int) error {
		return w.writeFile(name, numericRows(n))
	})
	sc.Step(`^the directory "([^"]*)" exists$`, func(name string) error {
		return os.MkdirAll(filepath.Join(w.rootDir, filepath.FromSlash(name)), 0o750)
	})
	sc.Step(`^a file "([^"]*)" outside the root$`, func(name string) error {
		w.outside = filepath.Join(w.base, name)
		return os.WriteFile(w.outside, []byte("secret\n42\n"), 0o600)
	})
	sc.Step(`^the file "([^"]*)" is a symlink to that outside file$`, func(name string) error {
		return os.Symlink(w.outside, filepath.Join(w.rootDir, name))
	})
	sc.Step(`^a row limit of (\d+)$`, func(n int) error { w.limits.MaxRows = n; return nil })
	sc.Step(`^the default row limit for queries is (\d+)$`, func(n int) error { w.defaults.MaxRows = n; return nil })
	sc.Step(`^a file size cap of (\d+) bytes$`, func(n int) error { w.maxBytes = int64(n); return nil })

	sc.Step(`^I query the file "([^"]*)"$`, func(p string) error {
		return w.query(map[string]any{"path": p})
	})
	sc.Step(`^I query the file "([^"]*)" with field type "([^"]*)" set to "([^"]*)"$`, func(p, field, typ string) error {
		return w.query(map[string]any{"path": p, "fieldTypes": map[string]string{field: typ}})
	})
	sc.Step(`^I validate a file source instance using root "([^"]*)"$`, func(root string) error {
		src, done, err := w.source()
		if err != nil {
			return err
		}
		defer done()
		cfg := w.cfg
		cfg.JSONData = json.RawMessage(fmt.Sprintf(`{"root":%q}`, root))
		w.err = src.Validate(context.Background(), cfg)
		return nil
	})

	sc.Step(`^the query succeeds with (\d+) frames? named "([^"]*)"$`, func(n int, name string) error {
		if w.err != nil {
			return fmt.Errorf("query failed: %w", w.err)
		}
		if len(w.result.Frames) != n {
			return fmt.Errorf("frames = %d, want %d", len(w.result.Frames), n)
		}
		if got := w.result.Frames[0].Name; got != name {
			return fmt.Errorf("frame name = %q, want %q", got, name)
		}
		return nil
	})
	sc.Step(`^the frame has (\d+) rows$`, func(n int) error {
		fr, err := w.frame()
		if err != nil {
			return err
		}
		if fr.Rows() != n {
			return fmt.Errorf("rows = %d, want %d", fr.Rows(), n)
		}
		return nil
	})
	sc.Step(`^the frame fields are:$`, func(tbl *godog.Table) error {
		fr, err := w.frame()
		if err != nil {
			return err
		}
		if len(fr.Fields) != len(tbl.Rows)-1 {
			return fmt.Errorf("fields = %d, want %d", len(fr.Fields), len(tbl.Rows)-1)
		}
		for i, row := range tbl.Rows[1:] {
			f := fr.Fields[i]
			if f.Name != row.Cells[0].Value || string(f.Type) != row.Cells[1].Value {
				return fmt.Errorf("field %d = %s:%s, want %s:%s", i, f.Name, f.Type, row.Cells[0].Value, row.Cells[1].Value)
			}
		}
		return nil
	})
	sc.Step(`^the frame field "([^"]*)" has type "([^"]*)"$`, func(name, typ string) error {
		f, err := w.field(name)
		if err != nil {
			return err
		}
		if string(f.Type) != typ {
			return fmt.Errorf("type = %s, want %s", f.Type, typ)
		}
		return nil
	})
	sc.Step(`^the frame field "([^"]*)" has values "([^"]*)"$`, func(name, want string) error {
		f, err := w.field(name)
		if err != nil {
			return err
		}
		parts := make([]string, f.Len())
		for i := range f.Len() {
			if v := f.At(i); v == nil {
				parts[i] = "null"
			} else {
				parts[i] = fmt.Sprint(v)
			}
		}
		if got := strings.Join(parts, ", "); got != want {
			return fmt.Errorf("values = %q, want %q", got, want)
		}
		return nil
	})
	sc.Step(`^the frame meta "executedQuery" is "([^"]*)"$`, func(want string) error {
		fr, err := w.frame()
		if err != nil {
			return err
		}
		if fr.Meta.ExecutedQuery != want {
			return fmt.Errorf("executedQuery = %q, want %q", fr.Meta.ExecutedQuery, want)
		}
		return nil
	})
	sc.Step(`^the frame has a warning notice mentioning "([^"]*)"$`, func(s string) error {
		fr, err := w.frame()
		if err != nil {
			return err
		}
		for _, n := range fr.Meta.Notices {
			if n.Severity == frame.SeverityWarning && strings.Contains(n.Text, s) {
				return nil
			}
		}
		return fmt.Errorf("no warning mentioning %q in %+v", s, fr.Meta.Notices)
	})
	sc.Step(`^the frame is marked truncated$`, func() error {
		fr, err := w.frame()
		if err != nil {
			return err
		}
		if !fr.Meta.Truncated {
			return errors.New("frame not truncated")
		}
		return nil
	})
	sc.Step(`^the query fails as "([^"]*)"$`, func(kind string) error {
		want, ok := errorKinds[kind]
		if !ok {
			return fmt.Errorf("unknown error kind %q", kind)
		}
		if !errors.Is(w.err, want) {
			return fmt.Errorf("error = %w, want kind %q", w.err, kind)
		}
		return nil
	})
	sc.Step(`^the query fails with an error mentioning "([^"]*)"$`, func(s string) error {
		if w.err == nil || !strings.Contains(w.err.Error(), s) {
			return fmt.Errorf("error = %w, want mention of %q", w.err, s)
		}
		return nil
	})
	sc.Step(`^the error does not reveal the root directory$`, func() error {
		if w.err == nil {
			return errors.New("expected an error")
		}
		if strings.Contains(w.err.Error(), w.rootDir) || strings.Contains(w.err.Error(), w.base) {
			return fmt.Errorf("error %q reveals a server path", w.err.Error())
		}
		return nil
	})
	sc.Step(`^validation fails with an error mentioning "([^"]*)"$`, func(s string) error {
		if w.err == nil || !strings.Contains(w.err.Error(), s) {
			return fmt.Errorf("error = %w, want mention of %q", w.err, s)
		}
		return nil
	})
}
