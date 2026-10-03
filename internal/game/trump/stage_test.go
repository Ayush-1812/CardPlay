package trump

import (
	"bytes"
	"slices"
	"testing"
)

// Every state the rules distinguish, walked in order through a real round.
func TestStages(t *testing.T) {
	t.Run("a table without four players", func(t *testing.T) {
		// NewGame refuses it, so no stored state can rest here; a partial
		// table still reports the stage rather than pretending to be ready.
		partial := &State{Schema: SchemaVersion, Players: []Player{{UserID: "u0"}}, Ready: make([]bool, Seats)}
		if partial.Stage() != StageWaitingForPlayers {
			t.Fatalf("stage %s", partial.Stage())
		}
		if _, _, err := NewGame([]string{"u0", "u1", "u2"}, bytes.NewReader(make([]byte, SeedSize))); err != ErrPlayers {
			t.Fatal("three players must be refused")
		}
	})

	s := fixture(t, 20)
	if s.Stage() != StageTrumpDecision {
		t.Fatalf("after the first deal: %s", s.Stage())
	}

	t.Run("delegation has its own stage", func(t *testing.T) {
		d := ok(t, s, SeatsOfTeam(s.EntitledTeam)[0], DelegateTrump{})
		if d.Stage() != StageDelegatedDecision {
			t.Fatalf("stage %s", d.Stage())
		}
		if p := ok(t, d, d.Decider, ChooseTrump{Suit: Hearts}); p.Stage() != StageActiveTrick {
			t.Fatalf("after choosing: %s", p.Stage())
		}
	})

	s = pickTrump(t, s, Hearts)
	if s.Stage() != StageActiveTrick || s.LastTrick != nil {
		t.Fatalf("stage %s", s.Stage())
	}
	// Mid-trick is still the active stage.
	s = ok(t, s, s.Turn, PlayCard{Card: s.legalCards(s.Turn)[0]})
	if s.Stage() != StageActiveTrick || len(s.Trick) != 1 {
		t.Fatalf("stage %s with %d cards", s.Stage(), len(s.Trick))
	}
	for range Seats - 1 {
		s = ok(t, s, s.Turn, PlayCard{Card: s.legalCards(s.Turn)[0]})
	}
	// The finished trick stays visible until its winner leads again.
	if s.Stage() != StageCompletedTrick {
		t.Fatalf("stage %s", s.Stage())
	}
	if s.LastTrick == nil || len(s.LastTrick.Cards) != Seats || s.LastTrick.Winner != s.Turn {
		t.Fatalf("last trick %+v", s.LastTrick)
	}
	if TeamOf(s.LastTrick.Winner) != s.LastTrick.Team {
		t.Fatal("the recorded team must match the winner")
	}
	s = ok(t, s, s.Turn, PlayCard{Card: s.legalCards(s.Turn)[0]})
	if s.Stage() != StageActiveTrick {
		t.Fatalf("leading again returns to %s, got %s", StageActiveTrick, s.Stage())
	}

	s = playRound(t, s, Hearts)
	if s.Stage() != StageCompletedRound {
		t.Fatalf("stage %s", s.Stage())
	}
	s = ok(t, s, 0, ReadyRound{})
	if s.Stage() != StageWaitingReadiness {
		t.Fatalf("stage %s", s.Stage())
	}
	for seat := 1; seat < Seats; seat++ {
		s = ok(t, s, seat, ReadyRound{})
	}
	if s.Stage() != StageTrumpDecision || s.Round != 2 || s.LastTrick != nil {
		t.Fatalf("next round starts at %s round %d", s.Stage(), s.Round)
	}
}

// Across a whole round every one of the 52 cards exists exactly once, and no
// two players ever hold the same card.
func TestCardConservationAndOwnership(t *testing.T) {
	s := fixture(t, 21)
	check := func(stage string) {
		t.Helper()
		owner := map[CardID]string{}
		claim := func(id CardID, by string) {
			if prev, ok := owner[id]; ok {
				t.Fatalf("%s: %s is held by %s and %s", stage, id, prev, by)
			}
			owner[id] = by
		}
		for seat, p := range s.Players {
			for _, id := range p.Hand {
				claim(id, "seat "+string(rune('0'+seat)))
			}
		}
		for _, id := range s.Rest {
			claim(id, "undealt")
		}
		for _, p := range s.Trick {
			claim(p.Card, "trick")
		}
		for _, id := range s.Played {
			claim(id, "played")
		}
		if len(owner) != 52 {
			t.Fatalf("%s: %d distinct cards, want 52", stage, len(owner))
		}
		if err := s.CheckInvariants(); err != nil {
			t.Fatalf("%s: %v", stage, err)
		}
	}
	check("after the first deal")
	s = pickTrump(t, s, Clubs)
	check("after the second deal")
	for i := 0; s.Phase == PhasePlay; i++ {
		a, _ := s.TimeoutAction(s.Turn)
		s = ok(t, s, s.Turn, a)
		check("during play")
	}
	check("after the round")
	for seat := range Seats {
		s = ok(t, s, seat, ReadyRound{})
	}
	check("after the next deal")
}

// Several trumps in one trick, and off-suit discards, resolve as specified.
func TestTrumpsAndDiscards(t *testing.T) {
	trick := func(cards ...CardID) *State {
		s := &State{Trump: Spades}
		for seat, c := range cards {
			s.Trick = append(s.Trick, Play{Seat: seat, Card: c})
		}
		return s
	}
	t.Run("the highest of several trumps wins", func(t *testing.T) {
		s := trick("hearts-k", "spades-5", "spades-j", "spades-9")
		if got := s.trickWinner(); got != 2 {
			t.Fatalf("seat %d won, want the jack of trumps at seat 2", got)
		}
	})
	t.Run("a trump played last still wins", func(t *testing.T) {
		s := trick("hearts-a", "hearts-k", "hearts-q", "spades-2")
		if got := s.trickWinner(); got != 3 {
			t.Fatalf("seat %d won, want the late trump at seat 3", got)
		}
	})
	t.Run("off-suit discards cannot win", func(t *testing.T) {
		s := trick("hearts-7", "clubs-a", "diamonds-k", "hearts-8")
		if got := s.trickWinner(); got != 3 {
			t.Fatalf("seat %d won, want the highest heart at seat 3", got)
		}
	})
	t.Run("a discard does not beat an earlier discard", func(t *testing.T) {
		s := trick("hearts-2", "clubs-a", "clubs-k", "diamonds-a")
		if got := s.trickWinner(); got != 0 {
			t.Fatalf("seat %d won, want the only heart at seat 0", got)
		}
	})

	// Through the engine: a player void in the lead suit may discard instead
	// of trumping, and the engine accepts both.
	s := pickTrump(t, fixture(t, 22), Spades)
	for range 60 {
		if s.Phase != PhasePlay {
			break
		}
		seat := s.Turn
		lead := s.leadSuit()
		if lead != "" && len(suitOf(s.Players[seat].Hand, lead)) == 0 {
			trumps := suitOf(s.Players[seat].Hand, s.Trump)
			plain := []CardID{}
			for _, id := range s.Players[seat].Hand {
				if mustCard(id).Suit != s.Trump {
					plain = append(plain, id)
				}
			}
			if len(trumps) > 0 && len(plain) > 0 {
				// Both are legal: trumping is never compulsory.
				if _, _, err := Apply(s, seat, PlayCard{Card: trumps[0]}); err != nil {
					t.Fatalf("trumping refused: %v", err)
				}
				if _, _, err := Apply(s, seat, PlayCard{Card: plain[0]}); err != nil {
					t.Fatalf("discarding refused: %v", err)
				}
				return
			}
		}
		a, _ := s.TimeoutAction(seat)
		s = ok(t, s, seat, a)
	}
}

// The opening lead belongs to whoever named the trump; every later trick is
// led by the player who won the previous one.
func TestLeaders(t *testing.T) {
	s := fixture(t, 23)
	first := SeatsOfTeam(s.EntitledTeam)[1]
	s = ok(t, s, first, ChooseTrump{Suit: Diamonds})
	if s.Leader != first || s.Turn != first {
		t.Fatalf("the chooser leads: leader %d turn %d, want %d", s.Leader, s.Turn, first)
	}
	// After a delegation it is the delegate, not the original player.
	d := fixture(t, 24)
	opener := SeatsOfTeam(d.EntitledTeam)[0]
	d = ok(t, d, opener, DelegateTrump{})
	d = ok(t, d, PartnerOf(opener), ChooseTrump{Suit: Clubs})
	if d.Leader != PartnerOf(opener) {
		t.Fatalf("the delegate leads, got seat %d", d.Leader)
	}

	seen := 0
	for s.Phase == PhasePlay && seen < 6 {
		expected := s.Turn
		var played []Play
		for range Seats {
			played = append(played, Play{Seat: s.Turn, Card: s.legalCards(s.Turn)[0]})
			s = ok(t, s, s.Turn, PlayCard{Card: played[len(played)-1].Card})
		}
		if played[0].Seat != expected {
			t.Fatal("the leader plays first")
		}
		if s.Phase != PhasePlay {
			break
		}
		if s.LastTrick == nil || s.Turn != s.LastTrick.Winner || s.Leader != s.LastTrick.Winner {
			t.Fatalf("the winner of the trick leads next: turn %d winner %+v", s.Turn, s.LastTrick)
		}
		seen++
	}
	if seen == 0 {
		t.Fatal("no trick was completed")
	}
}

// Dealing counts: five each then eight each, from one 52-card shuffle, with
// nothing left over.
func TestDealingCounts(t *testing.T) {
	s := fixture(t, 25)
	for _, p := range s.Players {
		if len(p.Hand) != 5 {
			t.Fatalf("first deal gave %d cards", len(p.Hand))
		}
	}
	if len(s.Rest) != 32 {
		t.Fatalf("%d cards wait for the second deal, want 32", len(s.Rest))
	}
	s = pickTrump(t, s, Hearts)
	for _, p := range s.Players {
		if len(p.Hand) != 13 {
			t.Fatalf("second deal left %d cards", len(p.Hand))
		}
	}
	if len(s.Rest) != 0 {
		t.Fatal("no card may stay undealt")
	}
	// Nobody receives the same card twice across the two deals.
	all := []CardID{}
	for _, p := range s.Players {
		all = append(all, p.Hand...)
	}
	slices.Sort(all)
	if len(slices.Compact(all)) != 52 {
		t.Fatal("the two deals must not repeat a card")
	}
}
