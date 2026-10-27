// Package health serves liveness and readiness probes.
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Pinger checks a dependency.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Handler serves GET /healthz (process alive) and GET /readyz (database
// reachable).
func Handler(db Pinger, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		write(w, http.StatusOK, "ok")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			log.Warn("readiness check failed", "error", err)
			write(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		write(w, http.StatusOK, "ok")
	})
	return mux
}

func write(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": msg})
}
