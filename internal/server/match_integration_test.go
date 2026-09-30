package server_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"cardplay/db"
	"cardplay/internal/accounts"
	"cardplay/internal/config"
	"cardplay/internal/game"
	"cardplay/internal/game/monopoly"
	"cardplay/internal/httpx"
	"cardplay/internal/server"
	"cardplay/internal/store"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ---- harness ---------------------------------------------------------------

type instance struct {
	app    *server.App
	srv    *httptest.Server
	cancel context.CancelFunc
}

func startInstance(t *testing.T, pool *pgxpool.Pool, c config.Config) *instance {
	t.Helper()
	app := server.New(pool, c)
	ctx, cancel := context.WithCancel(context.Background())
	go app.Hub.Run(ctx)
	inst := &instance{app: app, srv: httptest.NewServer(app.Handler), cancel: cancel}
	t.Cleanup(inst.stop)
	return inst
}

func (i *instance) stop() {
	i.cancel()
	i.srv.CloseClientConnections()
	i.srv.Close()
}

type env struct {
	t    *testing.T
	pool *pgxpool.Pool
	c    config.Config
	q    *store.Queries
}

func (e *env) user(name string) (string, string) {
	e.t.Helper()
	suffix := httpx.Token()[:10]
	u, err := e.q.CreateUser(context.Background(), store.CreateUserParams{Email: name + suffix + "@test.invalid", Handle: name + suffix, DisplayName: name, PasswordHash: accounts.PasswordHash("test-password-123"), EmailVerified: true})
	if err != nil {
		e.t.Fatal(err)
	}
	raw := httpx.Token()
	if err = e.q.CreateSession(context.Background(), store.CreateSessionParams{TokenHash: httpx.Hash(raw), UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		e.t.Fatal(err)
	}
	return u.ID, raw
}

func (e *env) do(inst *instance, method, path, token string, body any) (int, map[string]any) {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, inst.srv.URL+path, reader)
	if method != "GET" {
		req.Header.Set("Origin", e.c.Origin)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: accounts.CookieName, Value: token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

type frame struct {
	raw []byte
	msg map[string]any
}

type wsClient struct {
	t      *testing.T
	conn   *websocket.Conn
	frames chan frame
	closed chan websocket.StatusCode
}

func (e *env) dial(inst *instance, token string) *wsClient {
	e.t.Helper()
	h := http.Header{}
	h.Set("Origin", e.c.Origin)
	h.Set("Cookie", accounts.CookieName+"="+token)
	conn, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(inst.srv.URL, "http")+"/ws", &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		e.t.Fatal(err)
	}
	conn.SetReadLimit(1 << 20)
	c := &wsClient{t: e.t, conn: conn, frames: make(chan frame, 256), closed: make(chan websocket.StatusCode, 1)}
	go func() {
		for {
			_, raw, err := conn.Read(context.Background())
			if err != nil {
				c.closed <- websocket.CloseStatus(err)
				close(c.frames)
				return
			}
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			c.frames <- frame{raw, m}
		}
	}()
	e.t.Cleanup(func() { _ = conn.CloseNow() })
	return c
}

func uuid() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func (c *wsClient) send(fields map[string]any) string {
	c.t.Helper()
	id := uuid()
	fields["v"], fields["id"] = 1, id
	b, _ := json.Marshal(fields)
	if err := c.conn.Write(context.Background(), websocket.MessageText, b); err != nil {
		c.t.Fatal(err)
	}
	return id
}

// await returns the first frame matching pred, skipping others.
func (c *wsClient) await(what string, pred func(frame) bool) frame {
	c.t.Helper()
	deadline := time.After(8 * time.Second)
	var seen []string
	for {
		select {
		case f, ok := <-c.frames:
			if !ok {
				c.t.Fatalf("socket closed while waiting for %s; saw %v", what, seen)
			}
			if pred(f) {
				return f
			}
			summary := fmt.Sprint(f.msg["type"])
			if p, ok := f.msg["payload"].(map[string]any); ok {
				summary += fmt.Sprintf(" status=%v rev=%v code=%v", p["status"], p["revision"], p["code"])
			}
			seen = append(seen, summary)
		case <-deadline:
			c.t.Fatalf("timed out waiting for %s; saw %v", what, seen)
		}
	}
}

func (c *wsClient) reply(id string) frame {
	return c.await("reply "+id, func(f frame) bool { return f.msg["id"] == id })
}

// state waits for a match.state satisfying pred.
func (c *wsClient) state(what string, pred func(map[string]any) bool) (map[string]any, []byte) {
	f := c.await(what, func(f frame) bool {
		if f.msg["type"] != "match.state" {
			return false
		}
		return pred(f.msg["payload"].(map[string]any))
	})
	return f.msg["payload"].(map[string]any), f.raw
}

func (c *wsClient) command(match string, revision float64, kind string, payload any) frame {
	c.t.Helper()
	p, _ := json.Marshal(payload)
	id := c.send(map[string]any{"type": "game.command", "match_id": match, "payload": map[string]any{
		"command_id": uuid(), "expected_revision": revision, "kind": kind, "payload": json.RawMessage(p),
	}})
	return c.reply(id)
}

func (c *wsClient) closedWith() websocket.StatusCode {
	select {
	case code := <-c.closed:
		return code
	case <-time.After(8 * time.Second):
		c.t.Fatal("socket was not closed")
	}
	return 0
}

// ---- match state helpers ---------------------------------------------------

func hand(st map[string]any) []string {
	var out []string
	for _, id := range st["view"].(map[string]any)["self"].(map[string]any)["hand"].([]any) {
		out = append(out, id.(string))
	}
	return out
}

func activeUser(st map[string]any) string {
	seat := st["view"].(map[string]any)["public"].(map[string]any)["active"].(float64)
	for _, p := range st["participants"].([]any) {
		if p.(map[string]any)["seat"].(float64) == seat {
			return p.(map[string]any)["user_id"].(string)
		}
	}
	return ""
}

func endTurnReturn(st map[string]any) map[string]any {
	h := hand(st)
	if len(h) <= monopoly.HandLimit {
		return map[string]any{}
	}
	return map[string]any{"return": h[:len(h)-monopoly.HandLimit]}
}

func errCode(f frame) string {
	if f.msg["type"] != "error" {
		return ""
	}
	return f.msg["payload"].(map[string]any)["code"].(string)
}

// ---- the test --------------------------------------------------------------

// TestLiveMatch drives real WebSocket clients against PostgreSQL through the
// whole match lifecycle, including a server restart.
func TestLiveMatch(t *testing.T) {
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
	alice, aliceToken := e.user("alice")
	bob, bobToken := e.user("bob")
	_, eveToken := e.user("eve")
	tokens := map[string]string{alice: aliceToken, bob: bobToken}
	a := startInstance(t, pool, c)

	// Room, invite, readiness and start rules.
	_, room := e.do(a, "POST", "/api/v1/rooms", aliceToken, map[string]any{"name": "table", "capacity": 2})
	roomID := room["id"].(string)
	_, inv := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/invitations", aliceToken, map[string]any{})
	_, spare := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/invitations", aliceToken, map[string]any{})
	if s, _ := e.do(a, "POST", "/api/v1/rooms/join", bobToken, map[string]any{"token": inv["token"]}); s != 200 {
		t.Fatalf("join=%d", s)
	}
	if s, _ := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/matches", bobToken, nil); s != 403 {
		t.Fatalf("non-host start=%d", s)
	}
	if s, _ := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/matches", eveToken, nil); s != 404 {
		t.Fatalf("outsider start=%d", s)
	}
	if s, body := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/matches", aliceToken, nil); s != 409 || body["error"].(map[string]any)["code"] != "NOT_READY" {
		t.Fatalf("start before ready=%d %v", s, body)
	}
	ready := func() {
		for _, tok := range []string{aliceToken, bobToken} {
			if s, _ := e.do(a, "PUT", "/api/v1/rooms/"+roomID+"/ready", tok, map[string]any{"ready": true}); s != 204 {
				t.Fatalf("ready=%d", s)
			}
		}
	}
	start := func() string {
		ready()
		s, body := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/matches", aliceToken, nil)
		if s != 201 {
			t.Fatalf("start=%d %v", s, body)
		}
		return body["match_id"].(string)
	}
	match := start()
	if s, _ := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/matches", aliceToken, nil); s != 409 {
		t.Fatalf("second start=%d", s)
	}
	_, view := e.do(a, "GET", "/api/v1/rooms/"+roomID, aliceToken, nil)
	if view["room"].(map[string]any)["status"] != "playing" || view["match"].(map[string]any)["id"] != match {
		t.Fatalf("room view during match: %v", view)
	}
	if s, _ := e.do(a, "POST", "/api/v1/rooms/join", eveToken, map[string]any{"token": spare["token"]}); s != 404 {
		t.Fatalf("invitations must end when the match starts: %d", s)
	}
	if s, _ := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/leave", bobToken, nil); s != 409 {
		t.Fatalf("lobby leave during a match=%d", s)
	}

	t.Run("unauthorized subscriptions", func(t *testing.T) {
		ws := e.dial(a, eveToken)
		if code := errCode(ws.reply(ws.send(map[string]any{"type": "match.subscribe", "match_id": match}))); code != "NOT_FOUND" {
			t.Fatalf("outsider subscribe=%s", code)
		}
		if s, _ := e.do(a, "GET", "/api/v1/matches/"+match, eveToken, nil); s != 404 {
			t.Fatalf("outsider view=%d", s)
		}
		if code := errCode(ws.reply(ws.send(map[string]any{"type": "game.command", "match_id": match, "payload": map[string]any{}}))); code != "INVALID_REQUEST" {
			t.Fatalf("unsubscribed command=%s", code)
		}
	})

	// Both players subscribe; the match pauses until both are present.
	aws, bws := e.dial(a, aliceToken), e.dial(a, bobToken)
	_ = aws.send(map[string]any{"type": "match.subscribe", "match_id": match})
	if first, _ := aws.state("alice subscribed", func(map[string]any) bool { return true }); first["status"] != "paused" {
		t.Fatalf("a match waits for every seat: %v", first["status"])
	}
	_ = bws.send(map[string]any{"type": "match.subscribe", "match_id": match})
	stB, rawB := bws.state("bob playing", func(s map[string]any) bool { return s["status"] == "playing" })
	stA, rawA := aws.state("alice sees playing", func(s map[string]any) bool { return s["status"] == "playing" })

	t.Run("private hands", func(t *testing.T) {
		for _, id := range hand(stA) {
			if bytes.Contains(rawB, []byte(`"`+id+`"`)) {
				t.Fatalf("bob's frame leaks alice's card %s", id)
			}
		}
		for _, id := range hand(stB) {
			if bytes.Contains(rawA, []byte(`"`+id+`"`)) {
				t.Fatalf("alice's frame leaks bob's card %s", id)
			}
		}
		if bytes.Contains(rawA, []byte(`"seed"`)) || bytes.Contains(rawA, []byte(`"draw"`)) {
			t.Fatal("hidden state in a projection")
		}
	})

	sockets := map[string]*wsClient{alice: aws, bob: bws}
	states := map[string]map[string]any{alice: stA, bob: stB}
	active := activeUser(stA)
	other := alice
	if active == alice {
		other = bob
	}

	t.Run("commands, stale revisions, duplicates, rejections", func(t *testing.T) {
		st := states[active]
		payload, _ := json.Marshal(endTurnReturn(st))
		cmdID := uuid()
		send := func(ws *wsClient, id string, rev float64, kind string, p json.RawMessage) frame {
			return ws.reply(ws.send(map[string]any{"type": "game.command", "match_id": match, "payload": map[string]any{
				"command_id": id, "expected_revision": rev, "kind": kind, "payload": p,
			}}))
		}
		ack := send(sockets[active], cmdID, 0, "end_turn", payload)
		if ack.msg["type"] != "ack" || ack.msg["payload"].(map[string]any)["revision"] != 1.0 {
			t.Fatalf("end_turn ack %s", ack.raw)
		}
		if got, _ := sockets[other].state("other sees revision 1", func(s map[string]any) bool { return s["revision"] == 1.0 }); activeUser(got) != other {
			t.Fatal("turn did not pass")
		}
		dup := send(sockets[active], cmdID, 0, "end_turn", payload)
		if dup.msg["type"] != "ack" || dup.msg["payload"].(map[string]any)["duplicate"] != true {
			t.Fatalf("duplicate %s", dup.raw)
		}
		if code := errCode(send(sockets[active], cmdID, 0, "end_turn", json.RawMessage(`{"return":[]}`))); code != "IDEMPOTENCY_CONFLICT" {
			t.Fatalf("reused id=%s", code)
		}
		stale := send(sockets[active], uuid(), 0, "end_turn", payload)
		if errCode(stale) != "STALE_REVISION" || stale.msg["payload"].(map[string]any)["revision"] != 1.0 {
			t.Fatalf("stale %s", stale.raw)
		}
		rejectID := uuid()
		if code := errCode(send(sockets[active], rejectID, 1, "end_turn", json.RawMessage(`{}`))); code != monopoly.CodeNotYourTurn {
			t.Fatalf("out of turn=%s", code)
		}
		if code := errCode(send(sockets[active], rejectID, 1, "end_turn", json.RawMessage(`{}`))); code != monopoly.CodeNotYourTurn {
			t.Fatalf("retried rejection=%s", code)
		}
		active, other = other, active
	})

	t.Run("concurrent commands apply once", func(t *testing.T) {
		st, err := a.app.Hub.Matches.StateFor(ctx, match, active, 0)
		if err != nil {
			t.Fatal(err)
		}
		me, err := e.q.MatchParticipant(ctx, store.MatchParticipantParams{MatchID: match, UserID: active})
		if err != nil {
			t.Fatal(err)
		}
		var self monopoly.SelfView
		_ = json.Unmarshal(st.View.Self, &self)
		ret := map[string]any{}
		if len(self.Hand) > monopoly.HandLimit {
			ret["return"] = self.Hand[:len(self.Hand)-monopoly.HandLimit]
		}
		p, _ := json.Marshal(ret)
		var wg sync.WaitGroup
		results := make(chan error, 8)
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := a.app.Hub.Matches.Execute(ctx, match, active, me.ControllerGeneration, game.Command{ID: uuid(), ExpectedRevision: st.Revision, Kind: "end_turn", Payload: p})
				results <- err
			}()
		}
		wg.Wait()
		close(results)
		applied := 0
		for err := range results {
			if err == nil {
				applied++
			} else if !strings.Contains(err.Error(), "STALE_REVISION") {
				t.Fatalf("unexpected error %v", err)
			}
		}
		if applied != 1 {
			t.Fatalf("%d concurrent commands applied, want exactly 1", applied)
		}
		mt, _ := e.q.Match(ctx, match)
		if mt.Revision != st.Revision+1 {
			t.Fatalf("revision %d after race from %d", mt.Revision, st.Revision)
		}
		active, other = other, active
	})

	t.Run("second tab takes control", func(t *testing.T) {
		tab2 := e.dial(a, tokens[active])
		_ = tab2.send(map[string]any{"type": "match.subscribe", "match_id": match})
		st, _ := tab2.state("tab2 state", func(map[string]any) bool { return true })
		if code := sockets[active].closedWith(); code != 4009 {
			t.Fatalf("old tab closed with %d, want 4009", code)
		}
		ack := tab2.command(match, st["revision"].(float64), "end_turn", endTurnReturn(st))
		if ack.msg["type"] != "ack" {
			t.Fatalf("new controller command %s", ack.raw)
		}
		sockets[active] = tab2
		active, other = other, active
	})

	t.Run("disconnect pauses and reconnect resumes", func(t *testing.T) {
		_ = sockets[other].conn.Close(websocket.StatusNormalClosure, "")
		st, _ := sockets[active].state("paused", func(s map[string]any) bool { return s["status"] == "paused" })
		if code := errCode(sockets[active].command(match, st["revision"].(float64), "end_turn", endTurnReturn(st))); code != "MATCH_PAUSED" {
			t.Fatalf("command while paused=%s", code)
		}
		back := e.dial(a, tokens[other])
		_ = back.send(map[string]any{"type": "match.subscribe", "match_id": match})
		back.state("resumed", func(s map[string]any) bool { return s["status"] == "playing" })
		sockets[other] = back
	})

	t.Run("silent drop is detected by missed heartbeats", func(t *testing.T) {
		// A socket that vanishes without closing stops heartbeating; the sweep
		// then marks the seat absent and pauses the match.
		if _, err := pool.Exec(ctx, "UPDATE match_participants SET last_seen_at=now()-interval '2 minutes' WHERE match_id=$1 AND user_id=$2", match, other); err != nil {
			t.Fatal(err)
		}
		if err := a.app.Hub.Matches.Sweep(ctx); err != nil {
			t.Fatal(err)
		}
		sockets[active].state("paused by sweep", func(s map[string]any) bool { return s["status"] == "paused" })
		// If that controller was only slow, its next heartbeat resumes play.
		me, err := e.q.MatchParticipant(ctx, store.MatchParticipantParams{MatchID: match, UserID: other})
		if err != nil {
			t.Fatal(err)
		}
		if err := a.app.Hub.Matches.Heartbeat(ctx, match, other, me.ControllerGeneration); err != nil {
			t.Fatal(err)
		}
		sockets[active].state("resumed by heartbeat", func(s map[string]any) bool { return s["status"] == "playing" })
		if err := a.app.Hub.Matches.Heartbeat(ctx, match, other, me.ControllerGeneration-1); err == nil {
			t.Fatal("a replaced controller's heartbeat must fail")
		}
	})

	t.Run("server restart keeps every acknowledged command", func(t *testing.T) {
		before, err := a.app.Hub.Matches.StateFor(ctx, match, active, 0)
		if err != nil {
			t.Fatal(err)
		}
		a.stop()
		b := startInstance(e.t, pool, c) // outlives this subtest
		for _, u := range []string{alice, bob} {
			ws := e.dial(b, tokens[u])
			_ = ws.send(map[string]any{"type": "match.subscribe", "match_id": match})
			sockets[u] = ws
		}
		st, _ := sockets[active].state("restored and playing", func(s map[string]any) bool { return s["status"] == "playing" })
		var self monopoly.SelfView
		_ = json.Unmarshal(before.View.Self, &self)
		var want []string
		for _, id := range self.Hand {
			want = append(want, string(id))
		}
		if st["revision"].(float64) != float64(before.Revision) || !slices.Equal(hand(st), want) {
			t.Fatal("state changed across restart")
		}
		ack := sockets[active].command(match, st["revision"].(float64), "end_turn", endTurnReturn(st))
		if ack.msg["type"] != "ack" {
			t.Fatalf("command after restart %s", ack.raw)
		}
		a = b
		active, other = other, active
	})

	t.Run("abandon vote after the grace period", func(t *testing.T) {
		_ = sockets[other].conn.Close(websocket.StatusNormalClosure, "")
		sockets[active].state("paused", func(s map[string]any) bool { return s["status"] == "paused" })
		if s, body := e.do(a, "POST", "/api/v1/matches/"+match+"/abandon", tokens[active], map[string]any{"vote": true}); s != 409 || body["error"].(map[string]any)["code"] != "TOO_EARLY" {
			t.Fatalf("early vote=%d %v", s, body)
		}
		if _, err := pool.Exec(ctx, "UPDATE match_participants SET disconnected_at=now()-interval '6 minutes' WHERE match_id=$1 AND user_id=$2", match, other); err != nil {
			t.Fatal(err)
		}
		if s, _ := e.do(a, "POST", "/api/v1/matches/"+match+"/abandon", tokens[other], map[string]any{"vote": true}); s != 409 {
			t.Fatalf("absent player voted: %d", s)
		}
		if s, _ := e.do(a, "POST", "/api/v1/matches/"+match+"/abandon", tokens[active], map[string]any{"vote": true}); s != 204 {
			t.Fatalf("vote=%d", s)
		}
		st, _ := sockets[active].state("abandoned", func(s map[string]any) bool { return s["status"] == "abandoned" })
		if st["end_reason"] != "voted" || st["winner_id"] != nil {
			t.Fatalf("abandoned match %v", st["end_reason"])
		}
		_, view := e.do(a, "GET", "/api/v1/rooms/"+roomID, aliceToken, nil)
		if view["room"].(map[string]any)["status"] != "waiting" || view["members"].([]any)[0].(map[string]any)["ready"] != false {
			t.Fatal("room should return to the lobby with readiness reset")
		}
	})

	t.Run("explicit leave abandons", func(t *testing.T) {
		m2 := start()
		if s, _ := e.do(a, "POST", "/api/v1/matches/"+m2+"/leave", bobToken, nil); s != 204 {
			t.Fatalf("leave=%d", s)
		}
		_, st := e.do(a, "GET", "/api/v1/matches/"+m2, aliceToken, nil)
		if st["status"] != "abandoned" || st["end_reason"] != "left" || st["ended_by"] != bob || st["winner_id"] != nil {
			t.Fatalf("left match %v", st)
		}
		if s, _ := e.do(a, "POST", "/api/v1/matches/"+m2+"/leave", aliceToken, nil); s != 409 {
			t.Fatalf("leaving an ended match=%d", s)
		}
	})

	t.Run("a winning command finishes the match", func(t *testing.T) {
		m3 := start()
		ws := map[string]*wsClient{}
		for _, u := range []string{alice, bob} {
			ws[u] = e.dial(a, tokens[u])
			_ = ws[u].send(map[string]any{"type": "match.subscribe", "match_id": m3})
		}
		st, _ := ws[alice].state("playing", func(s map[string]any) bool { return s["status"] == "playing" })
		winner := activeUser(st)
		// Stage a near-win position in the stored snapshot.
		snap, err := e.q.LatestSnapshot(ctx, m3)
		if err != nil {
			t.Fatal(err)
		}
		var s monopoly.State
		if err := json.Unmarshal(snap.State, &s); err != nil {
			t.Fatal(err)
		}
		stage(t, &s)
		data, _ := json.Marshal(&s)
		if _, err := pool.Exec(ctx, "UPDATE game_snapshots SET state=$3 WHERE match_id=$1 AND revision=$2", m3, snap.Revision, data); err != nil {
			t.Fatal(err)
		}
		ack := ws[winner].command(m3, float64(snap.Revision), "play_property", map[string]any{"card": "property-baltic-avenue", "set": "staged-brown"})
		if ack.msg["type"] != "ack" {
			t.Fatalf("winning play %s", ack.raw)
		}
		loser := alice
		if winner == alice {
			loser = bob
		}
		fin, _ := ws[loser].state("finished", func(s map[string]any) bool { return s["status"] == "finished" })
		if fin["winner_id"] != winner || fin["end_reason"] != "won" {
			t.Fatalf("finished %v %v", fin["winner_id"], fin["end_reason"])
		}
		if code := errCode(ws[winner].command(m3, fin["revision"].(float64), "end_turn", map[string]any{})); code != "MATCH_OVER" {
			t.Fatalf("command after the end=%s", code)
		}
		events := fin["events"].([]any)
		if events[len(events)-1].(map[string]any)["kind"] != "game_won" {
			t.Fatal("winning event missing from the history")
		}
	})

	t.Run("an idle player's turn times out", func(t *testing.T) {
		m := start()
		ws := map[string]*wsClient{}
		for _, u := range []string{alice, bob} {
			ws[u] = e.dial(a, tokens[u])
			_ = ws[u].send(map[string]any{"type": "match.subscribe", "match_id": m})
		}
		st, _ := ws[alice].state("playing", func(s map[string]any) bool { return s["status"] == "playing" })
		if left, ok := st["turn_seconds_left"].(float64); !ok || left < 100 || left > 120 {
			t.Fatalf("turn clock %v", st["turn_seconds_left"])
		}
		idle := activeUser(st)
		watcher := alice
		if idle == alice {
			watcher = bob
		}
		sweep := func() {
			t.Helper()
			if err := a.app.Hub.Matches.Sweep(ctx); err != nil {
				t.Fatal(err)
			}
		}
		// Not yet due: nothing happens.
		sweep()
		if mt, _ := e.q.Match(ctx, m); mt.Revision != 0 {
			t.Fatalf("moved before the timeout: revision %d", mt.Revision)
		}
		// A paused match never times out, even when the clock has run out.
		_ = ws[watcher].conn.Close(websocket.StatusNormalClosure, "")
		ws[idle].state("paused", func(s map[string]any) bool { return s["status"] == "paused" })
		if _, err := pool.Exec(ctx, "UPDATE matches SET awaiting_since=now()-interval '10 minutes' WHERE id=$1", m); err != nil {
			t.Fatal(err)
		}
		sweep()
		if mt, _ := e.q.Match(ctx, m); mt.Revision != 0 {
			t.Fatal("a paused match timed out")
		}
		// Resuming restarts the clock.
		ws[watcher] = e.dial(a, tokens[watcher])
		_ = ws[watcher].send(map[string]any{"type": "match.subscribe", "match_id": m})
		ws[watcher].state("resumed", func(s map[string]any) bool { return s["status"] == "playing" })
		sweep()
		if mt, _ := e.q.Match(ctx, m); mt.Revision != 0 {
			t.Fatal("resuming did not restart the turn clock")
		}
		// Out of time: the server ends the idle player's turn for them.
		if _, err := pool.Exec(ctx, "UPDATE matches SET awaiting_since=now()-interval '3 minutes' WHERE id=$1", m); err != nil {
			t.Fatal(err)
		}
		sweep()
		after, _ := ws[watcher].state("timed out", func(s map[string]any) bool { return s["revision"].(float64) >= 1 })
		if activeUser(after) != watcher || after["revision"].(float64) != 1 {
			t.Fatalf("turn did not pass: active %s revision %v", activeUser(after), after["revision"])
		}
		var timedOut bool
		for _, ev := range after["events"].([]any) {
			ev := ev.(map[string]any)
			if ev["kind"] == "timed_out" && ev["payload"].(map[string]any)["user_id"] == idle {
				timedOut = true
			}
		}
		if !timedOut {
			t.Fatal("no timed_out event in the history")
		}
		if left := after["turn_seconds_left"].(float64); left < 100 {
			t.Fatalf("the clock did not restart for the next player: %v", left)
		}
		var recorded bool
		if err := pool.QueryRow(ctx, "SELECT (result->>'timeout')::boolean FROM game_commands WHERE match_id=$1 AND actor_id=$2", m, idle).Scan(&recorded); err != nil || !recorded {
			t.Fatalf("timeout move not recorded as a command: %v %v", recorded, err)
		}
		// A second sweep does nothing: the new player has a fresh clock.
		sweep()
		if mt, _ := e.q.Match(ctx, m); mt.Revision != 1 {
			t.Fatalf("double timeout: revision %d", mt.Revision)
		}
		if s, _ := e.do(a, "POST", "/api/v1/matches/"+m+"/leave", tokens[idle], nil); s != 204 {
			t.Fatalf("leave=%d", s)
		}
	})

	t.Run("all-offline matches expire", func(t *testing.T) {
		m4 := start()
		if _, err := pool.Exec(ctx, "UPDATE match_participants SET disconnected_at=now()-interval '25 hours' WHERE match_id=$1", m4); err != nil {
			t.Fatal(err)
		}
		if err := a.app.Hub.Matches.Sweep(ctx); err != nil {
			t.Fatal(err)
		}
		mt, _ := e.q.Match(ctx, m4)
		if mt.Status != "abandoned" || mt.EndReason.String != "expired" {
			t.Fatalf("expired match %s %s", mt.Status, mt.EndReason.String)
		}
	})

	t.Run("chat moderation", func(t *testing.T) {
		s, msg := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/chat", bobToken, map[string]any{"body": "hello", "client_id": uuid()})
		if s != 200 {
			t.Fatalf("chat=%d", s)
		}
		id := fmt.Sprintf("%.0f", msg["id"].(float64))
		report := func(token string) int {
			s, _ := e.do(a, "POST", "/api/v1/rooms/"+roomID+"/chat/"+id+"/report", token, map[string]any{"reason": "spam"})
			return s
		}
		if report(aliceToken) != 204 || report(aliceToken) != 204 {
			t.Fatal("report")
		}
		if report(bobToken) != 400 || report(eveToken) != 404 {
			t.Fatal("self or outsider report accepted")
		}
		var n int
		_ = pool.QueryRow(ctx, "SELECT count(*) FROM chat_reports WHERE message_id=$1", msg["id"]).Scan(&n)
		if n != 1 {
			t.Fatalf("%d reports stored", n)
		}
		count := func() int {
			_, list := e.do(a, "GET", "/api/v1/rooms/"+roomID+"/chat", aliceToken, nil)
			return len(list["items"].([]any))
		}
		before := count()
		if s, _ := e.do(a, "PUT", "/api/v1/mutes/"+bob, aliceToken, nil); s != 204 {
			t.Fatal("mute")
		}
		if count() >= before {
			t.Fatal("muted messages still shown")
		}
		_, mutes := e.do(a, "GET", "/api/v1/mutes", aliceToken, nil)
		if len(mutes["items"].([]any)) != 1 {
			t.Fatal("mute list")
		}
		if s, _ := e.do(a, "DELETE", "/api/v1/mutes/"+bob, aliceToken, nil); s != 204 || count() != before {
			t.Fatal("unmute")
		}
	})

	t.Run("deleting an account abandons its match", func(t *testing.T) {
		m5 := start()
		if s, _ := e.do(a, "DELETE", "/api/v1/me", bobToken, map[string]any{"password": "test-password-123"}); s != 204 {
			t.Fatalf("delete=%d", s)
		}
		mt, _ := e.q.Match(ctx, m5)
		if mt.Status != "abandoned" || mt.EndReason.String != "left" {
			t.Fatalf("match after deletion %s %s", mt.Status, mt.EndReason.String)
		}
	})
}

// stage gives the active player two complete sets, one brown card on the
// table in set "staged-brown", and Baltic Avenue in hand.
func stage(t *testing.T, s *monopoly.State) {
	t.Helper()
	cards := []monopoly.CardID{"property-park-place", "property-boardwalk", "property-electric-company", "property-water-works", "property-mediterranean-avenue", "property-baltic-avenue"}
	for _, id := range cards {
		remove := func(list []monopoly.CardID) []monopoly.CardID {
			return slices.DeleteFunc(list, func(x monopoly.CardID) bool { return x == id })
		}
		s.Draw, s.Discard = remove(s.Draw), remove(s.Discard)
		for i := range s.Players {
			p := &s.Players[i]
			p.Hand, p.Bank = remove(p.Hand), remove(p.Bank)
			for j := range p.Sets {
				p.Sets[j].Cards = remove(p.Sets[j].Cards)
			}
			p.Sets = slices.DeleteFunc(p.Sets, func(set monopoly.PropertySet) bool { return len(set.Cards) == 0 })
		}
	}
	p := &s.Players[s.Active]
	p.Sets = append(p.Sets,
		monopoly.PropertySet{ID: "staged-db", Color: monopoly.DarkBlue, Cards: cards[0:2]},
		monopoly.PropertySet{ID: "staged-ut", Color: monopoly.Utility, Cards: cards[2:4]},
		monopoly.PropertySet{ID: "staged-brown", Color: monopoly.Brown, Cards: cards[4:5]},
	)
	p.Hand = append(p.Hand, cards[5])
	if err := s.CheckInvariants(); err != nil {
		t.Fatalf("staged state invalid: %v", err)
	}
}
