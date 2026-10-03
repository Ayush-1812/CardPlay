package trump

import "slices"

// PublicView is what every seated player may see. It never contains another
// player's cards, the undealt remainder or the seed (spec rule 21).
type PublicView struct {
	Phase        Phase         `json:"phase"`
	Stage        Stage         `json:"stage"`
	Round        int           `json:"round"`
	Trump        Suit          `json:"trump,omitempty"`
	LeadSuit     Suit          `json:"lead_suit,omitempty"`
	EntitledTeam int           `json:"entitled_team"`
	Decider      int           `json:"decider"`
	Chooser      int           `json:"chooser"`
	Delegated    bool          `json:"delegated"`
	Leader       int           `json:"leader"`
	Turn         int           `json:"turn"`
	Trick        []Play        `json:"trick"`
	LastTrick    *Trick        `json:"last_trick,omitempty"`
	Tricks       [2]int        `json:"tricks"`
	RoundsWon    [2]int        `json:"rounds_won"`
	TricksToWin  int           `json:"tricks_to_win"`
	Players      []PublicSeat  `json:"players"`
	History      []RoundResult `json:"history"`
}

// PublicSeat is one player as everyone sees them.
type PublicSeat struct {
	Seat  int    `json:"seat"`
	Team  int    `json:"team"`
	Cards int    `json:"cards"`
	Ready bool   `json:"ready"`
	User  string `json:"user_id"`
}

// SelfView is the requesting player's own information.
type SelfView struct {
	Seat int      `json:"seat"`
	Team int      `json:"team"`
	Hand []CardID `json:"hand"`
	// Legal lists the cards playable right now, so the client can dim the
	// rest rather than guess the follow-suit rule.
	Legal []CardID `json:"legal"`
	// MayChoose is true while this player may name the trump, MayDelegate
	// while they may still pass that decision to their teammate.
	MayChoose   bool `json:"may_choose"`
	MayDelegate bool `json:"may_delegate"`
}

// View bundles both projections with the actions the player may take.
type View struct {
	Public       PublicView
	Self         SelfView
	LegalActions []string
}

// ViewFor builds one seat's view. It is the only way state reaches a client.
func (s *State) ViewFor(seat int) View {
	pub := PublicView{
		Phase: s.Phase, Stage: s.Stage(), LastTrick: s.LastTrick, Round: s.Round, Trump: s.Trump, LeadSuit: s.leadSuit(),
		EntitledTeam: s.EntitledTeam, Decider: s.Decider, Chooser: s.Chooser,
		Delegated: s.Delegated, Leader: s.Leader, Turn: s.Turn,
		Trick: slices.Clone(s.Trick), Tricks: s.Tricks, RoundsWon: s.RoundsWon,
		TricksToWin: TricksToWin, History: slices.Clone(s.History),
	}
	if pub.Trick == nil {
		pub.Trick = []Play{}
	}
	if pub.History == nil {
		pub.History = []RoundResult{}
	}
	for i, p := range s.Players {
		pub.Players = append(pub.Players, PublicSeat{
			Seat: i, Team: TeamOf(i), Cards: len(p.Hand), Ready: s.Ready[i], User: p.UserID,
		})
	}

	self := SelfView{Seat: seat, Team: TeamOf(seat), Hand: slices.Clone(s.Players[seat].Hand), Legal: []CardID{}}
	if self.Hand == nil {
		self.Hand = []CardID{}
	}
	var actions []string
	switch s.Phase {
	case PhaseSelect:
		if s.mayDecide(seat) == nil {
			self.MayChoose = true
			self.MayDelegate = !s.Delegated
			actions = append(actions, "choose_trump")
			if self.MayDelegate {
				actions = append(actions, "delegate_trump")
			}
		}
	case PhasePlay:
		if seat == s.Turn {
			self.Legal = s.legalCards(seat)
			actions = append(actions, "play_card")
		}
	case PhaseRoundOver:
		if !s.Ready[seat] {
			actions = append(actions, "ready_round")
		}
	}
	return View{Public: pub, Self: self, LegalActions: actions}
}

// ChatOpen reports whether room chat is accepted right now. It is closed
// during trump selection so nobody can describe their hand, and opens again
// once the trump is named (spec rule 20, owner decision 2026-10-03).
func (s *State) ChatOpen() bool { return s.Phase != PhaseSelect }

// AwaitedSeats lists the seats the game is waiting for.
func (s *State) AwaitedSeats() []int {
	switch s.Phase {
	case PhaseSelect:
		if s.Decider >= 0 {
			return []int{s.Decider}
		}
		seats := SeatsOfTeam(s.EntitledTeam)
		return []int{seats[0], seats[1]}
	case PhasePlay:
		return []int{s.Turn}
	case PhaseRoundOver:
		var waiting []int
		for seat, ready := range s.Ready {
			if !ready {
				waiting = append(waiting, seat)
			}
		}
		return waiting
	}
	return nil
}
