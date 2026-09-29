"use client";

import { FormEvent, useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import {
  api,
  APIError,
  ChatMessage,
  CreatedInvitation,
  Friend,
  Invitation,
  PublicUser,
  Room,
  RoomView,
  User,
} from "../lib/api";

const SESSION_ENDED = "Your session ended. Sign in again.";
// Server WebSocket close codes; any other close is transient and retried.
const CLOSE_SESSION_ENDED = 4001;
const CLOSE_ROOM_UNAVAILABLE = 4004;

function sessionEnded(e: unknown) {
  return (
    e instanceof APIError && e.status === 401 && e.code === "UNAUTHENTICATED"
  );
}

export function Dashboard() {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [roomList, setRooms] = useState<Room[]>([]);
  const [friends, setFriends] = useState<Friend[]>([]);
  const [blocks, setBlocks] = useState<PublicUser[]>([]);
  const [searchResult, setSearchResult] = useState<PublicUser | null>(null);
  const [dataLoading, setDataLoading] = useState(true);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [view, setView] = useState<RoomView | null>(null);
  const [chat, setChat] = useState<ChatMessage[]>([]);
  const [createdInvites, setCreatedInvites] = useState<CreatedInvitation[]>([]);
  const [connection, setConnection] = useState("Offline");
  const [link, setLink] = useState("");
  const [authMode, setAuthMode] = useState<
    "login" | "register" | "verify" | "forgot" | "reset"
  >("login");
  const [sessions, setSessions] = useState<
    { id: string; created_at: string; expires_at: string }[]
  >([]);
  const [inviteToken, setInviteToken] = useState("");
  const chatDraft = useRef<{ body: string; id: string } | null>(null);
  const busyRef = useRef(false);
  const selectedRef = useRef<string | null>(null);
  const lastChatID = useRef(0);
  const chatQueue = useRef<Promise<void>>(Promise.resolve());
  const chatLog = useRef<HTMLDivElement>(null);
  const userID = user?.id;
  const verified = !!user?.email_verified;

  // Clears everything tied to the signed-in account.
  const endSession = useCallback((message: string) => {
    setUser(null);
    setRooms([]);
    setFriends([]);
    setBlocks([]);
    setInvitations([]);
    setSearchResult(null);
    setDataLoading(true);
    setSelected(null);
    setView(null);
    setChat([]);
    setCreatedInvites([]);
    setSessions([]);
    setLink("");
    setConnection("Offline");
    setError("");
    setNotice(message);
  }, []);

  // A full load replaces the newest page; otherwise fetch messages after the
  // last one shown. Loads run one at a time so cursors never interleave.
  const loadChat = useCallback((roomID: string, full: boolean) => {
    const next = chatQueue.current.then(async () => {
      if (selectedRef.current !== roomID) return;
      if (full) {
        const page = await api<{ items: ChatMessage[] }>(
          `/rooms/${roomID}/chat`,
        );
        if (selectedRef.current !== roomID) return;
        lastChatID.current = page.items[page.items.length - 1]?.id ?? 0;
        setChat(page.items);
        return;
      }
      for (;;) {
        const page = await api<{ items: ChatMessage[] }>(
          `/rooms/${roomID}/chat?after=${lastChatID.current}`,
        );
        if (selectedRef.current !== roomID) return;
        if (page.items.length > 0) {
          lastChatID.current = page.items[page.items.length - 1].id;
          setChat((current) => [...current, ...page.items].slice(-300));
        }
        if (page.items.length < 100) return;
      }
    });
    chatQueue.current = next.catch(() => undefined);
    return next;
  }, []);

  const refresh = useCallback(async () => {
    const [r, f, i, b] = await Promise.all([
      api<{ items: Room[] }>("/rooms"),
      api<{ items: Friend[] }>("/friendships"),
      api<{ items: Invitation[] }>("/invitations"),
      api<{ items: PublicUser[] }>("/blocks"),
    ]);
    setRooms(r.items);
    setFriends(f.items);
    setInvitations(i.items);
    setBlocks(b.items);
    setDataLoading(false);
  }, []);

  useEffect(() => {
    let active = true;
    const fragment = new URLSearchParams(window.location.hash.slice(1));
    const token = fragment.get("invite");
    if (token) {
      sessionStorage.setItem("cardplay_invite", token);
      history.replaceState(null, "", window.location.pathname);
    }
    const savedToken = token ?? sessionStorage.getItem("cardplay_invite") ?? "";
    // Read once after hydration; URL fragments never reach server access logs.
    queueMicrotask(() => {
      if (active) setInviteToken(savedToken);
    });
    api<User>("/me")
      .then((u) => {
        if (active) setUser(u);
      })
      .catch((e: unknown) => {
        if (active && !(e instanceof APIError && e.status === 401))
          setError("Cannot reach CardPlay. Check that the API is running.");
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (!userID || !verified) return;
    let active = true;
    const load = () =>
      refresh().catch((e: Error) => {
        if (!active) return;
        if (sessionEnded(e)) {
          endSession(SESSION_ENDED);
          return;
        }
        setDataLoading(false);
        setError(e.message);
      });
    void load();
    const timer = setInterval(() => void load(), 15000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [userID, verified, refresh, endSession]);

  useEffect(() => {
    selectedRef.current = selected;
  }, [selected]);

  useEffect(() => {
    const log = chatLog.current;
    if (log) log.scrollTop = log.scrollHeight;
  }, [chat]);

  useEffect(() => {
    if (!selected || !userID || !verified) return;
    let active = true;
    let socket: WebSocket | undefined;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let attempt = 0;
    const loadRoom = async () => {
      const [v, invites] = await Promise.all([
        api<RoomView>(`/rooms/${selected}`),
        api<{ items: CreatedInvitation[] }>(`/rooms/${selected}/invitations`),
      ]);
      if (active) {
        setView(v);
        setCreatedInvites(invites.items);
      }
    };
    const roomGone = () => {
      setSelected(null);
      setView(null);
      setChat([]);
      setNotice("This room is no longer available to you.");
      void refresh().catch(() => undefined);
    };
    const onLoadError = (e: Error) => {
      if (!active) return;
      if (sessionEnded(e)) endSession(SESSION_ENDED);
      else if (e instanceof APIError && e.status === 404) roomGone();
      else setError(e.message);
    };
    const connect = () => {
      if (!active) return;
      setConnection("Connecting");
      socket = new WebSocket(
        `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/ws`,
      );
      socket.onopen = () => {
        attempt = 0;
        setConnection("Connected");
        socket?.send(
          JSON.stringify({
            v: 1,
            type: "room.subscribe",
            id: crypto.randomUUID(),
            room_id: selected,
          }),
        );
        void Promise.all([loadRoom(), loadChat(selected, true)]).catch(
          onLoadError,
        );
      };
      socket.onmessage = (event) => {
        const message = JSON.parse(event.data) as {
          type: string;
          payload?: RoomView;
        };
        if (message.type === "room.snapshot" && message.payload)
          setView(message.payload);
        if (message.type === "room.updated") void loadRoom().catch(onLoadError);
        if (message.type === "chat.updated")
          void loadChat(selected, false).catch(onLoadError);
      };
      socket.onclose = (event) => {
        if (!active) return;
        if (event.code === CLOSE_SESSION_ENDED) {
          endSession(SESSION_ENDED);
          return;
        }
        if (event.code === CLOSE_ROOM_UNAVAILABLE) {
          roomGone();
          return;
        }
        setConnection("Reconnecting");
        retry = setTimeout(
          connect,
          Math.min(1000 * 2 ** attempt++, 15000) + Math.random() * 300,
        );
      };
      socket.onerror = () => socket?.close();
    };
    connect();
    return () => {
      active = false;
      if (retry) clearTimeout(retry);
      socket?.close();
    };
  }, [selected, userID, verified, refresh, loadChat, endSession]);

  async function run(work: () => Promise<void>) {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await work();
    } catch (e) {
      if (sessionEnded(e)) endSession(SESSION_ENDED);
      else setError(e instanceof Error ? e.message : "Please try again");
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  }
  function openRoom(id: string | null) {
    // Reselecting the open room would clear it without triggering a reload.
    if (id === selected) return;
    setSelected(id);
    setView(null);
    setChat([]);
    setCreatedInvites([]);
    setLink("");
    lastChatID.current = 0;
  }
  function submit(
    event: FormEvent<HTMLFormElement>,
    work: (data: FormData, form: HTMLFormElement) => Promise<void>,
  ) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    void run(() => work(data, form));
  }
  async function join(payload: { token?: string; invitation_id?: string }) {
    const room = await api<Room>("/rooms/join", "POST", payload);
    await refresh();
    openRoom(room.id);
    sessionStorage.removeItem("cardplay_invite");
    setInviteToken("");
  }
  async function refreshCreatedInvites(roomID: string | null) {
    if (!roomID) return;
    const result = await api<{ items: CreatedInvitation[] }>(
      `/rooms/${roomID}/invitations`,
    );
    setCreatedInvites(result.items);
  }
  const approvedFriends = friends.filter((f) => f.status === "accepted");
  const roomFull = !!view && view.members.length >= view.room.capacity;
  const activeCreatedInvites = createdInvites.filter(
    (invite) => !invite.revoked_at && !invite.accepted_at,
  );

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <Link className="brand" href="/">
          <span className="brand-mark" aria-hidden="true">
            ♣
          </span>{" "}
          CardPlay<span className="brand-dot">.</span>
        </Link>
        <div className="nav-label">YOUR SPACE</div>
        <button
          className={`nav-item ${!selected ? "active" : ""}`}
          onClick={() => openRoom(null)}
        >
          ▦ <span>The lobby</span>
        </button>
        <div className="nav-label">
          YOUR ROOMS <span>{roomList.length}</span>
        </div>
        <nav aria-label="Your rooms">
          {user?.email_verified && !dataLoading && roomList.length === 0 && (
            <small>No rooms yet</small>
          )}
          {roomList.map((room) => (
            <button
              className={`nav-item ${selected === room.id ? "active" : ""}`}
              key={room.id}
              onClick={() => openRoom(room.id)}
            >
              {room.name}
              <span className="tiny-dot" />
            </button>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <span className="small-club">♧</span>
          <p>
            Good company.
            <br />
            Great card nights.
          </p>
          <small>Made for playing together.</small>
        </div>
      </aside>
      <main>
        <header className="topbar">
          <span>
            {selected ? "Your private table" : "Welcome to the lobby"}
          </span>
          <div className="account">
            <span className="status-dot" />
            {user ? `@${user.handle}` : "A place for your people"}
            {user && (
              <button
                className="text-button"
                disabled={busy}
                onClick={() =>
                  void run(async () => {
                    await api("/auth/logout", "POST");
                    endSession("");
                  })
                }
              >
                Sign out
              </button>
            )}
          </div>
        </header>
        <div className="content">
          {error && (
            <div className="alert" role="alert">
              {error}
              <button aria-label="Dismiss error" onClick={() => setError("")}>
                ×
              </button>
            </div>
          )}
          {notice && (
            <div className="notice" role="status">
              {notice}
            </div>
          )}
          {user?.email_verified && (
            <nav className="mobile-room-nav" aria-label="Room navigation">
              <button
                className={!selected ? "active" : ""}
                onClick={() => openRoom(null)}
              >
                Lobby
              </button>
              {roomList.map((room) => (
                <button
                  key={room.id}
                  className={selected === room.id ? "active" : ""}
                  onClick={() => openRoom(room.id)}
                >
                  {room.name}
                </button>
              ))}
            </nav>
          )}
          {loading ? (
            <p role="status">Finding your seat…</p>
          ) : !user ? (
            <>
              <section className="hero">
                <div>
                  <p className="eyebrow">YOUR NEXT GAME NIGHT STARTS HERE</p>
                  <h1>
                    A little competition.
                    <br />
                    <em>A lot of good company.</em>
                  </h1>
                  <p>
                    Bring your friends to the table. Your next card night is
                    just a room away.
                  </p>
                </div>
                <div className="hero-cards" aria-hidden="true">
                  <div className="playing-card card-one">
                    A<span>♣</span>
                    <small>A</small>
                  </div>
                  <div className="playing-card card-two">
                    K<span>♦</span>
                    <small>K</small>
                  </div>
                </div>
              </section>
              <section className="auth-card panel">
                <div>
                  <p className="eyebrow">SAVE YOUR SEAT</p>
                  <h2>
                    {authMode === "login"
                      ? "Welcome back"
                      : authMode === "register"
                        ? "Join the table"
                        : authMode === "verify"
                          ? "Verify your email"
                          : authMode === "forgot"
                            ? "Reset your password"
                            : "Set a new password"}
                  </h2>
                  <p className="muted">
                    Your account keeps friends and private rooms in one place.
                  </p>
                  <div className="tabs">
                    {(["login", "register", "verify", "forgot"] as const).map(
                      (mode) => (
                        <button
                          key={mode}
                          className={authMode === mode ? "chosen" : ""}
                          onClick={() => setAuthMode(mode)}
                        >
                          {mode === "login"
                            ? "Sign in"
                            : mode === "register"
                              ? "Create account"
                              : mode === "verify"
                                ? "Verify email"
                                : "Forgot password"}
                        </button>
                      ),
                    )}
                  </div>
                </div>
                <form
                  onSubmit={(event) =>
                    submit(event, async (data) => {
                      if (authMode === "verify") {
                        await api("/auth/verify", "POST", {
                          token: data.get("token"),
                        });
                        setNotice("Email verified. You can sign in now.");
                        setAuthMode("login");
                        return;
                      }
                      if (authMode === "forgot") {
                        await api("/auth/password/forgot", "POST", {
                          email: data.get("email"),
                        });
                        setNotice(
                          "If the account exists, a reset token is on its way.",
                        );
                        setAuthMode("reset");
                        return;
                      }
                      if (authMode === "reset") {
                        await api("/auth/password/reset", "POST", {
                          token: data.get("token"),
                          password: data.get("password"),
                        });
                        setNotice("Password updated. Sign in again.");
                        setAuthMode("login");
                        return;
                      }
                      if (authMode === "register") {
                        await api("/auth/register", "POST", {
                          email: data.get("email"),
                          password: data.get("password"),
                          handle: data.get("handle"),
                          display_name: data.get("display_name"),
                        });
                        setNotice("Check your email for a verification token.");
                        setAuthMode("verify");
                        return;
                      }
                      await api("/auth/login", "POST", {
                        email: data.get("email"),
                        password: data.get("password"),
                      });
                      const me = await api<User>("/me");
                      setUser(me);
                    })
                  }
                >
                  {authMode === "verify" || authMode === "reset" ? (
                    <label>
                      Email token
                      <input
                        name="token"
                        required
                        autoComplete="one-time-code"
                      />
                    </label>
                  ) : null}
                  {authMode === "forgot" && (
                    <label>
                      Email
                      <input
                        name="email"
                        type="email"
                        required
                        autoComplete="email"
                      />
                    </label>
                  )}
                  {authMode === "login" || authMode === "register" ? (
                    <>
                      {authMode === "register" && (
                        <div className="form-row">
                          <label>
                            Display name
                            <input
                              name="display_name"
                              required
                              maxLength={60}
                              autoComplete="nickname"
                            />
                          </label>
                          <label>
                            Handle
                            <input
                              name="handle"
                              required
                              pattern="[a-z0-9_]{3,24}"
                              autoComplete="username"
                            />
                          </label>
                        </div>
                      )}
                      <label>
                        Email
                        <input
                          name="email"
                          type="email"
                          required
                          autoComplete="email"
                          placeholder="you@example.com"
                        />
                      </label>
                      <label>
                        Password
                        <input
                          name="password"
                          type="password"
                          required
                          minLength={12}
                          maxLength={128}
                          autoComplete={
                            authMode === "login"
                              ? "current-password"
                              : "new-password"
                          }
                        />
                      </label>
                    </>
                  ) : null}
                  {authMode === "reset" && (
                    <label>
                      New password
                      <input
                        name="password"
                        type="password"
                        required
                        minLength={12}
                        maxLength={128}
                        autoComplete="new-password"
                      />
                    </label>
                  )}
                  <button className="primary" disabled={busy}>
                    {busy
                      ? "One moment…"
                      : authMode === "login"
                        ? "Take your seat →"
                        : authMode === "register"
                          ? "Create account →"
                          : authMode === "verify"
                            ? "Verify email →"
                            : authMode === "forgot"
                              ? "Send reset token →"
                              : "Save new password →"}
                  </button>
                </form>
              </section>
            </>
          ) : !user.email_verified ? (
            <section className="panel">
              <h1>One more step.</h1>
              <p>Verify your email to create rooms and invite friends.</p>
              <form
                onSubmit={(e) =>
                  submit(e, async (data) => {
                    await api("/auth/verify", "POST", {
                      token: data.get("token"),
                    });
                    setUser(await api<User>("/me"));
                  })
                }
              >
                <label>
                  Verification token
                  <input name="token" required />
                </label>
                <button className="primary" disabled={busy}>
                  Verify email
                </button>
              </form>
              <button
                className="text-button"
                disabled={busy}
                onClick={() =>
                  void run(async () => {
                    await api("/auth/verify/resend", "POST", {
                      email: (await api<{ email: string }>("/me")).email,
                    });
                    setNotice(
                      "If verification is still needed, a fresh token is on its way.",
                    );
                  })
                }
              >
                Resend token
              </button>
              <form
                onSubmit={(event) =>
                  submit(event, async (data) => {
                    if (
                      !window.confirm(
                        "Delete your account and sign out? This cannot be undone.",
                      )
                    )
                      return;
                    await api("/me", "DELETE", {
                      password: data.get("password"),
                    });
                    endSession("Account deleted.");
                  })
                }
              >
                <label>
                  Delete account
                  <input
                    name="password"
                    type="password"
                    required
                    autoComplete="current-password"
                    placeholder="Confirm with your password"
                  />
                </label>
                <button className="text-button" disabled={busy}>
                  Delete my account
                </button>
              </form>
            </section>
          ) : selected ? (
            <>
              <div className="section-heading">
                <div>
                  <p className="eyebrow">MONOPOLY DEAL · PRIVATE ROOM</p>
                  <h1>{view?.room.name ?? "Opening your table…"}</h1>
                </div>
                <span className="pill">{connection}</span>
              </div>
              <div className="room-grid">
                <section className="panel">
                  <h2>A seat for everyone</h2>
                  {!view && (
                    <p role="status" className="muted">
                      Loading your private room…
                    </p>
                  )}
                  <p className="muted">
                    {view?.members.length ?? 0} of {view?.room.capacity ?? 5}{" "}
                    seats filled
                  </p>
                  <div className="seat-list">
                    {view?.members.map((member) => (
                      <div className="seat" key={member.id}>
                        <span className="avatar">
                          {member.display_name.slice(0, 1).toUpperCase()}
                        </span>
                        <div>
                          <strong>{member.display_name}</strong>
                          <small>
                            @{member.handle}
                            {view.room.host_id === member.id ? " · Host" : ""}
                          </small>
                        </div>
                        <span className={`pill ${member.ready ? "ready" : ""}`}>
                          {member.ready ? "Ready" : "Getting settled"}
                        </span>
                      </div>
                    ))}
                  </div>
                  <button
                    className="primary"
                    disabled={busy || !view}
                    onClick={() =>
                      void run(async () => {
                        await api(`/rooms/${selected}/ready`, "PUT", {
                          ready: !view?.members.find((m) => m.id === user.id)
                            ?.ready,
                        });
                      })
                    }
                  >
                    {view?.members.find((m) => m.id === user.id)?.ready
                      ? "Not ready yet"
                      : "I'm ready"}
                  </button>
                  <p className="release-note">
                    Rooms and chat are open. Playable Monopoly Deal is coming in
                    the next milestone.
                  </p>
                  {view &&
                  view.room.status === "waiting" &&
                  view.room.host_id === user.id ? (
                    <div className="room-controls">
                      <h2>Host controls</h2>
                      <form
                        onSubmit={(event) =>
                          submit(event, async (data) => {
                            await api(`/rooms/${selected}`, "PATCH", {
                              name: data.get("name"),
                              capacity: Number(data.get("capacity")),
                            });
                            await refresh();
                            setNotice("Room updated.");
                          })
                        }
                      >
                        <label>
                          Room name
                          <input
                            name="name"
                            key={`${view.room.id}:${view.room.name}`}
                            defaultValue={view.room.name}
                            maxLength={80}
                            required
                          />
                        </label>
                        <label>
                          Maximum players
                          <select
                            name="capacity"
                            key={`${view.room.id}:${view.room.capacity}`}
                            defaultValue={view.room.capacity}
                          >
                            {[2, 3, 4, 5].map((n) => (
                              <option key={n} value={n}>
                                {n} players
                              </option>
                            ))}
                          </select>
                        </label>
                        <button className="secondary" disabled={busy}>
                          Save room
                        </button>
                      </form>
                      {view.members
                        .filter((member) => member.id !== user.id)
                        .map((member) => (
                          <div className="friend-row" key={member.id}>
                            <span>@{member.handle}</span>
                            <button
                              className="text-button"
                              disabled={busy}
                              onClick={() =>
                                void run(async () => {
                                  await api(`/rooms/${selected}/host`, "PUT", {
                                    user_id: member.id,
                                  });
                                  setNotice(`@${member.handle} is now host.`);
                                })
                              }
                            >
                              Make host
                            </button>
                            <button
                              className="text-button"
                              disabled={busy}
                              onClick={() =>
                                void run(async () => {
                                  if (
                                    !window.confirm(
                                      `Remove @${member.handle} from this room?`,
                                    )
                                  )
                                    return;
                                  await api(
                                    `/rooms/${selected}/members/${member.id}`,
                                    "DELETE",
                                  );
                                  setNotice(`@${member.handle} was removed.`);
                                })
                              }
                            >
                              Remove
                            </button>
                          </div>
                        ))}
                      <button
                        className="text-button"
                        disabled={busy}
                        onClick={() =>
                          void run(async () => {
                            if (!window.confirm("Close this private room?"))
                              return;
                            await api(`/rooms/${selected}`, "DELETE");
                            openRoom(null);
                            await refresh();
                            setNotice("Room closed.");
                          })
                        }
                      >
                        Close room
                      </button>
                    </div>
                  ) : view && view.room.status === "waiting" ? (
                    <button
                      className="text-button"
                      disabled={busy}
                      onClick={() =>
                        void run(async () => {
                          await api(`/rooms/${selected}/leave`, "POST");
                          openRoom(null);
                          await refresh();
                          setNotice("You left the room.");
                        })
                      }
                    >
                      Leave room
                    </button>
                  ) : null}
                </section>
                <section className="panel">
                  <h2>Invite your people</h2>
                  <p className="muted">
                    Links expire after 30 minutes. A seat is held only once your
                    friend joins.
                  </p>
                  {roomFull && (
                    <p className="notice" role="status">
                      This room is full. Increase the limit or remove a player
                      before inviting anyone else.
                    </p>
                  )}
                  <button
                    className="secondary"
                    disabled={busy || roomFull || !view}
                    onClick={() =>
                      void run(async () => {
                        const invite = await api<{ token: string }>(
                          `/rooms/${selected}/invitations`,
                          "POST",
                          {},
                        );
                        setLink(`${location.origin}/#invite=${invite.token}`);
                        await refreshCreatedInvites(selected);
                      })
                    }
                  >
                    Create an invite link ↗
                  </button>
                  {link && (
                    <label>
                      Share this link
                      <input
                        readOnly
                        value={link}
                        onFocus={(e) => e.currentTarget.select()}
                      />
                      <button
                        className="text-button"
                        onClick={() =>
                          void run(async () => {
                            await navigator.clipboard.writeText(link);
                            setNotice("Invite link copied.");
                          })
                        }
                      >
                        Copy link
                      </button>
                    </label>
                  )}
                  {approvedFriends
                    .filter(
                      (friend) =>
                        !view?.members.some(
                          (member) => member.id === friend.id,
                        ),
                    )
                    .map((friend) => (
                      <div className="friend-row" key={friend.id}>
                        <span>@{friend.handle}</span>
                        <button
                          className="text-button"
                          disabled={busy || roomFull || !view}
                          onClick={() =>
                            void run(async () => {
                              await api(
                                `/rooms/${selected}/invitations`,
                                "POST",
                                { target_id: friend.id },
                              );
                              await refreshCreatedInvites(selected);
                              setNotice(
                                `Invitation sent to @${friend.handle}.`,
                              );
                            })
                          }
                        >
                          Invite
                        </button>
                      </div>
                    ))}
                  {activeCreatedInvites.length === 0 && (
                    <p className="muted">No active invitations from you yet.</p>
                  )}
                  {activeCreatedInvites.map((invite) => (
                    <div className="friend-row" key={invite.id}>
                      <span>
                        {invite.target_id ? "Friend invitation" : "Invite link"}
                        <small>
                          Expires {new Date(invite.expires_at).toLocaleString()}
                        </small>
                      </span>
                      <button
                        className="text-button"
                        disabled={busy}
                        onClick={() =>
                          void run(async () => {
                            await api(`/invitations/${invite.id}`, "DELETE");
                            setLink("");
                            await refreshCreatedInvites(selected);
                            setNotice("Invitation revoked.");
                          })
                        }
                      >
                        Revoke
                      </button>
                    </div>
                  ))}
                </section>
                <section className="panel chat-panel">
                  <div className="section-heading">
                    <h2>At the table</h2>
                    <span className="muted">Room chat</span>
                  </div>
                  <div
                    className="chat-log"
                    ref={chatLog}
                    role="log"
                    aria-live="polite"
                    aria-label="Room chat"
                  >
                    {chat.length === 0 ? (
                      <p className="empty">Say hello. The table is yours.</p>
                    ) : (
                      chat.map((msg) => (
                        <div className="message" key={msg.id}>
                          <strong>{msg.display_name}</strong>
                          <time>
                            {new Date(msg.created_at).toLocaleTimeString([], {
                              hour: "2-digit",
                              minute: "2-digit",
                            })}
                          </time>
                          <p>{msg.body}</p>
                        </div>
                      ))
                    )}
                  </div>
                  <form
                    className="chat-form"
                    onSubmit={(event) =>
                      submit(event, async (data, form) => {
                        const body = String(data.get("body"));
                        if (chatDraft.current?.body !== body)
                          chatDraft.current = { body, id: crypto.randomUUID() };
                        await api(`/rooms/${selected}/chat`, "POST", {
                          body,
                          client_id: chatDraft.current.id,
                        });
                        chatDraft.current = null;
                        form.reset();
                        // Show the message even if the socket is reconnecting.
                        await loadChat(selected, false);
                      })
                    }
                  >
                    <label className="sr-only" htmlFor="chat-body">
                      Message
                    </label>
                    <input
                      id="chat-body"
                      name="body"
                      required
                      maxLength={500}
                      placeholder="Send a little table talk…"
                    />
                    <button className="primary" disabled={busy}>
                      Send
                    </button>
                  </form>
                </section>
              </div>
            </>
          ) : (
            <>
              <section className="hero">
                <div>
                  <p className="eyebrow">
                    GOOD TO HAVE YOU HERE, {user.display_name.toUpperCase()}
                  </p>
                  <h1>
                    Make room
                    <br />
                    <em>for a good time.</em>
                  </h1>
                  <p>Your friends. Your table. A little friendly rivalry.</p>
                  <a className="primary button-link" href="#new-room">
                    ＋ Create a room
                  </a>
                </div>
                <div className="hero-cards" aria-hidden="true">
                  <div className="playing-card card-one">
                    A<span>♣</span>
                    <small>A</small>
                  </div>
                  <div className="playing-card card-two">
                    K<span>♦</span>
                    <small>K</small>
                  </div>
                </div>
              </section>
              <div className="section-heading">
                <h2>Pick your next game</h2>
                <span className="muted">One table. More ways to play.</span>
              </div>
              {dataLoading && (
                <p role="status" className="muted">
                  Loading your rooms and friends…
                </p>
              )}
              <div className="game-grid">
                <article className="game-card">
                  <div className="game-art">
                    <span>▥</span>
                    <span>↗</span>
                    <span>⌂</span>
                  </div>
                  <div className="game-info">
                    <span className="pill ready">Lobby available</span>
                    <h2>Monopoly Deal</h2>
                    <p>Collect, trade, and plot your next move.</p>
                    <small>2–5 players · US 01723 edition</small>
                  </div>
                </article>
                <article className="game-card upcoming">
                  <div className="cambio-art" aria-hidden="true">
                    ?<span>↻</span>
                  </div>
                  <div className="game-info">
                    <span className="pill">Coming later</span>
                    <h2>Cambio</h2>
                    <p>Keep your cards close. And your memory closer.</p>
                    <small>More game nights on the horizon</small>
                  </div>
                </article>
              </div>
              <div className="room-grid">
                <section className="panel" id="new-room">
                  <h2>Set the table</h2>
                  <form
                    onSubmit={(event) =>
                      submit(event, async (data) => {
                        const room = await api<Room>("/rooms", "POST", {
                          name: data.get("name"),
                          capacity: Number(data.get("capacity")),
                        });
                        await refresh();
                        openRoom(room.id);
                      })
                    }
                  >
                    <label>
                      Room name
                      <input
                        name="name"
                        placeholder="Friday night crew"
                        maxLength={80}
                        required
                      />
                    </label>
                    <label>
                      Seats
                      <select name="capacity" defaultValue="5">
                        {[2, 3, 4, 5].map((n) => (
                          <option key={n} value={n}>
                            {n} players
                          </option>
                        ))}
                      </select>
                    </label>
                    <button className="primary" disabled={busy}>
                      Create private room →
                    </button>
                  </form>
                </section>
                <section className="panel">
                  <h2>Have an invitation?</h2>
                  <form
                    onSubmit={(event) =>
                      submit(event, async (data) => {
                        let token = String(data.get("token")).trim();
                        if (token.includes("#"))
                          token =
                            new URLSearchParams(token.split("#")[1]).get(
                              "invite",
                            ) ?? "";
                        await join({ token });
                      })
                    }
                  >
                    <label>
                      Invite link or token
                      <input
                        name="token"
                        value={inviteToken}
                        onChange={(e) => setInviteToken(e.target.value)}
                        required
                        placeholder="Paste your invitation"
                      />
                    </label>
                    <button className="secondary" disabled={busy}>
                      Join a room
                    </button>
                  </form>
                  {invitations.map((invite) => (
                    <div className="friend-row" key={invite.id}>
                      <span>{invite.name}</span>
                      <button
                        className="text-button"
                        disabled={busy}
                        onClick={() =>
                          void run(() => join({ invitation_id: invite.id }))
                        }
                      >
                        Join →
                      </button>
                    </div>
                  ))}
                  {!dataLoading && invitations.length === 0 && (
                    <p className="muted">
                      No personal invitations right now. A link from a friend
                      works here too.
                    </p>
                  )}
                </section>
                <section className="panel">
                  <h2>Better with friends</h2>
                  <form
                    className="inline-form"
                    onSubmit={(event) =>
                      submit(event, async (data) => {
                        const found = await api<PublicUser>(
                          `/users?handle=${encodeURIComponent(String(data.get("handle")))}`,
                        );
                        setSearchResult(found);
                      })
                    }
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
                    <button className="secondary" disabled={busy}>
                      Find
                    </button>
                  </form>
                  {searchResult && (
                    <div className="friend-row">
                      <span>
                        {searchResult.display_name}
                        <small>@{searchResult.handle}</small>
                      </span>
                      {searchResult.id !== user.id && (
                        <>
                          <button
                            className="text-button"
                            disabled={busy}
                            onClick={() =>
                              void run(async () => {
                                const result = await api<
                                  { status: string } | undefined
                                >(
                                  `/friendships/${searchResult.id}/request`,
                                  "POST",
                                );
                                await refresh();
                                setNotice(
                                  result?.status === "accepted"
                                    ? `You and @${searchResult.handle} are now friends.`
                                    : "Friend request sent.",
                                );
                              })
                            }
                          >
                            Request
                          </button>
                          <button
                            className="text-button"
                            disabled={busy}
                            onClick={() =>
                              void run(async () => {
                                await api(
                                  `/friendships/${searchResult.id}/block`,
                                  "POST",
                                );
                                setSearchResult(null);
                                await refresh();
                                setNotice("Account blocked.");
                              })
                            }
                          >
                            Block
                          </button>
                        </>
                      )}
                    </div>
                  )}
                  {dataLoading && (
                    <p role="status" className="muted">
                      Loading friends and invitations…
                    </p>
                  )}
                  {!dataLoading && friends.length === 0 && (
                    <p className="muted">
                      No friends yet. Search by an exact handle to send a
                      request.
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
                            className="text-button"
                            disabled={busy}
                            onClick={() =>
                              void run(async () => {
                                await api(
                                  `/friendships/${friend.id}/accept`,
                                  "POST",
                                );
                                await refresh();
                              })
                            }
                          >
                            Accept
                          </button>
                        )}
                      <button
                        className="text-button"
                        disabled={busy}
                        onClick={() =>
                          void run(async () => {
                            await api(
                              `/friendships/${friend.id}/${friend.status === "pending" && friend.recipient_id === user.id ? "decline" : "remove"}`,
                              "POST",
                            );
                            await refresh();
                          })
                        }
                      >
                        {friend.status === "accepted"
                          ? "Remove"
                          : friend.recipient_id === user.id
                            ? "Decline"
                            : "Cancel"}
                      </button>
                      <button
                        className="text-button"
                        disabled={busy}
                        onClick={() =>
                          void run(async () => {
                            await api(
                              `/friendships/${friend.id}/block`,
                              "POST",
                            );
                            await refresh();
                            setNotice(`@${friend.handle} blocked.`);
                          })
                        }
                      >
                        Block
                      </button>
                    </div>
                  ))}
                  {blocks.length > 0 && <h3>Blocked accounts</h3>}
                  {blocks.map((blocked) => (
                    <div className="friend-row" key={blocked.id}>
                      <span>@{blocked.handle}</span>
                      <button
                        className="text-button"
                        disabled={busy}
                        onClick={() =>
                          void run(async () => {
                            await api(
                              `/friendships/${blocked.id}/unblock`,
                              "POST",
                            );
                            await refresh();
                            setNotice(`@${blocked.handle} unblocked.`);
                          })
                        }
                      >
                        Unblock
                      </button>
                    </div>
                  ))}
                </section>
              </div>
              <section className="panel account-settings">
                <h2>Your account</h2>
                <p className="muted">
                  @{user.handle} · {user.email}
                </p>
                <form
                  className="inline-form"
                  onSubmit={(event) =>
                    submit(event, async (data) => {
                      const result = await api<{ display_name: string }>(
                        "/me",
                        "PATCH",
                        { display_name: data.get("display_name") },
                      );
                      setUser({ ...user, display_name: result.display_name });
                      setNotice("Display name updated.");
                    })
                  }
                >
                  <label>
                    Display name
                    <input
                      name="display_name"
                      defaultValue={user.display_name}
                      required
                      maxLength={60}
                    />
                  </label>
                  <button className="secondary" disabled={busy}>
                    Save
                  </button>
                </form>
                <button
                  className="text-button"
                  disabled={busy}
                  onClick={() =>
                    void run(async () => {
                      const result = await api<{ items: typeof sessions }>(
                        "/sessions",
                      );
                      setSessions(result.items);
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
                      className="text-button"
                      disabled={busy}
                      onClick={() =>
                        void run(async () => {
                          await api(`/sessions/${session.id}`, "DELETE");
                          setSessions(
                            sessions.filter((item) => item.id !== session.id),
                          );
                          // Fails with UNAUTHENTICATED, signing this page out,
                          // when the revoked session was the current one.
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
                  onSubmit={(event) =>
                    submit(event, async (data, form) => {
                      await api("/me/password", "PUT", {
                        current_password: data.get("current_password"),
                        new_password: data.get("new_password"),
                      });
                      form.reset();
                      endSession("Password changed. Sign in again.");
                    })
                  }
                >
                  <h3>Change password</h3>
                  <div className="form-row">
                    <label>
                      Current password
                      <input
                        name="current_password"
                        type="password"
                        autoComplete="current-password"
                        required
                      />
                    </label>
                    <label>
                      New password
                      <input
                        name="new_password"
                        type="password"
                        autoComplete="new-password"
                        minLength={12}
                        maxLength={128}
                        required
                      />
                    </label>
                  </div>
                  <button className="secondary" disabled={busy}>
                    Update password
                  </button>
                </form>
                <form
                  onSubmit={(event) =>
                    submit(event, async (data, form) => {
                      if (
                        !window.confirm(
                          "Delete your account and sign out? This cannot be undone.",
                        )
                      )
                        return;
                      await api("/me", "DELETE", {
                        password: data.get("password"),
                      });
                      form.reset();
                      endSession("Account deleted.");
                    })
                  }
                >
                  <label>
                    Delete account
                    <input
                      name="password"
                      type="password"
                      required
                      autoComplete="current-password"
                      placeholder="Confirm with your password"
                    />
                  </label>
                  <button className="text-button" disabled={busy}>
                    Delete my account
                  </button>
                </form>
              </section>
            </>
          )}
          <footer>
            <span>♣ CardPlay</span>
            <span>A little less scrolling. A little more playing.</span>
          </footer>
        </div>
      </main>
    </div>
  );
}
