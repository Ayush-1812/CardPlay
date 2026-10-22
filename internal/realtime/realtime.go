package realtime

import (
	"cardplay/internal/chat"
	"cardplay/internal/game"
	"cardplay/internal/httpx"
	"cardplay/internal/matches"
	"cardplay/internal/obs"
	"cardplay/internal/rooms"
	"cardplay/internal/store"
	"cardplay/internal/voice"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Application close codes tell the client why a socket ended. Other closes are
// transient and the client reconnects with backoff.
const (
	closeSessionEnded    websocket.StatusCode = 4001
	closeRoomUnavailable websocket.StatusCode = 4004
	// closeReplaced: another tab or device took control of this seat (PRD P08).
	closeReplaced websocket.StatusCode = 4009
)

// sockets numbers connections so a voice membership can be tied to the one
// socket that created it.
var sockets atomic.Uint64

type Envelope struct {
	Version int             `json:"v"`
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	RoomID  string          `json:"room_id,omitempty"`
	MatchID string          `json:"match_id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}
type outbound struct {
	Version int    `json:"v"`
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	RoomID  string `json:"room_id,omitempty"`
	MatchID string `json:"match_id,omitempty"`
	Payload any    `json:"payload,omitempty"`
}

// voiceSignal is one WebRTC negotiation frame passed between two players.
// The server never parses the description or the candidate: it checks who is
// allowed to receive it and relays it unchanged.
type voiceSignal struct {
	To   string          `json:"to"`
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// voiceState is a player's own microphone state, shown to the others.
type voiceState struct {
	Muted    bool `json:"muted"`
	Deafened bool `json:"deafened"`
	Speaking bool `json:"speaking"`
}

// commandMessage is the payload of a game.command frame.
type commandMessage struct {
	CommandID        string          `json:"command_id"`
	ExpectedRevision int64           `json:"expected_revision"`
	Kind             string          `json:"kind"`
	Payload          json.RawMessage `json:"payload"`
}

type client struct {
	conn                 *websocket.Conn
	actor, session, room string
	// match, matchRoom and gen identify the seat this socket controls; guarded by Hub.mu.
	match, matchRoom string
	gen              int64
	send             chan outbound
	// dirty coalesces match notifications; the push loop sends the latest projection.
	dirty chan struct{}
	// voiceRoom is the room whose voice channel this socket joined, and
	// owner identifies the socket itself, so one tab closing cannot remove a
	// player another tab put in the channel. Guarded by Hub.mu.
	voiceRoom string
	owner     string
}
type Hub struct {
	DB      *pgxpool.Pool
	Rooms   *rooms.Module
	Chat    *chat.Module
	Matches *matches.Module
	// Voice is optional: a nil module leaves voice signalling refused and the
	// client hides the controls.
	Voice   *voice.Module
	Origin  string
	mu      sync.Mutex
	clients map[*client]bool
	// listening reports whether the outbox LISTEN connection is active;
	// without it this instance cannot deliver live updates.
	listening atomic.Bool
	// Hourly runs extra hourly housekeeping (for example idle-guest cleanup).
	Hourly []func(context.Context) error
}

// Listening reports whether live change notifications are being received.
func (h *Hub) Listening() bool { return h.listening.Load() }

// Connections returns the number of open WebSocket connections.
func (h *Hub) Connections() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func New(db *pgxpool.Pool, rm *rooms.Module, ch *chat.Module, mt *matches.Module, origin string) *Hub {
	return &Hub{DB: db, Rooms: rm, Chat: ch, Matches: mt, Origin: origin, clients: map[*client]bool{}}
}

func (c *client) markDirty() {
	select {
	case c.dirty <- struct{}{}:
	default:
	}
}

// matchSeat returns the seat this socket controls, if any.
func (h *Hub) matchSeat(c *client) (string, int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return c.match, c.gen
}

func sendMatchError(h *Hub, c *client, id string, err error) {
	var me *matches.Error
	if errors.As(err, &me) {
		h.queue(c, outbound{Version: 1, Type: "error", ID: id, Payload: me})
		return
	}
	h.queue(c, outbound{Version: 1, Type: "error", ID: id, Payload: map[string]string{"code": "INTERNAL", "message": "Try again"}})
}
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// No credential in URL, and no wildcard origins. Recheck cookies through auth middleware.
	if r.Header.Get("Origin") != h.Origin {
		httpx.Error(w, r, 403, "ORIGIN_REJECTED", "WebSocket origin is not allowed")
		return
	}
	h.mu.Lock()
	count := 0
	for c := range h.clients {
		if c.actor == httpx.Actor(r).ID {
			count++
		}
	}
	h.mu.Unlock()
	if count >= 5 {
		obs.RateLimited.Inc("websocket_connections")
		httpx.Error(w, r, 429, "RATE_LIMITED", "Too many active connections")
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(16 << 10)
	a := httpx.Actor(r)
	c := &client{conn: conn, actor: a.ID, session: a.SessionHash, owner: strconv.FormatUint(sockets.Add(1), 10), send: make(chan outbound, 32), dirty: make(chan struct{}, 1)}
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.clients, c); h.mu.Unlock() }()
	// A closing socket leaves the voice channel it joined, so nobody is left
	// listed as present with no connection behind them.
	defer h.leaveVoice(c)
	// When the socket ends its seat becomes absent and the match pauses
	// (PRD P09). A replaced controller's generation no longer matches, so a
	// stale socket closing never pauses the match its successor is playing.
	defer func() {
		if match, gen := h.matchSeat(c); match != "" {
			dc, done := context.WithTimeout(context.Background(), 5*time.Second)
			_ = h.Matches.Disconnect(dc, match, c.actor, gen)
			done()
		}
	}()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	// Push loop: after every notification, send this player only their own
	// projection, read from the durable state.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.dirty:
			}
			match, gen := h.matchSeat(c)
			if match == "" {
				continue
			}
			st, err := h.Matches.StateFor(ctx, match, c.actor, gen)
			if errors.Is(err, matches.ErrReplaced) {
				_ = conn.Close(closeReplaced, "Opened in another tab")
				return
			}
			if err == nil {
				obs.StatePushes.Inc()
				h.queue(c, outbound{Version: 1, Type: "match.state", MatchID: match, Payload: st})
			}
		}
	}()
	go func() {
		defer cancel()
		tick := time.NewTicker(20 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-c.send:
				wc, done := context.WithTimeout(ctx, 5*time.Second)
				err := wsjson.Write(wc, conn, msg)
				done()
				if err != nil {
					return
				}
			case <-tick.C:
				// A transient database error skips this heartbeat rather than
				// ending a valid session or room subscription.
				if _, err := store.New(h.DB).SessionUser(ctx, c.session); errors.Is(err, pgx.ErrNoRows) {
					_ = conn.Close(closeSessionEnded, "Session expired")
					return
				}
				if match, gen := h.matchSeat(c); match != "" {
					if err := h.Matches.Heartbeat(ctx, match, c.actor, gen); errors.Is(err, matches.ErrReplaced) {
						_ = conn.Close(closeReplaced, "Opened in another tab")
						return
					}
				}
				h.mu.Lock()
				room := c.room
				h.mu.Unlock()
				if room != "" {
					if err := h.Rooms.Seen(ctx, room, c.actor); errors.Is(err, rooms.ErrForbidden) {
						_ = conn.Close(closeRoomUnavailable, "Room unavailable")
						return
					}
				}
				pc, done := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Ping(pc)
				done()
				if err != nil {
					return
				}
			}
		}
	}()
	c.send <- outbound{Version: 1, Type: "hello", Payload: map[string]any{"heartbeat_seconds": 20}}
	window := time.Now()
	messages := 0
	voiceMessages := 0
	for {
		var in Envelope
		if err = wsjson.Read(ctx, conn, &in); err != nil {
			return
		}
		if time.Since(window) > 10*time.Second {
			window = time.Now()
			messages = 0
			voiceMessages = 0
		}
		if strings.HasPrefix(in.Type, "voice.") {
			// Negotiating with three peers trickles dozens of candidates in
			// a few seconds, so signalling is budgeted separately from the
			// conversational traffic rather than sharing its limit.
			voiceMessages++
			if voiceMessages > 300 {
				obs.RateLimited.Inc("websocket_voice")
				_ = conn.Close(websocket.StatusPolicyViolation, "Rate limit")
				return
			}
		} else {
			messages++
			if messages > 30 {
				obs.RateLimited.Inc("websocket")
				_ = conn.Close(websocket.StatusPolicyViolation, "Rate limit")
				return
			}
		}
		sendError := func(code, message string) {
			h.queue(c, outbound{Version: 1, Type: "error", ID: in.ID, Payload: map[string]string{"code": code, "message": message}})
		}
		obs.WSMessages.Inc(messageType(in.Type))
		if in.Version != 1 || !httpx.UUID(in.ID) {
			sendError("INVALID_REQUEST", "Use v=1 and a UUID request id")
			continue
		}
		if _, err := store.New(h.DB).SessionUser(ctx, c.session); errors.Is(err, pgx.ErrNoRows) {
			_ = conn.Close(closeSessionEnded, "Session expired")
			return
		} else if err != nil {
			sendError("INTERNAL", "Try again")
			continue
		}
		switch in.Type {
		case "ping":
			h.queue(c, outbound{Version: 1, Type: "pong", ID: in.ID})
		case "room.subscribe":
			if !httpx.UUID(in.RoomID) {
				sendError("INVALID_REQUEST", "Invalid room id")
				continue
			}
			// Register before reading so no committed change can fall between
			// the snapshot and the subscription.
			h.mu.Lock()
			c.room = in.RoomID
			h.mu.Unlock()
			view, err := h.Rooms.View(ctx, in.RoomID, c.actor)
			if err == nil {
				err = h.Rooms.Seen(ctx, in.RoomID, c.actor)
			}
			if err != nil {
				h.mu.Lock()
				c.room = ""
				h.mu.Unlock()
				sendError("NOT_FOUND", "Room unavailable")
				continue
			}
			h.queue(c, outbound{Version: 1, Type: "room.snapshot", ID: in.ID, RoomID: in.RoomID, Payload: view})
		case "chat.send":
			if !httpx.UUID(in.RoomID) {
				sendError("INVALID_REQUEST", "Invalid room id")
				continue
			}
			var payload chat.Input
			if json.Unmarshal(in.Payload, &payload) != nil {
				sendError("INVALID_REQUEST", "Invalid chat payload")
				continue
			}
			msg, err := h.Chat.Send(ctx, in.RoomID, c.actor, payload)
			if err != nil {
				var ce *chat.Error
				if errors.As(err, &ce) {
					sendError(ce.Code, ce.Message)
				} else {
					sendError("INTERNAL", "Chat unavailable")
				}
				continue
			}
			h.queue(c, outbound{Version: 1, Type: "ack", ID: in.ID, RoomID: in.RoomID, Payload: map[string]any{"message_id": msg.ID, "client_id": msg.ClientID}})
		case "voice.join":
			if h.Voice == nil {
				sendError("VOICE_DISABLED", "Voice chat is switched off on this server")
				continue
			}
			if !httpx.UUID(in.RoomID) {
				sendError("INVALID_REQUEST", "Invalid room id")
				continue
			}
			// Membership of the room is checked inside Join, in the database,
			// not from anything the client said about itself.
			list, err := h.Voice.Join(ctx, in.RoomID, c.actor, c.owner)
			if err != nil {
				sendVoiceError(h, c, in.ID, err)
				continue
			}
			policy, err := h.Voice.Policy(ctx, in.RoomID)
			if err != nil {
				sendError("INTERNAL", "Try again")
				continue
			}
			h.mu.Lock()
			c.voiceRoom = in.RoomID
			h.mu.Unlock()
			h.queue(c, outbound{Version: 1, Type: "voice.joined", ID: in.ID, RoomID: in.RoomID, Payload: map[string]any{"self": c.actor, "participants": list, "policy": policy}})
			h.broadcastVoice(in.RoomID, outbound{Version: 1, Type: "voice.participants", RoomID: in.RoomID, Payload: map[string]any{"participants": list}}, c.actor)
		case "voice.leave":
			h.leaveVoice(c)
			h.queue(c, outbound{Version: 1, Type: "ack", ID: in.ID, RoomID: in.RoomID})
		case "voice.state":
			if h.Voice == nil {
				sendError("VOICE_DISABLED", "Voice chat is switched off on this server")
				continue
			}
			var st voiceState
			if json.Unmarshal(in.Payload, &st) != nil {
				sendError("INVALID_REQUEST", "Invalid voice state")
				continue
			}
			h.mu.Lock()
			room := c.voiceRoom
			h.mu.Unlock()
			if room == "" {
				sendError("VOICE_NOT_JOINED", "Join the voice channel first")
				continue
			}
			list, ok := h.Voice.SetState(room, c.actor, st.Muted, st.Deafened, st.Speaking)
			if !ok {
				sendError("VOICE_NOT_JOINED", "Join the voice channel first")
				continue
			}
			h.broadcastVoice(room, outbound{Version: 1, Type: "voice.participants", RoomID: room, Payload: map[string]any{"participants": list}}, "")
			h.queue(c, outbound{Version: 1, Type: "ack", ID: in.ID, RoomID: room})
		case "voice.signal":
			if h.Voice == nil {
				sendError("VOICE_DISABLED", "Voice chat is switched off on this server")
				continue
			}
			var sig voiceSignal
			if json.Unmarshal(in.Payload, &sig) != nil {
				sendError("INVALID_REQUEST", "Invalid signal")
				continue
			}
			switch sig.Kind {
			case "offer", "answer", "candidate", "bye":
			default:
				sendError("INVALID_REQUEST", "Unknown signal kind")
				continue
			}
			if !httpx.UUID(sig.To) || len(sig.Data) > 12<<10 {
				sendError("INVALID_REQUEST", "Invalid signal recipient or payload")
				continue
			}
			h.mu.Lock()
			room := c.voiceRoom
			h.mu.Unlock()
			if room == "" || !h.Voice.InChannel(room, c.actor) {
				sendError("VOICE_NOT_JOINED", "Join the voice channel first")
				continue
			}
			// Both ends must be in this room's channel. A player cannot
			// address a stranger, a spectator, or someone in another room.
			if sig.To == c.actor || !h.Voice.InChannel(room, sig.To) {
				sendError("VOICE_NO_PEER", "That player is not in this voice channel")
				continue
			}
			// The running game may forbid speech right now; refusing to relay
			// stops a connection being negotiated while it does.
			policy, err := h.Voice.Policy(ctx, room)
			if err != nil {
				sendError("INTERNAL", "Try again")
				continue
			}
			if !policy.Open {
				sendError("VOICE_CLOSED", policy.Reason)
				continue
			}
			if !h.sendVoice(room, sig.To, outbound{Version: 1, Type: "voice.signal", RoomID: room, Payload: map[string]any{"from": c.actor, "kind": sig.Kind, "data": sig.Data}}) {
				sendError("VOICE_NO_PEER", "That player is not in this voice channel")
				continue
			}
			h.queue(c, outbound{Version: 1, Type: "ack", ID: in.ID, RoomID: room})
		case "match.subscribe":
			if !httpx.UUID(in.MatchID) {
				sendError("INVALID_REQUEST", "Invalid match id")
				continue
			}
			gen, st, err := h.Matches.Subscribe(ctx, in.MatchID, c.actor)
			if err != nil {
				sendMatchError(h, c, in.ID, err)
				continue
			}
			var replaced []*client
			h.mu.Lock()
			prevMatch, prevGen := c.match, c.gen
			c.match, c.matchRoom, c.gen = in.MatchID, st.RoomID, gen
			for other := range h.clients {
				if other != c && other.actor == c.actor && other.match == in.MatchID {
					replaced = append(replaced, other)
				}
			}
			h.mu.Unlock()
			for _, other := range replaced {
				go func() { _ = other.conn.Close(closeReplaced, "Opened in another tab") }()
			}
			if prevMatch != "" && prevMatch != in.MatchID {
				_ = h.Matches.Disconnect(ctx, prevMatch, c.actor, prevGen)
			}
			h.queue(c, outbound{Version: 1, Type: "match.state", ID: in.ID, MatchID: in.MatchID, Payload: st})
			// A change committed after the state above was read but before this
			// socket was registered would otherwise be missed: refresh once more.
			c.markDirty()
		case "match.resync":
			if match, _ := h.matchSeat(c); match == "" {
				sendError("INVALID_REQUEST", "Subscribe to a match first")
				continue
			}
			c.markDirty()
		case "game.command":
			match, gen := h.matchSeat(c)
			if match == "" || in.MatchID != match {
				sendError("INVALID_REQUEST", "Subscribe to this match before sending commands")
				continue
			}
			var cmd commandMessage
			if json.Unmarshal(in.Payload, &cmd) != nil {
				sendError("INVALID_REQUEST", "Invalid command payload")
				continue
			}
			started := time.Now()
			ack, err := h.Matches.Execute(ctx, match, c.actor, gen, game.Command{ID: cmd.CommandID, ExpectedRevision: cmd.ExpectedRevision, Kind: cmd.Kind, Payload: cmd.Payload})
			obs.GameCommandDuration.Observe(time.Since(started).Seconds())
			obs.GameCommands.Inc(commandResult(err))
			if errors.Is(err, matches.ErrReplaced) {
				_ = conn.Close(closeReplaced, "Opened in another tab")
				return
			}
			if err != nil {
				sendMatchError(h, c, in.ID, err)
				var me *matches.Error
				if errors.As(err, &me) && me.Code == "STALE_REVISION" {
					c.markDirty()
				}
				continue
			}
			h.queue(c, outbound{Version: 1, Type: "ack", ID: in.ID, MatchID: match, Payload: ack})
		default:
			sendError("UNKNOWN_MESSAGE", "Unknown message type")
		}
	}
}

// messageType bounds metric label values to the known protocol.
func messageType(t string) string {
	switch t {
	case "ping", "room.subscribe", "chat.send", "match.subscribe", "match.resync", "game.command",
		"voice.join", "voice.leave", "voice.state", "voice.signal":
		return t
	}
	return "unknown"
}

func commandResult(err error) string {
	var me *matches.Error
	switch {
	case err == nil:
		return "applied"
	case !errors.As(err, &me):
		return "error"
	case me.Code == "STALE_REVISION":
		return "stale"
	case me.Code == "IDEMPOTENCY_CONFLICT":
		return "duplicate_conflict"
	case me.Status == 422:
		return "rule_rejected"
	}
	return "refused"
}

func notificationKind(k string) string {
	switch k {
	case "room.updated", "chat.updated", "match.updated":
		return k
	}
	return "other"
}

func (h *Hub) queue(c *client, msg outbound) {
	select {
	case c.send <- msg:
	default:
		_ = c.conn.CloseNow()
	}
}
func (h *Hub) broadcast(room, kind string) {
	// A match change can open or close voice -- Trump closes it while the
	// trump is chosen -- so the channel is told as soon as the state moves.
	if kind == "match.updated" {
		go h.pushVoicePolicy(context.Background(), room)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if kind == "match.updated" {
			if c.matchRoom == room {
				c.markDirty()
			}
			continue
		}
		if c.room == room {
			h.queue(c, outbound{Version: 1, Type: kind, RoomID: room})
		}
	}
}

// leaveVoice drops this socket's voice membership and tells the rest of the
// channel. It runs on an explicit leave and again when the socket closes, so
// a dropped connection never leaves a player listed as present.
func (h *Hub) leaveVoice(c *client) {
	h.mu.Lock()
	room := c.voiceRoom
	c.voiceRoom = ""
	h.mu.Unlock()
	if room == "" || h.Voice == nil {
		return
	}
	list := h.Voice.Leave(room, c.actor, c.owner)
	if list == nil {
		return
	}
	h.broadcastVoice(room, outbound{Version: 1, Type: "voice.participants", RoomID: room, Payload: map[string]any{"participants": list}}, "")
}

// broadcastVoice sends to every socket in a room's voice channel, skipping
// one user when except is set.
func (h *Hub) broadcastVoice(room string, msg outbound, except string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.voiceRoom == room && c.actor != except {
			h.queue(c, msg)
		}
	}
}

// sendVoice relays one frame to a single player's voice sockets and reports
// whether anyone received it.
func (h *Hub) sendVoice(room, userID string, msg outbound) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	sent := false
	for c := range h.clients {
		if c.voiceRoom == room && c.actor == userID {
			h.queue(c, msg)
			sent = true
		}
	}
	return sent
}

// pushVoicePolicy tells a room's voice channel whether the running game
// allows speech now. It runs after every match change, so the moment a
// restricted phase starts every client hears about it and stops sending.
func (h *Hub) pushVoicePolicy(ctx context.Context, room string) {
	if h.Voice == nil || len(h.Voice.Participants(room)) == 0 {
		return
	}
	policy, err := h.Voice.Policy(ctx, room)
	if err != nil {
		return
	}
	h.broadcastVoice(room, outbound{Version: 1, Type: "voice.policy", RoomID: room, Payload: policy}, "")
}

// sendVoiceError turns a voice refusal into the client's error frame.
func sendVoiceError(h *Hub, c *client, id string, err error) {
	var ve *voice.Error
	if errors.As(err, &ve) {
		h.queue(c, outbound{Version: 1, Type: "error", ID: id, Payload: map[string]string{"code": ve.Code, "message": ve.Message}})
		return
	}
	h.queue(c, outbound{Version: 1, Type: "error", ID: id, Payload: map[string]string{"code": "INTERNAL", "message": "Try again"}})
}

// broadcastSubscribed hints every subscribed client to refetch, used when
// notifications may have been missed while the listener was not listening.
func (h *Hub) broadcastSubscribed() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.room != "" {
			h.queue(c, outbound{Version: 1, Type: "room.updated", RoomID: c.room})
			h.queue(c, outbound{Version: 1, Type: "chat.updated", RoomID: c.room})
		}
		if c.match != "" {
			c.markDirty()
		}
	}
}

// Run pushes committed outbox invalidations to this instance's sockets. The
// outbox insert trigger issues a PostgreSQL NOTIFY on commit, so every API
// replica receives every event. No private state or chat body is broadcast;
// clients refetch through their authorized filtered views.
func (h *Hub) Run(ctx context.Context) {
	go h.listen(ctx)
	cleanup := time.NewTicker(time.Hour)
	defer cleanup.Stop()
	presence := time.NewTicker(5 * time.Second)
	defer presence.Stop()
	idleRooms := time.NewTicker(time.Minute)
	defer idleRooms.Stop()
	q := store.New(h.DB)
	for {
		select {
		case <-ctx.Done():
			h.mu.Lock()
			for c := range h.clients {
				_ = c.conn.CloseNow()
			}
			h.mu.Unlock()
			return
		case <-cleanup.C:
			_ = q.PurgeChat(ctx)
			_ = q.PurgeOutbox(ctx)
			_ = q.DeleteExpiredSessions(ctx)
			_ = q.DeleteExpiredLoginDevices(ctx)
			_ = q.DeleteExpiredSocketTickets(ctx)
			_ = h.Matches.Retention(ctx)
			for _, job := range h.Hourly {
				if err := job(ctx); err != nil && ctx.Err() == nil {
					slog.Warn("hourly housekeeping failed", "error_type", fmt.Sprintf("%T", err))
				}
			}
		case <-idleRooms.C:
			if _, err := h.Rooms.DeleteIdle(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("idle room cleanup failed", "error_type", fmt.Sprintf("%T", err))
			}
		case <-presence.C:
			_ = h.Rooms.TransferAbsentHosts(ctx)
			if err := h.Matches.Sweep(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("match sweep failed", "error_type", fmt.Sprintf("%T", err))
			}
		}
	}
}

func (h *Hub) listen(ctx context.Context) {
	delay := time.Second
	for {
		listened, err := h.listenOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if listened {
			delay = time.Second
		}
		slog.Warn("outbox listener disconnected; retrying", "error_type", fmt.Sprintf("%T", err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func (h *Hub) listenOnce(ctx context.Context) (bool, error) {
	pooled, err := h.DB.Acquire(ctx)
	if err != nil {
		return false, err
	}
	// A listening connection must never return to the pool.
	conn := pooled.Hijack()
	defer func() { _ = conn.Close(context.Background()) }()
	if _, err = conn.Exec(ctx, "LISTEN cardplay_outbox"); err != nil {
		return false, err
	}
	h.listening.Store(true)
	defer h.listening.Store(false)
	// Clients that subscribed before LISTEN took effect, at startup or during
	// a listener reconnect, may have missed notifications: tell them to refetch.
	h.broadcastSubscribed()
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, err
		}
		var event struct {
			RoomID string `json:"room_id"`
			Kind   string `json:"kind"`
		}
		if json.Unmarshal([]byte(n.Payload), &event) == nil && event.RoomID != "" {
			obs.Notifications.Inc(notificationKind(event.Kind))
			h.broadcast(event.RoomID, event.Kind)
		}
	}
}
