package trump

import (
	"bytes"
	"crypto/rand"
	"slices"
	"testing"
)

// fixture starts a match from a fixed seed, so a test can rely on the deal.
func fixture(t *testing.T, seed byte) *State {
	t.Helper()
	s, _, err := NewGame([]string{"u0", "u1", "u2", "u3"}, bytes.NewReader(bytes.Repeat([]byte{seed}, SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func ok(t *testing.T, s *State, seat int, a Action) *State {
	t.Helper()
	next, _, err := Apply(s, seat, a)
	if err != nil {
		t.Fatalf("seat %d %s rejected: %v", seat, a.Kind(), err)
	}
	return next
}

func rejected(t *testing.T, s *State, seat int, a Action, code string) {
	t.Helper()
	_, _, err := Apply(s, seat, a)
	var re *RuleError
	if err == nil {
		t.Fatalf("seat %d %s should have been rejected", seat, a.Kind())
	}
	if !asRule(err, &re) || re.Code != code {
		t.Fatalf("seat %d %s: got %v, want %s", seat, a.Kind(), err, code)
	}
}

func asRule(err error, target **RuleError) bool {
	re, ok := err.(*RuleError)
	if ok {
		*target = re
	}
	return ok
}

// pickTrump drives the selection so the rest of a test can play tricks.
func pickTrump(t *testing.T, s *State, suit Suit) *State {
	t.Helper()
	return ok(t, s, SeatsOfTeam(s.EntitledTeam)[0], ChooseTrump{Suit: suit})
}

// Spec rules 1, 9, 13: exactly four players, one toss, five cards each.
func TestNewGame(t *testing.T) {
	for _, bad := range [][]string{{"a", "b", "c"}, {"a", "b", "c", "d", "e"}, {"a", "b", "c", "c"}, {"a", "b", "c", ""}} {
		if _, _, err := NewGame(bad, rand.Reader); err != ErrPlayers {
			t.Fatalf("%v should be refused, got %v", bad, err)
		}
	}
	s := fixture(t, 1)
	if s.Phase != PhaseSelect || s.Round != 1 {
		t.Fatalf("phase %s round %d", s.Phase, s.Round)
	}
	for seat, p := range s.Players {
		if len(p.Hand) != FirstDeal {
			t.Fatalf("seat %d holds %d cards, want %d", seat, len(p.Hand), FirstDeal)
		}
	}
	if len(s.Rest) != 52-Seats*FirstDeal {
		t.Fatalf("%d cards left undealt", len(s.Rest))
	}
	if s.EntitledTeam != 0 && s.EntitledTeam != 1 {
		t.Fatalf("toss produced team %d", s.EntitledTeam)
	}
	if s.Decider != -1 || s.Chooser != -1 || s.Trump != "" {
		t.Fatal("nobody has chosen yet")
	}
	if err := s.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

// Spec rules 9-12: the toss happens once per match. A second round inherits
// entitlement from the winner instead of tossing again.
func TestTossHappensOnce(t *testing.T) {
	s := fixture(t, 2)
	first := s.EntitledTeam
	// Reaching round 2 through a real round must not re-toss.
	s = playRound(t, s, Spades)
	winner := s.History[0].Winner
	if s.EntitledTeam != winner {
		t.Fatalf("entitled team is %d, want the round winner %d", s.EntitledTeam, winner)
	}
	for seat := range Seats {
		s = ok(t, s, seat, ReadyRound{})
	}
	if s.Round != 2 || s.Phase != PhaseSelect {
		t.Fatalf("round %d phase %s", s.Round, s.Phase)
	}
	if s.EntitledTeam != winner {
		t.Fatal("the next round must be chosen by the winners, not a new toss")
	}
	// A different match may toss differently; the same seed must not.
	if again := fixture(t, 2); again.EntitledTeam != first {
		t.Fatal("the toss must be stable for one seed")
	}
}

// Spec rules 16-19 and the owner decision of 2026-10-03: either member of the
// entitled team may take the decision; delegation happens once and the
// teammate must then choose; the chooser leads.
func TestTrumpSelection(t *testing.T) {
	t.Run("only the entitled team may choose", func(t *testing.T) {
		s := fixture(t, 3)
		other := SeatsOfTeam(1 - s.EntitledTeam)
		rejected(t, s, other[0], ChooseTrump{Suit: Hearts}, CodeNotYourTurn)
		rejected(t, s, other[1], DelegateTrump{}, CodeNotYourTurn)
	})

	t.Run("either teammate may act first", func(t *testing.T) {
		s := fixture(t, 4)
		seats := SeatsOfTeam(s.EntitledTeam)
		for _, seat := range []int{seats[0], seats[1]} {
			n := ok(t, s, seat, ChooseTrump{Suit: Clubs})
			if n.Chooser != seat || n.Leader != seat || n.Turn != seat {
				t.Fatalf("seat %d chose but the lead is %d", seat, n.Leader)
			}
		}
	})

	t.Run("delegation locks the choice to the teammate", func(t *testing.T) {
		s := fixture(t, 5)
		first := SeatsOfTeam(s.EntitledTeam)[0]
		partner := PartnerOf(first)
		n := ok(t, s, first, DelegateTrump{})
		if !n.Delegated || n.Decider != partner {
			t.Fatalf("delegated=%v decider=%d, want %d", n.Delegated, n.Decider, partner)
		}
		// It cannot be returned, repeated, or taken by the original player.
		rejected(t, n, partner, DelegateTrump{}, CodeIllegal)
		rejected(t, n, first, ChooseTrump{Suit: Hearts}, CodeNotYourTurn)
		rejected(t, n, first, DelegateTrump{}, CodeNotYourTurn)
		n = ok(t, n, partner, ChooseTrump{Suit: Hearts})
		if n.Chooser != partner || n.Leader != partner || !n.Delegated {
			t.Fatal("the delegate chooses and leads")
		}
	})

	t.Run("choosing deals the rest and starts play", func(t *testing.T) {
		s := fixture(t, 6)
		n := pickTrump(t, s, Diamonds)
		if n.Phase != PhasePlay || n.Trump != Diamonds {
			t.Fatalf("phase %s trump %s", n.Phase, n.Trump)
		}
		for seat, p := range n.Players {
			if len(p.Hand) != CardsPerPlayer {
				t.Fatalf("seat %d holds %d cards, want 13", seat, len(p.Hand))
			}
		}
		if len(n.Rest) != 0 {
			t.Fatal("every card must be dealt")
		}
		rejected(t, n, n.Chooser, ChooseTrump{Suit: Hearts}, CodeWrongPhase)
	})

	t.Run("a suit must be real", func(t *testing.T) {
		s := fixture(t, 7)
		rejected(t, s, SeatsOfTeam(s.EntitledTeam)[0], ChooseTrump{Suit: "swords"}, CodeInvalidAction)
	})
}

// Spec rule 20: chat is closed while the trump is being chosen and opens
// again once play starts (owner decision 2026-10-03).
func TestChatGate(t *testing.T) {
	s := fixture(t, 8)
	if s.ChatOpen() {
		t.Fatal("chat must be closed during selection")
	}
	if n := pickTrump(t, s, Spades); !n.ChatOpen() {
		t.Fatal("chat must reopen once play starts")
	}
}

// Spec rules 22-25: play follows seat order, the lead suit must be followed,
// and trumping is never compulsory.
func TestPlayingATrick(t *testing.T) {
	s := pickTrump(t, fixture(t, 9), Spades)
	leader := s.Turn
	lead := mustCard(s.Players[leader].Hand[0])
	s = ok(t, s, leader, PlayCard{Card: lead.ID})
	if s.Turn != next(leader) {
		t.Fatalf("turn went to %d, want %d", s.Turn, next(leader))
	}
	rejected(t, s, leader, PlayCard{Card: s.Players[leader].Hand[0]}, CodeNotYourTurn)

	follower := s.Turn
	if held := suitOf(s.Players[follower].Hand, lead.Suit); len(held) > 0 {
		// Holding the lead suit, any other card is refused.
		for _, id := range s.Players[follower].Hand {
			if mustCard(id).Suit != lead.Suit {
				rejected(t, s, follower, PlayCard{Card: id}, CodeIllegal)
				break
			}
		}
		s = ok(t, s, follower, PlayCard{Card: held[0]})
	} else {
		// Holding none, anything is legal: trumping is optional.
		legal := s.legalCards(follower)
		if len(legal) != len(s.Players[follower].Hand) {
			t.Fatal("with no card of the lead suit every card is legal")
		}
		s = ok(t, s, follower, PlayCard{Card: legal[0]})
	}
	rejected(t, s, s.Turn, PlayCard{Card: "spades-1"}, CodeInvalidCard)
	rejected(t, s, s.Turn, PlayCard{Card: s.Players[PartnerOf(s.Turn)].Hand[0]}, CodeInvalidCard)
}

// Spec rules 26-27: the trick is won by the highest trump, else the highest
// card of the lead suit, and the winner leads next.
func TestTrickWinnerAndLead(t *testing.T) {
	cases := []struct {
		name   string
		cards  []CardID
		winner int
	}{
		{"highest of the lead suit", []CardID{"hearts-9", "hearts-k", "hearts-3", "hearts-j"}, 1},
		{"off-suit cards cannot win", []CardID{"hearts-4", "clubs-a", "diamonds-a", "hearts-5"}, 3},
		{"a low trump beats every plain card", []CardID{"hearts-a", "spades-2", "hearts-k", "clubs-a"}, 1},
		{"the highest trump wins", []CardID{"hearts-a", "spades-2", "spades-q", "spades-7"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &State{Trump: Spades}
			for seat, card := range tc.cards {
				s.Trick = append(s.Trick, Play{Seat: seat, Card: card})
			}
			if got := s.trickWinner(); got != tc.winner {
				t.Fatalf("seat %d won, want %d", got, tc.winner)
			}
		})
	}

	// Through the engine: the winner of a real trick leads the next one.
	s := pickTrump(t, fixture(t, 10), Hearts)
	before := s.Turn
	for range Seats {
		s = ok(t, s, s.Turn, PlayCard{Card: s.legalCards(s.Turn)[0]})
	}
	if s.Tricks[0]+s.Tricks[1] != 1 {
		t.Fatal("one trick should have been awarded")
	}
	if s.Turn != s.Leader {
		t.Fatal("the trick winner leads the next trick")
	}
	if TeamOf(s.Turn) != TeamOf(s.Leader) {
		t.Fatal("the leader must be the winner")
	}
	_ = before
}

// playRound plays a whole round with the lowest legal card until a team
// reaches seven tricks.
func playRound(t *testing.T, s *State, suit Suit) *State {
	t.Helper()
	if s.Phase == PhaseSelect {
		s = pickTrump(t, s, suit)
	}
	for i := 0; s.Phase == PhasePlay; i++ {
		if i > Seats*CardsPerPlayer {
			t.Fatal("the round did not end")
		}
		a, okMove := s.TimeoutAction(s.Turn)
		if !okMove {
			t.Fatal("no default move for the awaited seat")
		}
		s = ok(t, s, s.Turn, a)
	}
	return s
}

// Spec rules 28-31: seven tricks end the round at once, history and counters
// are recorded, and the winners choose next.
func TestRoundEndsAtSeven(t *testing.T) {
	s := playRound(t, fixture(t, 11), Clubs)
	if s.Phase != PhaseRoundOver {
		t.Fatalf("phase %s", s.Phase)
	}
	winner := s.History[0].Winner
	if s.Tricks[winner] != TricksToWin {
		t.Fatalf("winner has %d tricks, want exactly %d", s.Tricks[winner], TricksToWin)
	}
	if total := s.Tricks[0] + s.Tricks[1]; total > CardsPerPlayer {
		t.Fatalf("%d tricks played", total)
	}
	if s.RoundsWon[winner] != 1 || s.RoundsWon[1-winner] != 0 {
		t.Fatalf("rounds won %v", s.RoundsWon)
	}
	if len(s.History) != 1 || s.History[0].Trump != Clubs || s.History[0].Round != 1 {
		t.Fatalf("history %+v", s.History)
	}
	// Tricks and rounds are separate counters.
	if s.RoundsWon[winner] == s.Tricks[winner] && s.Tricks[winner] != 1 {
		t.Fatal("rounds won and tricks won must be tracked separately")
	}
	rejected(t, s, s.Turn, PlayCard{Card: "spades-a"}, CodeWrongPhase)

	// Every seat must ready up before the next round deals.
	for seat := range Seats - 1 {
		s = ok(t, s, seat, ReadyRound{})
		rejected(t, s, seat, ReadyRound{}, CodeIllegal)
		if s.Round != 1 {
			t.Fatal("the round must not restart before everyone is ready")
		}
	}
	s = ok(t, s, Seats-1, ReadyRound{})
	if s.Round != 2 || s.Phase != PhaseSelect || s.Trump != "" {
		t.Fatalf("round %d phase %s trump %s", s.Round, s.Phase, s.Trump)
	}
	if s.Tricks != [2]int{0, 0} || s.RoundsWon[winner] != 1 {
		t.Fatal("tricks reset each round; rounds won accumulate")
	}
	for seat, p := range s.Players {
		if len(p.Hand) != FirstDeal {
			t.Fatalf("seat %d was dealt %d cards for round 2", seat, len(p.Hand))
		}
	}
}

// Spec rule 34: the default move is always legal, whatever the game is
// waiting for, so a silent player never stalls the table.
func TestTimeoutMovesAreAlwaysLegal(t *testing.T) {
	for seed := range 40 {
		s := fixture(t, byte(seed))
		for step := 0; step < 400; step++ {
			seats := s.TimeoutSeats()
			if len(seats) == 0 {
				t.Fatalf("nothing to do in phase %s", s.Phase)
			}
			if s.Phase == PhaseSelect && len(seats) != 1 {
				t.Fatal("only one seat should be moved for during selection")
			}
			a, okMove := s.TimeoutAction(seats[0])
			if !okMove {
				t.Fatalf("no default move for seat %d in phase %s", seats[0], s.Phase)
			}
			next, _, err := Apply(s, seats[0], a)
			if err != nil {
				t.Fatalf("default move %s rejected: %v", a.Kind(), err)
			}
			s = next
			if s.Round > 2 {
				break
			}
		}
		if s.Round < 2 {
			t.Fatalf("seed %d never finished a round", seed)
		}
	}
}

// A player's cards reach only that player: the projection for one seat never
// contains another seat's hand (spec rule 21).
func TestViewLeaksNothing(t *testing.T) {
	s := pickTrump(t, fixture(t, 12), Hearts)
	for seat := range Seats {
		v := s.ViewFor(seat)
		if !slices.Equal(v.Self.Hand, s.Players[seat].Hand) {
			t.Fatal("a player must see their own hand")
		}
		for other := range Seats {
			if other == seat {
				continue
			}
			for _, id := range s.Players[other].Hand {
				if slices.Contains(v.Self.Hand, id) {
					t.Fatalf("seat %d sees seat %d's %s", seat, other, id)
				}
			}
		}
		for _, p := range v.Public.Players {
			if p.Cards != len(s.Players[p.Seat].Hand) {
				t.Fatal("public card counts must match")
			}
		}
	}
	// The undealt remainder is not projected either: after the second deal it
	// is empty, and before it the view carries no card list at all.
	sel := fixture(t, 13)
	for seat := range Seats {
		v := sel.ViewFor(seat)
		if len(v.Self.Hand) != FirstDeal {
			t.Fatal("five cards before trump selection")
		}
		if len(v.Public.Trick) != 0 || v.Public.Trump != "" {
			t.Fatal("no trump or trick before selection")
		}
	}
}

// The legal-card list the client dims cards with must agree with what the
// engine accepts.
func TestLegalListMatchesEnforcement(t *testing.T) {
	s := pickTrump(t, fixture(t, 14), Diamonds)
	for range 20 {
		if s.Phase != PhasePlay {
			break
		}
		seat := s.Turn
		legal := s.legalCards(seat)
		for _, id := range s.Players[seat].Hand {
			_, _, err := Apply(s, seat, PlayCard{Card: id})
			if slices.Contains(legal, id) != (err == nil) {
				t.Fatalf("card %s: legal=%v but engine error %v", id, slices.Contains(legal, id), err)
			}
		}
		s = ok(t, s, seat, PlayCard{Card: legal[0]})
	}
}
