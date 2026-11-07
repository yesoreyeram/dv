// Package server wires the HTTP router, middleware chain, API handlers, static SPA
// serving and the server lifecycle (graceful shutdown, NFR-AVAIL-001).
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/yesoreyeram/dv/api"
	"github.com/yesoreyeram/dv/internal/buildinfo"
	"github.com/yesoreyeram/dv/internal/config"
	"github.com/yesoreyeram/dv/internal/health"
	"github.com/yesoreyeram/dv/internal/httpx"
)

// Options configures a Server.
type Options struct {
	Config config.Config
	Logger *slog.Logger
	Health *health.Registry
	Build  buildinfo.Info
	// ExtraRoutes lets tests register additional routes behind the full middleware chain.
	ExtraRoutes func(mux *http.ServeMux)
}

// Server is the dv HTTP server.
type Server struct {
	cfg     config.Config
	log     *slog.Logger
	health  *health.Registry
	build   buildinfo.Info
	handler http.Handler
}

// route is a public endpoint. Every entry must be described in api/openapi.json
// (enforced by TestOpenAPICoversRoutes).
type route struct {
	method, path string
	handler      http.HandlerFunc
}

// probePaths are exempt from rate limiting so orchestrator probes are never throttled.
var probePaths = map[string]bool{"/healthz": true, "/readyz": true}

// New builds a server. It returns an error if the static web directory cannot be opened.
func New(opts Options) (*Server, error) {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Health == nil {
		opts.Health = health.New(opts.Config.ReadinessCheckTimeout.Std(), opts.Logger)
	}
	s := &Server{cfg: opts.Config, log: opts.Logger, health: opts.Health, build: opts.Build}

	mux := http.NewServeMux()
	for _, rt := range s.routes() {
		mux.HandleFunc(rt.method+" "+rt.path, rt.handler)
	}
	mux.HandleFunc("GET /api/", s.notFound)
	if opts.ExtraRoutes != nil {
		opts.ExtraRoutes(mux)
	}
	if opts.Config.WebDir != "" {
		static, err := newStaticHandler(opts.Config.WebDir)
		if err != nil {
			return nil, err
		}
		mux.Handle("GET /", static)
	} else {
		mux.HandleFunc("GET /", s.notFound)
	}

	var limited http.Handler = mux
	if opts.Config.RateLimitRPS > 0 {
		rl := httpx.NewRateLimiter(opts.Config.RateLimitRPS, opts.Config.RateLimitBurst, 100_000, nil)
		limitedMux := rl.Middleware(mux)
		limited = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if probePaths[r.URL.Path] {
				mux.ServeHTTP(w, r)
				return
			}
			limitedMux.ServeHTTP(w, r)
		})
	}

	s.handler = httpx.Chain(limited,
		httpx.RequestID,
		httpx.TraceContext,
		httpx.AccessLog(s.log),
		httpx.SecurityHeaders,
		httpx.Recoverer(s.log),
		httpx.MaxBody(opts.Config.MaxBodyBytes),
	)
	return s, nil
}

func (s *Server) routes() []route {
	return []route{
		{http.MethodGet, "/healthz", s.liveness},
		{http.MethodGet, "/readyz", s.readiness},
		{http.MethodGet, "/api/v1/version", s.version},
		{http.MethodGet, "/api/openapi.json", s.openapi},
	}
}

// Handler returns the fully wrapped root handler.
func (s *Server) Handler() http.Handler { return s.handler }

// Health returns the readiness registry.
func (s *Server) Health() *health.Registry { return s.health }

func (s *Server) liveness(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readiness(w http.ResponseWriter, r *http.Request) {
	rep := s.health.Check(r.Context())
	status := http.StatusOK
	if !rep.Ready {
		status = http.StatusServiceUnavailable
	}
	httpx.WriteJSON(w, status, rep)
}

func (s *Server) version(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, s.build)
}

func (s *Server) openapi(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(api.OpenAPI)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	httpx.WriteError(w, r, http.StatusNotFound, "not_found", "resource not found")
}

// newHTTPServer applies the timeouts from configuration (NFR-SEC-013).
func (s *Server) newHTTPServer() *http.Server {
	return &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: s.cfg.ReadHeaderTimeout.Std(),
		ReadTimeout:       s.cfg.ReadTimeout.Std(),
		WriteTimeout:      s.cfg.WriteTimeout.Std(),
		IdleTimeout:       s.cfg.IdleTimeout.Std(),
		MaxHeaderBytes:    s.cfg.MaxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}
}

// ListenAndServe listens on the configured address and serves until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.cfg.HTTPAddr, err)
	}
	return s.Serve(ctx, ln)
}

// Serve serves on ln until ctx is cancelled, then shuts down gracefully: readiness
// flips to 503 immediately and in-flight requests drain within ShutdownTimeout.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := s.newHTTPServer()
	srv.BaseContext = func(net.Listener) context.Context { return context.WithoutCancel(ctx) }
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	s.log.InfoContext(ctx, "server listening", "addr", ln.Addr().String(),
		"version", s.build.Version, "web_ui", s.cfg.WebDir != "")

	select {
	case err := <-errCh:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	s.health.SetDraining()
	s.log.Info("shutting down", "timeout", s.cfg.ShutdownTimeout.Std().String())
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.ShutdownTimeout.Std())
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	s.log.Info("server stopped")
	return nil
}

// isAssetPath reports whether the request targets a file (has an extension) rather than
// an SPA route.
func isAssetPath(p string) bool {
	last := p[strings.LastIndex(p, "/")+1:]
	return strings.Contains(last, ".")
}
