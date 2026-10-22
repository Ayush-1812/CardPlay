package bluff

import (
	"fmt"
	"slices"
	"time"
)

// SchemaVersion is the stored state format. Bump it on any breaking change.
const SchemaVersion = 1

// MinPlayers and MaxPlayers bound a table. Three is the smallest game where
// a challenge has anyone to come from other than the two players involved;
// six is where the deal stops dividing usefully.
const (
	MinPlayers = 3
	MaxPlayers = 6
)

// maxPlay is the most cards one play may put down: the four of a rank in
// one deck, and four more for every extra deck in use.
func (s *State) maxPlay() int { return CopiesPerRank * max(1, s.Decks) }

// ChallengeWindow is how long anyone may call a bluff. It is a CardPlay
// policy, not a rule of the game: five seconds is long enough to decide and
// short enough that the table does not stall.
const ChallengeWindow = 5 * time.Second

// Phase is what the game is waiting for.
type Phase string

const (
	// PhasePlay waits for the player whose turn it is to put cards down.
	PhasePlay Phase = "play"
	// PhaseChallenge waits for everyone else to accept the play or call it.
	PhaseChallenge Phase = "challenge"
	// PhaseOver is a finished game with a winner.
	PhaseOver Phase = "over"
)

// Player is one seat. Hand is private to that seat and never projected to
// anyone else.
type Player struct {
	UserID string   `json:"user_id"`
	Hand   []CardID `json:"hand"`
}

// Play is the latest batch on the pile. Cards are the actual cards put
// down, which stay secret unless the play is challenged; Rank and Count are
// the claim, which is public the moment it is made.
type Play struct {
	ID    string   `json:"id"`
	Seat  int      `json:"seat"`
	Rank  Rank     `json:"rank"`
	Count int      `json:"count"`
	Cards []CardID `json:"cards"`
}

// Truthful reports whether every card in the batch is the declared rank.
// Only a challenge may ask this: nothing projects it.
func (p *Play) Truthful() bool {
	for _, id := range p.Cards {
		if mustCard(id).Rank != p.Rank {
			return false
		}
	}
	return true
}

// Reveal is a resolved challenge, kept so the table can be shown what
// happened. Everything in it is public: these cards were turned face up.
type Reveal struct {
	PlayID string `json:"play_id"`
	// Claimed is what the player said they were putting down.
	Claimed Rank `json:"claimed"`
	// Cards are the cards of that one batch, now face up. No older card is
	// ever revealed, however the pile moves.
	Cards []CardID `json:"cards"`
	// Player made the play; Challenger called it.
	Player     int `json:"player"`
	Challenger int `json:"challenger"`
	// Truthful says whether the claim held up, and Collector is the seat
	// that took the whole pile because of it.
	Truthful  bool `json:"truthful"`
	Collector int  `json:"collector"`
	// Collected is how many cards the pile held when it was taken.
	Collected int `json:"collected"`
}

// State is the whole game. It is stored as one JSON snapshot per revision
// and is server-only: clients receive projections from ViewFor.
type State struct {
	Schema int `json:"schema"`
	// Decks is how many 52-card decks this game was dealt from. Every copy
	// of a card has its own id, so the deck is still a set.
	Decks   int      `json:"decks"`
	Players []Player `json:"players"`

	Phase Phase `json:"phase"`
	// Turn is the seat that must play next.
	Turn int `json:"turn"`
	// Rank is what the next play must be declared as.
	Rank Rank `json:"rank"`

	// Pile is every card put down and not yet collected, the latest batch
	// included. Face down: nothing projects it.
	Pile []CardID `json:"pile"`
	// Last is the batch that may still be challenged, or nil.
	Last *Play `json:"last,omitempty"`
	// Accepted marks seats that have given up their right to challenge the
	// current play. The player who made it is marked from the start.
	Accepted []bool `json:"accepted"`
	// Deadline is when the challenge window closes. The platform stamps the
	// time on the command that opened it; clients count down to this.
	Deadline time.Time `json:"deadline,omitempty"`

	// Pending is the seat that has played its last card and wins if the
	// play survives, or -1.
	Pending int `json:"pending"`
	// Winner is the seat that won, or -1 while the game runs.
	Winner int `json:"winner"`
	// Result is the last resolved challenge, shown until the next play.
	Result *Reveal `json:"result,omitempty"`
	// Plays counts accepted plays, which gives each one a stable id.
	Plays int `json:"plays"`
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

// next returns the seat clockwise after this one.
func (s *State) next(seat int) int { return (seat + 1) % len(s.Players) }

// mayChallenge reports whether a seat may still challenge the latest play.
// The player who made it never may, and accepting is binding.
func (s *State) mayChallenge(seat int) bool {
	if s.Phase != PhaseChallenge || s.Last == nil {
		return false
	}
	if seat < 0 || seat >= len(s.Accepted) {
		return false
	}
	return !s.Accepted[seat]
}

// pendingSeats lists the seats still to answer the challenge window.
func (s *State) pendingSeats() []int {
	var out []int
	for seat := range s.Players {
		if s.mayChallenge(seat) {
			out = append(out, seat)
		}
	}
	return out
}

// CheckInvariants verifies what must hold after every transition. A
// violation is an engine bug, never a player mistake.
func (s *State) CheckInvariants() error {
	if s.Schema != SchemaVersion {
		return fmt.Errorf("schema %d", s.Schema)
	}
	if len(s.Players) < MinPlayers || len(s.Players) > MaxPlayers {
		return fmt.Errorf("%d players", len(s.Players))
	}
	if len(s.Accepted) != len(s.Players) {
		return fmt.Errorf("%d acceptance flags for %d players", len(s.Accepted), len(s.Players))
	}
	// Every card is in exactly one place: a hand or the pile.
	seen := map[CardID]int{}
	count := 0
	for _, p := range s.Players {
		for _, id := range p.Hand {
			if _, ok := Lookup(id); !ok {
				return fmt.Errorf("unknown card %s", id)
			}
			seen[id]++
			count++
		}
	}
	for _, id := range s.Pile {
		seen[id]++
		count++
	}
	if s.Decks < 1 || s.Decks > MaxDecks {
		return fmt.Errorf("%d decks", s.Decks)
	}
	if want := DeckSize * s.Decks; count != want {
		return fmt.Errorf("%d cards accounted for, want %d", count, want)
	}
	for id, n := range seen {
		if n != 1 {
			return fmt.Errorf("card %s appears %d times", id, n)
		}
	}
	if !s.Rank.Valid() {
		return fmt.Errorf("unknown required rank %q", s.Rank)
	}
	if s.Turn < 0 || s.Turn >= len(s.Players) {
		return fmt.Errorf("turn is seat %d", s.Turn)
	}
	if s.Last != nil {
		if s.Last.Count != len(s.Last.Cards) {
			return fmt.Errorf("the latest play claims %d cards and holds %d", s.Last.Count, len(s.Last.Cards))
		}
		if s.Last.Count < 1 || s.Last.Count > s.maxPlay() {
			return fmt.Errorf("the latest play holds %d cards", s.Last.Count)
		}
		// The batch that may be challenged must still be on top of the pile.
		if len(s.Pile) < len(s.Last.Cards) {
			return fmt.Errorf("the pile is smaller than its latest batch")
		}
		top := s.Pile[len(s.Pile)-len(s.Last.Cards):]
		for i, id := range s.Last.Cards {
			if top[i] != id {
				return fmt.Errorf("the latest batch is not on top of the pile")
			}
		}
	}
	if s.Phase == PhaseChallenge && s.Last == nil {
		return fmt.Errorf("a challenge window with nothing to challenge")
	}
	if s.Phase == PhaseOver {
		if s.Winner < 0 || s.Winner >= len(s.Players) {
			return fmt.Errorf("the game is over with winner %d", s.Winner)
		}
		if len(s.Players[s.Winner].Hand) != 0 {
			return fmt.Errorf("the winner still holds %d cards", len(s.Players[s.Winner].Hand))
		}
	} else if s.Winner != -1 {
		return fmt.Errorf("a winner (%d) while the game runs", s.Winner)
	}
	if s.Pending != -1 {
		if s.Pending < 0 || s.Pending >= len(s.Players) {
			return fmt.Errorf("pending winner is seat %d", s.Pending)
		}
		if len(s.Players[s.Pending].Hand) != 0 {
			return fmt.Errorf("the pending winner holds %d cards", len(s.Players[s.Pending].Hand))
		}
	}
	return nil
}

// clone deep-copies the state so a rejected command cannot leave anything
// half-changed.
func (s *State) clone() *State {
	out := *s
	out.Players = make([]Player, len(s.Players))
	for i, p := range s.Players {
		out.Players[i] = Player{UserID: p.UserID, Hand: slices.Clone(p.Hand)}
	}
	out.Pile = slices.Clone(s.Pile)
	out.Accepted = slices.Clone(s.Accepted)
	if s.Last != nil {
		last := *s.Last
		last.Cards = slices.Clone(s.Last.Cards)
		out.Last = &last
	}
	if s.Result != nil {
		result := *s.Result
		result.Cards = slices.Clone(s.Result.Cards)
		out.Result = &result
	}
	return &out
}
