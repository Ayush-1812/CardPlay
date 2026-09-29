"use client";

import { useState } from "react";
import {
  actionName,
  CardInfo,
  cardName,
  Cards,
  Color,
  COLOR_CSS,
  COLOR_NAMES,
  COLORS,
  describeEvent,
  MatchState,
  PropertySet,
  PublicPlayer,
  SET_SIZE,
} from "../lib/game";

type Command = (kind: string, payload: object) => Promise<boolean>;

type Props = {
  state: MatchState;
  me: string;
  cards: Cards;
  busy: boolean;
  onCommand: Command;
  onLeave: () => void;
  onVote: (vote: boolean) => void;
};

function Chip({ cards, id }: { cards: Cards; id: string }) {
  const c = cards[id];
  const colors = c?.colors ?? [];
  const band =
    c?.kind === "rainbow_wild"
      ? "linear-gradient(90deg,#e0493f,#f2d64b,#3fa55b,#3452b4)"
      : colors.length === 2
        ? `linear-gradient(90deg,${COLOR_CSS[colors[0]]} 50%,${COLOR_CSS[colors[1]]} 50%)`
        : colors.length === 1
          ? COLOR_CSS[colors[0]]
          : "var(--line)";
  return (
    <span
      className="card-chip"
      title={c ? `${cardName(cards, id)}, ${c.value}M` : id}
    >
      <span
        className="card-band"
        style={{ background: band }}
        aria-hidden="true"
      />
      <span>{cardName(cards, id)}</span>
      {c && c.kind !== "rainbow_wild" && <small>{c.value}M</small>}
    </span>
  );
}

function SetView({ cards, set }: { cards: Cards; set: PropertySet }) {
  return (
    <div className={`prop-set ${set.complete ? "complete" : ""}`}>
      <div className="prop-set-head">
        <span
          className="swatch"
          style={{ background: COLOR_CSS[set.color] }}
          aria-hidden="true"
        />
        <strong>{COLOR_NAMES[set.color]}</strong>
        <small>
          {set.cards.length}/{SET_SIZE[set.color]}
          {set.complete ? " · full" : ""} · rent {set.rent}M
        </small>
      </div>
      {set.cards.map((id) => (
        <Chip key={id} cards={cards} id={id} />
      ))}
      {set.house && <Chip cards={cards} id={set.house} />}
      {set.hotel && <Chip cards={cards} id={set.hotel} />}
    </div>
  );
}

function PlayerArea({
  player,
  name,
  cards,
  active,
  connected,
  self,
}: {
  player: PublicPlayer;
  name: string;
  cards: Cards;
  active: boolean;
  connected: boolean;
  self: boolean;
}) {
  return (
    <section
      className={`player-area ${active ? "active" : ""}`}
      aria-label={`${name}'s table`}
    >
      <div className="player-head">
        <strong>
          {name}
          {self ? " (you)" : ""}
        </strong>
        <span className={`pill ${connected ? "ready" : ""}`}>
          {connected ? "Online" : "Away"}
        </span>
        {active && <span className="pill ready">Turn</span>}
        <small>
          {player.hand_count} in hand · bank {player.bank_value}M ·{" "}
          {player.complete_colors}/3 full sets
        </small>
      </div>
      <div className="zone">
        <small className="zone-label">Bank</small>
        {player.bank.length === 0 ? (
          <small className="muted">Empty</small>
        ) : (
          player.bank.map((id) => <Chip key={id} cards={cards} id={id} />)
        )}
      </div>
      <div className="sets">
        {player.sets.map((set) => (
          <SetView key={set.id} cards={cards} set={set} />
        ))}
        {player.sets.length === 0 && (
          <small className="muted">No properties yet</small>
        )}
      </div>
      {(player.unassigned.length > 0 ||
        player.detached.length > 0 ||
        player.incoming.length > 0) && (
        <div className="zone">
          {player.unassigned.map((id) => (
            <span key={id}>
              <small className="zone-label">Unassigned</small>{" "}
              <Chip cards={cards} id={id} />
            </span>
          ))}
          {player.detached.map((id) => (
            <span key={id}>
              <small className="zone-label">Unattached</small>{" "}
              <Chip cards={cards} id={id} />
            </span>
          ))}
          {player.incoming.map((id) => (
            <span key={id}>
              <small className="zone-label">To place</small>{" "}
              <Chip cards={cards} id={id} />
            </span>
          ))}
        </div>
      )}
    </section>
  );
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

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <label className="game-field">
      {label}
      {children}
    </label>
  );
}

// HandActions offers every legal-looking use of one hand card. The server
// validates; this only builds the typed intent.
function HandActions({
  id,
  state,
  cards,
  mySeat,
  name,
  busy,
  onCommand,
}: {
  id: string;
  state: MatchState;
  cards: Cards;
  mySeat: number;
  name: (seat: number) => string;
  busy: boolean;
  onCommand: Command;
}) {
  const pub = state.view.public;
  const me = pub.players[mySeat];
  const card = cards[id];
  const opponents = pub.players.filter((p) => p.seat !== mySeat);
  const [dest, setDest] = useState(() =>
    card?.kind.includes("wild") || card?.kind === "property"
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
  const run = (kind: string, payload: object) => void onCommand(kind, payload);
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
  let play: React.ReactNode = null;
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
          className="primary"
          disabled={busy}
          onClick={() =>
            run("play_property", { card: id, ...destinationPayload(dest) })
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
            className="primary"
            disabled={busy || !setID}
            onClick={() =>
              run("rent", {
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
            className="primary"
            disabled={busy}
            onClick={() => run("pass_go", { card: id })}
          >
            Play Pass Go (draw 2)
          </button>
        );
        break;
      case "birthday":
        play = (
          <button
            className="primary"
            disabled={busy}
            onClick={() => run("birthday", { card: id })}
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
              className="primary"
              disabled={busy}
              onClick={() => run("debt_collector", { card: id, target })}
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
              className="primary"
              disabled={busy || !take}
              onClick={() => run("sly_deal", { card: id, target, take })}
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
              className="primary"
              disabled={busy || !take || !offer}
              onClick={() =>
                run("forced_deal", { card: id, target, take, offer })
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
              className="primary"
              disabled={busy || !setID}
              onClick={() =>
                run("deal_breaker", { card: id, target, set: setID })
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
                className="primary"
                disabled={busy || !setID}
                onClick={() => run("play_building", { card: id, set: setID })}
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
    <div
      className="hand-actions"
      role="group"
      aria-label={`Use ${cardName(cards, id)}`}
    >
      <strong>{cardName(cards, id)}</strong>
      {play}
      {bank && (
        <button
          className="secondary"
          disabled={busy}
          onClick={() => run("bank", { card: id })}
        >
          Bank as {card.value}M
        </button>
      )}
    </div>
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
  let prompt: string;
  if (starting) {
    prompt = `${name(p.source)} played ${actionName(p.action)} on you. Accept, or play Just Say No.`;
  } else {
    prompt =
      mySeat === p.source
        ? `${name(t.seat)} said Just Say No to ${describe(t.chain!.component)}. Let it stand, or counter with your own.`
        : `${name(p.source)} countered your Just Say No. Accept, or counter again.`;
  }
  return (
    <div className="decision" role="region" aria-label="Your response">
      <p>{prompt}</p>
      <div className="decision-row">
        <button
          className="primary"
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
              className="secondary"
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
  const short = total < (t.owed ?? 0);
  const [chosen, setChosen] = useState<string[]>(() => (short ? eligible : []));
  const paid = chosen.reduce((n, c) => n + (cards[c]?.value ?? 0), 0);
  const breaks = me.sets.filter(
    (s) => s.complete && s.cards.some((c) => chosen.includes(c)),
  );
  return (
    <div className="decision" role="region" aria-label="Payment">
      <p>
        You owe <strong>{t.owed}M</strong>. Choose cards from your bank or
        properties; no change is given.
        {short &&
          " Your table is worth less than the debt, so you give everything with value."}
      </p>
      {eligible.length === 0 && (
        <p className="muted">You have nothing on the table to pay with.</p>
      )}
      <div className="pay-grid">
        {eligible.map((c) => (
          <label key={c} className="check">
            <input
              type="checkbox"
              checked={chosen.includes(c)}
              disabled={short}
              onChange={(e) =>
                setChosen(
                  e.target.checked
                    ? [...chosen, c]
                    : chosen.filter((x) => x !== c),
                )
              }
            />
            <Chip cards={cards} id={c} />
          </label>
        ))}
      </div>
      <p aria-live="polite">
        Selected {paid}M of {t.owed}M
        {paid > (t.owed ?? 0) ? ` (overpaying ${paid - (t.owed ?? 0)}M)` : ""}
        {breaks.length > 0
          ? ` · breaks your ${breaks.map((s) => COLOR_NAMES[s.color]).join(", ")} set`
          : ""}
      </p>
      <button
        className="primary"
        disabled={busy || (!short && paid < (t.owed ?? 0))}
        onClick={() =>
          void onCommand("pay", { pending: p.id, step: p.step, cards: chosen })
        }
      >
        Pay {paid}M
      </button>
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
      <p>
        You received <Chip cards={cards} id={id} />. Where should it go?
      </p>
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
          className="primary"
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
function Reorganize({
  state,
  cards,
  mySeat,
  busy,
  onCommand,
  onDone,
}: {
  state: MatchState;
  cards: Cards;
  mySeat: number;
  busy: boolean;
  onCommand: Command;
  onDone: () => void;
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
      onDone();
  };
  return (
    <div className="decision" role="region" aria-label="Reorganize properties">
      <p>
        Move property cards and buildings. Everything is checked when you save;
        this does not use a play.
      </p>
      {props.map(([c]) => (
        <Field key={c} label={cardName(cards, c)}>
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
      ))}
      {buildings.map(([b]) => (
        <Field key={b} label={cardName(cards, b)}>
          <select
            value={where[b]}
            onChange={(e) => setWhere({ ...where, [b]: e.target.value })}
          >
            <option value="">Unattached</option>
            {me.sets
              .filter((s) => s.color !== "railroad" && s.color !== "utility")
              .map((s) => (
                <option key={s.id} value={s.id}>
                  {COLOR_NAMES[s.color]} set
                </option>
              ))}
          </select>
        </Field>
      ))}
      <div className="decision-row">
        <button
          className="primary"
          disabled={busy}
          onClick={() => void submit()}
        >
          Save arrangement
        </button>
        <button className="text-button" onClick={onDone}>
          Cancel
        </button>
      </div>
    </div>
  );
}

function EndTurn({
  state,
  cards,
  busy,
  onCommand,
}: {
  state: MatchState;
  cards: Cards;
  busy: boolean;
  onCommand: Command;
}) {
  const hand = state.view.self.hand;
  const excess = Math.max(0, hand.length - 7);
  const [chosen, setChosen] = useState<string[]>([]);
  if (excess === 0) {
    return (
      <button
        className="secondary"
        disabled={busy}
        onClick={() => void onCommand("end_turn", {})}
      >
        End turn
      </button>
    );
  }
  return (
    <div className="decision" role="region" aria-label="End turn">
      <p>
        You have {hand.length} cards. Choose {excess} to put on the bottom of
        the draw pile (the first chosen is drawn first).
      </p>
      <div className="pay-grid">
        {hand.map((c) => (
          <label key={c} className="check">
            <input
              type="checkbox"
              checked={chosen.includes(c)}
              disabled={!chosen.includes(c) && chosen.length >= excess}
              onChange={(e) =>
                setChosen(
                  e.target.checked
                    ? [...chosen, c]
                    : chosen.filter((x) => x !== c),
                )
              }
            />
            <Chip cards={cards} id={c} />
          </label>
        ))}
      </div>
      <button
        className="secondary"
        disabled={busy || chosen.length !== excess}
        onClick={() => void onCommand("end_turn", { return: chosen })}
      >
        Return {excess} and end turn
      </button>
    </div>
  );
}

export function GameTable({
  state,
  me,
  cards,
  busy,
  onCommand,
  onLeave,
  onVote,
}: Props) {
  const pub = state.view.public;
  const mySeat = state.view.self.seat;
  const [selected, setSelected] = useState<string | null>(null);
  const [reorganizing, setReorganizing] = useState(false);
  const people = new Map(state.participants.map((p) => [p.seat, p]));
  const name = (seat: number) => {
    const p = people.get(seat);
    return p ? (p.user_id === me ? "You" : p.display_name) : `Seat ${seat + 1}`;
  };
  const waiting = pub.waiting_for.includes(mySeat);
  const myTurn = pub.active === mySeat && pub.phase === "play";
  const live = state.status === "playing" || state.status === "paused";
  const pending = pub.pending;
  const target = pending?.targets[pending.current];
  const away = state.participants.filter((p) => !p.connected);
  const winner = state.participants.find((p) => p.user_id === state.winner_id);
  const stateKey = `${state.revision}`;
  const hand = state.view.self.hand;
  const selectedCard = selected && hand.includes(selected) ? selected : null;

  let status: string;
  if (state.status === "finished")
    status = `${winner ? winner.display_name : "A player"} won with three full sets of different colors.`;
  else if (state.status === "abandoned")
    status =
      state.end_reason === "left"
        ? `Match abandoned: ${state.participants.find((p) => p.user_id === state.ended_by)?.display_name ?? "a player"} left. No winner.`
        : state.end_reason === "expired"
          ? "Match expired after everyone was away for 24 hours. No winner."
          : "Match abandoned by vote. No winner.";
  else if (state.status === "paused")
    status = `Paused: waiting for ${away.map((p) => p.display_name).join(", ")} to reconnect.`;
  else if (pub.phase === "play")
    status = myTurn
      ? `Your turn: ${pub.plays_left} play${pub.plays_left === 1 ? "" : "s"} left.`
      : `${name(pub.active)}'s turn.`;
  else if (pub.phase === "placement")
    status = "Received cards are being placed.";
  else if (pending && target) {
    const who =
      target.stage === "chain"
        ? name(target.chain!.waiting)
        : name(target.seat);
    status = `${name(pending.source)} played ${actionName(pending.action)}. Waiting for ${who}${target.stage === "pay" ? ` to pay ${target.owed}M` : " to respond"}.`;
  } else status = "";

  const log = state.events
    .map((e) => ({ e, text: describeEvent(e, cards, name) }))
    .filter((x) => x.text)
    .slice(-14)
    .reverse();

  return (
    <div className="game-table">
      <div
        className={`game-status ${state.status}`}
        role="status"
        aria-live="polite"
      >
        <strong>{status}</strong>
        <span className="muted">
          {" "}
          Turn {pub.turn} · draw pile {pub.draw_count} · center{" "}
          {pub.center_pile.length}
        </span>
      </div>
      {state.status === "paused" && (
        <div className="decision">
          {state.can_vote_abandon ? (
            <>
              <p>
                A seat has been empty for over 5 minutes. If everyone still here
                agrees, the match ends with no winner. You can also keep
                waiting.
              </p>
              <button
                className="secondary"
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
      )}
      {live &&
        state.status === "playing" &&
        waiting &&
        pub.phase === "response" && (
          <ResponsePanel
            key={`r${stateKey}`}
            state={state}
            cards={cards}
            mySeat={mySeat}
            name={name}
            busy={busy}
            onCommand={onCommand}
          />
        )}
      {live &&
        state.status === "playing" &&
        waiting &&
        pub.phase === "payment" && (
          <PaymentPanel
            key={`p${stateKey}`}
            state={state}
            cards={cards}
            mySeat={mySeat}
            busy={busy}
            onCommand={onCommand}
          />
        )}
      {live &&
        state.status === "playing" &&
        waiting &&
        pub.phase === "placement" && (
          <PlacementPanel
            key={`pl${stateKey}`}
            state={state}
            cards={cards}
            mySeat={mySeat}
            busy={busy}
            onCommand={onCommand}
          />
        )}
      <div className="table-players">
        {pub.players
          .filter((p) => p.seat !== mySeat)
          .map((p) => (
            <PlayerArea
              key={p.seat}
              player={p}
              name={name(p.seat)}
              cards={cards}
              active={pub.active === p.seat}
              connected={people.get(p.seat)?.connected ?? false}
              self={false}
            />
          ))}
      </div>
      <PlayerArea
        player={pub.players[mySeat]}
        name={name(mySeat)}
        cards={cards}
        active={pub.active === mySeat}
        connected
        self
      />
      <section className="my-hand" aria-label="Your hand">
        <h2>Your hand ({hand.length})</h2>
        <div className="hand-cards">
          {hand.map((id) => (
            <button
              key={id}
              className={`hand-card ${selectedCard === id ? "selected" : ""}`}
              aria-pressed={selectedCard === id}
              disabled={
                !myTurn || state.status !== "playing" || pub.plays_left === 0
              }
              onClick={() => setSelected(selectedCard === id ? null : id)}
            >
              <Chip cards={cards} id={id} />
            </button>
          ))}
        </div>
        {myTurn &&
          state.status === "playing" &&
          selectedCard &&
          pub.plays_left > 0 && (
            <HandActions
              key={`${selectedCard}:${stateKey}`}
              id={selectedCard}
              state={state}
              cards={cards}
              mySeat={mySeat}
              name={name}
              busy={busy}
              onCommand={onCommand}
            />
          )}
        {myTurn && state.status === "playing" && (
          <div className="decision-row">
            {reorganizing ? (
              <Reorganize
                key={`o${stateKey}`}
                state={state}
                cards={cards}
                mySeat={mySeat}
                busy={busy}
                onCommand={onCommand}
                onDone={() => setReorganizing(false)}
              />
            ) : (
              <button
                className="text-button"
                onClick={() => setReorganizing(true)}
              >
                Reorganize properties
              </button>
            )}
            <EndTurn
              key={`e${stateKey}`}
              state={state}
              cards={cards}
              busy={busy}
              onCommand={onCommand}
            />
          </div>
        )}
      </section>
      <section className="game-log" aria-label="Recent actions">
        <h2>Recent actions</h2>
        <ol>
          {log.map(({ e, text }, i) => (
            <li key={`${e.revision}-${i}`}>{text}</li>
          ))}
        </ol>
      </section>
      {live && (
        <button
          className="text-button"
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
      )}
    </div>
  );
}
