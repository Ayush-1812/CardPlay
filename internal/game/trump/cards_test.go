package trump

import (
	"bytes"
	"crypto/rand"
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

// The shuffle is a function of the bytes it is given: the same bytes deal
// the same cards, different bytes deal different ones, and either way the
// deck is still exactly the 52 cards it started with.
func TestShuffleDependsOnlyOnItsSource(t *testing.T) {
	ids := func() []CardID {
		var out []CardID
		for _, c := range Manifest() {
			out = append(out, c.ID)
		}
		return out
	}
	a, b, other := ids(), ids(), ids()
	if err := shuffle(a, stream(7)); err != nil {
		t.Fatal(err)
	}
	if err := shuffle(b, stream(7)); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a, b) {
		t.Fatal("the same bytes must produce the same deal")
	}
	if err := shuffle(other, stream(8)); err != nil {
		t.Fatal(err)
	}
	if slices.Equal(a, other) {
		t.Fatal("different bytes must produce a different deal")
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
	// A source that runs out is an error, not a half-shuffled deck.
	short := ids()
	if err := shuffle(short, bytes.NewReader([]byte{1, 2, 3})); err == nil {
		t.Fatal("a truncated source must be reported")
	}
}

// index must not fold an out-of-range draw back into the range: folding is
// what makes the low cards likelier than the high ones.
func TestIndexRejectsRatherThanFolds(t *testing.T) {
	// For n=3 the mask is 0b11, so a drawn 3 is out of range. It must be
	// thrown away and the next draw used, not turned into 0.
	r := bytes.NewReader([]byte{0, 0, 0, 3, 0, 0, 0, 1})
	got, err := index(3, r)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("index returned %d; the rejected draw was folded back in", got)
	}
	// One outcome needs no randomness at all, so nothing is read.
	rest := bytes.NewReader(nil)
	if got, err := index(1, rest); err != nil || got != 0 {
		t.Fatalf("index(1) = %d, %v", got, err)
	}

	// Over many draws each of three outcomes should come up about as often.
	counts := [3]int{}
	for range 3000 {
		v, err := index(3, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		counts[v]++
	}
	for i, n := range counts {
		if n < 850 || n > 1150 {
			t.Errorf("outcome %d came up %d times in 3000; expected about 1000", i, n)
		}
	}

	// The toss, which uses the same draw, reaches both teams.
	teams := [2]int{}
	for range 200 {
		team, err := index(Teams, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		teams[team]++
	}
	if teams[0] < 60 || teams[1] < 60 {
		t.Fatalf("the toss looks biased: %v", teams)
	}
}
