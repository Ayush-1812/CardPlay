package trump

import (
	"slices"
	"testing"
)

// Spec rules 5-7: a standard 52-card deck, four suits of thirteen ranks,
// every physical card with a unique parseable ID.
func TestDeck(t *testing.T) {
	cards := Manifest()
	if len(cards) != 52 {
		t.Fatalf("deck has %d cards, want 52", len(cards))
	}
	seen := map[CardID]bool{}
	perSuit := map[Suit]int{}
	for _, c := range cards {
		if seen[c.ID] {
			t.Fatalf("duplicate card ID %s", c.ID)
		}
		seen[c.ID] = true
		if !c.Suit.Valid() {
			t.Fatalf("%s has an unknown suit", c.ID)
		}
		if c.Rank.Strength() < 0 {
			t.Fatalf("%s has an unknown rank", c.ID)
		}
		if got, ok := Lookup(c.ID); !ok || got != c {
			t.Fatalf("%s does not look up to itself", c.ID)
		}
		perSuit[c.Suit]++
	}
	for _, s := range Suits {
		if perSuit[s] != 13 {
			t.Fatalf("%s has %d cards, want 13", s, perSuit[s])
		}
	}
	if _, ok := Lookup("spades-1"); ok {
		t.Fatal("a card that is not in the deck must not resolve")
	}
}

// Spec rule 6: A K Q J 10 9 8 7 6 5 4 3 2, highest first.
func TestRankOrder(t *testing.T) {
	want := []Rank{"a", "k", "q", "j", "10", "9", "8", "7", "6", "5", "4", "3", "2"}
	got := slices.Clone(Ranks)
	slices.Reverse(got)
	if !slices.Equal(got, want) {
		t.Fatalf("ranks high to low are %v, want %v", got, want)
	}
	if Rank("a").Strength() <= Rank("k").Strength() || Rank("2").Strength() != 0 {
		t.Fatal("the ace must be the strongest and the two the weakest")
	}
}

// Spec rule 26: trump beats any plain card, the lead suit beats other plain
// suits, and within a suit the higher rank wins.
func TestCardBeats(t *testing.T) {
	const lead, trump = Hearts, Spades
	card := func(id CardID) Card { return mustCard(id) }
	cases := []struct {
		name          string
		played, best  CardID
		shouldOutrank bool
	}{
		{"higher card of the lead suit wins", "hearts-k", "hearts-9", true},
		{"lower card of the lead suit loses", "hearts-3", "hearts-9", false},
		{"a low trump beats the highest plain card", "spades-2", "hearts-a", true},
		{"a higher trump beats a lower trump", "spades-10", "spades-4", true},
		{"a lower trump loses to a higher trump", "spades-4", "spades-10", false},
		{"a plain card never beats trump", "hearts-a", "spades-2", false},
		{"an off-suit card cannot win", "clubs-a", "hearts-2", false},
		{"an off-suit card cannot beat another off-suit winner", "clubs-a", "diamonds-2", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := card(tc.played).Beats(card(tc.best), lead, trump); got != tc.shouldOutrank {
				t.Fatalf("%s beats %s = %v, want %v", tc.played, tc.best, got, tc.shouldOutrank)
			}
		})
	}
}

// Spec rules 1-3: four seats, two teams, partners opposite, order wraps.
func TestSeating(t *testing.T) {
	if TeamOf(0) != TeamOf(2) || TeamOf(1) != TeamOf(3) || TeamOf(0) == TeamOf(1) {
		t.Fatal("seats 0 and 2 are one team, seats 1 and 3 the other")
	}
	for seat := range Seats {
		if PartnerOf(seat) == seat || TeamOf(PartnerOf(seat)) != TeamOf(seat) {
			t.Fatalf("seat %d has the wrong partner %d", seat, PartnerOf(seat))
		}
		if PartnerOf(PartnerOf(seat)) != seat {
			t.Fatal("partnership must be mutual")
		}
	}
	if SeatsOfTeam(0) != [2]int{0, 2} || SeatsOfTeam(1) != [2]int{1, 3} {
		t.Fatal("team seats are wrong")
	}
	order := []int{0}
	for seat := next(0); seat != 0; seat = next(seat) {
		order = append(order, seat)
	}
	if !slices.Equal(order, []int{0, 1, 2, 3}) {
		t.Fatalf("play order is %v, want 0,1,2,3", order)
	}
}

// Spec rule 8: the deal is unpredictable without the seed, reproducible with
// it, and different for each deal in the same match.
func TestShuffleIsSeeded(t *testing.T) {
	ids := func() []CardID {
		var out []CardID
		for _, c := range Manifest() {
			out = append(out, c.ID)
		}
		return out
	}
	seed := make([]byte, SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	a, b, other := ids(), ids(), ids()
	shuffle(a, seed, 0)
	shuffle(b, seed, 0)
	if !slices.Equal(a, b) {
		t.Fatal("the same seed and counter must deal the same cards")
	}
	shuffle(other, seed, 1)
	if slices.Equal(a, other) {
		t.Fatal("a later deal in the same match must differ")
	}
	if slices.Equal(a, ids()) {
		t.Fatal("the shuffle did not change the order")
	}
	sorted := slices.Clone(a)
	slices.Sort(sorted)
	want := ids()
	slices.Sort(want)
	if !slices.Equal(sorted, want) {
		t.Fatal("the shuffle must keep exactly the 52 cards")
	}

	// The toss must be uniform over the two teams and stable for one match.
	counts := [2]int{}
	for i := range 200 {
		s := make([]byte, SeedSize)
		s[0] = byte(i)
		s[1] = byte(i >> 8)
		counts[pick(Teams, s, 0)]++
	}
	if counts[0] < 60 || counts[1] < 60 {
		t.Fatalf("the toss looks biased: %v", counts)
	}
	if pick(Teams, seed, 0) != pick(Teams, seed, 0) {
		t.Fatal("the toss must be stable for one match")
	}
}
