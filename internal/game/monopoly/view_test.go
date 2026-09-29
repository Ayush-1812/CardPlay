package monopoly

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"cardplay/internal/game"
)

// Each seat sees its own hand; opponents get counts only. Draw order, the
// seed and other hands never appear in a projection (acceptance N01).
func TestViewPrivacy(t *testing.T) {
	s, _, err := NewGame([]string{"a", "b", "c"}, bytes.NewReader(bytes.Repeat([]byte{5}, SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	for seat := range s.Players {
		v := s.ViewFor(seat)
		raw, _ := json.Marshal(v)
		text := string(raw)
		if !slices.Equal(v.Self.Hand, s.Players[seat].Hand) {
			t.Fatalf("seat %d does not see its own hand", seat)
		}
		for other, p := range s.Players {
			if v.Public.Players[other].HandCount != len(p.Hand) {
				t.Fatalf("hand count wrong for seat %d", other)
			}
			if other == seat {
				continue
			}
			for _, id := range p.Hand {
				if strings.Contains(text, `"`+string(id)+`"`) {
					t.Fatalf("seat %d's view leaks seat %d's card %s", seat, other, id)
				}
			}
		}
		for _, id := range s.Draw {
			if strings.Contains(text, `"`+string(id)+`"`) {
				t.Fatalf("seat %d's view leaks draw card %s", seat, id)
			}
		}
		if strings.Contains(text, `"seed"`) || strings.Contains(text, `"draw"`) {
			t.Fatal("view exposes seed or draw pile")
		}
		if v.Public.DrawCount != len(s.Draw) {
			t.Fatal("draw count missing")
		}
	}
}

// Legal-action hints never reveal an opponent's Just Say No.
func TestLegalActionsHideHoldings(t *testing.T) {
	s := fixture(t, seat{Hand: []CardID{act(DebtCollector, 1)}}, seat{Hand: []CardID{jsn1}}, seat{})
	s = ok(t, s, 0, PlayDebtCollector{Card: act(DebtCollector, 1), Target: 1})
	if got := s.ViewFor(1).LegalActions; !slices.Equal(got, []string{"accept", "just_say_no"}) {
		t.Fatalf("defender actions %v", got)
	}
	for _, seat := range []int{0, 2} {
		if got := s.ViewFor(seat).LegalActions; len(got) != 0 {
			t.Fatalf("seat %d should have no actions, got %v", seat, got)
		}
		raw, _ := json.Marshal(s.ViewFor(seat))
		if strings.Contains(string(raw), string(jsn1)) {
			t.Fatal("an unplayed Just Say No is visible to others")
		}
	}
}

// The platform adapter: new game, JSON commands, per-user events and views.
func TestModuleAdapter(t *testing.T) {
	m := Module{}
	ctx := context.Background()
	st, err := m.New(ctx, game.Setup{
		Participants: []game.Participant{{UserID: "u2", Seat: 1}, {UserID: "u1", Seat: 0}},
		Random:       bytes.NewReader(bytes.Repeat([]byte{3}, SeedSize)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(st); err != nil {
		t.Fatal(err)
	}
	s, _ := decode(st)
	active := s.Players[s.Active].UserID
	other := s.Players[1-s.Active].UserID
	if s.Players[0].UserID != "u1" {
		t.Fatal("participants must be seated by seat number")
	}
	if _, err := m.Apply(ctx, st, other, game.Command{Kind: "end_turn"}); err == nil {
		t.Fatal("out-of-turn command accepted")
	}
	if _, err := m.Apply(ctx, st, "stranger", game.Command{Kind: "end_turn"}); err == nil {
		t.Fatal("unseated user accepted")
	}
	var re *RuleError
	if _, err := m.Apply(ctx, st, active, game.Command{Kind: "end_turn", Payload: json.RawMessage(`{"surprise":1}`)}); !errors.As(err, &re) {
		t.Fatalf("unknown fields must be rejected, got %v", err)
	}
	hand := s.Players[s.Active].Hand
	payload, _ := json.Marshal(EndTurn{Return: hand[:len(hand)-HandLimit]})
	tr, err := m.Apply(ctx, st, active, game.Command{Kind: "end_turn", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range tr.Events {
		if !e.Public && e.AudienceUserID == "" {
			t.Fatalf("private event %s has no audience", e.Kind)
		}
		if e.Kind == "drew_cards" && e.AudienceUserID != other {
			t.Fatal("drawn cards must go only to the drawer")
		}
	}
	view, err := m.View(tr.State, other)
	if err != nil {
		t.Fatal(err)
	}
	var self SelfView
	if err := json.Unmarshal(view.Self, &self); err != nil || len(self.Hand) != 7 {
		t.Fatalf("self view %s", view.Self)
	}
	if _, err := m.View(tr.State, "stranger"); err == nil {
		t.Fatal("stranger got a view")
	}
	if !m.Descriptor().Playable {
		t.Fatal("the engine is wired to matches and must be playable")
	}
}
