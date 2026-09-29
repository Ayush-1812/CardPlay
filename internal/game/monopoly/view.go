package monopoly

import "slices"

// SetView is a public set with derived facts clients should not recompute.
type SetView struct {
	PropertySet
	Complete bool `json:"complete"`
	Rent     int  `json:"rent"`
}

// PublicPlayer is what every seat may see about a player: hand size only,
// and the tabled bank and property area (B1: hands are secret).
type PublicPlayer struct {
	Seat           int       `json:"seat"`
	UserID         string    `json:"user_id"`
	HandCount      int       `json:"hand_count"`
	Bank           []CardID  `json:"bank"`
	BankValue      int       `json:"bank_value"`
	Sets           []SetView `json:"sets"`
	Unassigned     []CardID  `json:"unassigned"`
	Detached       []CardID  `json:"detached"`
	Incoming       []CardID  `json:"incoming"`
	CompleteColors int       `json:"complete_colors"`
}

// PublicView is shared by every seat. It never includes hands, draw order or
// the seed; the center pile is face up.
type PublicView struct {
	Phase      Phase          `json:"phase"`
	Turn       int            `json:"turn"`
	Active     int            `json:"active"`
	PlaysLeft  int            `json:"plays_left"`
	DrawCount  int            `json:"draw_count"`
	CenterPile []CardID       `json:"center_pile"`
	Players    []PublicPlayer `json:"players"`
	Pending    *Pending       `json:"pending,omitempty"`
	WaitingFor []int          `json:"waiting_for"`
	Winner     *int           `json:"winner,omitempty"`
}

// SelfView is private to one seat.
type SelfView struct {
	Seat int      `json:"seat"`
	Hand []CardID `json:"hand"`
}

// PlayerView is the complete projection for one seat.
type PlayerView struct {
	Public       PublicView `json:"public"`
	Self         SelfView   `json:"self"`
	LegalActions []string   `json:"legal_actions"`
}

// ViewFor builds seat's projection. Pending holds only public declarations,
// responses and frozen amounts, never whether anyone holds Just Say No.
func (s *State) ViewFor(seat int) PlayerView {
	pub := PublicView{
		Phase: s.Phase, Turn: s.Turn, Active: s.Active, PlaysLeft: MaxPlays - s.PlaysUsed,
		DrawCount: len(s.Draw), CenterPile: slices.Clone(s.Discard), WaitingFor: s.WaitingFor(),
	}
	if pub.CenterPile == nil {
		pub.CenterPile = []CardID{}
	}
	for i, p := range s.Players {
		pp := PublicPlayer{
			Seat: i, UserID: p.UserID, HandCount: len(p.Hand), Bank: slices.Clone(p.Bank),
			Unassigned: slices.Clone(p.Unassigned), Detached: slices.Clone(p.Detached), Incoming: slices.Clone(p.Incoming),
			CompleteColors: p.CompleteColors(), Sets: []SetView{},
		}
		for _, id := range p.Bank {
			pp.BankValue += mustCard(id).Value
		}
		for _, set := range p.Sets {
			set.Cards = slices.Clone(set.Cards)
			pp.Sets = append(pp.Sets, SetView{PropertySet: set, Complete: set.Complete(), Rent: set.Rent()})
		}
		pub.Players = append(pub.Players, pp)
	}
	if s.Pending != nil {
		pub.Pending = s.Clone().Pending
	}
	if s.Winner != nil {
		w := *s.Winner
		pub.Winner = &w
	}
	return PlayerView{
		Public:       pub,
		Self:         SelfView{Seat: seat, Hand: slices.Clone(s.Players[seat].Hand)},
		LegalActions: s.legalActions(seat),
	}
}

// WaitingFor lists the seats whose input the game needs now.
func (s *State) WaitingFor() []int {
	switch s.Phase {
	case PhasePlay:
		return []int{s.Active}
	case PhaseResponse, PhasePayment:
		t := s.Pending.Targets[s.Pending.Current]
		if t.Stage == StageChain {
			return []int{t.Chain.Waiting}
		}
		return []int{t.Seat}
	case PhasePlacement:
		var seats []int
		for i, p := range s.Players {
			if len(p.Incoming) > 0 {
				seats = append(seats, i)
			}
		}
		return seats
	}
	return []int{}
}

// legalActions lists action kinds the seat may submit now. It is guidance for
// clients; Apply remains the only authority.
func (s *State) legalActions(seat int) []string {
	out := []string{}
	if !slices.Contains(s.WaitingFor(), seat) {
		return out
	}
	hand := s.Players[seat].Hand
	holds := func(match func(Card) bool) bool {
		return slices.ContainsFunc(hand, func(id CardID) bool { return match(mustCard(id)) })
	}
	action := func(a ActionType) func(Card) bool {
		return func(c Card) bool { return c.Kind == KindAction && c.Action == a }
	}
	switch s.Phase {
	case PhasePlay:
		if s.PlaysUsed < MaxPlays {
			if holds(func(c Card) bool { return !c.IsProperty() }) {
				out = append(out, "bank")
			}
			if holds(Card.IsProperty) {
				out = append(out, "play_property")
			}
			for _, a := range []struct {
				kind string
				card ActionType
			}{{"pass_go", PassGo}, {"play_building", House}, {"play_building", Hotel}, {"sly_deal", SlyDeal}, {"forced_deal", ForcedDeal}, {"deal_breaker", DealBreaker}, {"debt_collector", DebtCollector}, {"birthday", Birthday}} {
				if holds(action(a.card)) && !slices.Contains(out, a.kind) {
					out = append(out, a.kind)
				}
			}
			if holds(func(c Card) bool { return c.Kind == KindRent || c.Kind == KindRentAny }) {
				out = append(out, "rent")
			}
		}
		out = append(out, "rearrange", "end_turn")
	case PhaseResponse:
		out = append(out, "accept")
		if holds(action(JustSayNo)) {
			out = append(out, "just_say_no")
		}
	case PhasePayment:
		out = append(out, "pay")
	case PhasePlacement:
		out = append(out, "place_received")
	}
	return out
}
