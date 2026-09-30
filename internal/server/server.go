package server

import (
	"bytes"
	"cardplay/db"
	"cardplay/internal/accounts"
	"cardplay/internal/chat"
	"cardplay/internal/config"
	"cardplay/internal/game"
	"cardplay/internal/game/monopoly"
	"cardplay/internal/httpx"
	"cardplay/internal/matches"
	"cardplay/internal/obs"
	"cardplay/internal/realtime"
	"cardplay/internal/rooms"
	"cardplay/internal/social"
	"context"
	"crypto/rand"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type App struct {
	Handler http.Handler
	Hub     *realtime.Hub
}

func New(pool *pgxpool.Pool, c config.Config) *App {
	auth := &accounts.Module{DB: pool, Config: c}
	rm := &rooms.Module{DB: pool}
	friends := &social.Module{DB: pool}
	ch := &chat.Module{DB: pool}
	games := game.NewRegistry(monopoly.Module{})
	match := &matches.Module{DB: pool, Games: games, Random: rand.Reader, TurnTimeout: c.TurnTimeout}
	hub := realtime.New(pool, rm, ch, match, c.Origin)
	r := chi.NewRouter()
	r.Use(middleware.RequestID, obs.Instrument(), obs.Recover)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			// API responses are JSON: nothing may frame, script or embed them.
			w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "no-referrer")
			if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" && r.Header.Get("Origin") != c.Origin {
				httpx.Error(w, r, 403, "ORIGIN_REJECTED", "Request origin is not allowed")
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { httpx.JSON(w, 200, map[string]string{"status": "ok"}) })
	// Readiness: the database answers, every embedded migration is applied
	// with its current checksum, and live notifications are being received.
	// Details stay generic; operators read the logs.
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		checks := map[string]string{"database": "ok", "migrations": "ok", "notifications": "ok"}
		ready := true
		if err := pool.Ping(ctx); err != nil {
			checks["database"], ready = "failing", false
		}
		if pending, err := db.Pending(ctx, pool); err != nil || pending > 0 {
			checks["migrations"], ready = "pending", false
		}
		if !hub.Listening() {
			checks["notifications"], ready = "failing", false
		}
		if !ready {
			slog.Warn("readiness check failed", "checks", checks)
			httpx.JSON(w, 503, map[string]any{"status": "not_ready", "checks": checks})
			return
		}
		httpx.JSON(w, 200, map[string]any{"status": "ready", "checks": checks})
	})
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(auth.Authenticate)
		// Browser error reports; open to signed-out pages, small and rate limited.
		r.With(newLimiter(30, time.Minute)).Post("/client-errors", clientError)
		r.Get("/games", func(w http.ResponseWriter, r *http.Request) {
			httpx.JSON(w, 200, map[string]any{"items": games.Catalog()})
		})
		r.Get("/games/{gameID}/cards", func(w http.ResponseWriter, r *http.Request) {
			g, ok := games.Find(chi.URLParam(r, "gameID"))
			catalog, hasCards := g.(game.CardCatalog)
			if !ok || !hasCards {
				httpx.Error(w, r, 404, "NOT_FOUND", "Game not found")
				return
			}
			w.Header().Set("Cache-Control", "public, max-age=3600")
			httpx.JSON(w, 200, map[string]any{"items": catalog.Cards()})
		})
		r.Group(func(r chi.Router) {
			r.Use(newAuthLimiter(auth.KnownDevice))
			r.Post("/auth/register", auth.Register)
			r.Post("/auth/verify", auth.Verify)
			r.Post("/auth/verify/resend", auth.ResendVerification)
			r.Post("/auth/password/forgot", auth.ForgotPassword)
			r.Post("/auth/password/reset", auth.ResetPassword)
			r.Post("/auth/login", auth.Login)
		})
		r.Group(func(r chi.Router) {
			r.Use(httpx.Require)
			r.Get("/me", auth.Me)
			r.Patch("/me", auth.UpdateMe)
			r.With(newLimiter(5, time.Minute)).Delete("/me", auth.DeleteMe)
			r.With(newLimiter(5, time.Minute)).Put("/me/password", auth.ChangeMyPassword)
			r.Get("/sessions", auth.Sessions)
			r.Delete("/sessions/{sessionID}", auth.RevokeSession)
			r.Post("/auth/logout", auth.Logout)
		})
		r.Group(func(r chi.Router) {
			r.Use(httpx.Verified, newLimiter(120, time.Minute))
			r.With(newLimiter(30, time.Minute)).Get("/users", friends.Search)
			r.Get("/friendships", friends.List)
			r.Get("/blocks", friends.Blocks)
			r.With(newLimiter(20, time.Minute)).Post("/friendships/{userID}/{action}", friends.Change)
			r.Get("/rooms", rm.List)
			r.With(newLimiter(10, time.Minute)).Post("/rooms", rm.Create)
			r.With(newLimiter(20, time.Minute)).Post("/rooms/join", rm.Join)
			r.Get("/rooms/{roomID}", rm.Get)
			r.Patch("/rooms/{roomID}", rm.Update)
			r.Delete("/rooms/{roomID}", rm.Close)
			r.Post("/rooms/{roomID}/leave", rm.Leave)
			r.Put("/rooms/{roomID}/host", rm.TransferHost)
			r.Delete("/rooms/{roomID}/members/{userID}", rm.Kick)
			r.Put("/rooms/{roomID}/ready", rm.Ready)
			r.Get("/rooms/{roomID}/invitations", rm.CreatedInvitations)
			r.With(newLimiter(20, time.Minute)).Post("/rooms/{roomID}/invitations", rm.Invite)
			r.Get("/invitations", rm.Invitations)
			r.Delete("/invitations/{invitationID}", rm.Revoke)
			r.Get("/rooms/{roomID}/chat", ch.List)
			r.Post("/rooms/{roomID}/chat", ch.Post)
			r.With(newLimiter(10, time.Minute)).Post("/rooms/{roomID}/chat/{messageID}/report", ch.Report)
			r.Get("/mutes", friends.Mutes)
			r.With(newLimiter(20, time.Minute)).Put("/mutes/{userID}", friends.Mute)
			r.Delete("/mutes/{userID}", friends.Unmute)
			r.With(newLimiter(10, time.Minute)).Post("/rooms/{roomID}/matches", match.Start)
			r.Get("/matches/{matchID}", match.View)
			r.Post("/matches/{matchID}/leave", match.LeaveMatch)
			r.With(newLimiter(20, time.Minute)).Post("/matches/{matchID}/abandon", match.Vote)
		})
	})
	r.With(auth.Authenticate, httpx.Verified).Get("/ws", hub.ServeHTTP)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, 404, "NOT_FOUND", "Endpoint not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, 405, "METHOD_NOT_ALLOWED", "Method not allowed")
	})
	return &App{Handler: r, Hub: hub}
}

type bucket struct {
	start time.Time
	count int
}

func newLimiter(limit int, window time.Duration) func(http.Handler) http.Handler {
	return newScopedLimiter("api", limit, window)
}

func newScopedLimiter(scope string, limit int, window time.Duration) func(http.Handler) http.Handler {
	var mu sync.Mutex
	entries := map[string]bucket{}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := httpx.Actor(r).ID
			if key == "" {
				key, _, _ = net.SplitHostPort(r.RemoteAddr)
			}
			now := time.Now()
			mu.Lock()
			if len(entries) >= 4096 {
				for k, v := range entries {
					if now.Sub(v.start) > window {
						delete(entries, k)
					}
				}
			}
			b, exists := entries[key]
			if !exists && len(entries) >= 4096 {
				mu.Unlock()
				obs.RateLimited.Inc(scope)
				httpx.Error(w, r, 429, "RATE_LIMITED", "Try again later")
				return
			}
			if now.Sub(b.start) > window {
				b = bucket{start: now}
			}
			b.count++
			entries[key] = b
			mu.Unlock()
			if b.count > limit {
				w.Header().Set("Retry-After", "60")
				obs.RateLimited.Inc(scope)
				httpx.Error(w, r, 429, "RATE_LIMITED", "Try again later")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Authentication requests need account-specific limits: when Next.js proxies
// requests, every visitor shares the API-facing source address and no client
// address is forwarded. Mail endpoints allow three requests per email so nobody
// can flood an inbox; other endpoints allow ten per email or token. Login from
// a browser that has signed in to the account before (a known device cookie)
// gets its own bucket, so failures sent by strangers cannot lock the owner out.
// A high proxy-wide ceiling bounds total work.
func newAuthLimiter(knownDevice func(ctx context.Context, token, email string) bool) func(http.Handler) http.Handler {
	var mu sync.Mutex
	entries := map[string]bucket{}
	global := newScopedLimiter("auth_global", 600, time.Minute)
	const scope = "auth"
	return func(next http.Handler) http.Handler {
		return global(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			originalBody := r.Body
			body, err := io.ReadAll(io.LimitReader(originalBody, (16<<10)+1))
			_ = originalBody.Close()
			if err != nil || len(body) > 16<<10 {
				httpx.Error(w, r, 400, "INVALID_REQUEST", "Request body is too large")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var input struct {
				Email string `json:"email"`
				Token string `json:"token"`
			}
			_ = json.Unmarshal(body, &input)
			email := strings.ToLower(strings.TrimSpace(input.Email))
			identity := email
			if identity == "" {
				identity = input.Token
			}
			if identity == "" {
				identity, _, _ = net.SplitHostPort(r.RemoteAddr)
			}
			key := r.URL.Path + ":" + httpx.Hash(identity)
			limit := 10
			if strings.HasSuffix(r.URL.Path, "/register") || strings.HasSuffix(r.URL.Path, "/forgot") || strings.HasSuffix(r.URL.Path, "/resend") {
				limit = 3
			}
			if strings.HasSuffix(r.URL.Path, "/login") && email != "" {
				if c, err := r.Cookie(accounts.DeviceCookieName); err == nil && knownDevice(r.Context(), c.Value, email) {
					key += ":device:" + httpx.Hash(c.Value)
				}
			}
			now := time.Now()
			mu.Lock()
			if len(entries) >= 65536 {
				for k, v := range entries {
					if now.Sub(v.start) > time.Minute {
						delete(entries, k)
					}
				}
			}
			if _, exists := entries[key]; !exists && len(entries) >= 65536 {
				mu.Unlock()
				obs.RateLimited.Inc(scope)
				httpx.Error(w, r, 429, "RATE_LIMITED", "Try again later")
				return
			}
			b := entries[key]
			if now.Sub(b.start) > time.Minute {
				b = bucket{start: now}
			}
			b.count++
			entries[key] = b
			mu.Unlock()
			if b.count > limit {
				w.Header().Set("Retry-After", "60")
				obs.RateLimited.Inc(scope)
				httpx.Error(w, r, 429, "RATE_LIMITED", "Try again later")
				return
			}
			next.ServeHTTP(w, r)
		}))
	}
}

// clientError records an error reported by the browser. Only bounded,
// sanitized fields are logged: never cookies, request bodies or query strings.
func clientError(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
		Page    string `json:"page"`
		Stack   string `json:"stack"`
		Digest  string `json:"digest"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	clip := func(s string, n int) string {
		s = strings.ToValidUTF8(s, "?")
		if len(s) > n {
			s = s[:n]
		}
		return strings.ToValidUTF8(s, "")
	}
	page, _, _ := strings.Cut(clip(in.Page, 200), "?")
	page, _, _ = strings.Cut(page, "#")
	switch in.Kind {
	case "error", "unhandledrejection", "render":
	default:
		in.Kind = "other"
	}
	obs.ClientErrors.Inc()
	slog.Warn("client_error",
		"request_id", middleware.GetReqID(r.Context()),
		"kind", in.Kind,
		"message", clip(in.Message, 500),
		"page", page,
		"stack", clip(in.Stack, 2000),
		"digest", clip(in.Digest, 64),
	)
	w.WriteHeader(204)
}
