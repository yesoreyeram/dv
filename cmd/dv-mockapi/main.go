// Command dv-mockapi serves deterministic vendor-API fixtures (GitHub, Sentry, Vercel,
// Datadog, ThousandEyes, Cloudflare, Dynatrace) for the prototype and UI scenarios
// (NFR-MAINT-009 AC2). It binds to loopback by default and holds no real credentials.
package main

import (
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/yesoreyeram/dv/internal/mockapi"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9090", "listen address")
	flag.Parse()
	srv := &http.Server{
		Addr:              *addr,
		Handler:           mockapi.Handler(mockapi.DefaultTokens),
		ReadHeaderTimeout: 5 * time.Second,
	}
	slog.Info("dv-mockapi listening", "addr", *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("dv-mockapi stopped", "error", err)
		os.Exit(1)
	}
}
