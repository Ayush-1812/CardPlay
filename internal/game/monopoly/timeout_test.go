package monopoly

import (
	"bytes"
	"math/rand/v2"
	"slices"
	"testing"
)

// Across many random games, whenever the awaited seat times out the default
// move is legal, keeps every invariant, and every phase is exercised.
func TestTimeoutActionIsAlwaysLegal(t *testing.T) {
	byPhase := map[Phase]int{}
	for g := range 200 {
		seed := bytes.Repeat([]byte{byte(g), byte(g >> 8), 3}, SeedSize/3+1)[:SeedSize]
		players := []string{"a", "b", "c", "d", "e"}[:2+g%4]
		s, _, err := NewGame(players, bytes.NewReader(seed))
		if err != nil {
			t.Fatal(err)
		}
		b := bot{rand.New(rand.NewPCG(uint64(g), 7))}
		for range 1500 {
			if s.Phase == PhaseFinished {
				break
			}
			waiting := s.WaitingFor()
			seat := waiting[b.r.IntN(len(waiting))]
			if b.r.IntN(3) == 0 {
				a, ok := s.TimeoutAction(seat)
				if !ok {
					t.Fatalf("game %d: seat %d is awaited in %s but has no timeout move", g, seat, s.Phase)
				}
				next, _, err := Apply(s, seat, a)
				if err != nil {
					t.Fatalf("game %d: timeout %s in %s rejected: %v", g, a.Kind(), s.Phase, err)
				}
				if err := next.CheckInvariants(); err != nil {
					t.Fatalf("game %d: %v", g, err)
				}
				byPhase[s.Phase]++
				s = next
				continue
			}
			if next, _, err := Apply(s, seat, b.propose(s, seat)); err == nil {
				s = next
			}
		}
	}
	for _, p := range []Phase{PhasePlay, PhaseResponse, PhasePayment, PhasePlacement} {
		if byPhase[p] == 0 {
			t.Errorf("no timeout exercised in phase %s", p)
		}
	}
	t.Logf("timeouts by phase: %v", byPhase)
}

func TestTimeoutActionChoices(t *testing.T) {
	t.Run("only the awaited seat has a timeout move", func(t *testing.T) {
		s := fixture(t, seat{Hand: []CardID{money(1, 1)}}, seat{})
		if _, ok := s.TimeoutAction(1); ok {
			t.Fatal("seat 1 is not awaited")
		}
	})

	t.Run("play phase ends the turn returning the lowest-value excess", func(t *testing.T) {
		hand := []CardID{money(10, 1), money(5, 1), money(1, 1), money(4, 1), money(3, 1), money(2, 1), money(1, 2), money(1, 3), money(5, 2)}
		s := fixture(t, seat{Hand: hand}, seat{})
		a, _ := s.TimeoutAction(0)
		end, isEnd := a.(EndTurn)
		if !isEnd || len(end.Return) != 2 || totalValue(end.Return) != 2 {
			t.Fatalf("got %#v", a)
		}
		next := ok(t, s, 0, a)
		if next.Active != 1 || len(next.Players[0].Hand) != HandLimit {
			t.Fatalf("turn did not pass correctly")
		}
	})

	t.Run("a charged player accepts, then pays from the bank first, lowest value first", func(t *testing.T) {
		s := fixture(t,
			seat{Hand: []CardID{act(DebtCollector, 1)}},
			seat{Bank: []CardID{money(1, 1), money(2, 1), money(10, 1)}, Sets: []PropertySet{set("s1", Brown, brownCards...)}},
		)
		s = ok(t, s, 0, PlayDebtCollector{Card: act(DebtCollector, 1), Target: 1})
		a, _ := s.TimeoutAction(1)
		if _, isAccept := a.(Accept); !isAccept {
			t.Fatalf("expected accept, got %#v", a)
		}
		s = ok(t, s, 1, a)
		a, _ = s.TimeoutAction(1)
		p, isPay := a.(Pay)
		// Owes 5M: 1M + 2M is not enough, so 10M is added; the brown set stays.
		if !isPay || !slices.Equal(p.Cards, []CardID{money(1, 1), money(2, 1), money(10, 1)}) {
			t.Fatalf("got %#v", a)
		}
		s = ok(t, s, 1, a)
		if len(s.Players[1].Sets) != 1 || len(s.Players[1].Bank) != 0 {
			t.Fatal("properties should be kept when money covers the debt")
		}
	})

	t.Run("with too little value the payer gives everything", func(t *testing.T) {
		s := fixture(t,
			seat{Hand: []CardID{act(DebtCollector, 1)}},
			seat{Bank: []CardID{money(1, 1)}, Sets: []PropertySet{set("s1", Brown, prop("baltic-avenue"))}},
		)
		s = ok(t, s, 0, PlayDebtCollector{Card: act(DebtCollector, 1), Target: 1})
		s = ok(t, s, 1, accept(s))
		a, _ := s.TimeoutAction(1)
		if p := a.(Pay); len(p.Cards) != 2 {
			t.Fatalf("got %#v", a)
		}
		s = ok(t, s, 1, a)
		// The collector must now place the received property.
		if s.Phase != PhasePlacement {
			t.Fatalf("phase %s", s.Phase)
		}
		a, _ = s.TimeoutAction(0)
		if pr := a.(PlaceReceived); pr.Card != prop("baltic-avenue") || pr.Color != Brown {
			t.Fatalf("got %#v", a)
		}
		ok(t, s, 0, a)
	})

	t.Run("received cards join an incomplete set of a legal color; multicolor wilds stay unassigned", func(t *testing.T) {
		s := fixture(t,
			seat{Hand: []CardID{act(SlyDeal, 1), act(SlyDeal, 2)}, Sets: []PropertySet{set("s1", Brown, prop("mediterranean-avenue"))}},
			seat{Sets: []PropertySet{set("s2", Brown, prop("baltic-avenue"))}, Unassigned: []CardID{rainbow(1)}},
		)
		for i, take := range []CardID{prop("baltic-avenue"), rainbow(1)} {
			s = ok(t, s, 0, PlaySlyDeal{Card: act(SlyDeal, i+1), Target: 1, Take: take})
			s = ok(t, s, 1, accept(s))
			if s.Phase != PhasePlacement {
				t.Fatalf("phase %s", s.Phase)
			}
			a, _ := s.TimeoutAction(0)
			pr := a.(PlaceReceived)
			if i == 0 && pr.Set != "s1" {
				t.Fatalf("property should join the incomplete brown set: %#v", a)
			}
			if i == 1 && (pr.Card != rainbow(1) || pr.Set != "" || pr.Color != "") {
				t.Fatalf("multicolor wild should stay unassigned: %#v", a)
			}
			s = ok(t, s, 0, a)
		}
		if s.Phase != PhasePlay || len(s.Players[0].Unassigned) != 1 || s.Players[0].CompleteColors() != 1 {
			t.Fatal("placement did not finish as expected")
		}
	})

	t.Run("the module reports the awaited user and a decodable command", func(t *testing.T) {
		s := fixture(t, seat{Hand: []CardID{money(1, 1)}}, seat{})
		st, err := encode(s)
		if err != nil {
			t.Fatal(err)
		}
		moves, err := Module{}.TimeoutMoves(st)
		if err != nil || len(moves) != 1 || moves[0].UserID != "user-0" || moves[0].Command.Kind != "end_turn" {
			t.Fatalf("%#v %v", moves, err)
		}
		if _, err := DecodeAction(moves[0].Command.Kind, moves[0].Command.Payload); err != nil {
			t.Fatal(err)
		}
	})
}
