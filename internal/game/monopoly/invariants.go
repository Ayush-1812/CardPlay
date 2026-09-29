package monopoly

import (
	"errors"
	"fmt"
)

// CheckInvariants verifies acceptance item A24: all 106 cards exist exactly
// once in a legal zone, sets respect sizes, colors and building rules, and
// the play budget and phase are consistent.
func (s *State) CheckInvariants() error {
	if s.Schema != SchemaVersion {
		return fmt.Errorf("unsupported schema %d", s.Schema)
	}
	if len(s.Seed) != SeedSize {
		return errors.New("missing seed")
	}
	if len(s.Players) < 2 || len(s.Players) > 5 {
		return errors.New("2-5 players required")
	}
	if s.Active < 0 || s.Active >= len(s.Players) {
		return errors.New("active seat out of range")
	}
	if s.PlaysUsed < 0 || s.PlaysUsed > MaxPlays {
		return fmt.Errorf("plays used %d out of range", s.PlaysUsed)
	}
	seen := make(map[CardID]string, len(manifest))
	put := func(id CardID, zone string) error {
		if _, ok := cardByID[id]; !ok {
			return fmt.Errorf("unknown card %s in %s", id, zone)
		}
		if prev, dup := seen[id]; dup {
			return fmt.Errorf("card %s in both %s and %s", id, prev, zone)
		}
		seen[id] = zone
		return nil
	}
	putAll := func(ids []CardID, zone string, ok func(Card) bool) error {
		for _, id := range ids {
			if err := put(id, zone); err != nil {
				return err
			}
			if ok != nil && !ok(mustCard(id)) {
				return fmt.Errorf("card %s not allowed in %s", id, zone)
			}
		}
		return nil
	}
	notProperty := func(c Card) bool { return !c.IsProperty() }
	isProperty := func(c Card) bool { return c.IsProperty() }
	for seat, p := range s.Players {
		zone := func(name string) string { return fmt.Sprintf("seat %d %s", seat, name) }
		if err := putAll(p.Hand, zone("hand"), nil); err != nil {
			return err
		}
		if err := putAll(p.Bank, zone("bank"), notProperty); err != nil {
			return err
		}
		if err := putAll(p.Unassigned, zone("unassigned"), func(c Card) bool { return c.Kind == KindRainbowWild }); err != nil {
			return err
		}
		if err := putAll(p.Detached, zone("detached"), Card.IsBuilding); err != nil {
			return err
		}
		if err := putAll(p.Incoming, zone("incoming"), isProperty); err != nil {
			return err
		}
		ids := map[string]bool{}
		for _, set := range p.Sets {
			info, ok := colorInfo[set.Color]
			if !ok || set.ID == "" || ids[set.ID] {
				return fmt.Errorf("seat %d has an invalid set %q", seat, set.ID)
			}
			ids[set.ID] = true
			if len(set.Cards) == 0 || len(set.Cards) > info.Size {
				return fmt.Errorf("set %s has %d cards", set.ID, len(set.Cards))
			}
			if err := putAll(set.Cards, "set "+set.ID, func(c Card) bool { return c.CanBe(set.Color) }); err != nil {
				return err
			}
			for _, b := range []struct {
				id   CardID
				kind ActionType
			}{{set.House, House}, {set.Hotel, Hotel}} {
				if b.id == "" {
					continue
				}
				if err := put(b.id, "set "+set.ID+" building"); err != nil {
					return err
				}
				if c := mustCard(b.id); c.Kind != KindAction || c.Action != b.kind || !set.Complete() || !info.Buildable {
					return fmt.Errorf("illegal building %s on set %s", b.id, set.ID)
				}
			}
			if set.Hotel != "" && set.House == "" {
				return fmt.Errorf("set %s has a Hotel without a House", set.ID)
			}
		}
	}
	if err := putAll(s.Draw, "draw pile", nil); err != nil {
		return err
	}
	if err := putAll(s.Discard, "center pile", nil); err != nil {
		return err
	}
	incoming := false
	for _, p := range s.Players {
		incoming = incoming || len(p.Incoming) > 0
	}
	switch s.Phase {
	case PhasePlay, PhaseFinished:
		if s.Pending != nil || incoming {
			return fmt.Errorf("%s phase with unresolved work", s.Phase)
		}
	case PhasePlacement:
		if s.Pending != nil || !incoming {
			return errors.New("placement phase without cards to place")
		}
	case PhaseResponse, PhasePayment:
		p := s.Pending
		if p == nil || p.Current < 0 || p.Current >= len(p.Targets) {
			return errors.New("response phase without a pending action")
		}
		if err := put(p.Card, "resolving"); err != nil {
			return err
		}
		if err := putAll(p.Doublers, "resolving", nil); err != nil {
			return err
		}
		if err := putAll(p.Spent, "resolving", nil); err != nil {
			return err
		}
		stage := p.Targets[p.Current].Stage
		if (s.Phase == PhasePayment) != (stage == StagePay) {
			return fmt.Errorf("phase %s with target stage %s", s.Phase, stage)
		}
	default:
		return fmt.Errorf("unknown phase %q", s.Phase)
	}
	if (s.Phase == PhaseFinished) != (s.Winner != nil) {
		return errors.New("winner and finished phase disagree")
	}
	if len(seen) != len(manifest) {
		return fmt.Errorf("%d of %d cards accounted for", len(seen), len(manifest))
	}
	return nil
}
