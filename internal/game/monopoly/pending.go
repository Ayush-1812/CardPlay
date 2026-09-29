package monopoly

import (
	"slices"
)

// declare validates the play budget and starts a pending action against
// targets in clockwise order from the source (spec response protocol 1–2).
// Assets do not move until each target's response closes.
func (s *State) declare(seat int, card Card, extra []CardID, p Pending, targets []int) []Event {
	s.fromHand(seat, card.ID)
	for _, id := range extra {
		s.fromHand(seat, id)
	}
	s.PlaysUsed += 1 + len(extra)
	s.NextPendingID++
	p.ID = s.NextPendingID
	p.Card = card.ID
	p.Source = seat
	p.Doublers = slices.Clone(extra)
	components := []Component{{Key: ComponentCharge, State: ComponentActive}}
	for _, d := range extra {
		components = append(components, Component{Key: string(d), State: ComponentActive})
	}
	for i, t := range targets {
		stage := StageWaiting
		if i == 0 {
			stage = StageRespond
		}
		p.Targets = append(p.Targets, Target{Seat: t, Stage: stage, Components: slices.Clone(components)})
	}
	s.Pending = &p
	s.Phase = PhaseResponse
	return []Event{{Kind: "action_declared", Audience: Public, Data: map[string]any{
		"pending": p.ID, "seat": seat, "action": p.Action, "card": card.ID, "doublers": p.Doublers,
		"targets": targets, "base": p.Base, "set": p.SetID, "color": p.Color, "take": p.Take, "offer": p.Offer,
	}}}
}

// clockwise returns every other seat, starting after source.
func (s *State) clockwise(source int) []int {
	var seats []int
	for i := 1; i < len(s.Players); i++ {
		seats = append(seats, (source+i)%len(s.Players))
	}
	return seats
}

func (s *State) actionCard(seat int, id CardID, want ActionType) (Card, error) {
	c, err := s.handCard(seat, id)
	if err != nil {
		return c, err
	}
	if c.Kind != KindAction || c.Action != want {
		return c, reject(CodeInvalidCard, "%s is not %s", id, want)
	}
	return c, nil
}

// stealable reports whether a single-card action may take id from a player:
// a property outside a complete set, an unassigned multicolor wild, or a
// detached building (B1 Sly Deal/Forced Deal; W Action FAQ Q3).
func stealable(p *Player, id CardID, allowBuilding bool) bool {
	loc, ok := p.locate(id)
	if !ok {
		return false
	}
	switch loc.zone {
	case "set":
		return !p.Sets[loc.set].Complete()
	case "unassigned":
		return true
	case "detached":
		return allowBuilding
	}
	return false
}

func (s *State) slyDeal(seat int, a PlaySlyDeal) ([]Event, error) {
	if err := s.requirePlay(seat, 1); err != nil {
		return nil, err
	}
	c, err := s.actionCard(seat, a.Card, SlyDeal)
	if err != nil {
		return nil, err
	}
	if err := s.opponent(seat, a.Target); err != nil {
		return nil, err
	}
	if !stealable(&s.Players[a.Target], a.Take, true) {
		return nil, reject(CodeInvalidTarget, "%s cannot be taken: it must be a property outside a complete set or a detached building", a.Take)
	}
	return s.declare(seat, c, nil, Pending{Action: SlyDeal, Take: a.Take}, []int{a.Target}), nil
}

func (s *State) forcedDeal(seat int, a PlayForcedDeal) ([]Event, error) {
	if err := s.requirePlay(seat, 1); err != nil {
		return nil, err
	}
	c, err := s.actionCard(seat, a.Card, ForcedDeal)
	if err != nil {
		return nil, err
	}
	if err := s.opponent(seat, a.Target); err != nil {
		return nil, err
	}
	if !stealable(&s.Players[a.Target], a.Take, true) {
		return nil, reject(CodeInvalidTarget, "%s cannot be taken: it must be a property outside a complete set or a detached building", a.Take)
	}
	// Decision Q1a: the offered card is also outside a complete set.
	// Decision Q1a-F2: the offer must be a property, not a building.
	if !stealable(&s.Players[seat], a.Offer, false) {
		return nil, reject(CodeInvalidTarget, "offer one of your properties outside a complete set")
	}
	return s.declare(seat, c, nil, Pending{Action: ForcedDeal, Take: a.Take, Offer: a.Offer}, []int{a.Target}), nil
}

func (s *State) dealBreaker(seat int, a PlayDealBreaker) ([]Event, error) {
	if err := s.requirePlay(seat, 1); err != nil {
		return nil, err
	}
	c, err := s.actionCard(seat, a.Card, DealBreaker)
	if err != nil {
		return nil, err
	}
	if err := s.opponent(seat, a.Target); err != nil {
		return nil, err
	}
	i, ok := s.Players[a.Target].setByID(a.Set)
	if !ok || !s.Players[a.Target].Sets[i].Complete() {
		return nil, reject(CodeInvalidTarget, "Deal Breaker needs one of that player's complete sets")
	}
	return s.declare(seat, c, nil, Pending{Action: DealBreaker, SetID: a.Set}, []int{a.Target}), nil
}

func (s *State) debtCollector(seat int, a PlayDebtCollector) ([]Event, error) {
	if err := s.requirePlay(seat, 1); err != nil {
		return nil, err
	}
	c, err := s.actionCard(seat, a.Card, DebtCollector)
	if err != nil {
		return nil, err
	}
	if err := s.opponent(seat, a.Target); err != nil {
		return nil, err
	}
	return s.declare(seat, c, nil, Pending{Action: DebtCollector, Base: 5}, []int{a.Target}), nil
}

func (s *State) birthday(seat int, a PlayBirthday) ([]Event, error) {
	if err := s.requirePlay(seat, 1); err != nil {
		return nil, err
	}
	c, err := s.actionCard(seat, a.Card, Birthday)
	if err != nil {
		return nil, err
	}
	return s.declare(seat, c, nil, Pending{Action: Birthday, Base: 2}, s.clockwise(seat)), nil
}

func (s *State) rent(seat int, a PlayRent) ([]Event, error) {
	if err := s.requirePlay(seat, 1+len(a.Doublers)); err != nil {
		return nil, err
	}
	c, err := s.handCard(seat, a.Card)
	if err != nil {
		return nil, err
	}
	if c.Kind != KindRent && c.Kind != KindRentAny {
		return nil, reject(CodeInvalidCard, "%s is not a Rent card", a.Card)
	}
	if len(a.Doublers) > 2 {
		return nil, reject(CodeIllegal, "at most two Double the Rent cards")
	}
	// Decision Q2: doublers accompany two-color Rent only.
	if c.Kind == KindRentAny && len(a.Doublers) > 0 {
		return nil, reject(CodeIllegal, "Double the Rent needs a two-color Rent card")
	}
	for i, d := range a.Doublers {
		if _, err := s.actionCard(seat, d, DoubleRent); err != nil {
			return nil, err
		}
		if slices.Index(a.Doublers, d) != i {
			return nil, reject(CodeInvalidCard, "duplicate Double the Rent card")
		}
	}
	p := &s.Players[seat]
	i, ok := p.setByID(a.Set)
	if !ok {
		return nil, reject(CodeInvalidTarget, "charge rent on one of your own sets")
	}
	set := p.Sets[i]
	if c.Kind == KindRent && !slices.Contains(c.Colors, set.Color) {
		return nil, reject(CodeIllegal, "this Rent card does not match %s", set.Color)
	}
	base := set.Rent()
	if base == 0 {
		return nil, reject(CodeIllegal, "set %s earns no rent", set.ID)
	}
	var targets []int
	if c.Kind == KindRentAny {
		if a.Target == nil {
			return nil, reject(CodeInvalidTarget, "multicolor Rent charges one chosen player")
		}
		if err := s.opponent(seat, *a.Target); err != nil {
			return nil, err
		}
		targets = []int{*a.Target}
	} else {
		if a.Target != nil {
			return nil, reject(CodeInvalidTarget, "two-color Rent charges every other player")
		}
		targets = s.clockwise(seat)
	}
	return s.declare(seat, c, a.Doublers, Pending{Action: ActionRent, Base: base, SetID: set.ID, Color: set.Color}, targets), nil
}

// currentTarget returns the target whose response or payment is open and
// checks the submission names the live pending action and step.
func (s *State) currentTarget(pending, step int) (*Target, error) {
	p := s.Pending
	if p == nil || (s.Phase != PhaseResponse && s.Phase != PhasePayment) {
		return nil, reject(CodeWrongPhase, "no action is waiting for a response")
	}
	if pending != p.ID || step != p.Step {
		return nil, reject(CodeStale, "that response is for an earlier state (pending %d step %d)", p.ID, p.Step)
	}
	return &p.Targets[p.Current], nil
}

func (t *Target) component(key string) *Component {
	for i := range t.Components {
		if t.Components[i].Key == key {
			return &t.Components[i]
		}
	}
	return nil
}

func (t *Target) anyActive() bool {
	for _, c := range t.Components {
		if c.State == ComponentActive {
			return true
		}
	}
	return false
}

func (s *State) accept(seat int, a Accept) ([]Event, error) {
	t, err := s.currentTarget(a.Pending, a.Step)
	if err != nil {
		return nil, err
	}
	p := s.Pending
	switch t.Stage {
	case StageRespond:
		if seat != t.Seat {
			return nil, reject(CodeNotYourTurn, "seat %d is responding", t.Seat)
		}
		p.Step++
		events := []Event{{Kind: "accepted", Audience: Public, Data: map[string]any{"pending": p.ID, "seat": seat}}}
		return append(events, s.proceed()...), nil
	case StageChain:
		if seat != t.Chain.Waiting {
			return nil, reject(CodeNotYourTurn, "seat %d may counter now", t.Chain.Waiting)
		}
		p.Step++
		return s.closeChain(seat), nil
	}
	return nil, reject(CodeWrongPhase, "no response is open")
}

// justSayNo starts or extends a chain. A defender starts a chain against one
// still-active component (decisions Q2, Q2-F1); afterwards only the source
// and that defender alternate (decision Q1b). Each Just Say No flips parity.
// Responses are free and cost no play (W Just Say No FAQ 1 and 5).
func (s *State) justSayNo(seat int, a PlayJustSayNo) ([]Event, error) {
	t, err := s.currentTarget(a.Pending, a.Step)
	if err != nil {
		return nil, err
	}
	if _, err := s.actionCard(seat, a.Card, JustSayNo); err != nil {
		return nil, err
	}
	p := s.Pending
	switch t.Stage {
	case StageRespond:
		if seat != t.Seat {
			return nil, reject(CodeNotYourTurn, "seat %d is responding", t.Seat)
		}
		key := a.Component
		if key == "" {
			key = ComponentCharge
		}
		c := t.component(key)
		if c == nil || c.State != ComponentActive {
			return nil, reject(CodeInvalidTarget, "%q is not an active part of this action", key)
		}
		t.Chain = &Chain{Component: key, Blocked: true, Waiting: p.Source}
		t.Stage = StageChain
	case StageChain:
		if seat != t.Chain.Waiting {
			return nil, reject(CodeNotYourTurn, "seat %d may counter now", t.Chain.Waiting)
		}
		if a.Component != "" && a.Component != t.Chain.Component {
			return nil, reject(CodeInvalidTarget, "a counter applies to the open chain")
		}
		t.Chain.Blocked = !t.Chain.Blocked
		if seat == p.Source {
			t.Chain.Waiting = t.Seat
		} else {
			t.Chain.Waiting = p.Source
		}
	default:
		return nil, reject(CodeWrongPhase, "no response is open")
	}
	s.fromHand(seat, a.Card)
	p.Spent = append(p.Spent, a.Card)
	p.Step++
	return []Event{{Kind: "just_say_no", Audience: Public, Data: map[string]any{
		"pending": p.ID, "seat": seat, "card": a.Card, "component": t.Chain.Component, "blocked": t.Chain.Blocked,
	}}}, nil
}

// closeChain ends a chain with its current parity. A blocked whole charge
// ends this defender's obligation; otherwise the defender may start another
// chain on a remaining active part or the effect proceeds.
func (s *State) closeChain(seat int) []Event {
	t := &s.Pending.Targets[s.Pending.Current]
	ch := t.Chain
	t.Chain = nil
	c := t.component(ch.Component)
	c.State = ComponentSettled
	if ch.Blocked {
		c.State = ComponentBlocked
	}
	events := []Event{{Kind: "chain_closed", Audience: Public, Data: map[string]any{
		"pending": s.Pending.ID, "seat": seat, "component": ch.Component, "blocked": ch.Blocked,
	}}}
	if t.component(ComponentCharge).State == ComponentBlocked {
		t.Stage = StageDone
		t.Outcome = "blocked"
		return append(events, s.advance()...)
	}
	if t.anyActive() {
		t.Stage = StageRespond
		return events
	}
	return append(events, s.proceed()...)
}

// proceed applies the surviving effect to the current target.
func (s *State) proceed() []Event {
	p := s.Pending
	t := &p.Targets[p.Current]
	switch p.Action {
	case DebtCollector, Birthday, ActionRent:
		owed := p.Base
		for _, d := range p.Doublers {
			if t.component(string(d)).State != ComponentBlocked {
				owed *= 2
			}
		}
		t.Owed = owed
		t.Stage = StagePay
		s.Phase = PhasePayment
		return []Event{{Kind: "payment_due", Audience: Public, Data: map[string]any{"pending": p.ID, "seat": t.Seat, "owed": owed}}}
	case SlyDeal:
		s.moveStolen(t.Seat, p.Source, p.Take)
	case ForcedDeal:
		s.moveStolen(t.Seat, p.Source, p.Take)
		s.Players[p.Source].takeTabled(p.Offer)
		s.Players[p.Source].normalize()
		s.Players[t.Seat].Incoming = append(s.Players[t.Seat].Incoming, p.Offer)
	case DealBreaker:
		target := &s.Players[t.Seat]
		i, _ := target.setByID(p.SetID)
		set := target.Sets[i]
		target.Sets = slices.Delete(target.Sets, i, i+1)
		s.Players[p.Source].Sets = append(s.Players[p.Source].Sets, set)
	}
	t.Stage = StageDone
	t.Outcome = "transferred"
	events := []Event{{Kind: "transferred", Audience: Public, Data: map[string]any{"pending": p.ID, "seat": t.Seat, "action": p.Action}}}
	return append(events, s.advance()...)
}

// moveStolen moves one card taken by Sly or Forced Deal. Properties await the
// recipient's placement; buildings stay detached (decision Q3).
func (s *State) moveStolen(from, to int, id CardID) {
	s.Players[from].takeTabled(id)
	s.Players[from].normalize()
	if mustCard(id).IsBuilding() {
		s.Players[to].Detached = append(s.Players[to].Detached, id)
	} else {
		s.Players[to].Incoming = append(s.Players[to].Incoming, id)
	}
}

// advance opens the next target clockwise, or finishes the action.
func (s *State) advance() []Event {
	p := s.Pending
	for p.Current+1 < len(p.Targets) {
		p.Current++
		t := &p.Targets[p.Current]
		if t.Stage == StageWaiting {
			t.Stage = StageRespond
			s.Phase = PhaseResponse
			return nil
		}
	}
	return s.finish()
}

// finish commits the action and every Just Say No to the center pile, then
// waits for received-card placements before play resumes and victory is
// checked (spec response protocol 4–5).
func (s *State) finish() []Event {
	p := s.Pending
	s.Discard = append(s.Discard, p.Card)
	s.Discard = append(s.Discard, p.Doublers...)
	s.Discard = append(s.Discard, p.Spent...)
	s.Pending = nil
	events := []Event{{Kind: "action_resolved", Audience: Public, Data: map[string]any{"pending": p.ID}}}
	for _, pl := range s.Players {
		if len(pl.Incoming) > 0 {
			s.Phase = PhasePlacement
			return events
		}
	}
	s.Phase = PhasePlay
	return append(events, s.checkVictory()...)
}

// payDebt applies the payer's chosen cards (B1 How to Pay; G2). The payer
// picks from bank and property area only. If eligible value covers the debt
// the selection must too, and overpaying is allowed with no change; otherwise
// every eligible card goes and the rest is forgiven. Multicolor wilds have no
// value and cannot be selected.
func (s *State) payDebt(seat int, a Pay) ([]Event, error) {
	t, err := s.currentTarget(a.Pending, a.Step)
	if err != nil {
		return nil, err
	}
	if t.Stage != StagePay {
		return nil, reject(CodeWrongPhase, "no payment is due")
	}
	if seat != t.Seat {
		return nil, reject(CodeNotYourTurn, "seat %d is paying", t.Seat)
	}
	payer := &s.Players[seat]
	eligible := 0
	var all []CardID
	for _, id := range payer.Bank {
		all = append(all, id)
	}
	for _, set := range payer.Sets {
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
	all = append(all, payer.Detached...)
	for _, id := range all {
		eligible += mustCard(id).Value
	}
	seen := map[CardID]bool{}
	paid := 0
	for _, id := range a.Cards {
		if seen[id] {
			return nil, reject(CodeInvalidCard, "%s is listed twice", id)
		}
		seen[id] = true
		if !slices.Contains(all, id) {
			return nil, reject(CodeInvalidCard, "%s is not a card you can pay with", id)
		}
		paid += mustCard(id).Value
	}
	if eligible >= t.Owed {
		if paid < t.Owed {
			return nil, reject(CodeIllegal, "pay at least %dM; selected %dM", t.Owed, paid)
		}
	} else if len(a.Cards) != len(all) {
		return nil, reject(CodeIllegal, "your tabled value is below the debt: give every card with value")
	}
	recipient := &s.Players[s.Pending.Source]
	for _, id := range a.Cards {
		loc, _ := payer.locate(id)
		payer.takeTabled(id)
		switch {
		case loc.zone == "bank":
			recipient.Bank = append(recipient.Bank, id)
		case mustCard(id).IsBuilding():
			// Decision Q3.3: a paid building stays a building, detached.
			recipient.Detached = append(recipient.Detached, id)
		default:
			recipient.Incoming = append(recipient.Incoming, id)
		}
	}
	payer.normalize()
	t.Stage = StageDone
	t.Outcome = "paid"
	s.Pending.Step++
	events := []Event{{Kind: "paid", Audience: Public, Data: map[string]any{"pending": s.Pending.ID, "seat": seat, "cards": a.Cards, "value": paid, "owed": t.Owed}}}
	return append(events, s.advance()...), nil
}
