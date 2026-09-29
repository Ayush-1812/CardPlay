package monopoly

import (
	"slices"
	"testing"
)

// A12 and decision Q1a: single-card theft only outside complete sets; a
// detached building may be taken (W Action FAQ Q3); banks and hands never.
func TestSlyDeal(t *testing.T) {
	victim := seat{
		Hand:       []CardID{prop("reading-railroad")},
		Bank:       []CardID{money(5, 1)},
		Sets:       []PropertySet{set("full", Brown, brownCards...), set("part", Green, greenCards[0], wild(Green, Railroad, 1)), set("rain", Pink, rainbow(1))},
		Unassigned: []CardID{rainbow(2)},
		Detached:   []CardID{act(House, 1)},
	}
	s := fixture(t, seat{Hand: []CardID{act(SlyDeal, 1), act(SlyDeal, 2)}}, victim)
	cases := []struct {
		name string
		take CardID
		code string
	}{
		{"card in a complete set", brownCards[0], CodeInvalidTarget},
		{"bank card", money(5, 1), CodeInvalidTarget},
		{"hand card", prop("reading-railroad"), CodeInvalidTarget},
		{"nonexistent card", prop("boardwalk"), CodeInvalidTarget},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rejected(t, s, 0, PlaySlyDeal{Card: act(SlyDeal, 1), Target: 1, Take: tc.take}, tc.code)
		})
	}
	rejected(t, s, 0, PlaySlyDeal{Card: act(SlyDeal, 1), Target: 0, Take: greenCards[0]}, CodeInvalidTarget)
	rejected(t, s, 0, PlaySlyDeal{Card: act(DealBreaker, 1), Target: 1, Take: greenCards[0]}, CodeInvalidCard)

	for _, take := range []CardID{greenCards[0], wild(Green, Railroad, 1), rainbow(1), rainbow(2)} {
		t.Run("takes "+string(take), func(t *testing.T) {
			n := ok(t, s, 0, PlaySlyDeal{Card: act(SlyDeal, 1), Target: 1, Take: take})
			if n.Phase != PhaseResponse || !has(n.Players[1].Sets[1].Cards, greenCards[0]) {
				t.Fatal("assets must not move before the response closes")
			}
			n = ok(t, n, 1, accept(n))
			if n.Phase != PhasePlacement || !has(n.Players[0].Incoming, take) {
				t.Fatalf("stolen property must await placement: %+v", n.Players[0])
			}
			rejected(t, n, 0, EndTurn{}, CodeWrongPhase)
			color := mustCard(take).Colors
			a := PlaceReceived{Card: take}
			if len(color) > 0 {
				a.Color = color[0]
			}
			n = ok(t, n, 0, a)
			if n.Phase != PhasePlay || !has(n.Discard, act(SlyDeal, 1)) {
				t.Fatal("action did not resolve into the center pile")
			}
		})
	}
	t.Run("detached building goes to the thief's detached area", func(t *testing.T) {
		n := ok(t, s, 0, PlaySlyDeal{Card: act(SlyDeal, 1), Target: 1, Take: act(House, 1)})
		n = ok(t, n, 1, accept(n))
		if !has(n.Players[0].Detached, act(House, 1)) || n.Phase != PhasePlay {
			t.Fatal("building not received as detached")
		}
	})
}

// A13 and decisions Q1a / Q1a-F2: an atomic swap of two properties that are
// both outside complete sets; the offer cannot be a building.
func TestForcedDeal(t *testing.T) {
	actor := seat{
		Hand:     []CardID{act(ForcedDeal, 1)},
		Sets:     []PropertySet{set("mine-full", DarkBlue, darkBlueCards...), set("mine", Red, redCards[0])},
		Detached: []CardID{act(Hotel, 1)},
	}
	victim := seat{Sets: []PropertySet{set("theirs-full", Brown, brownCards...), set("theirs", Orange, orangeCards[0])}, Detached: []CardID{act(House, 1)}}
	s := fixture(t, actor, victim)
	cases := []struct {
		name        string
		take, offer CardID
		code        string
	}{
		{"take from a complete set", brownCards[0], redCards[0], CodeInvalidTarget},
		{"offer from own complete set (Q1a)", orangeCards[0], darkBlueCards[0], CodeInvalidTarget},
		{"offer a detached building (Q1a-F2)", orangeCards[0], act(Hotel, 1), CodeInvalidTarget},
		{"offer someone else's card", orangeCards[0], orangeCards[0], CodeInvalidTarget},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rejected(t, s, 0, PlayForcedDeal{Card: act(ForcedDeal, 1), Target: 1, Take: tc.take, Offer: tc.offer}, tc.code)
		})
	}
	t.Run("swap needs placement by both players", func(t *testing.T) {
		n := ok(t, s, 0, PlayForcedDeal{Card: act(ForcedDeal, 1), Target: 1, Take: orangeCards[0], Offer: redCards[0]})
		n = ok(t, n, 1, accept(n))
		if !has(n.Players[0].Incoming, orangeCards[0]) || !has(n.Players[1].Incoming, redCards[0]) || n.Phase != PhasePlacement {
			t.Fatal("swap did not move both cards to incoming")
		}
		if _, found := n.Players[0].setByID("mine"); found {
			t.Fatal("the emptied set should disappear")
		}
		// Off-turn placement of an incoming card is allowed; play waits for both.
		n = ok(t, n, 1, PlaceReceived{Card: redCards[0]})
		if n.Phase != PhasePlacement {
			t.Fatal("play resumed before the actor placed")
		}
		n = ok(t, n, 0, PlaceReceived{Card: orangeCards[0]})
		if n.Phase != PhasePlay {
			t.Fatal("play did not resume")
		}
	})
	t.Run("taking a detached building", func(t *testing.T) {
		n := ok(t, s, 0, PlayForcedDeal{Card: act(ForcedDeal, 1), Target: 1, Take: act(House, 1), Offer: redCards[0]})
		n = ok(t, n, 1, accept(n))
		if !has(n.Players[0].Detached, act(House, 1)) || !has(n.Players[1].Incoming, redCards[0]) {
			t.Fatal("building swap wrong")
		}
	})
	t.Run("blocked swap leaves both untouched", func(t *testing.T) {
		v := victim
		v.Hand = []CardID{act(JustSayNo, 1)}
		n := fixture(t, actor, v)
		n = ok(t, n, 0, PlayForcedDeal{Card: act(ForcedDeal, 1), Target: 1, Take: orangeCards[0], Offer: redCards[0]})
		n = ok(t, n, 1, jsn(n, act(JustSayNo, 1), ""))
		n = ok(t, n, 0, accept(n))
		if n.Phase != PhasePlay || !has(n.Players[0].Sets[1].Cards, redCards[0]) || !has(n.Players[1].Sets[1].Cards, orangeCards[0]) {
			t.Fatal("blocked swap moved cards")
		}
		if !has(n.Discard, act(JustSayNo, 1)) || !has(n.Discard, act(ForcedDeal, 1)) {
			t.Fatal("spent cards must reach the center pile")
		}
	})
}

// A14: Deal Breaker takes one selected complete set with its buildings;
// duplicate complete sets of one color stay individually selectable.
func TestDealBreaker(t *testing.T) {
	victim := seat{Sets: []PropertySet{
		{ID: "g1", Color: Green, Cards: greenCards, House: act(House, 1), Hotel: act(Hotel, 1)},
		set("g2", Green, wild(DarkBlue, Green, 1), wild(Green, Railroad, 1), rainbow(1)),
		set("rr", Railroad, prop("reading-railroad")),
		set("allrain", Brown, rainbow(2)),
	}}
	s := fixture(t, seat{Hand: []CardID{act(DealBreaker, 1)}}, victim)
	rejected(t, s, 0, PlayDealBreaker{Card: act(DealBreaker, 1), Target: 1, Set: "rr"}, CodeInvalidTarget)
	rejected(t, s, 0, PlayDealBreaker{Card: act(DealBreaker, 1), Target: 1, Set: "missing"}, CodeInvalidTarget)
	for _, id := range []string{"g1", "g2"} {
		t.Run("takes "+id, func(t *testing.T) {
			n := ok(t, s, 0, PlayDealBreaker{Card: act(DealBreaker, 1), Target: 1, Set: id})
			n = ok(t, n, 1, accept(n))
			i, found := n.Players[0].setByID(id)
			if !found || !n.Players[0].Sets[i].Complete() || n.Phase != PhasePlay {
				t.Fatal("set not transferred intact")
			}
			if id == "g1" && (n.Players[0].Sets[i].House != act(House, 1) || n.Players[0].Sets[i].Hotel != act(Hotel, 1)) {
				t.Fatal("buildings must travel with the set")
			}
			if _, still := n.Players[1].setByID(id); still {
				t.Fatal("victim kept the set")
			}
		})
	}
}

// A15: Debt Collector targets one opponent; Birthday charges every other
// player one at a time, clockwise from the source.
func TestChargesAndOrder(t *testing.T) {
	s := fixture(t,
		seat{Hand: []CardID{act(Birthday, 1), act(DebtCollector, 1)}},
		seat{Bank: []CardID{money(2, 1)}},
		seat{Bank: []CardID{money(2, 2)}},
		seat{Bank: []CardID{money(1, 1)}},
	)
	s.Active = 0
	n := ok(t, s, 0, PlayBirthday{Card: act(Birthday, 1)})
	if got := n.WaitingFor(); !slices.Equal(got, []int{1}) {
		t.Fatalf("first responder %v, want seat 1", got)
	}
	rejected(t, n, 2, accept(n), CodeNotYourTurn)
	n = ok(t, n, 1, accept(n))
	n = ok(t, n, 1, pay(n, money(2, 1)))
	if got := n.WaitingFor(); !slices.Equal(got, []int{2}) {
		t.Fatalf("second responder %v", got)
	}
	n = ok(t, n, 2, accept(n))
	n = ok(t, n, 2, pay(n, money(2, 2)))
	n = ok(t, n, 3, accept(n))
	rejected(t, n, 3, pay(n), CodeIllegal) // has 1M, owes 2M: must give it
	n = ok(t, n, 3, pay(n, money(1, 1)))
	if n.Phase != PhasePlay || totalValue(n.Players[0].Bank) != 5 {
		t.Fatalf("birthday collected %d", totalValue(n.Players[0].Bank))
	}
	d := ok(t, s, 0, PlayDebtCollector{Card: act(DebtCollector, 1), Target: 2})
	if len(d.Pending.Targets) != 1 || d.Pending.Targets[0].Seat != 2 || d.Pending.Base != 5 {
		t.Fatal("debt collector must target exactly the chosen player for 5M")
	}
	rejected(t, s, 0, PlayDebtCollector{Card: act(DebtCollector, 1), Target: 0}, CodeInvalidTarget)
	rejected(t, s, 0, PlayDebtCollector{Card: act(DebtCollector, 1), Target: 9}, CodeInvalidTarget)
}
