package monopoly

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Action is a typed player intent. Clients submit intents only; the engine
// computes every result.
type Action interface{ Kind() string }

// Bank moves a money, action or Rent card from hand to the bank (B1 2A).
type Bank struct {
	Card CardID `json:"card"`
}

// PlayProperty places a property card from hand (B1 2B). Set is an existing
// own set ID or empty for a new set. Color is required for a new set, and
// optional for a multicolor wild, which may be placed unassigned (spec D).
type PlayProperty struct {
	Card  CardID `json:"card"`
	Set   string `json:"set,omitempty"`
	Color Color  `json:"color,omitempty"`
}

// PlayPassGo draws two cards (B1 Pass Go).
type PlayPassGo struct {
	Card CardID `json:"card"`
}

// PlayBuilding attaches a House or Hotel from hand to an own complete set.
type PlayBuilding struct {
	Card CardID `json:"card"`
	Set  string `json:"set"`
}

// PlaySlyDeal takes one property outside a complete set, or a detached building.
type PlaySlyDeal struct {
	Card   CardID `json:"card"`
	Target int    `json:"target"`
	Take   CardID `json:"take"`
}

// PlayForcedDeal swaps an own property for an opponent's (decisions Q1a, Q1a-F2).
type PlayForcedDeal struct {
	Card   CardID `json:"card"`
	Target int    `json:"target"`
	Take   CardID `json:"take"`
	Offer  CardID `json:"offer"`
}

// PlayDealBreaker takes one complete set with its buildings.
type PlayDealBreaker struct {
	Card   CardID `json:"card"`
	Target int    `json:"target"`
	Set    string `json:"set"`
}

// PlayDebtCollector charges one opponent 5M.
type PlayDebtCollector struct {
	Card   CardID `json:"card"`
	Target int    `json:"target"`
}

// PlayBirthday charges every other player 2M.
type PlayBirthday struct {
	Card CardID `json:"card"`
}

// PlayRent charges rent for one own set (decision Q3.2). Target is required
// for multicolor Rent only. Doublers must accompany two-color Rent (Q2).
type PlayRent struct {
	Card     CardID   `json:"card"`
	Set      string   `json:"set"`
	Doublers []CardID `json:"doublers,omitempty"`
	Target   *int     `json:"target,omitempty"`
}

// Accept closes the responder's current decision: in a chain it lets the
// last Just Say No stand; otherwise it accepts the remaining effect.
type Accept struct {
	Pending int `json:"pending"`
	Step    int `json:"step"`
}

// PlayJustSayNo responds from hand, free of the play budget. Component names
// the part a defender targets when starting a chain ("charge" or a doubler).
type PlayJustSayNo struct {
	Pending   int    `json:"pending"`
	Step      int    `json:"step"`
	Card      CardID `json:"card"`
	Component string `json:"component,omitempty"`
}

// Pay settles the payer's debt with chosen tabled cards.
type Pay struct {
	Pending int      `json:"pending"`
	Step    int      `json:"step"`
	Cards   []CardID `json:"cards"`
}

// PlaceReceived places one received property card.
type PlaceReceived struct {
	Card  CardID `json:"card"`
	Set   string `json:"set,omitempty"`
	Color Color  `json:"color,omitempty"`
}

// SetLayout describes one set in a Rearrange. ID keeps an existing set's
// identity; empty creates a new set.
type SetLayout struct {
	ID    string   `json:"id,omitempty"`
	Color Color    `json:"color"`
	Cards []CardID `json:"cards"`
	House CardID   `json:"house,omitempty"`
	Hotel CardID   `json:"hotel,omitempty"`
}

// Rearrange replaces the active player's whole property area atomically
// (B1 2B, Property Wildcards; decision Q3.5). It costs no play.
type Rearrange struct {
	Sets       []SetLayout `json:"sets"`
	Unassigned []CardID    `json:"unassigned,omitempty"`
	Detached   []CardID    `json:"detached,omitempty"`
}

// EndTurn ends the turn, returning exactly the excess over seven to the bottom
// of the draw pile in the listed order (B1 End Your Turn; spec D).
type EndTurn struct {
	Return []CardID `json:"return,omitempty"`
}

func (Bank) Kind() string              { return "bank" }
func (PlayProperty) Kind() string      { return "play_property" }
func (PlayPassGo) Kind() string        { return "pass_go" }
func (PlayBuilding) Kind() string      { return "play_building" }
func (PlaySlyDeal) Kind() string       { return "sly_deal" }
func (PlayForcedDeal) Kind() string    { return "forced_deal" }
func (PlayDealBreaker) Kind() string   { return "deal_breaker" }
func (PlayDebtCollector) Kind() string { return "debt_collector" }
func (PlayBirthday) Kind() string      { return "birthday" }
func (PlayRent) Kind() string          { return "rent" }
func (Accept) Kind() string            { return "accept" }
func (PlayJustSayNo) Kind() string     { return "just_say_no" }
func (Pay) Kind() string               { return "pay" }
func (PlaceReceived) Kind() string     { return "place_received" }
func (Rearrange) Kind() string         { return "rearrange" }
func (EndTurn) Kind() string           { return "end_turn" }

// DecodeAction builds a typed action from a wire kind and JSON payload.
// Unknown fields are rejected.
func DecodeAction(kind string, payload json.RawMessage) (Action, error) {
	var a Action
	switch kind {
	case "bank":
		a = &Bank{}
	case "play_property":
		a = &PlayProperty{}
	case "pass_go":
		a = &PlayPassGo{}
	case "play_building":
		a = &PlayBuilding{}
	case "sly_deal":
		a = &PlaySlyDeal{}
	case "forced_deal":
		a = &PlayForcedDeal{}
	case "deal_breaker":
		a = &PlayDealBreaker{}
	case "debt_collector":
		a = &PlayDebtCollector{}
	case "birthday":
		a = &PlayBirthday{}
	case "rent":
		a = &PlayRent{}
	case "accept":
		a = &Accept{}
	case "just_say_no":
		a = &PlayJustSayNo{}
	case "pay":
		a = &Pay{}
	case "place_received":
		a = &PlaceReceived{}
	case "rearrange":
		a = &Rearrange{}
	case "end_turn":
		a = &EndTurn{}
	default:
		return nil, reject(CodeInvalidAction, "unknown action %q", kind)
	}
	if len(payload) > 0 {
		dec := json.NewDecoder(bytes.NewReader(payload))
		dec.DisallowUnknownFields()
		if err := dec.Decode(a); err != nil {
			return nil, reject(CodeInvalidAction, "invalid %s payload", kind)
		}
	}
	// Return the value type so handlers switch on concrete values.
	switch v := a.(type) {
	case *Bank:
		return *v, nil
	case *PlayProperty:
		return *v, nil
	case *PlayPassGo:
		return *v, nil
	case *PlayBuilding:
		return *v, nil
	case *PlaySlyDeal:
		return *v, nil
	case *PlayForcedDeal:
		return *v, nil
	case *PlayDealBreaker:
		return *v, nil
	case *PlayDebtCollector:
		return *v, nil
	case *PlayBirthday:
		return *v, nil
	case *PlayRent:
		return *v, nil
	case *Accept:
		return *v, nil
	case *PlayJustSayNo:
		return *v, nil
	case *Pay:
		return *v, nil
	case *PlaceReceived:
		return *v, nil
	case *Rearrange:
		return *v, nil
	case *EndTurn:
		return *v, nil
	}
	return nil, reject(CodeInvalidAction, "unknown action %q", kind)
}

// Rejection codes. A rejected action never changes state.
const (
	CodeInvalidAction = "INVALID_ACTION"
	CodeNotYourTurn   = "NOT_YOUR_TURN"
	CodeWrongPhase    = "WRONG_PHASE"
	CodeNoPlays       = "NO_PLAYS_LEFT"
	CodeInvalidCard   = "INVALID_CARD"
	CodeInvalidTarget = "INVALID_TARGET"
	CodeStale         = "STALE_RESPONSE"
	CodeIllegal       = "ILLEGAL_MOVE"
	CodeGameOver      = "GAME_OVER"
)

// RuleError is a rejected action.
type RuleError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *RuleError) Error() string { return e.Code + ": " + e.Message }

func reject(code, format string, args ...any) *RuleError {
	return &RuleError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Public is the audience of events every seat may see.
const Public = -1

// Event records what happened. Audience is Public or one seat, so private
// details (drawn cards) reach only their owner.
type Event struct {
	Kind     string         `json:"kind"`
	Audience int            `json:"audience"`
	Data     map[string]any `json:"data,omitempty"`
}
