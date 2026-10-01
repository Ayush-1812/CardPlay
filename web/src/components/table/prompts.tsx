"use client";

// Decisions the game asks of a player, shown as large sheets like the
// reference game. Required decisions cannot be dismissed, but they can be
// minimized to look at the table first.

import { useState } from "react";
import {
  actionName,
  cardName,
  Cards,
  COLOR_NAMES,
  MatchState,
} from "../../lib/game";
import { Sheet } from "../ui/sheet";
import {
  destinationPayload,
  moveCard,
  payable,
  placements,
  setOf,
} from "./layout";
import { PlayingCard } from "./playing-card";
import { CardChoice, DestinationTile } from "./tiles";

type Command = (kind: string, payload: object) => Promise<boolean>;

// A required decision that can be tucked away to look at the table.
function Minimizable({
  label,
  children,
}: {
  label: string;
  children: (minimize: () => void) => React.ReactNode;
}) {
  const [hidden, setHidden] = useState(false);
  if (hidden)
    return (
      <button
        type="button"
        className="prompt-pill"
        onClick={() => setHidden(false)}
      >
        {label} ▲
      </button>
    );
  return <>{children(() => setHidden(true))}</>;
}

const CHARGES = new Set(["debt_collector", "birthday", "rent"]);

// ResponsePrompt: an action targets you (or a Just Say No exchange waits on
// you). Charges show the payment picker with one "Pay" that also accepts.
export function ResponsePrompt({
  state,
  cards,
  mySeat,
  name,
  busy,
  onCommand,
}: {
  state: MatchState;
  cards: Cards;
  mySeat: number;
  name: (seat: number) => string;
  busy: boolean;
  onCommand: Command;
}) {
  const pub = state.view.public;
  const p = pub.pending!;
  const t = p.targets[p.current];
  const me = pub.players[mySeat];
  const source = pub.players[p.source];
  const jsns = state.view.self.hand.filter(
    (c) => cards[c]?.action === "just_say_no",
  );
  const chain = t.stage === "chain";
  const charge = CHARGES.has(p.action) && !chain && t.seat === mySeat;
  const activeDoublers = (p.doublers ?? []).filter(
    (d) => t.components.find((c) => c.key === d)?.state !== "blocked",
  ).length;
  const fallback =
    p.action === "debt_collector" ? 5 : p.action === "birthday" ? 2 : 0;
  const owed =
    t.stage === "pay"
      ? (t.owed ?? 0)
      : (p.base ?? fallback) * 2 ** activeDoublers;
  const options = payable(me, cards);
  const total = options.reduce((n, c) => n + (cards[c]?.value ?? 0), 0);
  const short = total <= owed;
  const [chosen, setChosen] = useState<string[]>(() => (short ? options : []));
  const paid = chosen.reduce((n, c) => n + (cards[c]?.value ?? 0), 0);
  const active = t.components.filter((c) => c.state === "active");
  const [component, setComponent] = useState(active[0]?.key ?? "charge");
  const base = { pending: p.id, step: p.step };

  let description: string;
  if (chain)
    description =
      mySeat === p.source
        ? `${name(t.seat)} said Just Say No to your ${actionName(p.action)}. Let it stand, or answer with your own Just Say No.`
        : `${name(p.source)} answered your Just Say No with another. Let it stand, or say no again.`;
  else if (charge)
    description =
      p.action === "rent"
        ? `${name(p.source)} charges you ${owed}M rent.`
        : p.action === "birthday"
          ? `It's ${name(p.source)}'s birthday: pay ${owed}M.`
          : `${name(p.source)} collects a ${owed}M debt from you.`;
  else
    description =
      p.action === "sly_deal"
        ? `${name(p.source)} wants to steal your property.`
        : p.action === "forced_deal"
          ? `${name(p.source)} wants to swap properties with you.`
          : p.action === "deal_breaker"
            ? `${name(p.source)} wants to take your complete set.`
            : `${name(p.source)} played ${actionName(p.action)} on you.`;

  const takenSet =
    p.action === "deal_breaker" && p.set_id
      ? me.sets.find((s) => s.id === p.set_id)
      : undefined;
  const showsCards =
    p.action === "sly_deal" ||
    p.action === "forced_deal" ||
    p.action === "deal_breaker";
  const preview = !chain && t.seat === mySeat && showsCards && (
    <div className="trade-preview">
      {p.action === "forced_deal" && p.offer && (
        <>
          <div>
            <span className="trade-label">You get</span>
            <CardChoice id={p.offer} cards={cards} owner={source} width={84} />
          </div>
          <span className="trade-arrow" aria-hidden="true">
            ⇄
          </span>
        </>
      )}
      {p.take && (p.action === "sly_deal" || p.action === "forced_deal") && (
        <div>
          <span className="trade-label">They take</span>
          <CardChoice id={p.take} cards={cards} owner={me} width={84} />
        </div>
      )}
      {takenSet && (
        <div>
          <span className="trade-label">
            Your {COLOR_NAMES[takenSet.color]} set
          </span>
          <div className="trade-set">
            {[
              ...takenSet.cards,
              ...(takenSet.house ? [takenSet.house] : []),
              ...(takenSet.hotel ? [takenSet.hotel] : []),
            ].map((c) => (
              <PlayingCard key={c} card={cards[c]} width={60} />
            ))}
          </div>
        </div>
      )}
    </div>
  );

  const canSay = jsns.length > 0 && (t.stage === "respond" || chain);
  const sayNo = canSay && (
    <button
      type="button"
      className="btn-danger big"
      disabled={busy}
      onClick={() =>
        void onCommand("just_say_no", {
          ...base,
          card: jsns[0],
          ...(t.stage === "respond" ? { component } : {}),
        })
      }
    >
      Just Say No!
    </button>
  );
  const payButton = charge && (
    <button
      type="button"
      className="btn-gold big"
      disabled={busy || (!short && paid < owed)}
      onClick={() => void onCommand("pay", { ...base, cards: chosen })}
    >
      {options.length === 0
        ? "Nothing to pay with"
        : short
          ? `Give everything (${total}M)`
          : `Pay ${paid}M`}
    </button>
  );
  const acceptButton = !charge && (
    <button
      type="button"
      className="btn-gold big"
      disabled={busy}
      onClick={() => void onCommand("accept", base)}
    >
      {chain ? "Let it stand" : "Accept"}
    </button>
  );

  return (
    <Minimizable label={`Respond: ${actionName(p.action)}`}>
      {(minimize) => (
        <Sheet
          label="Your response"
          title={chain ? "Just Say No!" : "Action!"}
          tone="alert"
          footer={
            <>
              {payButton}
              {acceptButton}
              {sayNo}
            </>
          }
        >
          <button type="button" className="sheet-peek" onClick={minimize}>
            Look at the table first
          </button>
          <div className="prompt-head">
            <PlayingCard card={cards[p.card]} width={64} />
            <p>{description}</p>
          </div>
          {preview}
          {canSay && t.stage === "respond" && active.length > 1 && (
            <label className="game-field">
              Say no to
              <select
                value={component}
                onChange={(e) => setComponent(e.target.value)}
              >
                {active.map((c) => (
                  <option key={c.key} value={c.key}>
                    {c.key === "charge"
                      ? "The whole charge"
                      : `Double the Rent #${(p.doublers ?? []).indexOf(c.key) + 1} (halves it)`}
                  </option>
                ))}
              </select>
            </label>
          )}
          {charge && (
            <>
              <p className="pay-status" aria-live="polite">
                {options.length === 0
                  ? "You have nothing on the table, so you owe nothing."
                  : short
                    ? `Your table is worth ${total}M, less than the debt: everything with value goes.`
                    : `Selected ${paid}M of ${owed}M${paid > owed ? ` (no change given)` : ""}`}
              </p>
              <div className="card-grid">
                {options.map((c) => (
                  <CardChoice
                    key={c}
                    id={c}
                    cards={cards}
                    owner={me}
                    width={76}
                    selected={chosen.includes(c)}
                    disabled={short || busy}
                    onClick={() =>
                      setChosen(
                        chosen.includes(c)
                          ? chosen.filter((x) => x !== c)
                          : [...chosen, c],
                      )
                    }
                  />
                ))}
              </div>
            </>
          )}
        </Sheet>
      )}
    </Minimizable>
  );
}

// PlacementPrompt: place a received property, one tap per card.
export function PlacementPrompt({
  state,
  cards,
  mySeat,
  busy,
  onCommand,
}: {
  state: MatchState;
  cards: Cards;
  mySeat: number;
  busy: boolean;
  onCommand: Command;
}) {
  const me = state.view.public.players[mySeat];
  const id = me.incoming[0];
  const card = cards[id];
  if (!card) return null;
  const options = placements(card, id, me.sets, cards);
  return (
    <Minimizable label="Place your new card">
      {(minimize) => (
        <Sheet label="Place a received card" title="Place your new card">
          <button type="button" className="sheet-peek" onClick={minimize}>
            Look at the table first
          </button>
          <div className="prompt-head">
            <PlayingCard card={card} width={76} />
            <p>
              You received {cardName(cards, id)}
              {me.incoming.length > 1
                ? ` (${me.incoming.length - 1} more to place)`
                : ""}
              . Tap where it goes.
            </p>
          </div>
          <div className="dest-grid">
            {options.map((d, i) => (
              <DestinationTile
                key={d.value}
                d={d}
                best={i === 0 && d.kind === "set"}
                disabled={busy}
                onClick={() =>
                  void onCommand("place_received", {
                    card: id,
                    ...destinationPayload(d.value),
                  })
                }
              />
            ))}
          </div>
        </Sheet>
      )}
    </Minimizable>
  );
}

// DiscardPrompt: more than seven cards at the end of the turn. Required once
// all three plays are spent; optional when the player ends the turn early.
export function DiscardPrompt({
  state,
  cards,
  busy,
  onCommand,
  onClose,
}: {
  state: MatchState;
  cards: Cards;
  busy: boolean;
  onCommand: Command;
  onClose?: () => void;
}) {
  const hand = state.view.self.hand;
  const excess = Math.max(0, hand.length - 7);
  const [chosen, setChosen] = useState<string[]>([]);
  return (
    <Sheet
      label="End turn"
      title="Too many cards"
      onClose={onClose}
      footer={
        <button
          type="button"
          className="btn-gold big"
          disabled={busy || chosen.length !== excess}
          onClick={() => void onCommand("end_turn", { return: chosen })}
        >
          Return {excess} and end turn
        </button>
      }
    >
      <p className="sheet-hint">
        You can keep 7. Choose {excess} card{excess === 1 ? "" : "s"} to put at
        the bottom of the deck (the first you pick is drawn first).
      </p>
      <div className="card-grid">
        {hand.map((c) => (
          <CardChoice
            key={c}
            id={c}
            cards={cards}
            width={76}
            selected={chosen.includes(c)}
            disabled={busy || (!chosen.includes(c) && chosen.length >= excess)}
            onClick={() =>
              setChosen(
                chosen.includes(c)
                  ? chosen.filter((x) => x !== c)
                  : [...chosen, c],
              )
            }
          />
        ))}
      </div>
    </Sheet>
  );
}

// FlipSheet: move a property already on your table, typically flipping a
// two-color wild to its other color. Free, on your own turn.
export function FlipSheet({
  id,
  state,
  cards,
  mySeat,
  busy,
  onCommand,
  onClose,
}: {
  id: string;
  state: MatchState;
  cards: Cards;
  mySeat: number;
  busy: boolean;
  onCommand: Command;
  onClose: () => void;
}) {
  const me = state.view.public.players[mySeat];
  const card = cards[id];
  if (!card) return null;
  const current = setOf(me, id);
  const options = placements(card, id, me.sets, cards, current?.id).filter(
    (d) => !(d.kind === "unassigned" && !current),
  );
  return (
    <Sheet
      label={`Move ${cardName(cards, id)}`}
      title={`Move ${cardName(cards, id)}`}
      onClose={onClose}
    >
      <div className="prompt-head">
        <PlayingCard card={card} width={76} />
        <p>
          {current
            ? `Now in your ${COLOR_NAMES[current.color]} set.`
            : "Not in a set yet."}{" "}
          Moving it is free and does not use a play.
        </p>
      </div>
      {options.length === 0 ? (
        <p className="sheet-note">
          This card has nowhere else to go right now.
        </p>
      ) : (
        <div className="dest-grid">
          {options.map((d) => (
            <DestinationTile
              key={d.value}
              d={d}
              disabled={busy}
              onClick={async () => {
                if (
                  await onCommand("rearrange", moveCard(me, id, d.value, cards))
                )
                  onClose();
              }}
            />
          ))}
        </div>
      )}
    </Sheet>
  );
}
