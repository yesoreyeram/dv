package bdd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"

	"github.com/yesoreyeram/dv/internal/frame"
)

// TestFrameFeatures executes features/core/frame.feature against internal/frame.
func TestFrameFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Name:                "core",
		ScenarioInitializer: initializeFrameScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"../../features/core"},
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("core feature suite failed")
	}
}

type frameWorld struct {
	name    string
	fields  []*frame.Field
	limits  frame.Limits
	builder *frame.Builder
	err     error
}

func (w *frameWorld) build() (*frame.Builder, error) {
	if w.builder != nil {
		return w.builder, nil
	}
	b, err := frame.NewBuilder(w.name, w.limits, w.fields...)
	if err != nil {
		return nil, err
	}
	w.builder = b
	return b, nil
}

func (w *frameWorld) wire() ([]byte, error) {
	b, err := w.build()
	if err != nil {
		return nil, err
	}
	return json.Marshal(b.Frame())
}

// parseCell converts a table cell to the Go value for typ; "null" is null.
func parseCell(typ frame.FieldType, cell string) (any, error) {
	if cell == "null" {
		return nil, nil
	}
	switch typ {
	case frame.TypeTime:
		return time.Parse(time.RFC3339, cell)
	case frame.TypeNumber:
		return strconv.ParseFloat(cell, 64)
	case frame.TypeBool:
		return strconv.ParseBool(cell)
	default:
		return cell, nil
	}
}

func initializeFrameScenario(sc *godog.ScenarioContext) {
	w := &frameWorld{}
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*w = frameWorld{}
		return ctx, nil
	})

	sc.Step(`^a frame "([^"]*)" with fields:$`, func(name string, tbl *godog.Table) error {
		w.name = name
		for _, row := range tbl.Rows[1:] {
			f, err := frame.NewField(row.Cells[0].Value, frame.FieldType(row.Cells[1].Value))
			if err != nil {
				return err
			}
			w.fields = append(w.fields, f)
		}
		return nil
	})
	sc.Step(`^a row limit of (\d+)$`, func(n int) error { w.limits.MaxRows = n; return nil })
	sc.Step(`^a byte limit of (\d+)$`, func(n int) error { w.limits.MaxBytes = int64(n); return nil })

	sc.Step(`^I append the rows:$`, func(tbl *godog.Table) error {
		b, err := w.build()
		if err != nil {
			return err
		}
		for _, row := range tbl.Rows[1:] {
			vals := make([]any, len(row.Cells))
			for i, c := range row.Cells {
				if vals[i], err = parseCell(w.fields[i].Type, c.Value); err != nil {
					return err
				}
			}
			if err := b.AppendRow(vals...); err != nil {
				return err
			}
		}
		return nil
	})
	sc.Step(`^I append the value "([^"]*)" to field "([^"]*)"$`, func(v, _ string) error {
		b, err := w.build()
		if err != nil {
			return err
		}
		w.err = b.AppendRow(v)
		return nil
	})
	sc.Step(`^I append (\d+) numeric rows$`, func(n int) error {
		b, err := w.build()
		if err != nil {
			return err
		}
		for i := range n {
			if err := b.AppendRow(float64(i)); err != nil {
				w.err = err
				break
			}
		}
		return nil
	})
	sc.Step(`^I append (\d+) rows of (\d+)-character text$`, func(n, size int) error {
		b, err := w.build()
		if err != nil {
			return err
		}
		for range n {
			if err := b.AppendRow(strings.Repeat("x", size)); err != nil {
				w.err = err
				break
			}
		}
		return nil
	})

	sc.Step(`^the frame has (\d+) rows$`, func(n int) error {
		b, err := w.build()
		if err != nil {
			return err
		}
		if got := b.Frame().Rows(); got != n {
			return fmt.Errorf("rows = %d, want %d", got, n)
		}
		return nil
	})
	sc.Step(`^the frame has fewer than (\d+) rows$`, func(n int) error {
		if got := w.builder.Frame().Rows(); got >= n {
			return fmt.Errorf("rows = %d, want < %d", got, n)
		}
		return nil
	})
	sc.Step(`^the append fails with an error mentioning "([^"]*)"$`, func(s string) error {
		if w.err == nil || !strings.Contains(w.err.Error(), s) {
			return fmt.Errorf("error = %w, want mention of %q", w.err, s)
		}
		return nil
	})
	sc.Step(`^the frame is marked truncated$`, func() error {
		if !w.builder.Frame().Meta.Truncated {
			return errors.New("frame not truncated")
		}
		if !errors.Is(w.err, frame.ErrLimitReached) {
			return fmt.Errorf("builder error = %w, want ErrLimitReached", w.err)
		}
		return nil
	})
	sc.Step(`^the frame has a warning notice mentioning "([^"]*)"$`, func(s string) error {
		for _, n := range w.builder.Frame().Meta.Notices {
			if n.Severity == frame.SeverityWarning && strings.Contains(n.Text, s) {
				return nil
			}
		}
		return fmt.Errorf("no warning mentioning %q in %+v", s, w.builder.Frame().Meta.Notices)
	})

	sc.Step(`^the wire JSON field "([^"]*)" is "([^"]*)"$`, func(path, want string) error {
		b, err := w.wire()
		if err != nil {
			return err
		}
		got, err := jsonField(b, path)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("%s = %q, want %q", path, got, want)
		}
		return nil
	})
	sc.Step(`^the wire JSON field "([^"]*)" is the number (\d+)$`, func(path string, want int64) error {
		b, err := w.wire()
		if err != nil {
			return err
		}
		got, err := jsonField(b, path)
		if err != nil {
			return err
		}
		if got != strconv.FormatInt(want, 10) {
			return fmt.Errorf("%s = %s, want %d", path, got, want)
		}
		return nil
	})
	sc.Step(`^the wire JSON field "([^"]*)" is null$`, func(path string) error {
		b, err := w.wire()
		if err != nil {
			return err
		}
		v, err := jsonLookup(b, path)
		if err != nil {
			return err
		}
		if v != nil {
			return fmt.Errorf("%s = %#v, want null", path, v)
		}
		return nil
	})

	sc.Step(`^I decode the wire payload '([^']*)'$`, func(payload string) error {
		var f frame.Frame
		w.err = json.Unmarshal([]byte(payload), &f)
		return nil
	})
	sc.Step(`^decoding fails with an error mentioning "([^"]*)"$`, func(s string) error {
		if w.err == nil || !strings.Contains(w.err.Error(), s) {
			return fmt.Errorf("error = %w, want mention of %q", w.err, s)
		}
		return nil
	})
	sc.Step(`^decoding the frame's wire JSON yields an equal frame$`, func() error {
		b, err := w.wire()
		if err != nil {
			return err
		}
		var out frame.Frame
		if err := json.Unmarshal(b, &out); err != nil {
			return err
		}
		if !reflect.DeepEqual(w.builder.Frame(), &out) {
			return fmt.Errorf("round trip mismatch:\n in: %#v\nout: %#v", w.builder.Frame(), &out)
		}
		return nil
	})
}
