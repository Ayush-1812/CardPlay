package server

import (
	"cardplay/internal/accounts"
	"cardplay/internal/chat"
	"cardplay/internal/config"
	"cardplay/internal/game"
	"cardplay/internal/game/monopoly"
	"cardplay/internal/httpx"
	"cardplay/internal/matches"
	"cardplay/internal/realtime"
	"cardplay/internal/rooms"
	"cardplay/internal/social"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"net"
	"net/http"
	"sync"
	"time"
)

type App struct {
	Handler http.Handler
	Hub     *realtime.Hub
}

func New(db *pgxpool.Pool, c config.Config) *App {
	auth := &accounts.Module{DB: db, Config: c}
	rm := &rooms.Module{DB: db}
	friends := &social.Module{DB: db}
	ch := &chat.Module{DB: db}
	games := game.NewRegistry(monopoly.Module{})
	match := &matches.Module{DB: db, Games: games}
	hub := realtime.New(db, rm, ch, c.Origin)
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" && r.Header.Get("Origin") != c.Origin {
				httpx.Error(w, r, 403, "ORIGIN_REJECTED", "Request origin is not allowed")
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { httpx.JSON(w, 200, map[string]string{"status": "ok"}) })
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil {
			httpx.Error(w, r, 503, "NOT_READY", "Database unavailable")
			return
		}
		httpx.JSON(w, 200, map[string]string{"status": "ready"})
	})
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(auth.Authenticate)
		r.Get("/games", func(w http.ResponseWriter, r *http.Request) {
			httpx.JSON(w, 200, map[string]any{"items": games.Catalog()})
		})
		r.Group(func(r chi.Router) {
			r.Use(newLimiter(10, time.Minute))
			r.Post("/auth/register", auth.Register)
			r.Post("/auth/verify", auth.Verify)
			r.Post("/auth/login", auth.Login)
		})
		r.Group(func(r chi.Router) { r.Use(httpx.Require); r.Get("/me", auth.Me); r.Post("/auth/logout", auth.Logout) })
		r.Group(func(r chi.Router) {
			r.Use(httpx.Verified, newLimiter(120, time.Minute))
			r.Get("/users", friends.Search)
			r.Get("/friendships", friends.List)
			r.Post("/friendships/{userID}/{action}", friends.Change)
			r.Get("/rooms", rm.List)
			r.Post("/rooms", rm.Create)
			r.Post("/rooms/join", rm.Join)
			r.Get("/rooms/{roomID}", rm.Get)
			r.Put("/rooms/{roomID}/ready", rm.Ready)
			r.Post("/rooms/{roomID}/invitations", rm.Invite)
			r.Get("/invitations", rm.Invitations)
			r.Delete("/invitations/{invitationID}", rm.Revoke)
			r.Get("/rooms/{roomID}/chat", ch.List)
			r.Post("/rooms/{roomID}/chat", ch.Post)
			r.Post("/rooms/{roomID}/matches", match.Start)
			r.Get("/matches/{matchID}", match.View)
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
				httpx.Error(w, r, 429, "RATE_LIMITED", "Try again later")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
