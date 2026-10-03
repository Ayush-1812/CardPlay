package server_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"cardplay/internal/game"

	"cardplay/db"
	"cardplay/internal/config"
	"cardplay/internal/game/trump"
	"cardplay/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seatOfUser finds a user's seat in a match.state frame.
func seatOfUser(st map[string]any, user string) int {
	for _, p := range st["participants"].([]any) {
		m := p.(map[string]any)
		if m["user_id"] == user {
			return int(m["seat"].(float64))
		}
	}
	return -1
}

func publicOf(st map[string]any) map[string]any {
	return st["view"].(map[string]any)["public"].(map[string]any)
}

func selfOf(st map[string]any) map[string]any {
	return st["view"].(map[string]any)["self"].(map[string]any)
}

func cardsOf(v any) []string {
	var out []string
	for _, id := range v.([]any) {
		out = append(out, id.(string))
	}
	return out
}

// TestTrumpLiveMatch drives four real WebSocket clients against PostgreSQL:
// authorization, serialization, rejection of out-of-turn, stale, malformed and
// duplicate commands, atomic persistence before broadcast, reconnection and
// resynchronization, recovery after a restart, and the selection-phase chat
// restriction. It also checks that no frame carries another player's cards,
// the undealt remainder or the shuffle seed.
func TestTrumpLiveMatch(t *testing.T) {
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

	names := []string{"tru0", "tru1", "tru2", "tru3"}
	users := make([]string, 4)
	tokens := make([]string, 4)
	for i, n := range names {
		users[i], tokens[i] = e.user(n)
	}

	// A Trump room: the game is chosen at creation and the seat count is
	// pinned to four by the rules engine's descriptor.
	status, bad := e.do(a, "POST", "/api/v1/rooms", tokens[0], map[string]any{"name": "trump table", "capacity": 2, "game": "trump"})
	if status != 400 {
		t.Fatalf("two seats accepted for trump: %d %v", status, bad)
	}
	if _, body := e.do(a, "POST", "/api/v1/rooms", tokens[0], map[string]any{"name": "x", "game": "snap"}); body["error"].(map[string]any)["code"] != "UNKNOWN_GAME" {
		t.Fatalf("unknown game accepted: %v", body)
	}
	_, room := e.do(a, "POST", "/api/v1/rooms", tokens[0], map[string]any{"name": "trump table", "game": "trump"})
	roomID := room["id"].(string)
	if room["game_id"] != "trump" || room["capacity"].(float64) != 4 {
		t.Fatalf("room %v", room)
	}
	// Capacity cannot be changed away from four afterwards.
	if s, _ := e.do(a, "PATCH", "/api/v1/rooms/"+roomID, tokens[0], map[string]any{"name": "trump table", "capacity": 5}); s != 400 {
		t.Fatalf("capacity 5 accepted for trump: %d", s)
	}

	for i := 1; i < 4; i++ {
		_, inv := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/invitations", tokens[0], map[string]any{})
		if s, _ := e.do(a, "POST", "/api/v1/rooms/join", tokens[i], map[string]any{"token": inv["token"]}); s != 200 {
			t.Fatalf("player %d could not join", i)
		}
	}
	for i := range 4 {
		if s, _ := e.do(a, "PUT", "/api/v1/rooms/"+roomID+"/ready", tokens[i], map[string]any{"ready": true}); s != 204 {
			t.Fatalf("player %d could not ready", i)
		}
	}
	if s, body := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/matches", tokens[0], nil); s != 201 {
		t.Fatalf("start=%d %v", s, body)
	}

	// Everyone subscribes; the match leaves 'paused' once all four are present.
	clients := make([]*wsClient, 4)
	states := make([]map[string]any, 4)
	var matchID string
	for i := range 4 {
		clients[i] = e.dial(a, tokens[i])
		if i == 0 {
			_, view := e.do(a, "GET", "/api/v1/rooms/"+roomID, tokens[0], nil)
			matchID = view["match"].(map[string]any)["id"].(string)
		}
		clients[i].send(map[string]any{"type": "match.subscribe", "match_id": matchID})
	}
	for i := range 4 {
		states[i], _ = clients[i].state("playing", func(p map[string]any) bool { return p["status"] == "playing" })
	}

	pub := publicOf(states[0])
	if pub["stage"] != string(trump.StageTrumpDecision) {
		t.Fatalf("stage %v", pub["stage"])
	}
	entitled := int(pub["entitled_team"].(float64))
	toss := entitled
	seatOf := map[string]int{}
	for i, u := range users {
		seatOf[u] = seatOfUser(states[0], u)
		if len(cardsOf(selfOf(states[i])["hand"])) != trump.FirstDeal {
			t.Fatalf("player %d was dealt %d cards", i, len(cardsOf(selfOf(states[i])["hand"])))
		}
	}
	// Index clients by seat so the test can address the table by seat.
	bySeat := make([]*wsClient, 4)
	tokenBySeat := make([]string, 4)
	for i, u := range users {
		bySeat[seatOf[u]] = clients[i]
		tokenBySeat[seatOf[u]] = tokens[i]
	}
	rev := func(st map[string]any) float64 { return st["revision"].(float64) }

	t.Run("chat is closed while the trump is chosen", func(t *testing.T) {
		s, body := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/chat", tokenBySeat[0], map[string]any{"client_id": uuid(), "body": "I have three spades"})
		if s != 409 || body["error"].(map[string]any)["code"] != "CHAT_CLOSED" {
			t.Fatalf("chat during selection: %d %v", s, body)
		}
	})

	t.Run("the chat restriction cannot be bypassed over the socket", func(t *testing.T) {
		// The HTTP path is refused above; the WebSocket path must be too,
		// since both go through the same gate.
		id := bySeat[0].send(map[string]any{"type": "chat.send", "room_id": roomID, "payload": map[string]any{
			"client_id": uuid(), "body": "spades, partner",
		}})
		reply := bySeat[0].reply(id)
		if reply.msg["type"] != "error" {
			t.Fatalf("socket chat during selection: %v", reply.msg)
		}
		if code := reply.msg["payload"].(map[string]any)["code"]; code != "CHAT_CLOSED" {
			t.Fatalf("socket chat refused with %v, want CHAT_CLOSED", code)
		}
		// Nothing was stored, so no other player could have received it.
		stored, err := e.q.ChatPage(ctx, store.ChatPageParams{RoomID: roomID, AfterID: 0, ViewerID: users[0]})
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range stored {
			if strings.Contains(m.Body, "spades, partner") {
				t.Fatal("a refused message was stored anyway")
			}
		}
	})

	t.Run("only the entitled team may choose", func(t *testing.T) {
		outsider := trump.SeatsOfTeam(1 - entitled)[0]
		f := bySeat[outsider].command(matchID, rev(states[0]), "choose_trump", map[string]any{"suit": "hearts"})
		p := f.msg["payload"].(map[string]any)
		if p["code"] != trump.CodeNotYourTurn {
			t.Fatalf("outsider choice: %v", p)
		}
	})

	t.Run("malformed commands are refused", func(t *testing.T) {
		chooser := trump.SeatsOfTeam(entitled)[0]
		for _, bad := range []map[string]any{{"suit": "swords"}, {"suit": "hearts", "extra": 1}} {
			f := bySeat[chooser].command(matchID, rev(states[0]), "choose_trump", bad)
			if f.msg["payload"].(map[string]any)["code"] != trump.CodeInvalidAction {
				t.Fatalf("payload %v was accepted: %v", bad, f.msg["payload"])
			}
		}
		f := bySeat[chooser].command(matchID, rev(states[0]), "teleport", map[string]any{})
		if f.msg["payload"].(map[string]any)["code"] != trump.CodeInvalidAction {
			t.Fatalf("unknown kind accepted: %v", f.msg["payload"])
		}
	})

	t.Run("a stale revision is refused", func(t *testing.T) {
		chooser := trump.SeatsOfTeam(entitled)[0]
		f := bySeat[chooser].command(matchID, rev(states[0])-1, "choose_trump", map[string]any{"suit": "hearts"})
		if f.msg["payload"].(map[string]any)["code"] != "STALE_REVISION" {
			t.Fatalf("stale command: %v", f.msg["payload"])
		}
	})

	// Either teammate may take the decision; one delegates, the partner chooses.
	first := trump.SeatsOfTeam(entitled)[0]
	partner := trump.PartnerOf(first)
	if f := bySeat[first].command(matchID, rev(states[0]), "delegate_trump", map[string]any{}); f.msg["type"] != "ack" {
		t.Fatalf("delegate: %v", f.msg)
	}
	delegated, _ := bySeat[partner].state("delegated", func(p map[string]any) bool {
		return publicOf(map[string]any{"view": map[string]any{"public": p["view"].(map[string]any)["public"]}})["stage"] == string(trump.StageDelegatedDecision)
	})
	if f := bySeat[first].command(matchID, rev(delegated), "choose_trump", map[string]any{"suit": "clubs"}); f.msg["payload"].(map[string]any)["code"] != trump.CodeNotYourTurn {
		t.Fatalf("the delegating player must not choose: %v", f.msg["payload"])
	}
	if f := bySeat[partner].command(matchID, rev(delegated), "choose_trump", map[string]any{"suit": "clubs"}); f.msg["type"] != "ack" {
		t.Fatalf("delegate choice: %v", f.msg)
	}

	playing, _ := bySeat[partner].state("play", func(p map[string]any) bool {
		return p["view"].(map[string]any)["public"].(map[string]any)["stage"] == string(trump.StageActiveTrick)
	})
	if publicOf(playing)["trump"] != "clubs" || int(publicOf(playing)["turn"].(float64)) != partner {
		t.Fatalf("after selection: %v", publicOf(playing))
	}
	if len(cardsOf(selfOf(playing)["hand"])) != trump.CardsPerPlayer {
		t.Fatal("the second deal did not complete the hand")
	}

	t.Run("chat reopens once play starts", func(t *testing.T) {
		if s, body := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/chat", tokenBySeat[0], map[string]any{"client_id": uuid(), "body": "good luck"}); s != 200 {
			t.Fatalf("chat after selection: %d %v", s, body)
		}
	})

	t.Run("out-of-turn play is refused", func(t *testing.T) {
		wrong := trump.PartnerOf(partner)
		other, _ := bySeat[wrong].state("own view", func(p map[string]any) bool {
			return p["view"].(map[string]any)["public"].(map[string]any)["stage"] == string(trump.StageActiveTrick)
		})
		card := cardsOf(selfOf(other)["hand"])[0]
		f := bySeat[wrong].command(matchID, rev(other), "play_card", map[string]any{"card": card})
		if f.msg["payload"].(map[string]any)["code"] != trump.CodeNotYourTurn {
			t.Fatalf("out-of-turn play: %v", f.msg["payload"])
		}
	})

	t.Run("a card the player does not hold is refused", func(t *testing.T) {
		mine := cardsOf(selfOf(playing)["hand"])
		var absent string
		for _, card := range trump.Manifest() {
			if !slicesContains(mine, string(card.ID)) {
				absent = string(card.ID)
				break
			}
		}
		f := bySeat[partner].command(matchID, rev(playing), "play_card", map[string]any{"card": absent})
		if f.msg["payload"].(map[string]any)["code"] != trump.CodeInvalidCard {
			t.Fatalf("foreign card: %v", f.msg["payload"])
		}
	})

	// One accepted play, then the same command ID again: the recorded answer
	// comes back and nothing is applied twice.
	legal := cardsOf(selfOf(playing)["legal"])
	if len(legal) == 0 {
		t.Fatal("the player to act has no legal card")
	}
	commandID := uuid()
	payload, _ := json.Marshal(map[string]any{"card": legal[0]})
	sendPlay := func(client *wsClient) map[string]any {
		id := client.send(map[string]any{"type": "game.command", "match_id": matchID, "payload": map[string]any{
			"command_id": commandID, "expected_revision": rev(playing), "kind": "play_card", "payload": json.RawMessage(payload),
		}})
		return client.reply(id).msg
	}
	first1 := sendPlay(bySeat[partner])
	if first1["type"] != "ack" {
		t.Fatalf("play rejected: %v", first1)
	}
	afterOne, _ := bySeat[partner].state("one card played", func(p map[string]any) bool {
		return len(p["view"].(map[string]any)["public"].(map[string]any)["trick"].([]any)) == 1
	})
	repeat := sendPlay(bySeat[partner])
	if repeat["type"] != "ack" {
		t.Fatalf("a repeated command ID must return its recorded answer: %v", repeat)
	}
	if rev(afterOne) != repeat["payload"].(map[string]any)["revision"].(float64) {
		t.Fatal("a duplicate command must not advance the revision")
	}
	if len(publicOf(afterOne)["trick"].([]any)) != 1 {
		t.Fatal("a duplicate command must not play a second card")
	}

	t.Run("seats and teams are as the rules say", func(t *testing.T) {
		// Seats 0 and 2 are one team, 1 and 3 the other, so partners sit
		// opposite. Every seat is filled by a distinct player.
		seen := map[int]string{}
		for _, p := range publicOf(states[0])["players"].([]any) {
			seat := int(p.(map[string]any)["seat"].(float64))
			team := int(p.(map[string]any)["team"].(float64))
			if team != seat%2 {
				t.Fatalf("seat %d is on team %d", seat, team)
			}
			seen[seat] = p.(map[string]any)["user_id"].(string)
		}
		if len(seen) != 4 {
			t.Fatalf("%d seats", len(seen))
		}
		for _, u := range users {
			if seatOf[u] < 0 {
				t.Fatalf("%s has no seat", u)
			}
		}
	})

	t.Run("an outsider can neither watch nor act", func(t *testing.T) {
		_, outsiderToken := e.user("spy")
		if status, _ := e.do(a, "GET", "/api/v1/matches/"+matchID, outsiderToken, nil); status != 404 {
			t.Fatalf("an outsider read the match: %d", status)
		}
		if status, _ := e.do(a, "GET", "/api/v1/rooms/"+roomID, outsiderToken, nil); status != 404 {
			t.Fatalf("an outsider read the room: %d", status)
		}
		if status, _ := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/chat", outsiderToken, map[string]any{"client_id": uuid(), "body": "hello"}); status != 404 {
			t.Fatalf("an outsider posted chat: %d", status)
		}
		// Over the socket: subscribing and commanding are both refused.
		spy := e.dial(a, outsiderToken)
		id := spy.send(map[string]any{"type": "match.subscribe", "match_id": matchID})
		if reply := spy.reply(id); reply.msg["type"] != "error" {
			t.Fatalf("an outsider subscribed: %v", reply.msg)
		}
		f := spy.command(matchID, rev(states[0]), "play_card", map[string]any{"card": "spades-a"})
		if f.msg["type"] != "error" {
			t.Fatalf("an outsider commanded: %v", f.msg)
		}
	})

	t.Run("a refresh re-reads the committed state and redeals nothing", func(t *testing.T) {
		fresh := e.dial(a, tokenBySeat[partner])
		fresh.send(map[string]any{"type": "match.subscribe", "match_id": matchID})
		resync, _ := fresh.state("resync", func(p map[string]any) bool { return p["status"] == "playing" })
		if rev(resync) != rev(afterOne) {
			t.Fatalf("revision %v after resubscribe, want %v", rev(resync), rev(afterOne))
		}
		pubR := publicOf(resync)
		if pubR["trump"] != "clubs" || int(pubR["entitled_team"].(float64)) != toss {
			t.Fatal("a reconnect must not redeal or re-toss")
		}
		if len(cardsOf(selfOf(resync)["hand"])) != trump.CardsPerPlayer-1 {
			t.Fatal("the hand must come back as it was")
		}
		if len(pubR["trick"].([]any)) != 1 {
			t.Fatal("the trick in progress must survive a reconnect")
		}
		// The older socket for that seat was replaced, not duplicated.
		if code := bySeat[partner].closedWith(); code != 4009 {
			t.Fatalf("previous socket closed with %d, want 4009", code)
		}
		bySeat[partner] = fresh
	})

	t.Run("concurrent plays of the same card apply exactly once", func(t *testing.T) {
		current, _ := bySeat[partner].state("current", func(p map[string]any) bool { return p["status"] == "playing" })
		turnSeat := int(publicOf(current)["turn"].(float64))
		actor := users[0]
		for _, u := range users {
			if seatOf[u] == turnSeat {
				actor = u
			}
		}
		st, err := a.app.Hub.Matches.StateFor(ctx, matchID, actor, 0)
		if err != nil {
			t.Fatal(err)
		}
		var self trump.SelfView
		if err := json.Unmarshal(st.View.Self, &self); err != nil {
			t.Fatal(err)
		}
		if len(self.Legal) == 0 {
			t.Fatal("the player to act has no legal card")
		}
		me, err := e.q.MatchParticipant(ctx, store.MatchParticipantParams{MatchID: matchID, UserID: actor})
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(map[string]any{"card": self.Legal[0]})
		var wg sync.WaitGroup
		results := make(chan error, 8)
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Distinct command IDs at the same revision: this is a race,
				// not a retry, so exactly one may win.
				_, err := a.app.Hub.Matches.Execute(ctx, matchID, actor, me.ControllerGeneration, game.Command{
					ID: uuid(), ExpectedRevision: st.Revision, Kind: "play_card", Payload: payload,
				})
				results <- err
			}()
		}
		wg.Wait()
		close(results)
		applied := 0
		for err := range results {
			if err == nil {
				applied++
			}
		}
		if applied != 1 {
			t.Fatalf("%d concurrent plays applied, want exactly 1", applied)
		}
		mt, err := e.q.Match(ctx, matchID)
		if err != nil {
			t.Fatal(err)
		}
		if mt.Revision != st.Revision+1 {
			t.Fatalf("revision %d after the race, want %d", mt.Revision, st.Revision+1)
		}
		// The card was played once: it is in the trick and out of the hand.
		after, err := a.app.Hub.Matches.StateFor(ctx, matchID, actor, 0)
		if err != nil {
			t.Fatal(err)
		}
		var selfAfter trump.SelfView
		var pubAfter trump.PublicView
		_ = json.Unmarshal(after.View.Self, &selfAfter)
		_ = json.Unmarshal(after.View.Public, &pubAfter)
		for _, id := range selfAfter.Hand {
			if string(id) == string(self.Legal[0]) {
				t.Fatal("the played card is still in hand")
			}
		}
		played := 0
		for _, p := range pubAfter.Trick {
			if string(p.Card) == string(self.Legal[0]) {
				played++
			}
		}
		if played != 1 {
			t.Fatalf("the card appears %d times in the trick", played)
		}
	})

	t.Run("no projection carries another hand, the undealt rest or the seed", func(t *testing.T) {
		// The server's own snapshot says who holds what; every player's
		// projection is checked against it.
		row, err := e.q.RoomGameState(ctx, roomID)
		if err != nil {
			t.Fatal(err)
		}
		var full trump.State
		if err := json.Unmarshal(row.State, &full); err != nil {
			t.Fatal(err)
		}
		if len(full.Seed) == 0 {
			t.Fatal("the stored state should carry a seed")
		}
		seed := seedHex(full.Seed)
		for i, u := range users {
			seat := seatOf[u]
			status, body := e.do(a, "GET", "/api/v1/matches/"+matchID, tokens[i], nil)
			if status != 200 {
				t.Fatalf("player %d cannot read the match: %d", i, status)
			}
			blob, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			text := string(blob)
			for other := range full.Players {
				if other == seat {
					continue
				}
				for _, id := range full.Players[other].Hand {
					if strings.Contains(text, string(id)) {
						t.Fatalf("seat %d can see seat %d's %s", seat, other, id)
					}
				}
			}
			for _, id := range full.Rest {
				if strings.Contains(text, string(id)) {
					t.Fatalf("seat %d can see the undealt %s", seat, id)
				}
			}
			if strings.Contains(text, seed) || strings.Contains(text, "\"seed\"") {
				t.Fatalf("seat %d received the shuffle seed", seat)
			}
			// Its own hand must be there, so the check is not vacuous.
			own := full.Players[seat].Hand
			if len(own) == 0 || !strings.Contains(text, string(own[0])) {
				t.Fatalf("seat %d cannot see its own cards", seat)
			}
		}
	})
	t.Run("a restart recovers the match", func(t *testing.T) {
		a.stop()
		b := startInstance(t, pool, c)
		client := e.dial(b, tokenBySeat[partner])
		client.send(map[string]any{"type": "match.subscribe", "match_id": matchID})
		after, _ := client.state("after restart", func(p map[string]any) bool { return p["view"] != nil })
		committed, err := e.q.Match(ctx, matchID)
		if err != nil {
			t.Fatal(err)
		}
		if int64(rev(after)) != committed.Revision {
			t.Fatalf("revision %v after restart, want the committed %d", rev(after), committed.Revision)
		}
		if publicOf(after)["trump"] != "clubs" || int(publicOf(after)["entitled_team"].(float64)) != toss {
			t.Fatal("the restart must not re-toss or change the trump")
		}
		if len(publicOf(after)["trick"].([]any)) == 0 && publicOf(after)["last_trick"] == nil {
			t.Fatal("the trick in progress must survive a restart")
		}
	})
}

func slicesContains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func seedHex(seed []byte) string {
	b, _ := json.Marshal(seed)
	return strings.Trim(string(b), `"`)
}
