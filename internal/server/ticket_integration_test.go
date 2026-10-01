package server_test

import (
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
	"github.com/jackc/pgx/v5/pgxpool"
)

// A client hosted on another site cannot send its session cookie with the
// WebSocket handshake, so it asks for a one-shot ticket over the authenticated
// HTTP path. The ticket opens exactly one socket and then it is spent.
func TestSocketTicket(t *testing.T) {
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
	u, err := q.CreateUser(ctx, store.CreateUserParams{Email: "tkt" + suffix + "@test.invalid", Handle: "tkt" + suffix, DisplayName: "Ticket", PasswordHash: accounts.PasswordHash("test-password-123"), EmailVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	session := httpx.Token()
	if err = q.CreateSession(ctx, store.CreateSessionParams{TokenHash: httpx.Hash(session), UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	c, _ := config.Parse(func(k string) string {
		return map[string]string{"DATABASE_URL": dsn, "APP_ORIGIN": "http://localhost:3000", "APP_ENV": "test"}[k]
	})
	app := server.New(pool, c)
	srv := httptest.NewServer(app.Handler)
	defer srv.Close()
	go app.Hub.Run(ctx)

	issue := func(token string) (int, string) {
		req, _ := http.NewRequest("POST", srv.URL+"/api/v1/realtime/ticket", nil)
		req.Header.Set("Origin", c.Origin)
		if token != "" {
			req.AddCookie(&http.Cookie{Name: accounts.CookieName, Value: token})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			Ticket string `json:"ticket"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out.Ticket
	}
	// Dials without any cookie, exactly as a client on another site would.
	dial := func(query string) (*websocket.Conn, int) {
		h := http.Header{}
		h.Set("Origin", c.Origin)
		conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws"+query, &websocket.DialOptions{HTTPHeader: h})
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		if err != nil {
			return nil, status
		}
		return conn, status
	}

	t.Run("a ticket needs a session", func(t *testing.T) {
		if status, _ := issue(""); status != 401 {
			t.Fatalf("status=%d, want 401", status)
		}
	})

	t.Run("no credential at all is refused", func(t *testing.T) {
		conn, status := dial("")
		if conn != nil {
			conn.CloseNow()
			t.Fatal("the socket opened without a credential")
		}
		if status != 401 {
			t.Fatalf("status=%d, want 401", status)
		}
	})

	t.Run("a ticket opens the socket once", func(t *testing.T) {
		status, ticket := issue(session)
		if status != 201 || len(ticket) != 64 {
			t.Fatalf("status=%d ticket=%q", status, ticket)
		}
		conn, _ := dial("?ticket=" + ticket)
		if conn == nil {
			t.Fatal("the ticket did not open the socket")
		}
		conn.CloseNow()
		// Spent: the same ticket cannot open a second socket.
		again, status := dial("?ticket=" + ticket)
		if again != nil {
			again.CloseNow()
			t.Fatal("a spent ticket opened another socket")
		}
		if status != 401 {
			t.Fatalf("reuse status=%d, want 401", status)
		}
	})

	t.Run("a forged ticket is refused", func(t *testing.T) {
		conn, status := dial("?ticket=" + httpx.Token())
		if conn != nil {
			conn.CloseNow()
			t.Fatal("a forged ticket opened the socket")
		}
		if status != 401 {
			t.Fatalf("status=%d, want 401", status)
		}
	})

	t.Run("a ticket dies with its session", func(t *testing.T) {
		_, ticket := issue(session)
		if err := q.DeleteSession(ctx, httpx.Hash(session)); err != nil {
			t.Fatal(err)
		}
		conn, status := dial("?ticket=" + ticket)
		if conn != nil {
			conn.CloseNow()
			t.Fatal("a ticket outlived its session")
		}
		if status != 401 {
			t.Fatalf("status=%d, want 401", status)
		}
	})
}
