package bluff

import (
	"encoding/json"
	"slices"
	"time"

	"cardplay/internal/game"
)

// PublicView is what every player at the table may see. It carries no card
// identity at all except the cards a resolved challenge turned face up.
type PublicView struct {
	Phase Phase `json:"phase"`
	// Turn is the seat that must play; Rank is what they must declare.
	Turn int  `json:"turn"`
	Rank Rank `json:"rank"`
	// RankName is the word for the rank, so every client says the same
	// thing: "3 Queens".
	RankName string `json:"rank_name"`
	// Counts is how many cards each seat holds, by seat.
	Counts []int `json:"counts"`
	// PileCount is how many cards are face down in the middle.
	PileCount int `json:"pile_count"`
	// Last is the claim that may still be challenged, without its cards.
	Last *Claim `json:"last,omitempty"`
	// Accepted marks seats that can no longer challenge the current play.
	Accepted []bool `json:"accepted"`
	// Deadline is when the challenge window closes, as the server sees it.
	Deadline *time.Time `json:"deadline,omitempty"`
	// Pending is the seat that wins if the current play survives, or -1.
	Pending int `json:"pending"`
	// Winner is the seat that won, or -1.
	Winner int `json:"winner"`
	// Result is the last resolved challenge, cards and all: those were
	// turned face up for everyone.
	Result *Reveal `json:"result,omitempty"`
	// MaxCards is how many cards one play may hold, which grows with the
	// number of decks in play.
	MaxCards int `json:"max_cards"`
	// Decks is how many decks this game was dealt from.
	Decks int `json:"decks"`
}

// Claim is a declaration without the cards behind it: the count and the
// rank are public the moment a play is made, the cards are not.
type Claim struct {
	ID    string `json:"id"`
	Seat  int    `json:"seat"`
	Rank  Rank   `json:"rank"`
	Count int    `json:"count"`
	// RankName is the word to print, singular or plural to match Count.
	RankName string `json:"rank_name"`
}

// SelfView is what one player sees of their own position.
type SelfView struct {
	Seat int      `json:"seat"`
	Hand []CardID `json:"hand"`
	// MayPlay is true when it is this player's turn to put cards down.
	MayPlay bool `json:"may_play"`
	// MayAnswer is true while this player may still accept or challenge the
	// current play; AnswerID is the play they would be answering.
	MayAnswer bool   `json:"may_answer"`
	AnswerID  string `json:"answer_id,omitempty"`
}

// ViewFor projects the state for one seat. Nothing here reads another
// player's hand or the pile's contents, so no projection can leak them.
func (s *State) ViewFor(seat int) View {
	counts := make([]int, len(s.Players))
	for i, p := range s.Players {
		counts[i] = len(p.Hand)
	}
	pub := PublicView{
		Phase:     s.Phase,
		Turn:      s.Turn,
		Rank:      s.Rank,
		RankName:  RankName(s.Rank, true),
		Counts:    counts,
		PileCount: len(s.Pile),
		Accepted:  slices.Clone(s.Accepted),
		Pending:   s.Pending,
		Winner:    s.Winner,
		Result:    s.Result,
		MaxCards:  s.maxPlay(),
		Decks:     s.Decks,
	}
	if s.Last != nil {
		pub.Last = &Claim{
			ID:       s.Last.ID,
			Seat:     s.Last.Seat,
			Rank:     s.Last.Rank,
			Count:    s.Last.Count,
			RankName: RankName(s.Last.Rank, s.Last.Count != 1),
		}
	}
	if !s.Deadline.IsZero() {
		deadline := s.Deadline
		pub.Deadline = &deadline
	}
	self := SelfView{Seat: seat, Hand: []CardID{}}
	if seat >= 0 && seat < len(s.Players) {
		self.Hand = slices.Clone(s.Players[seat].Hand)
		self.MayPlay = s.Phase == PhasePlay && s.Turn == seat
		self.MayAnswer = s.mayChallenge(seat)
		if self.MayAnswer && s.Last != nil {
			self.AnswerID = s.Last.ID
		}
	}
	return View{Public: pub, Self: self}
}

// View pairs the two projections before they are marshalled.
type View struct {
	Public PublicView
	Self   SelfView
}

// Project marshals a view into the platform's envelope. Legal actions are
// listed so a client can be certain what it may send.
func (s *State) Project(seat int) (game.View, error) {
	v := s.ViewFor(seat)
	public, err := json.Marshal(v.Public)
	if err != nil {
		return game.View{}, err
	}
	self, err := json.Marshal(v.Self)
	if err != nil {
		return game.View{}, err
	}
	actions := []string{}
	if v.Self.MayPlay {
		actions = append(actions, "play_cards")
	}
	if v.Self.MayAnswer {
		actions = append(actions, "accept_play", "call_bluff")
	}
	return game.View{Public: public, Self: self, LegalActions: actions}, nil
}
