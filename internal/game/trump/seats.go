package trump

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand/v2"
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

// SeedSize is the length of the secret per-match seed.
const SeedSize = 32

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

// shuffle permutes cards with an unbiased Fisher-Yates shuffle, matching the
// construction Monopoly uses: ChaCha8 keyed by SHA-256(seed || counter). The
// seed comes from crypto/rand when the match is created, so deals are
// unpredictable to players while the match still replays exactly from its
// seed. The counter makes every later deal in the same match independent.
func shuffle(cards []CardID, seed []byte, counter uint64) {
	var c [8]byte
	binary.BigEndian.PutUint64(c[:], counter)
	key := sha256.Sum256(append(append([]byte{}, seed...), c[:]...))
	rand.New(rand.NewChaCha8(key)).Shuffle(len(cards), func(i, j int) {
		cards[i], cards[j] = cards[j], cards[i]
	})
}

// pick chooses one of n outcomes from the seed, independently of any shuffle.
// The toss uses it (spec rule 9).
func pick(n int, seed []byte, counter uint64) int {
	var c [8]byte
	binary.BigEndian.PutUint64(c[:], counter)
	key := sha256.Sum256(append(append([]byte{}, seed...), c[:]...))
	return rand.New(rand.NewChaCha8(key)).IntN(n)
}
