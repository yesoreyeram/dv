package server

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/yesoreyeram/dv/internal/httpx"
)

// Guards for the pre-authentication data-access endpoints (NFR-SEC-017).
//
// A loopback bind (NFR-SEC-016) does not stop DNS rebinding: a page on attacker.example
// can re-point its own name at 127.0.0.1, and the browser then treats requests to the dv
// port as same-origin, so the page could read every exposed file. Browsers send the name
// they resolved in the Host header, so accepting only loopback host names closes that path.
// Cross-site requests are refused separately, so a page cannot fire queries blind either.

// isLoopbackHost reports whether a Host header value names this machine in a way DNS
// cannot rebind: "localhost", "*.localhost" (RFC 6761) or a loopback IP literal.
func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Unmap().IsLoopback()
}

// crossSite reports whether a browser marked the request as coming from another site,
// either through Fetch Metadata or through an Origin that is not this host.
func crossSite(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	// "null" (sandboxed frames, file: pages) and anything unparsable count as foreign.
	scheme, rest, ok := strings.Cut(origin, "://")
	if !ok || (scheme != "http" && scheme != "https") || rest == "" || strings.ContainsAny(rest, "/?#@") {
		return true
	}
	return !strings.EqualFold(rest, r.Host)
}

// guardHost refuses requests whose Host is not a loopback name while the query API is
// loopback-only (NFR-SEC-017 AC1, AC4). It returns false after writing the response.
func (s *Server) guardHost(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.QueryAPIAllowRemote || isLoopbackHost(r.Host) {
		return true
	}
	httpx.WriteError(w, r, http.StatusForbidden, "host_not_allowed",
		"this endpoint only answers requests addressed to localhost or a loopback IP")
	return false
}

// guardCrossSite refuses browser requests from another site (NFR-SEC-017 AC2).
func (s *Server) guardCrossSite(w http.ResponseWriter, r *http.Request) bool {
	if !crossSite(r) {
		return true
	}
	httpx.WriteError(w, r, http.StatusForbidden, "cross_site_request",
		"cross-site requests to this endpoint are not allowed")
	return false
}
