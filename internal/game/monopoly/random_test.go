package monopoly

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"slices"
	"testing"
)

// bot proposes plausible and implausible actions for whichever seat the game
// is waiting on. It exercises the engine, it does not play well.
type bot struct{ r *rand.Rand }

func (b bot) pick(ids []CardID) CardID {
	if len(ids) == 0 {
		return ""
	}
	return ids[b.r.IntN(len(ids))]
}

func (b bot) tabled(p Player) []CardID {
	var out []CardID
	for _, set := range p.Sets {
		out = append(out, set.Cards...)
		if set.House != "" {
			out = append(out, set.House)
		}
	}
	return append(append(out, p.Unassigned...), p.Detached...)
}

func (b bot) setID(p Player) string {
	if len(p.Sets) == 0 || b.r.IntN(4) == 0 {
		return ""
	}
	return p.Sets[b.r.IntN(len(p.Sets))].ID
}

func (b bot) color() Color { return Colors[b.r.IntN(len(Colors))] }

func (b bot) propose(s *State, seat int) Action {
	p := s.Players[seat]
	other := b.r.IntN(len(s.Players))
	switch s.Phase {
	case PhaseResponse:
		if b.r.IntN(3) == 0 {
			comp := ComponentCharge
			if d := s.Pending.Doublers; len(d) > 0 && b.r.IntN(2) == 0 {
				comp = string(d[b.r.IntN(len(d))])
			}
			return jsn(s, b.pick(p.Hand), comp)
		}
		return accept(s)
	case PhasePayment:
		all := append(slices.Clone(p.Bank), b.tabled(p)...)
		b.r.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
		return pay(s, all[:b.r.IntN(len(all)+1)]...)
	case PhasePlacement:
		card := b.pick(p.Incoming)
		c, _ := Lookup(card)
		color := b.color()
		if len(c.Colors) > 0 && b.r.IntN(3) > 0 {
			color = c.Colors[b.r.IntN(len(c.Colors))]
		}
		return PlaceReceived{Card: card, Set: b.setID(p), Color: color}
	}
	card := b.pick(p.Hand)
	c, _ := Lookup(card)
	target := s.Players[other]
	switch b.r.IntN(12) {
	case 0:
		hand := slices.Clone(p.Hand)
		b.r.Shuffle(len(hand), func(i, j int) { hand[i], hand[j] = hand[j], hand[i] })
		return EndTurn{Return: hand[:max(0, len(hand)-HandLimit)]}
	case 1:
		return Bank{Card: card}
	case 2:
		return PlayPassGo{Card: card}
	case 3:
		return PlayBuilding{Card: card, Set: b.setID(p)}
	case 4:
		return PlaySlyDeal{Card: card, Target: other, Take: b.pick(b.tabled(target))}
	case 5:
		return PlayForcedDeal{Card: card, Target: other, Take: b.pick(b.tabled(target)), Offer: b.pick(b.tabled(p))}
	case 6:
		return PlayDealBreaker{Card: card, Target: other, Set: b.setID(target)}
	case 7:
		return PlayDebtCollector{Card: card, Target: other}
	case 8:
		return PlayBirthday{Card: card}
	case 9:
		a := PlayRent{Card: card, Set: b.setID(p)}
		if c.Kind == KindRentAny {
			a.Target = &other
		}
		for _, id := range p.Hand {
			if mustCard(id).Action == DoubleRent && b.r.IntN(2) == 0 {
				a.Doublers = append(a.Doublers, id)
			}
		}
		return a
	case 10:
		// Re-submit the current layout with one wild recolored.
		var layout Rearrange
		for _, set := range p.Sets {
			l := SetLayout{ID: set.ID, Color: set.Color, Cards: slices.Clone(set.Cards), House: set.House, Hotel: set.Hotel}
			if b.r.IntN(4) == 0 {
				l.Color = b.color()
			}
			layout.Sets = append(layout.Sets, l)
		}
		layout.Unassigned, layout.Detached = p.Unassigned, p.Detached
		return layout
	}
	color := Color("")
	if c.Kind == KindWild || b.r.IntN(5) == 0 {
		color = b.color()
		if len(c.Colors) > 0 && b.r.IntN(3) > 0 {
			color = c.Colors[b.r.IntN(len(c.Colors))]
		}
	}
	return PlayProperty{Card: card, Set: b.setID(p), Color: color}
}

// A24: across many random games every accepted transition keeps all 106
// cards in legal zones, and every rejection leaves the state unchanged.
func TestRandomGamesPreserveInvariants(t *testing.T) {
	// Turns end automatically after three plays, so random bots get fewer
	// free rearranges per turn; 3000 steps still finishes 18 seeded games.
	games, steps := 300, 3000
	finished, accepted, rejectedCount := 0, 0, 0
	kinds := map[string]int{}
	for g := range games {
		seed := bytes.Repeat([]byte{byte(g), byte(g >> 8)}, SeedSize/2)
		players := []string{"a", "b", "c", "d", "e"}[:2+g%4]
		s, _, err := NewGame(players, bytes.NewReader(seed))
		if err != nil {
			t.Fatal(err)
		}
		b := bot{rand.New(rand.NewPCG(uint64(g), 99))}
		for range steps {
			if s.Phase == PhaseFinished {
				finished++
				break
			}
			waiting := s.WaitingFor()
			seat := waiting[b.r.IntN(len(waiting))]
			if b.r.IntN(20) == 0 {
				seat = b.r.IntN(len(s.Players)) // sometimes the wrong seat
			}
			a := b.propose(s, seat)
			var before string
			if b.r.IntN(10) == 0 {
				before = snapshot(t, s)
			}
			next, _, err := Apply(s, seat, a)
			if err != nil {
				var ie *InvariantError
				if errors.As(err, &ie) {
					t.Fatalf("game %d: %s broke an invariant: %s", g, a.Kind(), ie.Detail)
				}
				var re *RuleError
				if !errors.As(err, &re) || next != s || (before != "" && snapshot(t, s) != before) {
					t.Fatalf("game %d: bad rejection of %s: %v", g, a.Kind(), err)
				}
				rejectedCount++
				continue
			}
			if err := next.CheckInvariants(); err != nil {
				t.Fatalf("game %d: %v", g, err)
			}
			accepted++
			kinds[a.Kind()]++
			s = next
		}
	}
	t.Logf("%d games, %d finished, %d accepted, %d rejected actions", games, finished, accepted, rejectedCount)
	for _, k := range []string{"bank", "play_property", "pass_go", "play_building", "sly_deal", "forced_deal", "deal_breaker", "debt_collector", "birthday", "rent", "accept", "just_say_no", "pay", "place_received", "rearrange", "end_turn"} {
		if kinds[k] == 0 {
			t.Errorf("random play never accepted %s", k)
		}
	}
	t.Logf("accepted by kind: %v", kinds)
	if finished == 0 || accepted < games*50 {
		t.Fatalf("random play did not progress: %d finished, %d accepted", finished, accepted)
	}
}
