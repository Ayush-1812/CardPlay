package trump

import (
	"crypto/rand"
	"encoding/binary"
	"io"
)

// Seats is the number of players: exactly four (spec rule 1).
const Seats = 4

// Teams is the number of teams, two of two.
const Teams = 2

// TricksToWin ends a round the moment one team reaches it (spec rule 28).
const TricksToWin = 7

// CardsPerPlayer is the full hand once the second deal is done.
const CardsPerPlayer = 13

// FirstDeal is how many cards each player sees before trump is chosen.
const FirstDeal = 5

// TeamOf maps a seat to its team: seats 0 and 2 are team 0, seats 1 and 3 are
// team 1, so teammates sit opposite each other (spec rule 2).
func TeamOf(seat int) int { return seat % Teams }

// PartnerOf returns the seat of a player's teammate.
func PartnerOf(seat int) int { return (seat + Teams) % Seats }

// SeatsOfTeam lists a team's two seats in seat order.
func SeatsOfTeam(team int) [2]int { return [2]int{team, team + Teams} }

// next returns the seat that plays after this one. Play runs in ascending
// seat order and wraps (spec rule 3).
func next(seat int) int { return (seat + 1) % Seats }

// source is where a deal draws its randomness. Production reads the
// operating system's generator; a test may swap it for a reader it controls.
// crypto/rand.Reader is safe to use from several goroutines at once.
var source io.Reader = rand.Reader

// shuffle permutes cards in place with a Fisher-Yates shuffle, drawing every
// index from r. A fresh deck is shuffled for each round and nothing is
// derived from a stored seed, so no one who can read a match -- including
// anyone with the database -- can work out what the next deal will be.
func shuffle(cards []CardID, r io.Reader) error {
	for i := len(cards) - 1; i > 0; i-- {
		j, err := index(i+1, r)
		if err != nil {
			return err
		}
		cards[i], cards[j] = cards[j], cards[i]
	}
	return nil
}

// index returns a uniform value in [0,n) read from r.
//
// Random bytes are masked down to the smallest power of two that covers n
// and anything landing outside the range is thrown away rather than folded
// back in. Folding -- a modulo or a multiply -- would make the low indices
// fractionally likelier than the high ones, which over a 52-card deck is a
// real bias in who gets the trumps.
func index(n int, r io.Reader) (int, error) {
	if n <= 1 {
		return 0, nil
	}
	mask := uint32(1)
	for mask < uint32(n) {
		mask <<= 1
	}
	mask--
	var b [4]byte
	for {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, err
		}
		if v := binary.BigEndian.Uint32(b[:]) & mask; v < uint32(n) {
			return int(v), nil
		}
	}
}
