package monopoly

import (
	"bytes"
	"crypto/rand"
	"slices"
	"testing"
)

// A01: B1 contents list, card-face values and verified ladders.
func TestManifest(t *testing.T) {
	cards := Manifest()
	if len(cards) != 106 {
		t.Fatalf("playable cards = %d, want 106 (110 less 4 Quick Start cards)", len(cards))
	}
	kinds := map[Kind]int{}
	actions := map[ActionType]int{}
	money := map[int]int{}
	colors := map[Color]int{}
	moneyTotal := 0
	for _, c := range cards {
		kinds[c.Kind]++
		if c.Kind == KindAction {
			actions[c.Action]++
		}
		if c.Kind == KindMoney {
			money[c.Value]++
			moneyTotal += c.Value
		}
		if c.Kind == KindProperty {
			colors[c.Colors[0]]++
			if c.Value != colorInfo[c.Colors[0]].Value {
				t.Errorf("%s value %d", c.ID, c.Value)
			}
		}
	}
	for _, tc := range []struct {
		name      string
		got, want int
	}{
		{"money", kinds[KindMoney], 20}, {"ordinary properties", kinds[KindProperty], 28},
		{"two-color wilds", kinds[KindWild], 9}, {"multicolor wilds", kinds[KindRainbowWild], 2},
		{"two-color rent", kinds[KindRent], 10}, {"multicolor rent", kinds[KindRentAny], 3},
		{"actions incl. buildings", kinds[KindAction], 34}, {"money total M", moneyTotal, 57},
		{"1M", money[1], 6}, {"2M", money[2], 5}, {"3M", money[3], 3}, {"4M", money[4], 3}, {"5M", money[5], 2}, {"10M", money[10], 1},
		{"deal breaker", actions[DealBreaker], 2}, {"forced deal", actions[ForcedDeal], 3}, {"sly deal", actions[SlyDeal], 3},
		{"just say no", actions[JustSayNo], 3}, {"debt collector", actions[DebtCollector], 3}, {"birthday", actions[Birthday], 3},
		{"double the rent", actions[DoubleRent], 2}, {"house", actions[House], 3}, {"hotel", actions[Hotel], 2}, {"pass go", actions[PassGo], 10},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
	for color, want := range map[Color]int{Brown: 2, LightBlue: 3, Pink: 3, Orange: 3, Red: 3, Yellow: 3, Green: 3, DarkBlue: 2, Railroad: 4, Utility: 2} {
		if colors[color] != want || colorInfo[color].Size != want || len(colorInfo[color].Rent) != want {
			t.Errorf("%s: %d cards, size %d, ladder %v; want %d", color, colors[color], colorInfo[color].Size, colorInfo[color].Rent, want)
		}
	}
	wildValues := map[CardID]int{
		wild(LightBlue, Brown, 1): 1, wild(LightBlue, Railroad, 1): 4, wild(Pink, Orange, 1): 2, wild(Pink, Orange, 2): 2,
		wild(Red, Yellow, 1): 3, wild(Red, Yellow, 2): 3, wild(DarkBlue, Green, 1): 4, wild(Green, Railroad, 1): 4,
		wild(Railroad, Utility, 1): 2, rainbow(1): 0, rainbow(2): 0,
	}
	for id, v := range wildValues {
		if c, ok := Lookup(id); !ok || c.Value != v {
			t.Errorf("%s value = %d, want %d", id, c.Value, v)
		}
	}
	for id, v := range map[CardID]int{rent2(LightBlue, Brown, 1): 1, rentAny(1): 3, act(DealBreaker, 1): 5, act(JustSayNo, 1): 4, act(Hotel, 1): 4, act(House, 1): 3, act(PassGo, 1): 1} {
		if mustCard(id).Value != v {
			t.Errorf("%s value = %d, want %d", id, mustCard(id).Value, v)
		}
	}
}

// A02, A03: deal five each, fixed seat order, first player draws two.
func TestNewGame(t *testing.T) {
	for _, n := range []int{2, 3, 4, 5} {
		users := []string{"a", "b", "c", "d", "e"}[:n]
		s, events, err := NewGame(users, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.CheckInvariants(); err != nil {
			t.Fatal(err)
		}
		for i, p := range s.Players {
			want := 5
			if i == s.Active {
				want = 7
			}
			if len(p.Hand) != want || p.UserID != users[i] {
				t.Errorf("%d players: seat %d has %d cards, want %d", n, i, len(p.Hand), want)
			}
		}
		if len(s.Draw) != 106-5*n-2 || s.Turn != 1 || s.PlaysUsed != 0 || s.Phase != PhasePlay {
			t.Errorf("%d players: draw %d turn %d", n, len(s.Draw), s.Turn)
		}
		if events[0].Kind != "game_started" {
			t.Errorf("first event %s", events[0].Kind)
		}
	}
	for _, users := range [][]string{{"a"}, {"a", "b", "c", "d", "e", "f"}, {"a", "a"}, {"a", ""}} {
		if _, _, err := NewGame(users, rand.Reader); err == nil {
			t.Errorf("NewGame(%v) accepted", users)
		}
	}
}

// Determinism: the same seed deals the same game; different seeds differ.
func TestSeededDealIsDeterministic(t *testing.T) {
	seed := bytes.Repeat([]byte{42}, SeedSize)
	a, _, _ := NewGame([]string{"x", "y", "z"}, bytes.NewReader(seed))
	b, _, _ := NewGame([]string{"x", "y", "z"}, bytes.NewReader(seed))
	if snapshot(t, a) != snapshot(t, b) {
		t.Fatal("same seed produced different games")
	}
	other, _, _ := NewGame([]string{"x", "y", "z"}, bytes.NewReader(bytes.Repeat([]byte{43}, SeedSize)))
	if slices.Equal(a.Draw, other.Draw) {
		t.Fatal("different seeds produced the same deck order")
	}
	firsts := map[int]bool{}
	for i := range 64 {
		g, _, _ := NewGame([]string{"x", "y", "z"}, bytes.NewReader(bytes.Repeat([]byte{byte(i)}, SeedSize)))
		firsts[g.Active] = true
	}
	if len(firsts) != 3 {
		t.Fatalf("first seat never varied across seeds: %v", firsts)
	}
}

// A03: draw two; draw five from an empty hand; no refill when a hand empties mid-turn.
func TestTurnStartDraw(t *testing.T) {
	cases := []struct {
		name     string
		nextHand []CardID
		want     int
	}{
		{"nonempty hand draws two", []CardID{money(1, 1)}, 3},
		{"empty hand draws five", nil, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t, seat{}, seat{Hand: tc.nextHand})
			s = ok(t, s, 0, EndTurn{})
			if s.Active != 1 || len(s.Players[1].Hand) != tc.want {
				t.Fatalf("seat 1 has %d cards, want %d", len(s.Players[1].Hand), tc.want)
			}
		})
	}
	t.Run("emptying hand mid-turn gives no refill", func(t *testing.T) {
		s := fixture(t, seat{Hand: []CardID{money(1, 1)}}, seat{})
		s = ok(t, s, 0, Bank{Card: money(1, 1)})
		if len(s.Players[0].Hand) != 0 {
			t.Fatalf("hand refilled to %d", len(s.Players[0].Hand))
		}
	})
}

// A06: partial draw continues from the reshuffled center pile; returned
// bottom cards come first; nothing is invented when both piles are empty.
func TestDrawDepletion(t *testing.T) {
	t.Run("partial draw then reshuffle", func(t *testing.T) {
		s := fixture(t, seat{Hand: []CardID{act(PassGo, 1)}}, seat{})
		last := s.Draw[len(s.Draw)-1]
		s.Discard = s.Draw[:len(s.Draw)-1]
		s.Draw = []CardID{last}
		s = ok(t, s, 0, PlayPassGo{Card: act(PassGo, 1)})
		hand := s.Players[0].Hand
		if len(hand) != 2 || hand[0] != last || s.Shuffles != 1 {
			t.Fatalf("hand %v shuffles %d", hand, s.Shuffles)
		}
		if !has(s.Discard, act(PassGo, 1)) || has(s.Draw, act(PassGo, 1)) {
			t.Fatal("the resolving Pass Go was recycled into the new draw pile")
		}
	})
	t.Run("cards returned to the bottom are drawn before recycling", func(t *testing.T) {
		hand := []CardID{money(1, 1), money(1, 2), money(1, 3), money(1, 4), money(1, 5), money(1, 6), money(2, 1), money(2, 2)}
		s := fixture(t, seat{Hand: hand}, seat{Hand: []CardID{money(3, 1)}})
		s.Discard, s.Draw = s.Draw, nil
		s = ok(t, s, 0, EndTurn{Return: []CardID{money(2, 2)}})
		if got := s.Players[1].Hand; len(got) != 3 || got[1] != money(2, 2) {
			t.Fatalf("seat 1 hand %v: the returned card must be drawn first", got)
		}
	})
	t.Run("both piles empty draws what exists", func(t *testing.T) {
		s := fixture(t, seat{Hand: []CardID{act(PassGo, 1)}}, seat{})
		s.Players[1].Hand = s.Draw
		s.Draw = nil
		s = ok(t, s, 0, PlayPassGo{Card: act(PassGo, 1)})
		if len(s.Players[0].Hand) != 0 || len(s.Draw) != 0 {
			t.Fatalf("drew %d from empty piles", len(s.Players[0].Hand))
		}
	})
}

// Replay: an action log from the same seed reproduces every state exactly.
func TestReplayIsDeterministic(t *testing.T) {
	seed := bytes.Repeat([]byte{9}, SeedSize)
	play := func() []string {
		s, _, _ := NewGame([]string{"a", "b"}, bytes.NewReader(seed))
		var states []string
		for range 12 {
			hand := s.Players[s.Active].Hand
			var ret []CardID
			if len(hand) > HandLimit {
				ret = hand[:len(hand)-HandLimit]
			}
			s = ok(t, s, s.Active, EndTurn{Return: ret})
			states = append(states, snapshot(t, s))
		}
		return states
	}
	if !slices.Equal(play(), play()) {
		t.Fatal("replay diverged")
	}
}
