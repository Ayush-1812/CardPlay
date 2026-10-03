package trump

// Stage names every state the rules distinguish. Seven of them are resting
// stages: the game sits in one until somebody acts, and Stage reports which.
// Two are transition stages — dealing is automatic, so no state can rest
// there — and they are reported on the events that perform them, which is why
// they are named here too.
//
//	resting      waiting_for_players, trump_decision,
//	             delegated_trump_decision, active_trick, completed_trick,
//	             completed_round, waiting_for_readiness
//	transition   initial_deal, remaining_deal
type Stage string

const (
	// StageWaitingForPlayers is a table without its four players. The platform
	// starts a match only once four have readied, so no stored state rests
	// here; NewGame refuses anything else, and Stage reports it for a partial
	// table so the condition is never silent.
	StageWaitingForPlayers Stage = "waiting_for_players"
	// StageInitialDeal is the transition that gives every player five cards.
	StageInitialDeal Stage = "initial_deal"
	// StageTrumpDecision waits for either member of the entitled team.
	StageTrumpDecision Stage = "trump_decision"
	// StageDelegatedDecision waits for the teammate the choice was passed to.
	StageDelegatedDecision Stage = "delegated_trump_decision"
	// StageRemainingDeal is the transition that completes every hand to 13.
	StageRemainingDeal Stage = "remaining_deal"
	// StageActiveTrick waits for the next card of the trick in progress.
	StageActiveTrick Stage = "active_trick"
	// StageCompletedTrick is the finished trick still on the table, waiting
	// for its winner to lead the next one.
	StageCompletedTrick Stage = "completed_trick"
	// StageCompletedRound is a round just won, before anyone readies up.
	StageCompletedRound Stage = "completed_round"
	// StageWaitingReadiness is a finished round with some players ready.
	StageWaitingReadiness Stage = "waiting_for_readiness"
)

// Stage derives the resting stage from the state. It is a pure function of
// stored fields, so a reconnecting client and the server always agree.
func (s *State) Stage() Stage {
	if len(s.Players) != Seats {
		return StageWaitingForPlayers
	}
	switch s.Phase {
	case PhaseSelect:
		if s.Delegated {
			return StageDelegatedDecision
		}
		return StageTrumpDecision
	case PhasePlay:
		// Between tricks the finished one stays on the table until its winner
		// leads, so clients can show who took it.
		if len(s.Trick) == 0 && s.LastTrick != nil {
			return StageCompletedTrick
		}
		return StageActiveTrick
	case PhaseRoundOver:
		for _, ready := range s.Ready {
			if ready {
				return StageWaitingReadiness
			}
		}
		return StageCompletedRound
	}
	return StageWaitingForPlayers
}
