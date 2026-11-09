package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsLoopbackHost(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost": true, "LOCALHOST:8080": true, "localhost.": true, "app.localhost:1": true,
		"127.0.0.1": true, "127.8.9.10:80": true, "[::1]:8080": true, "::1": true,
		"[::ffff:127.0.0.1]:80": true,
		"":                      false, "example.com": false, "localhost.attacker.io": false, "notlocalhost": false,
		"10.0.0.5:8080": false, "0.0.0.0": false, "[::]:80": false, "127.0.0.1.nip.io": false,
		"192.168.1.1": false,
	} {
		if got := isLoopbackHost(host); got != want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestCrossSite(t *testing.T) {
	cases := []struct {
		hdr  map[string]string
		want bool
	}{
		{nil, false},
		{map[string]string{"Sec-Fetch-Site": "same-origin"}, false},
		{map[string]string{"Sec-Fetch-Site": "none"}, false},
		{map[string]string{"Sec-Fetch-Site": "same-site"}, true},
		{map[string]string{"Sec-Fetch-Site": "cross-site"}, true},
		{map[string]string{"Origin": "http://127.0.0.1:8080"}, false},
		{map[string]string{"Origin": "https://127.0.0.1:8080"}, false},
		{map[string]string{"Origin": "HTTP://127.0.0.1:8080"}, true}, // scheme must be lower-case, as browsers send it
		{map[string]string{"Origin": "http://127.0.0.1:9090"}, true},
		{map[string]string{"Origin": "http://127.0.0.1"}, true},
		{map[string]string{"Origin": "null"}, true},
		{map[string]string{"Origin": "file://"}, true},
		{map[string]string{"Origin": "http://127.0.0.1:8080/x"}, true},
		{map[string]string{"Origin": "http://u@127.0.0.1:8080"}, true},
		{map[string]string{"Origin": "http://127.0.0.1:8080", "Sec-Fetch-Site": "cross-site"}, true},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Host = "127.0.0.1:8080"
		for k, v := range c.hdr {
			r.Header.Set(k, v)
		}
		if got := crossSite(r); got != c.want {
			t.Errorf("crossSite(%v) = %v, want %v", c.hdr, got, c.want)
		}
	}
}

func TestQueryEndpointsRefuseRebindingHosts(t *testing.T) {
	s := newQueryServer(t, true)
	h := s.Handler()
	for _, path := range []string{"/api/v1/datasources", "/api/v1/query"} {
		method := http.MethodGet
		if path == "/api/v1/query" {
			method = http.MethodPost
		}
		rr := doHost(t, h, method, path, "attacker.example:8080", nil)
		if rr.Code != http.StatusForbidden || errCode(t, rr) != "host_not_allowed" {
			t.Fatalf("%s: %d %s", path, rr.Code, rr.Body)
		}
		if strings.Contains(rr.Body.String(), "attacker") {
			t.Fatalf("%s echoes the host: %s", path, rr.Body)
		}
		if rr := doHost(t, h, method, path, "localhost:8080", nil); rr.Code != http.StatusOK {
			t.Fatalf("%s localhost: %d %s", path, rr.Code, rr.Body)
		}
	}
	// Other endpoints are not gated (the shell must keep working behind any name).
	if rr := doHost(t, h, http.MethodGet, "/api/v1/version", "attacker.example", nil); rr.Code != http.StatusOK {
		t.Fatalf("version: %d", rr.Code)
	}
}

func TestQueryRefusesCrossSiteBeforeReadingBody(t *testing.T) {
	s := newQueryServer(t, true)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/query", strings.NewReader("not json"))
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	// No Content-Type: the guard must answer before the 415 check and the body decode.
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden || errCode(t, rr) != "cross_site_request" {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	// A cross-site GET of the list is not refused by AC2 (it cannot be read cross-origin
	// without CORS), only by the host check.
	if rr := doHost(t, s.Handler(), http.MethodGet, "/api/v1/datasources", "127.0.0.1:8080",
		map[string]string{"Sec-Fetch-Site": "cross-site"}); rr.Code != http.StatusOK {
		t.Fatalf("datasources: %d", rr.Code)
	}
}

func TestRemoteOptInSkipsHostCheckButNotCrossSite(t *testing.T) {
	s := newQueryServer(t, true)
	s.cfg.QueryAPIAllowRemote = true
	h := s.Handler()
	if rr := doHost(t, h, http.MethodPost, "/api/v1/query", "dv.internal.example", nil); rr.Code != http.StatusOK {
		t.Fatalf("remote host: %d %s", rr.Code, rr.Body)
	}
	rr := doHost(t, h, http.MethodPost, "/api/v1/query", "dv.internal.example",
		map[string]string{"Origin": "http://attacker.example"})
	if rr.Code != http.StatusForbidden || errCode(t, rr) != "cross_site_request" {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	if rr := doHost(t, h, http.MethodPost, "/api/v1/query", "dv.internal.example",
		map[string]string{"Origin": "https://dv.internal.example"}); rr.Code != http.StatusOK {
		t.Fatalf("same origin: %d %s", rr.Code, rr.Body)
	}
}
