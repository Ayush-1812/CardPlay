package realtime

import (
	"cardplay/internal/chat"
	"cardplay/internal/httpx"
	"cardplay/internal/rooms"
	"cardplay/internal/store"
	"context"
	"encoding/json"
	"errors"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"sync"
	"time"
)

type Envelope struct {
	Version int             `json:"v"`
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	RoomID  string          `json:"room_id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}
type outbound struct {
	Version int    `json:"v"`
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	RoomID  string `json:"room_id,omitempty"`
	Payload any    `json:"payload,omitempty"`
}
type client struct {
	conn                 *websocket.Conn
	actor, session, room string
	send                 chan outbound
}
type Hub struct {
	DB      *pgxpool.Pool
	Rooms   *rooms.Module
	Chat    *chat.Module
	Origin  string
	mu      sync.Mutex
	clients map[*client]bool
}

func New(db *pgxpool.Pool, rm *rooms.Module, ch *chat.Module, origin string) *Hub {
	return &Hub{DB: db, Rooms: rm, Chat: ch, Origin: origin, clients: map[*client]bool{}}
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
	c := &client{conn: conn, actor: a.ID, session: a.SessionHash, send: make(chan outbound, 32)}
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.clients, c); h.mu.Unlock() }()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
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
				if _, err := store.New(h.DB).SessionUser(ctx, c.session); err != nil {
					_ = conn.Close(websocket.StatusPolicyViolation, "Session expired")
					return
				}
				h.mu.Lock()
				room := c.room
				h.mu.Unlock()
				if room != "" {
					if err := h.Rooms.Seen(ctx, room, c.actor); err != nil {
						_ = conn.Close(websocket.StatusPolicyViolation, "Room unavailable")
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
	for {
		var in Envelope
		if err = wsjson.Read(ctx, conn, &in); err != nil {
			return
		}
		if time.Since(window) > 10*time.Second {
			window = time.Now()
			messages = 0
		}
		messages++
		if messages > 30 {
			_ = conn.Close(websocket.StatusPolicyViolation, "Rate limit")
			return
		}
		sendError := func(code, message string) {
			h.queue(c, outbound{Version: 1, Type: "error", ID: in.ID, Payload: map[string]string{"code": code, "message": message}})
		}
		if in.Version != 1 || !httpx.UUID(in.ID) {
			sendError("INVALID_REQUEST", "Use v=1 and a UUID request id")
			continue
		}
		if _, err := store.New(h.DB).SessionUser(ctx, c.session); err != nil {
			_ = conn.Close(websocket.StatusPolicyViolation, "Session expired")
			return
		}
		switch in.Type {
		case "ping":
			h.queue(c, outbound{Version: 1, Type: "pong", ID: in.ID})
		case "room.subscribe":
			if !httpx.UUID(in.RoomID) {
				sendError("INVALID_REQUEST", "Invalid room id")
				continue
			}
			view, err := h.Rooms.View(ctx, in.RoomID, c.actor)
			if err != nil {
				sendError("NOT_FOUND", "Room unavailable")
				continue
			}
			if err = h.Rooms.Seen(ctx, in.RoomID, c.actor); err != nil {
				sendError("NOT_FOUND", "Room unavailable")
				continue
			}
			h.mu.Lock()
			c.room = in.RoomID
			h.mu.Unlock()
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
		case "game.command":
			sendError("GAME_NOT_READY", "Game commands are unavailable in the foundation release")
		default:
			sendError("UNKNOWN_MESSAGE", "Unknown message type")
		}
	}
}
func (h *Hub) queue(c *client, msg outbound) {
	select {
	case c.send <- msg:
	default:
		_ = c.conn.CloseNow()
	}
}
func (h *Hub) broadcast(room, kind string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.room == room {
			h.queue(c, outbound{Version: 1, Type: kind, RoomID: room})
		}
	}
}

// Run delivers persisted invalidations at least once. No private state or chat
// body is broadcast; clients refetch through their authorized filtered views.
func (h *Hub) Run(ctx context.Context) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	cleanup := time.NewTicker(time.Hour)
	defer cleanup.Stop()
	presence := time.NewTicker(5 * time.Second)
	defer presence.Stop()
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
		case <-t.C:
			items, err := q.PendingOutbox(ctx)
			if err != nil {
				continue
			}
			for _, item := range items {
				h.broadcast(item.RoomID, item.Kind)
				_ = q.DeliverOutbox(ctx, item.ID)
			}
		case <-cleanup.C:
			_ = q.PurgeChat(ctx)
			_ = q.PurgeOutbox(ctx)
			_ = q.DeleteExpiredSessions(ctx)
		case <-presence.C:
			_ = h.Rooms.TransferAbsentHosts(ctx)
		}
	}
}
