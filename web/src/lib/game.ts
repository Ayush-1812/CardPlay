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

// Property band colors: distinct hues that read on cream card paper.
export const COLOR_CSS: Record<Color, string> = {
  brown: "#6b4a30",
  light_blue: "#6db5d3",
  pink: "#d768a0",
  orange: "#df863e",
  red: "#c63939",
  yellow: "#e4b53a",
  green: "#2e8d5c",
  dark_blue: "#1d3b6f",
  railroad: "#1b1b1b",
  utility: "#6a717d",
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

// Rent in M for 1..size cards, from the verified card faces (rule spec).
export const RENT_LADDER: Record<Color, number[]> = {
  brown: [1, 2],
  light_blue: [1, 2, 3],
  pink: [1, 2, 4],
  orange: [1, 3, 5],
  red: [2, 3, 6],
  yellow: [2, 4, 6],
  green: [2, 4, 7],
  dark_blue: [3, 8],
  railroad: [1, 2, 3, 4],
  utility: [1, 2],
};

// Text color on each property band, chosen for contrast.
export const BAND_TEXT: Record<Color, string> = {
  brown: "#fff",
  light_blue: "#0d2330",
  pink: "#fff",
  orange: "#fff",
  red: "#fff",
  yellow: "#33260a",
  green: "#fff",
  dark_blue: "#fff",
  railroad: "#fff",
  utility: "#fff",
};

// Money card face colors by denomination.
export const MONEY_COLORS: Record<number, string> = {
  1: "#b3b3ad",
  2: "#d3bf72",
  3: "#68ad67",
  4: "#6a98cf",
  5: "#b27848",
  10: "#9f5a2b",
};

// Title, one-line effect and accent color for each action card face.
export const ACTION_FACE: Record<
  string,
  { title: string; effect: string; accent: string }
> = {
  pass_go: { title: "Pass Go", effect: "Draw 2 cards", accent: "#86d2fb" },
  sly_deal: {
    title: "Sly Deal",
    effect: "Steal 1 property",
    accent: "#5cdcd4",
  },
  forced_deal: {
    title: "Forced Deal",
    effect: "Swap properties",
    accent: "#5cdcd4",
  },
  deal_breaker: {
    title: "Deal Breaker",
    effect: "Steal a full set",
    accent: "#fb78dc",
  },
  debt_collector: {
    title: "Debt Collector",
    effect: "One player pays 5M",
    accent: "#78d886",
  },
  birthday: {
    title: "It's My Birthday",
    effect: "Everyone pays 2M",
    accent: "#78d886",
  },
  just_say_no: {
    title: "Just Say No!",
    effect: "Block an action",
    accent: "#fb8787",
  },
  double_the_rent: {
    title: "Double the Rent",
    effect: "2× rent",
    accent: "#fbad6e",
  },
  house: { title: "House", effect: "+3M rent", accent: "#78d886" },
  hotel: { title: "Hotel", effect: "+4M rent", accent: "#fb7878" },
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
  // Seconds until the server plays the awaited player's default move, as of
  // received_at (set by the client when the state arrives).
  turn_seconds_left?: number;
  received_at?: number;
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
  userName: (userID: string) => string = () => "A player",
): string | null {
  const d = e.payload as Record<string, never>;
  const seat = d.seat as number;
  const card = d.card as string;
  switch (e.kind) {
    case "game_started":
      return `Cards dealt. ${name(d.first_seat)} goes first.`;
    case "timed_out":
      return `${userName(d.user_id)} ran out of time, so the table made the default move for them.`;
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
