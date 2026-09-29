package monopoly

import (
	"encoding/json"
	"reflect"
	"testing"
)

// A24: the invariant checker catches every class of corrupted state.
func TestInvariantViolations(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(s *State)
	}{
		{"duplicate card", func(s *State) { s.Players[0].Hand = append(s.Players[0].Hand, s.Draw[0]) }},
		{"missing card", func(s *State) { s.Draw = s.Draw[1:] }},
		{"unknown card", func(s *State) { s.Draw[0] = "joker" }},
		{"property in bank", func(s *State) {
			s.Players[0].Bank = []CardID{prop("boardwalk")}
			s.Draw = remove(s.Draw, prop("boardwalk"))
		}},
		{"money in a set", func(s *State) {
			s.Players[0].Sets = []PropertySet{set("x", Brown, money(1, 1))}
			s.Draw = remove(s.Draw, money(1, 1))
		}},
		{"overfull set", func(s *State) {
			s.Players[0].Sets = []PropertySet{set("x", Brown, brownCards[0], brownCards[1], rainbow(1))}
			s.Draw = remove(s.Draw, brownCards[0], brownCards[1], rainbow(1))
		}},
		{"house on incomplete set", func(s *State) {
			s.Players[0].Sets = []PropertySet{{ID: "x", Color: Brown, Cards: brownCards[:1], House: act(House, 1)}}
			s.Draw = remove(s.Draw, brownCards[0], act(House, 1))
		}},
		{"house on utilities", func(s *State) {
			s.Players[0].Sets = []PropertySet{{ID: "x", Color: Utility, Cards: utilityCards, House: act(House, 1)}}
			s.Draw = remove(s.Draw, utilityCards[0], utilityCards[1], act(House, 1))
		}},
		{"hotel without house", func(s *State) {
			s.Players[0].Sets = []PropertySet{{ID: "x", Color: Brown, Cards: brownCards, Hotel: act(Hotel, 1)}}
			s.Draw = remove(s.Draw, brownCards[0], brownCards[1], act(Hotel, 1))
		}},
		{"duplicate set id", func(s *State) {
			s.Players[0].Sets = []PropertySet{set("x", Brown, brownCards[0]), set("x", Brown, brownCards[1])}
			s.Draw = remove(s.Draw, brownCards...)
		}},
		{"money detached", func(s *State) { s.Players[0].Detached = []CardID{money(1, 1)}; s.Draw = remove(s.Draw, money(1, 1)) }},
		{"ordinary property unassigned", func(s *State) {
			s.Players[0].Unassigned = []CardID{prop("boardwalk")}
			s.Draw = remove(s.Draw, prop("boardwalk"))
		}},
		{"too many plays", func(s *State) { s.PlaysUsed = 4 }},
		{"bad active seat", func(s *State) { s.Active = 7 }},
		{"placement without incoming", func(s *State) { s.Phase = PhasePlacement }},
		{"play phase with incoming", func(s *State) {
			s.Players[0].Incoming = []CardID{prop("boardwalk")}
			s.Draw = remove(s.Draw, prop("boardwalk"))
		}},
		{"response without pending", func(s *State) { s.Phase = PhaseResponse }},
		{"finished without winner", func(s *State) { s.Phase = PhaseFinished }},
		{"unknown phase", func(s *State) { s.Phase = "lunch" }},
		{"missing seed", func(s *State) { s.Seed = nil }},
		{"wrong schema", func(s *State) { s.Schema = 99 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t, seat{}, seat{})
			tc.corrupt(s)
			if s.CheckInvariants() == nil {
				t.Fatal("corruption not detected")
			}
		})
	}
}

func remove(list []CardID, ids ...CardID) []CardID {
	for _, id := range ids {
		list, _ = removeCard(list, id)
	}
	return list
}

// Every action kind round-trips through the wire format used by the platform.
func TestDecodeEveryAction(t *testing.T) {
	actions := []Action{
		Bank{Card: money(1, 1)},
		PlayProperty{Card: prop("boardwalk"), Set: "set-1", Color: DarkBlue},
		PlayPassGo{Card: act(PassGo, 1)},
		PlayBuilding{Card: act(House, 1), Set: "set-1"},
		PlaySlyDeal{Card: act(SlyDeal, 1), Target: 1, Take: prop("boardwalk")},
		PlayForcedDeal{Card: act(ForcedDeal, 1), Target: 1, Take: prop("boardwalk"), Offer: prop("park-place")},
		PlayDealBreaker{Card: act(DealBreaker, 1), Target: 1, Set: "set-2"},
		PlayDebtCollector{Card: act(DebtCollector, 1), Target: 2},
		PlayBirthday{Card: act(Birthday, 1)},
		PlayRent{Card: rent2(Red, Yellow, 1), Set: "set-3", Doublers: []CardID{act(DoubleRent, 1)}},
		PlayRent{Card: rentAny(1), Set: "set-3", Target: intp(0)},
		Accept{Pending: 4, Step: 2},
		PlayJustSayNo{Pending: 4, Step: 3, Card: act(JustSayNo, 1), Component: "charge"},
		Pay{Pending: 4, Step: 5, Cards: []CardID{money(2, 1)}},
		PlaceReceived{Card: rainbow(1)},
		Rearrange{Sets: []SetLayout{{ID: "set-1", Color: Green, Cards: greenCards, House: act(House, 1)}}, Detached: []CardID{act(Hotel, 1)}},
		EndTurn{Return: []CardID{money(1, 2)}},
	}
	for _, a := range actions {
		t.Run(a.Kind(), func(t *testing.T) {
			raw, err := json.Marshal(a)
			if err != nil {
				t.Fatal(err)
			}
			got, err := DecodeAction(a.Kind(), raw)
			if err != nil || !reflect.DeepEqual(got, a) {
				t.Fatalf("round trip %s: %+v, %v", raw, got, err)
			}
		})
	}
	for _, bad := range []struct{ kind, payload string }{
		{"steal_everything", `{}`},
		{"bank", `{"card":"money-1m-1","extra":true}`},
		{"bank", `not json`},
		{"pay", `{"cards":"money-1m-1"}`},
	} {
		if _, err := DecodeAction(bad.kind, json.RawMessage(bad.payload)); err == nil {
			t.Errorf("DecodeAction(%s, %s) accepted", bad.kind, bad.payload)
		}
	}
}
