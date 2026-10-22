package trump

import (
	"crypto/rand"
	"math"
	"slices"
	"sync"
	"testing"
)

// layout returns every card in play, by where it is: a hand or the undealt
// rest. It is the whole deck, which is what conservation means here.
func layout(s *State) map[CardID]string {
	out := map[CardID]string{}
	for seat, p := range s.Players {
		for _, id := range p.Hand {
			out[id] = string(rune('0' + seat))
		}
	}
	for _, id := range s.Rest {
		out[id] = "rest"
	}
	return out
}

// A deal puts all 52 cards somewhere, exactly once: five in each hand and
// the other thirty-two waiting for the second deal.
func TestDealUsesTheWholeDeckExactlyOnce(t *testing.T) {
	s := liveMatch(t)
	where := layout(s)
	if len(where) != 52 {
		t.Fatalf("%d distinct cards are in play, want 52", len(where))
	}
	for _, c := range Manifest() {
		if _, ok := where[c.ID]; !ok {
			t.Fatalf("%s was not dealt anywhere", c.ID)
		}
	}
	for seat, p := range s.Players {
		if len(p.Hand) != FirstDeal {
			t.Fatalf("seat %d holds %d cards, want %d", seat, len(p.Hand), FirstDeal)
		}
	}
	if len(s.Rest) != 52-Seats*FirstDeal {
		t.Fatalf("the rest holds %d cards, want %d", len(s.Rest), 52-Seats*FirstDeal)
	}

	// The second deal comes from the same shuffled deck: nothing is
	// reshuffled, and everyone ends with thirteen.
	after := pickTrump(t, s, Spades)
	for seat, p := range after.Players {
		if len(p.Hand) != CardsPerPlayer {
			t.Fatalf("seat %d holds %d cards after the second deal, want %d", seat, len(p.Hand), CardsPerPlayer)
		}
	}
	if len(after.Rest) != 0 {
		t.Fatalf("%d cards were left undealt", len(after.Rest))
	}
	seen := map[CardID]bool{}
	for _, p := range after.Players {
		for _, id := range p.Hand {
			if seen[id] {
				t.Fatalf("%s was dealt twice", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != 52 {
		t.Fatalf("%d cards are held, want 52", len(seen))
	}
	// Every card a player holds after the second deal was in the rest or
	// their own first five; none appeared from nowhere.
	for seat, p := range after.Players {
		for _, id := range p.Hand {
			if where[id] != "rest" && where[id] != string(rune('0'+seat)) {
				t.Fatalf("seat %d holds %s, which was dealt to %s", seat, id, where[id])
			}
		}
	}
}

// Each round deals a fresh deck. Nothing is carried over, and two rounds of
// the same match are not the same deal.
func TestEveryRoundIsDealtFresh(t *testing.T) {
	s := liveMatch(t)
	seen := map[string]int{}
	const rounds = 40
	for round := 1; round <= rounds; round++ {
		where := layout(s)
		if len(where) != 52 {
			t.Fatalf("round %d has %d cards in play, want 52", round, len(where))
		}
		// A deal is identified by the concatenation of the four hands.
		key := ""
		for _, p := range s.Players {
			hand := slices.Clone(p.Hand)
			slices.Sort(hand)
			for _, id := range hand {
				key += string(id) + ","
			}
			key += "|"
		}
		if before, repeat := seen[key]; repeat {
			t.Fatalf("round %d dealt exactly the hands of round %d", round, before)
		}
		seen[key] = round
		s = playRound(t, s, Spades)
		for seat := range s.Players {
			s = ok(t, s, seat, ReadyRound{})
		}
	}
}

// A distribution smoke test. Over many deals every card should reach every
// seat about as often as any other. This can catch a stuck or skewed
// shuffle; it cannot prove randomness, and is not meant to.
func TestDealDistributionLooksEven(t *testing.T) {
	if testing.Short() {
		t.Skip("distribution check runs the full suite only")
	}
	const deals = 20_000
	counts := map[CardID]int{}
	hands := map[string]bool{}
	repeats := 0
	for i := 0; i < deals; i++ {
		s, _, err := NewGame([]string{"u0", "u1", "u2", "u3"}, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range s.Players[0].Hand {
			counts[id]++
		}
		for _, p := range s.Players {
			hand := slices.Clone(p.Hand)
			slices.Sort(hand)
			key := ""
			for _, id := range hand {
				key += string(id) + ","
			}
			if hands[key] {
				repeats++
			}
			hands[key] = true
		}
	}
	if len(counts) != 52 {
		t.Fatalf("only %d of the 52 cards ever reached seat 0 in %d deals", len(counts), deals)
	}
	// Each of the 52 cards should land in seat 0's five about deals*5/52
	// times. The bound is six standard deviations: wide enough never to
	// flake on a sound shuffle, narrow enough to catch a broken one.
	expected := float64(deals) * float64(FirstDeal) / 52
	sd := math.Sqrt(float64(deals) * (float64(FirstDeal) / 52) * (1 - float64(FirstDeal)/52))
	low, high := expected-6*sd, expected+6*sd
	chi := 0.0
	minimum, maximum := math.MaxInt, 0
	for _, c := range Manifest() {
		n := counts[c.ID]
		minimum, maximum = min(minimum, n), max(maximum, n)
		d := float64(n) - expected
		chi += d * d / expected
		if float64(n) < low || float64(n) > high {
			t.Errorf("%s reached seat 0 %d times; expected about %.0f (%.0f-%.0f)", c.ID, n, expected, low, high)
		}
	}
	// Reported, not asserted as proof: with 51 degrees of freedom a sound
	// shuffle sits near 51 and practically never exceeds 150.
	t.Logf("seat 0 over %d deals: expected %.1f per card, observed %d-%d, chi-square %.1f (51 df)",
		deals, expected, minimum, maximum, chi)
	if chi > 150 {
		t.Errorf("chi-square %.1f over 51 degrees of freedom is far past what a sound shuffle produces", chi)
	}

	// Repeated hands are expected, and this says how many. There are
	// C(52,5) = 2,598,960 possible five-card hands, so among n hands about
	// n^2/2C of them repeat one already dealt -- the birthday problem. A
	// player who thinks the deal looks familiar is usually right, and the
	// shuffle is still sound. Far fewer repeats than this would mean
	// something was forcing variety; far more would mean a stuck shuffle.
	const possible = 2_598_960.0
	drawn := float64(deals * Seats)
	expectedRepeats := drawn * drawn / (2 * possible)
	spread := 6 * math.Sqrt(expectedRepeats)
	t.Logf("%.0f hands dealt: %d were a repeat of one already seen; the birthday expectation is %.0f",
		drawn, repeats, expectedRepeats)
	if float64(repeats) < expectedRepeats-spread || float64(repeats) > expectedRepeats+spread {
		t.Errorf("%d repeated hands in %.0f; expected about %.0f, which suggests the deal is not being drawn uniformly",
			repeats, drawn, expectedRepeats)
	}
}

// Applying the same action from several goroutines at once must not deal
// twice into one state or leave the state everyone started from altered.
// The platform serialises commands per match as well; this is the engine
// holding its own end up.
func TestConcurrentApplyCannotDealTwice(t *testing.T) {
	s := liveMatch(t)
	before := layout(s)
	chooser := SeatsOfTeam(s.EntitledTeam)[0]

	const racers = 16
	var wg sync.WaitGroup
	results := make([]*State, racers)
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			next, _, err := Apply(s, chooser, ChooseTrump{Suit: Spades})
			results[i], errs[i] = next, err
		}(i)
	}
	wg.Wait()

	// The state they all started from is untouched: Apply works on a clone.
	if after := layout(s); len(after) != len(before) {
		t.Fatalf("the shared state changed: %d cards before, %d after", len(before), len(after))
	}
	for id, place := range before {
		if layout(s)[id] != place {
			t.Fatalf("%s moved from %s while applying concurrently", id, place)
		}
	}
	// Every result is a complete, self-consistent deal of the same deck.
	for i, next := range results {
		if errs[i] != nil {
			t.Fatalf("racer %d failed: %v", i, errs[i])
		}
		if err := next.CheckInvariants(); err != nil {
			t.Fatalf("racer %d produced an invalid state: %v", i, err)
		}
		held := map[CardID]bool{}
		for _, p := range next.Players {
			for _, id := range p.Hand {
				if held[id] {
					t.Fatalf("racer %d dealt %s twice", i, id)
				}
				held[id] = true
			}
		}
		if len(held) != 52 {
			t.Fatalf("racer %d dealt %d cards, want 52", i, len(held))
		}
	}
}

// newMatch starts a match from the real source of randomness.
func liveMatch(t *testing.T) *State {
	t.Helper()
	s, _, err := NewGame([]string{"u0", "u1", "u2", "u3"}, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
