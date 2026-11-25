package netguard

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestCheckBlocksSpecialRanges(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1":              "loopback",
		"127.1.2.3":              "loopback",
		"::1":                    "loopback",
		"::ffff:127.0.0.1":       "loopback", // IPv4-mapped
		"0.0.0.0":                "unspecified",
		"::":                     "unspecified",
		"10.1.2.3":               "private",
		"172.31.255.255":         "private",
		"192.168.0.10":           "private",
		"100.64.0.1":             "shared address space (CGNAT)",
		"169.254.169.254":        "link-local (includes cloud metadata)",
		"fe80::1":                "link-local (includes cloud metadata)",
		"fd00:ec2::254":          "unique-local",
		"192.0.2.1":              "documentation",
		"2001:db8::1":            "documentation",
		"198.18.0.1":             "benchmarking",
		"224.0.0.1":              "multicast",
		"ff02::1":                "multicast",
		"240.0.0.1":              "reserved",
		"255.255.255.255":        "reserved",
		"64:ff9b::7f00:1":        "NAT64",
		"2002:7f00:1::":          "6to4",
		"2001:0:4136:e378::":     "Teredo",
		"192.0.0.8":              "IETF protocol assignments",
		"fec0::1":                "site-local (deprecated)",
		"::ffff:169.254.169.254": "link-local (includes cloud metadata)",
		"::ffff:10.0.0.1":        "private",
		"100::1":                 "discard-only",
		"3fff::1":                "documentation",
		"2001:2::1":              "benchmarking",
		"64:ff9b:1::1":           "NAT64",
		"fc00::1":                "unique-local",
		"0.1.2.3":                "unspecified",
		"203.0.113.9":            "documentation",
		"198.51.100.1":           "documentation",
		"172.16.0.1":             "private",
		"fe80::1%eth0":           "link-local (includes cloud metadata)",
		"::ffff:0:0":             "unspecified",
		"100.127.255.255":        "shared address space (CGNAT)",
		"224.0.0.251":            "multicast",
		"239.255.255.250":        "multicast",
		"::ffff:192.168.1.1":     "private",
		"::ffff:100.64.0.1":      "shared address space (CGNAT)",
		"::ffff:224.0.0.1":       "multicast",
		"::ffff:240.0.0.1":       "reserved",
		"::ffff:198.18.0.1":      "benchmarking",
		"::ffff:192.0.2.1":       "documentation",
		"::ffff:0.0.0.1":         "unspecified",
		"::ffff:127.255.255.254": "loopback",
		"::ffff:172.20.0.1":      "private",
		"::ffff:203.0.113.1":     "documentation",
		"::ffff:198.51.100.2":    "documentation",
		"::ffff:192.0.0.1":       "IETF protocol assignments",
		"::ffff:169.254.0.1":     "link-local (includes cloud metadata)",
		"::ffff:255.255.255.255": "reserved",
		"::ffff:100.100.100.200": "shared address space (CGNAT)",         // Alibaba metadata
		"::ffff:169.254.170.2":   "link-local (includes cloud metadata)", // ECS task metadata
	}
	var p Policy
	for s, cat := range cases {
		a := netip.MustParseAddr(s)
		err := p.Check(a)
		var be *BlockedError
		if !errors.Is(err, ErrBlocked) || !errors.As(err, &be) || be.Category != cat {
			t.Errorf("Check(%s) = %v, want blocked as %q", s, err, cat)
		}
		if err != nil && strings.Contains(err.Error(), s) {
			t.Errorf("Check(%s) error reveals the address: %v", s, err)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "140.82.112.3", "::ffff:8.8.8.8", "172.32.0.1", "100.128.0.1", "192.169.0.1"} {
		if err := p.Check(netip.MustParseAddr(s)); err != nil {
			t.Errorf("Check(%s) = %v, want allowed", s, err)
		}
	}
	if err := p.Check(netip.Addr{}); !errors.Is(err, ErrBlocked) {
		t.Errorf("invalid address: %v", err)
	}
}

func TestAllowedExemptions(t *testing.T) {
	allowed, err := ParseAllowed([]string{"127.0.0.1", "10.20.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	p := Policy{Allowed: allowed}
	for _, s := range []string{"127.0.0.1", "::ffff:127.0.0.1", "10.20.3.4"} {
		if err := p.Check(netip.MustParseAddr(s)); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	for _, s := range []string{"127.0.0.2", "10.21.0.1", "169.254.169.254"} {
		if err := p.Check(netip.MustParseAddr(s)); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s: %v, want blocked", s, err)
		}
	}
	if _, err := ParseAllowed([]string{"nope"}); err == nil {
		t.Error("ParseAllowed accepted garbage")
	}
	if got := Sensitive(netip.MustParsePrefix("127.0.0.1/32")); len(got) != 1 || got[0] != "loopback" {
		t.Errorf("Sensitive = %v", got)
	}
	if got := Sensitive(netip.MustParsePrefix("0.0.0.0/0")); len(got) != 3 {
		t.Errorf("Sensitive(0/0) = %v", got)
	}
	if got := Sensitive(netip.MustParsePrefix("10.0.0.0/8")); len(got) != 0 {
		t.Errorf("Sensitive(10/8) = %v", got)
	}
}

func TestClientBlocksAtDialTime(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits++ }))
	defer srv.Close()
	c := NewClient(Policy{}, Options{})
	// By IP and by a name that resolves to loopback: the check runs on the resolved
	// address, so a public-looking name pointing inward (DNS rebinding) is refused too.
	for _, u := range []string{srv.URL, strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)} {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
		resp, err := c.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
		var be *BlockedError
		if !errors.As(err, &be) || be.Category != "loopback" {
			t.Errorf("GET %s: err = %v, want blocked (loopback)", u, err)
		}
	}
	if hits != 0 {
		t.Fatalf("server was reached %d times", hits)
	}
	allowed, _ := ParseAllowed([]string{"127.0.0.1"})
	c = NewClient(Policy{Allowed: allowed}, Options{})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if hits != 1 {
		t.Fatalf("hits = %d", hits)
	}
}

func TestClientIgnoresProxyAndRefusesRedirectsByDefault(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/r" {
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	allowed, _ := ParseAllowed([]string{"127.0.0.1"})
	c := NewClient(Policy{Allowed: allowed}, Options{})
	if c.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("transport uses a proxy")
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/ok", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("direct request failed (proxy used?): %v", err)
	}
	_ = resp.Body.Close()
	req, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/r", nil)
	resp, err = c.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("redirect followed")
	}
	if c.Transport.(*http.Transport).TLSClientConfig.MinVersion < 0x0303 {
		t.Fatal("TLS minimum below 1.2")
	}
}

func TestLimitBodyCountsDecompressedBytes(t *testing.T) {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write(bytes.Repeat([]byte("a"), 1<<20)) // 1 MiB of 'a' compresses to ~1 KiB
	_ = zw.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(gz.Bytes())
	}))
	defer srv.Close()
	allowed, _ := ParseAllowed([]string{"127.0.0.1"})
	c := NewClient(Policy{Allowed: allowed}, Options{})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, err = io.ReadAll(LimitBody(resp.Body, 64<<10))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	// Exactly at the limit is fine.
	b, err := io.ReadAll(LimitBody(strings.NewReader("12345"), 5))
	if err != nil || string(b) != "12345" {
		t.Fatalf("at limit: %q %v", b, err)
	}
	if _, err := io.ReadAll(LimitBody(strings.NewReader("123456"), 5)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over limit: %v", err)
	}
}
