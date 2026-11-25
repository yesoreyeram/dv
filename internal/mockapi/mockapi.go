// Package mockapi is a deterministic fake of several vendor APIs (GitHub, Sentry, Vercel,
// Datadog, ThousandEyes, Cloudflare, Dynatrace) used by tests, UI scenarios and the
// prototype (NFR-MAINT-009 AC2). It serves recorded-style fixtures under /api with each
// vendor's pagination style and credential check. It never contains real tokens.
package mockapi

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Tokens are the credentials the fake accepts.
type Tokens struct {
	GitHub  string // bearer
	Sentry  string // bearer
	Vercel  string // bearer
	Datadog string // DD-API-KEY (any non-empty DD-APPLICATION-KEY is accepted)
}

// DefaultTokens are fixture values, not secrets.
var DefaultTokens = Tokens{GitHub: "s3cr3t-token", Sentry: "sentry-token", Vercel: "v-token", Datadog: "dd-api-key"}

// Handler returns the fake API.
func Handler(tok Tokens) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/github/repos/acme/web/issues", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+tok.GitHub {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
			return
		}
		per := intParam(r, "per_page", 30, 1, 100)
		page := intParam(r, "page", 1, 1, 1000)
		items, last := pageOf(githubIssues, page, per)
		var links []string
		if page < last {
			links = append(links, fmt.Sprintf(`<%s>; rel="next"`, selfURL(r, map[string]string{"page": strconv.Itoa(page + 1)})))
		}
		links = append(links, fmt.Sprintf(`<%s>; rel="last"`, selfURL(r, map[string]string{"page": strconv.Itoa(last)})))
		w.Header().Set("Link", strings.Join(links, ", "))
		writeJSON(w, http.StatusOK, items)
	})
	mux.HandleFunc("GET /api/sentry/organizations/acme/issues/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+tok.Sentry {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"detail": "Invalid token"})
			return
		}
		const per = 2
		start := 0
		if c := r.URL.Query().Get("cursor"); c != "" {
			start, _ = strconv.Atoi(strings.TrimPrefix(c, "0:"))
		}
		start = max(0, min(start, len(sentryIssues)))
		end := min(start+per, len(sentryIssues))
		more := end < len(sentryIssues)
		// Sentry always sends rel="next" and says with results= whether it has data.
		w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="previous"; results="false"; cursor="0:0", <%s>; rel="next"; results="%t"; cursor="0:%d"`,
			selfURL(r, map[string]string{"cursor": "0:0"}), selfURL(r, map[string]string{"cursor": fmt.Sprintf("0:%d", end)}), more, end))
		writeJSON(w, http.StatusOK, sentryIssues[start:end])
	})
	mux.HandleFunc("GET /api/vercel/v6/deployments", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+tok.Vercel {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]string{"code": "forbidden"}})
			return
		}
		until := int64(math.MaxInt64)
		if u := r.URL.Query().Get("until"); u != "" {
			until, _ = strconv.ParseInt(u, 10, 64)
		}
		var page []map[string]any
		for _, d := range vercelDeployments {
			if d["created"].(int64) < until && len(page) < 2 {
				page = append(page, d)
			}
		}
		var next any
		if len(page) == 2 && page[1]["created"].(int64) > vercelDeployments[len(vercelDeployments)-1]["created"].(int64) {
			next = page[1]["created"]
		}
		writeJSON(w, http.StatusOK, map[string]any{"deployments": page, "pagination": map[string]any{"count": len(page), "next": next}})
	})
	mux.HandleFunc("GET /api/datadog/api/v2/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("DD-API-KEY") != tok.Datadog || r.Header.Get("DD-APPLICATION-KEY") == "" {
			writeJSON(w, http.StatusForbidden, map[string]any{"errors": []string{"Forbidden"}})
			return
		}
		off := intParam(r, "offset", 0, 0, 1_000_000)
		lim := intParam(r, "limit", 10, 1, 1000)
		end := min(off+lim, len(datadogLogs))
		var data []any
		for _, l := range datadogLogs[min(off, len(datadogLogs)):end] {
			data = append(data, map[string]any{"id": l["id"], "type": "log", "attributes": l})
		}
		if data == nil {
			data = []any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data})
	})
	mux.HandleFunc("GET /api/thousandeyes/v7/tests", func(w http.ResponseWriter, r *http.Request) {
		page := intParam(r, "page", 1, 1, 100)
		items, last := pageOf(thousandEyesTests, page, 2)
		links := map[string]any{"self": map[string]string{"href": r.URL.RequestURI()}}
		if page < last {
			// A relative reference, resolved against the current page (FR-072 AC4).
			links["next"] = map[string]string{"href": "/api/thousandeyes/v7/tests?page=" + strconv.Itoa(page+1)}
		}
		writeJSON(w, http.StatusOK, map[string]any{"tests": items, "_links": links})
	})
	mux.HandleFunc("GET /api/cloudflare/client/v4/zones", func(w http.ResponseWriter, r *http.Request) {
		page := intParam(r, "page", 1, 1, 1000)
		per := intParam(r, "per_page", 20, 1, 50)
		items, last := pageOf(cloudflareZones, page, per)
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "result": items,
			"result_info": map[string]int{"page": page, "per_page": per, "total_pages": last, "count": len(items), "total_count": len(cloudflareZones)}})
	})
	mux.HandleFunc("GET /api/dynatrace/api/v2/metrics/query", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"totalCount": 1, "nextPageKey": nil, "resolution": "1h", "result": []any{
			map[string]any{"metricId": "builtin:host.cpu.usage", "data": []any{map[string]any{
				"dimensions": []string{"HOST-1"}, "timestamps": []int64{1761955200000, 1761958800000, 1761962400000}, "values": []float64{1.5, 2, 2.5},
			}}},
		}})
	})
	mux.HandleFunc("GET /api/metrics/cpu", func(w http.ResponseWriter, _ *http.Request) {
		start := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
		series := []any{}
		for h, host := range []string{"web-1", "web-2", "db-1"} {
			var pts [][2]float64
			for i := range 48 {
				t := start.Add(time.Duration(i) * 30 * time.Minute)
				v := 30 + 15*math.Sin(float64(i)/6+float64(h)) + float64(h*12) + float64((i*7+h*3)%5)
				pts = append(pts, [2]float64{float64(t.UnixMilli()), math.Round(v*10) / 10})
			}
			series = append(series, map[string]any{"host": host, "points": pts})
		}
		writeJSON(w, http.StatusOK, map[string]any{"series": series})
	})
	mux.HandleFunc("GET /api/evil/redirect-pagination", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", `<http://attacker.example/steal>; rel="next"`)
		writeJSON(w, http.StatusOK, []any{map[string]int{"n": 1}})
	})
	mux.HandleFunc("GET /api/echo", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		keys := make([]string, 0, len(q))
		for k := range q {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := map[string]string{}
		for _, k := range keys {
			out[k] = q.Get(k)
		}
		writeJSON(w, http.StatusOK, out)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func intParam(r *http.Request, name string, def, lo, hi int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	return max(lo, min(v, hi))
}

func selfURL(r *http.Request, set map[string]string) string {
	q := r.URL.Query()
	for k, v := range set {
		q.Set(k, v)
	}
	u := url.URL{Scheme: "http", Host: r.Host, Path: r.URL.Path, RawQuery: q.Encode()}
	return u.String()
}

func pageOf[T any](all []T, page, per int) ([]T, int) {
	last := max(1, (len(all)+per-1)/per)
	start := (page - 1) * per
	if start >= len(all) {
		return []T{}, last
	}
	return all[start:min(start+per, len(all))], last
}

type m = map[string]any

var githubIssues = []m{
	{"number": 101, "title": "Dashboard export fails on Safari", "state": "open", "user": m{"login": "ana"}, "labels": []m{{"name": "bug"}}, "comments": 4, "created_at": "2026-11-01T09:12:00Z"},
	{"number": 102, "title": "Add Loki connector preset", "state": "open", "user": m{"login": "ben"}, "labels": []m{{"name": "connector"}}, "comments": 2, "created_at": "2026-11-03T14:30:00Z"},
	{"number": 103, "title": "Time picker ignores browser time zone", "state": "closed", "user": m{"login": "ana"}, "labels": []m{{"name": "bug"}, {"name": "ui"}}, "comments": 7, "created_at": "2026-11-05T08:01:00Z"},
	{"number": 104, "title": "Support Datadog EU site", "state": "open", "user": m{"login": "cy"}, "labels": []m{{"name": "connector"}}, "comments": 1, "created_at": "2026-11-12T17:45:00Z"},
	{"number": 105, "title": "Join two queries by host", "state": "open", "user": m{"login": "ben"}, "labels": []m{{"name": "pipeline"}}, "comments": 0, "created_at": "2026-11-20T11:20:00Z"},
}

var sentryIssues = []m{
	{"id": "4501", "title": "TypeError: Cannot read properties of undefined", "level": "error", "count": "1532", "userCount": 211, "lastSeen": "2026-11-25T10:02:00Z"},
	{"id": "4502", "title": "TimeoutError: upstream took longer than 30s", "level": "warning", "count": "418", "userCount": 37, "lastSeen": "2026-11-25T09:41:00Z"},
	{"id": "4503", "title": "ZeroDivisionError in billing.rollup", "level": "error", "count": "96", "userCount": 4, "lastSeen": "2026-11-24T22:15:00Z"},
}

var vercelDeployments = []map[string]any{
	{"uid": "dpl_4", "name": "dv-docs", "state": "READY", "target": "production", "created": int64(1764064800000)},
	{"uid": "dpl_3", "name": "dv-web", "state": "READY", "target": "preview", "created": int64(1764043200000)},
	{"uid": "dpl_2", "name": "dv-web", "state": "ERROR", "target": "preview", "created": int64(1763978400000)},
	{"uid": "dpl_1", "name": "dv-web", "state": "READY", "target": "production", "created": int64(1763892000000)},
}

var datadogLogs = []m{
	{"id": "L1", "timestamp": "2026-11-25T10:00:00Z", "service": "checkout", "status": "error", "message": "payment declined", "host": "web-1"},
	{"id": "L2", "timestamp": "2026-11-25T10:00:05Z", "service": "checkout", "status": "info", "message": "order created", "host": "web-2"},
	{"id": "L3", "timestamp": "2026-11-25T10:00:09Z", "service": "search", "status": "warn", "message": "slow query 1.8s", "host": "web-1"},
	{"id": "L4", "timestamp": "2026-11-25T10:00:12Z", "service": "auth", "status": "info", "message": "token refreshed", "host": "web-3"},
	{"id": "L5", "timestamp": "2026-11-25T10:00:20Z", "service": "checkout", "status": "error", "message": "inventory timeout", "host": "web-2"},
	{"id": "L6", "timestamp": "2026-11-25T10:00:31Z", "service": "search", "status": "info", "message": "index rebuilt", "host": "web-1"},
	{"id": "L7", "timestamp": "2026-11-25T10:00:44Z", "service": "auth", "status": "warn", "message": "rate limit near", "host": "web-3"},
}

var thousandEyesTests = []m{
	{"testId": "281", "testName": "checkout.example – HTTP", "type": "http-server", "interval": 60, "enabled": true},
	{"testId": "282", "testName": "api.example – network", "type": "agent-to-server", "interval": 120, "enabled": true},
	{"testId": "283", "testName": "dns – example.com", "type": "dns-server", "interval": 300, "enabled": false},
	{"testId": "284", "testName": "login page – web transaction", "type": "web-transactions", "interval": 300, "enabled": true},
	{"testId": "285", "testName": "BGP – AS64500", "type": "bgp", "interval": 900, "enabled": true},
}

var cloudflareZones = []m{
	{"id": "z1", "name": "example.com", "status": "active", "plan": m{"name": "Enterprise"}},
	{"id": "z2", "name": "example.net", "status": "active", "plan": m{"name": "Pro"}},
	{"id": "z3", "name": "example.org", "status": "pending", "plan": m{"name": "Free"}},
	{"id": "z4", "name": "dv.example", "status": "active", "plan": m{"name": "Business"}},
	{"id": "z5", "name": "status.example", "status": "moved", "plan": m{"name": "Free"}},
}
