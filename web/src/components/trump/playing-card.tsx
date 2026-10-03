"use client";

// One playing card. Rank and suit are written as text and symbol together, so
// nothing depends on colour: a red suit also carries its symbol and name, and
// the label a screen reader reads is always the full "Queen of hearts".

import { parseCard, RANK_LABEL, SUIT_SYMBOL, cardLabel } from "../../lib/trump";

export function TrumpCardFace({
  id,
  size = "md",
  dimmed,
  highlight,
}: {
  id: string;
  size?: "sm" | "md" | "lg";
  dimmed?: boolean;
  highlight?: boolean;
}) {
  const card = parseCard(id);
  if (!card) return null;
  const classes = [
    "tcard",
    `tcard-${size}`,
    `suit-${card.suit}`,
    dimmed ? "dimmed" : "",
    highlight ? "playable" : "",
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <span className={classes} aria-label={cardLabel(id)}>
      <span className="tcard-corner" aria-hidden="true">
        <span className="tcard-rank">{RANK_LABEL[card.rank]}</span>
        <span className="tcard-suit">{SUIT_SYMBOL[card.suit]}</span>
      </span>
      <span className="tcard-pip" aria-hidden="true">
        {SUIT_SYMBOL[card.suit]}
      </span>
    </span>
  );
}

// A face-down card, used for opponents' hands.
export function TrumpCardBack({ size = "sm" }: { size?: "sm" | "md" }) {
  return (
    <span className={`tcard tcard-back tcard-${size}`} aria-hidden="true" />
  );
}
