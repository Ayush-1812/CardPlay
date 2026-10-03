package trump

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
)

// Rejection codes. A rejected action never changes state.
const (
	CodeInvalidAction = "INVALID_ACTION"
	CodeNotYourTurn   = "NOT_YOUR_TURN"
	CodeWrongPhase    = "WRONG_PHASE"
	CodeInvalidCard   = "INVALID_CARD"
	CodeIllegal       = "ILLEGAL_MOVE"
)

// RuleError is a rejected action.
type RuleError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *RuleError) Error() string { return e.Code + ": " + e.Message }

// RejectionCode implements game.Rejection.
func (e *RuleError) RejectionCode() string { return e.Code }

func reject(code, format string, args ...any) *RuleError {
	return &RuleError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Public is the audience of events every seat may see.
const Public = -1

// Event records what happened. Audience is Public or one seat, so a dealt
// hand reaches only its owner.
type Event struct {
	Kind     string         `json:"kind"`
	Audience int            `json:"audience"`
	Data     map[string]any `json:"data,omitempty"`
}

// InvariantError reports a transition that would break a state invariant: an
// engine bug, never a player mistake.
type InvariantError struct{ Detail string }

func (e *InvariantError) Error() string { return "trump rules engine invariant violated" }

// Action is one player intent.
type Action interface{ Kind() string }

// ChooseTrump names the trump suit for this round.
type ChooseTrump struct {
	Suit Suit `json:"suit"`
}

// DelegateTrump passes the decision to the teammate, once (spec rule 18).
type DelegateTrump struct{}

// PlayCard plays one card into the current trick.
type PlayCard struct {
	Card CardID `json:"card"`
}

// ReadyRound marks one seat ready for the next round.
type ReadyRound struct{}

func (ChooseTrump) Kind() string   { return "choose_trump" }
func (DelegateTrump) Kind() string { return "delegate_trump" }
func (PlayCard) Kind() string      { return "play_card" }
func (ReadyRound) Kind() string    { return "ready_round" }

// DecodeAction builds a typed action from a wire kind and JSON payload.
// Unknown fields are rejected.
func DecodeAction(kind string, payload json.RawMessage) (Action, error) {
	var a Action
	switch kind {
	case "choose_trump":
		a = &ChooseTrump{}
	case "delegate_trump":
		a = &DelegateTrump{}
	case "play_card":
		a = &PlayCard{}
	case "ready_round":
		a = &ReadyRound{}
	default:
		return nil, reject(CodeInvalidAction, "unknown action %q", kind)
	}
	if len(payload) > 0 && string(payload) != "null" {
		dec := json.NewDecoder(bytes.NewReader(payload))
		dec.DisallowUnknownFields()
		if err := dec.Decode(a); err != nil {
			return nil, reject(CodeInvalidAction, "malformed %s payload", kind)
		}
	}
	// Return the value, not the pointer, so Apply's type switch stays simple.
	switch v := a.(type) {
	case *ChooseTrump:
		return *v, nil
	case *DelegateTrump:
		return *v, nil
	case *PlayCard:
		return *v, nil
	case *ReadyRound:
		return *v, nil
	}
	return nil, reject(CodeInvalidAction, "unknown action %q", kind)
}

// ErrPlayers reports an invalid seating.
var ErrPlayers = errors.New("trump needs exactly four distinct players")

// NewGame seats four players, tosses once for the team that chooses the first
// trump, and deals five cards each (spec rules 1, 9, 13). The seed comes from
// random, which must be crypto/rand in production.
func NewGame(userIDs []string, random io.Reader) (*State, []Event, error) {
	if len(userIDs) != Seats {
		return nil, nil, ErrPlayers
	}
	seen := map[string]bool{}
	for _, id := range userIDs {
		if id == "" || seen[id] {
			return nil, nil, ErrPlayers
		}
		seen[id] = true
	}
	seed := make([]byte, SeedSize)
	if _, err := io.ReadFull(random, seed); err != nil {
		return nil, nil, err
	}
	s := &State{Schema: SchemaVersion, Seed: seed, Ready: make([]bool, Seats)}
	for _, id := range userIDs {
		s.Players = append(s.Players, Player{UserID: id, Hand: []CardID{}})
	}
	// The toss happens once per match. Later rounds inherit entitlement from
	// the round winner, so nothing here ever runs again (spec rules 9-12).
	s.EntitledTeam = pick(Teams, s.Seed, 0)
	events := []Event{{Kind: "toss_resolved", Audience: Public, Data: map[string]any{
		"team": s.EntitledTeam, "seats": SeatsOfTeam(s.EntitledTeam),
	}}}
	return s, append(events, s.startRound()...), nil
}

// startRound shuffles a fresh deck, deals five to each seat and opens the
// trump selection for the entitled team.
func (s *State) startRound() []Event {
	s.Round++
	s.Deals++
	deck := make([]CardID, 0, 52)
	for _, c := range Manifest() {
		deck = append(deck, c.ID)
	}
	shuffle(deck, s.Seed, s.Deals)
	for seat := range s.Players {
		s.Players[seat].Hand = slices.Clone(deck[seat*FirstDeal : (seat+1)*FirstDeal])
	}
	s.Rest = slices.Clone(deck[Seats*FirstDeal:])
	s.Phase = PhaseSelect
	s.Decider = -1
	s.Chooser = -1
	s.Delegated = false
	s.Trump = ""
	s.Trick = nil
	s.LastTrick = nil
	s.Played = nil
	s.Tricks = [2]int{}
	s.Ready = make([]bool, Seats)

	events := []Event{{Kind: "round_started", Audience: Public, Data: map[string]any{
		"round": s.Round, "entitled_team": s.EntitledTeam, "seats": SeatsOfTeam(s.EntitledTeam),
	}}}
	for seat, p := range s.Players {
		events = append(events, Event{Kind: "dealt_five", Audience: seat, Data: map[string]any{"stage": StageInitialDeal, "cards": slices.Clone(p.Hand)}})
	}
	return events
}

// Apply validates and applies one action by seat. It works on a clone and
// returns the new state only on success, so an illegal action leaves the
// original untouched.
func Apply(s *State, seat int, a Action) (*State, []Event, error) {
	if seat < 0 || seat >= Seats {
		return s, nil, reject(CodeInvalidAction, "unknown seat %d", seat)
	}
	next := s.clone()
	events, err := next.apply(seat, a)
	if err != nil {
		return s, nil, err
	}
	if err := next.CheckInvariants(); err != nil {
		return s, nil, &InvariantError{Detail: err.Error()}
	}
	return next, events, nil
}

func (s *State) apply(seat int, a Action) ([]Event, error) {
	switch a := a.(type) {
	case ChooseTrump:
		return s.chooseTrump(seat, a)
	case DelegateTrump:
		return s.delegate(seat)
	case PlayCard:
		return s.playCard(seat, a)
	case ReadyRound:
		return s.readyRound(seat)
	}
	return nil, reject(CodeInvalidAction, "unsupported action %T", a)
}

// mayDecide reports whether this seat is allowed to act in trump selection.
// Before anyone has acted either member of the entitled team may take the
// decision; once it is delegated only the teammate may choose (owner decision
// 2026-10-03, spec rules 16-18).
func (s *State) mayDecide(seat int) error {
	if s.Phase != PhaseSelect {
		return reject(CodeWrongPhase, "the trump has already been chosen")
	}
	if s.Decider >= 0 {
		if seat != s.Decider {
			return reject(CodeNotYourTurn, "seat %d is choosing the trump", s.Decider)
		}
		return nil
	}
	if TeamOf(seat) != s.EntitledTeam {
		return reject(CodeNotYourTurn, "team %d chooses the trump this round", s.EntitledTeam)
	}
	return nil
}

// chooseTrump names the suit, deals the remaining eight cards to each player
// and gives the lead to whoever chose (spec rules 14, 19).
func (s *State) chooseTrump(seat int, a ChooseTrump) ([]Event, error) {
	if err := s.mayDecide(seat); err != nil {
		return nil, err
	}
	if !a.Suit.Valid() {
		return nil, reject(CodeInvalidAction, "%q is not a suit", a.Suit)
	}
	s.Trump = a.Suit
	s.Chooser = seat
	s.Decider = seat
	s.Phase = PhasePlay
	s.Leader, s.Turn = seat, seat

	events := []Event{{Kind: "trump_chosen", Audience: Public, Data: map[string]any{
		"suit": a.Suit, "seat": seat, "delegated": s.Delegated,
	}}}
	// The second deal: eight more each, so everyone holds thirteen.
	per := CardsPerPlayer - FirstDeal
	for i := range s.Players {
		dealt := slices.Clone(s.Rest[i*per : (i+1)*per])
		s.Players[i].Hand = append(s.Players[i].Hand, dealt...)
		events = append(events, Event{Kind: "dealt_rest", Audience: i, Data: map[string]any{"stage": StageRemainingDeal, "cards": dealt}})
	}
	s.Rest = nil
	return append(events, Event{Kind: "lead_passed", Audience: Public, Data: map[string]any{"seat": seat}}), nil
}

// delegate hands the decision to the teammate. It can happen once and cannot
// be returned (spec rule 18).
func (s *State) delegate(seat int) ([]Event, error) {
	if err := s.mayDecide(seat); err != nil {
		return nil, err
	}
	if s.Delegated {
		return nil, reject(CodeIllegal, "the choice was already passed to you; you must choose a suit")
	}
	s.Delegated = true
	s.Decider = PartnerOf(seat)
	return []Event{{Kind: "trump_delegated", Audience: Public, Data: map[string]any{
		"from": seat, "to": s.Decider,
	}}}, nil
}

// playCard plays one legal card into the current trick, resolving the trick
// on the fourth card (spec rules 22-27).
func (s *State) playCard(seat int, a PlayCard) ([]Event, error) {
	if s.Phase != PhasePlay {
		return nil, reject(CodeWrongPhase, "no trick is in progress")
	}
	if seat != s.Turn {
		return nil, reject(CodeNotYourTurn, "it is seat %d's turn", s.Turn)
	}
	if !slices.Contains(s.Players[seat].Hand, a.Card) {
		return nil, reject(CodeInvalidCard, "%s is not in your hand", a.Card)
	}
	if !slices.Contains(s.legalCards(seat), a.Card) {
		return nil, reject(CodeIllegal, "you must follow %s", s.leadSuit())
	}
	hand := s.Players[seat].Hand
	s.Players[seat].Hand = slices.Delete(slices.Clone(hand), slices.Index(hand, a.Card), slices.Index(hand, a.Card)+1)
	s.Trick = append(s.Trick, Play{Seat: seat, Card: a.Card})
	events := []Event{{Kind: "card_played", Audience: Public, Data: map[string]any{
		"seat": seat, "card": a.Card, "lead": s.leadSuit(),
	}}}
	if len(s.Trick) < Seats {
		s.Turn = next(seat)
		return events, nil
	}
	return append(events, s.resolveTrick()...), nil
}

// resolveTrick awards the trick and either ends the round at seven or gives
// the lead to the winner (spec rules 27-28).
func (s *State) resolveTrick() []Event {
	winner := s.trickWinner()
	team := TeamOf(winner)
	s.Tricks[team]++
	for _, p := range s.Trick {
		s.Played = append(s.Played, p.Card)
	}
	cards := make([]CardID, 0, Seats)
	for _, p := range s.Trick {
		cards = append(cards, p.Card)
	}
	events := []Event{{Kind: "trick_won", Audience: Public, Data: map[string]any{
		"seat": winner, "team": team, "cards": cards, "tricks": s.Tricks,
	}}}
	// Keep the finished trick on the table until its winner leads again, so
	// every client can show who took it (stage completed_trick).
	s.LastTrick = &Trick{Cards: slices.Clone(s.Trick), Lead: s.leadSuit(), Winner: winner, Team: team}
	s.Trick = nil
	if s.Tricks[team] >= TricksToWin {
		return append(events, s.endRound(team)...)
	}
	s.Leader, s.Turn = winner, winner
	return events
}

// endRound stops the round the moment a team reaches seven tricks; the
// remaining cards are not played (spec rule 28, owner decision 2026-10-03).
func (s *State) endRound(team int) []Event {
	s.RoundsWon[team]++
	s.History = append(s.History, RoundResult{
		Round: s.Round, Trump: s.Trump, Chooser: s.Chooser,
		Delegated: s.Delegated, Winner: team, Tricks: s.Tricks,
	})
	s.Phase = PhaseRoundOver
	// The winners choose the next round's trump (spec rule 11). Nobody is
	// deciding until that round is dealt.
	s.EntitledTeam = team
	s.Decider = -1
	// Unplayed cards leave the hands so the deck still accounts for 52.
	for i := range s.Players {
		s.Played = append(s.Played, s.Players[i].Hand...)
		s.Players[i].Hand = []CardID{}
	}
	return []Event{{Kind: "round_won", Audience: Public, Data: map[string]any{
		"team": team, "round": s.Round, "tricks": s.Tricks,
		"rounds_won": s.RoundsWon, "trump": s.Trump,
	}}}
}

// readyRound marks a seat ready; the fourth starts the next round (spec rule 31).
func (s *State) readyRound(seat int) ([]Event, error) {
	if s.Phase != PhaseRoundOver {
		return nil, reject(CodeWrongPhase, "the round is still being played")
	}
	if s.Ready[seat] {
		return nil, reject(CodeIllegal, "you are already ready")
	}
	s.Ready[seat] = true
	events := []Event{{Kind: "ready_for_round", Audience: Public, Data: map[string]any{
		"seat": seat, "ready": slices.Clone(s.Ready),
	}}}
	if slices.Contains(s.Ready, false) {
		return events, nil
	}
	return append(events, s.startRound()...), nil
}

func (s *State) clone() *State {
	out := *s
	out.Seed = slices.Clone(s.Seed)
	out.Players = make([]Player, len(s.Players))
	for i, p := range s.Players {
		out.Players[i] = Player{UserID: p.UserID, Hand: slices.Clone(p.Hand)}
	}
	out.Rest = slices.Clone(s.Rest)
	out.Trick = slices.Clone(s.Trick)
	if s.LastTrick != nil {
		last := *s.LastTrick
		last.Cards = slices.Clone(s.LastTrick.Cards)
		out.LastTrick = &last
	}
	out.Played = slices.Clone(s.Played)
	out.History = slices.Clone(s.History)
	out.Ready = slices.Clone(s.Ready)
	return &out
}
