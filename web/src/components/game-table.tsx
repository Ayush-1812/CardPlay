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
  cardName,
  Cards,
  COLOR_NAMES,
  describeEvent,
  MatchState,
  PublicPlayer,
} from "../lib/game";
import { CardSheet } from "./table/action-sheet";
import { Hand } from "./table/hand";
import { destinationPayload, destinations, placements } from "./table/layout";
import { Board, CenterPile, DeckPile } from "./table/piles";
import { PlayingCard } from "./table/playing-card";
import {
  DiscardPrompt,
  FlipSheet,
  PlacementPrompt,
  ResponsePrompt,
} from "./table/prompts";

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
                  c,
                  me.sets,
                  cards,
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
  busy: busyProp,
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
  // The turn number on which the player asked to end with too many cards;
  // tied to the turn so the discard sheet can never reappear on a later one.
  const [returningTurn, setReturningTurn] = useState<number | null>(null);
  const [pinnedOpp, setPinnedOpp] = useState<number | null>(null);
  const [chatOpen, setChatOpen] = useState(false);
  // A property on the player's own table being moved (flipped).
  const [flipping, setFlipping] = useState<string | null>(null);
  // One-second tick for the turn clock.
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const tick = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(tick);
  }, []);
  // While the socket is down a move cannot reach the server, so every game
  // control is locked until the table reconnects.
  const offline = connection !== undefined && connection !== "Connected";
  const busy = busyProp || offline;
  const people = new Map(state.participants.map((p) => [p.seat, p]));
  const name = (seat: number) => {
    const p = people.get(seat);
    return p ? (p.user_id === me ? "You" : p.display_name) : `Seat ${seat + 1}`;
  };
  const userName = (id: string) => {
    const p = state.participants.find((x) => x.user_id === id);
    return p ? (p.user_id === me ? "You" : p.display_name) : "A player";
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
    .map((e) => ({ e, text: describeEvent(e, cards, name, userName) }))
    .filter((x) => x.text)
    .slice(-12)
    .reverse();

  // Decisions the game needs from this player open as large sheets.
  let prompt: ReactNode = null;
  if (
    playing &&
    waiting &&
    (pub.phase === "response" || pub.phase === "payment")
  )
    prompt = (
      <ResponsePrompt
        key={`r${stateKey}`}
        state={state}
        cards={cards}
        mySeat={mySeat}
        name={name}
        busy={busy}
        onCommand={onCommand}
      />
    );
  if (
    playing &&
    waiting &&
    pub.phase === "placement" &&
    myPlayer.incoming.length > 0
  )
    prompt = (
      <PlacementPrompt
        key={`pl${stateKey}`}
        state={state}
        cards={cards}
        mySeat={mySeat}
        busy={busy}
        onCommand={onCommand}
      />
    );
  // Three plays spent with more than seven cards: the turn waits for the
  // discard choice (otherwise the server ends it automatically).
  const mustDiscard = myTurn && pub.plays_left === 0 && hand.length > 7;
  const discarding =
    mustDiscard || (returningTurn === pub.turn && myTurn && hand.length > 7);
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
      disabled={!myTurn || pub.plays_left === 0 || offline}
      layout={wide ? "fan" : "rail"}
      onSelect={pickCard}
    />
  );

  // Tapping a card, as in the reference game: money goes straight to the
  // bank and a property with only one sensible home straight there; a sheet
  // opens only when there is a real choice.
  function pickCard(id: string) {
    const card = cards[id];
    if (!card || busy) return;
    if (card.kind === "money") {
      void onCommand("bank", { card: id });
      return;
    }
    if (
      card.kind === "property" ||
      card.kind === "wild" ||
      card.kind === "rainbow_wild"
    ) {
      const options = placements(card, id, myPlayer.sets, cards);
      if (options.length === 1) {
        void onCommand("play_property", {
          card: id,
          ...destinationPayload(options[0].value),
        });
        return;
      }
    }
    setSelected(id);
  }

  const endTurn = () => {
    if (hand.length > 7) setReturningTurn(pub.turn);
    else void onCommand("end_turn", {});
  };
  // Turn clock: when it runs out the server makes the default move for
  // whoever the game is waiting on.
  const secondsLeft =
    playing && state.turn_seconds_left != null && state.received_at != null
      ? Math.max(
          0,
          Math.ceil(state.turn_seconds_left - (now - state.received_at) / 1000),
        )
      : null;
  const clock = secondsLeft != null && pub.waiting_for.length > 0 && (
    <span
      className={`turn-clock ${secondsLeft <= 20 ? "urgent" : ""}`}
      aria-hidden="true"
      title="When time runs out, the table makes a default move: end the turn, accept, pay or place."
    >
      ⏱ {waiting ? "You" : pub.waiting_for.map(name).join(", ")}{" "}
      {Math.floor(secondsLeft / 60)}:{String(secondsLeft % 60).padStart(2, "0")}
    </span>
  );
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
      <button
        className="btn-ghost small"
        disabled={busy}
        onClick={() => setReorganizing(true)}
      >
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
        {clock}
        {waiting && secondsLeft !== null && secondsLeft <= 20 && (
          <span className="sr-only">
            Less than 20 seconds left before the table moves for you.
          </span>
        )}
        {offline && playing && (
          <span className="table-offline">
            Reconnecting. Your moves are paused until the table is back.
          </span>
        )}
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

      {vote && <div className="prompt-dock">{vote}</div>}

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
            <Board
              player={myPlayer}
              cards={cards}
              width={tableWidth}
              onPickProperty={myTurn && !busy ? setFlipping : undefined}
            />
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

      {prompt}
      {selectedCard && !prompt && !discarding && (
        <CardSheet
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
      {discarding && (
        <DiscardPrompt
          key={`e${stateKey}`}
          state={state}
          cards={cards}
          busy={busy}
          onCommand={onCommand}
          onClose={mustDiscard ? undefined : () => setReturningTurn(null)}
        />
      )}
      {flipping && myTurn && !prompt && (
        <FlipSheet
          key={`f${flipping}:${stateKey}`}
          id={flipping}
          state={state}
          cards={cards}
          mySeat={mySeat}
          busy={busy}
          onCommand={onCommand}
          onClose={() => setFlipping(null)}
        />
      )}

      <aside
        className={`chat-drawer ${chatOpen ? "open" : ""}`}
        aria-label="Room chat"
        inert={!chatOpen}
      >
        <button
          className="dialog-close chat-close"
          aria-label="Close chat"
          onClick={() => setChatOpen(false)}
        >
          <span className="chat-close-x">×</span>
          <span className="chat-close-text">← Back to table</span>
        </button>
        {chat}
      </aside>
    </div>
  );
}
