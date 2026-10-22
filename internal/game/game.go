// Package game defines the boundary between platform services and game rules.
// Raw state is server-only. Transports may publish only a Game-produced View.
package game

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"
)

var ErrNotReady = errors.New("game implementation is not available in the foundation release")

type Descriptor struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	RulesVersion string `json:"rules_version"`
	MinPlayers   int    `json:"min_players"`
	MaxPlayers   int    `json:"max_players"`
	Playable     bool   `json:"playable"`
}
type Participant struct {
	UserID string
	Seat   int
}
type Setup struct {
	Participants []Participant
	Random       io.Reader
}
type State struct {
	SchemaVersion int
	Data          json.RawMessage
}
type Command struct {
	ID               string          `json:"id"`
	ExpectedRevision int64           `json:"expected_revision"`
	Kind             string          `json:"kind"`
	Payload          json.RawMessage `json:"payload"`
}
type Event struct {
	Kind           string
	Public         bool
	AudienceUserID string
	Payload        json.RawMessage
}
type Outcome struct {
	Finished     bool
	WinnerUserID string
}
type Transition struct {
	State   State
	Events  []Event
	Outcome Outcome
}

// View intentionally has no state/deck/seed field. Each engine builds its own
// allowlisted public and requesting-player payload. Never marshal State directly.
type View struct {
	Public       json.RawMessage `json:"public"`
	Self         json.RawMessage `json:"self"`
	LegalActions []string        `json:"legal_actions"`
}
type Game interface {
	Descriptor() Descriptor
	New(context.Context, Setup) (State, error)
	Validate(State) error
	Apply(context.Context, State, string, Command) (Transition, error)
	View(State, string) (View, error)
}

// Rejection is a rule-level refusal of a command. Apply returns one when the
// command is illegal in the current state; the state is unchanged, and the
// platform records the refusal so a retried command ID gets the same answer.
type Rejection interface {
	error
	RejectionCode() string
}

// TimeoutMove is a game's default command for one awaited user. The platform
// fills in the command ID and expected revision.
type TimeoutMove struct {
	UserID  string
	Command Command
}

// TimeoutPolicy is implemented by games that can move for players who have
// not acted within the platform's turn timeout. TimeoutMoves returns one
// default move per user the game is currently waiting on. Each move still
// goes through Apply like any other command.
type TimeoutPolicy interface {
	TimeoutMoves(State) ([]TimeoutMove, error)
}

// ChatPolicy is implemented by games that must close room chat during part
// of a match, so players cannot pass information the rules hide. The platform
// asks the running game before accepting a message; a game that does not
// implement it leaves chat open.
type ChatPolicy interface {
	ChatOpen(State) (bool, error)
}

// nowKey carries the server's clock into a game. A game that needs a
// deadline -- Bluff's challenge window -- reads it instead of calling
// time.Now itself, so the platform decides what "now" means and the engine
// stays something a test can drive.
type nowKey struct{}

// WithNow stamps the time a command is being applied at.
func WithNow(ctx context.Context, t time.Time) context.Context {
	return context.WithValue(ctx, nowKey{}, t)
}

// Now reads the stamped time, falling back to the real clock so a caller
// that forgets is late rather than wrong.
func Now(ctx context.Context) time.Time {
	if t, ok := ctx.Value(nowKey{}).(time.Time); ok {
		return t
	}
	return time.Now().UTC()
}

// Paced is implemented by games whose wait has a deadline of its own, kept
// in the state and shorter than the platform's turn timeout. The platform
// sweeps expired deadlines and then asks TimeoutPolicy what to do about
// them, so a game that implements this implements that as well.
type Paced interface {
	// Deadline reports when the current wait expires. false means this
	// state is not waiting on a clock.
	Deadline(State) (time.Time, bool)
}

// VoicePolicyState is a game's answer about voice chat. Reason is shown to
// players while voice is closed, so a silenced microphone is never a mystery.
type VoicePolicyState struct {
	Open   bool   `json:"open"`
	Reason string `json:"reason,omitempty"`
}

// VoicePolicy is implemented by games that must close voice chat during part
// of a match, for the same reason they close text chat: so players cannot
// pass information the rules hide. The platform asks the running game before
// relaying any signalling and before letting a player transmit; a game that
// does not implement it leaves voice open.
type VoicePolicy interface {
	VoiceOpen(State) (VoicePolicyState, error)
}

// CardCatalog is implemented by games whose public card metadata clients need.
type CardCatalog interface {
	Cards() any
}

type Registry struct{ games map[string]Game }

func NewRegistry(games ...Game) *Registry {
	r := &Registry{games: map[string]Game{}}
	for _, g := range games {
		d := g.Descriptor()
		key := d.ID + ":" + d.RulesVersion
		if _, ok := r.games[key]; ok {
			panic("duplicate game rules version")
		}
		r.games[key] = g
	}
	return r
}
func (r *Registry) Get(id, version string) (Game, bool) {
	g, ok := r.games[id+":"+version]
	return g, ok
}

// Find returns a registered game by ID, for version-independent metadata.
func (r *Registry) Find(id string) (Game, bool) {
	for _, g := range r.games {
		if g.Descriptor().ID == id {
			return g, true
		}
	}
	return nil, false
}
func (r *Registry) Catalog() []Descriptor {
	out := []Descriptor{}
	for _, g := range r.games {
		out = append(out, g.Descriptor())
	}
	return out
}
