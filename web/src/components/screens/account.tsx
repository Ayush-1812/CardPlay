"use client";

// Account and friends, kept off the main path in sheets. Guests see only
// their name and a way out; friends need a signed-in account.

import { FormEvent, useState } from "react";
import { api, Friend, PublicUser, User } from "../../lib/api";
import { Sheet } from "../ui/sheet";

type Run = (work: () => Promise<void>) => Promise<void>;

export function AccountSheet({
  user,
  busy,
  run,
  onClose,
  onUser,
  onSignOut,
  onEnded,
  setNotice,
}: {
  user: User;
  busy: boolean;
  run: Run;
  onClose: () => void;
  onUser: (u: User) => void;
  onSignOut: () => void;
  onEnded: (message: string) => void;
  setNotice: (m: string) => void;
}) {
  const [sessions, setSessions] = useState<
    { id: string; created_at: string; expires_at: string }[]
  >([]);
  const submit =
    (work: (data: FormData, form: HTMLFormElement) => Promise<void>) =>
    (e: FormEvent<HTMLFormElement>) => {
      e.preventDefault();
      const form = e.currentTarget;
      void run(() => work(new FormData(form), form));
    };
  return (
    <Sheet label="Your account" title="Your account" onClose={onClose}>
      <p className="muted">
        {user.is_guest
          ? "Playing as a guest in this browser."
          : `@${user.handle} · ${user.email}`}
      </p>
      <form
        className="inline-form"
        onSubmit={submit(async (data) => {
          const result = await api<{ display_name: string }>("/me", "PATCH", {
            display_name: data.get("display_name"),
          });
          onUser({ ...user, display_name: result.display_name });
          setNotice("Name updated.");
        })}
      >
        <label className="field">
          <span>Name</span>
          <input
            name="display_name"
            defaultValue={user.display_name}
            required
            maxLength={user.is_guest ? 24 : 60}
          />
        </label>
        <button className="btn-ghost" disabled={busy}>
          Save
        </button>
      </form>
      {user.is_guest ? (
        <>
          <p className="fine">
            Signing out ends your guest seat: you leave your rooms and this
            guest is deleted.
          </p>
          <button
            type="button"
            className="btn-danger big"
            disabled={busy}
            onClick={() => {
              if (window.confirm("Sign out and end your guest seat?"))
                onSignOut();
            }}
          >
            Sign out
          </button>
        </>
      ) : (
        <>
          <button
            type="button"
            className="text-button"
            disabled={busy}
            onClick={() =>
              void run(async () => {
                setSessions(
                  (await api<{ items: typeof sessions }>("/sessions")).items,
                );
              })
            }
          >
            Show active sessions
          </button>
          {sessions.map((session) => (
            <div className="friend-row" key={session.id}>
              <span>
                Signed in {new Date(session.created_at).toLocaleString()}
              </span>
              <button
                type="button"
                className="text-button"
                disabled={busy}
                onClick={() =>
                  void run(async () => {
                    await api(`/sessions/${session.id}`, "DELETE");
                    setSessions(sessions.filter((s) => s.id !== session.id));
                    // Fails with UNAUTHENTICATED (signing this page out) when
                    // the revoked session was this one.
                    await api<User>("/me");
                    setNotice("Session revoked.");
                  })
                }
              >
                Revoke
              </button>
            </div>
          ))}
          <form
            onSubmit={submit(async (data, form) => {
              await api("/me/password", "PUT", {
                current_password: data.get("current_password"),
                new_password: data.get("new_password"),
              });
              form.reset();
              onEnded("Password changed. Sign in again.");
            })}
          >
            <h3>Change password</h3>
            <label className="field">
              <span>Current password</span>
              <input
                name="current_password"
                type="password"
                autoComplete="current-password"
                required
              />
            </label>
            <label className="field">
              <span>New password</span>
              <input
                name="new_password"
                type="password"
                autoComplete="new-password"
                minLength={12}
                maxLength={128}
                required
              />
            </label>
            <button className="btn-ghost" disabled={busy}>
              Update password
            </button>
          </form>
          <button
            type="button"
            className="btn-ghost big"
            disabled={busy}
            onClick={onSignOut}
          >
            Sign out
          </button>
          <form
            onSubmit={submit(async (data) => {
              if (
                !window.confirm(
                  "Delete your account and sign out? This cannot be undone.",
                )
              )
                return;
              await api("/me", "DELETE", { password: data.get("password") });
              onEnded("Your account was deleted.");
            })}
          >
            <h3>Delete account</h3>
            <label className="field">
              <span>Password</span>
              <input
                name="password"
                type="password"
                required
                autoComplete="current-password"
                placeholder="Confirm with your password"
              />
            </label>
            <button className="text-button danger" disabled={busy}>
              Delete my account
            </button>
          </form>
        </>
      )}
    </Sheet>
  );
}

export function FriendsSheet({
  user,
  friends,
  blocks,
  mutes,
  busy,
  run,
  refresh,
  onClose,
  setNotice,
}: {
  user: User;
  friends: Friend[];
  blocks: PublicUser[];
  mutes: PublicUser[];
  busy: boolean;
  run: Run;
  refresh: () => Promise<void>;
  onClose: () => void;
  setNotice: (m: string) => void;
}) {
  const [found, setFound] = useState<PublicUser | null>(null);
  const act = (path: string, message?: string) =>
    void run(async () => {
      await api(path, "POST");
      await refresh();
      if (message) setNotice(message);
    });
  return (
    <Sheet label="Friends" title="Friends" onClose={onClose}>
      {user.is_guest ? (
        <p className="sheet-note">
          Friends need an account. Sign out and create one to add friends and
          invite them straight into your rooms.
        </p>
      ) : (
        <>
          <form
            className="inline-form"
            onSubmit={(e) => {
              e.preventDefault();
              const handle = String(
                new FormData(e.currentTarget).get("handle"),
              );
              void run(async () => {
                setFound(
                  await api<PublicUser>(
                    `/users?handle=${encodeURIComponent(handle)}`,
                  ),
                );
              });
            }}
          >
            <label className="sr-only" htmlFor="friend-handle">
              Friend handle
            </label>
            <input
              id="friend-handle"
              name="handle"
              required
              placeholder="Find by exact handle"
            />
            <button className="btn-ghost" disabled={busy}>
              Find
            </button>
          </form>
          {found && (
            <div className="friend-row">
              <span>
                {found.display_name}
                <small>@{found.handle}</small>
              </span>
              {found.id !== user.id && (
                <>
                  <button
                    type="button"
                    className="text-button"
                    disabled={busy}
                    onClick={() =>
                      void run(async () => {
                        const result = await api<
                          { status: string } | undefined
                        >(`/friendships/${found.id}/request`, "POST");
                        await refresh();
                        setNotice(
                          result?.status === "accepted"
                            ? `You and @${found.handle} are now friends.`
                            : "Friend request sent.",
                        );
                      })
                    }
                  >
                    Request
                  </button>
                  <button
                    type="button"
                    className="text-button"
                    disabled={busy}
                    onClick={() => {
                      setFound(null);
                      act(`/friendships/${found.id}/block`, "Account blocked.");
                    }}
                  >
                    Block
                  </button>
                </>
              )}
            </div>
          )}
          {friends.length === 0 && (
            <p className="muted">
              No friends yet. Search by an exact handle to send a request.
            </p>
          )}
          {friends.map((friend) => (
            <div className="friend-row" key={friend.id}>
              <span>
                @{friend.handle}
                <small>
                  {friend.status === "accepted"
                    ? "Friend"
                    : friend.requester_id === user.id
                      ? "Request sent"
                      : "Wants to be friends"}
                </small>
              </span>
              {friend.status === "pending" &&
                friend.recipient_id === user.id && (
                  <button
                    type="button"
                    className="text-button"
                    disabled={busy}
                    onClick={() => act(`/friendships/${friend.id}/accept`)}
                  >
                    Accept
                  </button>
                )}
              <button
                type="button"
                className="text-button"
                disabled={busy}
                onClick={() =>
                  act(
                    `/friendships/${friend.id}/${friend.status === "pending" && friend.recipient_id === user.id ? "decline" : "remove"}`,
                  )
                }
              >
                {friend.status === "accepted"
                  ? "Remove"
                  : friend.recipient_id === user.id
                    ? "Decline"
                    : "Cancel"}
              </button>
              <button
                type="button"
                className="text-button"
                disabled={busy}
                onClick={() =>
                  act(
                    `/friendships/${friend.id}/block`,
                    `@${friend.handle} blocked.`,
                  )
                }
              >
                Block
              </button>
            </div>
          ))}
          {blocks.length > 0 && <h3>Blocked</h3>}
          {blocks.map((b) => (
            <div className="friend-row" key={b.id}>
              <span>@{b.handle}</span>
              <button
                type="button"
                className="text-button"
                disabled={busy}
                onClick={() =>
                  act(`/friendships/${b.id}/unblock`, `@${b.handle} unblocked.`)
                }
              >
                Unblock
              </button>
            </div>
          ))}
        </>
      )}
      {mutes.length > 0 && <h3>Muted in chat</h3>}
      {mutes.map((m) => (
        <div className="friend-row" key={m.id}>
          <span>{m.display_name}</span>
          <button
            type="button"
            className="text-button"
            disabled={busy}
            onClick={() =>
              void run(async () => {
                await api(`/mutes/${m.id}`, "DELETE");
                await refresh();
                setNotice(`${m.display_name} unmuted.`);
              })
            }
          >
            Unmute
          </button>
        </div>
      ))}
    </Sheet>
  );
}
