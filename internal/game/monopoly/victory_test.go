package monopoly

import "testing"

// A22: three complete sets of different colors, on your own turn.
func TestVictory(t *testing.T) {
	two := []PropertySet{set("db", DarkBlue, darkBlueCards...), set("ut", Utility, utilityCards...)}
	cases := []struct {
		name string
		sets []PropertySet
		play PlayProperty
		hand []CardID
		win  bool
	}{
		{"third distinct color wins", append(two, set("b", Brown, brownCards[0])), PlayProperty{Card: brownCards[1], Set: "b"}, []CardID{brownCards[1]}, true},
		{"duplicate color does not count", []PropertySet{two[0], set("g1", Green, greenCards...), set("g2", Green, wild(DarkBlue, Green, 1), wild(Green, Railroad, 1))}, PlayProperty{Card: rainbow(1), Set: "g2"}, []CardID{rainbow(1)}, false},
		{"all-multicolor set does not count (Q3.1)", append(two, set("b", Brown, rainbow(1))), PlayProperty{Card: rainbow(2), Set: "b"}, []CardID{rainbow(2)}, false},
		{"railroad set counts", append(two, set("rr", Railroad, prop("reading-railroad"), prop("pennsylvania-railroad"), prop("b-and-o-railroad"))), PlayProperty{Card: prop("short-line"), Set: "rr"}, []CardID{prop("short-line")}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t, seat{Hand: tc.hand, Sets: tc.sets}, seat{Hand: []CardID{money(1, 1)}})
			s = ok(t, s, 0, tc.play)
			if (s.Winner != nil) != tc.win || (s.Phase == PhaseFinished) != tc.win {
				t.Fatalf("winner %v phase %s", s.Winner, s.Phase)
			}
			if tc.win {
				rejected(t, s, 0, EndTurn{}, CodeGameOver)
				rejected(t, s, 1, Bank{Card: money(1, 1)}, CodeGameOver)
			}
		})
	}
	t.Run("rearranging into a win", func(t *testing.T) {
		s := fixture(t, seat{Sets: append(two, set("b", Brown, brownCards[0]), set("x", LightBlue, wild(LightBlue, Brown, 1)))}, seat{})
		s = ok(t, s, 0, Rearrange{Sets: []SetLayout{
			{ID: "db", Color: DarkBlue, Cards: darkBlueCards}, {ID: "ut", Color: Utility, Cards: utilityCards},
			{ID: "b", Color: Brown, Cards: []CardID{brownCards[0], wild(LightBlue, Brown, 1)}},
		}})
		if s.Winner == nil || *s.Winner != 0 {
			t.Fatal("rearranged collection should win")
		}
	})
}

// A22: an off-turn collection waits for its owner's turn and can be broken
// first; it wins at turn start before drawing.
func TestOffTurnVictory(t *testing.T) {
	three := []PropertySet{set("db", DarkBlue, darkBlueCards...), set("ut", Utility, utilityCards...), set("b", Brown, brownCards...)}
	t.Run("waits, then wins before drawing", func(t *testing.T) {
		s := fixture(t, seat{}, seat{Sets: three, Hand: []CardID{money(1, 1)}})
		if s.Winner != nil {
			t.Fatal("no off-turn win")
		}
		s = ok(t, s, 0, EndTurn{})
		if s.Winner == nil || *s.Winner != 1 || len(s.Players[1].Hand) != 1 {
			t.Fatalf("seat 1 should win at turn start without drawing: winner %v hand %d", s.Winner, len(s.Players[1].Hand))
		}
	})
	t.Run("broken before the owner's turn", func(t *testing.T) {
		s := fixture(t, seat{Hand: []CardID{act(DealBreaker, 1)}}, seat{Sets: three})
		s = ok(t, s, 0, PlayDealBreaker{Card: act(DealBreaker, 1), Target: 1, Set: "b"})
		s = ok(t, s, 1, accept(s))
		s = ok(t, s, 0, EndTurn{})
		if s.Winner != nil {
			t.Fatal("broken collection must not win")
		}
	})
	t.Run("forced deal completing both: the active player wins", func(t *testing.T) {
		s := fixture(t,
			seat{Hand: []CardID{act(ForcedDeal, 1)}, Sets: []PropertySet{set("db", DarkBlue, darkBlueCards...), set("ut", Utility, utilityCards...), set("b", Brown, brownCards[0]), set("x", Red, redCards[0])}},
			seat{Sets: []PropertySet{set("g", Green, greenCards...), set("o", Orange, orangeCards...), set("r", Red, redCards[1], redCards[2]), set("y", Brown, brownCards[1])}},
		)
		s = ok(t, s, 0, PlayForcedDeal{Card: act(ForcedDeal, 1), Target: 1, Take: brownCards[1], Offer: redCards[0]})
		// Both received cards are single-color, so they are placed at once and
		// the win is checked after the last placement.
		s = ok(t, s, 1, accept(s))
		if s.Winner == nil || *s.Winner != 0 || s.Players[1].CompleteColors() != 3 {
			t.Fatal("active player must win; the defender's collection waits")
		}
	})
}

// A23: no winner while the action that completes the set is unresolved.
func TestNoPrematureWin(t *testing.T) {
	s := fixture(t,
		seat{Hand: []CardID{act(Birthday, 1)}, Sets: []PropertySet{set("db", DarkBlue, darkBlueCards...), set("ut", Utility, utilityCards...), set("b", Brown, brownCards[0])}},
		seat{Sets: []PropertySet{set("bb", Brown, brownCards[1])}},
		seat{Bank: []CardID{money(2, 1)}},
	)
	s = ok(t, s, 0, PlayBirthday{Card: act(Birthday, 1)})
	s = ok(t, s, 1, accept(s))
	s = ok(t, s, 1, pay(s, brownCards[1]))
	if s.Winner != nil || s.Phase != PhaseResponse {
		t.Fatal("the second defender has not responded yet")
	}
	s = ok(t, s, 2, accept(s))
	s = ok(t, s, 2, pay(s, money(2, 1)))
	if s.Winner == nil || *s.Winner != 0 {
		t.Fatal("win after the action fully resolves")
	}
}
