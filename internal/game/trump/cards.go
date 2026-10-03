// Package trump is the authoritative rules engine for Trump, a four-player
// two-team trick-taking game with a chosen trump suit. It is pure: no HTTP,
// WebSocket, database or UI code. The specification is docs/13-trump-specification.md.
package trump

import (
	"fmt"
	"slices"
	"strings"
)

// Suit is one of the four suits. A round's trump is one of these.
type Suit string

const (
	Spades   Suit = "spades"
	Hearts   Suit = "hearts"
	Diamonds Suit = "diamonds"
	Clubs    Suit = "clubs"
)

// Suits lists the suits in a fixed order, used for display and for
// deterministic tie-breaking.
var Suits = []Suit{Spades, Hearts, Diamonds, Clubs}

// Valid reports whether s is one of the four suits.
func (s Suit) Valid() bool { return slices.Contains(Suits, s) }

// Rank is a card's rank. Ranks compare by Strength, never by string.
type Rank string

// Ranks lists ranks from lowest to highest (spec rule 6, read in reverse).
var Ranks = []Rank{"2", "3", "4", "5", "6", "7", "8", "9", "10", "j", "q", "k", "a"}

// Strength orders ranks: 2 is weakest at 0, the ace strongest at 12.
func (r Rank) Strength() int { return slices.Index(Ranks, r) }

// CardID identifies one physical card, "<suit>-<rank>", for example
// "spades-a". Every card in the deck has a distinct ID (spec rule 7).
type CardID string

// Card is one physical card. Cards are immutable data, not state.
type Card struct {
	ID   CardID `json:"id"`
	Suit Suit   `json:"suit"`
	Rank Rank   `json:"rank"`
}

// Name is the card's human label, for example "Q♥".
func (c Card) Name() string {
	symbols := map[Suit]string{Spades: "♠", Hearts: "♥", Diamonds: "♦", Clubs: "♣"}
	return strings.ToUpper(string(c.Rank)) + symbols[c.Suit]
}

// Beats reports whether c wins over best when both are in the same trick.
// Only the lead suit and trump can win: trump outranks every plain card, and
// within one suit the higher rank wins (spec rule 26).
func (c Card) Beats(best Card, lead, trump Suit) bool {
	switch {
	case c.Suit == best.Suit:
		return c.Rank.Strength() > best.Rank.Strength()
	case c.Suit == trump:
		return true
	case best.Suit == trump:
		return false
	default:
		// Neither is trump and they differ in suit: an off-suit card cannot
		// take the trick from the lead suit.
		return c.Suit == lead && best.Suit != lead
	}
}

// deck is the 52-card manifest, built once in suit then rank order.
var deck, byID = buildDeck()

func buildDeck() ([]Card, map[CardID]Card) {
	cards := make([]Card, 0, 52)
	index := make(map[CardID]Card, 52)
	for _, s := range Suits {
		for _, r := range Ranks {
			c := Card{ID: CardID(fmt.Sprintf("%s-%s", s, r)), Suit: s, Rank: r}
			cards = append(cards, c)
			index[c.ID] = c
		}
	}
	return cards, index
}

// Manifest returns the public card list. It holds no hidden state, so it may
// be served to clients.
func Manifest() []Card { return slices.Clone(deck) }

// Lookup returns the card for an ID.
func Lookup(id CardID) (Card, bool) {
	c, ok := byID[id]
	return c, ok
}

// mustCard returns a card the engine already knows exists. A missing ID is a
// bug in the engine, never player input.
func mustCard(id CardID) Card {
	c, ok := byID[id]
	if !ok {
		panic("trump: unknown card " + string(id))
	}
	return c
}

// suitOf groups card IDs by suit, preserving order.
func suitOf(ids []CardID, s Suit) []CardID {
	var out []CardID
	for _, id := range ids {
		if mustCard(id).Suit == s {
			out = append(out, id)
		}
	}
	return out
}
