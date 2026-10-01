"use client";

// CardSheet: what happens when the active player taps a card that needs a
// choice. Wild properties pick a destination; action and rent cards walk
// through "use or bank", then target player, then the card or set, with a
// step skipped whenever only one choice is possible. The server validates
// every intent.

import { useState } from "react";
import {
  ACTION_FACE,
  cardName,
  Cards,
  COLOR_CSS,
  COLOR_NAMES,
  MatchState,
} from "../../lib/game";
import { Sheet } from "../ui/sheet";
import { destinationPayload, placements, stealable } from "./layout";
import { PlayingCard } from "./playing-card";
import { CardChoice, DestinationTile, PlayerChoice } from "./tiles";

type Command = (kind: string, payload: object) => Promise<boolean>;

type Step =
  | { kind: "choose" }
  | { kind: "target" }
  | { kind: "take"; target: number }
  | { kind: "swap"; target: number }
  | { kind: "breaker"; target: number }
  | { kind: "rent" }
  | { kind: "rentTarget"; set: string }
  | { kind: "build" };

export function CardSheet({
  id,
  state,
  cards,
  mySeat,
  name,
  busy,
  onCommand,
  onClose,
}: {
  id: string;
  state: MatchState;
  cards: Cards;
  mySeat: number;
  name: (seat: number) => string;
  busy: boolean;
  onCommand: Command;
  onClose: () => void;
}) {
  const card = cards[id];
  const pub = state.view.public;
  const me = pub.players[mySeat];
  const opponents = pub.players.filter((p) => p.seat !== mySeat);
  const [step, setStep] = useState<Step>({ kind: "choose" });
  const [doublers, setDoublers] = useState<string[]>([]);
  const [picked, setTake] = useState("");
  const [offered, setGive] = useState("");
  if (!card) return null;

  const run = async (kind: string, payload: object) => {
    if (await onCommand(kind, payload)) onClose();
  };
  const back = (
    <button
      type="button"
      className="sheet-back"
      onClick={() => setStep({ kind: "choose" })}
    >
      ← Back
    </button>
  );

  // Wild properties: choose the color (two-color) or set (multicolor). Only
  // the relevant homes are offered; see placements.
  if (
    card.kind === "property" ||
    card.kind === "wild" ||
    card.kind === "rainbow_wild"
  ) {
    const options = placements(card, id, me.sets, cards);
    return (
      <Sheet
        label={`Play ${cardName(cards, id)}`}
        title={
          card.kind === "wild" ? "Which color?" : "Where should this card go?"
        }
        onClose={onClose}
      >
        <div className="sheet-card">
          <PlayingCard card={card} width={112} />
        </div>
        <div className="dest-grid">
          {options.map((d, i) => (
            <DestinationTile
              key={d.value}
              d={d}
              best={i === 0 && d.kind === "set"}
              disabled={busy}
              onClick={() =>
                void run("play_property", {
                  card: id,
                  ...destinationPayload(d.value),
                })
              }
            />
          ))}
        </div>
      </Sheet>
    );
  }

  const action = card.action ?? "";
  const face = ACTION_FACE[action];
  const isRent = card.kind === "rent" || card.kind === "rent_any";
  const slyTargets = opponents.filter((p) => stealable(p, true).length > 0);
  const swapTargets =
    stealable(me, false).length > 0
      ? opponents.filter((p) => stealable(p, true).length > 0)
      : [];
  const breakerTargets = opponents.filter((p) =>
    p.sets.some((s) => s.complete),
  );
  const buildSets = me.sets.filter(
    (s) =>
      s.complete &&
      s.color !== "railroad" &&
      s.color !== "utility" &&
      (action === "house" ? !s.house : !!s.house && !s.hotel),
  );
  // Sets worth charging: the richest set of each matching color.
  const rentSets = me.sets
    .filter(
      (s) =>
        s.rent > 0 &&
        (card.kind === "rent_any" || (card.colors ?? []).includes(s.color)),
    )
    .sort((a, b) => b.rent - a.rent)
    .filter((s, i, all) => all.findIndex((o) => o.color === s.color) === i);
  const myDoublers = state.view.self.hand.filter(
    (c) => cards[c]?.action === "double_the_rent",
  );
  const canDouble =
    card.kind === "rent" && myDoublers.length > 0 && pub.plays_left >= 2;
  const multiplier = 2 ** doublers.length;
  const charge = (set: string, target?: number) =>
    void run("rent", {
      card: id,
      set,
      ...(doublers.length ? { doublers } : {}),
      ...(target !== undefined ? { target } : {}),
    });
  // Charge a set, asking who pays only when a multicolor Rent has a choice.
  const chargeSet = (set: string) =>
    card.kind === "rent_any"
      ? opponents.length === 1
        ? charge(set, opponents[0].seat)
        : setStep({ kind: "rentTarget", set })
      : charge(set);
  // With a single possibility the action needs no further question.
  const onlySly =
    slyTargets.length === 1 && stealable(slyTargets[0], true).length === 1
      ? { target: slyTargets[0].seat, take: stealable(slyTargets[0], true)[0] }
      : null;
  const onlyBreaker =
    breakerTargets.length === 1 &&
    breakerTargets[0].sets.filter((s) => s.complete).length === 1
      ? {
          target: breakerTargets[0].seat,
          set: breakerTargets[0].sets.find((s) => s.complete)!,
        }
      : null;
  const theirs = (seat: number) => stealable(pub.players[seat], true);
  const mine = stealable(me, false);
  const onlySwap =
    swapTargets.length === 1 &&
    theirs(swapTargets[0].seat).length === 1 &&
    mine.length === 1
      ? {
          target: swapTargets[0].seat,
          take: theirs(swapTargets[0].seat)[0],
          give: mine[0],
        }
      : null;

  // Where "Use" leads, or why it cannot be used.
  const use = (): { label: string; go?: () => void; why?: string } => {
    switch (action) {
      case "pass_go":
        return {
          label: "Draw 2 cards",
          go: () => void run("pass_go", { card: id }),
        };
      case "birthday":
        return {
          label: "Everyone pays you 2M",
          go: () => void run("birthday", { card: id }),
        };
      case "debt_collector":
        return {
          label: "Collect 5M from a player",
          go: () =>
            opponents.length === 1
              ? void run("debt_collector", {
                  card: id,
                  target: opponents[0].seat,
                })
              : setStep({ kind: "target" }),
        };
      case "sly_deal":
        if (onlySly)
          return {
            label: `Steal ${cardName(cards, onlySly.take)} from ${name(onlySly.target)}`,
            go: () =>
              void run("sly_deal", {
                card: id,
                target: onlySly.target,
                take: onlySly.take,
              }),
          };
        return slyTargets.length === 0
          ? {
              label: "Steal a property",
              why: "No one has a property you can take (complete sets are safe).",
            }
          : {
              label: "Steal a property",
              go: () =>
                setStep(
                  slyTargets.length === 1
                    ? { kind: "take", target: slyTargets[0].seat }
                    : { kind: "target" },
                ),
            };
      case "forced_deal":
        if (onlySwap)
          return {
            label: `Swap your ${cardName(cards, onlySwap.give)} for ${name(onlySwap.target)}'s ${cardName(cards, onlySwap.take)}`,
            go: () =>
              void run("forced_deal", {
                card: id,
                target: onlySwap.target,
                take: onlySwap.take,
                offer: onlySwap.give,
              }),
          };
        return swapTargets.length === 0
          ? {
              label: "Swap properties",
              why: "You both need a property outside a complete set.",
            }
          : {
              label: "Swap properties",
              go: () => setStep({ kind: "swap", target: swapTargets[0].seat }),
            };
      case "deal_breaker":
        if (onlyBreaker)
          return {
            label: `Take ${name(onlyBreaker.target)}'s ${COLOR_NAMES[onlyBreaker.set.color]} set`,
            go: () =>
              void run("deal_breaker", {
                card: id,
                target: onlyBreaker.target,
                set: onlyBreaker.set.id,
              }),
          };
        return breakerTargets.length === 0
          ? {
              label: "Steal a complete set",
              why: "No one has a complete set yet.",
            }
          : {
              label: "Steal a complete set",
              go: () =>
                setStep(
                  breakerTargets.length === 1
                    ? { kind: "breaker", target: breakerTargets[0].seat }
                    : { kind: "target" },
                ),
            };
      case "house":
      case "hotel":
        return buildSets.length === 0
          ? {
              label: `Build a ${card.name}`,
              why:
                action === "house"
                  ? "A House needs a complete set (not railroads or utilities)."
                  : "A Hotel needs a complete set that already has a House.",
            }
          : {
              label: `Build a ${card.name}`,
              go: () =>
                buildSets.length === 1
                  ? void run("play_building", {
                      card: id,
                      set: buildSets[0].id,
                    })
                  : setStep({ kind: "build" }),
            };
      case "just_say_no":
        return {
          label: "Just Say No",
          why: "Just Say No is played when someone uses an action on you.",
        };
      case "double_the_rent":
        return {
          label: "Double the Rent",
          why: "Add it when you charge rent with a two-color Rent card.",
        };
    }
    if (isRent) {
      if (rentSets.length === 0)
        return {
          label: "Charge rent",
          why: "You have no matching property set to charge rent on.",
        };
      // One matching set: no question about which set. Only a Double the
      // Rent in hand still needs a step.
      if (rentSets.length === 1 && !canDouble) {
        const only = rentSets[0];
        return {
          label: `Charge ${COLOR_NAMES[only.color]} rent (${only.rent}M)`,
          go: () => chargeSet(only.id),
        };
      }
      return { label: "Charge rent", go: () => setStep({ kind: "rent" }) };
    }
    return { label: "Use" };
  };

  if (step.kind === "choose") {
    const u = use();
    return (
      <Sheet
        label={`Use ${cardName(cards, id)}`}
        title={face?.title ?? cardName(cards, id)}
        onClose={onClose}
        footer={
          <>
            {u.go && (
              <button
                type="button"
                className="btn-gold big"
                disabled={busy}
                onClick={u.go}
              >
                {u.label}
              </button>
            )}
            <button
              type="button"
              className="btn-ghost big"
              disabled={busy}
              onClick={() => void run("bank", { card: id })}
            >
              Bank as {card.value}M
            </button>
          </>
        }
      >
        <div className="sheet-card">
          <PlayingCard card={card} width={128} />
        </div>
        {u.why && <p className="sheet-note">{u.why}</p>}
        <p className="sheet-hint">
          {pub.plays_left} play{pub.plays_left === 1 ? "" : "s"} left this turn
        </p>
      </Sheet>
    );
  }

  if (step.kind === "target") {
    const targets =
      action === "sly_deal"
        ? slyTargets
        : action === "deal_breaker"
          ? breakerTargets
          : opponents;
    return (
      <Sheet label="Choose a player" title="Choose a player" onClose={onClose}>
        {back}
        <div className="player-list">
          {targets.map((p) => (
            <PlayerChoice
              key={p.seat}
              player={p}
              name={name(p.seat)}
              onClick={() => {
                if (action === "debt_collector")
                  void run("debt_collector", { card: id, target: p.seat });
                else if (action === "sly_deal")
                  setStep({ kind: "take", target: p.seat });
                else if (action === "deal_breaker")
                  setStep({ kind: "breaker", target: p.seat });
              }}
            />
          ))}
        </div>
      </Sheet>
    );
  }

  if (step.kind === "take") {
    const target = pub.players[step.target];
    return (
      <Sheet
        label="Choose a property to steal"
        title={`Steal from ${name(step.target)}`}
        onClose={onClose}
      >
        {back}
        <p className="sheet-hint">Tap the card you want.</p>
        <div className="card-grid">
          {stealable(target, true).map((c) => (
            <CardChoice
              key={c}
              id={c}
              cards={cards}
              owner={target}
              disabled={busy}
              onClick={() =>
                void run("sly_deal", { card: id, target: step.target, take: c })
              }
            />
          ))}
        </div>
      </Sheet>
    );
  }

  if (step.kind === "swap") {
    const target = pub.players[step.target];
    const options = theirs(step.target);
    const take = options.includes(picked)
      ? picked
      : options.length === 1
        ? options[0]
        : "";
    const give = mine.length === 1 ? mine[0] : offered;
    return (
      <Sheet
        label="Swap properties"
        title="Swap properties"
        onClose={onClose}
        wide
        footer={
          <button
            type="button"
            className="btn-gold big"
            disabled={busy || !take || !give}
            onClick={() =>
              void run("forced_deal", {
                card: id,
                target: step.target,
                take,
                offer: give,
              })
            }
          >
            Swap
          </button>
        }
      >
        {back}
        {swapTargets.length > 1 && (
          <div className="seg" role="group" aria-label="Player">
            {swapTargets.map((p) => (
              <button
                key={p.seat}
                type="button"
                className={p.seat === step.target ? "on" : ""}
                onClick={() => setStep({ kind: "swap", target: p.seat })}
              >
                {name(p.seat)}
              </button>
            ))}
          </div>
        )}
        <h3 className="sheet-sub">Take from {name(step.target)}</h3>
        <div className="card-grid">
          {stealable(target, true).map((c) => (
            <CardChoice
              key={c}
              id={c}
              cards={cards}
              owner={target}
              selected={take === c}
              onClick={() => setTake(c)}
            />
          ))}
        </div>
        <h3 className="sheet-sub">Give one of yours</h3>
        <div className="card-grid">
          {stealable(me, false).map((c) => (
            <CardChoice
              key={c}
              id={c}
              cards={cards}
              owner={me}
              selected={give === c}
              onClick={() => setGive(c)}
            />
          ))}
        </div>
      </Sheet>
    );
  }

  if (step.kind === "breaker") {
    const target = pub.players[step.target];
    return (
      <Sheet
        label="Choose a complete set"
        title={`Take a set from ${name(step.target)}`}
        onClose={onClose}
      >
        {back}
        <div className="set-choices">
          {target.sets
            .filter((s) => s.complete)
            .map((s) => (
              <button
                key={s.id}
                type="button"
                className="set-choice"
                disabled={busy}
                aria-label={`${COLOR_NAMES[s.color]} set`}
                style={
                  {
                    ["--band" as string]: COLOR_CSS[s.color],
                  } as React.CSSProperties
                }
                onClick={() =>
                  void run("deal_breaker", {
                    card: id,
                    target: step.target,
                    set: s.id,
                  })
                }
              >
                <span className="set-choice-name">
                  {COLOR_NAMES[s.color]}
                  {s.house ? " + House" : ""}
                  {s.hotel ? " + Hotel" : ""}
                </span>
                <span className="set-choice-cards">
                  {[
                    ...s.cards,
                    ...(s.house ? [s.house] : []),
                    ...(s.hotel ? [s.hotel] : []),
                  ].map((c) => (
                    <PlayingCard key={c} card={cards[c]} width={52} />
                  ))}
                </span>
                <span className="set-choice-rent">{s.rent}M rent</span>
              </button>
            ))}
        </div>
      </Sheet>
    );
  }

  if (step.kind === "build") {
    return (
      <Sheet
        label="Build on a set"
        title={`Build a ${card.name}`}
        onClose={onClose}
      >
        {back}
        <div className="set-choices">
          {buildSets.map((s) => (
            <button
              key={s.id}
              type="button"
              className="set-choice"
              disabled={busy}
              style={
                {
                  ["--band" as string]: COLOR_CSS[s.color],
                } as React.CSSProperties
              }
              onClick={() => void run("play_building", { card: id, set: s.id })}
            >
              <span className="set-choice-name">{COLOR_NAMES[s.color]}</span>
              <span className="set-choice-rent">
                {s.rent}M → {s.rent + (action === "house" ? 3 : 4)}M rent
              </span>
            </button>
          ))}
        </div>
      </Sheet>
    );
  }

  // Rent: doublers and the set (only when there is more than one), then a
  // player for multicolor Rent.
  if (step.kind === "rentTarget") {
    return (
      <Sheet
        label="Choose a player"
        title="Who pays the rent?"
        onClose={onClose}
      >
        {back}
        <div className="player-list">
          {opponents.map((p) => (
            <PlayerChoice
              key={p.seat}
              player={p}
              name={name(p.seat)}
              onClick={() => charge(step.set, p.seat)}
            />
          ))}
        </div>
      </Sheet>
    );
  }
  const only = rentSets.length === 1 ? rentSets[0] : null;
  return (
    <Sheet
      label="Charge rent"
      title={only ? `Charge ${COLOR_NAMES[only.color]} rent` : "Charge rent"}
      onClose={onClose}
      footer={
        only && (
          <button
            type="button"
            className="btn-gold big"
            disabled={busy}
            onClick={() => chargeSet(only.id)}
          >
            Charge {only.rent * multiplier}M
          </button>
        )
      }
    >
      {back}
      {canDouble && (
        <div className="doublers">
          {myDoublers.map((d, i) => (
            <label key={d} className="check">
              <input
                type="checkbox"
                checked={doublers.includes(d)}
                disabled={
                  !doublers.includes(d) && pub.plays_left < 2 + doublers.length
                }
                onChange={(e) =>
                  setDoublers(
                    e.target.checked
                      ? [...doublers, d]
                      : doublers.filter((x) => x !== d),
                  )
                }
              />
              Add Double the Rent {myDoublers.length > 1 ? i + 1 : ""} (uses a
              play)
            </label>
          ))}
        </div>
      )}
      <p className="sheet-hint">
        {card.kind === "rent"
          ? "Every other player pays."
          : "One player you choose pays."}
      </p>
      {!only && (
        <div className="set-choices">
          {rentSets.map((s) => (
            <button
              key={s.id}
              type="button"
              className="set-choice"
              disabled={busy}
              aria-label={`${COLOR_NAMES[s.color]} (${s.cards.length} cards, ${s.rent * multiplier}M)`}
              style={
                {
                  ["--band" as string]: COLOR_CSS[s.color],
                } as React.CSSProperties
              }
              onClick={() => chargeSet(s.id)}
            >
              <span className="set-choice-name">{COLOR_NAMES[s.color]}</span>
              <span className="set-choice-meta">{s.cards.length} cards</span>
              <span className="set-choice-rent">
                {s.rent * multiplier}M
                {multiplier > 1 ? ` (${s.rent}M × ${multiplier})` : ""}
              </span>
            </button>
          ))}
        </div>
      )}
    </Sheet>
  );
}
