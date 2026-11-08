package query

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/fsroot"
	"github.com/yesoreyeram/dv/internal/plugin"
	"github.com/yesoreyeram/dv/internal/plugins/csvformat"
	"github.com/yesoreyeram/dv/internal/plugins/filesource"
)

// fake is a scriptable source: the query body {"err":"..."} or {"sleep":ms} drives it.
type fake struct {
	validateErr error
	inFlight    atomic.Int32
	maxInFlight atomic.Int32
	lastLimits  atomic.Value // frame.Limits
	lastRange   atomic.Value // plugin.TimeRange
	nilResult   bool
}

var errs = map[string]error{
	"invalid":  fmt.Errorf("x: %w", fsroot.ErrInvalidPath),
	"missing":  fmt.Errorf("a.csv: %w", fsroot.ErrNotFound),
	"regular":  fsroot.ErrNotRegular,
	"large":    fsroot.ErrTooLarge,
	"root":     fsroot.ErrUnknownRoot,
	"format":   filesource.ErrUnsupportedFormat,
	"query":    filesource.ErrInvalidQuery,
	"config":   filesource.ErrInvalidConfig,
	"options":  fmt.Errorf("%w: bad delimiter", csvformat.ErrOptions),
	"decode":   fmt.Errorf("line 2: %w", csvformat.ErrDecode),
	"internal": errors.New("driver exploded at /srv/secret/dir"),
	"deadline": context.DeadlineExceeded,
}

func (f *fake) Meta() plugin.SourceMeta { return plugin.SourceMeta{ID: "fake", Name: "Fake"} }
func (f *fake) Validate(context.Context, plugin.SourceConfig) error {
	return f.validateErr
}
func (f *fake) Health(context.Context, plugin.SourceConfig) error { return nil }
func (f *fake) Query(ctx context.Context, q plugin.Query) (*frame.Result, error) {
	n := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)
	for {
		m := f.maxInFlight.Load()
		if n <= m || f.maxInFlight.CompareAndSwap(m, n) {
			break
		}
	}
	f.lastLimits.Store(q.Limits)
	f.lastRange.Store(q.TimeRange)
	var body struct {
		Err   string `json:"err"`
		Sleep int    `json:"sleep"`
	}
	if len(q.Body) > 0 {
		if err := json.Unmarshal(q.Body, &body); err != nil {
			return nil, err
		}
	}
	if body.Sleep > 0 {
		select {
		case <-time.After(time.Duration(body.Sleep) * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if body.Err != "" {
		return nil, errs[body.Err]
	}
	if f.nilResult {
		return nil, nil
	}
	fld, err := frame.NewField("v", frame.TypeNumber)
	if err != nil {
		return nil, err
	}
	fr, err := frame.New("f", fld)
	if err != nil {
		return nil, err
	}
	return &frame.Result{Frames: []*frame.Frame{fr}}, nil
}

type sources map[string]plugin.Source

func (s sources) Source(id string) (plugin.Source, error) {
	if src, ok := s[id]; ok {
		return src, nil
	}
	return nil, plugin.ErrNotFound
}

func newService(t *testing.T, f *fake, timeout time.Duration, log *slog.Logger) *Service {
	t.Helper()
	s, err := New(context.Background(), Options{
		Sources:   sources{"fake": f},
		Instances: []Instance{{UID: "b", Type: "fake", Name: "B"}, {UID: "a", Type: "fake", Name: "A", JSONData: json.RawMessage(`{"root":"secret"}`)}},
		Limits:    frame.Limits{MaxRows: 100, MaxBytes: 1 << 20},
		Timeout:   timeout,
		Logger:    log,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func q(ref, uid, body string) Item {
	return Item{RefID: ref, DataSource: DataSourceRef{UID: uid}, Query: json.RawMessage(body)}
}

func TestNewValidatesOptionsAndInstances(t *testing.T) {
	good := Options{Sources: sources{"fake": &fake{}}, Limits: frame.Limits{MaxRows: 1, MaxBytes: 1}, Timeout: time.Second}
	cases := map[string]func(o *Options){
		"nil sources":  func(o *Options) { o.Sources = nil },
		"zero rows":    func(o *Options) { o.Limits.MaxRows = 0 },
		"zero bytes":   func(o *Options) { o.Limits.MaxBytes = 0 },
		"zero timeout": func(o *Options) { o.Timeout = 0 },
		"unknown type": func(o *Options) { o.Instances = []Instance{{UID: "x", Type: "nope"}} },
		"duplicate":    func(o *Options) { o.Instances = []Instance{{UID: "x", Type: "fake"}, {UID: "x", Type: "fake"}} },
		"invalid": func(o *Options) {
			o.Sources = sources{"fake": &fake{validateErr: filesource.ErrInvalidConfig}}
			o.Instances = []Instance{{UID: "x", Type: "fake"}}
		},
	}
	for name, mut := range cases {
		o := good
		mut(&o)
		if _, err := New(context.Background(), o); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	s, err := New(context.Background(), good)
	if err != nil {
		t.Fatal(err)
	}
	if ds := s.DataSources(); ds == nil || len(ds) != 0 {
		t.Fatalf("empty listing = %#v", ds)
	}
}

func TestDataSourcesSortedIdentityOnly(t *testing.T) {
	s := newService(t, &fake{}, time.Second, nil)
	ds := s.DataSources()
	if len(ds) != 2 || ds[0].UID != "a" || ds[1].UID != "b" || ds[0].Name != "A" || ds[0].Type != "fake" {
		t.Fatalf("%+v", ds)
	}
	b, _ := json.Marshal(ds)
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "jsonData") {
		t.Fatalf("settings leaked: %s", b)
	}
	ds[0].UID = "mutated"
	if s.DataSources()[0].UID != "a" {
		t.Fatal("listing is not a copy")
	}
}

func TestValidate(t *testing.T) {
	t1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	many := make([]Item, MaxQueries+1)
	for i := range many {
		many[i] = q(fmt.Sprintf("Q%d", i), "a", "")
	}
	bad := map[string]Request{
		"empty":       {},
		"too many":    {Queries: many},
		"bad refId":   {Queries: []Item{q("a b", "a", "")}},
		"long refId":  {Queries: []Item{q(strings.Repeat("x", 65), "a", "")}},
		"dup refId":   {Queries: []Item{q("A", "a", ""), q("A", "b", "")}},
		"no uid":      {Queries: []Item{q("A", "", "")}},
		"neg maxRows": {Queries: []Item{{RefID: "A", DataSource: DataSourceRef{UID: "a"}, MaxRows: -1}}},
		"from only":   {Queries: []Item{q("A", "a", "")}, From: &t1},
		"to only":     {Queries: []Item{q("A", "a", "")}, To: &t1},
		"reversed":    {Queries: []Item{q("A", "a", "")}, From: &t2, To: &t1},
	}
	for name, r := range bad {
		if err := r.Validate(); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	ok := Request{Queries: many[:MaxQueries], From: &t1, To: &t2}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteInvalidRequestRunsNothing(t *testing.T) {
	f := &fake{}
	s := newService(t, f, time.Second, nil)
	_, _, err := s.Execute(context.Background(), Request{Queries: []Item{q("A", "a", ""), q("A", "a", "")}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	if f.maxInFlight.Load() != 0 {
		t.Fatal("a query ran")
	}
}

func TestExecuteStatuses(t *testing.T) {
	s := newService(t, &fake{}, time.Second, nil)
	cases := []struct {
		name    string
		queries []Item
		status  int
	}{
		{"all ok", []Item{q("A", "a", ""), q("B", "b", "")}, 200},
		{"partial", []Item{q("A", "a", ""), q("B", "a", `{"err":"missing"}`)}, 207},
		{"all same failure", []Item{q("A", "a", `{"err":"missing"}`), q("B", "zzz", "")}, 404},
		{"all different failures", []Item{q("A", "a", `{"err":"large"}`), q("B", "zzz", "")}, 207},
	}
	for _, c := range cases {
		resp, status, err := s.Execute(context.Background(), Request{Queries: c.queries})
		if err != nil {
			t.Fatal(err)
		}
		if status != c.status {
			t.Errorf("%s: status %d, want %d", c.name, status, c.status)
		}
		if len(resp.Results) != len(c.queries) {
			t.Errorf("%s: %d results", c.name, len(resp.Results))
		}
	}
}

func TestErrorMapping(t *testing.T) {
	s := newService(t, &fake{}, time.Second, nil)
	want := map[string]struct {
		status int
		code   string
	}{
		"invalid":  {400, "invalid_query"},
		"missing":  {404, "not_found"},
		"regular":  {400, "invalid_query"},
		"large":    {413, "too_large"},
		"root":     {400, "invalid_query"},
		"format":   {415, "unsupported_format"},
		"query":    {400, "invalid_query"},
		"config":   {400, "invalid_query"},
		"options":  {400, "invalid_query"},
		"decode":   {422, "decode_failed"},
		"internal": {500, "query_failed"},
		"deadline": {504, "timeout"},
	}
	for kind, w := range want {
		resp, _, err := s.Execute(context.Background(), Request{Queries: []Item{q("A", "a", fmt.Sprintf(`{"err":%q}`, kind))}})
		if err != nil {
			t.Fatal(err)
		}
		r := resp.Results["A"]
		if r.Status != w.status || r.Error == nil || r.Error.Code != w.code {
			t.Errorf("%s: %+v %+v", kind, r, r.Error)
			continue
		}
		if r.Frames != nil {
			t.Errorf("%s: frames on error", kind)
		}
		if strings.Contains(r.Error.Message, "/srv/secret") {
			t.Errorf("%s: internal detail leaked: %q", kind, r.Error.Message)
		}
	}
	resp, _, _ := s.Execute(context.Background(), Request{Queries: []Item{q("A", "nope", "")}})
	if r := resp.Results["A"]; r.Status != 404 || r.Error.Code != "datasource_not_found" {
		t.Fatalf("%+v", r)
	}
}

func TestTimeoutAndCancellation(t *testing.T) {
	s := newService(t, &fake{}, 30*time.Millisecond, nil)
	start := time.Now()
	resp, status, err := s.Execute(context.Background(), Request{Queries: []Item{q("A", "a", `{"sleep":5000}`)}})
	if err != nil || status != 504 || resp.Results["A"].Error.Code != "timeout" {
		t.Fatalf("%v %d %+v", err, status, resp.Results["A"])
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("timeout not enforced")
	}

	s = newService(t, &fake{}, 10*time.Second, nil)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	resp, status, _ = s.Execute(ctx, Request{Queries: []Item{q("A", "a", `{"sleep":5000}`)}})
	if status != StatusClientClosedRequest || resp.Results["A"].Error.Code != "cancelled" {
		t.Fatalf("%d %+v", status, resp.Results["A"])
	}

	// Queries waiting for a slot when the client leaves are answered as cancelled.
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	qs := make([]Item, MaxQueries)
	for i := range qs {
		qs[i] = q(fmt.Sprintf("Q%d", i), "a", `{"sleep":5000}`)
	}
	resp, status, _ = s.Execute(ctx, Request{Queries: qs})
	if status != StatusClientClosedRequest || len(resp.Results) != MaxQueries {
		t.Fatalf("%d %d", status, len(resp.Results))
	}
}

func TestConcurrencyIsBounded(t *testing.T) {
	f := &fake{}
	s := newService(t, f, 5*time.Second, nil)
	qs := make([]Item, MaxQueries)
	for i := range qs {
		qs[i] = q(fmt.Sprintf("Q%d", i), "a", `{"sleep":20}`)
	}
	_, status, err := s.Execute(context.Background(), Request{Queries: qs})
	if err != nil || status != 200 {
		t.Fatal(err, status)
	}
	if m := f.maxInFlight.Load(); m > MaxConcurrency || m < 2 {
		t.Fatalf("max in flight = %d", m)
	}
}

func TestHostLimitsCanOnlyBeLowered(t *testing.T) {
	f := &fake{}
	s := newService(t, f, time.Second, nil)
	for _, c := range []struct{ req, want int }{{0, 100}, {50, 50}, {1000, 100}} {
		_, _, err := s.Execute(context.Background(), Request{Queries: []Item{{RefID: "A", DataSource: DataSourceRef{UID: "a"}, MaxRows: c.req}}})
		if err != nil {
			t.Fatal(err)
		}
		l := f.lastLimits.Load().(frame.Limits)
		if l.MaxRows != c.want || l.MaxBytes != 1<<20 {
			t.Errorf("maxRows %d: limits %+v", c.req, l)
		}
	}
}

func TestTimeRangePassedInUTC(t *testing.T) {
	f := &fake{}
	s := newService(t, f, time.Second, nil)
	loc := time.FixedZone("x", 3600)
	from, to := time.Date(2026, 11, 1, 1, 0, 0, 0, loc), time.Date(2026, 11, 2, 1, 0, 0, 0, loc)
	if _, _, err := s.Execute(context.Background(), Request{Queries: []Item{q("A", "a", "")}, From: &from, To: &to}); err != nil {
		t.Fatal(err)
	}
	tr := f.lastRange.Load().(plugin.TimeRange)
	if tr.From.Location() != time.UTC || !tr.From.Equal(from) || !tr.To.Equal(to) {
		t.Fatalf("%+v", tr)
	}
}

func TestNilResultIsEmptyFrames(t *testing.T) {
	s := newService(t, &fake{nilResult: true}, time.Second, nil)
	resp, status, _ := s.Execute(context.Background(), Request{Queries: []Item{q("A", "a", "")}})
	b, _ := json.Marshal(resp)
	if status != 200 || resp.Results["A"].Frames == nil {
		t.Fatalf("%d %s", status, b)
	}
}

func TestQueriesAreLogged(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	s := newService(t, &fake{}, time.Second, log)
	_, _, _ = s.Execute(context.Background(), Request{Queries: []Item{q("A", "a", ""), q("B", "a", `{"err":"internal"}`)}})
	out := buf.String()
	for _, want := range []string{`"refId":"A"`, `"refId":"B"`, `"datasource":"a"`, `"type":"fake"`, `"status":200`, `"status":500`, `"code":"query_failed"`, `"duration_ms"`, `driver exploded`} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"frames"`) || strings.Contains(out, `"values"`) {
		t.Errorf("results logged:\n%s", out)
	}
}
