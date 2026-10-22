package server_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"cardplay/db"
	"cardplay/internal/config"
	"cardplay/internal/game/bluff"
	"cardplay/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestBluffLiveMatch plays Bluff through the real socket: four authenticated
// players, one server, one database. What is checked here is everything the
// engine cannot check on its own -- who may act, what each player is told,
// what happens when two challenges arrive at once, and that a reconnection
// restores a hand rather than dealing one.
func TestBluffLiveMatch(t *testing.T) {
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

	const seats = 4
	ids := make([]string, seats)
	tokens := make([]string, seats)
	for i := range seats {
		ids[i], tokens[i] = e.user("bluff")
	}
	_, room := e.do(inst, "POST", "/api/v1/rooms", tokens[0], map[string]any{"name": "bluff table", "capacity": seats, "game": "bluff"})
	roomID := room["id"].(string)
	for i := 1; i < seats; i++ {
		_, invite := e.do(inst, "POST", "/api/v1/rooms/"+roomID+"/invitations", tokens[0], map[string]any{})
		if status, body := e.do(inst, "POST", "/api/v1/rooms/join", tokens[i], map[string]any{"token": invite["token"]}); status >= 300 {
			t.Fatalf("player %d could not join: %d %v", i, status, body)
		}
	}
	for i := range seats {
		if status, body := e.do(inst, "PUT", "/api/v1/rooms/"+roomID+"/ready", tokens[i], map[string]any{"ready": true}); status != 204 {
			t.Fatalf("player %d could not ready: %d %v", i, status, body)
		}
	}
	status, started := e.do(inst, "POST", "/api/v1/rooms/"+roomID+"/matches", tokens[0], nil)
	if status != 201 {
		t.Fatalf("the match did not start: %d %v", status, started)
	}
	matchID := started["match_id"].(string)

	clients := make([]*wsClient, seats)
	states := make([]map[string]any, seats)
	for i := range seats {
		clients[i] = e.dial(inst, tokens[i])
		clients[i].send(map[string]any{"type": "match.subscribe", "match_id": matchID})
		states[i], _ = clients[i].state("the deal", func(p map[string]any) bool { return p["view"] != nil })
	}
	// seatOf maps a player index to the seat the engine gave them.
	seatOf := make([]int, seats)
	for i := range seats {
		seatOf[i] = int(selfOf(states[i])["seat"].(float64))
	}

	// The server's own snapshot is the truth every projection is checked
	// against.
	snapshot := func() *bluff.State {
		t.Helper()
		row, err := e.q.RoomGameState(ctx, roomID)
		if err != nil {
			t.Fatal(err)
		}
		var full bluff.State
		if err := json.Unmarshal(row.State, &full); err != nil {
			t.Fatal(err)
		}
		return &full
	}
	playerAt := func(seat int) int {
		for i, s := range seatOf {
			if s == seat {
				return i
			}
		}
		t.Fatalf("no player at seat %d", seat)
		return -1
	}

	t.Run("the whole deck is dealt and nobody sees another hand", func(t *testing.T) {
		full := snapshot()
		total := len(full.Pile)
		for _, p := range full.Players {
			total += len(p.Hand)
		}
		if total != bluff.DeckSize {
			t.Fatalf("%d cards in play, want %d", total, bluff.DeckSize)
		}
		for i := range seats {
			status, body := e.do(inst, "GET", "/api/v1/matches/"+matchID, tokens[i], nil)
			if status != 200 {
				t.Fatalf("player %d cannot read the match: %d", i, status)
			}
			blob, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			text := string(blob)
			for other := range full.Players {
				if other == seatOf[i] {
					continue
				}
				for _, id := range full.Players[other].Hand {
					if strings.Contains(text, string(id)) {
						t.Fatalf("seat %d can see seat %d's %s", seatOf[i], other, id)
					}
				}
			}
			own := full.Players[seatOf[i]].Hand
			if len(own) == 0 || !strings.Contains(text, string(own[0])) {
				t.Fatalf("seat %d cannot see its own cards", seatOf[i])
			}
		}
	})

	// play puts one card down for whoever is on turn and returns the
	// resulting public view.
	play := func(t *testing.T) (int, map[string]any) {
		t.Helper()
		full := snapshot()
		i := playerAt(full.Turn)
		card := full.Players[full.Turn].Hand[0]
		reply := clients[i].command(matchID, float64(snapshotRevision(t, e, matchID)), "play_cards", map[string]any{"cards": []string{string(card)}})
		if reply.msg["type"] != "ack" {
			t.Fatalf("the play was refused: %v", reply.msg)
		}
		next, _ := clients[i].state("the play", func(p map[string]any) bool {
			return publicOf(p)["phase"] == "challenge"
		})
		states[i] = next
		return i, publicOf(next)
	}

	t.Run("a play announces its count and rank, and nothing else", func(t *testing.T) {
		player, pub := play(t)
		last := pub["last"].(map[string]any)
		if int(last["count"].(float64)) != 1 {
			t.Fatalf("the claim says %v cards", last["count"])
		}
		if last["rank"] == nil || last["rank_name"] == nil {
			t.Fatalf("the claim carries no rank: %v", last)
		}
		if _, leaked := last["cards"]; leaked {
			t.Fatal("the claim carries the cards behind it")
		}
		if pub["deadline"] == nil {
			t.Fatal("the challenge window has no deadline")
		}
		// Everyone else is told the same thing, and may answer.
		for i := range seats {
			if i == player {
				continue
			}
			st, _ := clients[i].state("the claim", func(p map[string]any) bool {
				return publicOf(p)["phase"] == "challenge"
			})
			states[i] = st
			if selfOf(st)["may_answer"] != true {
				t.Fatalf("player %d cannot answer the play", i)
			}
		}
		// The player who made it cannot answer their own play.
		if selfOf(states[player])["may_answer"] == true {
			t.Fatal("the player may answer their own play")
		}
	})

	t.Run("two challenges at once apply exactly once", func(t *testing.T) {
		full := snapshot()
		if full.Phase != bluff.PhaseChallenge {
			t.Skip("the window closed before the race could be run")
		}
		pileBefore := len(full.Pile)
		player := playerAt(full.Last.Seat)
		var challengers []int
		for i := range seats {
			if i != player {
				challengers = append(challengers, i)
			}
		}
		rev := float64(snapshotRevision(t, e, matchID))
		var wg sync.WaitGroup
		replies := make([]string, len(challengers))
		for n, i := range challengers {
			wg.Add(1)
			go func(n, i int) {
				defer wg.Done()
				reply := clients[i].command(matchID, rev, "call_bluff", map[string]any{"play_id": full.Last.ID})
				replies[n] = reply.msg["type"].(string)
			}(n, i)
		}
		wg.Wait()
		acks := 0
		for _, r := range replies {
			if r == "ack" {
				acks++
			}
		}
		if acks != 1 {
			t.Fatalf("%d of %d challenges were accepted, want exactly one: %v", acks, len(replies), replies)
		}
		after := snapshot()
		if after.Result == nil {
			t.Fatal("the challenge was not resolved")
		}
		// Exactly one penalty: the pile moved to one hand, once.
		if len(after.Pile) != 0 {
			t.Fatalf("the pile still holds %d cards", len(after.Pile))
		}
		collector := after.Result.Collector
		if got := len(after.Players[collector].Hand); got < pileBefore {
			t.Fatalf("the collector holds %d cards, fewer than the %d-card pile", got, pileBefore)
		}
		// Only the challenged batch was revealed.
		if len(after.Result.Cards) != after.Result.Collected && len(after.Result.Cards) > 1 {
			t.Fatalf("%d cards were revealed for a one-card play", len(after.Result.Cards))
		}
		total := len(after.Pile)
		for _, p := range after.Players {
			total += len(p.Hand)
		}
		if total != bluff.DeckSize {
			t.Fatalf("%d cards after the challenge, want %d", total, bluff.DeckSize)
		}
	})

	t.Run("a challenge after the deadline is refused", func(t *testing.T) {
		player, _ := play(t)
		full := snapshot()
		deadline := full.Deadline
		// Wait the window out. The platform closes it within about a
		// second; either way a challenge arriving now is late.
		time.Sleep(time.Until(deadline) + 300*time.Millisecond)
		late := (player + 1) % seats
		rev := float64(snapshotRevision(t, e, matchID))
		reply := clients[late].command(matchID, rev, "call_bluff", map[string]any{"play_id": full.Last.ID})
		if reply.msg["type"] != "error" {
			t.Fatalf("a late challenge was accepted: %v", reply.msg)
		}
	})

	t.Run("the window closes itself and play moves on", func(t *testing.T) {
		if snapshot().Phase != bluff.PhaseChallenge {
			play(t)
		}
		before := snapshot()
		if before.Phase != bluff.PhaseChallenge {
			t.Fatalf("no challenge window to close: phase %s", before.Phase)
		}
		// The sweep runs once a second; give it a few.
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if after := snapshot(); after.Phase != bluff.PhaseChallenge {
				// Nobody answered, so the play stands: the next seat plays
				// the next rank, and nothing was revealed.
				if after.Turn != (before.Last.Seat+1)%seats {
					t.Fatalf("turn went to seat %d, want the seat after %d", after.Turn, before.Last.Seat)
				}
				if after.Rank == before.Rank {
					t.Fatalf("the required rank did not advance from %s", before.Rank)
				}
				if after.Result != nil && after.Result.PlayID == before.Last.ID {
					t.Fatal("an unchallenged play was revealed")
				}
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
		t.Fatal("the challenge window never closed")
	})

	t.Run("a reconnection restores the hand rather than dealing one", func(t *testing.T) {
		full := snapshot()
		i := playerAt(full.Turn)
		before := selfOf(states[i])["hand"]
		fresh := e.dial(inst, tokens[i])
		fresh.send(map[string]any{"type": "match.subscribe", "match_id": matchID})
		after, _ := fresh.state("the restored hand", func(p map[string]any) bool { return p["view"] != nil })
		if got := selfOf(after)["hand"]; !sameCards(before, got) {
			t.Fatalf("the hand changed on reconnect: before %v, after %v", before, got)
		}
		if len(snapshot().Pile)+handTotal(snapshot()) != bluff.DeckSize {
			t.Fatal("the deck changed on reconnect")
		}
	})

	t.Run("an outsider can neither watch nor act", func(t *testing.T) {
		_, outsider := e.user("bluffout")
		if status, _ := e.do(inst, "GET", "/api/v1/matches/"+matchID, outsider, nil); status != 404 {
			t.Fatalf("an outsider read the match: %d", status)
		}
		client := e.dial(inst, outsider)
		id := client.send(map[string]any{"type": "match.subscribe", "match_id": matchID})
		if reply := client.reply(id); reply.msg["type"] != "error" {
			t.Fatalf("an outsider subscribed: %v", reply.msg)
		}
	})
}

// snapshotRevision reads the match's committed revision.
func snapshotRevision(t *testing.T, e *env, matchID string) int64 {
	t.Helper()
	mt, err := e.q.Match(context.Background(), matchID)
	if err != nil {
		t.Fatal(err)
	}
	return mt.Revision
}

func handTotal(s *bluff.State) int {
	n := 0
	for _, p := range s.Players {
		n += len(p.Hand)
	}
	return n
}

func sameCards(a, b any) bool {
	left, lok := a.([]any)
	right, rok := b.([]any)
	if !lok || !rok || len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
