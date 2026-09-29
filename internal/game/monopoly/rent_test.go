package monopoly

import (
	"slices"
	"testing"
)

// A16: every property group's rent ladder matches the verified card faces.
func TestRentLadders(t *testing.T) {
	want := map[Color][]int{
		Brown: {1, 2}, LightBlue: {1, 2, 3}, Pink: {1, 2, 4}, Orange: {1, 3, 5}, Red: {2, 3, 6},
		Yellow: {2, 4, 6}, Green: {2, 4, 7}, DarkBlue: {3, 8}, Railroad: {1, 2, 3, 4}, Utility: {1, 2},
	}
	byColor := map[Color][]CardID{}
	for _, c := range manifest {
		if c.Kind == KindProperty {
			byColor[c.Colors[0]] = append(byColor[c.Colors[0]], c.ID)
		}
	}
	for color, ladder := range want {
		for n := 1; n <= len(ladder); n++ {
			got := PropertySet{Color: color, Cards: byColor[color][:n]}.Rent()
			if got != ladder[n-1] {
				t.Errorf("%s with %d: rent %d, want %d", color, n, got, ladder[n-1])
			}
		}
	}
}

func TestRentConfigurations(t *testing.T) {
	cases := []struct {
		name string
		set  PropertySet
		rent int
		full bool
	}{
		{"two-color wild counts at its active color", set("x", Railroad, wild(Railroad, Utility, 1), prop("short-line")), 2, false},
		{"multicolor wild completes with an anchor", set("x", DarkBlue, prop("boardwalk"), rainbow(1)), 8, true},
		{"multicolor wild alone earns nothing (G2/G4)", set("x", DarkBlue, rainbow(1)), 0, false},
		{"all-multicolor full-size set is not complete (Q3.1)", set("x", Brown, rainbow(1), rainbow(2)), 0, false},
		{"house adds 3M", PropertySet{ID: "x", Color: Green, Cards: greenCards, House: act(House, 1)}, 10, true},
		{"house and hotel add 7M", PropertySet{ID: "x", Color: Green, Cards: greenCards, House: act(House, 1), Hotel: act(Hotel, 1)}, 14, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set.Rent() != tc.rent || tc.set.Complete() != tc.full {
				t.Fatalf("rent %d complete %v; want %d %v", tc.set.Rent(), tc.set.Complete(), tc.rent, tc.full)
			}
		})
	}
}

// rentFixture: seat 0 owns two green sets and an orange set; three opponents.
func rentFixture(t *testing.T, hand ...CardID) *State {
	t.Helper()
	return fixture(t,
		seat{Hand: hand, Sets: []PropertySet{
			set("g-full", Green, greenCards...),
			set("g-one", Green, wild(DarkBlue, Green, 1)),
			set("o", Orange, orangeCards[:2]...),
			set("rain", Red, rainbow(1)),
		}},
		seat{Bank: []CardID{money(10, 1)}},
		seat{Bank: []CardID{money(5, 1), money(5, 2)}},
		seat{Bank: []CardID{money(4, 1), money(4, 2), money(4, 3)}},
	)
}

// A16, A18 and decisions Q2, Q3.2: rent from one chosen set, frozen at
// declaration; doublers only with two-color Rent, costing one play each.
func TestRentDeclaration(t *testing.T) {
	two := rent2(DarkBlue, Green, 1)
	any := rentAny(1)
	d1, d2 := act(DoubleRent, 1), act(DoubleRent, 2)
	s := rentFixture(t, two, any, d1, d2, rent2(Red, Yellow, 1))
	cases := []struct {
		name string
		a    PlayRent
		code string
	}{
		{"card does not match set color", PlayRent{Card: rent2(Red, Yellow, 1), Set: "o"}, CodeIllegal},
		{"set earns no rent", PlayRent{Card: rent2(Red, Yellow, 1), Set: "rain"}, CodeIllegal},
		{"opponent's set", PlayRent{Card: two, Set: "missing"}, CodeInvalidTarget},
		{"multicolor needs a target", PlayRent{Card: any, Set: "o"}, CodeInvalidTarget},
		{"two-color takes no target", PlayRent{Card: two, Set: "g-full", Target: intp(1)}, CodeInvalidTarget},
		{"multicolor cannot be doubled (Q2)", PlayRent{Card: any, Set: "o", Target: intp(1), Doublers: []CardID{d1}}, CodeIllegal},
		{"duplicate doubler", PlayRent{Card: two, Set: "g-full", Doublers: []CardID{d1, d1}}, CodeInvalidCard},
		{"doubler not in hand", PlayRent{Card: two, Set: "g-full", Doublers: []CardID{act(PassGo, 1)}}, CodeInvalidCard},
		{"three doublers", PlayRent{Card: two, Set: "g-full", Doublers: []CardID{d1, d2, d1}}, CodeNoPlays},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { rejected(t, s, 0, tc.a, tc.code) })
	}
	amounts := []struct {
		name     string
		a        PlayRent
		targets  []int
		owed     int
		plays    int
		complete bool
	}{
		{"complete green, everyone pays 7M", PlayRent{Card: two, Set: "g-full"}, []int{1, 2, 3}, 7, 1, true},
		{"chosen one-card green set charges 2M (Q3.2)", PlayRent{Card: two, Set: "g-one"}, []int{1, 2, 3}, 2, 1, false},
		{"one doubler 2x", PlayRent{Card: two, Set: "g-full", Doublers: []CardID{d1}}, []int{1, 2, 3}, 14, 2, true},
		{"two doublers 4x", PlayRent{Card: two, Set: "g-full", Doublers: []CardID{d1, d2}}, []int{1, 2, 3}, 28, 3, true},
		{"multicolor charges one chosen player", PlayRent{Card: any, Set: "o", Target: intp(2)}, []int{2}, 3, 1, false},
	}
	for _, tc := range amounts {
		t.Run(tc.name, func(t *testing.T) {
			n := ok(t, s, 0, tc.a)
			var seats []int
			for _, tg := range n.Pending.Targets {
				seats = append(seats, tg.Seat)
			}
			if !slices.Equal(seats, tc.targets) || n.PlaysUsed != tc.plays {
				t.Fatalf("targets %v plays %d", seats, n.PlaysUsed)
			}
			n = ok(t, n, tc.targets[0], accept(n))
			if n.Pending.Targets[0].Owed != tc.owed {
				t.Fatalf("owed %d, want %d", n.Pending.Targets[0].Owed, tc.owed)
			}
		})
	}
	t.Run("amount frozen for later payers", func(t *testing.T) {
		n := ok(t, s, 0, PlayRent{Card: two, Set: "g-one"})
		n = ok(t, n, 1, accept(n))
		// Seat 1 pays with its 10M; no change, and seat 2 still owes 2M.
		n = ok(t, n, 1, pay(n, money(10, 1)))
		n = ok(t, n, 2, accept(n))
		if n.Pending.Targets[1].Owed != 2 {
			t.Fatalf("second payer owes %d", n.Pending.Targets[1].Owed)
		}
	})
	t.Run("no standalone doubling", func(t *testing.T) {
		// Double the Rent has no action of its own; it only rides on PlayRent.
		rejected(t, s, 0, PlayPassGo{Card: d1}, CodeInvalidCard)
	})
}
