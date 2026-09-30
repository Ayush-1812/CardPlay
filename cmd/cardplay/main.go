package main

import (
	"cardplay/db"
	"cardplay/internal/accounts"
	"cardplay/internal/config"
	"cardplay/internal/obs"
	"cardplay/internal/server"
	"cardplay/internal/store"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("command failed", "reason", err.Error())
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) > 1 && os.Args[1] == "health" {
		port := "8080"
		if _, p, err := net.SplitHostPort(os.Getenv("HTTP_ADDR")); err == nil && p != "" {
			port = p
		}
		client := http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", port) + "/readyz")
		if err != nil {
			return errors.New("API not ready")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return errors.New("API not ready")
		}
		return nil
	}
	obs.SetupLogging(os.Getenv("APP_ENV"), os.Getenv("LOG_FORMAT"), os.Getenv("LOG_LEVEL"))
	c, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pc, err := pgxpool.ParseConfig(c.DatabaseURL)
	if err != nil {
		return errors.New("invalid database connection configuration")
	}
	pc.MaxConns = c.DBMaxConns
	pc.MinConns = 1
	pc.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return errors.New("database pool initialization failed")
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		return errors.New("database unavailable; check DATABASE_URL and migrations setup")
	}
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	switch command {
	case "migrate":
		err = db.Migrate(ctx, pool, false)
		if err != nil {
			return errors.New("migration failed; check schema and database permissions")
		}
		slog.Info("migrations applied")
		return nil
	case "migrate-down":
		if c.Environment == "production" {
			return errors.New("migrate-down is disabled in production")
		}
		return db.Migrate(ctx, pool, true)
	case "seed":
		if c.Environment == "production" {
			return errors.New("seed is disabled in production")
		}
		password := os.Getenv("SEED_PASSWORD")
		if len(password) < 12 || len(password) > 128 {
			return errors.New("SEED_PASSWORD must be 12-128 bytes")
		}
		q := store.New(pool)
		for _, name := range []string{"alice", "bob", "carol"} {
			email := name + "@cardplay.test"
			_, e := q.UserByEmail(ctx, email)
			if e == nil {
				continue
			}
			if !errors.Is(e, pgx.ErrNoRows) {
				return errors.New("seed lookup failed")
			}
			_, err = q.CreateUser(ctx, store.CreateUserParams{Email: email, Handle: name, DisplayName: name, PasswordHash: accounts.PasswordHash(password), EmailVerified: true})
			if err != nil {
				return errors.New("seed creation failed")
			}
		}
		slog.Info("seed accounts available", "accounts", "alice@cardplay.test, bob@cardplay.test, carol@cardplay.test")
		return nil
	case "serve":
	default:
		return errors.New("usage: cardplay [serve|migrate|migrate-down|seed]")
	}
	var migrations int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&migrations); err != nil || migrations == 0 {
		return errors.New("run cardplay migrate before serve")
	}
	app := server.New(pool, c)
	go app.Hub.Run(ctx)
	started := time.Now()
	obs.NewGauge("cardplay_ws_connections", "Open WebSocket connections on this instance.", func() float64 { return float64(app.Hub.Connections()) })
	obs.NewGauge("cardplay_notifications_listening", "1 while the PostgreSQL change listener is active.", func() float64 {
		if app.Hub.Listening() {
			return 1
		}
		return 0
	})
	obs.NewGauge("cardplay_db_pool_acquired_connections", "Pool connections in use.", func() float64 { return float64(pool.Stat().AcquiredConns()) })
	obs.NewGauge("cardplay_db_pool_idle_connections", "Idle pool connections.", func() float64 { return float64(pool.Stat().IdleConns()) })
	obs.NewGauge("cardplay_db_pool_max_connections", "Configured pool size.", func() float64 { return float64(pool.Stat().MaxConns()) })
	obs.NewGauge("cardplay_db_pool_empty_acquire_total", "Acquires that had to wait for a connection.", func() float64 { return float64(pool.Stat().EmptyAcquireCount()) })
	obs.NewGauge("cardplay_db_pool_acquire_wait_seconds_total", "Total time spent waiting for pool connections.", func() float64 { return pool.Stat().AcquireDuration().Seconds() })
	obs.NewGauge("cardplay_goroutines", "Goroutines in this process.", func() float64 { return float64(runtime.NumGoroutine()) })
	obs.NewGauge("cardplay_uptime_seconds", "Seconds since this process started serving.", func() float64 { return time.Since(started).Seconds() })
	if c.MetricsAddress != "" {
		metrics := &http.Server{Addr: c.MetricsAddress, Handler: obs.Handler(), ReadHeaderTimeout: 5 * time.Second}
		go func() {
			slog.Info("metrics listening", "address", c.MetricsAddress)
			if err := metrics.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("metrics listener failed", "error", err.Error())
			}
		}()
		defer func() { _ = metrics.Close() }()
	}
	srv := &http.Server{Addr: c.Address, Handler: app.Handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	done := make(chan error, 1)
	go func() {
		slog.Info("CardPlay API listening", "address", c.Address, "environment", c.Environment)
		done <- srv.ListenAndServe()
	}()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	}
}
