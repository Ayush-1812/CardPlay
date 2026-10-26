package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"cardplay/db"
	"cardplay/internal/accounts"
	"cardplay/internal/config"
	"cardplay/internal/store"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestNativeClient covers the app's way in: a session as a token rather
// than a cookie, and the origin rules that let it through without letting
// a web page through with it.
func TestNativeClient(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to an isolated PostgreSQL database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = db.Migrate(ctx, pool, false); err != nil {
		t.Fatal(err)
	}
	c, err := config.Parse(func(k string) string {
		return map[string]string{"DATABASE_URL": dsn, "APP_ORIGIN": "http://localhost:3000", "APP_ENV": "test"}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, pool: pool, c: c, q: store.New(pool)}
	inst := startInstance(t, pool, c)

	// send makes one request the way an app does, or without the header
	// when native is false.
	send := func(method, path, token string, native bool, origin string, body any) (*http.Response, map[string]any) {
		t.Helper()
		var payload io.Reader
		if body != nil {
			blob, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			payload = bytes.NewReader(blob)
		}
		req, err := http.NewRequest(method, inst.srv.URL+path, payload)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if native {
			req.Header.Set("X-CardPlay-Client", "native")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var decoded map[string]any
		_ = json.Unmarshal(raw, &decoded)
		return resp, decoded
	}

	var token string

	t.Run("an app is given a token and no cookie", func(t *testing.T) {
		resp, body := send("POST", "/api/v1/auth/guest", "", true, "", map[string]any{"display_name": "app player"})
		if resp.StatusCode != 201 {
			t.Fatalf("status %d: %v", resp.StatusCode, body)
		}
		got, _ := body["token"].(string)
		if len(got) != 64 {
			t.Fatalf("no usable token in the reply: %v", body)
		}
		if len(resp.Cookies()) != 0 {
			t.Fatal("an app was given a cookie it cannot keep")
		}
		user, _ := body["user"].(map[string]any)
		if user["display_name"] != "app player" || user["guest"] != true {
			t.Fatalf("the reply does not describe the player: %v", body)
		}
		token = got
	})

	t.Run("a browser still gets a cookie and no token", func(t *testing.T) {
		resp, body := send("POST", "/api/v1/auth/guest", "", false, c.Origin, map[string]any{"display_name": "web player"})
		if resp.StatusCode != 201 {
			t.Fatalf("status %d: %v", resp.StatusCode, body)
		}
		if _, leaked := body["token"]; leaked {
			t.Fatal("the session token was put where a script on the page can read it")
		}
		var session *http.Cookie
		for _, cookie := range resp.Cookies() {
			if cookie.Name == accounts.CookieName {
				session = cookie
			}
		}
		if session == nil || !session.HttpOnly {
			t.Fatalf("a browser must get an HttpOnly session cookie: %v", resp.Cookies())
		}
	})

	t.Run("the token reads and writes without an Origin header", func(t *testing.T) {
		resp, body := send("GET", "/api/v1/me", token, true, "", nil)
		if resp.StatusCode != 200 {
			t.Fatalf("reading the session failed: %d %v", resp.StatusCode, body)
		}
		user, _ := body["user"].(map[string]any)
		if user == nil {
			user = body
		}
		if user["display_name"] != "app player" {
			t.Fatalf("the token named the wrong player: %v", body)
		}
		// A write is what the origin rule guards, so that is the one that
		// has to go through for an app.
		resp, body = send("POST", "/api/v1/rooms", token, true, "", map[string]any{"name": "app room", "capacity": 4, "game": "trump"})
		if resp.StatusCode >= 300 {
			t.Fatalf("an app could not create a room: %d %v", resp.StatusCode, body)
		}
	})

	t.Run("a browser write from the wrong site is still refused", func(t *testing.T) {
		resp, _ := send("POST", "/api/v1/rooms", "", false, "https://evil.example", map[string]any{"name": "nope", "capacity": 4, "game": "trump"})
		if resp.StatusCode != 403 {
			t.Fatalf("a cross-site write returned %d, want 403", resp.StatusCode)
		}
		// And a cookie-carrying write with no Origin at all stays refused:
		// that is the shape a forged request takes.
		resp, _ = send("POST", "/api/v1/rooms", "", false, "", map[string]any{"name": "nope", "capacity": 4, "game": "trump"})
		if resp.StatusCode != 403 {
			t.Fatalf("a write with no origin and no token returned %d, want 403", resp.StatusCode)
		}
	})

	t.Run("a bad token is nobody", func(t *testing.T) {
		resp, _ := send("GET", "/api/v1/me", strings.Repeat("a", 64), true, "", nil)
		if resp.StatusCode != 401 {
			t.Fatalf("an invented token returned %d, want 401", resp.StatusCode)
		}
	})

	t.Run("the socket opens with a ticket and no origin", func(t *testing.T) {
		resp, body := send("POST", "/api/v1/realtime/ticket", token, true, "", nil)
		if resp.StatusCode != 200 && resp.StatusCode != 201 {
			t.Fatalf("no ticket: %d %v", resp.StatusCode, body)
		}
		ticket, _ := body["ticket"].(string)
		if ticket == "" {
			t.Fatalf("no ticket in %v", body)
		}
		url := "ws" + strings.TrimPrefix(inst.srv.URL, "http") + "/ws?ticket=" + ticket
		conn, _, err := websocket.Dial(ctx, url, nil)
		if err != nil {
			t.Fatalf("an app could not open the socket: %v", err)
		}
		defer conn.CloseNow()

		// A page on another site still cannot, ticket or not. It needs a
		// ticket of its own: the first one died when it was used.
		_, second := send("POST", "/api/v1/realtime/ticket", token, true, "", nil)
		fresh, _ := second["ticket"].(string)
		if fresh == "" {
			t.Fatalf("no second ticket in %v", second)
		}
		h := http.Header{}
		h.Set("Origin", "https://evil.example")
		bad, resp2, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(inst.srv.URL, "http")+"/ws?ticket="+fresh, &websocket.DialOptions{HTTPHeader: h})
		if err == nil {
			bad.CloseNow()
			t.Fatal("a foreign origin opened a socket")
		}
		if resp2 != nil && resp2.StatusCode != 403 {
			t.Fatalf("a foreign origin got %d, want 403", resp2.StatusCode)
		}
	})

	_ = e
}
