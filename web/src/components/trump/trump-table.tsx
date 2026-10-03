"use client";

// The Trump table. The viewer always sits at the bottom with their partner
// opposite, while the real seat order is unchanged: play still runs 0, 1, 2,
// 3. Everything the table shows comes from the server's projection, and every
// action is validated there; the client only decides what to offer.

import { ReactNode, useEffect, useRef, useState } from "react";
import {
  nextLeader,
  parseCard,
  seatRing,
  sortHand,
  SUIT_NAME,
  SUIT_SYMBOL,
  Suit,
  TEAM_NAME,
  TrumpMatch,
  cardLabel,
  whyIllegal,
} from "../../lib/trump";
import { TrumpCardBack, TrumpCardFace } from "./playing-card";
import { RoundOver, TrumpChoice } from "./sheets";

type Command = (kind: string, payload: object) => Promise<boolean>;

// The lower-numbered seat of a team; its partner is two seats along.
const trump0 = (team: number) => team;

export function TrumpTable({
  state,
  me,
  busy,
  roomName,
  teamNames,
  connection,
  alerts,
  chat,
  unread = 0,
  onCommand,
  onLeave,
}: {
  state: TrumpMatch;
  me: string;
  busy: boolean;
  roomName?: string;
  teamNames?: [string, string];
  connection?: string;
  alerts?: ReactNode;
  chat?: ReactNode;
  unread?: number;
  onCommand: Command;
  onLeave: () => void;
}) {
  const pub = state.view.public;
  const self = state.view.self;
  const mySeat = self.seat;
  const ring = seatRing(mySeat);
  const teamName = (team: number) =>
    teamNames?.[team]?.trim() || TEAM_NAME[team];
  const [chatOpen, setChatOpen] = useState(false);
  // The card this client has sent and is waiting for the server to confirm.
  // It is optimistic only in appearance: no trick, count or result moves
  // until the server's state arrives.
  const [pending, setPending] = useState<string | null>(null);
  const [refused, setRefused] = useState<string | null>(null);
  // The finished trick is shown travelling to its team for a moment, then the
  // table is clear. Which trick has been cleared is state; the cards come
  // from the server, so a state push cannot strand them on the table.
  const [clearedTrick, setClearedTrick] = useState("");
  const revision = useRef(state.revision);
  useEffect(() => {
    if (state.revision !== revision.current) {
      revision.current = state.revision;
      setPending(null);
    }
  }, [state.revision]);

  const last = pub.last_trick;
  const lastKey = last
    ? `${pub.round}:${last.cards.map((c) => c.card).join(",")}`
    : "";
  // Only while the server has no live trick: once someone leads, the new
  // trick takes the table.
  const collecting =
    last && lastKey !== clearedTrick && pub.trick.length === 0 ? last : null;
  useEffect(() => {
    if (!collecting) return;
    const reduced =
      typeof window !== "undefined" &&
      window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    const timer = setTimeout(
      () => setClearedTrick(lastKey),
      reduced ? 450 : 1000,
    );
    return () => clearTimeout(timer);
  }, [collecting, lastKey]);

  const offline = connection !== undefined && connection !== "Connected";
  const playing = state.status === "playing";
  const myTurn = pub.turn === mySeat && pub.phase === "play";
  const chatClosed = pub.phase === "trump_selection";
  const leader = nextLeader(pub);

  const nameOf = (seat: number) => {
    const p = state.participants.find((x) => x.seat === seat);
    if (!p) return `Seat ${seat + 1}`;
    return p.user_id === me ? "You" : p.display_name;
  };
  const seatInfo = (seat: number) => pub.players.find((p) => p.seat === seat);
  const participant = (seat: number) =>
    state.participants.find((p) => p.seat === seat);

  const play = async (id: string) => {
    const reason = whyIllegal(id, pub, self, myTurn);
    if (reason) {
      setRefused(reason);
      return;
    }
    setRefused(null);
    setPending(id);
    const accepted = await onCommand("play_card", { card: id });
    if (!accepted) setPending(null);
  };

  // One seat around the table: name, team, connection, readiness and the
  // number of cards still in hand.
  // Rendered by a plain call, not as a component: it holds no state of its
  // own and must not be re-created on every render.
  const seatCard = (seat: number, where: string) => {
    const info = seatInfo(seat);
    const p = participant(seat);
    const turn = pub.turn === seat && pub.phase === "play";
    const deciding =
      pub.phase === "trump_selection" &&
      (pub.decider === seat ||
        (pub.decider < 0 && info?.team === pub.entitled_team));
    const away = p ? !p.connected : false;
    return (
      <div
        className={`tseat tseat-${where} team-${info?.team ?? 0} ${turn ? "turn" : ""} ${deciding ? "deciding" : ""}`}
      >
        <div className="tseat-head">
          <span className="tseat-name">{nameOf(seat)}</span>
          <span className="tseat-team">
            {teamName(info?.team ?? 0)}
            {seat === ring.top ? " · partner" : ""}
          </span>
        </div>
        <div className="tseat-meta">
          <span
            className="tseat-cards"
            aria-label={`${info?.cards ?? 0} cards in hand`}
          >
            {seat !== mySeat && (
              <span className="tseat-backs" aria-hidden="true">
                <TrumpCardBack />
              </span>
            )}
            {info?.cards ?? 0} cards
          </span>
          {away && <span className="tseat-flag away">Away</span>}
          {turn && <span className="tseat-flag turn-flag">Their turn</span>}
          {deciding && <span className="tseat-flag">Choosing trump</span>}
          {pub.phase === "round_over" && info?.ready && (
            <span className="tseat-flag ready">Ready</span>
          )}
          {leader === seat && <span className="tseat-flag">Leads next</span>}
        </div>
      </div>
    );
  };

  // The middle of the table: the cards of the current or just-finished trick,
  // placed where their player sits.
  const trick = pub.trick.length > 0 ? pub.trick : (collecting?.cards ?? []);
  const finished = pub.trick.length === 0 && collecting != null;
  // Which side of the table the four cards fly to: your team's pile is at
  // the bottom, the opponents' at the top.
  const collectSide =
    collecting && collecting.team === self.team ? "mine" : "theirs";
  const teamPile = (team: number) => (
    <div
      className={`counter team-${team} ${self.team === team ? "mine" : "theirs"} ${collecting?.team === team ? "collected" : ""}`}
      aria-label={`${teamName(team)}: ${pub.tricks[team]} tricks won this round`}
    >
      <span className="counter-team">
        {teamName(team)}
        {self.team === team ? " · yours" : ""}
      </span>
      <span className="counter-pile" aria-hidden="true">
        {Array.from({ length: pub.tricks[team] }).map((_, trickIndex) => (
          <span className="trick-set" key={trickIndex}>
            {Array.from({ length: 4 }).map((__, cardIndex) => (
              <span className="pile-card" key={cardIndex} />
            ))}
          </span>
        ))}
      </span>
      <span className="counter-value">
        {pub.tricks[team]}
        <span className="counter-of"> / {pub.tricks_to_win} tricks</span>
      </span>
      <span className="counter-rounds">
        {pub.rounds_won[team]} round{pub.rounds_won[team] === 1 ? "" : "s"} won
      </span>
    </div>
  );
  const positionOf = (seat: number) =>
    seat === ring.bottom
      ? "bottom"
      : seat === ring.left
        ? "left"
        : seat === ring.top
          ? "top"
          : "right";

  return (
    <div className={`trump-table ${offline ? "offline" : ""}`}>
      <header className="trump-bar">
        <div className="trump-bar-left">
          <h1>{roomName ?? "Trump"}</h1>
          <p className="trump-round">
            Round {pub.round} · First to {pub.tricks_to_win} tricks
          </p>
        </div>
        <div className="trump-bar-right">
          <span
            className={`trump-indicator ${pub.trump ? `suit-${pub.trump}` : "none"}`}
            aria-label={
              pub.trump
                ? `Trump suit: ${SUIT_NAME[pub.trump]}`
                : "Trump suit not chosen yet"
            }
          >
            <span aria-hidden="true">
              {pub.trump ? SUIT_SYMBOL[pub.trump] : "–"}
            </span>
            <span className="trump-indicator-name">
              {pub.trump ? SUIT_NAME[pub.trump] : "No trump yet"}
            </span>
          </span>
          {connection && (
            <span
              className={`conn ${offline ? "bad" : "good"}`}
              role="status"
              aria-live="polite"
            >
              {connection}
            </span>
          )}
        </div>
      </header>

      {alerts}

      {(pub.phase === "trump_selection" || pub.round === 1) && (
        <p className="toss-banner" role="status">
          <span className="toss-label">
            {pub.round === 1 ? "Toss winner" : "Trump choice"}
          </span>
          <strong>{teamName(pub.entitled_team)}</strong>
          {pub.phase === "trump_selection" ? (
            <>
              {" chooses the trump suit. "}
              {pub.decider >= 0 ? (
                <>
                  <strong>{nameOf(pub.decider)}</strong> is choosing.
                </>
              ) : (
                <>
                  {nameOf(trump0(pub.entitled_team))} or{" "}
                  {nameOf(trump0(pub.entitled_team) + 2)} may choose.
                </>
              )}
            </>
          ) : (
            <>
              {" won the toss. "}
              <strong>{nameOf(pub.chooser)}</strong> chose{" "}
              {pub.trump ? SUIT_NAME[pub.trump] : "the trump suit"}.
            </>
          )}
          {pub.round > 1 && " They won the previous round."}
        </p>
      )}

      {teamPile(1 - self.team)}

      <div className="trump-felt">
        {seatCard(ring.top, "top")}
        {seatCard(ring.left, "left")}
        {seatCard(ring.right, "right")}

        <div
          className={`trick-area ${finished ? "finished" : ""}`}
          aria-label="Cards played this trick"
          aria-live="polite"
        >
          {trick.length === 0 ? (
            <p className="trick-empty">
              {pub.phase === "trump_selection"
                ? "Waiting for the trump suit"
                : myTurn
                  ? "Your lead"
                  : `${nameOf(pub.turn)} to lead`}
            </p>
          ) : (
            trick.map((p, i) => {
              const won = finished && collecting?.winner === p.seat;
              return (
                <div
                  key={p.card}
                  className={`trick-card at-${positionOf(p.seat)} ${won ? "won" : ""} ${finished ? `collecting to-${collectSide}` : ""}`}
                >
                  <TrumpCardFace id={p.card} size="md" />
                  <span className="trick-who">
                    {i === 0 ? "Led · " : ""}
                    {nameOf(p.seat)}
                    {won ? " · won" : ""}
                  </span>
                </div>
              );
            })
          )}
        </div>

        {pub.lead_suit && pub.trick.length > 0 && (
          <p className="lead-note" role="status">
            Lead suit: {SUIT_NAME[pub.lead_suit]} {SUIT_SYMBOL[pub.lead_suit]}
          </p>
        )}
      </div>

      {teamPile(self.team)}
      {seatCard(ring.bottom, "bottom")}

      <div className="hand-area">
        <p className="hand-status" role="status" aria-live="polite">
          {!playing
            ? "Waiting for every player"
            : pub.phase === "trump_selection"
              ? "Trump is being chosen"
              : myTurn
                ? pending
                  ? "Sending your card…"
                  : "Your turn — choose a card"
                : `Waiting for ${nameOf(pub.turn)}`}
        </p>
        <ul className="trump-hand-rail" aria-label="Your hand">
          {sortHand(self.hand, pub.trump).map((id) => {
            const legal = self.legal.includes(id);
            const reason = whyIllegal(id, pub, self, myTurn);
            const card = parseCard(id);
            return (
              <li key={id}>
                <button
                  type="button"
                  className={`hand-card-btn ${legal ? "legal" : "illegal"} ${pending === id ? "pending" : ""}`}
                  data-card={id}
                  disabled={busy || offline || pending !== null}
                  aria-label={`${cardLabel(id)}${legal ? "" : `. ${reason ?? ""}`}`}
                  aria-disabled={!legal}
                  onClick={() => void play(id)}
                >
                  <TrumpCardFace
                    id={id}
                    size="lg"
                    dimmed={!legal && myTurn}
                    highlight={legal && myTurn}
                  />
                  {card && (
                    <span className="sr-only">
                      {SUIT_NAME[card.suit as Suit]}
                    </span>
                  )}
                </button>
              </li>
            );
          })}
        </ul>
        {refused && (
          <p className="refusal" role="alert">
            {refused}
          </p>
        )}
      </div>

      <div className="trump-actions">
        <button type="button" className="btn-ghost" onClick={onLeave}>
          Leave match
        </button>
        {chat && (
          <button
            type="button"
            className="chat-toggle"
            disabled={chatClosed}
            aria-describedby={chatClosed ? "chat-closed-note" : undefined}
            onClick={() => setChatOpen((v) => !v)}
          >
            Chat{unread > 0 ? ` (${unread})` : ""}
          </button>
        )}
      </div>
      {chatClosed && (
        <p id="chat-closed-note" className="chat-closed" role="note">
          Chat is closed while the trump is chosen. It reopens for the rest of
          the round.
        </p>
      )}
      {chat && chatOpen && !chatClosed && (
        <div className="trump-chat">
          <button
            type="button"
            className="chat-close"
            onClick={() => setChatOpen(false)}
          >
            ← Back to table
          </button>
          {chat}
        </div>
      )}

      {playing && pub.phase === "trump_selection" && (
        <TrumpChoice
          pub={pub}
          self={self}
          name={nameOf}
          teamName={teamName}
          busy={busy || offline}
          onChoose={(suit) => void onCommand("choose_trump", { suit })}
          onDelegate={() => void onCommand("delegate_trump", {})}
        />
      )}
      {playing && pub.phase === "round_over" && !collecting && (
        <RoundOver
          pub={pub}
          self={self}
          name={nameOf}
          teamName={teamName}
          busy={busy || offline}
          onReady={() => void onCommand("ready_round", {})}
        />
      )}
    </div>
  );
}
