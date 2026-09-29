package monopoly

import (
	"slices"
	"testing"
)

// A04: zero to three plays; the fourth is rejected; Pass Go draws do not
// restore plays; a third-play Pass Go still requires hand reduction.
func TestPlayBudget(t *testing.T) {
	hand := []CardID{money(1, 1), money(1, 2), act(PassGo, 1), money(1, 3)}
	s := fixture(t, seat{Hand: hand}, seat{})
	s = ok(t, s, 0, Bank{Card: money(1, 1)})
	s = ok(t, s, 0, Bank{Card: money(1, 2)})
	s = ok(t, s, 0, PlayPassGo{Card: act(PassGo, 1)})
	if s.PlaysUsed != 3 || len(s.Players[0].Hand) != 3 {
		t.Fatalf("plays %d hand %d", s.PlaysUsed, len(s.Players[0].Hand))
	}
	rejected(t, s, 0, Bank{Card: money(1, 3)}, CodeNoPlays)
	// Rent plus two doublers needs three plays at once.
	r := fixture(t, seat{Hand: []CardID{money(1, 1), rent2(DarkBlue, Green, 1), act(DoubleRent, 1), act(DoubleRent, 2)}, Sets: []PropertySet{set("g", Green, prop("pacific-avenue"))}}, seat{})
	r = ok(t, r, 0, Bank{Card: money(1, 1)})
	rejected(t, r, 0, PlayRent{Card: rent2(DarkBlue, Green, 1), Set: "g", Doublers: []CardID{act(DoubleRent, 1), act(DoubleRent, 2)}}, CodeNoPlays)
	// Zero plays is fine.
	z := fixture(t, seat{Hand: []CardID{money(1, 1)}}, seat{})
	z = ok(t, z, 0, EndTurn{})
	if z.Active != 1 {
		t.Fatal("ending with zero plays failed")
	}
}

func TestOutOfTurnAndPhase(t *testing.T) {
	s := fixture(t, seat{Hand: []CardID{money(1, 1)}}, seat{Hand: []CardID{money(1, 2), act(JustSayNo, 1)}})
	rejected(t, s, 1, Bank{Card: money(1, 2)}, CodeNotYourTurn)
	rejected(t, s, 1, EndTurn{}, CodeNotYourTurn)
	rejected(t, s, 1, Rearrange{}, CodeNotYourTurn)
	rejected(t, s, 0, Accept{Pending: 1}, CodeWrongPhase)
	rejected(t, s, 1, PlayJustSayNo{Pending: 1, Card: act(JustSayNo, 1)}, CodeWrongPhase)
	rejected(t, s, 0, PlaceReceived{Card: money(1, 1)}, CodeWrongPhase)
	rejected(t, s, 5, Bank{Card: money(1, 1)}, CodeInvalidAction)
	rejected(t, s, 0, Bank{Card: money(1, 2)}, CodeInvalidCard) // another player's card
}

// A05: end-of-turn hand limit; excess to the draw bottom in listed order.
func TestEndTurnExcess(t *testing.T) {
	eight := []CardID{money(1, 1), money(1, 2), money(1, 3), money(1, 4), money(1, 5), money(1, 6), money(2, 1), money(2, 2)}
	nine := append(slices.Clone(eight), money(2, 3))
	cases := []struct {
		name   string
		hand   []CardID
		ret    []CardID
		code   string
		bottom []CardID
	}{
		{"eight returns one", eight, []CardID{money(2, 2)}, "", []CardID{money(2, 2)}},
		{"nine returns two in order", nine, []CardID{money(2, 3), money(1, 1)}, "", []CardID{money(2, 3), money(1, 1)}},
		{"eight returns none", eight, nil, CodeIllegal, nil},
		{"eight returns two", eight, []CardID{money(1, 1), money(1, 2)}, CodeIllegal, nil},
		{"duplicate return", nine, []CardID{money(1, 1), money(1, 1)}, CodeInvalidCard, nil},
		{"card not in hand", eight, []CardID{money(5, 1)}, CodeInvalidCard, nil},
		{"no voluntary discard at seven", eight[:7], []CardID{money(1, 1)}, CodeIllegal, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t, seat{Hand: tc.hand}, seat{Hand: []CardID{money(10, 1)}})
			if tc.code != "" {
				rejected(t, s, 0, EndTurn{Return: tc.ret}, tc.code)
				return
			}
			drawBefore := len(s.Draw)
			next, events := s, []Event(nil)
			next, events, _ = Apply(s, 0, EndTurn{Return: tc.ret})
			if len(next.Players[0].Hand) != HandLimit {
				t.Fatalf("hand %d after ending", len(next.Players[0].Hand))
			}
			// Seat 1 drew two from the top; the returned cards are now the bottom.
			if got := next.Draw[len(next.Draw)-len(tc.bottom):]; !slices.Equal(got, tc.bottom) || len(next.Draw) != drawBefore+len(tc.ret)-2 {
				t.Fatalf("draw bottom %v", got)
			}
			for _, e := range events {
				if e.Kind == "turn_ended" && e.Data["returned"] != len(tc.ret) {
					t.Fatal("public event must carry the count")
				}
				if e.Kind == "returned_cards" && e.Audience != 0 {
					t.Fatal("returned card identities must be private to the returner")
				}
			}
		})
	}
}

// A07: bank money, actions and Rent; never properties.
func TestBanking(t *testing.T) {
	cases := []struct {
		name string
		card CardID
		code string
	}{
		{"money", money(5, 1), ""},
		{"action", act(DealBreaker, 1), ""},
		{"just say no", act(JustSayNo, 1), ""},
		{"rent", rentAny(1), ""},
		{"house", act(House, 1), ""},
		{"property", prop("boardwalk"), CodeInvalidCard},
		{"two-color wild", wild(Pink, Orange, 1), CodeInvalidCard},
		{"multicolor wild", rainbow(1), CodeInvalidCard},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t, seat{Hand: []CardID{tc.card}}, seat{})
			if tc.code != "" {
				rejected(t, s, 0, Bank{Card: tc.card}, tc.code)
				return
			}
			s = ok(t, s, 0, Bank{Card: tc.card})
			if !has(s.Players[0].Bank, tc.card) || s.PlaysUsed != 1 {
				t.Fatal("card not banked")
			}
		})
	}
	t.Run("banked actions cannot be activated", func(t *testing.T) {
		s := fixture(t, seat{Bank: []CardID{act(PassGo, 1), act(SlyDeal, 1)}}, seat{Sets: []PropertySet{set("b", Brown, brownCards[0])}})
		rejected(t, s, 0, PlayPassGo{Card: act(PassGo, 1)}, CodeInvalidCard)
		rejected(t, s, 0, PlaySlyDeal{Card: act(SlyDeal, 1), Target: 1, Take: brownCards[0]}, CodeInvalidCard)
	})
}

func TestPlayProperty(t *testing.T) {
	s := fixture(t, seat{
		Hand: []CardID{prop("boardwalk"), wild(Pink, Orange, 1), rainbow(1), money(1, 1), prop("baltic-avenue")},
		Sets: []PropertySet{set("db", DarkBlue, prop("park-place")), set("br", Brown, prop("mediterranean-avenue"), wild(LightBlue, Brown, 1))},
	}, seat{Sets: []PropertySet{set("theirs", Pink, prop("states-avenue"))}})
	cases := []struct {
		name string
		a    PlayProperty
		code string
	}{
		{"money is not property", PlayProperty{Card: money(1, 1)}, CodeInvalidCard},
		{"wrong color set", PlayProperty{Card: prop("boardwalk"), Set: "br"}, CodeIllegal},
		{"full set", PlayProperty{Card: prop("baltic-avenue"), Set: "br"}, CodeIllegal},
		{"opponent set", PlayProperty{Card: wild(Pink, Orange, 1), Set: "theirs"}, CodeInvalidTarget},
		{"wild needs a color", PlayProperty{Card: wild(Pink, Orange, 1)}, CodeIllegal},
		{"wild wrong color", PlayProperty{Card: wild(Pink, Orange, 1), Color: Red}, CodeIllegal},
		{"property wrong color", PlayProperty{Card: prop("boardwalk"), Color: Green}, CodeIllegal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { rejected(t, s, 0, tc.a, tc.code) })
	}
	t.Run("legal placements", func(t *testing.T) {
		n := ok(t, s, 0, PlayProperty{Card: prop("boardwalk"), Set: "db"})
		if !n.Players[0].Sets[0].Complete() {
			t.Fatal("dark blue should be complete")
		}
		n = ok(t, n, 0, PlayProperty{Card: wild(Pink, Orange, 1), Color: Orange})
		n = ok(t, n, 0, PlayProperty{Card: rainbow(1)})
		if !has(n.Players[0].Unassigned, rainbow(1)) || len(n.Players[0].Sets) != 3 {
			t.Fatalf("placements wrong: %+v", n.Players[0])
		}
	})
	t.Run("duplicate color starts a separate set", func(t *testing.T) {
		n := ok(t, s, 0, PlayProperty{Card: prop("baltic-avenue")})
		if len(n.Players[0].Sets) != 3 || n.Players[0].Sets[2].Color != Brown {
			t.Fatal("second brown set not created")
		}
	})
}

// A17: House only on a complete non-railroad/utility set; Hotel needs a House;
// one of each.
func TestBuildings(t *testing.T) {
	sets := []PropertySet{
		set("db", DarkBlue, darkBlueCards...),
		set("gr", Green, greenCards[:2]...),
		set("ut", Utility, utilityCards...),
		{ID: "or", Color: Orange, Cards: orangeCards, House: act(House, 2), Hotel: act(Hotel, 2)},
		set("rain", Brown, rainbow(1), rainbow(2)),
	}
	s := fixture(t, seat{Hand: []CardID{act(House, 1), act(Hotel, 1), act(House, 3)}, Sets: sets}, seat{})
	cases := []struct {
		name string
		a    PlayBuilding
		code string
	}{
		{"hotel before house", PlayBuilding{Card: act(Hotel, 1), Set: "db"}, CodeIllegal},
		{"incomplete set", PlayBuilding{Card: act(House, 1), Set: "gr"}, CodeIllegal},
		{"utility", PlayBuilding{Card: act(House, 1), Set: "ut"}, CodeIllegal},
		{"second house", PlayBuilding{Card: act(House, 1), Set: "or"}, CodeIllegal},
		{"second hotel", PlayBuilding{Card: act(Hotel, 1), Set: "or"}, CodeIllegal},
		{"all-multicolor set is not complete (Q3.1)", PlayBuilding{Card: act(House, 1), Set: "rain"}, CodeIllegal},
		{"not a building", PlayBuilding{Card: money(1, 1), Set: "db"}, CodeInvalidCard},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { rejected(t, s, 0, tc.a, tc.code) })
	}
	n := ok(t, s, 0, PlayBuilding{Card: act(House, 1), Set: "db"})
	n = ok(t, n, 0, PlayBuilding{Card: act(Hotel, 1), Set: "db"})
	if got := n.Players[0].Sets[0].Rent(); got != 8+3+4 {
		t.Fatalf("dark blue with House and Hotel rents %d, want 15 (+7M total)", got)
	}
	if n.PlaysUsed != 2 {
		t.Fatal("each building costs one play")
	}
}

// A21 and decision Q3.5: free own-turn reorganization, validated atomically.
func TestRearrange(t *testing.T) {
	mk := func(t *testing.T) *State {
		return fixture(t, seat{
			Sets: []PropertySet{
				{ID: "db", Color: DarkBlue, Cards: []CardID{prop("park-place"), wild(DarkBlue, Green, 1)}, House: act(House, 1)},
				set("gr", Green, greenCards[:2]...),
			},
			Unassigned: []CardID{rainbow(1)},
			Detached:   []CardID{act(Hotel, 1)},
		}, seat{Sets: []PropertySet{set("b", Brown, brownCards...)}})
	}
	s := mk(t)
	cases := []struct {
		name string
		a    Rearrange
		code string
	}{
		{"missing a card", Rearrange{Sets: []SetLayout{{ID: "db", Color: DarkBlue, Cards: []CardID{prop("park-place"), wild(DarkBlue, Green, 1)}, House: act(House, 1)}}}, CodeIllegal},
		{"card twice", Rearrange{
			Sets:       []SetLayout{{ID: "db", Color: DarkBlue, Cards: []CardID{prop("park-place"), wild(DarkBlue, Green, 1)}, House: act(House, 1)}, {ID: "gr", Color: Green, Cards: append(slices.Clone(greenCards[:2]), wild(DarkBlue, Green, 1))}},
			Unassigned: []CardID{rainbow(1)}, Detached: []CardID{act(Hotel, 1)},
		}, CodeInvalidCard},
		{"opponent card", Rearrange{
			Sets:       []SetLayout{{ID: "db", Color: DarkBlue, Cards: []CardID{prop("park-place"), wild(DarkBlue, Green, 1)}, House: act(House, 1)}, {ID: "gr", Color: Green, Cards: greenCards[:2]}, {Color: Brown, Cards: brownCards}},
			Unassigned: []CardID{rainbow(1)}, Detached: []CardID{act(Hotel, 1)},
		}, CodeInvalidCard},
		{"house left on broken set", Rearrange{
			Sets:       []SetLayout{{ID: "db", Color: DarkBlue, Cards: []CardID{prop("park-place")}, House: act(House, 1)}, {ID: "gr", Color: Green, Cards: append(slices.Clone(greenCards[:2]), wild(DarkBlue, Green, 1))}},
			Unassigned: []CardID{rainbow(1)}, Detached: []CardID{act(Hotel, 1)},
		}, CodeIllegal},
		{"overfull set", Rearrange{
			Sets:     []SetLayout{{ID: "db", Color: DarkBlue, Cards: []CardID{prop("park-place"), wild(DarkBlue, Green, 1), rainbow(1)}, House: act(House, 1)}, {ID: "gr", Color: Green, Cards: greenCards[:2]}},
			Detached: []CardID{act(Hotel, 1)},
		}, CodeIllegal},
		{"wild in impossible color", Rearrange{
			Sets:       []SetLayout{{ID: "db", Color: DarkBlue, Cards: []CardID{prop("park-place")}}, {ID: "gr", Color: Red, Cards: []CardID{wild(DarkBlue, Green, 1)}}, {Color: Green, Cards: greenCards[:2]}},
			Unassigned: []CardID{rainbow(1)}, Detached: []CardID{act(House, 1), act(Hotel, 1)},
		}, CodeIllegal},
		{"building left unassigned", Rearrange{
			Sets:       []SetLayout{{ID: "db", Color: DarkBlue, Cards: []CardID{prop("park-place"), wild(DarkBlue, Green, 1)}}, {ID: "gr", Color: Green, Cards: greenCards[:2]}},
			Unassigned: []CardID{rainbow(1), act(House, 1)}, Detached: []CardID{act(Hotel, 1)},
		}, CodeIllegal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { rejected(t, s, 0, tc.a, tc.code) })
	}
	t.Run("move wild, reattach Hotel, assign rainbow", func(t *testing.T) {
		n := ok(t, s, 0, Rearrange{
			Sets: []SetLayout{
				{ID: "db", Color: DarkBlue, Cards: []CardID{prop("park-place"), rainbow(1)}, House: act(House, 1), Hotel: act(Hotel, 1)},
				{ID: "gr", Color: Green, Cards: append(slices.Clone(greenCards[:2]), wild(DarkBlue, Green, 1))},
			},
		})
		p := n.Players[0]
		if !p.Sets[0].Complete() || !p.Sets[1].Complete() || p.Sets[0].Hotel != act(Hotel, 1) || len(p.Detached) != 0 || n.PlaysUsed != 0 {
			t.Fatalf("rearrange result %+v", p)
		}
	})
	t.Run("not during a pending action", func(t *testing.T) {
		p := fixture(t, seat{Hand: []CardID{act(DebtCollector, 1)}}, seat{})
		p = ok(t, p, 0, PlayDebtCollector{Card: act(DebtCollector, 1), Target: 1})
		rejected(t, p, 0, Rearrange{}, CodeWrongPhase)
		rejected(t, p, 0, EndTurn{}, CodeWrongPhase)
	})
}
