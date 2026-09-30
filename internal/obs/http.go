package obs

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// SetupLogging installs the default structured logger: JSON in production
// or when format is "json", readable text otherwise.
func SetupLogging(environment, format, level string) {
	lvl := slog.LevelInfo
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler = slog.NewTextHandler(os.Stderr, opts)
	if format == "json" || (format == "" && environment == "production") {
		h = slog.NewJSONHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(h).With("service", "cardplay-api"))
}

func routeOf(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	return "unmatched"
}

// Instrument logs one structured line per request and records metrics. It
// logs the route pattern, never the raw URL, so query strings and invite
// fragments stay out of logs; it records no user identifiers. The wrapped
// writer still supports WebSocket upgrades.
func Instrument() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			route := routeOf(r)
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			elapsed := time.Since(start)
			HTTPRequests.Inc(r.Method, route, fmt.Sprintf("%dxx", status/100))
			if route != "/ws" {
				HTTPDuration.Observe(elapsed.Seconds(), route)
			}
			level := slog.LevelInfo
			if route == "/healthz" || route == "/readyz" {
				level = slog.LevelDebug
			} else if status >= 500 {
				level = slog.LevelError
			}
			slog.Log(r.Context(), level, "http_request",
				"request_id", middleware.GetReqID(r.Context()),
				"method", r.Method,
				"route", route,
				"status", status,
				"bytes", ww.BytesWritten(),
				"duration_ms", elapsed.Milliseconds(),
			)
		})
	}
}

// Recover turns a handler panic into a logged, counted 500 response.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(v)
			}
			Panics.Inc()
			slog.Error("panic",
				"request_id", middleware.GetReqID(r.Context()),
				"route", routeOf(r),
				"error", fmt.Sprint(v),
				"stack", string(debug.Stack()),
			)
			if r.Header.Get("Connection") != "Upgrade" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":{"code":"INTERNAL","message":"The request could not be completed"}}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
