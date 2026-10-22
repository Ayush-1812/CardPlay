package bluff

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"cardplay/internal/game"
)

// Module implements game.Game for Bluff. The engine is pure; this adapter
// is the only part that knows a platform exists.
type Module struct{}

// Descriptor reports the game and its pinned rules version.
func (Module) Descriptor() game.Descriptor {
	return game.Descriptor{
		ID: "bluff", Name: "Bluff", RulesVersion: "bluff-v1",
		MinPlayers: MinPlayers, MaxPlayers: MaxPlayers, Playable: true,
	}
}

// Cards returns the public 52-card manifest; it holds no hidden state.
func (Module) Cards() any { return Manifest() }

// New seats the players, shuffles a fresh deck and deals it out.
func (Module) New(_ context.Context, setup game.Setup) (game.State, error) {
	seats := slices.Clone(setup.Participants)
	slices.SortFunc(seats, func(a, b game.Participant) int { return a.Seat - b.Seat })
	var users []string
	for _, p := range seats {
		users = append(users, p.UserID)
	}
	if setup.Random == nil {
		return game.State{}, errors.New("setup needs a random source")
	}
	s, _, err := NewGame(users, decksFrom(setup.Options), setup.Random)
	if err != nil {
		return game.State{}, err
	}
	return encode(s)
}

// decksFrom reads how many decks the room asked for. Anything missing or
// out of range falls back to one, so a room that has never set it plays the
// ordinary game rather than refusing to start.
func decksFrom(options json.RawMessage) int {
	if len(options) == 0 {
		return 1
	}
	var in struct {
		Decks int `json:"decks"`
	}
	if json.Unmarshal(options, &in) != nil || in.Decks < 1 || in.Decks > MaxDecks {
		return 1
	}
	return in.Decks
}

// Validate decodes persisted state and checks every invariant.
func (Module) Validate(st game.State) error {
	_, err := decode(st)
	return err
}

// Apply runs one command for actorUserID at the time the platform stamped
// on the context. Rejections are *RuleError values and leave the state
// unchanged.
func (Module) Apply(ctx context.Context, st game.State, actorUserID string, cmd game.Command) (game.Transition, error) {
	s, err := decode(st)
	if err != nil {
		return game.Transition{}, err
	}
	seat := s.seatOf(actorUserID)
	if seat < 0 {
		return game.Transition{}, reject(CodeNotYourTurn, "you are not seated in this match")
	}
	action, err := DecodeAction(cmd.Kind, cmd.Payload)
	if err != nil {
		return game.Transition{}, err
	}
	next, events, err := Apply(s, seat, action, game.Now(ctx))
	if err != nil {
		return game.Transition{}, err
	}
	state, err := encode(next)
	if err != nil {
		return game.Transition{}, err
	}
	out := game.Transition{State: state, Events: convert(next, events)}
	if next.Phase == PhaseOver {
		out.Outcome = game.Outcome{Finished: true, WinnerUserID: next.Players[next.Winner].UserID}
	}
	return out, nil
}

// View projects the state for one viewer.
func (Module) View(st game.State, viewerUserID string) (game.View, error) {
	s, err := decode(st)
	if err != nil {
		return game.View{}, err
	}
	return s.Project(s.seatOf(viewerUserID))
}

// Deadline implements game.Paced: during a challenge window the table is
// waiting on a five-second clock, not on the platform's turn timeout.
func (Module) Deadline(st game.State) (time.Time, bool) {
	s, err := decode(st)
	if err != nil || s.Phase != PhaseChallenge || s.Deadline.IsZero() {
		return time.Time{}, false
	}
	return s.Deadline, true
}

// TimeoutMoves implements game.TimeoutPolicy. When a challenge window has
// run out, the one move to make is closing it, which is what silence
// means. It is attributed to the player who made the play, because it is
// their play being accepted; no card is ever played for anybody.
func (Module) TimeoutMoves(st game.State) ([]game.TimeoutMove, error) {
	s, err := decode(st)
	if err != nil {
		return nil, err
	}
	if s.Phase != PhaseChallenge || s.Last == nil {
		return nil, nil
	}
	payload, err := json.Marshal(CloseWindow{PlayID: s.Last.ID})
	if err != nil {
		return nil, err
	}
	return []game.TimeoutMove{{
		UserID:  s.Players[s.Last.Seat].UserID,
		Command: game.Command{Kind: "close_window", Payload: payload},
	}}, nil
}

// DecodeAction turns a command into an action, refusing anything else.
func DecodeAction(kind string, payload json.RawMessage) (Action, error) {
	var into Action
	switch kind {
	case "play_cards":
		var a PlayCards
		if err := unmarshal(payload, &a); err != nil {
			return nil, err
		}
		into = a
	case "accept_play":
		var a AcceptPlay
		if err := unmarshal(payload, &a); err != nil {
			return nil, err
		}
		into = a
	case "call_bluff":
		var a CallBluff
		if err := unmarshal(payload, &a); err != nil {
			return nil, err
		}
		into = a
	case "close_window":
		var a CloseWindow
		if err := unmarshal(payload, &a); err != nil {
			return nil, err
		}
		into = a
	default:
		return nil, reject(CodeInvalidInput, "unknown action %q", kind)
	}
	return into, nil
}

func unmarshal(payload json.RawMessage, into any) error {
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	if err := json.Unmarshal(payload, into); err != nil {
		return reject(CodeInvalidInput, "malformed action payload")
	}
	return nil
}

// convert maps engine events onto the platform's envelope.
func convert(s *State, events []Event) []game.Event {
	out := make([]game.Event, 0, len(events))
	for _, e := range events {
		data, err := json.Marshal(e.Data)
		if err != nil {
			continue
		}
		ev := game.Event{Kind: e.Kind, Payload: data, Public: e.Audience == Public}
		if e.Audience != Public && e.Audience >= 0 && e.Audience < len(s.Players) {
			ev.AudienceUserID = s.Players[e.Audience].UserID
		}
		out = append(out, ev)
	}
	return out
}

func encode(s *State) (game.State, error) {
	data, err := json.Marshal(s)
	return game.State{SchemaVersion: SchemaVersion, Data: data}, err
}

func decode(st game.State) (*State, error) {
	if st.SchemaVersion != SchemaVersion {
		return nil, errors.New("unsupported bluff state version")
	}
	var s State
	if err := json.Unmarshal(st.Data, &s); err != nil {
		return nil, err
	}
	if err := s.CheckInvariants(); err != nil {
		return nil, err
	}
	return &s, nil
}
