package monopoly

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
)

// Card ID shorthands.
func money(value, n int) CardID      { return CardID(fmt.Sprintf("money-%dm-%d", value, n)) }
func prop(name string) CardID        { return CardID("property-" + name) }
func act(a ActionType, n int) CardID { return CardID(fmt.Sprintf("action-%s-%d", a, n)) }
func wild(a, b Color, n int) CardID {
	return CardID(fmt.Sprintf("wild-%s-%s-%d", a, b, n))
}
func rainbow(n int) CardID           { return CardID(fmt.Sprintf("wild-multicolor-%d", n)) }
func rent2(a, b Color, n int) CardID { return CardID(fmt.Sprintf("rent-%s-%s-%d", a, b, n)) }
func rentAny(n int) CardID           { return CardID(fmt.Sprintf("rent-multicolor-%d", n)) }

// Frequently used complete sets.
var (
	brownCards    = []CardID{prop("mediterranean-avenue"), prop("baltic-avenue")}
	darkBlueCards = []CardID{prop("park-place"), prop("boardwalk")}
	utilityCards  = []CardID{prop("electric-company"), prop("water-works")}
	greenCards    = []CardID{prop("pacific-avenue"), prop("north-carolina-avenue"), prop("pennsylvania-avenue")}
	orangeCards   = []CardID{prop("st-james-place"), prop("tennessee-avenue"), prop("new-york-avenue")}
	redCards      = []CardID{prop("kentucky-avenue"), prop("indiana-avenue"), prop("illinois-avenue")}
)

func set(id string, color Color, cards ...CardID) PropertySet {
	return PropertySet{ID: id, Color: color, Cards: cards}
}

// seat describes one player's zones in a fixture.
type seat struct {
	Hand, Bank, Unassigned, Detached, Incoming []CardID
	Sets                                       []PropertySet
}

// fixture builds a legal mid-game state: seat 0 active in its play phase,
// every unused card in the draw pile in manifest order.
func fixture(t *testing.T, seats ...seat) *State {
	t.Helper()
	s := &State{Schema: SchemaVersion, Seed: bytes.Repeat([]byte{7}, SeedSize), Phase: PhasePlay, Turn: 1, NextSetID: 100}
	used := map[CardID]bool{}
	mark := func(ids []CardID) []CardID {
		for _, id := range ids {
			used[id] = true
		}
		if ids == nil {
			return []CardID{}
		}
		return slices.Clone(ids)
	}
	for i, sp := range seats {
		p := Player{UserID: fmt.Sprintf("user-%d", i), Hand: mark(sp.Hand), Bank: mark(sp.Bank), Unassigned: mark(sp.Unassigned), Detached: mark(sp.Detached), Incoming: mark(sp.Incoming), Sets: []PropertySet{}}
		for _, st := range sp.Sets {
			st.Cards = mark(st.Cards)
			mark([]CardID{st.House, st.Hotel})
			p.Sets = append(p.Sets, st)
		}
		s.Players = append(s.Players, p)
	}
	delete(used, "")
	for _, c := range manifest {
		if !used[c.ID] {
			s.Draw = append(s.Draw, c.ID)
		}
	}
	if err := s.CheckInvariants(); err != nil {
		t.Fatalf("fixture is illegal: %v", err)
	}
	return s
}

func snapshot(t *testing.T, s *State) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ok applies an action that must succeed.
func ok(t *testing.T, s *State, seat int, a Action) *State {
	t.Helper()
	next, _, err := Apply(s, seat, a)
	if err != nil {
		t.Fatalf("seat %d %s rejected: %v", seat, a.Kind(), err)
	}
	return next
}

// rejected applies an action that must fail with code and change nothing.
func rejected(t *testing.T, s *State, seat int, a Action, code string) {
	t.Helper()
	before := snapshot(t, s)
	next, events, err := Apply(s, seat, a)
	var re *RuleError
	if !errors.As(err, &re) {
		t.Fatalf("seat %d %s: want %s, got %v", seat, a.Kind(), code, err)
	}
	if re.Code != code {
		t.Fatalf("seat %d %s: want %s, got %s (%s)", seat, a.Kind(), code, re.Code, re.Message)
	}
	if next != s || events != nil || snapshot(t, s) != before {
		t.Fatalf("seat %d %s: rejected action changed state", seat, a.Kind())
	}
}

// respond helpers read the live pending id and step.
func accept(s *State) Accept { return Accept{Pending: s.Pending.ID, Step: s.Pending.Step} }
func jsn(s *State, card CardID, component string) PlayJustSayNo {
	return PlayJustSayNo{Pending: s.Pending.ID, Step: s.Pending.Step, Card: card, Component: component}
}
func pay(s *State, cards ...CardID) Pay {
	return Pay{Pending: s.Pending.ID, Step: s.Pending.Step, Cards: cards}
}

func intp(v int) *int { return &v }

func has(list []CardID, id CardID) bool { return slices.Contains(list, id) }

func totalValue(ids []CardID) int {
	v := 0
	for _, id := range ids {
		v += mustCard(id).Value
	}
	return v
}
