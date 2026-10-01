package monopoly

import (
	"slices"
	"testing"
)

var (
	jsn1 = act(JustSayNo, 1)
	jsn2 = act(JustSayNo, 2)
	jsn3 = act(JustSayNo, 3)
)

// A19: parity over a chain. One JSN blocks, two restore, three block. Each
// response is free, even for an active player with no plays left.
func TestJustSayNoParity(t *testing.T) {
	cases := []struct {
		name    string
		chain   []int // seats playing JSN in order; the next waiting seat then accepts
		blocked bool
	}{
		{"no JSN", nil, false},
		{"one JSN blocks", []int{1}, true},
		{"counter restores", []int{1, 0}, false},
		{"third JSN blocks again", []int{1, 0, 1}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t,
				seat{Hand: []CardID{act(SlyDeal, 1), jsn2, money(1, 1), money(1, 2)}, Bank: []CardID{money(1, 3)}},
				seat{Hand: []CardID{jsn1, jsn3}, Sets: []PropertySet{set("r", Red, wild(Red, Yellow, 1))}},
			)
			s = ok(t, s, 0, Bank{Card: money(1, 1)})
			s = ok(t, s, 0, Bank{Card: money(1, 2)})
			s = ok(t, s, 0, PlaySlyDeal{Card: act(SlyDeal, 1), Target: 1, Take: wild(Red, Yellow, 1)})
			if s.PlaysUsed != MaxPlays {
				t.Fatal("setup should exhaust plays")
			}
			cards := map[int][]CardID{0: {jsn2}, 1: {jsn1, jsn3}}
			for _, who := range tc.chain {
				s = ok(t, s, who, jsn(s, cards[who][0], ""))
				cards[who] = cards[who][1:]
				if s.PlaysUsed != MaxPlays {
					t.Fatal("Just Say No must not consume plays")
				}
			}
			waiting := s.WaitingFor()[0]
			s = ok(t, s, waiting, accept(s))
			_, kept := s.Players[1].locate(wild(Red, Yellow, 1))
			if tc.blocked != kept || tc.blocked == has(s.Players[0].Incoming, wild(Red, Yellow, 1)) {
				t.Fatalf("blocked=%v but defender kept card=%v", tc.blocked, kept)
			}
			// A blocked third play leaves nothing to place, so the turn ends
			// by itself; a stolen wild waits for the thief to choose its color.
			if tc.blocked != (s.Active == 1) {
				t.Fatalf("blocked=%v but active seat is %d", tc.blocked, s.Active)
			}
			if got := len(s.Discard); got != 1+len(tc.chain) {
				t.Fatalf("center pile has %d cards, want action + %d JSN", got, len(tc.chain))
			}
		})
	}
}

// A19, A20: who may respond, stale or repeated responses, banked JSN.
func TestJustSayNoResponders(t *testing.T) {
	s := fixture(t,
		seat{Hand: []CardID{act(DebtCollector, 1), jsn2}},
		seat{Hand: []CardID{jsn1}, Bank: []CardID{money(5, 1)}},
		seat{Hand: []CardID{jsn3}},
	)
	s = ok(t, s, 0, PlayDebtCollector{Card: act(DebtCollector, 1), Target: 1})
	rejected(t, s, 2, jsn(s, jsn3, ""), CodeNotYourTurn) // uninvolved player
	rejected(t, s, 2, accept(s), CodeNotYourTurn)        // uninvolved player
	rejected(t, s, 0, jsn(s, jsn2, ""), CodeNotYourTurn) // source cannot pre-empt
	rejected(t, s, 1, jsn(s, jsn2, ""), CodeInvalidCard) // another player's card
	rejected(t, s, 1, jsn(s, act(DealBreaker, 1), ""), CodeInvalidCard)
	rejected(t, s, 1, jsn(s, jsn1, "nonsense"), CodeInvalidTarget)
	stale := jsn(s, jsn1, "")
	s = ok(t, s, 1, stale)
	rejected(t, s, 1, stale, CodeStale) // duplicate network submission is not a new counter
	rejected(t, s, 1, jsn(s, jsn1, ""), CodeInvalidCard)
	rejected(t, s, 2, jsn(s, jsn3, ""), CodeNotYourTurn) // Q1b: only source and defender
	rejected(t, s, 1, accept(s), CodeNotYourTurn)        // source's turn to counter

	t.Run("banked Just Say No cannot respond", func(t *testing.T) {
		b := fixture(t, seat{Hand: []CardID{act(DebtCollector, 1)}}, seat{Bank: []CardID{jsn1, money(5, 1)}})
		b = ok(t, b, 0, PlayDebtCollector{Card: act(DebtCollector, 1), Target: 1})
		rejected(t, b, 1, jsn(b, jsn1, ""), CodeInvalidCard)
	})
	t.Run("no response to banking, properties, Pass Go or buildings", func(t *testing.T) {
		b := fixture(t, seat{Hand: []CardID{money(1, 1), act(PassGo, 1)}}, seat{Hand: []CardID{jsn1}})
		b = ok(t, b, 0, Bank{Card: money(1, 1)})
		b = ok(t, b, 0, PlayPassGo{Card: act(PassGo, 1)})
		if b.Phase != PhasePlay || b.Pending != nil {
			t.Fatal("these plays must not open a response window")
		}
		rejected(t, b, 1, PlayJustSayNo{Card: jsn1}, CodeWrongPhase)
	})
}

// Decision Q1b: on a group charge a Just Say No protects only its defender,
// and other defenders keep their own obligations.
func TestGroupJustSayNoIsPerDefender(t *testing.T) {
	s := fixture(t,
		seat{Hand: []CardID{act(Birthday, 1)}},
		seat{Hand: []CardID{jsn1}, Bank: []CardID{money(2, 1)}},
		seat{Bank: []CardID{money(2, 2)}},
	)
	s = ok(t, s, 0, PlayBirthday{Card: act(Birthday, 1)})
	s = ok(t, s, 1, jsn(s, jsn1, ""))
	s = ok(t, s, 0, accept(s))
	if s.Pending.Targets[0].Outcome != "blocked" || !slices.Equal(s.WaitingFor(), []int{2}) {
		t.Fatal("seat 1 should be protected and seat 2 next")
	}
	s = ok(t, s, 2, accept(s))
	s = ok(t, s, 2, pay(s, money(2, 2)))
	if !has(s.Players[0].Bank, money(2, 2)) || has(s.Players[0].Bank, money(2, 1)) {
		t.Fatal("only the unprotected defender pays")
	}
}

// Decisions Q2 and Q2-F1: against doubled Rent a defender targets the whole
// charge or one doubler; counters restore the targeted part; the defender may
// keep starting chains on other active parts until they accept.
func TestJustSayNoAgainstDoubledRent(t *testing.T) {
	d1, d2 := act(DoubleRent, 1), act(DoubleRent, 2)
	build := func(t *testing.T, defender, source []CardID) *State {
		s := fixture(t,
			seat{Hand: append([]CardID{rent2(DarkBlue, Green, 1), d1, d2}, source...), Sets: []PropertySet{set("g", Green, greenCards[0])}},
			seat{Hand: defender, Bank: []CardID{money(10, 1)}},
		)
		// Base 2M (one green), x2 x2 = 8M.
		return ok(t, s, 0, PlayRent{Card: rent2(DarkBlue, Green, 1), Set: "g", Doublers: []CardID{d1, d2}})
	}
	type step struct {
		seat      int
		jsn       CardID
		component string
		accept    bool
	}
	cases := []struct {
		name     string
		defender []CardID
		source   []CardID
		steps    []step
		owed     int // 0 means the whole charge was blocked
	}{
		{"accept pays 8M", nil, nil, []step{{seat: 1, accept: true}}, 8},
		{"block one doubler: 4M", []CardID{jsn1}, nil, []step{{1, jsn1, string(d1), false}, {0, "", "", true}, {1, "", "", true}}, 4},
		{"block both doublers: 2M", []CardID{jsn1, jsn3}, nil, []step{{1, jsn1, string(d1), false}, {0, "", "", true}, {1, jsn3, string(d2), false}, {0, "", "", true}, {1, "", "", true}}, 2},
		{"block the whole charge", []CardID{jsn1}, nil, []step{{1, jsn1, ComponentCharge, false}, {0, "", "", true}}, 0},
		{"counter restores a doubler", []CardID{jsn1}, []CardID{jsn2}, []step{{1, jsn1, string(d1), false}, {0, jsn2, "", false}, {1, "", "", true}, {1, "", "", true}}, 8},
		{"after a doubler, block the whole charge", []CardID{jsn1, jsn3}, nil, []step{{1, jsn1, string(d2), false}, {0, "", "", true}, {1, jsn3, ComponentCharge, false}, {0, "", "", true}}, 0},
		{"charge restored, then a doubler blocked", []CardID{jsn1, jsn3}, []CardID{jsn2}, []step{{1, jsn1, ComponentCharge, false}, {0, jsn2, "", false}, {1, "", "", true}, {1, jsn3, string(d1), false}, {0, "", "", true}, {1, "", "", true}}, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := build(t, tc.defender, tc.source)
			for _, st := range tc.steps {
				if st.accept {
					s = ok(t, s, st.seat, accept(s))
				} else {
					s = ok(t, s, st.seat, jsn(s, st.jsn, st.component))
				}
			}
			if tc.owed == 0 {
				if s.Pending != nil || s.Phase != PhasePlay || !has(s.Players[1].Bank, money(10, 1)) {
					t.Fatal("blocked charge should resolve with no payment")
				}
				return
			}
			if s.Phase != PhasePayment || s.Pending.Targets[0].Owed != tc.owed {
				t.Fatalf("phase %s owed %d, want %d", s.Phase, s.Pending.Targets[0].Owed, tc.owed)
			}
		})
	}
	t.Run("a settled part cannot be targeted again", func(t *testing.T) {
		s := build(t, []CardID{jsn1, jsn3}, []CardID{jsn2})
		s = ok(t, s, 1, jsn(s, jsn1, string(d1)))
		s = ok(t, s, 0, jsn(s, jsn2, ""))
		s = ok(t, s, 1, accept(s)) // defender lets the counter stand: d1 settled
		rejected(t, s, 1, jsn(s, jsn3, string(d1)), CodeInvalidTarget)
		rejected(t, s, 1, jsn(s, jsn3, "unknown"), CodeInvalidTarget)
	})
	t.Run("counter must stay on the open chain", func(t *testing.T) {
		s := build(t, []CardID{jsn1}, []CardID{jsn2})
		s = ok(t, s, 1, jsn(s, jsn1, string(d1)))
		rejected(t, s, 0, jsn(s, jsn2, string(d2)), CodeInvalidTarget)
	})
	t.Run("non-rent actions have only the whole charge", func(t *testing.T) {
		s := fixture(t, seat{Hand: []CardID{act(DebtCollector, 1)}}, seat{Hand: []CardID{jsn1}})
		s = ok(t, s, 0, PlayDebtCollector{Card: act(DebtCollector, 1), Target: 1})
		rejected(t, s, 1, jsn(s, jsn1, string(d1)), CodeInvalidTarget)
	})
}

// All three Just Say No cards in one chain on a Deal Breaker (spec edge list).
func TestThreeCardChain(t *testing.T) {
	s := fixture(t,
		seat{Hand: []CardID{act(DealBreaker, 1), jsn2}},
		seat{Hand: []CardID{jsn1, jsn3}, Sets: []PropertySet{set("db", DarkBlue, darkBlueCards...)}},
	)
	s = ok(t, s, 0, PlayDealBreaker{Card: act(DealBreaker, 1), Target: 1, Set: "db"})
	s = ok(t, s, 1, jsn(s, jsn1, ""))
	s = ok(t, s, 0, jsn(s, jsn2, ""))
	s = ok(t, s, 1, jsn(s, jsn3, ""))
	if !slices.Equal(s.WaitingFor(), []int{0}) {
		t.Fatal("source should decide after the third JSN")
	}
	rejected(t, s, 0, jsn(s, jsn2, ""), CodeInvalidCard) // already spent
	s = ok(t, s, 0, accept(s))
	if _, kept := s.Players[1].setByID("db"); !kept || len(s.Discard) != 4 {
		t.Fatal("three JSNs should block and all four cards reach the center")
	}
}
