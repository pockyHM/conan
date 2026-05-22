package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pockyHM/conan/internal/tools"
	"github.com/pockyHM/conan/pkg/configschema"
)

type Server struct {
	config   *configschema.AgentConfig
	handler  *Handler
	http     *http.Server
	registry *tools.Registry
}

func NewServer(cfg *configschema.AgentConfig, registry *tools.Registry, version string) *Server {
	handler := NewHandler(registry, version)

	var rpcHandler http.Handler = handler
	rpcHandler = auditMiddleware(cfg.AuditLog)(rpcHandler)
	rpcHandler = rateLimitMiddleware(cfg.RateLimit)(rpcHandler)
	rpcHandler = authMiddleware(cfg.Token)(rpcHandler)

	var filesHandler http.Handler = fileHandler{}
	filesHandler = auditMiddleware(cfg.AuditLog)(filesHandler)
	filesHandler = rateLimitMiddleware(cfg.RateLimit)(filesHandler)
	filesHandler = authMiddleware(cfg.Token)(filesHandler)

	mux := http.NewServeMux()
	mux.Handle("/rpc", rpcHandler)
	mux.Handle("/files/download", filesHandler)
	mux.Handle("/files/upload", filesHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	return &Server{
		config:   cfg,
		handler:  handler,
		registry: registry,
		http: &http.Server{
			Addr:    cfg.Listen,
			Handler: mux,
		},
	}
}

func (s *Server) Start() error {
	slog.Info("starting agent server", "listen", s.config.Listen, "tls", s.config.TLS)
	if s.config.TLS {
		return s.http.ListenAndServeTLS(s.config.TLSCert, s.config.TLSKey)
	}
	return s.http.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("shutting down agent server")
	return s.http.Shutdown(ctx)
}

func (s *Server) WaitForSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	for sig := range sigCh {
		switch sig {
		case syscall.SIGHUP:
			slog.Info("received SIGHUP, reloading config")
		case syscall.SIGINT, syscall.SIGTERM:
			slog.Info("received signal, shutting down", "signal", sig)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.Shutdown(ctx); err != nil {
				slog.Error("shutdown error", "error", err)
			}
			return
		}
	}
}
