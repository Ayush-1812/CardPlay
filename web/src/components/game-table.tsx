"use client";

// The live Monopoly Deal table: a felt surface with an opponent rail, the
// selected opponent's board, deck and center pile, and the player's gold
// platter with their board and fanned hand. Below 1024px the same pieces
// stack vertically. The server decides every result; this only builds
// typed intents and shows the player's own projection.

import {
  ReactNode,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import {
  actionName,
  CardInfo,
  cardName,
  Cards,
  Color,
  COLOR_NAMES,
  COLORS,
  describeEvent,
  MatchState,
  PropertySet,
  PublicPlayer,
  SET_SIZE,
} from "../lib/game";
import { Hand } from "./table/hand";
import { Board, CenterPile, DeckPile } from "./table/piles";
import { PlayingCard } from "./table/playing-card";

type Command = (kind: string, payload: object) => Promise<boolean>;

type Props = {
  state: MatchState;
  me: string;
  cards: Cards;
  busy: boolean;
  onCommand: Command;
  onLeave: () => void;
  onVote: (vote: boolean) => void;
  roomName?: string;
  connection?: string;
  // Rendered inside the table: alerts, notices, the tab-takeover banner.
  alerts?: ReactNode;
  // Room chat, shown in a drawer; unread counts messages not yet seen.
  chat?: ReactNode;
  unread?: number;
};

const wideQuery = "(min-width: 1024px)";
function useWide() {
  return useSyncExternalStore(
    (notify) => {
      const mq = window.matchMedia(wideQuery);
      mq.addEventListener("change", notify);
      return () => mq.removeEventListener("change", notify);
    },
    () => window.matchMedia(wideQuery).matches,
    () => true,
  );
}

function initial(name: string) {
  return name.slice(0, 1).toUpperCase() || "?";
}

// Destinations a property card may go to: an own set of a legal color with
// room, a new set of a legal color, or unassigned for a multicolor wild.
function destinations(card: CardInfo, sets: PropertySet[], ownSetID?: string) {
  const colors: Color[] =
    card.kind === "rainbow_wild" ? COLORS : (card.colors ?? []);
  const options: { value: string; label: string }[] = [];
  for (const set of sets) {
    if (
      colors.includes(set.color) &&
      (set.id === ownSetID || set.cards.length < SET_SIZE[set.color])
    ) {
      options.push({
        value: `set:${set.id}`,
        label: `${COLOR_NAMES[set.color]} set (${set.cards.length}/${SET_SIZE[set.color]})`,
      });
    }
  }
  for (const color of colors)
    options.push({
      value: `new:${color}`,
      label: `New ${COLOR_NAMES[color]} set`,
    });
  if (card.kind === "rainbow_wild")
    options.push({ value: "unassigned", label: "Unassigned (no color yet)" });
  return options;
}

function destinationPayload(value: string) {
  if (value.startsWith("set:")) return { set: value.slice(4) };
  if (value.startsWith("new:")) return { color: value.slice(4) };
  return {};
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="game-field">
      {label}
      {children}
    </label>
  );
}

// Modal dialog with focus placed inside and restored on close.
function Dialog({
  label,
  onClose,
  children,
  wide = false,
}: {
  label: string;
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
}) {
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    box.current
      ?.querySelector<HTMLElement>("select, button:not(.dialog-close), input")
      ?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      previous?.focus?.();
    };
  }, [onClose]);
  return (
    <div
      className="dialog-backdrop"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <div
        className={`dialog ${wide ? "wide" : ""}`}
        role="dialog"
        aria-modal="true"
        aria-label={label}
        ref={box}
      >
        <button className="dialog-close" aria-label="Close" onClick={onClose}>
          ×
        </button>
        {children}
      </div>
    </div>
  );
}

// Selectable card thumbnails used for payments and returns.
function CardPicker({
  ids,
  cards,
  chosen,
  onToggle,
  locked = false,
  order = false,
}: {
  ids: string[];
  cards: Cards;
  chosen: string[];
  onToggle: (id: string) => void;
  locked?: boolean;
  order?: boolean;
}) {
  return (
    <div className="card-picker">
      {ids.map((id) => {
        const on = chosen.includes(id);
        return (
          <button
            key={id}
            type="button"
            className={`pick ${on ? "on" : ""}`}
            aria-pressed={on}
            aria-label={`${cardName(cards, id)}, ${cards[id]?.value ?? 0}M`}
            disabled={locked}
            onClick={() => onToggle(id)}
          >
            <PlayingCard card={cards[id]} width={64} selected={on} />
            {order && on && (
              <span className="pick-order">{chosen.indexOf(id) + 1}</span>
            )}
          </button>
        );
      })}
    </div>
  );
}

// CardDialog offers every legal-looking use of one hand card. The server
// validates; this only builds the typed intent.
function CardDialog({
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
  const pub = state.view.public;
  const me = pub.players[mySeat];
  const card = cards[id];
  const opponents = pub.players.filter((p) => p.seat !== mySeat);
  const [dest, setDest] = useState(() =>
    card && (card.kind.includes("wild") || card.kind === "property")
      ? (destinations(card, me.sets)[0]?.value ?? "")
      : "",
  );
  const [target, setTarget] = useState(opponents[0]?.seat ?? 0);
  const [take, setTake] = useState("");
  const [offer, setOffer] = useState("");
  const [setID, setSetID] = useState("");
  const [doublers, setDoublers] = useState<string[]>([]);
  if (!card) return null;
  const targetPlayer = pub.players[target];
  const stealable = (p: PublicPlayer, buildings: boolean) => [
    ...p.sets.filter((s) => !s.complete).flatMap((s) => s.cards),
    ...p.unassigned,
    ...(buildings ? p.detached : []),
  ];
  const bank =
    card.kind !== "property" &&
    card.kind !== "wild" &&
    card.kind !== "rainbow_wild";
  const run = async (kind: string, payload: object) => {
    if (await onCommand(kind, payload)) onClose();
  };
  const targetSelect = (
    <Field label="Player">
      <select
        value={target}
        onChange={(e) => setTarget(Number(e.target.value))}
      >
        {opponents.map((p) => (
          <option key={p.seat} value={p.seat}>
            {name(p.seat)}
          </option>
        ))}
      </select>
    </Field>
  );
  const cardSelect = (
    label: string,
    value: string,
    set: (v: string) => void,
    ids: string[],
  ) => (
    <Field label={label}>
      <select value={value} onChange={(e) => set(e.target.value)}>
        <option value="">Choose…</option>
        {ids.map((c) => (
          <option key={c} value={c}>
            {cardName(cards, c)} ({cards[c]?.value ?? 0}M)
          </option>
        ))}
      </select>
    </Field>
  );
  let play: ReactNode = null;
  if (
    card.kind === "property" ||
    card.kind === "wild" ||
    card.kind === "rainbow_wild"
  ) {
    play = (
      <>
        <Field label="Place in">
          <select value={dest} onChange={(e) => setDest(e.target.value)}>
            {destinations(card, me.sets).map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </Field>
        <button
          className="btn-gold"
          disabled={busy}
          onClick={() =>
            void run("play_property", { card: id, ...destinationPayload(dest) })
          }
        >
          Play property
        </button>
      </>
    );
  } else if (card.kind === "rent" || card.kind === "rent_any") {
    const eligible = me.sets.filter(
      (s) =>
        s.rent > 0 &&
        (card.kind === "rent_any" || card.colors!.includes(s.color)),
    );
    const myDoublers = state.view.self.hand.filter(
      (c) => cards[c]?.action === "double_the_rent",
    );
    play =
      eligible.length === 0 ? (
        <p className="muted">You have no matching set to charge rent on.</p>
      ) : (
        <>
          <Field label="Charge rent on">
            <select value={setID} onChange={(e) => setSetID(e.target.value)}>
              <option value="">Choose a set…</option>
              {eligible.map((s) => (
                <option key={s.id} value={s.id}>
                  {COLOR_NAMES[s.color]} ({s.cards.length} cards, {s.rent}M)
                </option>
              ))}
            </select>
          </Field>
          {card.kind === "rent_any" && targetSelect}
          {card.kind === "rent" &&
            myDoublers.map((d, i) => (
              <label key={d} className="check">
                <input
                  type="checkbox"
                  checked={doublers.includes(d)}
                  disabled={
                    !doublers.includes(d) &&
                    pub.plays_left < 2 + doublers.length
                  }
                  onChange={(e) =>
                    setDoublers(
                      e.target.checked
                        ? [...doublers, d]
                        : doublers.filter((x) => x !== d),
                    )
                  }
                />
                Add Double the Rent {i + 1} (uses a play)
              </label>
            ))}
          <button
            className="btn-gold"
            disabled={busy || !setID}
            onClick={() =>
              void run("rent", {
                card: id,
                set: setID,
                ...(doublers.length ? { doublers } : {}),
                ...(card.kind === "rent_any" ? { target } : {}),
              })
            }
          >
            Charge rent{card.kind === "rent" ? " to everyone" : ""}
          </button>
        </>
      );
  } else if (card.kind === "action") {
    switch (card.action) {
      case "pass_go":
        play = (
          <button
            className="btn-gold"
            disabled={busy}
            onClick={() => void run("pass_go", { card: id })}
          >
            Play Pass Go (draw 2)
          </button>
        );
        break;
      case "birthday":
        play = (
          <button
            className="btn-gold"
            disabled={busy}
            onClick={() => void run("birthday", { card: id })}
          >
            Everyone pays you 2M
          </button>
        );
        break;
      case "debt_collector":
        play = (
          <>
            {targetSelect}
            <button
              className="btn-gold"
              disabled={busy}
              onClick={() => void run("debt_collector", { card: id, target })}
            >
              Collect 5M
            </button>
          </>
        );
        break;
      case "sly_deal":
        play = (
          <>
            {targetSelect}
            {cardSelect("Take", take, setTake, stealable(targetPlayer, true))}
            <button
              className="btn-gold"
              disabled={busy || !take}
              onClick={() => void run("sly_deal", { card: id, target, take })}
            >
              Steal
            </button>
          </>
        );
        break;
      case "forced_deal":
        play = (
          <>
            {targetSelect}
            {cardSelect("Take", take, setTake, stealable(targetPlayer, true))}
            {cardSelect("Give", offer, setOffer, stealable(me, false))}
            <button
              className="btn-gold"
              disabled={busy || !take || !offer}
              onClick={() =>
                void run("forced_deal", { card: id, target, take, offer })
              }
            >
              Swap
            </button>
          </>
        );
        break;
      case "deal_breaker":
        play = (
          <>
            {targetSelect}
            <Field label="Complete set">
              <select value={setID} onChange={(e) => setSetID(e.target.value)}>
                <option value="">Choose…</option>
                {targetPlayer.sets
                  .filter((s) => s.complete)
                  .map((s) => (
                    <option key={s.id} value={s.id}>
                      {COLOR_NAMES[s.color]}
                      {s.house ? " + House" : ""}
                      {s.hotel ? " + Hotel" : ""}
                    </option>
                  ))}
              </select>
            </Field>
            <button
              className="btn-gold"
              disabled={busy || !setID}
              onClick={() =>
                void run("deal_breaker", { card: id, target, set: setID })
              }
            >
              Take the set
            </button>
          </>
        );
        break;
      case "house":
      case "hotel": {
        const eligible = me.sets.filter(
          (s) =>
            s.complete &&
            s.color !== "railroad" &&
            s.color !== "utility" &&
            (card.action === "house" ? !s.house : !!s.house && !s.hotel),
        );
        play =
          eligible.length === 0 ? (
            <p className="muted">
              {card.action === "house"
                ? "A House needs a complete set (not railroads or utilities)."
                : "A Hotel needs a complete set with a House."}
            </p>
          ) : (
            <>
              <Field label="Build on">
                <select
                  value={setID}
                  onChange={(e) => setSetID(e.target.value)}
                >
                  <option value="">Choose…</option>
                  {eligible.map((s) => (
                    <option key={s.id} value={s.id}>
                      {COLOR_NAMES[s.color]}
                    </option>
                  ))}
                </select>
              </Field>
              <button
                className="btn-gold"
                disabled={busy || !setID}
                onClick={() =>
                  void run("play_building", { card: id, set: setID })
                }
              >
                Build {card.name}
              </button>
            </>
          );
        break;
      }
      case "just_say_no":
        play = (
          <p className="muted">
            Just Say No is played when an action targets you.
          </p>
        );
        break;
      case "double_the_rent":
        play = (
          <p className="muted">
            Double the Rent is added when you play a two-color Rent card.
          </p>
        );
        break;
    }
  }
  return (
    <Dialog label={`Use ${cardName(cards, id)}`} onClose={onClose}>
      <div className="card-dialog hand-actions">
        <div className="card-dialog-preview">
          <PlayingCard card={card} width={150} />
        </div>
        <div className="card-dialog-body">
          <strong>{cardName(cards, id)}</strong>
          <p className="muted">
            {pub.plays_left} play{pub.plays_left === 1 ? "" : "s"} left this
            turn
          </p>
          <div className="card-dialog-actions">
            {play}
            {bank && (
              <button
                className="btn-ghost"
                disabled={busy}
                onClick={() => void run("bank", { card: id })}
              >
                Bank as {card.value}M
              </button>
            )}
          </div>
        </div>
      </div>
    </Dialog>
  );
}

function ResponsePanel({
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
  name: (s: number) => string;
  busy: boolean;
  onCommand: Command;
}) {
  const p = state.view.public.pending!;
  const t = p.targets[p.current];
  const jsns = state.view.self.hand.filter(
    (c) => cards[c]?.action === "just_say_no",
  );
  const starting = t.stage === "respond";
  const active = t.components.filter((c) => c.state === "active");
  const [component, setComponent] = useState(active[0]?.key ?? "charge");
  const base = { pending: p.id, step: p.step };
  const describe = (key: string) =>
    key === "charge" ? "the whole charge" : "one Double the Rent";
  const prompt = starting
    ? `${name(p.source)} played ${actionName(p.action)} on you. Accept, or play Just Say No.`
    : mySeat === p.source
      ? `${name(t.seat)} said Just Say No to ${describe(t.chain!.component)}. Let it stand, or counter with your own.`
      : `${name(p.source)} countered your Just Say No. Accept, or counter again.`;
  return (
    <div className="decision" role="region" aria-label="Your response">
      <div className="decision-head">
        <PlayingCard card={cards[p.card]} width={56} />
        <p>{prompt}</p>
      </div>
      <div className="decision-row">
        <button
          className="btn-gold"
          disabled={busy}
          onClick={() => void onCommand("accept", base)}
        >
          {starting ? "Accept" : "Let it stand"}
        </button>
        {jsns.length > 0 && (
          <>
            {starting && active.length > 1 && (
              <Field label="Say no to">
                <select
                  value={component}
                  onChange={(e) => setComponent(e.target.value)}
                >
                  {active.map((c) => (
                    <option key={c.key} value={c.key}>
                      {c.key === "charge"
                        ? "The whole charge"
                        : `Double the Rent #${(p.doublers ?? []).indexOf(c.key) + 1} (halves the charge)`}
                    </option>
                  ))}
                </select>
              </Field>
            )}
            <button
              className="btn-danger"
              disabled={busy}
              onClick={() =>
                void onCommand("just_say_no", {
                  ...base,
                  card: jsns[0],
                  ...(starting ? { component } : {}),
                })
              }
            >
              Just Say No!
            </button>
          </>
        )}
      </div>
    </div>
  );
}

function PaymentPanel({
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
  const p = state.view.public.pending!;
  const t = p.targets[p.current];
  const owed = t.owed ?? 0;
  const me = state.view.public.players[mySeat];
  const eligible = [
    ...me.bank,
    ...me.sets.flatMap((s) => [
      ...s.cards.filter((c) => cards[c]?.kind !== "rainbow_wild"),
      ...(s.house ? [s.house] : []),
      ...(s.hotel ? [s.hotel] : []),
    ]),
    ...me.detached,
  ];
  const total = eligible.reduce((n, c) => n + (cards[c]?.value ?? 0), 0);
  const short = total < owed;
  const [chosen, setChosen] = useState<string[]>(() => (short ? eligible : []));
  const paid = chosen.reduce((n, c) => n + (cards[c]?.value ?? 0), 0);
  const breaks = me.sets.filter(
    (s) => s.complete && s.cards.some((c) => chosen.includes(c)),
  );
  return (
    <div className="decision" role="region" aria-label="Payment">
      <p>
        You owe <strong className="gold">{owed}M</strong>. Choose cards from
        your bank or properties; no change is given.
        {short &&
          " Your table is worth less than the debt, so you give everything with value."}
      </p>
      {eligible.length === 0 && (
        <p className="muted">You have nothing on the table to pay with.</p>
      )}
      <CardPicker
        ids={eligible}
        cards={cards}
        chosen={chosen}
        locked={short}
        onToggle={(c) =>
          setChosen(
            chosen.includes(c) ? chosen.filter((x) => x !== c) : [...chosen, c],
          )
        }
      />
      <div className="decision-row">
        <span aria-live="polite" className="pay-total">
          Selected {paid}M of {owed}M
          {paid > owed ? ` (overpaying ${paid - owed}M)` : ""}
          {breaks.length > 0
            ? ` · breaks your ${breaks.map((s) => COLOR_NAMES[s.color]).join(", ")} set`
            : ""}
        </span>
        <button
          className="btn-gold"
          disabled={busy || (!short && paid < owed)}
          onClick={() =>
            void onCommand("pay", {
              pending: p.id,
              step: p.step,
              cards: chosen,
            })
          }
        >
          Pay {paid}M
        </button>
      </div>
    </div>
  );
}

function PlacementPanel({
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
  const options = card ? destinations(card, me.sets) : [];
  const [dest, setDest] = useState(options[0]?.value ?? "");
  if (!card) return null;
  return (
    <div className="decision" role="region" aria-label="Place a received card">
      <div className="decision-head">
        <PlayingCard card={card} width={64} />
        <p>You received {cardName(cards, id)}. Where should it go?</p>
      </div>
      <div className="decision-row">
        <Field label="Place in">
          <select value={dest} onChange={(e) => setDest(e.target.value)}>
            {options.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </Field>
        <button
          className="btn-gold"
          disabled={busy}
          onClick={() =>
            void onCommand("place_received", {
              card: id,
              ...destinationPayload(dest),
            })
          }
        >
          Place card
        </button>
      </div>
    </div>
  );
}

// Reorganize: choose a destination for every tabled card, then submit the
// whole layout at once (it costs no play).
function ReorganizeDialog({
  state,
  cards,
  mySeat,
  busy,
  onCommand,
  onClose,
}: {
  state: MatchState;
  cards: Cards;
  mySeat: number;
  busy: boolean;
  onCommand: Command;
  onClose: () => void;
}) {
  const me = state.view.public.players[mySeat];
  const props: [string, string][] = [
    ...me.sets.flatMap((s) =>
      s.cards.map((c) => [c, `set:${s.id}`] as [string, string]),
    ),
    ...me.unassigned.map((c) => [c, "unassigned"] as [string, string]),
  ];
  const buildings: [string, string][] = [
    ...me.sets.flatMap(
      (s) =>
        [
          ...(s.house ? [[s.house, s.id]] : []),
          ...(s.hotel ? [[s.hotel, s.id]] : []),
        ] as [string, string][],
    ),
    ...me.detached.map((c) => [c, ""] as [string, string]),
  ];
  const [where, setWhere] = useState<Record<string, string>>(() =>
    Object.fromEntries([...props, ...buildings]),
  );
  const submit = async () => {
    const sets = new Map<
      string,
      {
        id?: string;
        color: string;
        cards: string[];
        house?: string;
        hotel?: string;
      }
    >();
    const unassigned: string[] = [];
    for (const [c] of props) {
      const d = where[c];
      if (d === "unassigned") {
        unassigned.push(c);
        continue;
      }
      if (!sets.has(d)) {
        const existing = d.startsWith("set:")
          ? me.sets.find((s) => s.id === d.slice(4))
          : undefined;
        sets.set(
          d,
          existing
            ? { id: existing.id, color: existing.color, cards: [] }
            : { color: d.slice(4), cards: [] },
        );
      }
      sets.get(d)!.cards.push(c);
    }
    const detached: string[] = [];
    for (const [b] of buildings) {
      const target = [...sets.values()].find((s) => s.id && s.id === where[b]);
      if (!target) {
        detached.push(b);
        continue;
      }
      if (cards[b]?.action === "house") target.house = b;
      else target.hotel = b;
    }
    if (
      await onCommand("rearrange", {
        sets: [...sets.values()],
        unassigned,
        detached,
      })
    )
      onClose();
  };
  return (
    <Dialog label="Reorganize properties" onClose={onClose} wide>
      <h2 className="dialog-title">Reorganize properties</h2>
      <p className="muted">
        Move property cards and buildings. Everything is checked when you save;
        this does not use a play.
      </p>
      <div className="reorg-grid">
        {props.map(([c]) => (
          <div key={c} className="reorg-item">
            <PlayingCard card={cards[c]} width={64} />
            <Field label={cardName(cards, c)}>
              <select
                value={where[c]}
                onChange={(e) => setWhere({ ...where, [c]: e.target.value })}
              >
                {destinations(
                  cards[c],
                  me.sets,
                  where[c].startsWith("set:") ? where[c].slice(4) : undefined,
                ).map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </Field>
          </div>
        ))}
        {buildings.map(([b]) => (
          <div key={b} className="reorg-item">
            <PlayingCard card={cards[b]} width={64} />
            <Field label={cardName(cards, b)}>
              <select
                value={where[b]}
                onChange={(e) => setWhere({ ...where, [b]: e.target.value })}
              >
                <option value="">Unattached</option>
                {me.sets
                  .filter(
                    (s) => s.color !== "railroad" && s.color !== "utility",
                  )
                  .map((s) => (
                    <option key={s.id} value={s.id}>
                      {COLOR_NAMES[s.color]} set
                    </option>
                  ))}
              </select>
            </Field>
          </div>
        ))}
      </div>
      <div className="decision-row">
        <button
          className="btn-gold"
          disabled={busy}
          onClick={() => void submit()}
        >
          Save arrangement
        </button>
        <button className="btn-ghost" onClick={onClose}>
          Cancel
        </button>
      </div>
    </Dialog>
  );
}

function ReturnDialog({
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
  onClose: () => void;
}) {
  const hand = state.view.self.hand;
  const excess = Math.max(0, hand.length - 7);
  const [chosen, setChosen] = useState<string[]>([]);
  return (
    <Dialog label="End turn" onClose={onClose} wide>
      <div className="decision">
        <p>
          You have {hand.length} cards. Choose {excess} to put on the bottom of
          the draw pile (the first chosen is drawn first).
        </p>
        <CardPicker
          ids={hand}
          cards={cards}
          chosen={chosen}
          order
          onToggle={(c) =>
            setChosen(
              chosen.includes(c)
                ? chosen.filter((x) => x !== c)
                : chosen.length < excess
                  ? [...chosen, c]
                  : chosen,
            )
          }
        />
        <div className="decision-row">
          <button
            className="btn-gold"
            disabled={busy || chosen.length !== excess}
            onClick={() =>
              void onCommand("end_turn", { return: chosen }).then(
                (ok) => ok && onClose(),
              )
            }
          >
            Return {excess} and end turn
          </button>
        </div>
      </div>
    </Dialog>
  );
}

function Crest({
  name,
  you = false,
  active = false,
  waiting = false,
  away = false,
  stats,
  onClick,
  selected = false,
  compact = false,
}: {
  name: string;
  you?: boolean;
  active?: boolean;
  waiting?: boolean;
  away?: boolean;
  stats: { sets: number; money: number; hand: number };
  onClick?: () => void;
  selected?: boolean;
  compact?: boolean;
}) {
  const body = (
    <>
      <span className="crest-avatar">
        {initial(name)}
        {waiting && (
          <span className="crest-wait" title="Waiting for this player">
            ⌛
          </span>
        )}
      </span>
      <span className="crest-text">
        <span className="crest-name">
          {name}
          {you && <em> (YOU)</em>}
          {away && <em className="away"> away</em>}
        </span>
        <span className="crest-stats">
          <b className="money">${stats.money}M</b>
          <b>{stats.sets}/3 sets</b>
          <span>{stats.hand}c</span>
        </span>
      </span>
    </>
  );
  const cls = `crest ${active ? "turn" : ""} ${selected ? "selected" : ""} ${away ? "is-away" : ""} ${compact ? "compact" : ""}`;
  return onClick ? (
    <button
      type="button"
      className={cls}
      aria-pressed={selected}
      onClick={onClick}
    >
      {body}
    </button>
  ) : (
    <div className={cls}>{body}</div>
  );
}

// Read-only result: winner banner and every player's final table.
function ResultView({
  state,
  cards,
  name,
}: {
  state: MatchState;
  cards: Cards;
  name: (seat: number) => string;
}) {
  const winner = state.participants.find((p) => p.user_id === state.winner_id);
  const pub = state.view.public;
  return (
    <div className="felt result-view game-table">
      <div className={`game-status result ${state.status}`} role="status">
        <strong>{status(state, name, state.view.self.seat)}</strong>
        {winner && <span className="muted"> Final tables below.</span>}
      </div>
      {pub.players.map((p) => (
        <section
          key={p.seat}
          className="result-board"
          aria-label={`${name(p.seat)}'s final table`}
        >
          <div className="result-head">
            <strong>{name(p.seat)}</strong>
            <span className="muted">
              {p.complete_colors}/3 full sets · bank {p.bank_value}M
            </span>
          </div>
          <div className="board-scroll">
            <Board player={p} cards={cards} width={76} />
          </div>
        </section>
      ))}
    </div>
  );
}

function status(
  state: MatchState,
  name: (seat: number) => string,
  mySeat: number,
): string {
  const pub = state.view.public;
  const pending = pub.pending;
  const target = pending?.targets[pending.current];
  const winner = state.participants.find((p) => p.user_id === state.winner_id);
  const away = state.participants.filter((p) => !p.connected);
  const myTurn = pub.active === mySeat && pub.phase === "play";
  if (state.status === "finished")
    return `${winner ? winner.display_name : "A player"} won with three full sets of different colors.`;
  if (state.status === "abandoned")
    return state.end_reason === "left"
      ? `Match abandoned: ${state.participants.find((p) => p.user_id === state.ended_by)?.display_name ?? "a player"} left. No winner.`
      : state.end_reason === "expired"
        ? "Match expired after everyone was away for 24 hours. No winner."
        : "Match abandoned by vote. No winner.";
  if (state.status === "paused")
    return `Paused: waiting for ${away.map((p) => p.display_name).join(", ")} to reconnect.`;
  if (pub.phase === "play")
    return myTurn
      ? `Your turn: ${pub.plays_left} play${pub.plays_left === 1 ? "" : "s"} left.`
      : `${name(pub.active)}'s turn.`;
  if (pub.phase === "placement") return "Received cards are being placed.";
  if (pending && target) {
    const who =
      target.stage === "chain"
        ? name(target.chain!.waiting)
        : name(target.seat);
    return `${name(pending.source)} played ${actionName(pending.action)}. Waiting for ${who}${target.stage === "pay" ? ` to pay ${target.owed}M` : " to respond"}.`;
  }
  return "";
}

export function GameTable({
  state,
  me,
  cards,
  busy,
  onCommand,
  onLeave,
  onVote,
  roomName,
  connection,
  alerts,
  chat,
  unread = 0,
}: Props) {
  const wide = useWide();
  const pub = state.view.public;
  const mySeat = state.view.self.seat;
  const [selected, setSelected] = useState<string | null>(null);
  const [reorganizing, setReorganizing] = useState(false);
  const [returning, setReturning] = useState(false);
  const [pinnedOpp, setPinnedOpp] = useState<number | null>(null);
  const [chatOpen, setChatOpen] = useState(false);
  const [promptHidden, setPromptHidden] = useState(false);
  const people = new Map(state.participants.map((p) => [p.seat, p]));
  const name = (seat: number) => {
    const p = people.get(seat);
    return p ? (p.user_id === me ? "You" : p.display_name) : `Seat ${seat + 1}`;
  };
  const live = state.status === "playing" || state.status === "paused";
  if (!live) return <ResultView state={state} cards={cards} name={name} />;

  const waiting = pub.waiting_for.includes(mySeat);
  const playing = state.status === "playing";
  const myTurn = pub.active === mySeat && pub.phase === "play" && playing;
  const hand = state.view.self.hand;
  const selectedCard =
    selected && hand.includes(selected) && myTurn && pub.plays_left > 0
      ? selected
      : null;
  const stateKey = `${state.revision}`;
  const myPlayer = pub.players[mySeat];
  const opponents = pub.players.filter((p) => p.seat !== mySeat);
  // Follow whoever the game is waiting on unless the player picked a board.
  const followed =
    opponents.find((p) => pub.waiting_for.includes(p.seat)) ??
    opponents.find((p) => p.seat === pub.active) ??
    opponents[0];
  const shownOpp = opponents.find((p) => p.seat === pinnedOpp) ?? followed;
  const stats = (p: PublicPlayer) => ({
    sets: p.complete_colors,
    money: p.bank_value,
    hand: p.hand_count,
  });
  const tableWidth = wide ? 88 : 76;
  const log = state.events
    .map((e) => ({ e, text: describeEvent(e, cards, name) }))
    .filter((x) => x.text)
    .slice(-12)
    .reverse();

  let prompt: ReactNode = null;
  if (playing && waiting && pub.phase === "response")
    prompt = (
      <ResponsePanel
        key={`r${stateKey}`}
        state={state}
        cards={cards}
        mySeat={mySeat}
        name={name}
        busy={busy}
        onCommand={onCommand}
      />
    );
  if (playing && waiting && pub.phase === "payment")
    prompt = (
      <PaymentPanel
        key={`p${stateKey}`}
        state={state}
        cards={cards}
        mySeat={mySeat}
        busy={busy}
        onCommand={onCommand}
      />
    );
  if (playing && waiting && pub.phase === "placement")
    prompt = (
      <PlacementPanel
        key={`pl${stateKey}`}
        state={state}
        cards={cards}
        mySeat={mySeat}
        busy={busy}
        onCommand={onCommand}
      />
    );
  const vote =
    state.status === "paused" ? (
      <div className="decision vote">
        {state.can_vote_abandon ? (
          <>
            <p>
              A seat has been empty for over 5 minutes. If everyone still here
              agrees, the match ends with no winner. You can also keep waiting.
            </p>
            <button
              className="btn-ghost"
              disabled={busy}
              onClick={() => onVote(true)}
            >
              Vote to abandon
            </button>
          </>
        ) : state.participants.find((p) => p.user_id === me)?.abandon_vote ? (
          <p>
            You voted to abandon.{" "}
            <button className="text-button" onClick={() => onVote(false)}>
              Withdraw vote
            </button>
          </p>
        ) : (
          <p className="muted">
            Seats are held for 5 minutes
            {state.vote_opens_at
              ? ` (until ${new Date(state.vote_opens_at).toLocaleTimeString()})`
              : ""}
            ; then remaining players may vote to abandon.
          </p>
        )}
      </div>
    ) : null;

  const handNode = (
    <Hand
      ids={hand}
      cards={cards}
      selected={selectedCard}
      disabled={!myTurn || pub.plays_left === 0}
      layout={wide ? "fan" : "rail"}
      onSelect={(id) => setSelected(selectedCard === id ? null : id)}
    />
  );

  const endTurn = () => {
    if (hand.length > 7) setReturning(true);
    else void onCommand("end_turn", {});
  };
  const turnPill = (
    <div className={`turn-pill ${myTurn ? "yours" : ""}`}>
      <b>
        {myTurn
          ? "Your turn"
          : pub.active === mySeat
            ? "Waiting on others"
            : `${name(pub.active)}'s turn`}
      </b>
      <small>
        {myTurn
          ? `${3 - pub.plays_left}/3 cards played`
          : `${hand.length} cards in hand`}
      </small>
    </div>
  );
  const turnActions = myTurn && (
    <div className="platter-actions">
      <button className="btn-ghost small" onClick={() => setReorganizing(true)}>
        Reorganize properties
      </button>
      <button className="btn-gold" disabled={busy} onClick={endTurn}>
        End turn
      </button>
    </div>
  );

  return (
    <div
      className={`felt game-table ${wide ? "wide" : "compact"}`}
      role="region"
      aria-label="Match table"
    >
      <header className="table-top">
        <div className="table-brand">
          <span className="brand-mark">♣</span> CardPlay
          {roomName && <span className="table-room">{roomName}</span>}
        </div>
        <div className="table-top-actions">
          {connection && <span className="table-conn">{connection}</span>}
          <button
            className="btn-ghost small chat-toggle"
            aria-expanded={chatOpen}
            onClick={() => setChatOpen(!chatOpen)}
          >
            Chat
            {unread > 0 && (
              <>
                <span className="chat-count" aria-hidden="true">
                  {unread}
                </span>
                <span className="sr-only unread-badge">
                  {unread} new chat message{unread === 1 ? "" : "s"}
                </span>
              </>
            )}
          </button>
          <button
            className="btn-danger small"
            disabled={busy}
            onClick={() => {
              if (
                window.confirm(
                  "Leave the match? It ends for everyone with no winner.",
                )
              )
                onLeave();
            }}
          >
            Leave match
          </button>
        </div>
      </header>

      {alerts && <div className="table-alerts">{alerts}</div>}

      <div
        className={`game-status ${state.status}`}
        role="status"
        aria-live="polite"
      >
        <strong>{status(state, name, mySeat)}</strong>
        <span className="status-meta">
          Turn {pub.turn} · draw pile {pub.draw_count} · center{" "}
          {pub.center_pile.length}
        </span>
      </div>

      <nav className="opp-rail" aria-label="Opponents">
        {opponents.map((p) => (
          <Crest
            key={p.seat}
            name={name(p.seat)}
            active={pub.active === p.seat}
            waiting={pub.waiting_for.includes(p.seat) && pub.phase !== "play"}
            away={!people.get(p.seat)?.connected}
            stats={stats(p)}
            selected={shownOpp?.seat === p.seat}
            compact={!wide}
            onClick={() => setPinnedOpp(p.seat)}
          />
        ))}
      </nav>

      <div className="table-center">
        <section
          className="table-panel opp-panel"
          aria-label={
            shownOpp ? `${name(shownOpp.seat)}'s table` : "Opponent table"
          }
        >
          {shownOpp && (
            <>
              <div className="panel-caption">
                <span>
                  {name(shownOpp.seat)}&apos;s table
                  {pub.active === shownOpp.seat ? " · in play" : ""}
                  {shownOpp.complete_colors === 2 ? " · 1 set from win" : ""}
                </span>
                <span>
                  {shownOpp.sets.filter((s) => s.complete).length} complete ·{" "}
                  {shownOpp.sets.filter((s) => !s.complete).length} partial ·{" "}
                  {shownOpp.hand_count} cards in hand
                </span>
              </div>
              <div className="board-scroll">
                <Board player={shownOpp} cards={cards} width={tableWidth} />
              </div>
            </>
          )}
        </section>
        <aside
          className="table-panel pile-rail"
          aria-label="Draw pile and center pile"
        >
          <div className="panel-caption">
            <span>Table</span>
          </div>
          <div className="piles">
            <div className="pile-block">
              <DeckPile count={pub.draw_count} width={wide ? 72 : 44} />
              <span>
                DECK <b>{pub.draw_count}</b>
              </span>
            </div>
            <div className="pile-block">
              <CenterPile
                ids={pub.center_pile}
                cards={cards}
                width={wide ? 72 : 44}
              />
              <span>
                CENTER <b>{pub.center_pile.length}</b>
              </span>
            </div>
          </div>
          <details className="game-log" open={wide}>
            <summary>Recent actions</summary>
            <ol>
              {log.map(({ e, text }, i) => (
                <li key={`${e.revision}-${i}`}>{text}</li>
              ))}
            </ol>
          </details>
        </aside>
      </div>

      {(prompt || vote) && (
        <div className={`prompt-dock ${promptHidden ? "hidden" : ""}`}>
          <button
            className="prompt-toggle"
            onClick={() => setPromptHidden(!promptHidden)}
          >
            {promptHidden ? "Show decision ▲" : "Look at the table ▼"}
          </button>
          {!promptHidden && (
            <>
              {prompt}
              {vote}
            </>
          )}
        </div>
      )}

      <section className="platter" aria-label="Your area">
        <div className="platter-head">
          <Crest
            name={people.get(mySeat)?.display_name ?? "You"}
            you
            active={pub.active === mySeat}
            stats={stats(myPlayer)}
            compact={!wide}
          />
          {wide && turnPill}
          {wide && turnActions}
        </div>
        <div className="platter-grid">
          <div className="board-scroll my-board">
            <Board player={myPlayer} cards={cards} width={tableWidth} />
          </div>
          {wide && <div className="hand-area my-hand">{handNode}</div>}
        </div>
      </section>

      {!wide && (
        <div className="hand-dock my-hand">
          <div className="dock-bar">
            {turnPill}
            {turnActions}
          </div>
          {handNode}
        </div>
      )}

      {selectedCard && (
        <CardDialog
          key={`${selectedCard}:${stateKey}`}
          id={selectedCard}
          state={state}
          cards={cards}
          mySeat={mySeat}
          name={name}
          busy={busy}
          onCommand={onCommand}
          onClose={() => setSelected(null)}
        />
      )}
      {reorganizing && myTurn && (
        <ReorganizeDialog
          key={`o${stateKey}`}
          state={state}
          cards={cards}
          mySeat={mySeat}
          busy={busy}
          onCommand={onCommand}
          onClose={() => setReorganizing(false)}
        />
      )}
      {returning && myTurn && (
        <ReturnDialog
          key={`e${stateKey}`}
          state={state}
          cards={cards}
          busy={busy}
          onCommand={onCommand}
          onClose={() => setReturning(false)}
        />
      )}

      <aside
        className={`chat-drawer ${chatOpen ? "open" : ""}`}
        aria-label="Room chat"
        inert={!chatOpen}
      >
        <button
          className="dialog-close"
          aria-label="Close chat"
          onClick={() => setChatOpen(false)}
        >
          ×
        </button>
        {chat}
      </aside>
    </div>
  );
}
