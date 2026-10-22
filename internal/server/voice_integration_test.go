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

// TestVoiceSignalling exercises the server's half of voice chat: who may
// join a channel, who may be addressed, and what a player is told when they
// may not. Audio itself never reaches this server, so what is tested here is
// exactly what it is responsible for.
func TestVoiceSignalling(t *testing.T) {
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
		return map[string]string{
			"DATABASE_URL":      dsn,
			"APP_ORIGIN":        "http://localhost:3000",
			"APP_ENV":           "test",
			"VOICE_TURN_URLS":   "turn:turn.example:3478?transport=udp",
			"VOICE_TURN_SECRET": "integration-test-secret",
		}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, pool: pool, c: c, q: store.New(pool)}
	inst := startInstance(t, pool, c)

	aliceID, alice := e.user("voicea")
	_, bob := e.user("voiceb")
	malloryID, mallory := e.user("voicem")

	// Alice hosts a room; Bob joins it; Mallory is in a room of her own.
	_, room := e.do(inst, "POST", "/api/v1/rooms", alice, map[string]any{"name": "voice room", "capacity": 4})
	roomID := room["id"].(string)
	status, invite := e.do(inst, "POST", "/api/v1/rooms/"+roomID+"/invitations", alice, map[string]any{})
	if status >= 300 {
		t.Fatalf("invitation failed: %d %v", status, invite)
	}
	if status, body := e.do(inst, "POST", "/api/v1/rooms/join", bob, map[string]any{"token": invite["token"]}); status >= 300 {
		t.Fatalf("bob could not join: %d %v", status, body)
	}
	_, other := e.do(inst, "POST", "/api/v1/rooms", mallory, map[string]any{"name": "another room", "capacity": 4})
	otherID := other["id"].(string)

	t.Run("ICE credentials are issued to the player who asks", func(t *testing.T) {
		status, body := e.do(inst, "GET", "/api/v1/voice/ice", alice, nil)
		if status != 200 {
			t.Fatalf("status %d: %v", status, body)
		}
		if body["enabled"] != true || body["relay_available"] != true {
			t.Fatalf("voice should be enabled with a relay: %v", body)
		}
		servers, _ := body["ice_servers"].([]any)
		if len(servers) == 0 {
			t.Fatal("no ICE servers were issued")
		}
		relay := servers[len(servers)-1].(map[string]any)
		username, _ := relay["username"].(string)
		if username == "" || relay["credential"] == "" {
			t.Fatalf("the TURN entry carries no credential: %v", relay)
		}
		// The credential names the caller, not whoever asked first.
		if want := ":" + aliceID; len(username) <= len(want) || username[len(username)-len(want):] != want {
			t.Fatalf("username %q does not name the caller", username)
		}
		// A signed-out caller gets nothing at all.
		if status, _ := e.do(inst, "GET", "/api/v1/voice/ice", "", nil); status != 401 {
			t.Fatalf("an unauthenticated request returned %d, want 401", status)
		}
	})

	ac := e.dial(inst, alice)
	bc := e.dial(inst, bob)
	mc := e.dial(inst, mallory)

	t.Run("a player must be in the room to join its channel", func(t *testing.T) {
		reply := mc.reply(mc.send(map[string]any{"type": "voice.join", "room_id": roomID}))
		if reply.msg["type"] != "error" {
			t.Fatalf("a stranger joined the channel: %v", reply.msg)
		}
		if code := reply.msg["payload"].(map[string]any)["code"]; code != "NOT_FOUND" {
			t.Fatalf("refused with %v, want NOT_FOUND", code)
		}
	})

	t.Run("signalling needs a channel", func(t *testing.T) {
		reply := ac.reply(ac.send(map[string]any{
			"type": "voice.signal", "room_id": roomID,
			"payload": map[string]any{"to": uuid(), "kind": "offer", "data": map[string]any{"sdp": "x"}},
		}))
		if code := reply.msg["payload"].(map[string]any)["code"]; code != "VOICE_NOT_JOINED" {
			t.Fatalf("refused with %v, want VOICE_NOT_JOINED", code)
		}
	})

	t.Run("two players in a room reach each other", func(t *testing.T) {
		joined := ac.reply(ac.send(map[string]any{"type": "voice.join", "room_id": roomID}))
		if joined.msg["type"] != "voice.joined" {
			t.Fatalf("alice could not join: %v", joined.msg)
		}
		payload := joined.msg["payload"].(map[string]any)
		if policy := payload["policy"].(map[string]any); policy["open"] != true {
			t.Fatalf("voice should be open with no match running: %v", policy)
		}
		bjoined := bc.reply(bc.send(map[string]any{"type": "voice.join", "room_id": roomID}))
		if bjoined.msg["type"] != "voice.joined" {
			t.Fatalf("bob could not join: %v", bjoined.msg)
		}
		bobID := bjoined.msg["payload"].(map[string]any)["self"].(string)
		// Alice is told Bob arrived.
		f := ac.await("bob joining", func(f frame) bool { return f.msg["type"] == "voice.participants" })
		list := f.msg["payload"].(map[string]any)["participants"].([]any)
		if len(list) != 2 {
			t.Fatalf("the channel holds %d players, want 2", len(list))
		}

		// An offer from Alice reaches Bob, unchanged and attributed.
		id := ac.send(map[string]any{
			"type": "voice.signal", "room_id": roomID,
			"payload": map[string]any{"to": bobID, "kind": "offer", "data": map[string]any{"sdp": "v=0 fake"}},
		})
		got := bc.await("the offer", func(f frame) bool { return f.msg["type"] == "voice.signal" })
		sig := got.msg["payload"].(map[string]any)
		if sig["kind"] != "offer" || sig["from"] != payload["self"] {
			t.Fatalf("bob received %v", sig)
		}
		if data := sig["data"].(map[string]any); data["sdp"] != "v=0 fake" {
			t.Fatalf("the description was altered: %v", data)
		}
		if reply := ac.reply(id); reply.msg["type"] != "ack" {
			t.Fatalf("a relayed signal was not acknowledged: %v", reply.msg)
		}
	})

	t.Run("a signal cannot cross into another room", func(t *testing.T) {
		// Mallory joins her own room's channel, so she is in a channel --
		// just not this one.
		if r := mc.reply(mc.send(map[string]any{"type": "voice.join", "room_id": otherID})); r.msg["type"] != "voice.joined" {
			t.Fatalf("mallory could not join her own room: %v", r.msg)
		}
		state := mc.reply(mc.send(map[string]any{"type": "voice.state", "room_id": otherID,
			"payload": map[string]any{"muted": false, "deafened": false, "speaking": false}}))
		if state.msg["type"] != "ack" {
			t.Fatalf("state in her own channel was refused: %v", state.msg)
		}
		// Alice addresses her anyway, naming her correctly.
		reply := ac.reply(ac.send(map[string]any{
			"type": "voice.signal", "room_id": roomID,
			"payload": map[string]any{"to": malloryID, "kind": "offer", "data": map[string]any{"sdp": "x"}},
		}))
		if reply.msg["type"] != "error" {
			t.Fatalf("a signal crossed rooms: %v", reply.msg)
		}
		if code := reply.msg["payload"].(map[string]any)["code"]; code != "VOICE_NO_PEER" {
			t.Fatalf("refused with %v, want VOICE_NO_PEER", code)
		}
	})

	t.Run("a malformed signal is refused", func(t *testing.T) {
		for _, payload := range []map[string]any{
			{"to": uuid(), "kind": "shout", "data": map[string]any{}},
			{"to": "not-a-user", "kind": "offer", "data": map[string]any{}},
		} {
			reply := ac.reply(ac.send(map[string]any{"type": "voice.signal", "room_id": roomID, "payload": payload}))
			if reply.msg["type"] != "error" {
				t.Fatalf("%v was accepted", payload)
			}
			if code := reply.msg["payload"].(map[string]any)["code"]; code != "INVALID_REQUEST" {
				t.Fatalf("%v refused with %v", payload, code)
			}
		}
	})

	t.Run("leaving empties the channel", func(t *testing.T) {
		if r := bc.reply(bc.send(map[string]any{"type": "voice.leave", "room_id": roomID})); r.msg["type"] != "ack" {
			t.Fatalf("leaving failed: %v", r.msg)
		}
		f := ac.await("bob leaving", func(f frame) bool {
			if f.msg["type"] != "voice.participants" {
				return false
			}
			return len(f.msg["payload"].(map[string]any)["participants"].([]any)) == 1
		})
		if f.msg["type"] != "voice.participants" {
			t.Fatal("alice was not told")
		}
	})
}
