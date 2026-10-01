"use client";

// Waiting room: invite people, get ready, start. The room exists only while
// someone is in it (owner decision 2026-10-01), so leaving as the last
// player deletes it.

import { ReactNode, useState } from "react";
import { Friend, RoomView, User } from "../../lib/api";

export function RoomScreen({
  user,
  view,
  busy,
  connection,
  link,
  friends,
  result,
  chat,
  unreadBadge,
  onNewLink,
  onInviteFriend,
  onReady,
  onStart,
  onLeave,
  onClose,
  onUpdate,
  onMakeHost,
  onKick,
}: {
  user: User;
  view: RoomView | null;
  busy: boolean;
  connection: string;
  link: string;
  friends: Friend[];
  result?: ReactNode;
  chat: ReactNode;
  unreadBadge?: ReactNode;
  onNewLink: () => void;
  onInviteFriend: (friend: Friend) => void;
  onReady: (ready: boolean) => void;
  onStart: () => void;
  onLeave: () => void;
  onClose: () => void;
  onUpdate: (name: string, capacity: number) => void;
  onMakeHost: (id: string, handle: string) => void;
  onKick: (id: string, handle: string) => void;
}) {
  const [copied, setCopied] = useState(false);
  if (!view)
    return (
      <main className="screen room">
        <p role="status" className="muted">
          Opening your table…
        </p>
      </main>
    );
  const room = view.room;
  const host = room.host_id === user.id;
  const waiting = room.status === "waiting";
  const me = view.members.find((m) => m.id === user.id);
  const full = view.members.length >= room.capacity;
  const everyoneReady =
    view.members.length >= 2 && view.members.every((m) => m.ready);
  const invitable = friends.filter(
    (f) => !view.members.some((m) => m.id === f.id),
  );
  const share = async () => {
    try {
      if (navigator.share) {
        await navigator.share({
          title: `Join ${room.name} on CardPlay`,
          text: "Join my Monopoly Deal table",
          url: link,
        });
        return;
      }
      await navigator.clipboard.writeText(link);
      setCopied(true);
    } catch {
      // The person cancelled the share sheet or blocked the clipboard; the
      // link stays visible to copy by hand.
    }
  };

  return (
    <main className="screen room">
      <div className="room-head">
        <div>
          <p className="eyebrow">Monopoly Deal · private room</p>
          <h1>{room.name}</h1>
        </div>
        <span className={`pill ${connection === "Connected" ? "ok" : ""}`}>
          {connection}
        </span>
      </div>

      {result}

      {waiting && (
        <section className="panel invite-panel" aria-label="Invite players">
          <h2>Invite players</h2>
          {full ? (
            <p className="notice" role="status">
              The table is full.{" "}
              {host
                ? "Add seats below, or remove a player, to invite more."
                : ""}
            </p>
          ) : link ? (
            <>
              <div className="invite-link">
                <input
                  readOnly
                  value={link}
                  aria-label="Invite link"
                  onFocus={(e) => e.currentTarget.select()}
                />
                <button
                  type="button"
                  className="btn-gold"
                  onClick={() => void share()}
                >
                  {copied ? "Copied!" : "Share link"}
                </button>
              </div>
              <p className="fine">
                Anyone with this link can take a free seat for the next 30
                minutes.{" "}
                <button
                  type="button"
                  className="text-button"
                  disabled={busy}
                  onClick={() => {
                    setCopied(false);
                    onNewLink();
                  }}
                >
                  New link
                </button>
              </p>
            </>
          ) : (
            <button
              type="button"
              className="btn-gold big"
              disabled={busy}
              onClick={onNewLink}
            >
              Create an invite link
            </button>
          )}
          {!full &&
            invitable.map((friend) => (
              <div className="friend-row" key={friend.id}>
                <span>
                  {friend.display_name}
                  <small>@{friend.handle}</small>
                </span>
                <button
                  type="button"
                  className="text-button"
                  disabled={busy}
                  onClick={() => onInviteFriend(friend)}
                >
                  Invite
                </button>
              </div>
            ))}
        </section>
      )}

      <section className="panel" aria-label="Players">
        <h2>
          Players{" "}
          <span className="muted">
            {view.members.length}/{room.capacity}
          </span>
        </h2>
        <div className="seat-list">
          {view.members.map((member) => (
            <div className="seat" key={member.id}>
              <span className="avatar">
                {member.display_name.slice(0, 1).toUpperCase()}
              </span>
              <div>
                <strong>
                  {member.display_name}
                  {member.id === user.id ? " (you)" : ""}
                </strong>
                <small>{room.host_id === member.id ? "Host" : "Player"}</small>
              </div>
              <span className={`pill ${member.ready ? "ready" : ""}`}>
                {member.ready ? "Ready" : "Not ready"}
              </span>
              {host && waiting && member.id !== user.id && (
                <span className="seat-tools">
                  <button
                    type="button"
                    className="text-button"
                    disabled={busy}
                    onClick={() => onMakeHost(member.id, member.display_name)}
                  >
                    Make host
                  </button>
                  <button
                    type="button"
                    className="text-button"
                    disabled={busy}
                    onClick={() => onKick(member.id, member.display_name)}
                  >
                    Remove
                  </button>
                </span>
              )}
            </div>
          ))}
        </div>
        {waiting && (
          <div className="room-actions">
            <button
              type="button"
              className={me?.ready ? "btn-ghost big" : "btn-gold big"}
              disabled={busy}
              onClick={() => onReady(!me?.ready)}
            >
              {me?.ready ? "Not ready yet" : "I'm ready"}
            </button>
            {host && (
              <button
                type="button"
                className="btn-gold big"
                disabled={busy || !everyoneReady}
                onClick={onStart}
              >
                Start match
              </button>
            )}
          </div>
        )}
        <p className="release-note">
          {!waiting
            ? "A match is in progress."
            : everyoneReady
              ? host
                ? "Everyone is ready. Start when you like."
                : "Everyone is ready. Waiting for the host to start."
              : "The host can start once 2-5 players are all ready."}
        </p>
      </section>

      {chat}
      {unreadBadge}

      {waiting && (
        <section className="panel room-settings" aria-label="Room settings">
          {host && (
            <details>
              <summary>Room settings</summary>
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  const data = new FormData(e.currentTarget);
                  onUpdate(
                    String(data.get("name")),
                    Number(data.get("capacity")),
                  );
                }}
              >
                <label className="field">
                  <span>Room name</span>
                  <input
                    name="name"
                    key={`${room.id}:${room.name}`}
                    defaultValue={room.name}
                    maxLength={80}
                    required
                  />
                </label>
                <label className="field">
                  <span>Seats</span>
                  <select
                    name="capacity"
                    key={`${room.id}:${room.capacity}`}
                    defaultValue={room.capacity}
                  >
                    {[2, 3, 4, 5].map((n) => (
                      <option key={n} value={n}>
                        {n} players
                      </option>
                    ))}
                  </select>
                </label>
                <button className="btn-ghost" disabled={busy}>
                  Save
                </button>
              </form>
            </details>
          )}
          <div className="room-exit">
            <button
              type="button"
              className="text-button"
              disabled={busy}
              onClick={onLeave}
            >
              Leave room
            </button>
            {host && (
              <button
                type="button"
                className="text-button danger"
                disabled={busy}
                onClick={onClose}
              >
                Close room for everyone
              </button>
            )}
          </div>
        </section>
      )}
    </main>
  );
}
