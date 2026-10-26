"use client";

// The Bluff table. Everything shown here comes from the server's own
// projection: this component never decides whether a claim is true, never
// hints that a selected card does not match, and never counts the window
// down for the server -- the countdown is a display of the server's
// deadline, and the server alone decides whether a challenge arrived late.

import { ReactNode, useEffect, useState } from "react";
import {
  BluffMatch,
  BluffPublic,
  BluffSelf,
  cardLabel,
  declaration,
  rankWord,
  secondsLeft,
  sortHand,
} from "../../lib/bluff";
import { HowToPlayButton } from "../how-to-play";
import { BluffCard, BluffCardBack, HandCard } from "./playing-card";

type Command = (kind: string, payload: object) => Promise<boolean>;

export function BluffTable({
  state,
  busy,
  roomName,
  connection,
  alerts,
  chat,
  voice,
  unread,
  onCommand,
  onLeave,
  onHome,
}: {
  state: BluffMatch;
  busy: boolean;
  roomName?: string;
  connection?: string;
  alerts?: ReactNode;
  chat?: ReactNode;
  voice?: ReactNode;
  unread?: number;
  onCommand: Command;
  onLeave: () => void;
  // onHome returns to the room once the game is over, where the table can
  // ready up for another.
  onHome?: () => void;
}) {
  const pub: BluffPublic = state.view.public;
  const self: BluffSelf = state.view.self;
  const playing = state.status === "playing";
  const offline = connection !== undefined && connection !== "Connected";

  const [picked, setPicked] = useState<string[]>([]);
  const [chatOpen, setChatOpen] = useState(false);
  // A refusal belongs to the play it was about, so a new play clears it
  // without an effect reaching back into state.
  const [refusal, setRefusal] = useState<{ play: string; text: string } | null>(
    null,
  );
  // The countdown ticks locally between pushes; the deadline it counts to
  // is the server's.
  const [, tick] = useState(0);
  // The answer this player has already sent, and what it was about.
  const [sent, setSent] = useState<{ play: string; kind: string } | null>(null);

  useEffect(() => {
    if (pub.phase !== "challenge" || !pub.deadline) return;
    const id = setInterval(() => tick((n) => n + 1), 250);
    return () => clearInterval(id);
  }, [pub.phase, pub.deadline]);

  // A card that has left the hand cannot stay selected: the server's view
  // of the hand decides, so nothing needs clearing when it changes.
  const selected = picked.filter((id) => self.hand.includes(id));
  // What the table is waiting on right now. A refusal or an answer belongs
  // to one of these, and stops applying when it changes.
  const here = `${pub.last?.id ?? ""}:${pub.phase}`;
  const refused = refusal && refusal.play === here ? refusal.text : null;
  const answered = sent?.play === here;

  const seated = state.participants ?? [];
  const nameOf = (seat: number) =>
    seated.find((p) => p.seat === seat)?.display_name ?? `Seat ${seat + 1}`;
  const isAway = (seat: number) =>
    seated.find((p) => p.seat === seat)?.connected === false;

  const myTurn = self.may_play && playing && !offline;
  const canAnswer = self.may_answer && playing && !offline;
  const left = secondsLeft(pub.deadline);

  const toggle = (id: string) =>
    setPicked((held) => {
      const current = held.filter((c) => self.hand.includes(c));
      if (current.includes(id)) return current.filter((c) => c !== id);
      return current.length >= pub.max_cards ? current : [...current, id];
    });

  async function playSelected() {
    if (!myTurn || selected.length === 0 || busy) return;
    const cards = [...selected];
    setRefusal(null);
    const ok = await onCommand("play_cards", { cards });
    if (ok) setPicked([]);
    else
      setRefusal({
        play: here,
        text: "That play was not accepted. Check the table and try again.",
      });
  }

  async function answer(kind: "accept_play" | "call_bluff") {
    // One answer per play per player: pressing twice must not send twice.
    if (!canAnswer || busy || answered) return;
    setSent({ play: here, kind });
    const ok = await onCommand(kind, { play_id: self.answer_id ?? "" });
    if (!ok) {
      setSent(null);
      setRefusal({
        play: here,
        text:
          kind === "call_bluff"
            ? "That challenge did not land: the window closed, or someone called it first."
            : "That did not go through. The window may already have closed.",
      });
    }
  }

  const winner = pub.winner >= 0 ? nameOf(pub.winner) : null;
  const opponents = pub.counts
    .map((count, seat) => ({ seat, count }))
    .filter((o) => o.seat !== self.seat);

  return (
    <div className="bluff-table">
      <header className="bluff-bar">
        <div>
          <h1>{roomName ?? "Bluff"}</h1>
          <p className="bluff-sub">
            {winner
              ? `${winner} won`
              : `${nameOf(pub.turn)} to play · ${pub.pile_count} in the pile`}
          </p>
        </div>
        <div className="bluff-bar-right">
          <span
            className="rank-call"
            aria-label={`The call is ${rankWord(pub.rank, true)}`}
          >
            <span className="rank-call-label">Call</span>
            <strong>{rankWord(pub.rank, true)}</strong>
          </span>
          <HowToPlayButton
            gameID="bluff"
            label="Rules"
            className="conn rules-link"
          />
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

      {voice}

      <div className="bluff-body">
        {alerts}

        <ul className="opponents" aria-label="The other players">
          {opponents.map(({ seat, count }) => (
            <li
              key={seat}
              className={`opponent ${pub.turn === seat ? "turn" : ""} ${isAway(seat) ? "away" : ""}`}
            >
              <span className="opponent-name">{nameOf(seat)}</span>
              <span className="opponent-count">
                {count} card{count === 1 ? "" : "s"}
              </span>
              <span className="opponent-flags">
                {isAway(seat) && <span className="flag away">offline</span>}
                {pub.pending === seat && (
                  <span className="flag pending">one play from winning</span>
                )}
                {pub.phase === "challenge" &&
                  pub.accepted[seat] &&
                  seat !== pub.last?.seat && (
                    <span className="flag">accepted</span>
                  )}
              </span>
            </li>
          ))}
        </ul>

        <section className="middle" aria-label="The pile">
          <div className="pile" aria-hidden="true">
            {Array.from({ length: Math.min(pub.pile_count, 12) }).map(
              (_, i) => (
                <span
                  className="pile-layer"
                  key={i}
                  style={{ ["--i" as string]: i }}
                >
                  <BluffCardBack size="md" />
                </span>
              ),
            )}
            {pub.pile_count === 0 && <span className="pile-empty">empty</span>}
          </div>
          <p className="pile-count">
            {pub.pile_count} card{pub.pile_count === 1 ? "" : "s"} face down
          </p>

          {pub.last && (
            <p className="claim" role="status">
              {declaration(nameOf(pub.last.seat), pub.last)}
              {pub.pending === pub.last.seat && " — their last cards"}
            </p>
          )}

          {pub.phase === "challenge" && (
            <div className="window">
              <p className="countdown" role="timer">
                <span className="countdown-number">{left}</span>
                <span className="countdown-label">
                  second{left === 1 ? "" : "s"} to call it
                </span>
              </p>
              <span className="countdown-bar" aria-hidden="true">
                <span style={{ width: `${Math.min(100, left * 20)}%` }} />
              </span>
            </div>
          )}

          {pub.result && (
            <div
              className={`result ${pub.result.truthful ? "truthful" : "bluffed"}`}
              role="status"
            >
              <p className="result-line">
                <strong>{nameOf(pub.result.player)}</strong> played{" "}
                {pub.result.cards.length}{" "}
                {rankWord(pub.result.claimed, pub.result.cards.length !== 1)}{" "}
                and <strong>{nameOf(pub.result.challenger)}</strong> called it.{" "}
                {pub.result.truthful
                  ? "The claim was true."
                  : "It was a bluff."}
              </p>
              <ul
                className="revealed"
                aria-label="The cards that were revealed"
              >
                {pub.result.cards.map((id) => (
                  <li key={id}>
                    <BluffCard id={id} size="md" />
                    <span className="sr-only">{cardLabel(id)}</span>
                  </li>
                ))}
              </ul>
              <p className="result-collect">
                {nameOf(pub.result.collector)} took the pile of{" "}
                {pub.result.collected}.
              </p>
            </div>
          )}

          {winner && (
            <p className="winner" role="status">
              {pub.winner === self.seat ? "You win." : `${winner} wins.`} Ready
              up in the room for another game.
            </p>
          )}
        </section>
      </div>

      <div className="bluff-dock">
        <p className="hand-status" role="status" aria-live="polite">
          {!playing
            ? "Waiting for the table"
            : winner
              ? "The game is over."
              : myTurn
                ? `Your turn — play 1 to ${pub.max_cards} cards as ${rankWord(pub.rank, true)}`
                : canAnswer
                  ? "Accept the play, or call it a bluff"
                  : pub.phase === "challenge"
                    ? "Waiting for the others"
                    : `Waiting for ${nameOf(pub.turn)}`}
        </p>
        {refused && (
          <p className="refusal" role="alert">
            {refused}
          </p>
        )}

        <ul className="hand-rail" aria-label="Your hand">
          {sortHand(self.hand).map((id) => (
            <li key={id}>
              <HandCard
                id={id}
                selected={selected.includes(id)}
                disabled={!myTurn || busy}
                onToggle={() => toggle(id)}
              />
            </li>
          ))}
          {self.hand.length === 0 && (
            <li className="hand-empty">
              {pub.pending === self.seat
                ? "That was your last card. You win if nobody calls it."
                : "No cards left."}
            </li>
          )}
        </ul>

        <div className="bluff-actions">
          {myTurn && (
            <button
              type="button"
              className="btn-gold"
              disabled={selected.length === 0 || busy}
              onClick={() => void playSelected()}
            >
              {selected.length === 0
                ? `Choose up to ${pub.max_cards} cards`
                : `Play ${selected.length} as ${rankWord(pub.rank, selected.length !== 1)}`}
            </button>
          )}
          {canAnswer && (
            <>
              <button
                type="button"
                className="btn-ghost"
                disabled={busy || answered}
                onClick={() => void answer("accept_play")}
              >
                {answered && sent?.kind === "accept_play"
                  ? "Accepted"
                  : "Accept"}
              </button>
              <button
                type="button"
                className="btn-danger"
                disabled={busy || answered}
                onClick={() => void answer("call_bluff")}
              >
                {answered && sent?.kind === "call_bluff"
                  ? "Called"
                  : "Call bluff"}
              </button>
            </>
          )}
          {winner && onHome ? (
            <button type="button" className="btn-gold" onClick={onHome}>
              Back to the room
            </button>
          ) : (
            <button type="button" className="btn-ghost small" onClick={onLeave}>
              Leave match
            </button>
          )}
          {chat && (
            <button
              type="button"
              className="btn-ghost small"
              onClick={() => setChatOpen((v) => !v)}
            >
              Chat{unread ? ` (${unread})` : ""}
            </button>
          )}
        </div>
      </div>

      {chat && chatOpen && (
        <div className="bluff-chat">
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
    </div>
  );
}
