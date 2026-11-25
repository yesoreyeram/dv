// Package netguard builds the outbound HTTP client handed to connector plugins. It is
// the SSRF guard of NFR-SEC-005: every connection's resolved address is checked at dial
// time against blocked special-purpose ranges, ambient proxies are ignored, TLS is
// verified, and timeouts and response sizes are bounded (ADR-0002, ADR-0008).
package netguard

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// ErrBlocked is returned (wrapped) when a destination address is in a blocked range.
var ErrBlocked = errors.New("destination address is in a blocked range")

// BlockedError reports the category of the blocked range; it matches ErrBlocked.
type BlockedError struct{ Category string }

func (e *BlockedError) Error() string { return ErrBlocked.Error() + " (" + e.Category + ")" }

// Is makes errors.Is(err, ErrBlocked) true.
func (e *BlockedError) Is(target error) bool { return target == ErrBlocked }

func blockedErr(cat string) error { return &BlockedError{Category: cat} }

// ErrTooLarge is returned by LimitBody readers once the cap is exceeded.
var ErrTooLarge = errors.New("response body exceeds the size limit")

// blockedRange is a special-purpose network with a category name used in errors.
type blockedRange struct {
	prefix   netip.Prefix
	category string
}

func mustPrefixes(category string, cidrs ...string) []blockedRange {
	out := make([]blockedRange, len(cidrs))
	for i, c := range cidrs {
		out[i] = blockedRange{netip.MustParsePrefix(c), category}
	}
	return out
}

// blocked lists the ranges refused by default (NFR-SEC-005 AC2; IANA special-purpose
// registries for IPv4 and IPv6).
var blocked = concat(
	mustPrefixes("unspecified", "0.0.0.0/8", "::/128"),
	mustPrefixes("loopback", "127.0.0.0/8", "::1/128"),
	mustPrefixes("private", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"),
	mustPrefixes("shared address space (CGNAT)", "100.64.0.0/10"),
	mustPrefixes("link-local (includes cloud metadata)", "169.254.0.0/16", "fe80::/10"),
	mustPrefixes("IETF protocol assignments", "192.0.0.0/24"),
	mustPrefixes("documentation", "192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32", "3fff::/20"),
	mustPrefixes("benchmarking", "198.18.0.0/15", "2001:2::/48"),
	mustPrefixes("multicast", "224.0.0.0/4", "ff00::/8"),
	mustPrefixes("reserved", "240.0.0.0/4"),
	mustPrefixes("unique-local", "fc00::/7"),
	mustPrefixes("NAT64", "64:ff9b::/96", "64:ff9b:1::/48"),
	mustPrefixes("6to4", "2002::/16"),
	mustPrefixes("Teredo", "2001::/32"),
	mustPrefixes("discard-only", "100::/64"),
	mustPrefixes("site-local (deprecated)", "fec0::/10"),
)

func concat(parts ...[]blockedRange) []blockedRange {
	var out []blockedRange
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// Policy decides which destination addresses may be dialled.
type Policy struct {
	// Allowed lists networks exempt from the block list (operator configuration,
	// NFR-SEC-005 AC3). An address in an allowed prefix is permitted even when it is in
	// a blocked range.
	Allowed []netip.Prefix
}

// ParseAllowed parses CIDR strings (or single addresses) for Policy.Allowed.
func ParseAllowed(cidrs []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		if p, err := netip.ParsePrefix(c); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(c)
		if err != nil {
			return nil, fmt.Errorf("%q is not a CIDR or IP address", c)
		}
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

// Sensitive reports the blocked-range categories an allowed prefix overlaps that deserve
// a start-up warning (loopback, link-local, unspecified).
func Sensitive(p netip.Prefix) []string {
	var cats []string
	seen := map[string]bool{}
	for _, b := range blocked {
		switch b.category {
		case "loopback", "link-local (includes cloud metadata)", "unspecified":
		default:
			continue
		}
		if b.prefix.Overlaps(p) && !seen[b.category] {
			seen[b.category] = true
			cats = append(cats, b.category)
		}
	}
	return cats
}

// Check returns nil when addr may be dialled, otherwise an error wrapping ErrBlocked
// that names the range category but not the address (NFR-SEC-005 AC8).
func (p Policy) Check(addr netip.Addr) error {
	addr = addr.Unmap().WithZone("")
	if !addr.IsValid() {
		return blockedErr("invalid address")
	}
	for _, a := range p.Allowed {
		if a.Contains(addr) {
			return nil
		}
	}
	for _, b := range blocked {
		if b.prefix.Contains(addr) {
			return blockedErr(b.category)
		}
	}
	if addr.Is4() && addr == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
		return blockedErr("broadcast")
	}
	return nil
}

// control is the net.Dialer Control hook: it sees the address actually being dialled,
// after DNS resolution, for every connection attempt (including redirects and every
// address of a multi-address name), which defeats DNS rebinding.
func (p Policy) control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return blockedErr("unparseable address")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return blockedErr("unparseable address")
	}
	return p.Check(ip)
}

// Options tune the client. Zero members use the defaults.
type Options struct {
	ConnectTimeout        time.Duration // default 10 s
	TLSHandshakeTimeout   time.Duration // default 10 s
	ResponseHeaderTimeout time.Duration // default 30 s
	// CheckRedirect is installed on the client; nil means redirects are refused.
	CheckRedirect func(req *http.Request, via []*http.Request) error
	// Resolver overrides DNS (tests); nil uses the system resolver.
	Resolver *net.Resolver
	// RootCAs overrides the trusted roots (tests); nil uses the system pool.
	TLS *tls.Config
}

// NewClient returns an HTTP client that enforces p (NFR-SEC-005 AC2–AC7). The client
// ignores HTTP(S)_PROXY, requires TLS 1.2+, and has no overall timeout: callers bound
// each request with a context deadline.
func NewClient(p Policy, o Options) *http.Client {
	if o.ConnectTimeout <= 0 {
		o.ConnectTimeout = 10 * time.Second
	}
	if o.TLSHandshakeTimeout <= 0 {
		o.TLSHandshakeTimeout = 10 * time.Second
	}
	if o.ResponseHeaderTimeout <= 0 {
		o.ResponseHeaderTimeout = 30 * time.Second
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if o.TLS != nil {
		tlsCfg = o.TLS.Clone()
		if tlsCfg.MinVersion < tls.VersionTLS12 {
			tlsCfg.MinVersion = tls.VersionTLS12
		}
		tlsCfg.InsecureSkipVerify = false
	}
	dialer := &net.Dialer{Timeout: o.ConnectTimeout, KeepAlive: 30 * time.Second, Control: p.control, Resolver: o.Resolver}
	tr := &http.Transport{
		Proxy:                  nil, // NFR-SEC-005 AC4: never an ambient proxy
		DialContext:            dialer.DialContext,
		TLSClientConfig:        tlsCfg,
		TLSHandshakeTimeout:    o.TLSHandshakeTimeout,
		ResponseHeaderTimeout:  o.ResponseHeaderTimeout,
		ExpectContinueTimeout:  time.Second,
		MaxResponseHeaderBytes: 1 << 20,
		MaxIdleConns:           32,
		MaxIdleConnsPerHost:    4,
		IdleConnTimeout:        90 * time.Second,
		ForceAttemptHTTP2:      true,
	}
	check := o.CheckRedirect
	if check == nil {
		check = func(*http.Request, []*http.Request) error { return errors.New("redirects are not followed") }
	}
	return &http.Client{Transport: tr, CheckRedirect: check}
}

// LimitBody returns a reader over r that fails with ErrTooLarge once more than max bytes
// were read. It counts decoded bytes, so a compressed body is capped at its expanded
// size (NFR-SEC-005 AC6).
func LimitBody(r io.Reader, max int64) io.Reader { return &limitReader{r: r, left: max} }

type limitReader struct {
	r    io.Reader
	left int64
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.left < 0 {
		return 0, ErrTooLarge
	}
	if int64(len(p)) > l.left+1 {
		p = p[:l.left+1]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	if l.left < 0 {
		return n, ErrTooLarge
	}
	return n, err
}
