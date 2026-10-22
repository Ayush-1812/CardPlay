// Package bluff implements the card game Bluff: players must play to the
// required rank whether or not they hold it, and anyone may challenge the
// claim. The engine is pure -- no clock of its own beyond the time the
// platform stamps on a command, no network, no storage.
package bluff

import (
	"fmt"
	"strconv"
	"strings"
)

// Suit is a card's suit. Bluff gives suits no meaning at all: they exist so
// the 52 cards are distinct, and nothing in the rules reads them.
type Suit string

const (
	Spades   Suit = "spades"
	Hearts   Suit = "hearts"
	Diamonds Suit = "diamonds"
	Clubs    Suit = "clubs"
)

// Rank is what a play is declared as. Ranks have no strength here; they
// only form the cycle the table walks through.
type Rank string

const (
	Ace   Rank = "a"
	Two   Rank = "2"
	Three Rank = "3"
	Four  Rank = "4"
	Five  Rank = "5"
	Six   Rank = "6"
	Seven Rank = "7"
	Eight Rank = "8"
	Nine  Rank = "9"
	Ten   Rank = "10"
	Jack  Rank = "j"
	Queen Rank = "q"
	King  Rank = "k"
)

// Order is the cycle the required rank follows. It advances after every
// accepted play and wraps from King back to Ace; a challenge never resets
// it.
var Order = []Rank{Ace, Two, Three, Four, Five, Six, Seven, Eight, Nine, Ten, Jack, Queen, King}

// Suits lists the four suits in a fixed order, so a deck is built the same
// way every time before it is shuffled.
var Suits = []Suit{Spades, Hearts, Diamonds, Clubs}

// CardID is one physical card. With a single deck that is "rank-suit":
// "q-hearts", "10-spades". With more than one deck every copy needs its own
// identity, so the second deck's queen of hearts is "q-hearts#2". The rank
// and suit read the same either way; only the copy differs.
type CardID string

// Card is a card's public identity. Nothing here is secret; what is secret
// is which hand or pile a card is in.
type Card struct {
	ID   CardID `json:"id"`
	Rank Rank   `json:"rank"`
	Suit Suit   `json:"suit"`
}

// DeckSize is one whole deck: four suits of thirteen ranks, no jokers.
const DeckSize = 52

// MaxDecks is how many decks one game may be dealt from. Four is 208
// cards, which is already a lot to hold at a six-player table.
const MaxDecks = 4

// CopiesPerRank is how many cards of each rank one deck holds, which is
// what bounds a single play.
const CopiesPerRank = 4

var (
	manifest []Card
	byID     map[CardID]Card
)

func init() {
	manifest = make([]Card, 0, DeckSize)
	byID = make(map[CardID]Card, DeckSize)
	for _, s := range Suits {
		for _, r := range Order {
			c := Card{ID: CardID(string(r) + "-" + string(s)), Rank: r, Suit: s}
			manifest = append(manifest, c)
			byID[c.ID] = c
		}
	}
}

// Manifest returns the 52 cards. The slice is a copy: a caller cannot
// reorder the deck everyone else reads.
func Manifest() []Card {
	out := make([]Card, len(manifest))
	copy(out, manifest)
	return out
}

// Lookup returns a card by id, whichever deck the copy came from.
func Lookup(id CardID) (Card, bool) {
	c, ok := byID[baseID(id)]
	return c, ok
}

// copyID names the nth copy of a card. The first deck keeps the plain id,
// so a single-deck game reads exactly as it always did.
func copyID(id CardID, deck int) CardID {
	if deck <= 1 {
		return id
	}
	return CardID(string(id) + "#" + strconv.Itoa(deck))
}

// baseID strips the copy marker, leaving the card's face.
func baseID(id CardID) CardID {
	if cut := strings.IndexByte(string(id), '#'); cut >= 0 {
		return CardID(string(id)[:cut])
	}
	return id
}

// BuildDeck returns decks whole decks, every card with its own id.
func BuildDeck(decks int) []CardID {
	out := make([]CardID, 0, DeckSize*decks)
	for deck := 1; deck <= decks; deck++ {
		for _, c := range manifest {
			out = append(out, copyID(c.ID, deck))
		}
	}
	return out
}

// RankName writes a rank the way a player would say it, for the sentence
// the table is shown: "played 3 Queens".
func RankName(r Rank, plural bool) string {
	name := map[Rank]string{
		Ace: "Ace", Two: "Two", Three: "Three", Four: "Four", Five: "Five",
		Six: "Six", Seven: "Seven", Eight: "Eight", Nine: "Nine", Ten: "Ten",
		Jack: "Jack", Queen: "Queen", King: "King",
	}[r]
	if name == "" {
		return string(r)
	}
	if !plural {
		return name
	}
	if r == Six {
		return "Sixes"
	}
	return name + "s"
}

// NextRank returns the rank that follows r in the cycle, wrapping King to
// Ace.
func NextRank(r Rank) Rank {
	for i, candidate := range Order {
		if candidate == r {
			return Order[(i+1)%len(Order)]
		}
	}
	return Ace
}

// Valid reports whether a rank is one of the thirteen.
func (r Rank) Valid() bool {
	for _, candidate := range Order {
		if candidate == r {
			return true
		}
	}
	return false
}

// ParseCard splits an id into its rank and suit, ignoring the copy.
func ParseCard(id CardID) (Card, error) {
	c, ok := Lookup(id)
	if !ok {
		return Card{}, fmt.Errorf("unknown card %q", id)
	}
	return c, nil
}

// mustCard is for ids already known to be in the deck.
func mustCard(id CardID) Card {
	c, ok := Lookup(id)
	if !ok {
		panic("bluff: unknown card " + string(id))
	}
	return c
}
