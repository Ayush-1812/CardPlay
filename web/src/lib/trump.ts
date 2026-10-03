// Trump's view of a match: the shapes the server projects, and small pure
// helpers the table uses. Card metadata is derived from the card ID, so the
// client needs no catalog request.

import { Participant } from "./game";

export const SUITS = ["spades", "hearts", "diamonds", "clubs"] as const;
export type Suit = (typeof SUITS)[number];

export const SUIT_SYMBOL: Record<Suit, string> = {
  spades: "♠",
  hearts: "♥",
  diamonds: "♦",
  clubs: "♣",
};

// Suit names are always written out beside the symbol: colour alone must
// never carry meaning, and red/black is invisible to many players.
export const SUIT_NAME: Record<Suit, string> = {
  spades: "Spades",
  hearts: "Hearts",
  diamonds: "Diamonds",
  clubs: "Clubs",
};

export const RANK_LABEL: Record<string, string> = {
  a: "A",
  k: "K",
  q: "Q",
  j: "J",
  "10": "10",
  "9": "9",
  "8": "8",
  "7": "7",
  "6": "6",
  "5": "5",
  "4": "4",
  "3": "3",
  "2": "2",
};

const RANK_WORD: Record<string, string> = {
  a: "Ace",
  k: "King",
  q: "Queen",
  j: "Jack",
};

export type TrumpCard = { id: string; suit: Suit; rank: string };

// parseCard reads "hearts-q" into its parts. Card IDs come from the server.
export function parseCard(id: string): TrumpCard | null {
  const cut = id.lastIndexOf("-");
  if (cut < 0) return null;
  const suit = id.slice(0, cut) as Suit;
  const rank = id.slice(cut + 1);
  if (!SUITS.includes(suit) || !RANK_LABEL[rank]) return null;
  return { id, suit, rank };
}

// cardLabel is what a screen reader announces, for example "Queen of hearts".
export function cardLabel(id: string): string {
  const card = parseCard(id);
  if (!card) return id;
  return `${RANK_WORD[card.rank] ?? card.rank} of ${card.suit}`;
}

export type Play = { seat: number; card: string };

export type TrumpTrick = {
  cards: Play[];
  lead: Suit;
  winner: number;
  team: number;
};

export type RoundResult = {
  round: number;
  trump: Suit;
  chooser: number;
  delegated: boolean;
  winner: number;
  tricks: [number, number];
};

export type TrumpSeat = {
  seat: number;
  team: number;
  cards: number;
  ready: boolean;
  user_id: string;
};

export type TrumpStage =
  | "waiting_for_players"
  | "trump_decision"
  | "delegated_trump_decision"
  | "active_trick"
  | "completed_trick"
  | "completed_round"
  | "waiting_for_readiness";

export type TrumpPublic = {
  phase: "trump_selection" | "play" | "round_over";
  stage: TrumpStage;
  round: number;
  trump?: Suit;
  lead_suit?: Suit;
  entitled_team: number;
  decider: number;
  chooser: number;
  delegated: boolean;
  leader: number;
  turn: number;
  trick: Play[];
  last_trick?: TrumpTrick;
  tricks: [number, number];
  rounds_won: [number, number];
  tricks_to_win: number;
  players: TrumpSeat[];
  history: RoundResult[];
};

export type TrumpSelf = {
  seat: number;
  team: number;
  hand: string[];
  legal: string[];
  may_choose: boolean;
  may_delegate: boolean;
};

// TrumpMatch is the match envelope with Trump's projection inside it.
export type TrumpMatch = {
  match_id: string;
  room_id: string;
  game_id: string;
  status: "playing" | "paused" | "finished" | "abandoned";
  end_reason?: string;
  revision: number;
  version: number;
  participants: Participant[];
  can_vote_abandon: boolean;
  received_at?: number;
  view: { public: TrumpPublic; self: TrumpSelf };
};

export const TEAM_NAME = ["Team A", "Team B"];

// seatRing orders the four seats so the viewer is always at the bottom and
// their partner opposite, without changing the real turn order.
export function seatRing(mySeat: number): {
  bottom: number;
  left: number;
  top: number;
  right: number;
} {
  return {
    bottom: mySeat,
    left: (mySeat + 1) % 4,
    top: (mySeat + 2) % 4,
    right: (mySeat + 3) % 4,
  };
}

// whyIllegal explains, in the player's terms, why a card cannot be played
// now. The server decides for real; this only has to be honest about the
// common case so the table never refuses a card silently.
export function whyIllegal(
  id: string,
  pub: TrumpPublic,
  self: TrumpSelf,
  myTurn: boolean,
): string | null {
  if (self.legal.includes(id)) return null;
  if (pub.phase !== "play") return "The round is not in play.";
  if (!myTurn) return "It is not your turn yet.";
  const lead = pub.lead_suit;
  if (lead) {
    const card = parseCard(id);
    const holds = self.hand.some((c) => parseCard(c)?.suit === lead);
    if (holds && card?.suit !== lead) {
      return `You must follow ${SUIT_NAME[lead].toLowerCase()} ${SUIT_SYMBOL[lead]} while you still hold one.`;
    }
  }
  return "That card cannot be played right now.";
}

// nextLeader names who will lead the next trick once the current one is
// complete, which is only known when a trick has just been won.
export function nextLeader(pub: TrumpPublic): number | null {
  if (pub.stage === "completed_trick" && pub.last_trick) {
    return pub.last_trick.winner;
  }
  return null;
}

// The suit of the same colour as another: spades pair with clubs, hearts with
// diamonds. Used to keep a hand readable, not for any rule.
export const SAME_COLOUR: Record<Suit, Suit> = {
  spades: "clubs",
  clubs: "spades",
  hearts: "diamonds",
  diamonds: "hearts",
};

// sortHand arranges cards the way a player wants to read them: the trump suit
// first, then the suit of the same colour, then the other two, and every suit
// from the highest rank down. Before a trump is chosen it is just the four
// suits in their usual order.
export function sortHand(hand: string[], trump?: Suit): string[] {
  const order: Suit[] = trump
    ? [
        trump,
        SAME_COLOUR[trump],
        ...SUITS.filter((s) => s !== trump && s !== SAME_COLOUR[trump]),
      ]
    : [...SUITS];
  const rank = (id: string) => {
    const card = parseCard(id);
    if (!card) return [order.length, 0] as const;
    const suit = order.indexOf(card.suit);
    // Descending by rank inside each suit.
    return [
      suit < 0 ? order.length : suit,
      -RANK_ORDER.indexOf(card.rank),
    ] as const;
  };
  return [...hand].sort((a, b) => {
    const [sa, ra] = rank(a);
    const [sb, rb] = rank(b);
    return sa - sb || ra - rb;
  });
}

// Ranks from lowest to highest, so a higher index is a stronger card.
const RANK_ORDER = [
  "2",
  "3",
  "4",
  "5",
  "6",
  "7",
  "8",
  "9",
  "10",
  "j",
  "q",
  "k",
  "a",
];
