// Package voice carries in-game voice chat: the ICE configuration players
// need to reach each other, and the room-scoped registry the signalling
// layer validates against.
//
// Audio never passes through this server. Peers connect directly (or through
// a TURN relay) and the server only relays offers, answers and ICE
// candidates between players who are in the same room. Nothing is recorded
// and nothing is stored: the registry below lives in memory and is emptied
// when a socket closes.
package voice

import (
	"cardplay/internal/game"
	"cardplay/internal/httpx"
	"cardplay/internal/store"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MaxParticipants bounds a room's voice channel. Every peer connects to
// every other one, so the number of connections grows with the square of the
// participants; past this a mesh costs more upstream bandwidth than a phone
// can give. CardPlay's largest room seats five.
const MaxParticipants = 8

// Config is the ICE configuration, from internal/config.
type Config struct {
	Enabled    bool
	STUNURLs   []string
	TURNURLs   []string
	TURNSecret string
	TURNTTL    time.Duration
}

// Gate is asked whether the game running in a room currently allows players
// to transmit. It is the match runtime in production; nil leaves voice open.
type Gate interface {
	VoiceOpen(ctx context.Context, q *store.Queries, roomID string) (game.VoicePolicyState, error)
}

// Module serves the ICE endpoint and keeps the per-room participant list.
type Module struct {
	DB     *pgxpool.Pool
	Config Config
	Gate   Gate

	mu sync.Mutex
	// rooms maps a room id to the users currently in its voice channel and
	// the state each of them published. It is in-memory on purpose: a voice
	// channel is a property of a live connection, not something to store.
	rooms map[string]map[string]entry
}

// entry is one participant plus the socket that owns the membership. A
// player who opens a second tab takes the channel over; the first tab's
// socket then closes without removing the player the second tab put there.
type entry struct {
	Participant
	owner string
}

// Participant is one player in a voice channel, as everyone else sees them.
type Participant struct {
	UserID string `json:"user_id"`
	// Muted is the player's own choice not to transmit. Deafened is their
	// choice not to hear anyone; both are published so the others can show it.
	Muted    bool `json:"muted"`
	Deafened bool `json:"deafened"`
	// Speaking is published by the sender's own level detection, so the room
	// can light up who is talking without inspecting anyone's audio here.
	Speaking bool `json:"speaking"`
}

// Error is a refusal the client can act on.
type Error struct {
	Code    string
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

// ICEServer is one entry of RTCConfiguration.iceServers.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// ICEResponse is what a client needs to open a peer connection.
type ICEResponse struct {
	Enabled bool `json:"enabled"`
	// Servers is empty when voice is disabled.
	Servers []ICEServer `json:"ice_servers"`
	// TTLSeconds is how long the TURN credential stays valid; the client
	// fetches again before it expires, and before each new connection.
	TTLSeconds int `json:"ttl_seconds"`
	// RelayAvailable tells the client whether a restrictive network has any
	// hope: without TURN, two players behind symmetric NAT cannot connect.
	RelayAvailable  bool `json:"relay_available"`
	MaxParticipants int  `json:"max_participants"`
}

// ICE issues the ICE servers for the calling player, with a TURN credential
// that expires. The shared secret itself is never sent.
func (m *Module) ICE(w http.ResponseWriter, r *http.Request) {
	actor := httpx.Actor(r)
	if !m.Config.Enabled {
		httpx.JSON(w, 200, ICEResponse{Enabled: false, Servers: []ICEServer{}, MaxParticipants: MaxParticipants})
		return
	}
	// A credential is tied to the player it was issued to, so a leaked one
	// cannot be used to relay someone else's traffic for long.
	servers, ttl := m.iceServers(actor.ID, time.Now())
	httpx.JSON(w, 200, ICEResponse{
		Enabled:         true,
		Servers:         servers,
		TTLSeconds:      int(ttl.Seconds()),
		RelayAvailable:  len(m.Config.TURNURLs) > 0,
		MaxParticipants: MaxParticipants,
	})
}

// iceServers builds the STUN and TURN entries. TURN credentials follow
// coturn's REST scheme: the username is "<expiry unix>:<user id>" and the
// credential is the base64 HMAC-SHA1 of that username under the shared
// secret, which coturn recomputes rather than storing per-user passwords.
func (m *Module) iceServers(userID string, now time.Time) ([]ICEServer, time.Duration) {
	out := []ICEServer{}
	if len(m.Config.STUNURLs) > 0 {
		out = append(out, ICEServer{URLs: slices.Clone(m.Config.STUNURLs)})
	}
	ttl := m.Config.TURNTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if len(m.Config.TURNURLs) > 0 && m.Config.TURNSecret != "" {
		username := strconv.FormatInt(now.Add(ttl).Unix(), 10) + ":" + userID
		mac := hmac.New(sha1.New, []byte(m.Config.TURNSecret))
		mac.Write([]byte(username))
		out = append(out, ICEServer{
			URLs:       slices.Clone(m.Config.TURNURLs),
			Username:   username,
			Credential: base64.StdEncoding.EncodeToString(mac.Sum(nil)),
		})
	}
	return out, ttl
}

// Join puts a player in a room's voice channel and returns everyone in it,
// including the caller. The caller must already be a member of the room.
func (m *Module) Join(ctx context.Context, roomID, actor, owner string) ([]Participant, error) {
	if !m.Config.Enabled {
		return nil, &Error{"VOICE_DISABLED", 503, "Voice chat is switched off on this server"}
	}
	ok, err := store.New(m.DB).IsMember(ctx, store.IsMemberParams{RoomID: roomID, UserID: actor})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if !ok {
		return nil, &Error{"NOT_FOUND", 404, "Room not found"}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rooms == nil {
		m.rooms = map[string]map[string]entry{}
	}
	channel, exists := m.rooms[roomID]
	if !exists {
		channel = map[string]entry{}
		m.rooms[roomID] = channel
	}
	was, already := channel[actor]
	if !already && len(channel) >= MaxParticipants {
		return nil, &Error{"VOICE_FULL", 409, "This room's voice channel is full"}
	}
	if !already {
		was = entry{Participant: Participant{UserID: actor}}
	}
	was.owner = owner
	channel[actor] = was
	return participants(channel), nil
}

// Leave removes a player, but only if owner still holds the membership. An
// empty owner removes it whoever holds it.
func (m *Module) Leave(roomID, actor, owner string) []Participant {
	m.mu.Lock()
	defer m.mu.Unlock()
	channel, ok := m.rooms[roomID]
	if !ok {
		return nil
	}
	held, in := channel[actor]
	if !in || (owner != "" && held.owner != owner) {
		return nil
	}
	delete(channel, actor)
	if len(channel) == 0 {
		delete(m.rooms, roomID)
		return []Participant{}
	}
	return participants(channel)
}

// SetState records a player's own mute, deafen and speaking flags so the
// others can show them. A player who is not in the channel is ignored.
func (m *Module) SetState(roomID, actor string, muted, deafened, speaking bool) ([]Participant, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	channel, ok := m.rooms[roomID]
	if !ok {
		return nil, false
	}
	p, in := channel[actor]
	if !in {
		return nil, false
	}
	p.Muted, p.Deafened, p.Speaking = muted, deafened, speaking
	channel[actor] = p
	return participants(channel), true
}

// InChannel reports whether a user is currently in a room's voice channel.
// Signalling is relayed only between two players who both are.
func (m *Module) InChannel(roomID, userID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.rooms[roomID][userID]
	return ok
}

// Participants returns a room's channel as everyone sees it.
func (m *Module) Participants(roomID string) []Participant {
	m.mu.Lock()
	defer m.mu.Unlock()
	return participants(m.rooms[roomID])
}

// participants returns the channel as a stable, sorted slice. The caller
// holds the lock.
func participants(channel map[string]entry) []Participant {
	out := make([]Participant, 0, len(channel))
	for _, p := range channel {
		out = append(out, p.Participant)
	}
	slices.SortFunc(out, func(a, b Participant) int {
		switch {
		case a.UserID < b.UserID:
			return -1
		case a.UserID > b.UserID:
			return 1
		}
		return 0
	})
	return out
}

// Policy asks the running game whether players in this room may transmit
// right now. A room with no match, or a game with no opinion, is open.
func (m *Module) Policy(ctx context.Context, roomID string) (game.VoicePolicyState, error) {
	if m.Gate == nil {
		return game.VoicePolicyState{Open: true}, nil
	}
	return m.Gate.VoiceOpen(ctx, store.New(m.DB), roomID)
}
