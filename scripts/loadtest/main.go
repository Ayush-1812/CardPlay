// Command loadtest drives realistic concurrent Monopoly Deal matches against a
// running API: real logins, rooms, invitations, WebSocket play by simple bots,
// chat and dashboard reads. It creates its own accounts directly in the
// database, so point it only at a disposable test database.
//
//	go run ./scripts/loadtest -api http://127.0.0.1:18080 -origin http://localhost:3000 \
//	    -database postgres://... -matches 25 -players 4 -duration 3m
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	mrand "math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cardplay/internal/accounts"
	"cardplay/internal/store"
	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	apiURL   = flag.String("api", "http://127.0.0.1:18080", "API base URL")
	origin   = flag.String("origin", "http://localhost:3000", "APP_ORIGIN of the API")
	dbURL    = flag.String("database", "", "database URL used to create load-test accounts")
	nMatches = flag.Int("matches", 25, "concurrent matches")
	nPlayers = flag.Int("players", 4, "players per match (2-5)")
	duration = flag.Duration("duration", 3*time.Minute, "how long to play")
	think    = flag.Duration("think", 400*time.Millisecond, "mean bot think time per action")
	chatGap  = flag.Duration("chat", 20*time.Second, "mean interval between chat messages per player")
	ramp     = flag.Duration("ramp", 10*time.Second, "spread logins evenly over this long (0 = all at once)")
)

const password = "load-test-password-1"

// stats collects latencies (milliseconds) and counters across goroutines.
type stats struct {
	mu       sync.Mutex
	lat      map[string][]float64
	counts   map[string]int
	finished atomic.Int64
}

func (s *stats) observe(name string, d time.Duration) {
	s.mu.Lock()
	s.lat[name] = append(s.lat[name], float64(d.Microseconds())/1000)
	s.mu.Unlock()
}
func (s *stats) inc(name string) {
	s.mu.Lock()
	s.counts[name]++
	s.mu.Unlock()
}

var st = &stats{lat: map[string][]float64{}, counts: map[string]int{}}

func uuid() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

type card struct {
	ID     string   `json:"id"`
	Kind   string   `json:"kind"`
	Value  int      `json:"value"`
	Colors []string `json:"colors"`
	Action string   `json:"action"`
}

var catalog = map[string]card{}

type player struct {
	email  string
	client *http.Client
	cookie string
	userID string
}

func (p *player) do(ctx context.Context, name, method, path string, body any, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(ctx, method, *apiURL+path, reader)
	req.Header.Set("Content-Type", "application/json")
	if method != "GET" {
		req.Header.Set("Origin", *origin)
	}
	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		reason := err.Error()
		if i := strings.LastIndex(reason, ": "); i >= 0 {
			reason = reason[i+2:]
		}
		st.inc("http_error " + name + ": " + reason)
		return 0, err
	}
	defer resp.Body.Close()
	st.observe("http "+name, time.Since(start))
	if resp.StatusCode >= 400 {
		st.inc(fmt.Sprintf("http_%d %s", resp.StatusCode, name))
	}
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode, nil
}

// must stops the run on an unexpected status; want 0 accepts any 2xx.
func must(code int, err error, want int, what string) {
	if err != nil || (want != 0 && code != want) || code/100 != 2 {
		log.Fatalf("%s: status %d err %v", what, code, err)
	}
}

type envelope struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	MatchID string          `json:"match_id"`
	Payload json.RawMessage `json:"payload"`
}

type setView struct {
	ID       string   `json:"id"`
	Color    string   `json:"color"`
	Cards    []string `json:"cards"`
	House    string   `json:"house"`
	Hotel    string   `json:"hotel"`
	Complete bool     `json:"complete"`
}
type publicPlayer struct {
	Seat       int       `json:"seat"`
	Bank       []string  `json:"bank"`
	Sets       []setView `json:"sets"`
	Unassigned []string  `json:"unassigned"`
	Detached   []string  `json:"detached"`
	Incoming   []string  `json:"incoming"`
}
type matchState struct {
	Status   string `json:"status"`
	Revision int64  `json:"revision"`
	View     struct {
		Public struct {
			Phase      string         `json:"phase"`
			PlaysLeft  int            `json:"plays_left"`
			Players    []publicPlayer `json:"players"`
			WaitingFor []int          `json:"waiting_for"`
			Pending    *struct {
				ID      int `json:"id"`
				Step    int `json:"step"`
				Current int `json:"current"`
				Targets []struct {
					Seat int `json:"seat"`
					Owed int `json:"owed"`
				} `json:"targets"`
			} `json:"pending"`
		} `json:"public"`
		Self struct {
			Seat int      `json:"seat"`
			Hand []string `json:"hand"`
		} `json:"self"`
		LegalActions []string `json:"legal_actions"`
	} `json:"view"`
}

// choose picks a legal, simple move. Bots bank money, lay properties, charge
// Birthday and Debt Collector (so payments happen), pay with the cheapest
// cards, accept every action and end their turn.
func choose(s *matchState) (string, any) {
	v := s.View
	me := v.Public.Players[v.Self.Seat]
	legal := func(k string) bool { return slices.Contains(v.LegalActions, k) }
	switch v.Public.Phase {
	case "response":
		p := v.Public.Pending
		return "accept", map[string]int{"pending": p.ID, "step": p.Step}
	case "payment":
		p := v.Public.Pending
		owed := p.Targets[p.Current].Owed
		var all []string
		all = append(all, me.Bank...)
		for _, id := range me.Unassigned {
			if catalog[id].Kind != "rainbow_wild" {
				all = append(all, id)
			}
		}
		for _, set := range me.Sets {
			for _, id := range set.Cards {
				if catalog[id].Kind != "rainbow_wild" {
					all = append(all, id)
				}
			}
			for _, b := range []string{set.House, set.Hotel} {
				if b != "" {
					all = append(all, b)
				}
			}
		}
		all = append(all, me.Detached...)
		sort.SliceStable(all, func(i, j int) bool { return catalog[all[i]].Value < catalog[all[j]].Value })
		pay, sum := []string{}, 0
		for _, id := range all {
			if sum >= owed {
				break
			}
			if catalog[id].Value > 0 {
				pay = append(pay, id)
				sum += catalog[id].Value
			}
		}
		if sum < owed {
			pay = all
		}
		return "pay", map[string]any{"pending": p.ID, "step": p.Step, "cards": pay}
	case "placement":
		id := me.Incoming[0]
		return "place_received", placement(me, id)
	}
	hand := v.Self.Hand
	if v.Public.PlaysLeft > 0 {
		for _, id := range hand {
			c := catalog[id]
			switch {
			case c.Kind == "property" || c.Kind == "wild" || c.Kind == "rainbow_wild":
				if legal("play_property") {
					return "play_property", placement(me, id)
				}
			case c.Action == "birthday" && legal("birthday"):
				return "birthday", map[string]string{"card": id}
			case c.Action == "debt_collector" && legal("debt_collector"):
				return "debt_collector", map[string]any{"card": id, "target": (v.Self.Seat + 1) % len(v.Public.Players)}
			}
		}
		for _, id := range hand {
			if c := catalog[id]; c.Kind == "money" || c.Kind == "action" || c.Kind == "rent" || c.Kind == "rent_any" {
				if legal("bank") {
					return "bank", map[string]string{"card": id}
				}
			}
		}
	}
	var ret []string
	if len(hand) > 7 {
		ret = hand[:len(hand)-7]
	}
	return "end_turn", map[string]any{"return": ret}
}

// placement puts a property into a matching incomplete set, else a new set;
// a multicolor wild stays unassigned.
func placement(me publicPlayer, id string) map[string]any {
	c := catalog[id]
	if c.Kind == "rainbow_wild" {
		return map[string]any{"card": id}
	}
	for _, color := range c.Colors {
		for _, set := range me.Sets {
			if set.Color == color && !set.Complete {
				return map[string]any{"card": id, "set": set.ID}
			}
		}
	}
	return map[string]any{"card": id, "color": c.Colors[0]}
}

type seat struct {
	p     *player
	conn  *websocket.Conn
	match string
}

func (s *seat) send(ctx context.Context, fields map[string]any) (string, error) {
	id := uuid()
	fields["v"], fields["id"] = 1, id
	b, _ := json.Marshal(fields)
	wc, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return id, s.conn.Write(wc, websocket.MessageText, b)
}

func dial(ctx context.Context, p *player) (*websocket.Conn, error) {
	h := http.Header{}
	h.Set("Origin", *origin)
	h.Set("Cookie", accounts.CookieName+"="+p.cookie)
	start := time.Now()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(*apiURL, "http")+"/ws", &websocket.DialOptions{HTTPHeader: h})
	if err == nil {
		st.observe("ws connect", time.Since(start))
		conn.SetReadLimit(1 << 20)
	}
	return conn, err
}

// play runs one seat's bot until ctx ends or the match finishes.
func (s *seat) play(ctx context.Context, roomID string) {
	pendingSince := map[string]time.Time{}
	var mu sync.Mutex
	var inflight string
	var sentAt time.Time
	var sentRev int64
	// Chat and dashboard reads in the background, like a real player.
	go func() {
		for {
			wait := time.Duration(mrand.Int64N(int64(2 * *chatGap)))
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			mu.Lock()
			id, err := s.send(ctx, map[string]any{"type": "chat.send", "room_id": roomID, "payload": map[string]string{"client_id": uuid(), "body": "gl hf " + time.Now().Format("15:04:05")}})
			if err == nil {
				pendingSince[id] = time.Now()
			}
			mu.Unlock()
			_, _ = s.p.do(ctx, "GET /rooms", "GET", "/api/v1/rooms", nil, nil)
		}
	}()
	// Frames are timestamped on arrival by a separate reader so think time
	// in the bot loop never inflates measured latency.
	type stamped struct {
		e  envelope
		at time.Time
	}
	frames := make(chan stamped, 256)
	var finished atomic.Bool
	defer finished.Store(true)
	go func() {
		defer close(frames)
		for {
			_, raw, err := s.conn.Read(ctx)
			if err != nil {
				// Closes after the match ended are this tool's own.
				if ctx.Err() == nil && !finished.Load() {
					st.inc("ws_closed " + websocket.CloseStatus(err).String())
				}
				return
			}
			var e envelope
			if json.Unmarshal(raw, &e) == nil {
				frames <- stamped{e, time.Now()}
			}
		}
	}()
	for f := range frames {
		e := f.e
		mu.Lock()
		if t, ok := pendingSince[e.ID]; ok {
			delete(pendingSince, e.ID)
			if e.Type == "ack" {
				st.observe("ws chat ack", f.at.Sub(t))
			} else {
				var p struct {
					Code string `json:"code"`
				}
				_ = json.Unmarshal(e.Payload, &p)
				st.inc("chat_error " + p.Code)
			}
		}
		mu.Unlock()
		switch e.Type {
		case "ack":
			if e.ID == inflight {
				st.observe("ws command ack", f.at.Sub(sentAt))
			}
		case "error":
			if e.ID == inflight {
				var p struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				}
				_ = json.Unmarshal(e.Payload, &p)
				st.inc("command_error " + p.Code)
				if p.Code != "STALE_REVISION" {
					log.Printf("command error %s: %s", p.Code, p.Message)
				}
				inflight = ""
				// Ask for fresh state rather than wait for someone else to move.
				_, _ = s.send(ctx, map[string]any{"type": "match.resync"})
			}
		case "match.state":
			var ms matchState
			if json.Unmarshal(e.Payload, &ms) != nil {
				continue
			}
			st.inc("states received")
			if inflight != "" && ms.Revision > sentRev {
				st.observe("ws command to new state", f.at.Sub(sentAt))
				inflight = ""
			}
			if ms.Status == "finished" || ms.Status == "abandoned" {
				if ms.Status == "finished" && ms.View.Self.Seat == 0 {
					st.finished.Add(1)
				}
				return
			}
			if inflight != "" || ms.Status != "playing" || !slices.Contains(ms.View.Public.WaitingFor, ms.View.Self.Seat) {
				continue
			}
			kind, payload := choose(&ms)
			time.Sleep(time.Duration(float64(*think) * (0.5 + mrand.Float64())))
			sentRev, sentAt = ms.Revision, time.Now()
			var err error
			inflight, err = s.send(ctx, map[string]any{"type": "game.command", "match_id": s.match, "payload": map[string]any{
				"command_id": uuid(), "expected_revision": ms.Revision, "kind": kind, "payload": payload,
			}})
			if err != nil {
				return
			}
			st.inc("commands sent")
		}
	}
}

func runMatch(ctx context.Context, players []*player, wg *sync.WaitGroup, ready chan<- struct{}) {
	defer wg.Done()
	host := players[0]
	var room struct {
		ID string `json:"id"`
	}
	code, err := host.do(ctx, "POST /rooms", "POST", "/api/v1/rooms", map[string]any{"name": "Load table", "capacity": len(players)}, &room)
	must(code, err, 201, "create room")
	var inv struct {
		Token string `json:"token"`
	}
	code, err = host.do(ctx, "POST /invitations", "POST", "/api/v1/rooms/"+room.ID+"/invitations", map[string]any{}, &inv)
	must(code, err, 201, "invite")
	for _, p := range players[1:] {
		code, err = p.do(ctx, "POST /rooms/join", "POST", "/api/v1/rooms/join", map[string]string{"token": inv.Token}, nil)
		must(code, err, 200, "join")
	}
	seats := make([]*seat, len(players))
	for i, p := range players {
		conn, err := dial(ctx, p)
		if err != nil {
			log.Fatalf("dial: %v", err)
		}
		seats[i] = &seat{p: p, conn: conn}
		_, _ = seats[i].send(ctx, map[string]any{"type": "room.subscribe", "room_id": room.ID})
		code, err = p.do(ctx, "PUT /ready", "PUT", "/api/v1/rooms/"+room.ID+"/ready", map[string]bool{"ready": true}, nil)
		must(code, err, 0, "ready")
	}
	ready <- struct{}{}
	var started struct {
		MatchID string `json:"match_id"`
	}
	code, err = host.do(ctx, "POST /matches", "POST", "/api/v1/rooms/"+room.ID+"/matches", nil, &started)
	must(code, err, 201, "start match")
	var inner sync.WaitGroup
	for _, s := range seats {
		s.match = started.MatchID
		_, _ = s.send(ctx, map[string]any{"type": "match.subscribe", "match_id": started.MatchID})
		inner.Add(1)
		go func() {
			defer inner.Done()
			s.play(ctx, room.ID)
		}()
	}
	inner.Wait()
	for _, s := range seats {
		_ = s.conn.Close(websocket.StatusNormalClosure, "")
	}
}

func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	i := int(float64(len(xs)-1) * p)
	return xs[i]
}

func main() {
	flag.Parse()
	if *dbURL == "" || *nPlayers < 2 || *nPlayers > 5 {
		log.Fatal("need -database and 2-5 -players")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	q := store.New(pool)
	run := uuid()[:8]
	total := *nMatches * *nPlayers
	hash := accounts.PasswordHash(password)
	players := make([]*player, total)
	for i := range players {
		email := fmt.Sprintf("load-%s-%d@load.test", run, i)
		u, err := q.CreateUser(ctx, store.CreateUserParams{Email: email, Handle: fmt.Sprintf("l%s%d", run, i), DisplayName: fmt.Sprintf("Bot %d", i), PasswordHash: hash, EmailVerified: true})
		if err != nil {
			log.Fatal(err)
		}
		jar, _ := cookiejar.New(nil)
		players[i] = &player{email: email, userID: u.ID, client: &http.Client{Jar: jar, Timeout: 15 * time.Second}}
	}
	var catalogOut struct {
		Items []card `json:"items"`
	}
	if code, err := players[0].do(ctx, "GET /cards", "GET", "/api/v1/games/monopoly-deal/cards", nil, &catalogOut); err != nil || code != 200 {
		log.Fatalf("catalog: %d %v", code, err)
	}
	for _, c := range catalogOut.Items {
		catalog[c.ID] = c
	}
	// Players arrive over the ramp, as after an announcement; -ramp 0 sends
	// every login at once.
	log.Printf("logging in %d players over %s", total, *ramp)
	var wg sync.WaitGroup
	loginStart := time.Now()
	var loginFailures atomic.Int64
	for i, p := range players {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(int64(*ramp) * int64(i) / int64(total)))
			code, err := p.do(ctx, "POST /auth/login", "POST", "/api/v1/auth/login", map[string]string{"email": p.email, "password": password}, nil)
			if err != nil || code != 200 {
				loginFailures.Add(1)
				return
			}
			u, _ := http.NewRequest("GET", *apiURL, nil)
			for _, c := range p.client.Jar.Cookies(u.URL) {
				if c.Name == accounts.CookieName {
					p.cookie = c.Value
				}
			}
		}()
	}
	wg.Wait()
	log.Printf("logins done in %s (%d failed)", time.Since(loginStart).Round(time.Millisecond), loginFailures.Load())
	if loginFailures.Load() > 0 {
		report(time.Since(loginStart), total)
		os.Exit(1)
	}
	playCtx, cancel := context.WithTimeout(ctx, *duration)
	defer cancel()
	ready := make(chan struct{}, *nMatches)
	start := time.Now()
	for m := 0; m < *nMatches; m++ {
		wg.Add(1)
		go runMatch(playCtx, players[m**nPlayers:(m+1)**nPlayers], &wg, ready)
	}
	for range *nMatches {
		<-ready
	}
	log.Printf("%d rooms ready and %d sockets open after %s; playing", *nMatches, total, time.Since(start).Round(time.Millisecond))
	wg.Wait()
	elapsed := time.Since(start)
	report(elapsed, total)
	if st.counts["commands sent"] == 0 {
		log.Fatal(errors.New("no commands were sent"))
	}
}

func report(elapsed time.Duration, total int) {
	st.mu.Lock()
	defer st.mu.Unlock()
	fmt.Printf("\nLoad test: %d matches x %d players (%d users, %d sockets), %s, think %s\n", *nMatches, *nPlayers, total, total, elapsed.Round(time.Second), *think)
	fmt.Printf("matches finished: %d\n", st.finished.Load())
	fmt.Printf("\n%-28s %8s %9s %9s %9s %9s\n", "latency (ms)", "count", "p50", "p95", "p99", "max")
	names := make([]string, 0, len(st.lat))
	for k := range st.lat {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		xs := st.lat[k]
		sort.Float64s(xs)
		fmt.Printf("%-28s %8d %9.1f %9.1f %9.1f %9.1f\n", k, len(xs), percentile(xs, .5), percentile(xs, .95), percentile(xs, .99), xs[len(xs)-1])
	}
	fmt.Printf("\ncounters\n")
	keys := make([]string, 0, len(st.counts))
	for k := range st.counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-40s %d\n", k, st.counts[k])
	}
	fmt.Printf("  %-40s %.1f/s\n", "commands per second", float64(st.counts["commands sent"])/elapsed.Seconds())
}
