package trump

import "slices"

// TimeoutAction is the most passive legal move for one seat, used when a
// connected player makes no move within the platform's turn timeout. It never
// reveals a choice a player would not obviously make:
//
//   - choosing trump: the suit the player holds most of, ties by suit order;
//   - playing: the lowest card that follows the rules;
//   - after a round: ready up, so the table is not stuck.
//
// Delegating is never chosen for a player, because that would hand a real
// decision to someone else.
func (s *State) TimeoutAction(seat int) (Action, bool) {
	switch s.Phase {
	case PhaseSelect:
		if s.mayDecide(seat) != nil {
			return nil, false
		}
		return ChooseTrump{Suit: s.longestSuit(seat)}, true
	case PhasePlay:
		if seat != s.Turn {
			return nil, false
		}
		legal := s.legalCards(seat)
		if len(legal) == 0 {
			return nil, false
		}
		lowest := legal[0]
		for _, id := range legal[1:] {
			if mustCard(id).Rank.Strength() < mustCard(lowest).Rank.Strength() {
				lowest = id
			}
		}
		return PlayCard{Card: lowest}, true
	case PhaseRoundOver:
		if s.Ready[seat] {
			return nil, false
		}
		return ReadyRound{}, true
	}
	return nil, false
}

// longestSuit is the suit a seat holds most of; ties go to the earlier suit
// in Suits, so the choice is deterministic.
func (s *State) longestSuit(seat int) Suit {
	best, count := Suits[0], -1
	for _, suit := range Suits {
		if n := len(suitOf(s.Players[seat].Hand, suit)); n > count {
			best, count = suit, n
		}
	}
	return best
}

// TimeoutSeats lists the seats a default move would be made for. During trump
// selection, when either teammate may still act, only the lower seat is moved
// for, so the two are never both played at once.
func (s *State) TimeoutSeats() []int {
	seats := s.AwaitedSeats()
	if s.Phase == PhaseSelect && len(seats) > 1 {
		return seats[:1]
	}
	return slices.Clone(seats)
}
