package trump

import (
	"fmt"
	"slices"
)

// SchemaVersion is the stored state format. Bump it on any breaking change.
const SchemaVersion = 1

// Phase is what the game is waiting for.
type Phase string

const (
	// PhaseSelect waits for the entitled team to name the trump suit.
	PhaseSelect Phase = "trump_selection"
	// PhasePlay waits for the player whose turn it is to play a card.
	PhasePlay Phase = "play"
	// PhaseRoundOver waits for all four players to ready up for the next round.
	PhaseRoundOver Phase = "round_over"
)

// Player is one seat. Hand is private to that seat and never projected to
// anyone else, teammate included (spec rule 21).
type Player struct {
	UserID string   `json:"user_id"`
	Hand   []CardID `json:"hand"`
}

// Play is one card played into the current trick.
type Play struct {
	Seat int    `json:"seat"`
	Card CardID `json:"card"`
}

// Trick is a finished trick kept on the table until its winner leads the
// next one, so every client can see who took it and with what. Its cards are
// also in Played; conservation counts them there, never twice.
type Trick struct {
	Cards  []Play `json:"cards"`
	Lead   Suit   `json:"lead"`
	Winner int    `json:"winner"`
	Team   int    `json:"team"`
}

// RoundResult is one finished round, kept as history (spec rule 29).
type RoundResult struct {
	Round     int    `json:"round"`
	Trump     Suit   `json:"trump"`
	Chooser   int    `json:"chooser"`
	Delegated bool   `json:"delegated"`
	Winner    int    `json:"winner"`
	Tricks    [2]int `json:"tricks"`
}

// State is the whole game. It is stored as one JSON snapshot per revision and
// is server-only: clients receive projections from ViewFor.
type State struct {
	Schema  int      `json:"schema"`
	Seed    []byte   `json:"seed"`
	Deals   uint64   `json:"deals"`
	Players []Player `json:"players"`

	Phase Phase `json:"phase"`
	Round int   `json:"round"`

	// EntitledTeam may choose this round's trump: the toss winner in round 1,
	// afterwards the team that won the previous round (spec rules 9-12).
	EntitledTeam int `json:"entitled_team"`
	// Decider is the seat that must choose, or -1 while either member of the
	// entitled team may still take the decision (owner decision 2026-10-03).
	Decider int `json:"decider"`
	// Chooser is the seat that actually named the trump; -1 until then. That
	// player leads the first trick (spec rule 19).
	Chooser   int  `json:"chooser"`
	Delegated bool `json:"delegated"`
	Trump     Suit `json:"trump"`

	// Rest holds the undealt cards between the first deal of five and the
	// second deal of eight (spec rule 14).
	Rest []CardID `json:"rest"`

	Leader int    `json:"leader"`
	Turn   int    `json:"turn"`
	Trick  []Play `json:"trick"`
	// LastTrick is the previous completed trick, or nil at the start of a
	// round. It is display state: its cards are counted in Played.
	LastTrick *Trick   `json:"last_trick,omitempty"`
	Played    []CardID `json:"played"`

	Tricks    [2]int        `json:"tricks"`
	RoundsWon [2]int        `json:"rounds_won"`
	History   []RoundResult `json:"history"`
	Ready     []bool        `json:"ready"`
}

// seatOf returns a user's seat, or -1.
func (s *State) seatOf(userID string) int {
	for i, p := range s.Players {
		if p.UserID == userID {
			return i
		}
	}
	return -1
}

// leadSuit is the suit of the first card in the current trick, or "" when no
// card has been played yet (spec rule 23).
func (s *State) leadSuit() Suit {
	if len(s.Trick) == 0 {
		return ""
	}
	return mustCard(s.Trick[0].Card).Suit
}

// legalCards lists the cards a seat may play right now. A player holding the
// lead suit must follow it; otherwise anything goes, trump included, with no
// duty to beat the winning card (spec rules 24-25).
func (s *State) legalCards(seat int) []CardID {
	hand := s.Players[seat].Hand
	if lead := s.leadSuit(); lead != "" {
		if following := suitOf(hand, lead); len(following) > 0 {
			return following
		}
	}
	return slices.Clone(hand)
}

// trickWinner returns the seat holding the best card in a complete or partial
// trick: the highest trump if any was played, otherwise the highest card of
// the lead suit (spec rule 26).
func (s *State) trickWinner() int {
	lead := s.leadSuit()
	best := s.Trick[0]
	for _, p := range s.Trick[1:] {
		if mustCard(p.Card).Beats(mustCard(best.Card), lead, s.Trump) {
			best = p
		}
	}
	return best.Seat
}

// CheckInvariants verifies what must hold after every transition. A violation
// is an engine bug, never a player mistake.
func (s *State) CheckInvariants() error {
	if s.Schema != SchemaVersion {
		return fmt.Errorf("schema %d", s.Schema)
	}
	if len(s.Players) != Seats || len(s.Ready) != Seats {
		return fmt.Errorf("%d players", len(s.Players))
	}
	seen := map[CardID]int{}
	count := 0
	for seat, p := range s.Players {
		for _, id := range p.Hand {
			if _, ok := Lookup(id); !ok {
				return fmt.Errorf("unknown card %s", id)
			}
			seen[id]++
			count++
			_ = seat
		}
	}
	for _, id := range s.Rest {
		seen[id]++
		count++
	}
	for _, p := range s.Trick {
		seen[p.Card]++
		count++
	}
	for _, id := range s.Played {
		seen[id]++
		count++
	}
	if count != 52 {
		return fmt.Errorf("%d cards accounted for, want 52", count)
	}
	for id, n := range seen {
		if n != 1 {
			return fmt.Errorf("card %s appears %d times", id, n)
		}
	}
	if len(s.Trick) > Seats {
		return fmt.Errorf("trick holds %d cards", len(s.Trick))
	}
	if s.Tricks[0]+s.Tricks[1] > CardsPerPlayer {
		return fmt.Errorf("%d tricks played", s.Tricks[0]+s.Tricks[1])
	}
	// Outside a trick every player holds the same number of cards.
	if s.Phase == PhasePlay && len(s.Trick) == 0 {
		for _, p := range s.Players[1:] {
			if len(p.Hand) != len(s.Players[0].Hand) {
				return fmt.Errorf("hands are uneven between tricks")
			}
		}
	}
	if s.Trump != "" && !s.Trump.Valid() {
		return fmt.Errorf("unknown trump %s", s.Trump)
	}
	if s.Phase == PhasePlay && s.Trump == "" {
		return fmt.Errorf("play started without a trump")
	}
	if s.Phase == PhaseSelect && s.Decider >= 0 && TeamOf(s.Decider) != s.EntitledTeam {
		return fmt.Errorf("seat %d may not choose for team %d", s.Decider, s.EntitledTeam)
	}
	return nil
}
