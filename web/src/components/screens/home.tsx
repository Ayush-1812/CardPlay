"use client";

// Home: pick a game; rejoin a table you are still in; friends' invitations.
// Lobby: for the chosen game, set your name and create a room or join one
// with an invite link.

import { FormEvent, useState } from "react";
import { Invitation, Room, User } from "../../lib/api";

export type GameEntry = {
  id: string;
  name: string;
  blurb: string;
  players: string;
  playable: boolean;
  suit: string;
  // Seats a room for this game is created with. Omitted means the player
  // picks, within the limits the server enforces.
  seats?: number;
};

export const GAMES: GameEntry[] = [
  {
    id: "monopoly-deal",
    name: "Monopoly Deal",
    blurb: "Collect three full property sets. Steal, charge rent, say no.",
    players: "2–5 players",
    playable: true,
    suit: "♦",
  },
  {
    id: "cambio",
    name: "Cambio",
    blurb: "Memory and nerve: swap your way to the lowest hand.",
    players: "2–6 players",
    playable: false,
    suit: "♠",
  },
  {
    id: "trump",
    name: "Trump",
    blurb:
      "Two teams, a chosen trump suit, thirteen tricks. First to seven wins the round.",
    players: "4 players · 2 teams",
    playable: true,
    suit: "♥",
    seats: 4,
  },
];

export function GamesScreen({
  user,
  rooms,
  invitations,
  busy,
  onPick,
  onOpenRoom,
  onJoinInvitation,
}: {
  user: User;
  rooms: Room[];
  invitations: Invitation[];
  busy: boolean;
  onPick: (game: GameEntry) => void;
  onOpenRoom: (id: string) => void;
  onJoinInvitation: (id: string) => void;
}) {
  return (
    <main className="screen home">
      <div className="screen-head">
        <p className="eyebrow">Hi, {user.display_name}</p>
        <h1>Choose a game</h1>
      </div>

      {rooms.length > 0 && (
        <section className="rejoin" aria-label="Your tables">
          {rooms.map((room) => (
            <button
              key={room.id}
              type="button"
              className="rejoin-row"
              onClick={() => onOpenRoom(room.id)}
            >
              <span>
                <b>{room.name}</b>
                <small>
                  {room.status === "playing"
                    ? "Game in progress"
                    : "Waiting room"}
                </small>
              </span>
              <span className="rejoin-go">Rejoin →</span>
            </button>
          ))}
        </section>
      )}

      {invitations.length > 0 && (
        <section className="rejoin" aria-label="Invitations from friends">
          {invitations.map((invite) => (
            <div className="rejoin-row" key={invite.id}>
              <span>
                <b>{invite.name}</b>
                <small>A friend invited you</small>
              </span>
              <button
                type="button"
                className="btn-gold"
                disabled={busy}
                onClick={() => onJoinInvitation(invite.id)}
              >
                Join →
              </button>
            </div>
          ))}
        </section>
      )}

      <div className="game-grid">
        {GAMES.map((g) => (
          <button
            key={g.id}
            type="button"
            className={`game-tile ${g.playable ? "" : "soon"}`}
            disabled={!g.playable}
            aria-label={`${g.name}${g.playable ? "" : ", coming soon"}`}
            onClick={() => onPick(g)}
          >
            <span className="game-suit" aria-hidden="true">
              {g.suit}
            </span>
            <span className="game-name">{g.name}</span>
            <span className="game-blurb">{g.blurb}</span>
            <span className="game-meta">
              {g.players}
              {g.playable ? "" : " · Coming soon"}
            </span>
          </button>
        ))}
      </div>
    </main>
  );
}

// Lobby for one game: your name, then create a room or join by link.
export function GameLobby({
  game,
  user,
  busy,
  invite,
  onBack,
  onCreate,
  onJoin,
}: {
  game: GameEntry;
  user: User;
  busy: boolean;
  invite: string;
  onBack: () => void;
  onCreate: (name: string) => void;
  onJoin: (link: string, name: string) => void;
}) {
  const [name, setName] = useState(user.display_name);
  const [link, setLink] = useState(invite);
  const named = name.trim().length > 0;
  return (
    <main className="screen lobby">
      <button type="button" className="sheet-back" onClick={onBack}>
        ← All games
      </button>
      <div className="screen-head">
        <p className="eyebrow">{game.players}</p>
        <h1>{game.name}</h1>
      </div>
      <section className="entry-card">
        <label className="field">
          <span>Your name at the table</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={24}
            required
            autoComplete="nickname"
          />
        </label>
        <button
          type="button"
          className="btn-gold big"
          disabled={busy || !named}
          onClick={() => onCreate(name.trim())}
        >
          Create room
        </button>
        <div className="divider">
          <span>or join with an invite</span>
        </div>
        <form
          className="join-form"
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            onJoin(link.trim(), name.trim());
          }}
        >
          <label className="sr-only" htmlFor="invite-link">
            Invite link
          </label>
          <input
            id="invite-link"
            value={link}
            onChange={(e) => setLink(e.target.value)}
            required
            placeholder="Paste an invite link"
          />
          <button className="btn-ghost big" disabled={busy || !named}>
            Join
          </button>
        </form>
      </section>
    </main>
  );
}
