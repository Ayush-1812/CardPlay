// Package monopoly is the authoritative Monopoly Deal (US 01723) rules engine.
// It is pure: no HTTP, WebSocket, database or UI code. Module adapts it to
// the platform's game.Game boundary.
package monopoly

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"cardplay/internal/game"
)

// Module implements game.Game for rules version us-01723-v1.
type Module struct{}

// Descriptor stays unplayable until matches are wired to the platform
// (roadmap M3: durable commands, per-seat projections). The engine is complete.
func (Module) Descriptor() game.Descriptor {
	return game.Descriptor{ID: "monopoly-deal", Name: "Monopoly Deal", RulesVersion: "us-01723-v1", MinPlayers: 2, MaxPlayers: 5, Playable: false}
}

// New deals a game. Setup.Random must be crypto/rand in production.
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
	if next.Winner != nil {
		out.Outcome = game.Outcome{Finished: true, WinnerUserID: next.Players[*next.Winner].UserID}
	}
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

func (s *State) seatOf(userID string) int {
	for i, p := range s.Players {
		if p.UserID == userID {
			return i
		}
	}
	return -1
}

func encode(s *State) (game.State, error) {
	data, err := json.Marshal(s)
	return game.State{SchemaVersion: SchemaVersion, Data: data}, err
}

func decode(st game.State) (*State, error) {
	if st.SchemaVersion != SchemaVersion {
		return nil, errors.New("unsupported monopoly state schema")
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

var _ game.Game = Module{}
