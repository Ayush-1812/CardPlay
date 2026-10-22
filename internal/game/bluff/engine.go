package bluff

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"
)

// ErrPlayers is returned when a table cannot be seated.
var ErrPlayers = errors.New("bluff needs three to six distinct players")

// ErrDecks is returned when a room asks for a deck count out of range.
var ErrDecks = errors.New("bluff is played with one to four decks")

// Rejection codes. Each is a rule-level refusal: the state is unchanged and
// the player is told why.
const (
	CodeNotYourTurn  = "NOT_YOUR_TURN"
	CodeIllegal      = "ILLEGAL_MOVE"
	CodeWrongPhase   = "WRONG_PHASE"
	CodeStalePlay    = "STALE_PLAY"
	CodeNotEligible  = "NOT_ELIGIBLE"
	CodeInvalidInput = "INVALID_ACTION"
)

// RuleError is a refusal a player can act on.
type RuleError struct {
	Code    string
	Message string
}

func (e *RuleError) Error() string         { return e.Message }
func (e *RuleError) RejectionCode() string { return e.Code }

func reject(code, format string, args ...any) *RuleError {
	return &RuleError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// InvariantError means the engine produced a state that breaks its own
// rules. It is a bug, not a player mistake.
type InvariantError struct{ Detail string }

func (e *InvariantError) Error() string { return "bluff invariant violated: " + e.Detail }

// Audience addresses an event. Public reaches the table; a seat number
// reaches one player.
const Public = -1

// Event is something the table is told.
type Event struct {
	Kind     string
	Audience int
	Data     map[string]any
}

// source is where a deal draws its randomness. Production reads the
// operating system; a test may swap it for a reader it controls.
var source io.Reader = rand.Reader

// NewGame seats the players, deals the whole deck and chooses who starts.
// decks is how many 52-card decks to deal; one is the ordinary game.
//
// Dealing and the first player are drawn separately, so the seat that gets
// the extra card is not also always the seat that leads.
func NewGame(userIDs []string, decks int, random io.Reader) (*State, []Event, error) {
	if len(userIDs) < MinPlayers || len(userIDs) > MaxPlayers {
		return nil, nil, ErrPlayers
	}
	if decks < 1 || decks > MaxDecks {
		return nil, nil, ErrDecks
	}
	seen := map[string]bool{}
	for _, id := range userIDs {
		if id == "" || seen[id] {
			return nil, nil, ErrPlayers
		}
		seen[id] = true
	}
	if random == nil {
		random = source
	}
	s := &State{
		Schema:   SchemaVersion,
		Decks:    decks,
		Phase:    PhasePlay,
		Rank:     Ace,
		Pile:     []CardID{},
		Accepted: make([]bool, len(userIDs)),
		Pending:  -1,
		Winner:   -1,
	}
	for _, id := range userIDs {
		s.Players = append(s.Players, Player{UserID: id, Hand: []CardID{}})
	}

	deck := BuildDeck(decks)
	if err := shuffle(deck, random); err != nil {
		return nil, nil, err
	}
	// Deal one at a time, clockwise, from a seat chosen at random: with
	// three, five or six players the deck does not divide evenly, and the
	// spare cards should not always land on the same seats.
	first, err := index(len(s.Players), random)
	if err != nil {
		return nil, nil, err
	}
	for i, id := range deck {
		seat := (first + i) % len(s.Players)
		s.Players[seat].Hand = append(s.Players[seat].Hand, id)
	}
	// Who leads is drawn separately from who was dealt to first.
	lead, err := index(len(s.Players), random)
	if err != nil {
		return nil, nil, err
	}
	s.Turn = lead

	events := []Event{{Kind: "game_started", Audience: Public, Data: map[string]any{
		"players": len(s.Players), "first_seat": lead, "rank": string(s.Rank),
		"decks": decks,
	}}}
	for seat, p := range s.Players {
		events = append(events, Event{Kind: "dealt", Audience: seat, Data: map[string]any{
			"cards": slices.Clone(p.Hand),
		}})
	}
	return s, events, nil
}

// shuffle permutes cards in place with a Fisher-Yates shuffle, drawing each
// index from r. A fresh deck is shuffled for every game and nothing is
// derived from a stored seed, so no one who can read a match can work out
// the deal.
func shuffle(cards []CardID, r io.Reader) error {
	for i := len(cards) - 1; i > 0; i-- {
		j, err := index(i+1, r)
		if err != nil {
			return err
		}
		cards[i], cards[j] = cards[j], cards[i]
	}
	return nil
}

// index returns a uniform value in [0,n) read from r. Draws outside the
// range are discarded rather than folded back in, which would make the low
// indices likelier.
func index(n int, r io.Reader) (int, error) {
	if n <= 1 {
		return 0, nil
	}
	mask := uint32(1)
	for mask < uint32(n) {
		mask <<= 1
	}
	mask--
	var b [4]byte
	for {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, err
		}
		if v := binary.BigEndian.Uint32(b[:]) & mask; v < uint32(n) {
			return int(v), nil
		}
	}
}

// Action is one player intent.
type Action interface{ isAction() }

// PlayCards puts one to four cards down as the required rank. The cards may
// be anything the player holds: that is the game.
type PlayCards struct {
	Cards []CardID `json:"cards"`
}

// AcceptPlay gives up the right to challenge the named play. It binds.
type AcceptPlay struct {
	PlayID string `json:"play_id"`
}

// CallBluff challenges the named play.
type CallBluff struct {
	PlayID string `json:"play_id"`
}

// CloseWindow ends a challenge window whose deadline has passed, which is
// what silence means. The platform sends it when it sweeps the deadline; it
// is refused while the window is still open, so nobody can cut it short.
type CloseWindow struct {
	PlayID string `json:"play_id"`
}

func (PlayCards) isAction()   {}
func (AcceptPlay) isAction()  {}
func (CallBluff) isAction()   {}
func (CloseWindow) isAction() {}

// Apply validates and applies one action by seat at the time the platform
// stamped on the command. It works on a clone and returns the new state
// only on success, so an illegal action leaves the original untouched.
func Apply(s *State, seat int, a Action, now time.Time) (*State, []Event, error) {
	if seat < 0 || seat >= len(s.Players) {
		return s, nil, reject(CodeNotYourTurn, "unknown seat %d", seat)
	}
	next := s.clone()
	events, err := next.apply(seat, a, now)
	if err != nil {
		return s, nil, err
	}
	if err := next.CheckInvariants(); err != nil {
		return s, nil, &InvariantError{Detail: err.Error()}
	}
	return next, events, nil
}

func (s *State) apply(seat int, a Action, now time.Time) ([]Event, error) {
	if s.Phase == PhaseOver {
		return nil, reject(CodeWrongPhase, "this game has finished")
	}
	switch action := a.(type) {
	case PlayCards:
		return s.play(seat, action, now)
	case AcceptPlay:
		return s.accept(seat, action.PlayID, now)
	case CallBluff:
		return s.challenge(seat, action.PlayID, now)
	case CloseWindow:
		return s.expire(action.PlayID, now)
	}
	return nil, reject(CodeInvalidInput, "unknown action")
}

// play puts cards down and opens the challenge window.
func (s *State) play(seat int, a PlayCards, now time.Time) ([]Event, error) {
	if s.Phase != PhasePlay {
		return nil, reject(CodeWrongPhase, "the table is waiting on the last play")
	}
	if seat != s.Turn {
		return nil, reject(CodeNotYourTurn, "it is not your turn")
	}
	if len(a.Cards) < 1 || len(a.Cards) > s.maxPlay() {
		return nil, reject(CodeIllegal, "play between one and %d cards", s.maxPlay())
	}
	// Every card must be one this player holds, and named once.
	hand := s.Players[seat].Hand
	seen := map[CardID]bool{}
	for _, id := range a.Cards {
		if _, ok := Lookup(id); !ok {
			return nil, reject(CodeIllegal, "%s is not a card", id)
		}
		if seen[id] {
			return nil, reject(CodeIllegal, "%s was named twice", id)
		}
		seen[id] = true
		if !slices.Contains(hand, id) {
			return nil, reject(CodeIllegal, "you do not hold %s", id)
		}
	}
	// The cards leave the hand and go face down on the pile. Whether they
	// match the declared rank is not checked: a wrong card is the game.
	s.Players[seat].Hand = slices.DeleteFunc(slices.Clone(hand), func(id CardID) bool { return seen[id] })
	cards := slices.Clone(a.Cards)
	s.Pile = append(s.Pile, cards...)
	s.Plays++
	s.Last = &Play{
		ID:    fmt.Sprintf("p%d", s.Plays),
		Seat:  seat,
		Rank:  s.Rank,
		Count: len(cards),
		Cards: cards,
	}
	// A new play replaces whatever the table was looking at.
	s.Result = nil
	// The player cannot challenge their own play; everyone else may.
	s.Accepted = make([]bool, len(s.Players))
	s.Accepted[seat] = true
	s.Phase = PhaseChallenge
	s.Deadline = now.Add(ChallengeWindow)
	// Playing the last card is not a win yet: the claim must survive.
	if len(s.Players[seat].Hand) == 0 {
		s.Pending = seat
	}

	return []Event{{Kind: "played", Audience: Public, Data: map[string]any{
		"play_id": s.Last.ID, "seat": seat, "rank": string(s.Last.Rank),
		"count": s.Last.Count, "deadline": s.Deadline, "pending_win": s.Pending == seat,
	}}}, nil
}

// accept gives up one player's right to challenge. When the last eligible
// player accepts, the window closes early and play moves on.
func (s *State) accept(seat int, playID string, now time.Time) ([]Event, error) {
	if s.Phase != PhaseChallenge || s.Last == nil {
		return nil, reject(CodeWrongPhase, "there is no play to answer")
	}
	if playID != "" && playID != s.Last.ID {
		return nil, reject(CodeStalePlay, "that play has already been resolved")
	}
	if seat == s.Last.Seat {
		return nil, reject(CodeNotEligible, "you made this play")
	}
	if s.Accepted[seat] {
		return nil, reject(CodeNotEligible, "you have already accepted this play")
	}
	if now.After(s.Deadline) {
		return nil, reject(CodeStalePlay, "the challenge window has closed")
	}
	s.Accepted[seat] = true
	events := []Event{{Kind: "accepted", Audience: Public, Data: map[string]any{
		"play_id": s.Last.ID, "seat": seat,
	}}}
	if len(s.pendingSeats()) > 0 {
		return events, nil
	}
	// Everyone has accepted: the window is over before the deadline.
	return append(events, s.closeWindow(now)...), nil
}

// expire closes a challenge window whose deadline has passed. Silence is
// acceptance, so this ends exactly as everyone accepting would.
func (s *State) expire(playID string, now time.Time) ([]Event, error) {
	if s.Phase != PhaseChallenge || s.Last == nil {
		return nil, reject(CodeWrongPhase, "there is no play to answer")
	}
	if playID != "" && playID != s.Last.ID {
		return nil, reject(CodeStalePlay, "that play has already been resolved")
	}
	if now.Before(s.Deadline) {
		return nil, reject(CodeWrongPhase, "the challenge window is still open")
	}
	return s.closeWindow(now), nil
}

// Expire is the same thing for a caller that holds the state directly.
func (s *State) Expire(now time.Time) ([]Event, error) { return s.expire("", now) }

// closeWindow ends an unchallenged play: the cards stay face down, nobody
// learns whether the claim was true, and the next seat plays the next rank.
// A player who put their last card down wins here.
func (s *State) closeWindow(_ time.Time) []Event {
	last := s.Last
	events := []Event{{Kind: "unchallenged", Audience: Public, Data: map[string]any{
		"play_id": last.ID, "seat": last.Seat,
	}}}
	if s.Pending == last.Seat {
		return append(events, s.finish(last.Seat)...)
	}
	s.advance(last.Seat)
	return events
}

// challenge resolves a call. Only the latest batch is revealed, whatever
// else the pile holds.
func (s *State) challenge(seat int, playID string, now time.Time) ([]Event, error) {
	if s.Phase != PhaseChallenge || s.Last == nil {
		return nil, reject(CodeWrongPhase, "there is no play to challenge")
	}
	// Late is late, by the server's clock: a challenge that arrives after
	// the deadline has missed its window, whatever the client thought.
	if now.After(s.Deadline) {
		return nil, reject(CodeStalePlay, "the challenge window has closed")
	}
	if playID != "" && playID != s.Last.ID {
		return nil, reject(CodeStalePlay, "that play has already been resolved")
	}
	if seat == s.Last.Seat {
		return nil, reject(CodeNotEligible, "you cannot challenge your own play")
	}
	if s.Accepted[seat] {
		return nil, reject(CodeNotEligible, "you accepted this play")
	}
	last := s.Last
	truthful := last.Truthful()
	// Truthful: the challenger was wrong and takes everything. A bluff: the
	// player takes everything. Either way it is the whole pile, which is
	// every uncollected play, not only this batch.
	collector := last.Seat
	if truthful {
		collector = seat
	}
	collected := len(s.Pile)
	s.Players[collector].Hand = append(s.Players[collector].Hand, slices.Clone(s.Pile)...)
	s.Pile = []CardID{}
	// Only this batch is turned face up. The rest of the pile goes into a
	// hand still face down, and nobody learns what it was.
	s.Result = &Reveal{
		PlayID:     last.ID,
		Claimed:    last.Rank,
		Cards:      slices.Clone(last.Cards),
		Player:     last.Seat,
		Challenger: seat,
		Truthful:   truthful,
		Collector:  collector,
		Collected:  collected,
	}
	events := []Event{{Kind: "challenged", Audience: Public, Data: map[string]any{
		"play_id": last.ID, "seat": seat, "player": last.Seat,
		"truthful": truthful, "collector": collector, "collected": collected,
		"cards": slices.Clone(last.Cards), "claimed": string(last.Rank),
	}}}
	// A player whose last cards were truthful wins even though they were
	// challenged: the challenger collects and the game ends.
	if truthful && s.Pending == last.Seat {
		return append(events, s.finish(last.Seat)...), nil
	}
	// Otherwise the player who was challenged is back in the game with a
	// handful of cards, and play continues after them.
	if collector == last.Seat {
		s.Pending = -1
	}
	s.advance(last.Seat)
	return events, nil
}

// advance moves to the seat clockwise after the player whose play just
// ended, and steps the required rank on. Both happen however the play
// ended: unchallenged, challenged truthfully, or caught out.
func (s *State) advance(after int) {
	s.Turn = s.next(after)
	s.Rank = NextRank(s.Rank)
	s.Phase = PhasePlay
	s.Last = nil
	s.Accepted = make([]bool, len(s.Players))
	s.Deadline = time.Time{}
}

// finish ends the game with a winner.
func (s *State) finish(seat int) []Event {
	s.Winner = seat
	s.Pending = -1
	s.Phase = PhaseOver
	s.Last = nil
	s.Accepted = make([]bool, len(s.Players))
	s.Deadline = time.Time{}
	return []Event{{Kind: "won", Audience: Public, Data: map[string]any{"seat": seat}}}
}
