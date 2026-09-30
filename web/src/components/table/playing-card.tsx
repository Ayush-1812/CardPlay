// Card faces for the Monopoly Deal table. Every size derives from one
// width (2:3 aspect); type and padding scale with --fs so a card reads
// the same in the hand, on the table and on the deck.

import type { CSSProperties } from "react";
import {
  ACTION_FACE,
  BAND_TEXT,
  CardInfo,
  Color,
  COLOR_CSS,
  COLOR_NAMES,
  COLORS,
  MONEY_COLORS,
  RENT_LADDER,
  SET_SIZE,
} from "../../lib/game";

type Props = {
  card?: CardInfo;
  width?: number;
  // For a wildcard on the table, the color it currently represents.
  activeColor?: Color;
  selected?: boolean;
  dimmed?: boolean;
};

function frame(width: number): CSSProperties {
  const fs = Math.max(0.7, Math.min(1.4, width / 96));
  return {
    width,
    height: Math.round(width * 1.5),
    ["--fs" as string]: fs,
  } as CSSProperties;
}

function Ladder({
  color,
  compact = false,
}: {
  color: Color;
  compact?: boolean;
}) {
  const ladder = RENT_LADDER[color];
  return (
    <div className={`pc-ladder ${compact ? "compact" : ""}`}>
      <div className="pc-ladder-label">Rent</div>
      <div className="pc-ladder-rows">
        {ladder.map((rent, i) => (
          <div key={i} className="pc-ladder-row">
            <span>
              {i + 1}
              {i + 1 === SET_SIZE[color] ? "★" : ""}
            </span>
            <strong>
              ${rent}
              {compact ? "" : "M"}
            </strong>
          </div>
        ))}
      </div>
      <div
        className="pc-ladder-stripe"
        style={{ background: COLOR_CSS[color] }}
      />
    </div>
  );
}

function PropertyFace({ card }: { card: CardInfo }) {
  const color = card.colors![0];
  return (
    <>
      <div
        className="pc-banner"
        style={{ background: COLOR_CSS[color], color: BAND_TEXT[color] }}
      >
        <span className="pc-chip">${card.value}M</span>
        <span className="pc-banner-name">{card.name}</span>
      </div>
      <div className="pc-paper pc-body">
        <Ladder color={color} />
      </div>
    </>
  );
}

function WildFace({
  card,
  activeColor,
}: {
  card: CardInfo;
  activeColor?: Color;
}) {
  const [a, b] = card.colors!;
  const top = activeColor === b ? b : a;
  const bottom = top === a ? b : a;
  const band = (color: Color, flipped: boolean) => (
    <div
      className={`pc-wild-band ${flipped ? "flipped" : ""}`}
      style={{ background: COLOR_CSS[color], color: BAND_TEXT[color] }}
    >
      <span className="pc-chip">${card.value}M</span>
      <span>{COLOR_NAMES[color]}</span>
    </div>
  );
  return (
    <>
      {band(top, false)}
      <div className="pc-paper pc-wild-body">
        {/* The other color's rents read upside down, as on a printed card. */}
        <div className="pc-flipped">
          <Ladder color={bottom} compact />
        </div>
        <Ladder color={top} compact />
      </div>
      {band(bottom, true)}
    </>
  );
}

function RainbowFace() {
  const stripes = (reverse: boolean) => (
    <div className="pc-stripes">
      {(reverse ? [...COLORS].reverse() : COLORS).map((c) => (
        <span key={c} style={{ background: COLOR_CSS[c] }} />
      ))}
    </div>
  );
  return (
    <>
      {stripes(false)}
      <div className="pc-paper pc-center">
        <div className="pc-title">
          Property
          <br />
          Wild Card
        </div>
        <div className="pc-sub">Any color · no cash value</div>
      </div>
      {stripes(true)}
    </>
  );
}

function MoneyFace({ card }: { card: CardInfo }) {
  const bg = MONEY_COLORS[card.value] ?? MONEY_COLORS[1];
  return (
    <div
      className="pc-money"
      style={{ ["--money" as string]: bg } as CSSProperties}
    >
      <div className="pc-money-head">Money</div>
      <div className="pc-money-value">${card.value}M</div>
    </div>
  );
}

function ActionFace({ card }: { card: CardInfo }) {
  const face = ACTION_FACE[card.action ?? ""] ?? {
    title: card.name,
    effect: "",
    accent: "#e6c476",
  };
  return (
    <div className="pc-paper pc-action">
      <div
        className="pc-action-head"
        style={{ color: face.accent, borderColor: face.accent }}
      >
        <span className="pc-chip dark">${card.value}M</span>
        <span className="pc-action-kind">Action</span>
        <span
          className="pc-chip dark"
          aria-hidden="true"
          style={{ visibility: "hidden" }}
        >
          ${card.value}M
        </span>
      </div>
      <div className="pc-center">
        <div className="pc-title">{face.title}</div>
        {face.effect && <div className="pc-sub">{face.effect}</div>}
      </div>
    </div>
  );
}

function RentFace({ card }: { card: CardInfo }) {
  const any = card.kind === "rent_any";
  const [a, b] = card.colors ?? [];
  const stripe = any
    ? `conic-gradient(${COLORS.map((c, i) => `${COLOR_CSS[c]} ${(i / COLORS.length) * 360}deg ${((i + 1) / COLORS.length) * 360}deg`).join(", ")})`
    : `linear-gradient(135deg, ${COLOR_CSS[a]} 50%, ${COLOR_CSS[b]} 50%)`;
  return (
    <div className="pc-paper pc-rent">
      <div className="pc-rent-stripe" style={{ background: stripe }} />
      <span className="pc-chip dark pc-corner">${card.value}M</span>
      <div className="pc-center">
        <div className="pc-title pc-rent-title">Rent</div>
        <div className="pc-sub">
          {any
            ? "Any color · one player"
            : `${COLOR_NAMES[a]} / ${COLOR_NAMES[b]}`}
        </div>
      </div>
    </div>
  );
}

export function PlayingCard({
  card,
  width = 96,
  activeColor,
  selected,
  dimmed,
}: Props) {
  let face = null;
  switch (card?.kind) {
    case "property":
      face = <PropertyFace card={card} />;
      break;
    case "wild":
      face = <WildFace card={card} activeColor={activeColor} />;
      break;
    case "rainbow_wild":
      face = <RainbowFace />;
      break;
    case "money":
      face = <MoneyFace card={card} />;
      break;
    case "rent":
    case "rent_any":
      face = <RentFace card={card} />;
      break;
    case "action":
      face = <ActionFace card={card} />;
      break;
  }
  return (
    <div
      className={`pc ${selected ? "selected" : ""} ${dimmed ? "dimmed" : ""}`}
      style={frame(width)}
      aria-hidden="true"
    >
      {face}
    </div>
  );
}

export function CardBack({ width = 72 }: { width?: number }) {
  return (
    <div className="pc pc-back" style={frame(width)} aria-hidden="true">
      <div className="pc-back-inner">
        <span className="pc-back-mark">♣</span>
        <span className="pc-back-brand">CardPlay</span>
      </div>
    </div>
  );
}
