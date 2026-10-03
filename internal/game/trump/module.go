package trump

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"cardplay/internal/game"
)

// Module implements game.Game for Trump. The engine itself is pure; this
// adapter is the only place that knows about the platform.
type Module struct{}

// Descriptor reports the game and its pinned rules version.
func (Module) Descriptor() game.Descriptor {
	return game.Descriptor{ID: "trump", Name: "Trump", RulesVersion: "trump-v1", MinPlayers: Seats, MaxPlayers: Seats, Playable: true}
}

// Cards returns the public 52-card manifest; it holds no hidden state.
func (Module) Cards() any { return Manifest() }

// New seats four players, tosses for the first trump choice and deals five
// cards each. Setup.Random must be crypto/rand in production.
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
	s, _, err := NewGame(users, setup.Random)
	if err != nil {
		return game.State{}, err
	}
	return encode(s)
}

// Validate decodes persisted state and checks every invariant.
func (Module) Validate(st game.State) error {
	_, err := decode(st)
	return err
}

// Apply runs one command for actorUserID. Rejections are *RuleError values
// and leave the state unchanged.
func (Module) Apply(_ context.Context, st game.State, actorUserID string, cmd game.Command) (game.Transition, error) {
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
	next, events, err := Apply(s, seat, action)
	if err != nil {
		return game.Transition{}, err
	}
	encoded, err := encode(next)
	if err != nil {
		return game.Transition{}, err
	}
	out := game.Transition{State: encoded}
	for _, e := range events {
		payload, err := json.Marshal(e.Data)
		if err != nil {
			return game.Transition{}, err
		}
		ge := game.Event{Kind: e.Kind, Public: e.Audience == Public, Payload: payload}
		if e.Audience != Public {
			ge.AudienceUserID = next.Players[e.Audience].UserID
		}
		out.Events = append(out.Events, ge)
	}
	// Trump has rounds but no session victory condition, so a match never
	// reports a winner; it ends only when players leave (spec §1).
	return out, nil
}

// View projects the state for one seated user.
func (Module) View(st game.State, userID string) (game.View, error) {
	s, err := decode(st)
	if err != nil {
		return game.View{}, err
	}
	seat := s.seatOf(userID)
	if seat < 0 {
		return game.View{}, errors.New("user is not seated in this match")
	}
	v := s.ViewFor(seat)
	public, err := json.Marshal(v.Public)
	if err != nil {
		return game.View{}, err
	}
	self, err := json.Marshal(v.Self)
	if err != nil {
		return game.View{}, err
	}
	return game.View{Public: public, Self: self, LegalActions: v.LegalActions}, nil
}

// Trump deliberately does NOT implement game.TimeoutPolicy. No automatic
// move, forfeit or timeout is applied to a Trump match without an owner
// policy (owner instruction 2026-10-03). The engine can still compute the
// most passive legal move -- State.TimeoutAction -- so turning a policy on
// later is a few lines here, not a redesign.
//
// ChatOpen implements the platform's chat gate: chat is closed while the
// trump is being chosen (spec rule 20).
func (Module) ChatOpen(st game.State) (bool, error) {
	s, err := decode(st)
	if err != nil {
		return true, err
	}
	return s.ChatOpen(), nil
}

func encode(s *State) (game.State, error) {
	data, err := json.Marshal(s)
	return game.State{SchemaVersion: SchemaVersion, Data: data}, err
}

func decode(st game.State) (*State, error) {
	if st.SchemaVersion != SchemaVersion {
		return nil, errors.New("unsupported trump state schema")
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
