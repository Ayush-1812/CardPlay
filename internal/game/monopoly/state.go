package monopoly

import (
	"slices"
	"strconv"
)

// Phase is the explicit game phase. Every action is legal in exactly the
// phases its handler names.
type Phase string

const (
	PhasePlay      Phase = "play"      // active player may play, rearrange or end the turn
	PhaseResponse  Phase = "response"  // a target or the source must accept or play Just Say No
	PhasePayment   Phase = "payment"   // a target must choose payment cards
	PhasePlacement Phase = "placement" // received properties must be placed
	PhaseFinished  Phase = "finished"
)

// MaxPlays is the hand-card play budget per turn (B1 On Your Turn 2).
const MaxPlays = 3

// HandLimit is the end-of-turn hand size (B1 End Your Turn).
const HandLimit = 7

// PropertySet is one visible set in a property area. A set holds cards of one
// color; buildings are attached only while the set is complete.
type PropertySet struct {
	ID    string   `json:"id"`
	Color Color    `json:"color"`
	Cards []CardID `json:"cards"`
	House CardID   `json:"house,omitempty"`
	Hotel CardID   `json:"hotel,omitempty"`
}

// Player holds one seat's zones. Hand is private; everything else is public.
type Player struct {
	UserID string        `json:"user_id"`
	Hand   []CardID      `json:"hand"`
	Bank   []CardID      `json:"bank"`
	Sets   []PropertySet `json:"sets"`
	// Unassigned holds multicolor wilds placed without a color (spec D): no rent
	// or completion credit until assigned.
	Unassigned []CardID `json:"unassigned"`
	// Detached holds tabled buildings not attached to any set (decision Q3).
	Detached []CardID `json:"detached"`
	// Incoming holds received property cards awaiting the recipient's placement.
	Incoming []CardID `json:"incoming"`
}

// Component states within one target's response (decisions Q2 and Q2-F1).
type ComponentState string

const (
	ComponentActive  ComponentState = "active"  // may still be targeted by a new Just Say No chain
	ComponentBlocked ComponentState = "blocked" // a chain ended with this part cancelled
	ComponentSettled ComponentState = "settled" // a chain ended with this part standing
)

// Component is a part of an action a defender may target: "charge" (the whole
// effect against them) or, for doubled Rent, one Double the Rent card ID.
type Component struct {
	Key   string         `json:"key"`
	State ComponentState `json:"state"`
}

// ComponentCharge is the whole effect against one defender.
const ComponentCharge = "charge"

// TargetStage is the state of one defender's obligation.
type TargetStage string

const (
	StageWaiting TargetStage = "waiting" // not yet reached (group charges go clockwise)
	StageRespond TargetStage = "respond" // defender accepts or starts a Just Say No chain
	StageChain   TargetStage = "chain"   // Chain.Waiting accepts or counters
	StagePay     TargetStage = "pay"     // defender chooses payment
	StageDone    TargetStage = "done"
)

// Chain is an open Just Say No exchange over one component. Only the source
// and this defender may take part (decision Q1b).
type Chain struct {
	Component string `json:"component"`
	Blocked   bool   `json:"blocked"` // parity: true after the defender's JSN
	Waiting   int    `json:"waiting"` // seat that may now accept or counter
}

// Target is one defender of a pending action.
type Target struct {
	Seat       int         `json:"seat"`
	Stage      TargetStage `json:"stage"`
	Components []Component `json:"components"`
	Chain      *Chain      `json:"chain,omitempty"`
	Owed       int         `json:"owed,omitempty"`    // frozen debt once payment starts
	Outcome    string      `json:"outcome,omitempty"` // blocked, paid, transferred
}

// Pending is an action awaiting responses, payments or placements. Its cards
// sit in the resolving area until the whole effect finishes.
type Pending struct {
	ID       int        `json:"id"`
	Step     int        `json:"step"` // bumps on every response; stale submissions are rejected
	Action   ActionType `json:"action"`
	Card     CardID     `json:"card"`
	Source   int        `json:"source"`
	Doublers []CardID   `json:"doublers,omitempty"`
	Spent    []CardID   `json:"spent,omitempty"` // Just Say No cards played during this action
	Base     int        `json:"base,omitempty"`  // frozen rent or fixed debt before doubling
	SetID    string     `json:"set_id,omitempty"`
	Color    Color      `json:"color,omitempty"`
	Take     CardID     `json:"take,omitempty"`
	Offer    CardID     `json:"offer,omitempty"`
	Targets  []Target   `json:"targets"`
	Current  int        `json:"current"`
}

// ActionRent marks a pending Rent (either Rent card kind).
const ActionRent ActionType = "rent"

// State is the complete authoritative game state. It contains private data
// (hands, draw order, seed) and must never be sent to clients; use View.
type State struct {
	Schema        int      `json:"schema"`
	Seed          []byte   `json:"seed"`
	Shuffles      int      `json:"shuffles"`
	Players       []Player `json:"players"`
	Active        int      `json:"active"`
	Turn          int      `json:"turn"`
	PlaysUsed     int      `json:"plays_used"`
	Phase         Phase    `json:"phase"`
	Draw          []CardID `json:"draw"`    // index 0 is the top
	Discard       []CardID `json:"discard"` // resolved center pile, oldest first
	Pending       *Pending `json:"pending,omitempty"`
	NextSetID     int      `json:"next_set_id"`
	NextPendingID int      `json:"next_pending_id"`
	Winner        *int     `json:"winner,omitempty"`
}

// SchemaVersion is the persisted state schema.
const SchemaVersion = 1

// Clone returns a deep copy, so a failed action can never leak a mutation.
func (s *State) Clone() *State {
	c := *s
	c.Seed = slices.Clone(s.Seed)
	c.Draw = slices.Clone(s.Draw)
	c.Discard = slices.Clone(s.Discard)
	c.Players = make([]Player, len(s.Players))
	for i, p := range s.Players {
		q := p
		q.Hand = slices.Clone(p.Hand)
		q.Bank = slices.Clone(p.Bank)
		q.Unassigned = slices.Clone(p.Unassigned)
		q.Detached = slices.Clone(p.Detached)
		q.Incoming = slices.Clone(p.Incoming)
		q.Sets = make([]PropertySet, len(p.Sets))
		for j, set := range p.Sets {
			set.Cards = slices.Clone(set.Cards)
			q.Sets[j] = set
		}
		c.Players[i] = q
	}
	if s.Pending != nil {
		p := *s.Pending
		p.Doublers = slices.Clone(p.Doublers)
		p.Spent = slices.Clone(p.Spent)
		p.Targets = make([]Target, len(s.Pending.Targets))
		for i, t := range s.Pending.Targets {
			t.Components = slices.Clone(t.Components)
			if t.Chain != nil {
				ch := *t.Chain
				t.Chain = &ch
			}
			p.Targets[i] = t
		}
		c.Pending = &p
	}
	if s.Winner != nil {
		w := *s.Winner
		c.Winner = &w
	}
	return &c
}

func (s *State) newSetID() string {
	s.NextSetID++
	return "set-" + strconv.Itoa(s.NextSetID)
}

// Complete reports whether a set is full. A set needs its printed size and at
// least one card that is not a multicolor wild (decision Q3.1).
func (set PropertySet) Complete() bool {
	info, ok := colorInfo[set.Color]
	if !ok || len(set.Cards) != info.Size {
		return false
	}
	return hasAnchor(set)
}

func hasAnchor(set PropertySet) bool {
	for _, id := range set.Cards {
		if mustCard(id).Kind != KindRainbowWild {
			return true
		}
	}
	return false
}

// Rent is the set's current rent: the printed ladder for its card count plus
// attached House (+3M) and Hotel (+4M). A set of only multicolor wilds earns
// nothing (G2/G4; decision Q3.1).
func (set PropertySet) Rent() int {
	info, ok := colorInfo[set.Color]
	if !ok || len(set.Cards) == 0 || !hasAnchor(set) {
		return 0
	}
	n := min(len(set.Cards), info.Size)
	rent := info.Rent[n-1]
	if set.Complete() {
		if set.House != "" {
			rent += 3
		}
		if set.Hotel != "" {
			rent += 4
		}
	}
	return rent
}

// CompleteColors counts distinct colors among a player's complete sets.
func (p Player) CompleteColors() int {
	seen := map[Color]bool{}
	for _, set := range p.Sets {
		if set.Complete() {
			seen[set.Color] = true
		}
	}
	return len(seen)
}

func (p *Player) setByID(id string) (int, bool) {
	for i, set := range p.Sets {
		if set.ID == id {
			return i, true
		}
	}
	return -1, false
}

func removeCard(list []CardID, id CardID) ([]CardID, bool) {
	i := slices.Index(list, id)
	if i < 0 {
		return list, false
	}
	return slices.Delete(list, i, i+1), true
}

// location describes where a player's tabled card sits.
type location struct {
	zone string // bank, set, building, unassigned, detached
	set  int    // index into Sets for set/building
}

func (p *Player) locate(id CardID) (location, bool) {
	if slices.Contains(p.Bank, id) {
		return location{zone: "bank"}, true
	}
	if slices.Contains(p.Unassigned, id) {
		return location{zone: "unassigned"}, true
	}
	if slices.Contains(p.Detached, id) {
		return location{zone: "detached"}, true
	}
	for i, set := range p.Sets {
		if slices.Contains(set.Cards, id) {
			return location{zone: "set", set: i}, true
		}
		if set.House == id || set.Hotel == id {
			return location{zone: "building", set: i}, true
		}
	}
	return location{}, false
}

// takeTabled removes one tabled card from the player without normalizing sets.
func (p *Player) takeTabled(id CardID) bool {
	loc, ok := p.locate(id)
	if !ok {
		return false
	}
	switch loc.zone {
	case "bank":
		p.Bank, _ = removeCard(p.Bank, id)
	case "unassigned":
		p.Unassigned, _ = removeCard(p.Unassigned, id)
	case "detached":
		p.Detached, _ = removeCard(p.Detached, id)
	case "set":
		p.Sets[loc.set].Cards, _ = removeCard(p.Sets[loc.set].Cards, id)
	case "building":
		set := &p.Sets[loc.set]
		if set.House == id {
			set.House = ""
		} else {
			set.Hotel = ""
		}
	}
	return true
}

// normalize enforces decisions Q3.3–Q3.4 after cards leave sets: empty sets
// disappear; an incomplete set or a Hotel without a House detaches buildings.
func (p *Player) normalize() {
	kept := p.Sets[:0]
	for _, set := range p.Sets {
		if len(set.Cards) == 0 {
			if set.House != "" {
				p.Detached = append(p.Detached, set.House)
			}
			if set.Hotel != "" {
				p.Detached = append(p.Detached, set.Hotel)
			}
			continue
		}
		if !set.Complete() {
			if set.House != "" {
				p.Detached = append(p.Detached, set.House)
				set.House = ""
			}
			if set.Hotel != "" {
				p.Detached = append(p.Detached, set.Hotel)
				set.Hotel = ""
			}
		}
		if set.Hotel != "" && set.House == "" {
			p.Detached = append(p.Detached, set.Hotel)
			set.Hotel = ""
		}
		kept = append(kept, set)
	}
	p.Sets = kept
}
