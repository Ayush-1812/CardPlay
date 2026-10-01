"use client";

// Tap targets used by the table's sheets: a card with its set context, a
// destination for a property, and an opponent summary.

import {
  BAND_TEXT,
  Cards,
  COLOR_CSS,
  COLOR_NAMES,
  PublicPlayer,
  SET_SIZE,
} from "../../lib/game";
import { Destination, setOf } from "./layout";
import { PlayingCard } from "./playing-card";

// A card plus the facts that make a choice readable: which set it sits in,
// how full that set is and what it currently earns.
export function CardChoice({
  id,
  cards,
  owner,
  selected = false,
  onClick,
  width = 88,
  disabled = false,
}: {
  id: string;
  cards: Cards;
  owner?: PublicPlayer;
  selected?: boolean;
  onClick?: () => void;
  width?: number;
  disabled?: boolean;
}) {
  const card = cards[id];
  const set = owner ? setOf(owner, id) : undefined;
  const name = card?.name ?? id;
  let caption = card ? `${card.value}M` : "";
  if (set)
    caption = `${COLOR_NAMES[set.color]} ${set.cards.length}/${SET_SIZE[set.color]} · ${set.rent}M rent`;
  else if (owner?.bank.includes(id)) caption = `Bank · ${card?.value ?? 0}M`;
  else if (owner?.unassigned.includes(id)) caption = "Unassigned";
  else if (owner?.detached.includes(id)) caption = "Unattached building";
  const label = `${name}, ${card?.value ?? 0}M${set ? `, ${COLOR_NAMES[set.color]} set` : ""}`;
  return (
    <button
      type="button"
      className={`card-choice ${selected ? "on" : ""}`}
      aria-pressed={onClick ? selected : undefined}
      aria-label={label}
      disabled={disabled || !onClick}
      onClick={onClick}
      style={
        set
          ? ({
              ["--band" as string]: COLOR_CSS[set.color],
            } as React.CSSProperties)
          : undefined
      }
    >
      <PlayingCard card={card} width={width} selected={selected} />
      {caption && <span className="card-choice-caption">{caption}</span>}
    </button>
  );
}

// A place a property card can go, with what it changes.
export function DestinationTile({
  d,
  best = false,
  onClick,
  disabled = false,
}: {
  d: Destination;
  best?: boolean;
  onClick: () => void;
  disabled?: boolean;
}) {
  const band = d.color ? COLOR_CSS[d.color] : undefined;
  return (
    <button
      type="button"
      className={`dest-tile ${d.kind} ${d.completes ? "completes" : ""}`}
      disabled={disabled}
      onClick={onClick}
      style={
        band
          ? ({
              ["--band" as string]: band,
              ["--band-text" as string]: BAND_TEXT[d.color!],
            } as React.CSSProperties)
          : undefined
      }
    >
      {best && <span className="dest-badge">Best</span>}
      <span className="dest-name">{d.label}</span>
      {d.kind === "unassigned" ? (
        <span className="dest-meta">Choose a color later, free</span>
      ) : (
        <>
          <span className="dest-meta">
            {d.kind === "new" ? "Starts at" : `${d.before} →`} {d.after}/
            {d.size} cards
          </span>
          <span className="dest-rent">
            {d.completes ? "Completes the set · " : ""}
            {d.rentAfter}M rent
          </span>
        </>
      )}
    </button>
  );
}

export function PlayerChoice({
  player,
  name,
  onClick,
  detail,
}: {
  player: PublicPlayer;
  name: string;
  onClick: () => void;
  detail?: string;
}) {
  return (
    <button type="button" className="player-choice" onClick={onClick}>
      <span className="player-choice-name">{name}</span>
      <span className="player-choice-money">${player.bank_value}M</span>
      <span className="player-choice-meta">
        {player.hand_count} cards · {player.complete_colors} complete set
        {player.complete_colors === 1 ? "" : "s"}
        {detail ? ` · ${detail}` : ""}
      </span>
      <span className="player-choice-sets" aria-hidden="true">
        {player.sets.map((s) => (
          <span
            key={s.id}
            className={`set-pip ${s.complete ? "full" : ""}`}
            style={{ background: COLOR_CSS[s.color] }}
          />
        ))}
      </span>
    </button>
  );
}
