package monopoly

import (
	"cmp"
	"encoding/json"
	"slices"

	"cardplay/internal/game"
)

// Turn timeout (platform decision 2026-09-30, not in the printed rules): when
// the seat the game waits on makes no move within the timeout, the server
// plays the most passive legal move for it. It never plays a card from hand
// onto the table or targets anyone.
//
//   - Play phase: end the turn, returning the lowest-value excess cards.
//   - Response or Just Say No chain: accept (let the effect or last Just Say
//     No stand).
//   - Payment: pay from the bank first, then detached buildings, then
//     property cards and attached buildings, lowest value first, stopping
//     once the debt is covered; with too little value, pay everything.
//   - Placement: put each received card into an incomplete set of a legal
//     color, otherwise a new set of its first color; a multicolor wild
//     stays unassigned.
func (s *State) TimeoutAction(seat int) (Action, bool) {
	if !slices.Contains(s.WaitingFor(), seat) {
		return nil, false
	}
	p := &s.Players[seat]
	switch s.Phase {
	case PhasePlay:
		excess := max(0, len(p.Hand)-HandLimit)
		hand := slices.Clone(p.Hand)
		slices.SortStableFunc(hand, func(a, b CardID) int { return cmp.Compare(mustCard(a).Value, mustCard(b).Value) })
		return EndTurn{Return: hand[:excess]}, true
	case PhaseResponse:
		return Accept{Pending: s.Pending.ID, Step: s.Pending.Step}, true
	case PhasePayment:
		owed := s.Pending.Targets[s.Pending.Current].Owed
		rank := func(id CardID) int {
			switch {
			case slices.Contains(p.Bank, id):
				return 0
			case slices.Contains(p.Detached, id):
				return 1
			}
			return 2
		}
		all := p.payable()
		slices.SortStableFunc(all, func(a, b CardID) int {
			if c := cmp.Compare(rank(a), rank(b)); c != 0 {
				return c
			}
			return cmp.Compare(mustCard(a).Value, mustCard(b).Value)
		})
		total := 0
		for _, id := range all {
			total += mustCard(id).Value
		}
		if total <= owed {
			return Pay{Pending: s.Pending.ID, Step: s.Pending.Step, Cards: all}, true
		}
		pay, sum := []CardID{}, 0
		for _, id := range all {
			if sum >= owed {
				break
			}
			if v := mustCard(id).Value; v > 0 {
				pay = append(pay, id)
				sum += v
			}
		}
		return Pay{Pending: s.Pending.ID, Step: s.Pending.Step, Cards: pay}, true
	case PhasePlacement:
		id := p.Incoming[0]
		c := mustCard(id)
		if c.Kind == KindRainbowWild {
			return PlaceReceived{Card: id}, true
		}
		for _, color := range c.Colors {
			for _, set := range p.Sets {
				if set.Color == color && len(set.Cards) < colorInfo[color].Size {
					return PlaceReceived{Card: id, Set: set.ID}, true
				}
			}
		}
		return PlaceReceived{Card: id, Color: c.Colors[0]}, true
	}
	return nil, false
}

// payable lists the tabled cards a player may pay with: bank, set cards
// except multicolor wilds (no value), attached and detached buildings.
func (p *Player) payable() []CardID {
	all := slices.Clone(p.Bank)
	for _, set := range p.Sets {
		for _, id := range set.Cards {
			if mustCard(id).Kind != KindRainbowWild {
				all = append(all, id)
			}
		}
		if set.House != "" {
			all = append(all, set.House)
		}
		if set.Hotel != "" {
			all = append(all, set.Hotel)
		}
	}
	return append(all, p.Detached...)
}

// TimeoutMoves implements game.TimeoutPolicy: the default move for every
// seat the game is waiting on, in seat order.
func (Module) TimeoutMoves(st game.State) ([]game.TimeoutMove, error) {
	s, err := decode(st)
	if err != nil {
		return nil, err
	}
	var moves []game.TimeoutMove
	for _, seat := range s.WaitingFor() {
		a, ok := s.TimeoutAction(seat)
		if !ok {
			continue
		}
		payload, err := json.Marshal(a)
		if err != nil {
			return nil, err
		}
		moves = append(moves, game.TimeoutMove{UserID: s.Players[seat].UserID, Command: game.Command{Kind: a.Kind(), Payload: payload}})
	}
	return moves, nil
}

var _ game.TimeoutPolicy = Module{}
