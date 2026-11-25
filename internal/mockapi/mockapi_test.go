package mockapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEndpoints(t *testing.T) {
	h := Handler(DefaultTokens)
	cases := []struct {
		path, header, value string
		status              int
		contains            string
	}{
		{"/api/github/repos/acme/web/issues?per_page=2", "Authorization", "Bearer s3cr3t-token", 200, `"number":101`},
		{"/api/github/repos/acme/web/issues", "Authorization", "Bearer nope", 401, "Bad credentials"},
		{"/api/sentry/organizations/acme/issues/", "Authorization", "Bearer sentry-token", 200, `"id":"4501"`},
		{"/api/sentry/organizations/acme/issues/", "", "", 401, "Invalid token"},
		{"/api/vercel/v6/deployments", "Authorization", "Bearer v-token", 200, `"next":1764043200000`},
		{"/api/vercel/v6/deployments", "", "", 403, "forbidden"},
		{"/api/datadog/api/v2/logs?offset=6&limit=3", "DD-API-KEY", "dd-api-key", 403, "Forbidden"},
		{"/api/thousandeyes/v7/tests", "", "", 200, `"href":"/api/thousandeyes/v7/tests?page=2"`},
		{"/api/cloudflare/client/v4/zones?per_page=2&page=3", "", "", 200, `"total_pages":3`},
		{"/api/cloudflare/client/v4/zones?per_page=2&page=9", "", "", 200, `"result":[]`},
		{"/api/dynatrace/api/v2/metrics/query", "", "", 200, `"values":[1.5,2,2.5]`},
		{"/api/metrics/cpu", "", "", 200, `"host":"db-1"`},
		{"/api/evil/redirect-pagination", "", "", 200, `"n":1`},
		{"/api/echo?b=2&a=1", "", "", 200, `{"a":"1","b":"2"}`},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.path, nil)
		if c.header != "" {
			req.Header.Set(c.header, c.value)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		body, _ := io.ReadAll(rec.Body)
		if rec.Code != c.status || !strings.Contains(string(body), c.contains) {
			t.Errorf("%s: %d %s", c.path, rec.Code, body)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/datadog/api/v2/logs?offset=6&limit=3", nil)
	req.Header.Set("DD-API-KEY", "dd-api-key")
	req.Header.Set("DD-APPLICATION-KEY", "x")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"L7"`) || strings.Contains(rec.Body.String(), `"L6"`) {
		t.Errorf("datadog offset page: %s", rec.Body.String())
	}
}
