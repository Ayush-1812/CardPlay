package main

import (
	"cardplay/db"
	"cardplay/internal/accounts"
	"cardplay/internal/config"
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
	pc.MaxConns = 10
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
		for _, name := range []string{"alice", "bob"} {
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
		slog.Info("seed accounts available", "accounts", "alice@cardplay.test, bob@cardplay.test")
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
