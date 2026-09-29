package monopoly

import "slices"

// Apply validates and applies one action by seat. It works on a clone and
// returns the new state only on success, so an illegal or out-of-turn action
// leaves the original state untouched. The result depends only on the input
// state and action: randomness comes from the state's seed.
func Apply(s *State, seat int, a Action) (*State, []Event, error) {
	if seat < 0 || seat >= len(s.Players) {
		return s, nil, reject(CodeInvalidAction, "unknown seat %d", seat)
	}
	if s.Phase == PhaseFinished {
		return s, nil, reject(CodeGameOver, "the game is over")
	}
	next := s.Clone()
	events, err := next.apply(seat, a)
	if err != nil {
		return s, nil, err
	}
	if err := next.CheckInvariants(); err != nil {
		return s, nil, reject(CodeIllegal, "rejected: %v", err)
	}
	return next, events, nil
}

func (s *State) apply(seat int, a Action) ([]Event, error) {
	switch a := a.(type) {
	case Bank:
		return s.bank(seat, a)
	case PlayProperty:
		return s.playProperty(seat, a)
	case PlayPassGo:
		return s.passGo(seat, a)
	case PlayBuilding:
		return s.playBuilding(seat, a)
	case PlaySlyDeal:
		return s.slyDeal(seat, a)
	case PlayForcedDeal:
		return s.forcedDeal(seat, a)
	case PlayDealBreaker:
		return s.dealBreaker(seat, a)
	case PlayDebtCollector:
		return s.debtCollector(seat, a)
	case PlayBirthday:
		return s.birthday(seat, a)
	case PlayRent:
		return s.rent(seat, a)
	case Accept:
		return s.accept(seat, a)
	case PlayJustSayNo:
		return s.justSayNo(seat, a)
	case Pay:
		return s.payDebt(seat, a)
	case PlaceReceived:
		return s.placeReceived(seat, a)
	case Rearrange:
		return s.rearrange(seat, a)
	case EndTurn:
		return s.endTurn(seat, a)
	}
	return nil, reject(CodeInvalidAction, "unsupported action %T", a)
}

// requirePlay checks the active player may spend n plays now.
func (s *State) requirePlay(seat, n int) error {
	if s.Phase != PhasePlay {
		return reject(CodeWrongPhase, "cannot play cards during %s", s.Phase)
	}
	if seat != s.Active {
		return reject(CodeNotYourTurn, "it is seat %d's turn", s.Active)
	}
	if s.PlaysUsed+n > MaxPlays {
		return reject(CodeNoPlays, "this needs %d play(s); %d left", n, MaxPlays-s.PlaysUsed)
	}
	return nil
}

// handCard returns a card the seat holds.
func (s *State) handCard(seat int, id CardID) (Card, error) {
	if !slices.Contains(s.Players[seat].Hand, id) {
		return Card{}, reject(CodeInvalidCard, "card %s is not in your hand", id)
	}
	return mustCard(id), nil
}

func (s *State) fromHand(seat int, id CardID) {
	s.Players[seat].Hand, _ = removeCard(s.Players[seat].Hand, id)
}

func (s *State) opponent(seat, target int) error {
	if target < 0 || target >= len(s.Players) || target == seat {
		return reject(CodeInvalidTarget, "choose another player")
	}
	return nil
}

// checkVictory ends the game when the active player has three complete sets
// of different colors (B1 How to Win). Only the active player can win, so an
// off-turn collection waits for its owner's turn (B1 turn 2B).
func (s *State) checkVictory() []Event {
	if s.Phase != PhasePlay || s.Players[s.Active].CompleteColors() < 3 {
		return nil
	}
	winner := s.Active
	s.Winner = &winner
	s.Phase = PhaseFinished
	return []Event{{Kind: "game_won", Audience: Public, Data: map[string]any{"seat": winner}}}
}

func (s *State) bank(seat int, a Bank) ([]Event, error) {
	if err := s.requirePlay(seat, 1); err != nil {
		return nil, err
	}
	c, err := s.handCard(seat, a.Card)
	if err != nil {
		return nil, err
	}
	if c.IsProperty() {
		return nil, reject(CodeInvalidCard, "properties cannot be banked")
	}
	s.fromHand(seat, a.Card)
	s.Players[seat].Bank = append(s.Players[seat].Bank, a.Card)
	s.PlaysUsed++
	return []Event{{Kind: "banked", Audience: Public, Data: map[string]any{"seat": seat, "card": a.Card}}}, nil
}

// place adds a property card to the player's area: an existing own set with
// room, a new set of a legal color, or (multicolor wild only) unassigned.
func (s *State) place(seat int, id CardID, setID string, color Color) error {
	c := mustCard(id)
	p := &s.Players[seat]
	if setID != "" {
		i, ok := p.setByID(setID)
		if !ok {
			return reject(CodeInvalidTarget, "set %s is not yours", setID)
		}
		set := &p.Sets[i]
		if color != "" && color != set.Color {
			return reject(CodeIllegal, "set %s is %s", setID, set.Color)
		}
		if !c.CanBe(set.Color) {
			return reject(CodeIllegal, "%s cannot be %s", id, set.Color)
		}
		if len(set.Cards) >= colorInfo[set.Color].Size {
			return reject(CodeIllegal, "set %s is full; start a new set", setID)
		}
		set.Cards = append(set.Cards, id)
		return nil
	}
	if color == "" {
		switch c.Kind {
		case KindRainbowWild:
			p.Unassigned = append(p.Unassigned, id)
			return nil
		case KindProperty:
			color = c.Colors[0]
		default:
			return reject(CodeIllegal, "choose a color for %s", id)
		}
	}
	if !c.CanBe(color) {
		return reject(CodeIllegal, "%s cannot be %s", id, color)
	}
	p.Sets = append(p.Sets, PropertySet{ID: s.newSetID(), Color: color, Cards: []CardID{id}})
	return nil
}

func (s *State) playProperty(seat int, a PlayProperty) ([]Event, error) {
	if err := s.requirePlay(seat, 1); err != nil {
		return nil, err
	}
	c, err := s.handCard(seat, a.Card)
	if err != nil {
		return nil, err
	}
	if !c.IsProperty() {
		return nil, reject(CodeInvalidCard, "%s is not a property", a.Card)
	}
	s.fromHand(seat, a.Card)
	if err := s.place(seat, a.Card, a.Set, a.Color); err != nil {
		return nil, err
	}
	s.PlaysUsed++
	events := []Event{{Kind: "property_played", Audience: Public, Data: map[string]any{"seat": seat, "card": a.Card, "set": a.Set, "color": a.Color}}}
	return append(events, s.checkVictory()...), nil
}

func (s *State) passGo(seat int, a PlayPassGo) ([]Event, error) {
	if err := s.requirePlay(seat, 1); err != nil {
		return nil, err
	}
	c, err := s.handCard(seat, a.Card)
	if err != nil {
		return nil, err
	}
	if c.Kind != KindAction || c.Action != PassGo {
		return nil, reject(CodeInvalidCard, "%s is not Pass Go", a.Card)
	}
	s.fromHand(seat, a.Card)
	s.PlaysUsed++
	// The Pass Go card is resolving while its draw happens, so a reshuffle
	// cannot recycle it; it reaches the center pile afterwards.
	events := []Event{{Kind: "pass_go", Audience: Public, Data: map[string]any{"seat": seat, "card": a.Card}}}
	events = append(events, s.drawEvents(seat, 2)...)
	s.Discard = append(s.Discard, a.Card)
	return events, nil
}

func (s *State) playBuilding(seat int, a PlayBuilding) ([]Event, error) {
	if err := s.requirePlay(seat, 1); err != nil {
		return nil, err
	}
	c, err := s.handCard(seat, a.Card)
	if err != nil {
		return nil, err
	}
	if !c.IsBuilding() {
		return nil, reject(CodeInvalidCard, "%s is not a House or Hotel", a.Card)
	}
	p := &s.Players[seat]
	i, ok := p.setByID(a.Set)
	if !ok {
		return nil, reject(CodeInvalidTarget, "set %s is not yours", a.Set)
	}
	if err := canAttach(p.Sets[i], c.Action); err != nil {
		return nil, err
	}
	s.fromHand(seat, a.Card)
	if c.Action == House {
		p.Sets[i].House = a.Card
	} else {
		p.Sets[i].Hotel = a.Card
	}
	s.PlaysUsed++
	return []Event{{Kind: "building_played", Audience: Public, Data: map[string]any{"seat": seat, "card": a.Card, "set": a.Set}}}, nil
}

// canAttach applies B1 House/Hotel: a complete set, not railroads or
// utilities, one of each, and a House before a Hotel.
func canAttach(set PropertySet, kind ActionType) error {
	if !set.Complete() {
		return reject(CodeIllegal, "buildings need a complete set")
	}
	if !colorInfo[set.Color].Buildable {
		return reject(CodeIllegal, "no buildings on railroads or utilities")
	}
	if kind == House && set.House != "" {
		return reject(CodeIllegal, "set %s already has a House", set.ID)
	}
	if kind == Hotel {
		if set.House == "" {
			return reject(CodeIllegal, "a Hotel needs a House first")
		}
		if set.Hotel != "" {
			return reject(CodeIllegal, "set %s already has a Hotel", set.ID)
		}
	}
	return nil
}

func (s *State) endTurn(seat int, a EndTurn) ([]Event, error) {
	if s.Phase != PhasePlay {
		return nil, reject(CodeWrongPhase, "finish the pending action first")
	}
	if seat != s.Active {
		return nil, reject(CodeNotYourTurn, "it is seat %d's turn", s.Active)
	}
	p := &s.Players[seat]
	excess := max(0, len(p.Hand)-HandLimit)
	if len(a.Return) != excess {
		return nil, reject(CodeIllegal, "return exactly %d card(s) to end your turn", excess)
	}
	seen := map[CardID]bool{}
	for _, id := range a.Return {
		if seen[id] || !slices.Contains(p.Hand, id) {
			return nil, reject(CodeInvalidCard, "returned cards must be distinct cards in your hand")
		}
		seen[id] = true
	}
	for _, id := range a.Return {
		p.Hand, _ = removeCard(p.Hand, id)
	}
	// The first listed card is the next of this group to be drawn.
	s.Draw = append(s.Draw, a.Return...)
	events := []Event{{Kind: "turn_ended", Audience: Public, Data: map[string]any{"seat": seat, "returned": len(a.Return)}}}
	if len(a.Return) > 0 {
		events = append(events, Event{Kind: "returned_cards", Audience: seat, Data: map[string]any{"cards": a.Return}})
	}
	return append(events, s.startTurn((seat+1)%len(s.Players))...), nil
}

func (s *State) placeReceived(seat int, a PlaceReceived) ([]Event, error) {
	if s.Phase != PhasePlacement {
		return nil, reject(CodeWrongPhase, "nothing to place now")
	}
	p := &s.Players[seat]
	if !slices.Contains(p.Incoming, a.Card) {
		return nil, reject(CodeInvalidCard, "%s is not waiting for you to place it", a.Card)
	}
	p.Incoming, _ = removeCard(p.Incoming, a.Card)
	if err := s.place(seat, a.Card, a.Set, a.Color); err != nil {
		return nil, err
	}
	events := []Event{{Kind: "received_placed", Audience: Public, Data: map[string]any{"seat": seat, "card": a.Card, "set": a.Set, "color": a.Color}}}
	for _, other := range s.Players {
		if len(other.Incoming) > 0 {
			return events, nil
		}
	}
	s.Phase = PhasePlay
	return append(events, s.checkVictory()...), nil
}

// rearrange replaces the active player's property area atomically. Every
// tabled property and non-banked building must appear exactly once in a
// legal position; buildings may move free to eligible complete sets (Q3.5).
func (s *State) rearrange(seat int, a Rearrange) ([]Event, error) {
	if s.Phase != PhasePlay {
		return nil, reject(CodeWrongPhase, "rearrange only when nothing is pending")
	}
	if seat != s.Active {
		return nil, reject(CodeNotYourTurn, "you can only reorganize on your turn")
	}
	p := &s.Players[seat]
	current := map[CardID]bool{}
	for _, set := range p.Sets {
		for _, id := range set.Cards {
			current[id] = true
		}
		if set.House != "" {
			current[set.House] = true
		}
		if set.Hotel != "" {
			current[set.Hotel] = true
		}
	}
	for _, id := range append(slices.Clone(p.Unassigned), p.Detached...) {
		current[id] = true
	}
	used := map[CardID]bool{}
	claim := func(id CardID) error {
		if id == "" {
			return nil
		}
		if used[id] || !current[id] {
			return reject(CodeInvalidCard, "%s is not an unplaced card from your property area", id)
		}
		used[id] = true
		return nil
	}
	ids := map[string]bool{}
	var sets []PropertySet
	for _, l := range a.Sets {
		info, ok := colorInfo[l.Color]
		if !ok {
			return nil, reject(CodeIllegal, "unknown color %q", l.Color)
		}
		if len(l.Cards) == 0 || len(l.Cards) > info.Size {
			return nil, reject(CodeIllegal, "a %s set holds 1-%d properties", l.Color, info.Size)
		}
		set := PropertySet{ID: l.ID, Color: l.Color, Cards: slices.Clone(l.Cards)}
		for _, id := range l.Cards {
			if err := claim(id); err != nil {
				return nil, err
			}
			if !mustCard(id).CanBe(l.Color) {
				return nil, reject(CodeIllegal, "%s cannot be %s", id, l.Color)
			}
		}
		for _, b := range []struct {
			id   CardID
			kind ActionType
		}{{l.House, House}, {l.Hotel, Hotel}} {
			if b.id == "" {
				continue
			}
			if err := claim(b.id); err != nil {
				return nil, err
			}
			if c := mustCard(b.id); c.Kind != KindAction || c.Action != b.kind {
				return nil, reject(CodeInvalidCard, "%s is not a %s", b.id, b.kind)
			}
			if err := canAttach(set, b.kind); err != nil {
				return nil, err
			}
			if b.kind == House {
				set.House = b.id
			} else {
				set.Hotel = b.id
			}
		}
		if set.ID != "" {
			if _, ok := p.setByID(set.ID); !ok || ids[set.ID] {
				return nil, reject(CodeInvalidTarget, "set %s cannot be reused", set.ID)
			}
			ids[set.ID] = true
		}
		sets = append(sets, set)
	}
	for _, id := range a.Unassigned {
		if err := claim(id); err != nil {
			return nil, err
		}
		if mustCard(id).Kind != KindRainbowWild {
			return nil, reject(CodeIllegal, "only multicolor wilds may be unassigned")
		}
	}
	for _, id := range a.Detached {
		if err := claim(id); err != nil {
			return nil, err
		}
		if !mustCard(id).IsBuilding() {
			return nil, reject(CodeIllegal, "only buildings may be detached")
		}
	}
	if len(used) != len(current) {
		return nil, reject(CodeIllegal, "the layout must include every card in your property area exactly once")
	}
	for i := range sets {
		if sets[i].ID == "" {
			sets[i].ID = s.newSetID()
		}
	}
	p.Sets = sets
	p.Unassigned = append([]CardID{}, a.Unassigned...)
	p.Detached = append([]CardID{}, a.Detached...)
	events := []Event{{Kind: "rearranged", Audience: Public, Data: map[string]any{"seat": seat}}}
	return append(events, s.checkVictory()...), nil
}
