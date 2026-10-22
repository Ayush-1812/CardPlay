// Types and helpers for the Bluff table. The server decides everything;
// this file only names what it sends and formats it for the screen.

export type BluffPhase = "play" | "challenge" | "over";

export type BluffRank =
  "a" | "2" | "3" | "4" | "5" | "6" | "7" | "8" | "9" | "10" | "j" | "q" | "k";

export type BluffSuit = "spades" | "hearts" | "diamonds" | "clubs";

// Claim is a declaration: how many cards and what they were called. The
// cards themselves are not here, because nobody may see them yet.
export type BluffClaim = {
  id: string;
  seat: number;
  rank: BluffRank;
  count: number;
  rank_name: string;
};

// Reveal is a resolved challenge. These cards were turned face up, so they
// are the one place card identities reach everyone.
export type BluffReveal = {
  play_id: string;
  claimed: BluffRank;
  cards: string[];
  player: number;
  challenger: number;
  truthful: boolean;
  collector: number;
  collected: number;
};

export type BluffPublic = {
  phase: BluffPhase;
  turn: number;
  rank: BluffRank;
  rank_name: string;
  counts: number[];
  pile_count: number;
  last?: BluffClaim;
  accepted: boolean[];
  deadline?: string;
  pending: number;
  winner: number;
  result?: BluffReveal;
  max_cards: number;
};

export type BluffSelf = {
  seat: number;
  hand: string[];
  may_play: boolean;
  may_answer: boolean;
  answer_id?: string;
};

export type BluffMatch = {
  match_id: string;
  room_id: string;
  game_id: string;
  status: string;
  revision: number;
  version: number;
  winner_id?: string | null;
  end_reason?: string | null;
  received_at?: number;
  participants: {
    user_id: string;
    seat: number;
    display_name: string;
    connected: boolean;
    absent_since?: string;
  }[];
  view: { public: BluffPublic; self: BluffSelf };
};

export const SUIT_SYMBOL: Record<BluffSuit, string> = {
  spades: "♠",
  hearts: "♥",
  diamonds: "♦",
  clubs: "♣",
};

export const SUIT_NAME: Record<BluffSuit, string> = {
  spades: "spades",
  hearts: "hearts",
  diamonds: "diamonds",
  clubs: "clubs",
};

const RANK_LABEL: Record<BluffRank, string> = {
  a: "A",
  "2": "2",
  "3": "3",
  "4": "4",
  "5": "5",
  "6": "6",
  "7": "7",
  "8": "8",
  "9": "9",
  "10": "10",
  j: "J",
  q: "Q",
  k: "K",
};

const RANK_WORD: Record<BluffRank, string> = {
  a: "Ace",
  "2": "Two",
  "3": "Three",
  "4": "Four",
  "5": "Five",
  "6": "Six",
  "7": "Seven",
  "8": "Eight",
  "9": "Nine",
  "10": "Ten",
  j: "Jack",
  q: "Queen",
  k: "King",
};

// Order is the cycle the required rank follows, for showing what comes next.
export const RANK_ORDER: BluffRank[] = [
  "a",
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
];

export function parseCard(
  id: string,
): { rank: BluffRank; suit: BluffSuit } | null {
  const cut = id.lastIndexOf("-");
  if (cut <= 0) return null;
  const rank = id.slice(0, cut) as BluffRank;
  const suit = id.slice(cut + 1) as BluffSuit;
  if (!RANK_LABEL[rank] || !SUIT_SYMBOL[suit]) return null;
  return { rank, suit };
}

export function rankLabel(rank: BluffRank): string {
  return RANK_LABEL[rank] ?? rank.toUpperCase();
}

export function rankWord(rank: BluffRank, plural = false): string {
  const word = RANK_WORD[rank] ?? rank;
  if (!plural) return word;
  return rank === "6" ? "Sixes" : `${word}s`;
}

// cardLabel is what a screen reader says for one card.
export function cardLabel(id: string): string {
  const card = parseCard(id);
  if (!card) return id;
  return `${rankWord(card.rank)} of ${SUIT_NAME[card.suit]}`;
}

// sortHand groups a hand by rank in the playing order, so the cards that
// match the current call sit together. It is a convenience only: every card
// stays playable, and nothing here hints at what a player should do.
export function sortHand(hand: string[]): string[] {
  return [...hand].sort((a, b) => {
    const left = parseCard(a);
    const right = parseCard(b);
    if (!left || !right) return a.localeCompare(b);
    const byRank =
      RANK_ORDER.indexOf(left.rank) - RANK_ORDER.indexOf(right.rank);
    return byRank !== 0 ? byRank : left.suit.localeCompare(right.suit);
  });
}

// declaration is the sentence the table is shown after a play.
export function declaration(name: string, claim: BluffClaim): string {
  return `${name} played ${claim.count} ${claim.rank_name}`;
}

// secondsLeft is how long the challenge window has to run, from the
// server's deadline. It is for the countdown only: whether a challenge is
// late is decided by the server, never here.
export function secondsLeft(deadline: string | undefined): number {
  if (!deadline) return 0;
  const left = Date.parse(deadline) - Date.now();
  return left <= 0 ? 0 : Math.ceil(left / 1000);
}
