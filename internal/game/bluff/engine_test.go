package bluff

import (
	"crypto/rand"
	"io"
	mrand "math/rand/v2"
	"slices"
	"testing"
	"time"
)

// stream is a deterministic source for tests: the same seed always deals
// the same cards, and unlike a fixed slice of bytes it never runs out part
// way through a shuffle.
func stream(seed byte) io.Reader {
	var key [32]byte
	for i := range key {
		key[i] = seed
	}
	return mrand.NewChaCha8(key)
}

func users(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = string(rune('a' + i))
	}
	return out
}

var when = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func ok(t *testing.T, s *State, seat int, a Action, now time.Time) *State {
	t.Helper()
	next, _, err := Apply(s, seat, a, now)
	if err != nil {
		t.Fatalf("seat %d: %v", seat, err)
	}
	return next
}

func rejected(t *testing.T, s *State, seat int, a Action, code string) {
	t.Helper()
	_, _, err := Apply(s, seat, a, when)
	var re *RuleError
	if err == nil {
		t.Fatalf("seat %d: the action was accepted; want %s", seat, code)
	}
	if !asRule(err, &re) || re.Code != code {
		t.Fatalf("seat %d: refused with %v; want %s", seat, err, code)
	}
}

func asRule(err error, target **RuleError) bool {
	re, ok := err.(*RuleError)
	if ok {
		*target = re
	}
	return ok
}

// table builds a state with hands chosen by the test, so a rule can be
// exercised without hunting for the right deal.
func table(t *testing.T, rank Rank, turn int, hands ...[]CardID) *State {
	t.Helper()
	s := &State{
		Schema: SchemaVersion, Decks: 1, Phase: PhasePlay, Rank: rank, Turn: turn,
		Pile: []CardID{}, Accepted: make([]bool, len(hands)), Pending: -1, Winner: -1,
	}
	held := map[CardID]bool{}
	for i, hand := range hands {
		s.Players = append(s.Players, Player{UserID: string(rune('a' + i)), Hand: slices.Clone(hand)})
		for _, id := range hand {
			held[id] = true
		}
	}
	// Everything nobody was given starts on the pile, so the deck is whole
	// and a collection has something to move.
	for _, c := range Manifest() {
		if !held[c.ID] {
			s.Pile = append(s.Pile, c.ID)
		}
	}
	if err := s.CheckInvariants(); err != nil {
		t.Fatalf("fixture is invalid: %v", err)
	}
	return s
}

// Section 2: the whole deck is dealt, as evenly as the player count allows,
// and every card is somewhere.
func TestDealShapes(t *testing.T) {
	for _, tc := range []struct {
		players int
		want    []int
	}{
		{3, []int{18, 17, 17}},
		{4, []int{13, 13, 13, 13}},
		{5, []int{11, 11, 10, 10, 10}},
		{6, []int{9, 9, 9, 9, 8, 8}},
	} {
		s, _, err := NewGame(users(tc.players), 1, stream(byte(tc.players)))
		if err != nil {
			t.Fatal(err)
		}
		sizes := make([]int, 0, tc.players)
		seen := map[CardID]bool{}
		for _, p := range s.Players {
			sizes = append(sizes, len(p.Hand))
			for _, id := range p.Hand {
				if seen[id] {
					t.Fatalf("%d players: %s was dealt twice", tc.players, id)
				}
				seen[id] = true
			}
		}
		if len(seen) != DeckSize {
			t.Fatalf("%d players: %d cards dealt, want %d", tc.players, len(seen), DeckSize)
		}
		slices.Sort(sizes)
		slices.Reverse(sizes)
		if !slices.Equal(sizes, tc.want) {
			t.Fatalf("%d players: hands %v, want %v", tc.players, sizes, tc.want)
		}
		if len(s.Pile) != 0 {
			t.Fatalf("%d players: the pile starts with %d cards", tc.players, len(s.Pile))
		}
		if s.Rank != Ace {
			t.Fatalf("%d players: the first rank is %s, want an Ace", tc.players, s.Rank)
		}
		if err := s.CheckInvariants(); err != nil {
			t.Fatal(err)
		}
	}
	for _, bad := range [][]string{users(2), users(7), {"a", "a", "b"}, {"a", "", "b"}} {
		if _, _, err := NewGame(bad, 1, rand.Reader); err != ErrPlayers {
			t.Fatalf("%v should be refused, got %v", bad, err)
		}
	}
}

// The extra cards of an uneven deal must not always fall on the same seats,
// and the lead must not always be the seat dealt to first.
func TestDealStartsSomewhereDifferent(t *testing.T) {
	firstBiggest := map[int]int{}
	leads := map[int]int{}
	for i := range 400 {
		s, _, err := NewGame(users(5), 1, stream(byte(i)))
		if err != nil {
			t.Fatal(err)
		}
		biggest, size := 0, 0
		for seat, p := range s.Players {
			if len(p.Hand) > size {
				biggest, size = seat, len(p.Hand)
			}
		}
		firstBiggest[biggest]++
		leads[s.Turn]++
	}
	if len(firstBiggest) < 4 {
		t.Fatalf("the extra cards only ever reached %d seats: %v", len(firstBiggest), firstBiggest)
	}
	if len(leads) < 4 {
		t.Fatalf("only %d seats ever led: %v", len(leads), leads)
	}
}

// Section 3-4: a play may be anything the player holds; what is checked is
// ownership, the count, the turn and the phase.
func TestPlayAcceptsAnyCardsTheHandHolds(t *testing.T) {
	s := table(t, Queen, 0,
		[]CardID{"q-hearts", "q-clubs", "k-spades", "2-hearts"},
		[]CardID{"3-spades"},
		[]CardID{"4-spades"},
	)
	// A mismatched card is a legal play, not an error.
	bluffed := ok(t, s, 0, PlayCards{Cards: []CardID{"k-spades"}}, when)
	if bluffed.Phase != PhaseChallenge || bluffed.Last.Count != 1 || bluffed.Last.Rank != Queen {
		t.Fatalf("the play was not recorded as one Queen: %+v", bluffed.Last)
	}
	if slices.Contains(bluffed.Players[0].Hand, CardID("k-spades")) {
		t.Fatal("the card did not leave the hand")
	}
	if got := bluffed.Pile[len(bluffed.Pile)-1]; got != "k-spades" {
		t.Fatalf("the pile's top card is %s", got)
	}

	// Mixed, and the full four.
	mixed := ok(t, s, 0, PlayCards{Cards: []CardID{"q-hearts", "k-spades", "2-hearts", "q-clubs"}}, when)
	if mixed.Last.Count != 4 {
		t.Fatalf("count %d", mixed.Last.Count)
	}

	rejected(t, s, 0, PlayCards{Cards: nil}, CodeIllegal)
	rejected(t, s, 0, PlayCards{Cards: []CardID{"q-hearts", "q-clubs", "k-spades", "2-hearts", "2-hearts"}}, CodeIllegal)
	rejected(t, s, 0, PlayCards{Cards: []CardID{"q-hearts", "q-hearts"}}, CodeIllegal)
	rejected(t, s, 0, PlayCards{Cards: []CardID{"a-spades"}}, CodeIllegal)
	rejected(t, s, 0, PlayCards{Cards: []CardID{"not-a-card"}}, CodeIllegal)
	rejected(t, s, 1, PlayCards{Cards: []CardID{"3-spades"}}, CodeNotYourTurn)
	// Nobody may play while a challenge window is open.
	rejected(t, bluffed, 1, PlayCards{Cards: []CardID{"3-spades"}}, CodeWrongPhase)
}

// Section 3: the rank advances after every play that ends, and wraps.
func TestRankAdvancesAndWraps(t *testing.T) {
	s := table(t, King, 0,
		[]CardID{"k-hearts", "k-clubs"},
		[]CardID{"3-spades", "4-spades"},
		[]CardID{"5-spades", "6-spades"},
	)
	after := ok(t, s, 0, PlayCards{Cards: []CardID{"k-hearts"}}, when)
	after = ok(t, after, 1, AcceptPlay{PlayID: after.Last.ID}, when)
	after = ok(t, after, 2, AcceptPlay{PlayID: after.Last.ID}, when)
	if after.Rank != Ace {
		t.Fatalf("after Kings the table plays %s, want Aces", after.Rank)
	}
	if after.Turn != 1 {
		t.Fatalf("turn went to seat %d, want 1", after.Turn)
	}
	// And on through the cycle.
	next := ok(t, after, 1, PlayCards{Cards: []CardID{"3-spades"}}, when)
	next = ok(t, next, 2, AcceptPlay{}, when)
	next = ok(t, next, 0, AcceptPlay{}, when)
	if next.Rank != Two {
		t.Fatalf("rank %s, want a Two", next.Rank)
	}
}

// Section 6: a truthful claim costs the challenger the whole pile; a false
// one costs the player. Only the latest batch is revealed either way.
func TestChallengeOutcomes(t *testing.T) {
	t.Run("truthful: the challenger collects", func(t *testing.T) {
		s := table(t, Queen, 0,
			[]CardID{"q-hearts", "q-clubs", "9-spades"},
			[]CardID{"3-spades"},
			[]CardID{"4-spades"},
		)
		pileBefore := len(s.Pile)
		played := ok(t, s, 0, PlayCards{Cards: []CardID{"q-hearts", "q-clubs"}}, when)
		after := ok(t, played, 1, CallBluff{PlayID: played.Last.ID}, when)
		if !after.Result.Truthful || after.Result.Collector != 1 {
			t.Fatalf("result %+v", after.Result)
		}
		if len(after.Pile) != 0 {
			t.Fatalf("the pile still holds %d cards", len(after.Pile))
		}
		if got := len(after.Players[1].Hand); got != 1+pileBefore+2 {
			t.Fatalf("the challenger holds %d cards, want %d", got, 1+pileBefore+2)
		}
		// Only the challenged batch is revealed.
		if !slices.Equal(after.Result.Cards, []CardID{"q-hearts", "q-clubs"}) {
			t.Fatalf("revealed %v", after.Result.Cards)
		}
		if after.Turn != 1 || after.Rank != King {
			t.Fatalf("play continues at seat %d with %s", after.Turn, after.Rank)
		}
	})

	t.Run("false: the player collects", func(t *testing.T) {
		s := table(t, Seven, 0,
			[]CardID{"7-hearts", "k-clubs"},
			[]CardID{"3-spades"},
			[]CardID{"4-spades"},
		)
		played := ok(t, s, 0, PlayCards{Cards: []CardID{"7-hearts", "k-clubs"}}, when)
		after := ok(t, played, 2, CallBluff{}, when)
		if after.Result.Truthful || after.Result.Collector != 0 {
			t.Fatalf("result %+v", after.Result)
		}
		if len(after.Players[0].Hand) != len(played.Pile) {
			t.Fatalf("the player holds %d cards, want the pile's %d", len(after.Players[0].Hand), len(played.Pile))
		}
		// Play continues after the player who was challenged, not the
		// challenger.
		if after.Turn != 1 {
			t.Fatalf("turn went to seat %d, want 1", after.Turn)
		}
		if after.Rank != Eight {
			t.Fatalf("rank %s, want an Eight", after.Rank)
		}
	})

	t.Run("the whole pile moves, not only the batch", func(t *testing.T) {
		s := table(t, Ace, 0,
			[]CardID{"a-hearts"},
			[]CardID{"2-spades"},
			[]CardID{"3-spades"},
		)
		buried := len(s.Pile)
		if buried == 0 {
			t.Fatal("this test needs cards already on the pile")
		}
		played := ok(t, s, 0, PlayCards{Cards: []CardID{"a-hearts"}}, when)
		after := ok(t, played, 1, CallBluff{}, when)
		if got := len(after.Players[1].Hand); got != 1+buried+1 {
			t.Fatalf("the collector took %d cards, want the whole pile of %d", got-1, buried+1)
		}
		if len(after.Result.Cards) != 1 {
			t.Fatalf("%d cards were revealed, want only the latest batch", len(after.Result.Cards))
		}
	})
}

// Section 10: accepting binds, only the first challenge counts, and a stale
// play id is refused.
func TestChallengeWindowRules(t *testing.T) {
	s := table(t, Ace, 0,
		[]CardID{"a-hearts", "a-clubs"},
		[]CardID{"2-spades"},
		[]CardID{"3-spades"},
		[]CardID{"4-spades"},
	)
	played := ok(t, s, 0, PlayCards{Cards: []CardID{"a-hearts"}}, when)

	// The player cannot answer their own play, either way.
	rejected(t, played, 0, AcceptPlay{}, CodeNotEligible)
	rejected(t, played, 0, CallBluff{}, CodeNotEligible)

	accepted := ok(t, played, 1, AcceptPlay{}, when)
	// Accepting binds: no second thoughts, and no accepting twice.
	rejected(t, accepted, 1, CallBluff{}, CodeNotEligible)
	rejected(t, accepted, 1, AcceptPlay{}, CodeNotEligible)

	// A play id from an earlier play is refused rather than applied to this
	// one.
	rejected(t, accepted, 2, CallBluff{PlayID: "p0"}, CodeStalePlay)
	rejected(t, accepted, 2, AcceptPlay{PlayID: "p99"}, CodeStalePlay)

	// Two challengers racing: each is applied to its own copy of the state,
	// and the platform commits one. Neither may be applied twice.
	first := ok(t, accepted, 2, CallBluff{}, when)
	rejected(t, first, 3, CallBluff{}, CodeWrongPhase)
	rejected(t, first, 2, CallBluff{}, CodeWrongPhase)

	// The window closes early once every eligible player has accepted --
	// all three of them, not the first two.
	two := ok(t, accepted, 2, AcceptPlay{}, when)
	if two.Phase != PhaseChallenge {
		t.Fatal("the window closed with a player still to answer")
	}
	all := ok(t, two, 3, AcceptPlay{}, when)
	if all.Phase != PhasePlay {
		t.Fatalf("phase %s with one player still to answer", all.Phase)
	}
	if all.Turn != 1 || all.Rank != Two {
		t.Fatalf("after the window: seat %d plays %s", all.Turn, all.Rank)
	}
}

// Section 10: silence is acceptance, but only once the deadline has passed.
func TestExpiryClosesTheWindow(t *testing.T) {
	s := table(t, Ace, 0,
		[]CardID{"k-hearts", "a-clubs"},
		[]CardID{"2-spades"},
		[]CardID{"3-spades"},
	)
	played := ok(t, s, 0, PlayCards{Cards: []CardID{"k-hearts"}}, when)
	if !played.Deadline.Equal(when.Add(ChallengeWindow)) {
		t.Fatalf("deadline %s", played.Deadline)
	}
	// Too early.
	if _, err := played.clone().Expire(when.Add(ChallengeWindow - time.Millisecond)); err == nil {
		t.Fatal("the window closed before its deadline")
	}
	next := played.clone()
	if _, err := next.Expire(when.Add(ChallengeWindow)); err != nil {
		t.Fatal(err)
	}
	if next.Phase != PhasePlay || next.Turn != 1 || next.Rank != Two {
		t.Fatalf("after expiry: phase %s, seat %d, rank %s", next.Phase, next.Turn, next.Rank)
	}
	// The bluff stays buried: nothing in the state says it was one.
	if next.Result != nil {
		t.Fatal("an unchallenged play must not be revealed")
	}
	if _, err := next.Expire(when.Add(time.Hour)); err == nil {
		t.Fatal("expiring twice must be refused")
	}
}

// Section 8: the last card is a pending win, confirmed or lost by what
// happens next.
func TestWinningOnTheLastCard(t *testing.T) {
	t.Run("unchallenged, even as a bluff", func(t *testing.T) {
		s := table(t, Ace, 0,
			[]CardID{"k-hearts"},
			[]CardID{"2-spades"},
			[]CardID{"3-spades"},
		)
		played := ok(t, s, 0, PlayCards{Cards: []CardID{"k-hearts"}}, when)
		if played.Pending != 0 || played.Winner != -1 {
			t.Fatalf("pending %d winner %d", played.Pending, played.Winner)
		}
		done := played.clone()
		if _, err := done.Expire(when.Add(ChallengeWindow)); err != nil {
			t.Fatal(err)
		}
		if done.Winner != 0 || done.Phase != PhaseOver {
			t.Fatalf("winner %d phase %s", done.Winner, done.Phase)
		}
		// The winning bluff is never revealed.
		if done.Result != nil {
			t.Fatal("the last play must stay face down")
		}
	})

	t.Run("challenged and truthful: the win stands", func(t *testing.T) {
		s := table(t, Ace, 0,
			[]CardID{"a-hearts"},
			[]CardID{"2-spades"},
			[]CardID{"3-spades"},
		)
		played := ok(t, s, 0, PlayCards{Cards: []CardID{"a-hearts"}}, when)
		after := ok(t, played, 1, CallBluff{}, when)
		if after.Winner != 0 || after.Phase != PhaseOver {
			t.Fatalf("winner %d phase %s", after.Winner, after.Phase)
		}
		if after.Result.Collector != 1 || len(after.Players[1].Hand) == 1 {
			t.Fatal("the challenger must still collect the pile")
		}
	})

	t.Run("challenged and false: the game continues", func(t *testing.T) {
		s := table(t, Ace, 0,
			[]CardID{"k-hearts"},
			[]CardID{"2-spades"},
			[]CardID{"3-spades"},
		)
		played := ok(t, s, 0, PlayCards{Cards: []CardID{"k-hearts"}}, when)
		after := ok(t, played, 1, CallBluff{}, when)
		if after.Winner != -1 || after.Phase != PhasePlay {
			t.Fatalf("winner %d phase %s", after.Winner, after.Phase)
		}
		if after.Pending != -1 {
			t.Fatal("the pending win must be gone")
		}
		if len(after.Players[0].Hand) == 0 {
			t.Fatal("the caught player must be holding the pile")
		}
		if after.Turn != 1 {
			t.Fatalf("turn %d, want the seat after the player", after.Turn)
		}
	})

	t.Run("nothing happens after the game is over", func(t *testing.T) {
		s := table(t, Ace, 0, []CardID{"a-hearts"}, []CardID{"2-spades"}, []CardID{"3-spades"})
		played := ok(t, s, 0, PlayCards{Cards: []CardID{"a-hearts"}}, when)
		done := ok(t, played, 1, CallBluff{}, when)
		rejected(t, done, 1, PlayCards{Cards: []CardID{"2-spades"}}, CodeWrongPhase)
		rejected(t, done, 2, CallBluff{}, CodeWrongPhase)
	})
}

// A full game played to a winner keeps all 52 cards and every invariant.
func TestAGameRunsToAWinner(t *testing.T) {
	s, _, err := NewGame(users(4), 1, stream(3))
	if err != nil {
		t.Fatal(err)
	}
	now := when
	for move := 0; s.Phase != PhaseOver; move++ {
		if move > 5000 {
			t.Fatal("the game did not finish")
		}
		switch s.Phase {
		case PhasePlay:
			hand := s.Players[s.Turn].Hand
			n := min(s.maxPlay(), len(hand))
			s = ok(t, s, s.Turn, PlayCards{Cards: slices.Clone(hand[:n])}, now)
		case PhaseChallenge:
			// A couple of challenges early on, then the table settles. If
			// every play were challenged the pile would always return to a
			// hand and nobody could ever run out, which is a property of
			// the rules worth knowing.
			if move == 2 || move == 9 {
				s = ok(t, s, s.next(s.Last.Seat), CallBluff{}, now)
				break
			}
			for _, seat := range s.pendingSeats() {
				s = ok(t, s, seat, AcceptPlay{}, now)
			}
		}
		now = now.Add(time.Second)
		if err := s.CheckInvariants(); err != nil {
			t.Fatalf("move %d: %v", move, err)
		}
	}
	if len(s.Players[s.Winner].Hand) != 0 {
		t.Fatal("the winner is holding cards")
	}
	total := len(s.Pile)
	for _, p := range s.Players {
		total += len(p.Hand)
	}
	if total != DeckSize {
		t.Fatalf("%d cards at the end, want %d", total, DeckSize)
	}
}

// More than one deck: every copy is its own card, the whole lot is dealt,
// and a play may name as many as the decks allow.
func TestMoreDecks(t *testing.T) {
	for decks := 1; decks <= MaxDecks; decks++ {
		s, _, err := NewGame(users(4), decks, stream(byte(decks)))
		if err != nil {
			t.Fatal(err)
		}
		seen := map[CardID]bool{}
		held := 0
		for _, p := range s.Players {
			for _, id := range p.Hand {
				if seen[id] {
					t.Fatalf("%d decks: %s was dealt twice", decks, id)
				}
				if _, ok := Lookup(id); !ok {
					t.Fatalf("%d decks: %s is not a card", decks, id)
				}
				seen[id] = true
				held++
			}
		}
		if want := DeckSize * decks; held != want {
			t.Fatalf("%d decks: %d cards dealt, want %d", decks, held, want)
		}
		if got := s.maxPlay(); got != CopiesPerRank*decks {
			t.Fatalf("%d decks: a play may hold %d cards, want %d", decks, got, CopiesPerRank*decks)
		}
		if err := s.CheckInvariants(); err != nil {
			t.Fatalf("%d decks: %v", decks, err)
		}
	}
	// Out of range is refused rather than quietly clamped.
	for _, bad := range []int{0, -1, MaxDecks + 1} {
		if _, _, err := NewGame(users(4), bad, rand.Reader); err != ErrDecks {
			t.Fatalf("%d decks should be refused, got %v", bad, err)
		}
	}
}

// Two copies of one card are two cards: both can be held, both can be
// played together, and a challenge reads each on its own.
func TestDuplicateCardsAreDistinct(t *testing.T) {
	// A two-deck table: three hands, and the other 104 minus those on the
	// pile, so the deck is whole.
	hands := [][]CardID{
		{"q-hearts", "q-hearts#2", "k-spades"},
		{"3-spades"},
		{"4-spades"},
	}
	s := &State{
		Schema: SchemaVersion, Decks: 2, Phase: PhasePlay, Rank: Queen, Turn: 0,
		Pile: []CardID{}, Accepted: make([]bool, len(hands)), Pending: -1, Winner: -1,
	}
	held := map[CardID]bool{}
	for i, hand := range hands {
		s.Players = append(s.Players, Player{UserID: string(rune('a' + i)), Hand: slices.Clone(hand)})
		for _, id := range hand {
			held[id] = true
		}
	}
	for _, id := range BuildDeck(2) {
		if !held[id] {
			s.Pile = append(s.Pile, id)
		}
	}
	if err := s.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
	// Both queens, from different decks, are a truthful pair.
	played := ok(t, s, 0, PlayCards{Cards: []CardID{"q-hearts", "q-hearts#2"}}, when)
	if played.Last.Count != 2 {
		t.Fatalf("count %d", played.Last.Count)
	}
	after := ok(t, played, 1, CallBluff{}, when)
	if !after.Result.Truthful {
		t.Fatal("two queens from two decks are still two queens")
	}
	// Naming the same copy twice is still refused.
	rejected(t, s, 0, PlayCards{Cards: []CardID{"q-hearts", "q-hearts"}}, CodeIllegal)
}
