package monopoly

import "testing"

// debtFixture puts seat 1 in the payment stage for a Debt Collector (5M).
func debtFixture(t *testing.T, payer seat) *State {
	t.Helper()
	s := fixture(t, seat{Hand: []CardID{act(DebtCollector, 1)}}, payer)
	s = ok(t, s, 0, PlayDebtCollector{Card: act(DebtCollector, 1), Target: 1})
	return ok(t, s, 1, accept(s))
}

// A08, A09, A11: payer's choice, overpayment without change, shortfall
// forgiveness, and atomic rejection of illegal selections (B1 How to Pay; G2).
func TestPaymentSelection(t *testing.T) {
	rich := seat{
		Hand: []CardID{money(10, 1)},
		Bank: []CardID{money(2, 1), money(3, 1), act(PassGo, 1)},
		Sets: []PropertySet{set("o", Orange, orangeCards[0], rainbow(1))},
	}
	cases := []struct {
		name  string
		payer seat
		cards []CardID
		code  string
		value int
	}{
		{"overpay 2M+3M+Pass Go for 5M allowed", rich, []CardID{money(2, 1), money(3, 1), act(PassGo, 1)}, "", 6},
		{"exact with property", rich, []CardID{money(3, 1), money(2, 1)}, "", 5},
		{"property breaks into payment", rich, []CardID{money(3, 1), orangeCards[0]}, "", 5},
		{"underpay while value remains", rich, []CardID{money(3, 1)}, CodeIllegal, 0},
		{"hand card", rich, []CardID{money(10, 1)}, CodeInvalidCard, 0},
		{"multicolor wild", rich, []CardID{money(2, 1), money(3, 1), rainbow(1)}, CodeInvalidCard, 0},
		{"duplicate card", rich, []CardID{money(3, 1), money(3, 1)}, CodeInvalidCard, 0},
		{"other player's card", rich, []CardID{money(3, 1), money(2, 1), money(5, 1)}, CodeInvalidCard, 0},
		{"shortfall must give everything", seat{Bank: []CardID{money(1, 1)}, Sets: []PropertySet{set("b", Brown, brownCards[0])}}, []CardID{money(1, 1)}, CodeIllegal, 0},
		{"shortfall gives all", seat{Bank: []CardID{money(1, 1)}, Sets: []PropertySet{set("b", Brown, brownCards[0])}}, []CardID{money(1, 1), brownCards[0]}, "", 2},
		{"only a multicolor wild pays nothing", seat{Unassigned: []CardID{rainbow(1)}}, nil, "", 0},
		{"nothing on the table pays nothing", seat{Hand: []CardID{money(5, 1)}}, nil, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := debtFixture(t, tc.payer)
			if s.Phase != PhasePayment {
				t.Fatal("not in payment phase")
			}
			rejected(t, s, 0, pay(s, tc.cards...), CodeNotYourTurn) // creditor cannot choose
			if tc.code != "" {
				rejected(t, s, 1, pay(s, tc.cards...), tc.code)
				return
			}
			n := ok(t, s, 1, pay(s, tc.cards...))
			got := totalValue(n.Players[0].Bank) + totalValue(n.Players[0].Incoming)
			for _, set := range n.Players[0].Sets {
				got += totalValue(set.Cards)
			}
			if got != tc.value {
				t.Fatalf("creditor received %dM, want %dM", got, tc.value)
			}
			if n.Pending != nil {
				t.Fatal("debt must not remain after payment")
			}
		})
	}
	t.Run("stale payment is rejected", func(t *testing.T) {
		s := debtFixture(t, rich)
		rejected(t, s, 1, Pay{Pending: s.Pending.ID, Step: s.Pending.Step - 1, Cards: []CardID{money(3, 1), money(2, 1)}}, CodeStale)
		rejected(t, s, 1, Pay{Pending: s.Pending.ID + 1, Step: s.Pending.Step, Cards: []CardID{money(3, 1), money(2, 1)}}, CodeStale)
	})
	t.Run("banked action stays money for the recipient", func(t *testing.T) {
		s := debtFixture(t, seat{Bank: []CardID{act(DealBreaker, 1)}})
		n := ok(t, s, 1, pay(s, act(DealBreaker, 1)))
		if !has(n.Players[0].Bank, act(DealBreaker, 1)) || has(n.Players[0].Hand, act(DealBreaker, 1)) {
			t.Fatal("banked action must move bank to bank")
		}
	})
}

// A10 and decisions Q3.3–Q3.4: paying with a property from a complete set
// breaks it and detaches its buildings; a paid building stays a building.
func TestPaymentBreaksSets(t *testing.T) {
	built := func() seat {
		return seat{Sets: []PropertySet{{ID: "g", Color: Green, Cards: greenCards, House: act(House, 1), Hotel: act(Hotel, 1)}}}
	}
	t.Run("property from a built set", func(t *testing.T) {
		s := debtFixture(t, built())
		n := ok(t, s, 1, pay(s, greenCards[0], greenCards[1]))
		payer := n.Players[1]
		if len(payer.Sets) != 1 || payer.Sets[0].House != "" || !has(payer.Detached, act(House, 1)) || !has(payer.Detached, act(Hotel, 1)) {
			t.Fatalf("buildings should detach from the broken set: %+v", payer)
		}
		// Single-color properties are placed for the recipient at once.
		if len(n.Players[0].Sets) != 1 || len(n.Players[0].Sets[0].Cards) != 2 || n.Phase != PhasePlay {
			t.Fatalf("paid properties go to the recipient's property area: %+v", n.Players[0])
		}
	})
	t.Run("paying the House detaches the Hotel", func(t *testing.T) {
		s := debtFixture(t, built())
		n := ok(t, s, 1, pay(s, act(House, 1), greenCards[0]))
		if !has(n.Players[1].Detached, act(Hotel, 1)) || n.Players[1].Sets[0].Hotel != "" {
			t.Fatal("Hotel must detach when its House is gone")
		}
		if !has(n.Players[0].Detached, act(House, 1)) || has(n.Players[0].Bank, act(House, 1)) {
			t.Fatal("a paid building stays a detached building, not money")
		}
	})
	t.Run("paying the Hotel keeps the House", func(t *testing.T) {
		payer := built()
		payer.Bank = []CardID{money(1, 1)}
		s := debtFixture(t, payer)
		n := ok(t, s, 1, pay(s, act(Hotel, 1), money(1, 1)))
		set := n.Players[1].Sets[0]
		if !set.Complete() || set.House != act(House, 1) || set.Hotel != "" || len(n.Players[1].Detached) != 0 {
			t.Fatalf("House should stay on the intact set: %+v", set)
		}
		if n.Phase != PhasePlay {
			t.Fatal("no placement needed for a building and money")
		}
	})
}

// Paying while a charge awaits your response accepts it in the same move;
// nothing else may be "paid" that way.
func TestPayAcceptsAnOpenCharge(t *testing.T) {
	s := fixture(t,
		seat{Hand: []CardID{act(DebtCollector, 1), act(SlyDeal, 1)}},
		seat{Bank: []CardID{money(5, 1)}, Sets: []PropertySet{set("r", Red, redCards[0])}},
	)
	charged := ok(t, s, 0, PlayDebtCollector{Card: act(DebtCollector, 1), Target: 1})
	rejected(t, charged, 0, pay(charged, money(5, 1)), CodeWrongPhase) // only the payer
	paid := ok(t, charged, 1, pay(charged, money(5, 1)))
	if !has(paid.Players[0].Bank, money(5, 1)) || paid.Phase != PhasePlay || paid.Pending != nil {
		t.Fatal("paying should accept and settle the debt in one move")
	}
	// A Sly Deal is not a charge: "paying" it changes nothing.
	stolen := ok(t, s, 0, PlaySlyDeal{Card: act(SlyDeal, 1), Target: 1, Take: redCards[0]})
	before := snapshot(t, stolen)
	rejected(t, stolen, 1, pay(stolen), CodeWrongPhase)
	if snapshot(t, stolen) != before {
		t.Fatal("a rejected payment must not accept the steal")
	}
}
