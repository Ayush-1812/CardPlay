package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"cardplay/internal/server"
	"cardplay/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Exercises real PostgreSQL authorization and transaction behavior. CI supplies
// an isolated TEST_DATABASE_URL; local runs skip unless explicitly configured.
func TestRoomAndChatAuthorization(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to an isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = db.Migrate(ctx, pool, false); err != nil {
		t.Fatal(err)
	}
	q := store.New(pool)
	suffix := httpx.Token()[:12]
	create := func(handle string) (string, string) {
		user, err := q.CreateUser(ctx, store.CreateUserParams{Email: handle + suffix + "@test.invalid", Handle: handle + suffix, DisplayName: handle, PasswordHash: accounts.PasswordHash("test-password-123"), EmailVerified: true})
		if err != nil {
			t.Fatal(err)
		}
		raw := httpx.Token()
		if err = q.CreateSession(ctx, store.CreateSessionParams{TokenHash: httpx.Hash(raw), UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		return user.ID, raw
	}
	alice, aliceToken := create("alice")
	bob, bobToken := create("bob")
	_, eveToken := create("eve")
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
	app := server.New(pool, c)
	srv := httptest.NewServer(app.Handler)
	defer srv.Close()
	go app.Hub.Run(ctx)
	do := func(method, path, token string, body any) (int, map[string]any) {
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
		if method != "GET" {
			req.Header.Set("Origin", c.Origin)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.AddCookie(&http.Cookie{Name: accounts.CookieName, Value: token})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if resp.StatusCode != 204 {
			if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
				t.Fatal(err)
			}
		}
		return resp.StatusCode, out
	}
	if status, _ := do("POST", "/api/v1/rooms", "", map[string]any{"name": "private", "capacity": 2}); status != 401 {
		t.Fatalf("unauthenticated create=%d", status)
	}
	status, created := do("POST", "/api/v1/rooms", aliceToken, map[string]any{"name": "private", "capacity": 2})
	if status != 201 {
		t.Fatalf("create=%d %v", status, created)
	}
	room := created["id"].(string)
	if status, _ := do("GET", "/api/v1/rooms/"+room, eveToken, nil); status != 404 {
		t.Fatalf("nonmember room read=%d", status)
	}
	if status, _ := do("GET", "/api/v1/rooms/"+room, bobToken, nil); status != 404 {
		t.Fatalf("uninvited room read=%d", status)
	}
	status, inv := do("POST", "/api/v1/rooms/"+room+"/invitations", aliceToken, map[string]any{})
	if status != 201 {
		t.Fatalf("invite=%d %v", status, inv)
	}
	token := inv["token"].(string)
	if token == "" {
		t.Fatal("empty link token")
	}
	status, joined := do("POST", "/api/v1/rooms/join", bobToken, map[string]any{"token": token})
	if status != 200 {
		t.Fatalf("join=%d %v", status, joined)
	}
	if status, _ := do("POST", "/api/v1/rooms/join", eveToken, map[string]any{"token": token}); status != 409 {
		t.Fatalf("third seat=%d", status)
	}
	status, view := do("GET", "/api/v1/rooms/"+room, bobToken, nil)
	if status != 200 {
		t.Fatalf("member read=%d %v", status, view)
	}
	members := view["members"].([]any)
	if len(members) != 2 {
		t.Fatalf("members=%d", len(members))
	}
	clientID := fmt.Sprintf("%s-%s-%s-%s-%s", suffix[:8], suffix[:4], suffix[:4], suffix[:4], strings.Repeat("a", 12))
	message := map[string]any{"body": "Hello from Bob", "client_id": clientID}
	status, first := do("POST", "/api/v1/rooms/"+room+"/chat", bobToken, message)
	if status != 200 {
		t.Fatalf("chat=%d %v", status, first)
	}
	status, second := do("POST", "/api/v1/rooms/"+room+"/chat", bobToken, message)
	if status != 200 || first["id"] != second["id"] {
		t.Fatalf("idempotent retry=%d %v", status, second)
	}
	message["body"] = "different"
	if status, _ := do("POST", "/api/v1/rooms/"+room+"/chat", bobToken, message); status != 409 {
		t.Fatalf("changed retry=%d", status)
	}
	if status, _ := do("GET", "/api/v1/rooms/"+room+"/chat", eveToken, nil); status != 404 {
		t.Fatalf("nonmember chat=%d", status)
	}
	status, list := do("GET", "/api/v1/rooms/"+room+"/chat", aliceToken, nil)
	if status != 200 || len(list["items"].([]any)) != 1 {
		t.Fatalf("chat page=%d %v", status, list)
	}
	if status, _ := do("POST", "/api/v1/rooms/"+room+"/matches", bobToken, nil); status != 403 {
		t.Fatalf("nonhost start=%d", status)
	}
	if status, _ := do("POST", "/api/v1/rooms/"+room+"/matches", aliceToken, nil); status != 501 {
		t.Fatalf("engine gate=%d", status)
	}
	if status, _ := do("PATCH", "/api/v1/me", aliceToken, map[string]any{"display_name": "Alice New"}); status != 200 {
		t.Fatalf("profile update=%d", status)
	}
	status, sessions := do("GET", "/api/v1/sessions", bobToken, nil)
	if status != 200 || len(sessions["items"].([]any)) != 1 {
		t.Fatalf("session list=%d %v", status, sessions)
	}
	if status, _ := do("DELETE", "/api/v1/sessions/"+sessions["items"].([]any)[0].(map[string]any)["id"].(string), bobToken, nil); status != 204 {
		t.Fatalf("session revocation=%d", status)
	}
	if status, _ := do("GET", "/api/v1/me", bobToken, nil); status != 401 {
		t.Fatalf("revoked session still valid=%d", status)
	}
	resetToken := httpx.Token()
	if err = q.CreateAccountToken(ctx, store.CreateAccountTokenParams{TokenHash: httpx.Hash(resetToken), UserID: alice, Purpose: "reset_password", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if status, _ := do("POST", "/api/v1/auth/password/reset", "", map[string]any{"token": resetToken, "password": "new-test-password-456"}); status != 200 {
		t.Fatalf("password reset=%d", status)
	}
	if status, _ := do("GET", "/api/v1/me", aliceToken, nil); status != 401 {
		t.Fatalf("old session after reset=%d", status)
	}
	if status, _ := do("POST", "/api/v1/auth/login", "", map[string]any{"email": "alice" + suffix + "@test.invalid", "password": "test-password-123"}); status != 401 {
		t.Fatalf("old password after reset=%d", status)
	}
	if status, _ := do("POST", "/api/v1/auth/login", "", map[string]any{"email": "alice" + suffix + "@test.invalid", "password": "new-test-password-456"}); status != 200 {
		t.Fatalf("new password after reset=%d", status)
	}
	newSession := httpx.Token()
	if err = q.CreateSession(ctx, store.CreateSessionParams{TokenHash: httpx.Hash(newSession), UserID: alice, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if status, _ := do("DELETE", "/api/v1/me", newSession, map[string]any{"password": "new-test-password-456"}); status != 204 {
		t.Fatalf("account deletion=%d", status)
	}
	if status, _ := do("GET", "/api/v1/me", newSession, nil); status != 401 {
		t.Fatalf("deleted account session=%d", status)
	}
	_ = alice
	_ = bob
}
