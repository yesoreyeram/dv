// Package query runs batches of refId-tagged queries against provisioned data-source
// instances (FR-016, FR-017). It owns request validation, per-query deadlines, host
// result limits, bounded concurrency and the mapping of plugin errors to stable,
// path-free API errors (B-040).
package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/fsroot"
	"github.com/yesoreyeram/dv/internal/plugin"
	"github.com/yesoreyeram/dv/internal/plugins/csvformat"
	"github.com/yesoreyeram/dv/internal/plugins/filesource"
)

// Request limits (FR-016 AC2, AC5).
const (
	MaxQueries     = 20
	MaxConcurrency = 4
)

// StatusClientClosedRequest is the de-facto status for a client that went away (nginx).
const StatusClientClosedRequest = 499

var refIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ErrInvalidRequest wraps every request validation failure.
var ErrInvalidRequest = errors.New("invalid request")

// Instance is a provisioned data-source instance.
type Instance struct {
	UID      string
	Type     string
	Name     string
	JSONData json.RawMessage
}

// Summary is the public view of an instance (FR-017 AC3): identity only, no settings.
type Summary struct {
	UID  string `json:"uid"`
	Type string `json:"type"`
	Name string `json:"name"`
}

// Sources looks up source plugins; *plugin.Registry implements it.
type Sources interface {
	Source(id string) (plugin.Source, error)
}

// Options configure a Service.
type Options struct {
	Sources   Sources
	Instances []Instance
	Limits    frame.Limits  // host limits; both members must be > 0
	Timeout   time.Duration // per-query deadline; must be > 0
	Logger    *slog.Logger
}

// Service executes query requests.
type Service struct {
	sources   Sources
	instances map[string]Instance
	summaries []Summary
	limits    frame.Limits
	timeout   time.Duration
	log       *slog.Logger
}

// New builds a service and validates every instance with its plugin (FR-017 AC2).
func New(ctx context.Context, opts Options) (*Service, error) {
	if opts.Sources == nil {
		return nil, errors.New("query service needs a source registry")
	}
	if opts.Limits.MaxRows <= 0 || opts.Limits.MaxBytes <= 0 {
		return nil, errors.New("query service needs positive host limits")
	}
	if opts.Timeout <= 0 {
		return nil, errors.New("query service needs a positive timeout")
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	s := &Service{
		sources: opts.Sources, instances: map[string]Instance{}, limits: opts.Limits,
		timeout: opts.Timeout, log: opts.Logger,
	}
	for _, in := range opts.Instances {
		if _, dup := s.instances[in.UID]; dup {
			return nil, fmt.Errorf("data source %q: duplicate uid", in.UID)
		}
		src, err := s.sources.Source(in.Type)
		if err != nil {
			return nil, fmt.Errorf("data source %q: unknown type %q", in.UID, in.Type)
		}
		vctx, cancel := context.WithTimeout(ctx, opts.Timeout)
		err = src.Validate(vctx, in.config())
		cancel()
		if err != nil {
			return nil, fmt.Errorf("data source %q: %w", in.UID, err)
		}
		s.instances[in.UID] = in
		s.summaries = append(s.summaries, Summary{UID: in.UID, Type: in.Type, Name: in.Name})
	}
	sort.Slice(s.summaries, func(i, j int) bool { return s.summaries[i].UID < s.summaries[j].UID })
	return s, nil
}

func (in Instance) config() plugin.SourceConfig {
	return plugin.SourceConfig{UID: in.UID, Type: in.Type, Name: in.Name, JSONData: in.JSONData}
}

// DataSources lists the instances sorted by uid. The slice is a copy and never nil.
func (s *Service) DataSources() []Summary {
	out := make([]Summary, len(s.summaries))
	copy(out, s.summaries)
	return out
}

// Request is the POST /api/v1/query body (FR-016 AC1).
type Request struct {
	Queries []Item     `json:"queries"`
	From    *time.Time `json:"from,omitempty"`
	To      *time.Time `json:"to,omitempty"`
}

// Item is one query of a Request.
type Item struct {
	RefID      string          `json:"refId"`
	DataSource DataSourceRef   `json:"datasource"`
	Query      json.RawMessage `json:"query,omitempty"`
	MaxRows    int             `json:"maxRows,omitempty"`
}

// DataSourceRef names an instance.
type DataSourceRef struct {
	UID string `json:"uid"`
}

// Response is the POST /api/v1/query response (FR-016 AC3).
type Response struct {
	Results map[string]Result `json:"results"`
}

// Result is the outcome of one query.
type Result struct {
	Status int            `json:"status"`
	Frames []*frame.Frame `json:"frames,omitempty"`
	Error  *Error         `json:"error,omitempty"`
}

// Error is a stable, path-free query error.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Validate checks a request statically (FR-016 AC1, AC2). Errors wrap ErrInvalidRequest.
func (r Request) Validate() error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidRequest, fmt.Sprintf(format, a...))
	}
	if len(r.Queries) == 0 || len(r.Queries) > MaxQueries {
		return bad("queries must contain between 1 and %d entries", MaxQueries)
	}
	if (r.From == nil) != (r.To == nil) {
		return bad("from and to must be given together")
	}
	if r.From != nil && r.From.After(*r.To) {
		return bad("from must not be after to")
	}
	seen := map[string]bool{}
	for i, q := range r.Queries {
		switch {
		case !refIDPattern.MatchString(q.RefID):
			return bad("queries[%d].refId must match %s", i, refIDPattern)
		case seen[q.RefID]:
			return bad("queries[%d].refId %q is not unique", i, q.RefID)
		case q.DataSource.UID == "":
			return bad("queries[%d].datasource.uid is required", i)
		case q.MaxRows < 0:
			return bad("queries[%d].maxRows must not be negative", i)
		}
		seen[q.RefID] = true
	}
	return nil
}

// Execute validates and runs a request. It returns the response and the HTTP status
// for it (FR-016 AC3). A request validation error is returned as an error and no query
// runs.
func (s *Service) Execute(ctx context.Context, req Request) (Response, int, error) {
	if err := req.Validate(); err != nil {
		return Response{}, 0, err
	}
	var tr plugin.TimeRange
	if req.From != nil {
		tr = plugin.TimeRange{From: req.From.UTC(), To: req.To.UTC()}
	}
	results := make([]Result, len(req.Queries))
	bud := newBudget(s.limits.MaxBytes, len(req.Queries))
	sem := make(chan struct{}, MaxConcurrency)
	var wg sync.WaitGroup
	for i, q := range req.Queries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				grant := bud.reserve()
				var used int64
				results[i], used = s.run(ctx, q, tr, grant, len(req.Queries))
				bud.settle(grant, used)
			case <-ctx.Done():
				results[i] = failure(StatusClientClosedRequest, "cancelled", "the request was cancelled")
			}
		}()
	}
	wg.Wait()

	resp := Response{Results: make(map[string]Result, len(results))}
	for i, r := range results {
		resp.Results[req.Queries[i].RefID] = r
	}
	return resp, overallStatus(results), nil
}

// overallStatus is 200 when all succeeded, the shared status when all failed alike,
// else 207 Multi-Status.
func overallStatus(results []Result) int {
	first := results[0].Status
	for _, r := range results[1:] {
		if r.Status != first {
			return 207
		}
	}
	return first
}

// run executes one query with grant bytes of the request budget (FR-016 AC9). It returns
// the result and the estimated bytes its frames retain.
func (s *Service) run(ctx context.Context, q Item, tr plugin.TimeRange, grant int64, queries int) (Result, int64) {
	start := time.Now()
	in, ok := s.instances[q.DataSource.UID]
	if !ok {
		res := failure(404, "datasource_not_found", "no data source with this uid")
		s.logQuery(ctx, q, in, res, start, nil)
		return res, 0
	}
	if grant <= 0 {
		res := failure(413, "budget_exhausted", fmt.Sprintf(
			"earlier queries of this request used its whole result byte budget of %d bytes; send this query in its own request", s.limits.MaxBytes))
		s.logQuery(ctx, q, in, res, start, nil)
		return res, 0
	}
	limits := s.limits
	limits.MaxBytes = min(grant, limits.MaxBytes)
	if q.MaxRows > 0 && q.MaxRows < limits.MaxRows {
		limits.MaxRows = q.MaxRows
	}
	qctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var res Result
	src, err := s.sources.Source(in.Type)
	if err == nil {
		var out *frame.Result
		out, err = src.Query(qctx, plugin.Query{RefID: q.RefID, Source: in.config(), Body: q.Query, TimeRange: tr, Limits: limits})
		if err == nil {
			frames := []*frame.Frame{}
			if out != nil && out.Frames != nil {
				frames = out.Frames
			}
			res = Result{Status: 200, Frames: frames}
		}
	}
	if err != nil {
		res = mapError(ctx, qctx, err)
	}
	var used int64
	for _, f := range res.Frames {
		used += f.EstimatedBytes()
		if f != nil && f.Meta.Truncated && limits.MaxBytes < s.limits.MaxBytes && f.Rows() < limits.MaxRows {
			f.AddNotice(frame.SeverityWarning, fmt.Sprintf(
				"This query received %d bytes of the request byte budget of %d bytes, which the request's %d queries share. Send it in its own request to get up to %d bytes.",
				limits.MaxBytes, s.limits.MaxBytes, queries, s.limits.MaxBytes))
		}
	}
	s.logQuery(ctx, q, in, res, start, err)
	return res, used
}

func (s *Service) logQuery(ctx context.Context, q Item, in Instance, res Result, start time.Time, err error) {
	attrs := []any{
		"refId", q.RefID, "datasource", q.DataSource.UID, "type", in.Type,
		"status", res.Status, "duration_ms", time.Since(start).Milliseconds(),
	}
	if res.Error != nil {
		attrs = append(attrs, "code", res.Error.Code)
	}
	if err != nil && res.Status >= 500 {
		// Plugin errors are path-free (FR-011 AC9); still only logged server-side.
		attrs = append(attrs, "error", err.Error())
	}
	s.log.InfoContext(ctx, "query executed", attrs...)
}

func failure(status int, code, msg string) Result {
	return Result{Status: status, Error: &Error{Code: code, Message: msg}}
}

// mapError turns a plugin error into a stable API error (FR-016 AC4). Client-input
// errors keep the plugin's message, which is path-free by contract (FR-011 AC9); every
// other error gets a fixed message.
func mapError(parent, qctx context.Context, err error) Result {
	switch {
	case parent.Err() != nil:
		return failure(StatusClientClosedRequest, "cancelled", "the request was cancelled")
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(qctx.Err(), context.DeadlineExceeded):
		return failure(504, "timeout", "the query did not finish within the query timeout")
	case errors.Is(err, fsroot.ErrNotFound):
		return failure(404, "not_found", err.Error())
	case errors.Is(err, fsroot.ErrTooLarge):
		return failure(413, "too_large", err.Error())
	case errors.Is(err, filesource.ErrUnsupportedFormat):
		return failure(415, "unsupported_format", err.Error())
	case errors.Is(err, csvformat.ErrDecode):
		return failure(422, "decode_failed", err.Error())
	case errors.Is(err, fsroot.ErrInvalidPath), errors.Is(err, fsroot.ErrNotRegular),
		errors.Is(err, fsroot.ErrUnknownRoot), errors.Is(err, csvformat.ErrOptions),
		errors.Is(err, filesource.ErrInvalidQuery), errors.Is(err, filesource.ErrInvalidConfig):
		return failure(400, "invalid_query", err.Error())
	}
	return failure(500, "query_failed", "the query failed; see the server log for this request ID")
}
