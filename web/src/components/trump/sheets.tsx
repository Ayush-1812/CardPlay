"use client";

// The two decisions a Trump player makes outside playing a card: naming the
// trump suit (or passing it to their partner), and readying for the next
// round. Both are sheets so they read the same on a phone and a desktop.

import { Sheet } from "../ui/sheet";
import { TrumpCardFace } from "./playing-card";
import {
  RoundResult,
  SUITS,
  SUIT_NAME,
  SUIT_SYMBOL,
  Suit,
  sortHand,
  TrumpPublic,
  TrumpSelf,
} from "../../lib/trump";

// TrumpChoice is shown to whoever may name the suit. Everyone else sees a
// waiting state, so nobody is left wondering what the table is doing.
export function TrumpChoice({
  pub,
  self,
  name,
  teamName,
  busy,
  onChoose,
  onDelegate,
}: {
  pub: TrumpPublic;
  self: TrumpSelf;
  name: (seat: number) => string;
  teamName: (team: number) => string;
  busy: boolean;
  onChoose: (suit: Suit) => void;
  onDelegate: () => void;
}) {
  const mine = self.may_choose;
  const waitingOn =
    pub.decider >= 0
      ? name(pub.decider)
      : `${teamName(pub.entitled_team)} (${name(pub.entitled_team)} or ${name(pub.entitled_team + 2)})`;
  return (
    <Sheet
      label="Choose the trump suit"
      title={mine ? "Choose the trump suit" : "Choosing the trump suit"}
    >
      <p className="sheet-toss" role="status">
        <span className="sheet-toss-label">
          {pub.round > 1 ? "Previous round winner" : "Toss winner"}
        </span>
        <strong className="sheet-toss-team">
          {teamName(pub.entitled_team)}
        </strong>
        <span>chooses this round&rsquo;s trump suit.</span>
      </p>
      {mine ? (
        <>
          <p className="sheet-hint">
            {pub.delegated
              ? "Your partner passed the choice to you. Pick a suit from your five cards."
              : "Pick a suit from your five cards, or pass the choice to your partner."}
          </p>
          <div className="trump-hand" aria-label="Your first five cards">
            {sortHand(self.hand).map((id) => (
              <TrumpCardFace key={id} id={id} size="md" />
            ))}
          </div>
          <div className="suit-grid">
            {SUITS.map((suit) => (
              <button
                key={suit}
                type="button"
                className={`suit-choice suit-${suit}`}
                disabled={busy}
                onClick={() => onChoose(suit)}
              >
                <span className="suit-mark" aria-hidden="true">
                  {SUIT_SYMBOL[suit]}
                </span>
                <span className="suit-name">{SUIT_NAME[suit]}</span>
                <span className="suit-count">
                  {self.hand.filter((id) => id.startsWith(suit)).length} in hand
                </span>
              </button>
            ))}
          </div>
          {self.may_delegate && (
            <button
              type="button"
              className="btn-ghost big"
              disabled={busy}
              onClick={onDelegate}
            >
              My teammate will choose trump
            </button>
          )}
        </>
      ) : (
        <>
          <p className="sheet-note" role="status">
            {pub.delegated
              ? `${waitingOn} was asked to choose and must now pick a suit.`
              : `Waiting for ${waitingOn} to choose.`}
          </p>
          <p className="sheet-hint">
            Chat is closed until the trump is chosen, so nothing can be
            signalled about anyone&rsquo;s cards.
          </p>
          <div className="trump-hand" aria-label="Your first five cards">
            {sortHand(self.hand).map((id) => (
              <TrumpCardFace key={id} id={id} size="md" />
            ))}
          </div>
        </>
      )}
    </Sheet>
  );
}

// RoundOver shows the result, the running history and who is ready.
export function RoundOver({
  pub,
  self,
  name,
  teamName,
  busy,
  onReady,
}: {
  pub: TrumpPublic;
  self: TrumpSelf;
  name: (seat: number) => string;
  teamName: (team: number) => string;
  busy: boolean;
  onReady: () => void;
}) {
  const result: RoundResult | undefined = pub.history[pub.history.length - 1];
  if (!result) return null;
  const iWon = result.winner === self.team;
  const ready = pub.players.filter((p) => p.ready).length;
  const mine = pub.players.find((p) => p.seat === self.seat);
  return (
    <Sheet
      label="Round result"
      title={
        iWon
          ? "Your team won the round"
          : `${teamName(result.winner)} won the round`
      }
      tone={iWon ? "default" : "alert"}
      footer={
        <button
          type="button"
          className="btn-gold big"
          disabled={busy || mine?.ready}
          onClick={onReady}
        >
          {mine?.ready ? `Ready (${ready} of 4)` : "Ready for next round"}
        </button>
      }
    >
      <div className="round-score">
        <div className={`score-side ${result.winner === 0 ? "won" : ""}`}>
          <span className="score-team">{teamName(0)}</span>
          <span className="score-tricks">{result.tricks[0]}</span>
          <span className="score-label">tricks</span>
        </div>
        <div className={`score-side ${result.winner === 1 ? "won" : ""}`}>
          <span className="score-team">{teamName(1)}</span>
          <span className="score-tricks">{result.tricks[1]}</span>
          <span className="score-label">tricks</span>
        </div>
      </div>
      <p className="sheet-hint">
        Trump was {SUIT_NAME[result.trump]} {SUIT_SYMBOL[result.trump]}, chosen
        by {name(result.chooser)}
        {result.delegated ? " after their partner passed the choice" : ""}.{" "}
        {teamName(result.winner)} chooses next.
      </p>
      <h3 className="sheet-sub">Rounds won</h3>
      <p className="rounds-line">
        {teamName(0)} {pub.rounds_won[0]} &middot; {teamName(1)}{" "}
        {pub.rounds_won[1]}
      </p>
      {pub.history.length > 1 && (
        <>
          <h3 className="sheet-sub">Round history</h3>
          <table className="round-history">
            <thead>
              <tr>
                <th scope="col">Round</th>
                <th scope="col">Trump</th>
                <th scope="col">Won by</th>
                <th scope="col">Tricks</th>
              </tr>
            </thead>
            <tbody>
              {[...pub.history].reverse().map((r) => (
                <tr key={r.round}>
                  <td>{r.round}</td>
                  <td>
                    {SUIT_SYMBOL[r.trump]} {SUIT_NAME[r.trump]}
                  </td>
                  <td>{teamName(r.winner)}</td>
                  <td>
                    {r.tricks[0]}&ndash;{r.tricks[1]}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
      <p className="sheet-hint" role="status">
        {ready} of 4 ready for the next round.
      </p>
    </Sheet>
  );
}
