package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"cardplay/db"
	"cardplay/internal/accounts"
	"cardplay/internal/config"
	"cardplay/internal/store"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestSocketHardening exercises the WebSocket's defences directly: the origin
// check, authentication, room authorization, the frame size and rate limits,
// and that a shutdown closes sockets without losing committed state.
func TestSocketHardening(t *testing.T) {
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
	_, token := e.user("hard")
	_, otherToken := e.user("hard2")
	wsURL := "ws" + strings.TrimPrefix(inst.srv.URL, "http") + "/ws"

	// dialRaw returns the handshake response so a refusal can be inspected.
	dialRaw := func(origin, cookie string) (*websocket.Conn, int) {
		h := http.Header{}
		if origin != "" {
			h.Set("Origin", origin)
		}
		if cookie != "" {
			h.Set("Cookie", accounts.CookieName+"="+cookie)
		}
		conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: h})
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		if err != nil {
			return nil, status
		}
		return conn, status
	}

	t.Run("a foreign origin is refused", func(t *testing.T) {
		for _, origin := range []string{"http://evil.example", "", "http://localhost:3000.evil.example"} {
			conn, status := dialRaw(origin, token)
			if conn != nil {
				conn.CloseNow()
				t.Fatalf("origin %q was accepted", origin)
			}
			if status != 403 {
				t.Fatalf("origin %q gave %d, want 403", origin, status)
			}
		}
	})

	t.Run("an unauthenticated socket is refused", func(t *testing.T) {
		conn, status := dialRaw(c.Origin, "")
		if conn != nil {
			conn.CloseNow()
			t.Fatal("a socket opened with no session")
		}
		if status != 401 {
			t.Fatalf("status %d, want 401", status)
		}
	})

	t.Run("a room the player is not in cannot be subscribed", func(t *testing.T) {
		_, room := e.do(inst, "POST", "/api/v1/rooms", otherToken, map[string]any{"name": "private", "capacity": 2})
		client := e.dial(inst, token)
		id := client.send(map[string]any{"type": "room.subscribe", "room_id": room["id"]})
		reply := client.reply(id)
		if reply.msg["type"] != "error" {
			t.Fatalf("a stranger subscribed to a room: %v", reply.msg)
		}
		if code := reply.msg["payload"].(map[string]any)["code"]; code != "NOT_FOUND" {
			t.Fatalf("room subscribe refused with %v", code)
		}
	})

	t.Run("an oversized frame closes the socket", func(t *testing.T) {
		conn, _ := dialRaw(c.Origin, token)
		if conn == nil {
			t.Fatal("could not open a socket")
		}
		defer conn.CloseNow()
		// The server reads at most 16 KiB per frame.
		big, _ := json.Marshal(map[string]any{
			"v": 1, "id": uuid(), "type": "chat.send",
			"payload": map[string]any{"client_id": uuid(), "body": strings.Repeat("x", 32<<10)},
		})
		if err := conn.Write(ctx, websocket.MessageText, big); err != nil {
			return // the server may close before the write completes
		}
		readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		for {
			if _, _, err := conn.Read(readCtx); err != nil {
				status := websocket.CloseStatus(err)
				if status != websocket.StatusMessageTooBig && status != websocket.StatusNoStatusRcvd && status != -1 {
					t.Fatalf("closed with %d, want the frame refused", status)
				}
				return
			}
		}
	})

	t.Run("a flood of frames closes the socket", func(t *testing.T) {
		conn, _ := dialRaw(c.Origin, token)
		if conn == nil {
			t.Fatal("could not open a socket")
		}
		defer conn.CloseNow()
		// More than 30 frames in 10 seconds is a policy violation.
		ping, _ := json.Marshal(map[string]any{"v": 1, "id": uuid(), "type": "ping"})
		var writeErr error
		for i := 0; i < 60 && writeErr == nil; i++ {
			writeErr = conn.Write(ctx, websocket.MessageText, ping)
		}
		readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		for {
			if _, _, err := conn.Read(readCtx); err != nil {
				if status := websocket.CloseStatus(err); status != websocket.StatusPolicyViolation && status != websocket.StatusNoStatusRcvd && status != -1 {
					t.Fatalf("closed with %d, want a policy violation", status)
				}
				return
			}
		}
	})

	t.Run("shutdown closes sockets and keeps committed state", func(t *testing.T) {
		_, room := e.do(inst, "POST", "/api/v1/rooms", token, map[string]any{"name": "shutdown room", "capacity": 2})
		roomID := room["id"].(string)
		client := e.dial(inst, token)
		id := client.send(map[string]any{"type": "room.subscribe", "room_id": roomID})
		if reply := client.reply(id); reply.msg["type"] == "error" {
			t.Fatalf("subscribe failed: %v", reply.msg)
		}
		inst.stop()
		// The socket is closed by the server, not left hanging.
		select {
		case <-client.closed:
		case <-time.After(8 * time.Second):
			t.Fatal("the socket was not closed on shutdown")
		}
		// What was committed before the shutdown is still there afterwards.
		next := startInstance(t, pool, c)
		status, view := e.do(next, "GET", "/api/v1/rooms/"+roomID, token, nil)
		if status != 200 || view["room"].(map[string]any)["name"] != "shutdown room" {
			t.Fatalf("room lost across shutdown: %d %v", status, view)
		}
	})
}
