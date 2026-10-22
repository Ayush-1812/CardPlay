"use client";

// Card faces for the Bluff table. Suits carry no meaning in this game, so
// a card shows its rank large and its suit as a quiet mark: what players
// are counting is ranks.

import { parseCard, rankLabel, SUIT_SYMBOL, cardLabel } from "../../lib/bluff";

export function BluffCard({
  id,
  size = "md",
  selected = false,
  dimmed = false,
}: {
  id: string;
  size?: "sm" | "md" | "lg";
  selected?: boolean;
  dimmed?: boolean;
}) {
  const card = parseCard(id);
  if (!card) return null;
  const red = card.suit === "hearts" || card.suit === "diamonds";
  return (
    <span
      className={`bcard bcard-${size} ${red ? "red" : "black"} ${selected ? "chosen" : ""} ${dimmed ? "dimmed" : ""}`}
      data-card={id}
      aria-hidden="true"
    >
      <span className="bcard-rank">{rankLabel(card.rank)}</span>
      <span className="bcard-suit">{SUIT_SYMBOL[card.suit]}</span>
    </span>
  );
}

// BluffCardBack is a card nobody may see: the pile, and every card in
// another player's hand.
export function BluffCardBack({ size = "md" }: { size?: "sm" | "md" | "lg" }) {
  return (
    <span className={`bcard bcard-${size} bcard-back`} aria-hidden="true" />
  );
}

// A whole card as a button, for the player's own hand.
export function HandCard({
  id,
  selected,
  disabled,
  onToggle,
}: {
  id: string;
  selected: boolean;
  disabled: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      className={`hand-card ${selected ? "chosen" : ""}`}
      data-card={id}
      aria-pressed={selected}
      aria-label={cardLabel(id)}
      disabled={disabled}
      onClick={onToggle}
    >
      <BluffCard id={id} size="lg" selected={selected} />
    </button>
  );
}
