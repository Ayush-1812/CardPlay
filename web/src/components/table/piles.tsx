// Table piles: property sets and the bank are stacked with each card's
// top band peeking out; a pill above shows progress or the bank total.

import {
  Cards,
  COLOR_CSS,
  BAND_TEXT,
  PublicPlayer,
  SET_SIZE,
  PropertySet,
} from "../../lib/game";
import { CardBack, PlayingCard } from "./playing-card";

// Vertical offset between stacked cards, relative to card width.
const overlapFor = (width: number) => Math.round(width * 0.3);

function Stack({
  ids,
  cards,
  width,
  color,
  maxHeight,
}: {
  ids: string[];
  cards: Cards;
  width: number;
  color?: PropertySet["color"];
  maxHeight?: number;
}) {
  const cardH = Math.round(width * 1.5);
  let overlap = overlapFor(width);
  if (maxHeight && ids.length > 1)
    overlap = Math.max(
      6,
      Math.min(overlap, (maxHeight - cardH) / (ids.length - 1)),
    );
  return (
    <div
      className="pile-stack"
      style={{ width, height: cardH + Math.max(0, ids.length - 1) * overlap }}
    >
      {ids.map((id, i) => (
        <div
          key={id}
          className="pile-card"
          style={{
            top: i * overlap,
            transform: `rotate(${i % 2 ? 0.5 : -0.5}deg)`,
          }}
        >
          <PlayingCard card={cards[id]} width={width} activeColor={color} />
        </div>
      ))}
    </div>
  );
}

export function SetPile({
  set,
  cards,
  width,
}: {
  set: PropertySet;
  cards: Cards;
  width: number;
}) {
  const ids = [
    ...set.cards,
    ...(set.house ? [set.house] : []),
    ...(set.hotel ? [set.hotel] : []),
  ];
  const band = COLOR_CSS[set.color];
  return (
    <div
      className={`pile ${set.complete ? "complete" : ""}`}
      style={{ ["--band" as string]: band } as React.CSSProperties}
    >
      <div
        className="pile-pill"
        style={
          set.complete
            ? { background: band, color: BAND_TEXT[set.color] }
            : undefined
        }
      >
        <span
          className="pile-dot"
          style={{ background: set.complete ? "rgba(255,255,255,0.75)" : band }}
        />
        <span>
          {set.cards.length}/{SET_SIZE[set.color]}
        </span>
        {set.complete && <span className="pile-done">✓ SET</span>}
        <span className="pile-rent">{set.rent}M</span>
      </div>
      <Stack ids={ids} cards={cards} width={width} color={set.color} />
    </div>
  );
}

export function BankPile({
  ids,
  cards,
  width,
}: {
  ids: string[];
  cards: Cards;
  width: number;
}) {
  const total = ids.reduce((n, id) => n + (cards[id]?.value ?? 0), 0);
  const sorted = [...ids].sort(
    (a, b) => (cards[b]?.value ?? 0) - (cards[a]?.value ?? 0),
  );
  const cardH = Math.round(width * 1.5);
  return (
    <div className="pile bank">
      <div className="pile-pill gold">
        <span
          className="pile-dot"
          style={{ background: "rgba(255,255,255,0.8)" }}
        />
        <span>BANK ${total}M</span>
      </div>
      {ids.length === 0 ? (
        <div className="pile-empty" style={{ width, height: cardH }}>
          empty
        </div>
      ) : (
        <Stack
          ids={sorted}
          cards={cards}
          width={width}
          maxHeight={cardH + overlapFor(width) * 2}
        />
      )}
    </div>
  );
}

function LoosePile({
  label,
  ids,
  cards,
  width,
}: {
  label: string;
  ids: string[];
  cards: Cards;
  width: number;
}) {
  if (ids.length === 0) return null;
  return (
    <div className="pile loose">
      <div className="pile-pill">
        <span>{label}</span>
      </div>
      <Stack ids={ids} cards={cards} width={width} />
    </div>
  );
}

// Board: a player's bank, sets and loose cards in one horizontal row.
export function Board({
  player,
  cards,
  width,
}: {
  player: PublicPlayer;
  cards: Cards;
  width: number;
}) {
  return (
    <div className="board-row">
      <BankPile ids={player.bank} cards={cards} width={width} />
      {player.sets.map((set) => (
        <SetPile key={set.id} set={set} cards={cards} width={width} />
      ))}
      <LoosePile
        label="UNASSIGNED"
        ids={player.unassigned}
        cards={cards}
        width={width}
      />
      <LoosePile
        label="UNATTACHED"
        ids={player.detached}
        cards={cards}
        width={width}
      />
      <LoosePile
        label="TO PLACE"
        ids={player.incoming}
        cards={cards}
        width={width}
      />
      {player.sets.length === 0 && player.unassigned.length === 0 && (
        <div className="board-empty">No properties yet</div>
      )}
    </div>
  );
}

// DeckPile: a face-down stack whose visible depth follows the count.
export function DeckPile({
  count,
  width = 72,
}: {
  count: number;
  width?: number;
}) {
  const depth =
    count === 0 ? 0 : Math.min(5, Math.max(1, Math.round(count / 18)));
  return (
    <div
      className="deck-pile"
      style={{
        width: width + depth * 2,
        height: Math.round(width * 1.5) + depth * 2,
      }}
    >
      {count === 0 ? (
        <div
          className="pile-empty"
          style={{ width, height: Math.round(width * 1.5) }}
        >
          empty
        </div>
      ) : (
        Array.from({ length: depth }, (_, i) => (
          <div
            key={i}
            className="deck-layer"
            style={{ left: i * 2, top: (depth - 1 - i) * 2 }}
          >
            <CardBack width={width} />
          </div>
        ))
      )}
    </div>
  );
}

export function CenterPile({
  ids,
  cards,
  width = 72,
}: {
  ids: string[];
  cards: Cards;
  width?: number;
}) {
  const top = ids[ids.length - 1];
  return (
    <div
      className="deck-pile"
      style={{ width, height: Math.round(width * 1.5) }}
    >
      {top ? (
        <PlayingCard card={cards[top]} width={width} />
      ) : (
        <div
          className="pile-empty"
          style={{ width, height: Math.round(width * 1.5) }}
        >
          empty
        </div>
      )}
    </div>
  );
}
