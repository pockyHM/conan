package agent

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const nodeIDKey contextKey = "node_id"

func authMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if strings.TrimPrefix(auth, "Bearer ") != token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func auditMiddleware(logPath string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			slog.Info("rpc call",
				"method", r.Method,
				"path", r.URL.Path,
				"remote", r.RemoteAddr,
				"duration", time.Since(start),
			)
		})
	}
}

func rateLimitMiddleware(rps int) func(http.Handler) http.Handler {
	limiter := make(chan struct{}, rps)
	for i := 0; i < rps; i++ {
		limiter <- struct{}{}
	}
	go func() {
		for range time.Tick(time.Second) {
			for i := len(limiter); i < rps; i++ {
				select {
				case limiter <- struct{}{}:
				default:
				}
			}
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-limiter:
				next.ServeHTTP(w, r)
			default:
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			}
		})
	}
}

func ContextWithNodeID(ctx context.Context, nodeID string) context.Context {
	return context.WithValue(ctx, nodeIDKey, nodeID)
}

func NodeIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(nodeIDKey).(string); ok {
		return v
	}
	return ""
}
