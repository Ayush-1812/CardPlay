package server_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Verifies that room invalidations reach sockets on another API instance and
// that an ended session closes the socket with code 4001.
func TestRealtimeAcrossInstances(t *testing.T) {
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
	q := store.New(pool)
	suffix := httpx.Token()[:12]
	create := func(handle string) (string, string) {
		u, err := q.CreateUser(ctx, store.CreateUserParams{Email: handle + suffix + "@test.invalid", Handle: handle + suffix, DisplayName: handle, PasswordHash: accounts.PasswordHash("test-password-123"), EmailVerified: true})
		if err != nil {
			t.Fatal(err)
		}
		raw := httpx.Token()
		if err = q.CreateSession(ctx, store.CreateSessionParams{TokenHash: httpx.Hash(raw), UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		return u.ID, raw
	}
	_, aliceToken := create("rta")
	_, bobToken := create("rtb")
	c, _ := config.Parse(func(k string) string {
		return map[string]string{"DATABASE_URL": dsn, "APP_ORIGIN": "http://localhost:3000", "APP_ENV": "test"}[k]
	})
	// Two independent API instances sharing one database.
	appA, appB := server.New(pool, c), server.New(pool, c)
	srvA, srvB := httptest.NewServer(appA.Handler), httptest.NewServer(appB.Handler)
	defer srvA.Close()
	defer srvB.Close()
	go appA.Hub.Run(ctx)
	go appB.Hub.Run(ctx)
	time.Sleep(500 * time.Millisecond) // let both listeners LISTEN
	do := func(base, method, path, token string, body any) (int, map[string]any) {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, base+path, bytes.NewReader(b))
		req.Header.Set("Origin", c.Origin)
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: accounts.CookieName, Value: token})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	_, room := do(srvA.URL, "POST", "/api/v1/rooms", aliceToken, map[string]any{"name": "rt", "capacity": 2})
	roomID := room["id"].(string)
	_, inv := do(srvA.URL, "POST", "/api/v1/rooms/"+roomID+"/invitations", aliceToken, map[string]any{})
	if s, _ := do(srvA.URL, "POST", "/api/v1/rooms/join", bobToken, map[string]any{"token": inv["token"]}); s != 200 {
		t.Fatalf("join=%d", s)
	}
	dial := func(base, token string) *websocket.Conn {
		h := http.Header{}
		h.Set("Origin", c.Origin)
		h.Set("Cookie", accounts.CookieName+"="+token)
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+"/ws", &websocket.DialOptions{HTTPHeader: h})
		if err != nil {
			t.Fatal(err)
		}
		return conn
	}
	// Bob listens on instance B.
	bob := dial(srvB.URL, bobToken)
	defer bob.CloseNow()
	_ = wsjson.Write(ctx, bob, map[string]any{"v": 1, "type": "room.subscribe", "id": "11111111-1111-1111-1111-111111111111", "room_id": roomID})
	waitFor := func(conn *websocket.Conn, kind string) {
		t.Helper()
		wc, done := context.WithTimeout(ctx, 5*time.Second)
		defer done()
		for {
			var m map[string]any
			if err := wsjson.Read(wc, conn, &m); err != nil {
				t.Fatalf("waiting for %s: %v", kind, err)
			}
			if m["type"] == kind {
				return
			}
		}
	}
	waitFor(bob, "room.snapshot")
	// Alice chats through instance A; Bob on instance B must be told.
	if s, _ := do(srvA.URL, "POST", "/api/v1/rooms/"+roomID+"/chat", aliceToken, map[string]any{"body": "hi", "client_id": "22222222-2222-2222-2222-222222222222"}); s != 200 {
		t.Fatalf("chat=%d", s)
	}
	waitFor(bob, "chat.updated")
	if s, _ := do(srvA.URL, "PUT", "/api/v1/rooms/"+roomID+"/ready", aliceToken, map[string]any{"ready": true}); s != 204 {
		t.Fatalf("ready=%d", s)
	}
	waitFor(bob, "room.updated")
	// Revoked session: next command closes the socket with 4001.
	if _, err = pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash=$1", httpx.Hash(bobToken)); err != nil {
		t.Fatal(err)
	}
	_ = wsjson.Write(ctx, bob, map[string]any{"v": 1, "type": "ping", "id": "33333333-3333-3333-3333-333333333333"})
	var m map[string]any
	err = wsjson.Read(ctx, bob, &m)
	if websocket.CloseStatus(err) != 4001 {
		t.Fatalf("revoked session close=%v", err)
	}
}
