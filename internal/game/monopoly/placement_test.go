package monopoly

import "testing"

// Owner decision 2026-10-01: a card played or received without a chosen set
// joins the fullest incomplete set of its color; a new set starts only when
// every set of that color is full or there is none.
func TestPlacementJoinsOpenSet(t *testing.T) {
	s := fixture(t,
		seat{
			Hand: []CardID{redCards[1], wild(Red, Yellow, 1), brownCards[1]},
			Sets: []PropertySet{set("r", Red, redCards[0]), set("b", Brown, brownCards[0])},
		},
		seat{},
	)
	s = ok(t, s, 0, PlayProperty{Card: redCards[1]})
	s = ok(t, s, 0, PlayProperty{Card: wild(Red, Yellow, 1), Color: Red})
	if len(s.Players[0].Sets) != 2 || len(s.Players[0].Sets[0].Cards) != 3 {
		t.Fatalf("both cards should join the red set: %+v", s.Players[0].Sets)
	}

	full := fixture(t,
		seat{Hand: []CardID{brownCards[1]}, Sets: []PropertySet{set("b", Brown, brownCards[0])}},
		seat{},
	)
	full = ok(t, full, 0, PlayProperty{Card: brownCards[1]})
	if full.Players[0].Sets[0].ID != "b" || len(full.Players[0].Sets) != 1 {
		t.Fatalf("brown should complete the existing set: %+v", full.Players[0].Sets)
	}

	again := fixture(t,
		seat{Hand: []CardID{wild(LightBlue, Brown, 1)}, Sets: []PropertySet{set("b", Brown, brownCards...)}},
		seat{},
	)
	again = ok(t, again, 0, PlayProperty{Card: wild(LightBlue, Brown, 1), Color: Brown})
	if len(again.Players[0].Sets) != 2 {
		t.Fatal("a full set leaves only a new set")
	}
}

// A received multicolor wild waits for a choice only when it has a set to
// join; otherwise it goes straight to unassigned.
func TestReceivedMulticolorWild(t *testing.T) {
	victim := seat{Unassigned: []CardID{rainbow(1)}}
	s := fixture(t, seat{Hand: []CardID{act(SlyDeal, 1)}, Sets: []PropertySet{set("r", Red, redCards[0])}}, victim)
	s = ok(t, s, 0, PlaySlyDeal{Card: act(SlyDeal, 1), Target: 1, Take: rainbow(1)})
	s = ok(t, s, 1, accept(s))
	if s.Phase != PhasePlacement || !has(s.Players[0].Incoming, rainbow(1)) {
		t.Fatal("with an open set the owner chooses")
	}
	s = ok(t, s, 0, PlaceReceived{Card: rainbow(1), Set: "r"})
	if s.Phase != PhasePlay || len(s.Players[0].Sets[0].Cards) != 2 {
		t.Fatal("wild should join the red set")
	}
}
