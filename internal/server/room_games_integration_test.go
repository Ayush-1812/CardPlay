package server_test

import (
	"context"
	"os"
	"testing"

	"cardplay/db"
	"cardplay/internal/config"
	"cardplay/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestRoomChangesGame covers a group that has finished one game and wants
// another. The point of a room is the people in it, so changing the game
// must not cost them the lobby, the invitations or -- on a phone, where it
// matters most -- the voice call they are already in.
func TestRoomChangesGame(t *testing.T) {
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
	a := startInstance(t, pool, c)

	_, host := e.user("switch-host")
	guestID, guest := e.user("switch-guest")

	_, room := e.do(a, "POST", "/api/v1/rooms", host, map[string]any{"name": "the usual four", "game": "trump"})
	roomID, _ := room["id"].(string)
	if roomID == "" || room["game_id"] != "trump" {
		t.Fatalf("no trump room: %v", room)
	}

	// Somebody else joins, so the switch has to be checked against a room
	// that has people in it rather than an empty one.
	_, invite := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/invitations", host, map[string]any{})
	if status, body := e.do(a, "POST", "/api/v1/rooms/join", guest, map[string]any{"token": invite["token"]}); status != 200 {
		t.Fatalf("join=%d %v", status, body)
	}
	if status, body := e.do(a, "PUT", "/api/v1/rooms/"+roomID+"/ready", guest, map[string]any{"ready": true}); status != 200 {
		t.Fatalf("ready=%d %v", status, body)
	}

	t.Run("the host switches the room to another game", func(t *testing.T) {
		status, body := e.do(a, "PATCH", "/api/v1/rooms/"+roomID, host, map[string]any{
			"name": "the usual four", "capacity": 4, "game": "bluff",
		})
		if status != 200 {
			t.Fatalf("switch to bluff=%d %v", status, body)
		}
		_, view := e.do(a, "GET", "/api/v1/rooms/"+roomID, host, nil)
		got := view["room"].(map[string]any)
		if got["game_id"] != "bluff" {
			t.Fatalf("the room did not change game: %v", got)
		}
		// The people are the thing that survives: everyone is still in
		// their seat.
		if len(view["members"].([]any)) != 2 {
			t.Fatalf("members lost in the switch: %v", view["members"])
		}
	})

	t.Run("nobody is left ready for a game they did not agree to", func(t *testing.T) {
		_, view := e.do(a, "GET", "/api/v1/rooms/"+roomID, host, nil)
		for _, m := range view["members"].([]any) {
			member := m.(map[string]any)
			if member["ready"] == true {
				t.Fatalf("%v is still ready after the game changed", member["display_name"])
			}
		}
		_ = guestID
	})

	t.Run("a capacity the new game cannot seat is refused", func(t *testing.T) {
		// Bluff seats up to six; Trump is exactly four. Switching back
		// with six would leave a room that cannot start.
		status, body := e.do(a, "PATCH", "/api/v1/rooms/"+roomID, host, map[string]any{
			"name": "the usual four", "capacity": 6, "game": "trump",
		})
		if status != 400 {
			t.Fatalf("six seats accepted for trump: %d %v", status, body)
		}
		_, view := e.do(a, "GET", "/api/v1/rooms/"+roomID, host, nil)
		if view["room"].(map[string]any)["game_id"] != "bluff" {
			t.Fatal("a refused switch changed the game anyway")
		}
	})

	t.Run("a game nobody has is refused", func(t *testing.T) {
		_, body := e.do(a, "PATCH", "/api/v1/rooms/"+roomID, host, map[string]any{
			"name": "the usual four", "capacity": 4, "game": "snap",
		})
		if body["error"].(map[string]any)["code"] != "UNKNOWN_GAME" {
			t.Fatalf("unknown game accepted: %v", body)
		}
	})

	t.Run("only the host decides what is being played", func(t *testing.T) {
		status, _ := e.do(a, "PATCH", "/api/v1/rooms/"+roomID, guest, map[string]any{
			"name": "my choice", "capacity": 4, "game": "trump",
		})
		if status != 404 {
			t.Fatalf("a member changed the game: %d", status)
		}
	})

	t.Run("the game cannot change under a match in progress", func(t *testing.T) {
		// Four players, everyone ready, and a Trump match started: the
		// room is no longer waiting, so it is no longer up for discussion.
		if status, body := e.do(a, "PATCH", "/api/v1/rooms/"+roomID, host, map[string]any{
			"name": "the usual four", "capacity": 4, "game": "trump",
		}); status != 200 {
			t.Fatalf("switch back to trump=%d %v", status, body)
		}
		tokens := []string{host, guest}
		for i := 0; i < 2; i++ {
			_, extra := e.user("switch-extra")
			tokens = append(tokens, extra)
			_, link := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/invitations", host, map[string]any{})
			if status, body := e.do(a, "POST", "/api/v1/rooms/join", extra, map[string]any{"token": link["token"]}); status != 200 {
				t.Fatalf("extra join=%d %v", status, body)
			}
		}
		for _, token := range tokens {
			if status, body := e.do(a, "PUT", "/api/v1/rooms/"+roomID+"/ready", token, map[string]any{"ready": true}); status != 200 {
				t.Fatalf("ready=%d %v", status, body)
			}
		}
		if status, body := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/matches", host, nil); status != 201 && status != 200 {
			t.Fatalf("start=%d %v", status, body)
		}
		status, body := e.do(a, "PATCH", "/api/v1/rooms/"+roomID, host, map[string]any{
			"name": "the usual four", "capacity": 4, "game": "bluff",
		})
		if status != 409 {
			t.Fatalf("the game changed mid-match: %d %v", status, body)
		}
	})
}
