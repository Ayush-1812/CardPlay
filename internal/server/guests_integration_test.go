package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"cardplay/db"
	"cardplay/internal/accounts"
	"cardplay/internal/config"
	"cardplay/internal/server"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Guests (name-only accounts) and temporary rooms, owner decisions of
// 2026-10-01.
func TestGuestsAndTemporaryRooms(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to an isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
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
	app := server.New(pool, c)
	srv := httptest.NewServer(app.Handler)
	defer srv.Close()
	go app.Hub.Run(ctx)

	type client struct{ http *http.Client }
	newClient := func() *client {
		jar, _ := cookiejar.New(nil)
		return &client{&http.Client{Jar: jar}}
	}
	do := func(cl *client, method, path string, body any) (int, map[string]any) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			reader = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, srv.URL+path, reader)
		if method != "GET" {
			req.Header.Set("Origin", c.Origin)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := cl.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	guest := func(name string) (*client, string) {
		t.Helper()
		cl := newClient()
		status, me := do(cl, "POST", "/api/v1/auth/guest", map[string]any{"display_name": name})
		if status != 201 || me["is_guest"] != true {
			t.Fatalf("guest sign-in=%d %v", status, me)
		}
		return cl, me["id"].(string)
	}
	exists := func(table, id string) bool {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}

	t.Run("guest sign-in validates the name", func(t *testing.T) {
		for _, name := range []string{"", "   ", "bad‮name", "abcdefghijklmnopqrstuvwxy"} {
			if status, _ := do(newClient(), "POST", "/api/v1/auth/guest", map[string]any{"display_name": name}); status != 400 {
				t.Errorf("name %q=%d", name, status)
			}
		}
		cl, _ := guest("  Riya  ")
		status, me := do(cl, "GET", "/api/v1/me", nil)
		if status != 200 || me["display_name"] != "Riya" || me["is_guest"] != true || me["email"] != "" {
			t.Fatalf("me=%d %v", status, me)
		}
	})

	t.Run("guests play and chat but cannot use friends", func(t *testing.T) {
		host, _ := guest("Host")
		other, _ := guest("Other")
		status, room := do(host, "POST", "/api/v1/rooms", map[string]any{"name": "Guest table", "capacity": 2})
		if status != 201 {
			t.Fatalf("guest create room=%d %v", status, room)
		}
		roomID := room["id"].(string)
		_, inv := do(host, "POST", "/api/v1/rooms/"+roomID+"/invitations", map[string]any{})
		if status, _ := do(other, "POST", "/api/v1/rooms/join", map[string]any{"token": inv["token"]}); status != 200 {
			t.Fatalf("guest join by link=%d", status)
		}
		for _, cl := range []*client{host, other} {
			if status, _ := do(cl, "PUT", "/api/v1/rooms/"+roomID+"/ready", map[string]any{"ready": true}); status != 204 {
				t.Fatalf("ready=%d", status)
			}
		}
		if status, _ := do(host, "POST", "/api/v1/rooms/"+roomID+"/matches", nil); status != 201 {
			t.Fatalf("guest start match=%d", status)
		}
		if status, _ := do(other, "POST", "/api/v1/rooms/"+roomID+"/chat", map[string]any{"client_id": uuid(), "body": "hi"}); status != 200 {
			t.Fatalf("guest chat=%d", status)
		}
		for _, path := range []string{"/api/v1/friendships", "/api/v1/blocks", "/api/v1/users?handle=alice"} {
			status, body := do(host, "GET", path, nil)
			if status != 403 || body["error"].(map[string]any)["code"] != "GUEST_NOT_ALLOWED" {
				t.Errorf("guest %s=%d %v", path, status, body)
			}
		}
	})

	t.Run("a guest's sign-out deletes the guest and empties its rooms", func(t *testing.T) {
		cl, id := guest("Leaver")
		_, room := do(cl, "POST", "/api/v1/rooms", map[string]any{"name": "Solo", "capacity": 2})
		roomID := room["id"].(string)
		if status, _ := do(cl, "POST", "/api/v1/auth/logout", nil); status != 204 {
			t.Fatalf("logout=%d", status)
		}
		var deleted bool
		if err := pool.QueryRow(ctx, "SELECT deleted_at IS NOT NULL FROM users WHERE id=$1", id).Scan(&deleted); err != nil || !deleted {
			t.Fatalf("guest not deleted: %v", err)
		}
		if exists("rooms", roomID) {
			t.Fatal("the guest's empty room should be gone")
		}
		if status, _ := do(cl, "GET", "/api/v1/me", nil); status != 401 {
			t.Fatalf("signed-out guest me=%d", status)
		}
	})

	t.Run("the last player out deletes the room and its game", func(t *testing.T) {
		a, _ := guest("A")
		b, _ := guest("B")
		_, room := do(a, "POST", "/api/v1/rooms", map[string]any{"name": "Short lived", "capacity": 2})
		roomID := room["id"].(string)
		_, inv := do(a, "POST", "/api/v1/rooms/"+roomID+"/invitations", map[string]any{})
		do(b, "POST", "/api/v1/rooms/join", map[string]any{"token": inv["token"]})
		for _, cl := range []*client{a, b} {
			do(cl, "PUT", "/api/v1/rooms/"+roomID+"/ready", map[string]any{"ready": true})
		}
		_, started := do(a, "POST", "/api/v1/rooms/"+roomID+"/matches", nil)
		matchID := started["match_id"].(string)
		// Leaving the match ends it; then both leave the room.
		if status, _ := do(a, "POST", "/api/v1/matches/"+matchID+"/leave", nil); status != 204 {
			t.Fatalf("leave match=%d", status)
		}
		if status, _ := do(a, "POST", "/api/v1/rooms/"+roomID+"/leave", nil); status != 204 {
			t.Fatalf("first leave=%d", status)
		}
		if !exists("rooms", roomID) {
			t.Fatal("the room must stay while someone is in it")
		}
		if status, _ := do(b, "POST", "/api/v1/rooms/"+roomID+"/leave", nil); status != 204 {
			t.Fatalf("last leave=%d", status)
		}
		if exists("rooms", roomID) || exists("matches", matchID) {
			t.Fatal("the room and its game should be deleted")
		}
	})

	t.Run("closing a room deletes it for everyone", func(t *testing.T) {
		a, _ := guest("Closer")
		b, _ := guest("Member")
		_, room := do(a, "POST", "/api/v1/rooms", map[string]any{"name": "Closing", "capacity": 2})
		roomID := room["id"].(string)
		_, inv := do(a, "POST", "/api/v1/rooms/"+roomID+"/invitations", map[string]any{})
		do(b, "POST", "/api/v1/rooms/join", map[string]any{"token": inv["token"]})
		if status, _ := do(a, "DELETE", "/api/v1/rooms/"+roomID, nil); status != 204 {
			t.Fatalf("close=%d", status)
		}
		if status, _ := do(b, "GET", "/api/v1/rooms/"+roomID, nil); status != 404 {
			t.Fatalf("closed room for a member=%d", status)
		}
		if exists("rooms", roomID) {
			t.Fatal("closed room still stored")
		}
	})

	t.Run("rooms nobody has opened for an hour are deleted with their games", func(t *testing.T) {
		a, _ := guest("Idle")
		_, room := do(a, "POST", "/api/v1/rooms", map[string]any{"name": "Forgotten", "capacity": 2})
		idle := room["id"].(string)
		_, fresh := do(a, "POST", "/api/v1/rooms", map[string]any{"name": "Recent", "capacity": 2})
		if _, err := pool.Exec(ctx, "UPDATE rooms SET created_at=now()-interval '2 hours' WHERE id=ANY($1)", []string{idle, fresh["id"].(string)}); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, "UPDATE room_members SET last_seen_at=now()-interval '61 minutes' WHERE room_id=$1", idle); err != nil {
			t.Fatal(err)
		}
		// Each sweep deletes at most 200 rooms, oldest first; repeat until the
		// backlog (old rooms left by other tests) is gone.
		for {
			n, err := app.Hub.Rooms.DeleteIdle(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
		}
		if exists("rooms", idle) {
			t.Fatal("idle room survived")
		}
		if !exists("rooms", fresh["id"].(string)) {
			t.Fatal("a room seen within the hour was deleted")
		}
	})

	t.Run("guests unused for 7 days are deleted; active guests stay signed in", func(t *testing.T) {
		stale, staleID := guest("Stale")
		active, activeID := guest("Active")
		if _, err := pool.Exec(ctx, "UPDATE users SET last_active_at=now()-interval '8 days' WHERE id=$1", staleID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, "UPDATE users SET last_active_at=now()-interval '2 hours' WHERE id=$1", activeID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, "UPDATE sessions SET expires_at=now()+interval '1 day' WHERE user_id=$1", activeID); err != nil {
			t.Fatal(err)
		}
		// Any request records activity and slides the guest's session.
		if status, _ := do(active, "GET", "/api/v1/me", nil); status != 200 {
			t.Fatal("active guest signed out")
		}
		var expires time.Time
		if err := pool.QueryRow(ctx, "SELECT max(expires_at) FROM sessions WHERE user_id=$1", activeID).Scan(&expires); err != nil || time.Until(expires) < 6*24*time.Hour {
			t.Fatalf("session did not slide: %v %v", expires, err)
		}
		mod := &accounts.Module{DB: pool, Config: c}
		if err := mod.PurgeIdleGuests(ctx); err != nil {
			t.Fatal(err)
		}
		if status, _ := do(stale, "GET", "/api/v1/me", nil); status != 401 {
			t.Fatalf("stale guest still signed in: %d", status)
		}
		if status, _ := do(active, "GET", "/api/v1/me", nil); status != 200 {
			t.Fatal("active guest was purged")
		}
	})
}
