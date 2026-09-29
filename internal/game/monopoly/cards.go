package monopoly

import "fmt"

// Color is a property group. Values are stable wire identifiers.
type Color string

const (
	Brown     Color = "brown"
	LightBlue Color = "light_blue"
	Pink      Color = "pink"
	Orange    Color = "orange"
	Red       Color = "red"
	Yellow    Color = "yellow"
	Green     Color = "green"
	DarkBlue  Color = "dark_blue"
	Railroad  Color = "railroad"
	Utility   Color = "utility"
)

// Colors lists every property group in a stable order.
var Colors = []Color{Brown, LightBlue, Pink, Orange, Red, Yellow, Green, DarkBlue, Railroad, Utility}

// ColorInfo is the verified card-face data for one property group
// (rules spec: ordinary property table; B1 contents; W card photographs).
type ColorInfo struct {
	Size      int   // cards in a full set
	Rent      []int // rent in M for 1..Size cards
	Value     int   // printed value of an ordinary property card
	Buildable bool  // House/Hotel allowed (not railroads or utilities)
}

var colorInfo = map[Color]ColorInfo{
	Brown:     {2, []int{1, 2}, 1, true},
	LightBlue: {3, []int{1, 2, 3}, 1, true},
	Pink:      {3, []int{1, 2, 4}, 2, true},
	Orange:    {3, []int{1, 3, 5}, 2, true},
	Red:       {3, []int{2, 3, 6}, 3, true},
	Yellow:    {3, []int{2, 4, 6}, 3, true},
	Green:     {3, []int{2, 4, 7}, 4, true},
	DarkBlue:  {2, []int{3, 8}, 4, true},
	Railroad:  {4, []int{1, 2, 3, 4}, 2, false},
	Utility:   {2, []int{1, 2}, 2, false},
}

// Info returns the group data and whether the color exists.
func Info(c Color) (ColorInfo, bool) { i, ok := colorInfo[c]; return i, ok }

// Kind classifies a card.
type Kind string

const (
	KindMoney       Kind = "money"
	KindProperty    Kind = "property"
	KindWild        Kind = "wild"         // two-color property wildcard
	KindRainbowWild Kind = "rainbow_wild" // multicolor property wildcard, no monetary value
	KindRent        Kind = "rent"         // two-color Rent: every opponent pays
	KindRentAny     Kind = "rent_any"     // multicolor Rent: one chosen opponent pays
	KindAction      Kind = "action"
)

// ActionType names an action card's printed effect.
type ActionType string

const (
	DealBreaker   ActionType = "deal_breaker"
	ForcedDeal    ActionType = "forced_deal"
	SlyDeal       ActionType = "sly_deal"
	JustSayNo     ActionType = "just_say_no"
	DebtCollector ActionType = "debt_collector"
	Birthday      ActionType = "birthday"
	DoubleRent    ActionType = "double_the_rent"
	House         ActionType = "house"
	Hotel         ActionType = "hotel"
	PassGo        ActionType = "pass_go"
)

// CardID is a stable identifier for one physical card.
type CardID string

// Card is immutable manifest data. Clients may receive the manifest; it holds
// no hidden game state.
type Card struct {
	ID     CardID     `json:"id"`
	Kind   Kind       `json:"kind"`
	Name   string     `json:"name"`
	Value  int        `json:"value"`            // printed monetary value in M
	Colors []Color    `json:"colors,omitempty"` // property: 1; wild and two-color Rent: 2
	Action ActionType `json:"action,omitempty"`
}

// IsProperty reports whether the card belongs in a property area.
func (c Card) IsProperty() bool {
	return c.Kind == KindProperty || c.Kind == KindWild || c.Kind == KindRainbowWild
}

// IsBuilding reports whether the card is a House or Hotel.
func (c Card) IsBuilding() bool {
	return c.Kind == KindAction && (c.Action == House || c.Action == Hotel)
}

// CanBe reports whether a property card may represent color c.
func (c Card) CanBe(color Color) bool {
	if _, ok := colorInfo[color]; !ok {
		return false
	}
	switch c.Kind {
	case KindRainbowWild:
		return true
	case KindProperty, KindWild:
		for _, own := range c.Colors {
			if own == color {
				return true
			}
		}
	}
	return false
}

var (
	manifest []Card
	cardByID map[CardID]Card
)

// Manifest returns the 106 playable cards in stable order. The four Quick
// Start reference cards are never part of the game (B1 Set Up 1).
func Manifest() []Card { return append([]Card(nil), manifest...) }

// Lookup returns a card by ID.
func Lookup(id CardID) (Card, bool) { c, ok := cardByID[id]; return c, ok }

func mustCard(id CardID) Card {
	c, ok := cardByID[id]
	if !ok {
		panic("unknown card " + string(id))
	}
	return c
}

func init() {
	add := func(c Card) { manifest = append(manifest, c) }
	// Money: B1 contents; 57M total.
	for _, m := range []struct{ value, count int }{{1, 6}, {2, 5}, {3, 3}, {4, 3}, {5, 2}, {10, 1}} {
		for i := 1; i <= m.count; i++ {
			add(Card{ID: CardID(fmt.Sprintf("money-%dm-%d", m.value, i)), Kind: KindMoney, Name: fmt.Sprintf("%dM", m.value), Value: m.value})
		}
	}
	// Ordinary properties: US names verified against card photographs (spec U2).
	properties := []struct {
		color Color
		names []string
		ids   []string
	}{
		{Brown, []string{"Mediterranean Avenue", "Baltic Avenue"}, []string{"mediterranean-avenue", "baltic-avenue"}},
		{LightBlue, []string{"Oriental Avenue", "Vermont Avenue", "Connecticut Avenue"}, []string{"oriental-avenue", "vermont-avenue", "connecticut-avenue"}},
		{Pink, []string{"St. Charles Place", "States Avenue", "Virginia Avenue"}, []string{"st-charles-place", "states-avenue", "virginia-avenue"}},
		{Orange, []string{"St. James Place", "Tennessee Avenue", "New York Avenue"}, []string{"st-james-place", "tennessee-avenue", "new-york-avenue"}},
		{Red, []string{"Kentucky Avenue", "Indiana Avenue", "Illinois Avenue"}, []string{"kentucky-avenue", "indiana-avenue", "illinois-avenue"}},
		{Yellow, []string{"Atlantic Avenue", "Ventnor Avenue", "Marvin Gardens"}, []string{"atlantic-avenue", "ventnor-avenue", "marvin-gardens"}},
		{Green, []string{"Pacific Avenue", "North Carolina Avenue", "Pennsylvania Avenue"}, []string{"pacific-avenue", "north-carolina-avenue", "pennsylvania-avenue"}},
		{DarkBlue, []string{"Park Place", "Boardwalk"}, []string{"park-place", "boardwalk"}},
		{Railroad, []string{"Reading Railroad", "Pennsylvania Railroad", "B. & O. Railroad", "Short Line"}, []string{"reading-railroad", "pennsylvania-railroad", "b-and-o-railroad", "short-line"}},
		{Utility, []string{"Electric Company", "Water Works"}, []string{"electric-company", "water-works"}},
	}
	for _, p := range properties {
		for i, name := range p.names {
			add(Card{ID: CardID("property-" + p.ids[i]), Kind: KindProperty, Name: name, Value: colorInfo[p.color].Value, Colors: []Color{p.color}})
		}
	}
	// Property wildcards: counts B1 contents; values B1 pictures and W photographs.
	wilds := []struct {
		a, b         Color
		value, count int
	}{
		{LightBlue, Brown, 1, 1}, {LightBlue, Railroad, 4, 1}, {Pink, Orange, 2, 2}, {Red, Yellow, 3, 2},
		{DarkBlue, Green, 4, 1}, {Green, Railroad, 4, 1}, {Railroad, Utility, 2, 1},
	}
	for _, w := range wilds {
		for i := 1; i <= w.count; i++ {
			add(Card{ID: CardID(fmt.Sprintf("wild-%s-%s-%d", w.a, w.b, i)), Kind: KindWild, Name: "Property Wild Card", Value: w.value, Colors: []Color{w.a, w.b}})
		}
	}
	for i := 1; i <= 2; i++ {
		add(Card{ID: CardID(fmt.Sprintf("wild-multicolor-%d", i)), Kind: KindRainbowWild, Name: "Multicolor Property Wild Card", Value: 0})
	}
	// Rent: five two-color pairs of two, three multicolor (B1 contents / Rent cards).
	for _, r := range [][2]Color{{LightBlue, Brown}, {Pink, Orange}, {Red, Yellow}, {DarkBlue, Green}, {Railroad, Utility}} {
		for i := 1; i <= 2; i++ {
			add(Card{ID: CardID(fmt.Sprintf("rent-%s-%s-%d", r[0], r[1], i)), Kind: KindRent, Name: "Rent", Value: 1, Colors: []Color{r[0], r[1]}})
		}
	}
	for i := 1; i <= 3; i++ {
		add(Card{ID: CardID(fmt.Sprintf("rent-multicolor-%d", i)), Kind: KindRentAny, Name: "Multicolor Rent", Value: 3})
	}
	// Actions: B1 contents and pictured values.
	actions := []struct {
		action       ActionType
		name         string
		value, count int
	}{
		{DealBreaker, "Deal Breaker", 5, 2}, {ForcedDeal, "Forced Deal", 3, 3}, {SlyDeal, "Sly Deal", 3, 3},
		{JustSayNo, "Just Say No", 4, 3}, {DebtCollector, "Debt Collector", 3, 3}, {Birthday, "It's My Birthday", 2, 3},
		{DoubleRent, "Double the Rent", 1, 2}, {House, "House", 3, 3}, {Hotel, "Hotel", 4, 2}, {PassGo, "Pass Go", 1, 10},
	}
	for _, a := range actions {
		for i := 1; i <= a.count; i++ {
			add(Card{ID: CardID(fmt.Sprintf("action-%s-%d", a.action, i)), Kind: KindAction, Name: a.name, Value: a.value, Action: a.action})
		}
	}
	cardByID = make(map[CardID]Card, len(manifest))
	for _, c := range manifest {
		if _, dup := cardByID[c.ID]; dup {
			panic("duplicate card id " + string(c.ID))
		}
		cardByID[c.ID] = c
	}
}
