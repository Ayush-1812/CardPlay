// Types and display helpers for the Monopoly Deal table. The server is
// authoritative: these helpers only describe server-provided state and never
// decide rule outcomes.

export type Color =
  | "brown"
  | "light_blue"
  | "pink"
  | "orange"
  | "red"
  | "yellow"
  | "green"
  | "dark_blue"
  | "railroad"
  | "utility";

export const COLORS: Color[] = [
  "brown",
  "light_blue",
  "pink",
  "orange",
  "red",
  "yellow",
  "green",
  "dark_blue",
  "railroad",
  "utility",
];

export const COLOR_NAMES: Record<Color, string> = {
  brown: "Brown",
  light_blue: "Light blue",
  pink: "Pink",
  orange: "Orange",
  red: "Red",
  yellow: "Yellow",
  green: "Green",
  dark_blue: "Dark blue",
  railroad: "Railroad",
  utility: "Utility",
};

export const COLOR_CSS: Record<Color, string> = {
  brown: "#8a5a3c",
  light_blue: "#9fd8f0",
  pink: "#e27bb8",
  orange: "#f09a3e",
  red: "#e0493f",
  yellow: "#f2d64b",
  green: "#3fa55b",
  dark_blue: "#3452b4",
  railroad: "#2b2b2b",
  utility: "#c9d6c4",
};

export const SET_SIZE: Record<Color, number> = {
  brown: 2,
  light_blue: 3,
  pink: 3,
  orange: 3,
  red: 3,
  yellow: 3,
  green: 3,
  dark_blue: 2,
  railroad: 4,
  utility: 2,
};

export type CardInfo = {
  id: string;
  kind:
    | "money"
    | "property"
    | "wild"
    | "rainbow_wild"
    | "rent"
    | "rent_any"
    | "action";
  name: string;
  value: number;
  colors?: Color[];
  action?: string;
};

export type PropertySet = {
  id: string;
  color: Color;
  cards: string[];
  house?: string;
  hotel?: string;
  complete: boolean;
  rent: number;
};

export type PublicPlayer = {
  seat: number;
  user_id: string;
  hand_count: number;
  bank: string[];
  bank_value: number;
  sets: PropertySet[];
  unassigned: string[];
  detached: string[];
  incoming: string[];
  complete_colors: number;
};

export type Component = {
  key: string;
  state: "active" | "blocked" | "settled";
};

export type Target = {
  seat: number;
  stage: "waiting" | "respond" | "chain" | "pay" | "done";
  components: Component[];
  chain?: { component: string; blocked: boolean; waiting: number };
  owed?: number;
  outcome?: string;
};

export type Pending = {
  id: number;
  step: number;
  action: string;
  card: string;
  source: number;
  doublers?: string[];
  spent?: string[];
  base?: number;
  set_id?: string;
  color?: Color;
  take?: string;
  offer?: string;
  targets: Target[];
  current: number;
};

export type PublicView = {
  phase: "play" | "response" | "payment" | "placement" | "finished";
  turn: number;
  active: number;
  plays_left: number;
  draw_count: number;
  center_pile: string[];
  players: PublicPlayer[];
  pending?: Pending;
  waiting_for: number[];
  winner?: number;
};

export type Participant = {
  user_id: string;
  seat: number;
  handle: string;
  display_name: string;
  connected: boolean;
  absent_since?: string;
  abandon_vote: boolean;
};

export type MatchEvent = {
  revision: number;
  kind: string;
  payload: Record<string, unknown>;
  created_at: string;
};

export type MatchState = {
  match_id: string;
  room_id: string;
  status: "playing" | "paused" | "finished" | "abandoned";
  end_reason?: string;
  winner_id?: string;
  ended_by?: string;
  revision: number;
  version: number;
  participants: Participant[];
  can_vote_abandon: boolean;
  vote_opens_at?: string;
  view: {
    public: PublicView;
    self: { seat: number; hand: string[] };
    legal_actions: string[];
  };
  events: MatchEvent[];
};

export type Cards = Record<string, CardInfo>;

export function cardName(cards: Cards, id: string): string {
  const c = cards[id];
  if (!c) return id;
  if (c.kind === "money") return c.name;
  if (c.kind === "wild")
    return `Wild ${c.colors!.map((x) => COLOR_NAMES[x]).join("/")}`;
  if (c.kind === "rent")
    return `Rent ${c.colors!.map((x) => COLOR_NAMES[x]).join("/")}`;
  return c.name;
}

export function actionName(action: string): string {
  const names: Record<string, string> = {
    rent: "Rent",
    deal_breaker: "Deal Breaker",
    forced_deal: "Forced Deal",
    sly_deal: "Sly Deal",
    debt_collector: "Debt Collector",
    birthday: "It's My Birthday",
  };
  return names[action] ?? action;
}

// describeEvent turns a server event into plain language. It never guesses
// hidden information: private events arrive only to their audience.
export function describeEvent(
  e: MatchEvent,
  cards: Cards,
  name: (seat: number) => string,
): string | null {
  const d = e.payload as Record<string, never>;
  const seat = d.seat as number;
  const card = d.card as string;
  switch (e.kind) {
    case "game_started":
      return `Cards dealt. ${name(d.first_seat)} goes first.`;
    case "turn_started":
      return `${name(seat)}'s turn begins.`;
    case "drew":
      return `${name(seat)} drew ${d.count} card${d.count === 1 ? "" : "s"}.`;
    case "drew_cards":
      return `You drew ${(d.cards as string[]).map((c) => cardName(cards, c)).join(", ") || "nothing"}.`;
    case "banked":
      return `${name(seat)} banked ${cardName(cards, card)}.`;
    case "property_played":
      return `${name(seat)} played ${cardName(cards, card)}.`;
    case "pass_go":
      return `${name(seat)} played Pass Go.`;
    case "building_played":
      return `${name(seat)} built a ${cardName(cards, card)}.`;
    case "action_declared": {
      const targets = (d.targets as number[]).map(name).join(", ");
      const amount = d.base ? ` (${d.base}M)` : "";
      const doubled = (d.doublers as string[] | null)?.length
        ? ` doubled ${(d.doublers as string[]).length}x`
        : "";
      return `${name(seat)} played ${actionName(d.action)}${amount}${doubled} on ${targets}.`;
    }
    case "just_say_no":
      return `${name(seat)} said "Just Say No!"`;
    case "chain_closed":
      return d.blocked
        ? "The Just Say No stands."
        : "The action goes ahead after the counter.";
    case "accepted":
      return `${name(seat)} accepted.`;
    case "payment_due":
      return `${name(seat)} owes ${d.owed}M.`;
    case "paid":
      return `${name(seat)} paid ${d.value}M.`;
    case "transferred":
      return `${actionName(d.action)} resolved against ${name(seat)}.`;
    case "received_placed":
      return `${name(seat)} placed ${cardName(cards, card)}.`;
    case "rearranged":
      return `${name(seat)} reorganized their properties.`;
    case "turn_ended":
      return d.returned
        ? `${name(seat)} ended their turn and returned ${d.returned} card${d.returned === 1 ? "" : "s"} to the draw pile.`
        : `${name(seat)} ended their turn.`;
    case "center_pile_reshuffled":
      return "The center pile was shuffled into a new draw pile.";
    case "game_won":
      return `${name(seat)} collected three full sets and wins!`;
    default:
      return null;
  }
}
