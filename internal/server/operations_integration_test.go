package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"cardplay/db"
	"cardplay/internal/accounts"
	"cardplay/internal/config"
	"cardplay/internal/httpx"
	"cardplay/internal/obs"
	"cardplay/internal/server"
	"cardplay/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Covers the release surface: health and readiness, security headers, text
// validation, browser error reports and metrics.
func TestOperationsAndValidation(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to an isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = db.Migrate(ctx, pool, false); err != nil {
		t.Fatal(err)
	}
	c, err := config.Parse(func(key string) string {
		switch key {
		case "DATABASE_URL":
			return dsn
		case "APP_ORIGIN":
			return "http://localhost:3000"
		case "APP_ENV":
			return "test"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	q := store.New(pool)
	suffix := httpx.Token()[:12]
	user, err := q.CreateUser(ctx, store.CreateUserParams{Email: "ops" + suffix + "@test.invalid", Handle: "ops" + suffix, DisplayName: "ops", PasswordHash: accounts.PasswordHash("test-password-123"), EmailVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	token := httpx.Token()
	if err = q.CreateSession(ctx, store.CreateSessionParams{TokenHash: httpx.Hash(token), UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	app := server.New(pool, c)
	srv := httptest.NewServer(app.Handler)
	defer srv.Close()

	do := func(method, path, origin string, body any) *http.Response {
		t.Helper()
		var reader io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			reader = bytes.NewReader(b)
		}
		req, err := http.NewRequest(method, srv.URL+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: accounts.CookieName, Value: token})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}

	t.Run("readiness waits for the notification listener", func(t *testing.T) {
		if resp := do("GET", "/healthz", "", nil); resp.StatusCode != 200 {
			t.Fatalf("healthz=%d", resp.StatusCode)
		}
		resp := do("GET", "/readyz", "", nil)
		var body struct {
			Status string            `json:"status"`
			Checks map[string]string `json:"checks"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if resp.StatusCode != 503 || body.Checks["notifications"] != "failing" || body.Checks["database"] != "ok" || body.Checks["migrations"] != "ok" {
			t.Fatalf("readyz before listener=%d %+v", resp.StatusCode, body)
		}
		go app.Hub.Run(ctx)
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if do("GET", "/readyz", "", nil).StatusCode == 200 {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("readyz never became ready after the hub started")
	})

	t.Run("security headers", func(t *testing.T) {
		resp := do("GET", "/api/v1/me", "", nil)
		for key, want := range map[string]string{
			"Cache-Control":           "no-store",
			"X-Content-Type-Options":  "nosniff",
			"X-Frame-Options":         "DENY",
			"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
			"Referrer-Policy":         "no-referrer",
		} {
			if got := resp.Header.Get(key); got != want {
				t.Errorf("%s=%q want %q", key, got, want)
			}
		}
		if resp.Header.Get("X-Request-Id") == "" && resp.Header.Get("X-Request-ID") == "" {
			t.Log("request id is logged but not echoed; that is acceptable")
		}
	})

	t.Run("text fields reject control and direction-override characters", func(t *testing.T) {
		for _, name := range []string{"bad‮name", "tab\tname", "  ", strings.Repeat("x", 81)} {
			if resp := do("POST", "/api/v1/rooms", c.Origin, map[string]any{"name": name, "capacity": 2}); resp.StatusCode != 400 {
				t.Errorf("room name %q=%d", name, resp.StatusCode)
			}
		}
		if resp := do("PATCH", "/api/v1/me", c.Origin, map[string]any{"display_name": "evil⁦name"}); resp.StatusCode != 400 {
			t.Errorf("display name with isolate=%d", resp.StatusCode)
		}
		resp := do("POST", "/api/v1/rooms", c.Origin, map[string]any{"name": "  Friday cards ♠  ", "capacity": 2})
		if resp.StatusCode != 201 {
			t.Fatalf("valid room=%d", resp.StatusCode)
		}
		var room struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&room)
		if room.Name != "Friday cards ♠" {
			t.Errorf("room name not trimmed: %q", room.Name)
		}
		for _, body := range []string{"line\nbreak", "null\x00byte", "rtl‮override", ""} {
			resp := do("POST", "/api/v1/rooms/"+room.ID+"/chat", c.Origin, map[string]any{"client_id": uuid(), "body": body})
			if resp.StatusCode != 400 {
				t.Errorf("chat %q=%d", body, resp.StatusCode)
			}
		}
		if resp := do("POST", "/api/v1/rooms/"+room.ID+"/chat", c.Origin, map[string]any{"client_id": uuid(), "body": "héllo 👋"}); resp.StatusCode != 200 {
			t.Errorf("valid chat=%d", resp.StatusCode)
		}
	})

	t.Run("client error reports", func(t *testing.T) {
		before := obs.ClientErrors.Value()
		report := map[string]any{"kind": "error", "message": "TypeError: x is undefined", "page": "/?invite=secret#token", "stack": "at a (b.js:1:2)"}
		if resp := do("POST", "/api/v1/client-errors", c.Origin, report); resp.StatusCode != 204 {
			t.Fatalf("report=%d", resp.StatusCode)
		}
		if obs.ClientErrors.Value() != before+1 {
			t.Fatal("client error not counted")
		}
		if resp := do("POST", "/api/v1/client-errors", "https://evil.example", report); resp.StatusCode != 403 {
			t.Fatalf("cross-origin report=%d", resp.StatusCode)
		}
		if resp := do("POST", "/api/v1/client-errors", c.Origin, map[string]any{"cookie": "x"}); resp.StatusCode != 400 {
			t.Fatalf("unknown field report=%d", resp.StatusCode)
		}
		big := map[string]any{"message": strings.Repeat("a", 20<<10)}
		if resp := do("POST", "/api/v1/client-errors", c.Origin, big); resp.StatusCode != 400 {
			t.Fatalf("oversized report=%d", resp.StatusCode)
		}
	})

	t.Run("metrics use route patterns, not raw paths", func(t *testing.T) {
		rec := httptest.NewRecorder()
		obs.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
		out := rec.Body.String()
		for _, want := range []string{
			`cardplay_http_requests_total{method="POST",route="/api/v1/rooms/{roomID}/chat",code="4xx"}`,
			`cardplay_http_request_duration_seconds_bucket{route="/readyz",le="+Inf"}`,
			"# TYPE cardplay_game_commands_total counter",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("metrics missing %s", want)
			}
		}
		if strings.Contains(out, user.ID) || strings.Contains(out, "secret") {
			t.Error("metrics leak identifiers")
		}
	})
}
