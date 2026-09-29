package monopoly

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"math/rand/v2"
)

// SeedSize is the length of the secret per-match seed.
const SeedSize = 32

// shuffle permutes cards with an unbiased Fisher–Yates shuffle. Randomness is
// ChaCha8, a cryptographically strong generator, keyed by
// SHA-256(seed || shuffle number). The seed comes from crypto/rand at setup,
// so every later reshuffle is unpredictable to players yet the game replays
// exactly from its seed and action log.
func (s *State) shuffle(cards []CardID) {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(s.Shuffles))
	s.Shuffles++
	key := sha256.Sum256(append(append([]byte{}, s.Seed...), counter[:]...))
	r := rand.New(rand.NewChaCha8(key))
	r.Shuffle(len(cards), func(i, j int) { cards[i], cards[j] = cards[j], cards[i] })
}

// draw moves up to n cards from the top of the draw pile to the player's hand.
// When the draw pile empties mid-draw, the resolved center pile is shuffled
// into a new draw pile and the draw continues (G1 Finish). Cards in a pending
// action are not in the center pile, so they are never recycled. With both
// piles empty the player draws only what exists (spec D).
func (s *State) draw(seat, n int) []CardID {
	var drawn []CardID
	for len(drawn) < n {
		if len(s.Draw) == 0 {
			if len(s.Discard) == 0 {
				break
			}
			s.Draw, s.Discard = s.Discard, nil
			s.shuffle(s.Draw)
		}
		drawn = append(drawn, s.Draw[0])
		s.Draw = s.Draw[1:]
	}
	s.Players[seat].Hand = append(s.Players[seat].Hand, drawn...)
	return drawn
}

// ErrPlayers reports an invalid seating.
var ErrPlayers = errors.New("monopoly deal needs 2-5 distinct players")

// NewGame shuffles the 106 playing cards, deals five to each seat, picks the
// first seat at random and starts that seat's turn (B1 Set Up; spec D for the
// random first seat). Seats are in clockwise order. The seed is read from
// random, which must be crypto/rand in production.
func NewGame(userIDs []string, random io.Reader) (*State, []Event, error) {
	if len(userIDs) < 2 || len(userIDs) > 5 {
		return nil, nil, ErrPlayers
	}
	seen := map[string]bool{}
	for _, id := range userIDs {
		if id == "" || seen[id] {
			return nil, nil, ErrPlayers
		}
		seen[id] = true
	}
	seed := make([]byte, SeedSize)
	if _, err := io.ReadFull(random, seed); err != nil {
		return nil, nil, err
	}
	s := &State{Schema: SchemaVersion, Seed: seed, Phase: PhasePlay}
	for _, id := range userIDs {
		s.Players = append(s.Players, Player{UserID: id, Hand: []CardID{}, Bank: []CardID{}, Sets: []PropertySet{}, Unassigned: []CardID{}, Detached: []CardID{}, Incoming: []CardID{}})
	}
	for _, c := range manifest {
		s.Draw = append(s.Draw, c.ID)
	}
	s.shuffle(s.Draw)
	for range 5 {
		for seat := range s.Players {
			s.draw(seat, 1)
		}
	}
	// A separate keyed draw chooses the first seat, so it is uniform and
	// independent of the deal order.
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(s.Shuffles))
	s.Shuffles++
	key := sha256.Sum256(append(append([]byte{}, s.Seed...), counter[:]...))
	first := rand.New(rand.NewChaCha8(key)).IntN(len(s.Players))
	events := []Event{{Kind: "game_started", Audience: Public, Data: map[string]any{"first_seat": first, "players": len(s.Players)}}}
	for seat, p := range s.Players {
		events = append(events, Event{Kind: "dealt", Audience: seat, Data: map[string]any{"cards": append([]CardID(nil), p.Hand...)}})
	}
	events = append(events, s.startTurn(first)...)
	return s, events, nil
}

// startTurn makes seat active. An existing qualifying collection wins before
// drawing (B1 turn 2B: wait until your turn; spec D timing). Otherwise the
// player draws two, or five if their hand is empty (B1 On Your Turn 1).
func (s *State) startTurn(seat int) []Event {
	s.Active = seat
	s.Turn++
	s.PlaysUsed = 0
	s.Phase = PhasePlay
	events := []Event{{Kind: "turn_started", Audience: Public, Data: map[string]any{"seat": seat, "turn": s.Turn}}}
	if won := s.checkVictory(); won != nil {
		return append(events, won...)
	}
	n := 2
	if len(s.Players[seat].Hand) == 0 {
		n = 5
	}
	return append(events, s.drawEvents(seat, n)...)
}

func (s *State) drawEvents(seat, n int) []Event {
	before := s.Shuffles
	drawn := s.draw(seat, n)
	var events []Event
	if s.Shuffles != before {
		events = append(events, Event{Kind: "center_pile_reshuffled", Audience: Public, Data: map[string]any{"draw_count": len(s.Draw) + len(drawn)}})
	}
	events = append(events,
		Event{Kind: "drew", Audience: Public, Data: map[string]any{"seat": seat, "count": len(drawn)}},
		Event{Kind: "drew_cards", Audience: seat, Data: map[string]any{"cards": drawn}},
	)
	return events
}
