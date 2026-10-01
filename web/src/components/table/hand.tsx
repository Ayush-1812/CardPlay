"use client";

// The player's hand. Desktop: an arc fan where the card under the pointer
// lifts and straightens while neighbors slide aside. Compact: an
// overlapping horizontal rail. Every card is a real button, so tap and
// keyboard reach every play.

import { useEffect, useRef, useState } from "react";
import { cardName, Cards } from "../../lib/game";
import { PlayingCard } from "./playing-card";

type Props = {
  ids: string[];
  cards: Cards;
  selected: string | null;
  disabled: boolean;
  layout: "fan" | "rail";
  onSelect: (id: string) => void;
};

export function Hand({
  ids,
  cards,
  selected,
  disabled,
  layout,
  onSelect,
}: Props) {
  const rail = useRef<HTMLDivElement>(null);
  const [railWidth, setRailWidth] = useState(640);
  const [hovered, setHovered] = useState<number | null>(null);

  useEffect(() => {
    const el = rail.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(([entry]) =>
      setRailWidth(entry.contentRect.width || 640),
    );
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const button = (
    id: string,
    width: number,
    style?: React.CSSProperties,
    index?: number,
  ) => (
    <button
      key={id}
      type="button"
      className={`hand-card ${selected === id ? "selected" : ""}`}
      data-card={id}
      aria-pressed={selected === id}
      aria-label={`${cardName(cards, id)}, ${cards[id]?.value ?? 0}M`}
      disabled={disabled}
      style={style}
      // Peek on keyboard focus only; focus restored after a dialog should
      // not leave a card lifted.
      onFocus={(e) =>
        index != null &&
        e.currentTarget.matches(":focus-visible") &&
        setHovered(index)
      }
      onBlur={() => setHovered(null)}
      onClick={() => onSelect(id)}
    >
      <PlayingCard
        card={cards[id]}
        width={width}
        selected={selected === id}
        dimmed={disabled}
      />
    </button>
  );

  if (ids.length === 0) {
    return (
      <div className="hand-empty" ref={rail}>
        Your hand is empty. You draw 5 at the start of your next turn.
      </div>
    );
  }

  if (layout === "rail") {
    return (
      <div className="hand-rail" ref={rail} role="group" aria-label="Your hand">
        {ids.map((id) => button(id, 96))}
      </div>
    );
  }

  // Fan geometry: spacing tightens as the hand grows so it always fits.
  const cardW = 116;
  const cardH = 174;
  const n = ids.length;
  const mid = (n - 1) / 2;
  const room = Math.max(240, railWidth - 24);
  const push = Math.min(cardW * 0.45, room * 0.12);
  const fit = n <= 1 ? 0 : (room - cardW - push * 2) / (n - 1);
  const spread = Math.max(20, Math.min(Math.max(34, 96 - n * 5), fit));
  const lift = 28;
  const focus = hovered ?? (selected ? ids.indexOf(selected) : null);

  const onMove = (e: React.MouseEvent) => {
    const el = rail.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const i = Math.round(
      (e.clientX - r.left - r.width / 2) / Math.max(1, spread) + mid,
    );
    setHovered(Math.max(0, Math.min(n - 1, i)));
  };

  return (
    <div
      className="hand-fan"
      ref={rail}
      role="group"
      aria-label="Your hand"
      style={{ height: cardH + 44 }}
      onMouseMove={onMove}
      onMouseLeave={() => setHovered(null)}
    >
      {ids.map((id, i) => {
        const off = i - mid;
        const tilt = off * 2.2;
        let x = off * spread;
        let y = Math.abs(off) * 2;
        let rotate = tilt;
        let z = i;
        if (focus != null && focus >= 0) {
          if (i === focus) {
            y -= lift;
            rotate = 0;
            z = 100;
          } else {
            const dist = i - focus;
            x +=
              Math.sign(dist) * push * (1 / Math.max(1, Math.abs(dist) * 0.6));
          }
        }
        return button(
          id,
          cardW,
          {
            position: "absolute",
            bottom: 8,
            left: "50%",
            transform: `translateX(calc(-50% + ${x}px)) translateY(${y}px) rotate(${rotate}deg)`,
            zIndex: z,
            filter:
              focus != null && focus >= 0 && focus !== i
                ? "brightness(0.9)"
                : undefined,
          },
          i,
        );
      })}
    </div>
  );
}
