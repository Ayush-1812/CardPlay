"use client";

// The app shell: entry (guest or sign-in) → choose a game → that game's
// lobby (name, create or join) → the room → the live table. All session,
// room, chat and match state lives here; screens only render it.

import { useCallback, useEffect, useRef, useState } from "react";
import {
  api,
  APIError,
  ChatMessage,
  Friend,
  Invitation,
  PublicUser,
  Room,
  RoomView,
  User,
} from "../lib/api";
import { CardInfo, Cards, MatchState } from "../lib/game";
import { ChatPanel } from "./chat";
import { GameTable } from "./game-table";
import { TrumpTable } from "./trump/trump-table";
import { TrumpMatch } from "../lib/trump";
import { AccountSheet, FriendsSheet } from "./screens/account";
import { EntryScreen, VerifyScreen } from "./screens/entry";
import { GameEntry, GameLobby, GamesScreen } from "./screens/home";
import { RoomScreen } from "./screens/room";

const SESSION_ENDED = "Your session ended. Sign in again.";
// Set only when the API is published on its own origin, which Vercel-style
// deployments need because they cannot proxy a WebSocket. Empty means the
// socket is same-origin, proxied next to /api by the web server.
const API_ORIGIN = (process.env.NEXT_PUBLIC_API_ORIGIN ?? "").replace(
  /\/$/,
  "",
);
// Server WebSocket close codes; any other close is transient and retried.
const CLOSE_SESSION_ENDED = 4001;
const CLOSE_ROOM_UNAVAILABLE = 4004;
// Another tab or device took control of this seat; do not reconnect on our own
// or the two tabs would keep taking the seat from each other.
const CLOSE_REPLACED = 4009;

type Reply = { type: string; payload?: { code?: string; message?: string } };

function liveMatch(status?: string) {
  return status === "playing" || status === "paused";
}

// Engine messages look like "NO_PLAYS_LEFT: this needs 1 play(s)"; show the prose.
function ruleMessage(message = "") {
  const text = message.replace(/^[A-Z_]+: /, "");
  return text.charAt(0).toUpperCase() + text.slice(1);
}

function sessionEnded(e: unknown) {
  return (
    e instanceof APIError && e.status === 401 && e.code === "UNAUTHENTICATED"
  );
}

// An invite link (…#invite=<token>) or a bare token.
function inviteToken(text: string) {
  const t = text.trim();
  if (t.includes("#"))
    return new URLSearchParams(t.split("#")[1]).get("invite") ?? "";
  return t;
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
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [mutes, setMutes] = useState<PublicUser[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [view, setView] = useState<RoomView | null>(null);
  const [chat, setChat] = useState<ChatMessage[]>([]);
  const [connection, setConnection] = useState("Offline");
  const [link, setLink] = useState("");
  const [invite, setInvite] = useState("");
  const [game, setGame] = useState<GameEntry | null>(null);
  const [sheet, setSheet] = useState<"account" | "friends" | null>(null);
  const [match, setMatch] = useState<MatchState | null>(null);
  const [cards, setCards] = useState<Cards>({});
  const [replaced, setReplaced] = useState(false);
  const [socketNonce, setSocketNonce] = useState(0);
  const [gameBusy, setGameBusy] = useState(false);
  const [dismissedMatch, setDismissedMatch] = useState<string | null>(null);
  const [chatVisible, setChatVisible] = useState(true);
  const [chatBoundary, setChatBoundary] = useState(0);
  const sendRef = useRef<
    ((frame: Record<string, unknown>) => Promise<Reply>) | null
  >(null);
  const matchRef = useRef<MatchState | null>(null);
  const gameBusyRef = useRef(false);
  const chatPanel = useRef<HTMLElement>(null);
  const chatDraft = useRef<{ body: string; id: string } | null>(null);
  const busyRef = useRef(false);
  const selectedRef = useRef<string | null>(null);
  const lastChatID = useRef(0);
  const chatQueue = useRef<Promise<void>>(Promise.resolve());
  const chatLog = useRef<HTMLDivElement>(null);
  const linkFor = useRef<string | null>(null);
  const joining = useRef(false);
  const userID = user?.id;
  const guest = !!user?.is_guest;
  // Guests and verified accounts may use rooms, games and chat.
  const canPlay = !!user && (user.email_verified || guest);

  // Clears everything tied to the signed-in account.
  const endSession = useCallback((message: string) => {
    sessionStorage.removeItem("cardplay_room");
    setUser(null);
    setRooms([]);
    setFriends([]);
    setBlocks([]);
    setInvitations([]);
    setSelected(null);
    setView(null);
    setChat([]);
    setLink("");
    setConnection("Offline");
    setMatch(null);
    setMutes([]);
    setReplaced(false);
    setGame(null);
    setSheet(null);
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

  // Rooms you are in, mutes and (for accounts) friends and invitations.
  const refresh = useCallback(async () => {
    const [r, m] = await Promise.all([
      api<{ items: Room[] }>("/rooms"),
      api<{ items: PublicUser[] }>("/mutes"),
    ]);
    setRooms(r.items.filter((room) => room.status !== "closed"));
    setMutes(m.items);
    if (guest) return;
    const [f, i, b] = await Promise.all([
      api<{ items: Friend[] }>("/friendships"),
      api<{ items: Invitation[] }>("/invitations"),
      api<{ items: PublicUser[] }>("/blocks"),
    ]);
    setFriends(f.items);
    setInvitations(i.items);
    setBlocks(b.items);
  }, [guest]);

  useEffect(() => {
    let active = true;
    const fragment = new URLSearchParams(window.location.hash.slice(1));
    const token = fragment.get("invite");
    if (token) {
      sessionStorage.setItem("cardplay_invite", token);
      history.replaceState(null, "", window.location.pathname);
    }
    const savedToken = token ?? sessionStorage.getItem("cardplay_invite") ?? "";
    // Reopen this tab's room after a refresh (PRD P08); the server still
    // authorizes it and an unavailable room falls back to the games screen.
    const savedRoom = sessionStorage.getItem("cardplay_room");
    queueMicrotask(() => {
      if (!active) return;
      setInvite(savedToken);
      if (savedRoom) setSelected(savedRoom);
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
    // Opening an invite link in a tab that already shows CardPlay changes
    // only the URL fragment, which does not reload the page.
    const onHash = () => {
      const t = new URLSearchParams(window.location.hash.slice(1)).get(
        "invite",
      );
      if (!t) return;
      sessionStorage.setItem("cardplay_invite", t);
      history.replaceState(null, "", window.location.pathname);
      setInvite(t);
    };
    window.addEventListener("hashchange", onHash);
    return () => {
      active = false;
      window.removeEventListener("hashchange", onHash);
    };
  }, []);

  useEffect(() => {
    if (!userID || !canPlay) return;
    let active = true;
    const load = () =>
      refresh().catch((e: Error) => {
        if (!active) return;
        if (sessionEnded(e)) {
          endSession(SESSION_ENDED);
          return;
        }
        setError(e.message);
      });
    void load();
    const timer = setInterval(() => void load(), 15000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [userID, canPlay, refresh, endSession]);

  useEffect(() => {
    selectedRef.current = selected;
  }, [selected]);

  useEffect(() => {
    matchRef.current = match;
  }, [match]);

  // Public card metadata for the table (no hidden state).
  useEffect(() => {
    if (!canPlay) return;
    let active = true;
    api<{ items: CardInfo[] }>("/games/monopoly-deal/cards")
      .then((r) => {
        if (active) setCards(Object.fromEntries(r.items.map((c) => [c.id, c])));
      })
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, [canPlay]);

  // Unread chat: while the chat panel is off screen, messages after the
  // point it left view count as new.
  useEffect(() => {
    const panel = chatPanel.current;
    if (!panel || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(([entry]) => {
      if (!entry.isIntersecting) setChatBoundary(lastChatID.current);
      setChatVisible(entry.isIntersecting);
    });
    observer.observe(panel);
    return () => observer.disconnect();
  }, [selected, view, match?.status]);

  useEffect(() => {
    const log = chatLog.current;
    if (log) log.scrollTop = log.scrollHeight;
  }, [chat]);

  useEffect(() => {
    if (!selected || !userID || !canPlay) return;
    let active = true;
    let socket: WebSocket | undefined;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let attempt = 0;
    let subscribed: string | null = null;
    const replies = new Map<string, (reply: Reply) => void>();
    const offline: Reply = {
      type: "error",
      payload: {
        code: "OFFLINE",
        message: "Reconnecting to the table. Try again in a moment.",
      },
    };
    sendRef.current = (frame) =>
      new Promise((resolve) => {
        if (!socket || socket.readyState !== WebSocket.OPEN) {
          resolve(offline);
          return;
        }
        const id = crypto.randomUUID();
        replies.set(id, resolve);
        socket.send(JSON.stringify({ v: 1, id, ...frame }));
      });
    // Follow the room's match: subscribe to a live one (taking control of
    // our seat) or fetch the final result of an ended one.
    const syncMatch = (v: RoomView) => {
      const m = v.match;
      if (
        !m ||
        subscribed === m.id ||
        !socket ||
        socket.readyState !== WebSocket.OPEN
      )
        return;
      subscribed = m.id;
      if (liveMatch(m.status)) {
        socket.send(
          JSON.stringify({
            v: 1,
            id: crypto.randomUUID(),
            type: "match.subscribe",
            match_id: m.id,
          }),
        );
      } else {
        void api<MatchState>(`/matches/${m.id}`)
          .then((st) => {
            if (active) setMatch(st);
          })
          .catch(() => undefined);
      }
    };
    const loadRoom = async () => {
      const v = await api<RoomView>(`/rooms/${selected}`);
      if (active) {
        setView(v);
        syncMatch(v);
      }
    };
    const roomGone = () => {
      sessionStorage.removeItem("cardplay_room");
      setSelected(null);
      setView(null);
      setChat([]);
      setMatch(null);
      setNotice("That room has closed.");
      void refresh().catch(() => undefined);
    };
    const onLoadError = (e: Error) => {
      if (!active) return;
      if (sessionEnded(e)) endSession(SESSION_ENDED);
      else if (e instanceof APIError && e.status === 404) roomGone();
      else setError(e.message);
    };
    // Where the socket lives. Same origin by default, which is how the
    // Docker image and local development run. When the API is published on
    // its own origin the session cookie cannot travel with the handshake, so
    // the client asks for a one-shot ticket over the authenticated HTTP path.
    const socketURL = async () => {
      if (!API_ORIGIN)
        return `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/ws`;
      const { ticket } = await api<{ ticket: string }>(
        "/realtime/ticket",
        "POST",
      );
      return `${API_ORIGIN.replace(/^http/, "ws")}/ws?ticket=${encodeURIComponent(ticket)}`;
    };
    const connect = () => {
      if (!active) return;
      setConnection("Connecting");
      void socketURL().then(
        (url) => {
          if (!active) return;
          socket = new WebSocket(url);
          socket.onopen = () => {
            attempt = 0;
            subscribed = null;
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
              id?: string;
              payload?: unknown;
            };
            const reply = message.id ? replies.get(message.id) : undefined;
            if (reply) {
              replies.delete(message.id!);
              reply(message as Reply);
            }
            if (message.type === "room.snapshot" && message.payload) {
              setView(message.payload as RoomView);
              syncMatch(message.payload as RoomView);
            }
            if (message.type === "match.state" && message.payload) {
              // Stamp arrival so the turn clock counts down locally without
              // depending on the client's clock matching the server's.
              const next = {
                ...(message.payload as MatchState),
                received_at: Date.now(),
              };
              // Pushes may overtake each other; keep the newest version.
              setMatch((current) =>
                !current ||
                current.match_id !== next.match_id ||
                next.version >= current.version
                  ? next
                  : current,
              );
            }
            if (message.type === "room.updated")
              void loadRoom().catch(onLoadError);
            if (message.type === "chat.updated")
              void loadChat(selected, false).catch(onLoadError);
          };
          socket.onclose = (event) => {
            for (const resolve of replies.values()) resolve(offline);
            replies.clear();
            if (!active) return;
            if (event.code === CLOSE_REPLACED) {
              setReplaced(true);
              setConnection("Open in another tab");
              return;
            }
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
        },
        (e: Error) => {
          if (!active) return;
          if (sessionEnded(e)) {
            endSession(SESSION_ENDED);
            return;
          }
          setConnection("Reconnecting");
          retry = setTimeout(
            connect,
            Math.min(1000 * 2 ** attempt++, 15000) + Math.random() * 300,
          );
        },
      );
    };
    connect();
    return () => {
      active = false;
      sendRef.current = null;
      if (retry) clearTimeout(retry);
      socket?.close();
    };
  }, [selected, userID, canPlay, refresh, loadChat, endSession, socketNonce]);

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
  // command sends one game intent over the socket and waits for the durable
  // acknowledgement. The server decides the result; the table then updates
  // from the pushed projection.
  async function command(kind: string, payload: object): Promise<boolean> {
    const m = matchRef.current;
    const send = sendRef.current;
    if (!m || !send || gameBusyRef.current) return false;
    gameBusyRef.current = true;
    setGameBusy(true);
    setError("");
    try {
      const reply = await send({
        type: "game.command",
        match_id: m.match_id,
        payload: {
          command_id: crypto.randomUUID(),
          expected_revision: m.revision,
          kind,
          payload,
        },
      });
      if (reply.type === "ack") return true;
      const p = reply.payload ?? {};
      setError(
        p.code === "STALE_REVISION"
          ? "The table changed before your move arrived. Check the table and try again."
          : ruleMessage(p.message),
      );
      return false;
    } finally {
      gameBusyRef.current = false;
      setGameBusy(false);
    }
  }
  function openRoom(id: string | null) {
    // Reselecting the open room would clear it without triggering a reload.
    if (id === selected) return;
    // Leaving a room returns to the games list, not the last game lobby.
    setGame(null);
    setMatch(null);
    setReplaced(false);
    setSelected(id);
    if (id) sessionStorage.setItem("cardplay_room", id);
    else sessionStorage.removeItem("cardplay_room");
    setView(null);
    setChat([]);
    setLink("");
    lastChatID.current = 0;
  }
  async function join(payload: { token?: string; invitation_id?: string }) {
    const room = await api<Room>("/rooms/join", "POST", payload);
    await refresh();
    openRoom(room.id);
    sessionStorage.removeItem("cardplay_invite");
    setInvite("");
  }
  // A different name for this table updates the player's name everywhere.
  async function applyName(name: string) {
    if (!user || !name || name === user.display_name) return;
    const result = await api<{ display_name: string }>("/me", "PATCH", {
      display_name: name,
    });
    setUser({ ...user, display_name: result.display_name });
  }
  async function newLink(roomID: string) {
    const created = await api<{ token: string }>(
      `/rooms/${roomID}/invitations`,
      "POST",
      {},
    );
    setLink(`${location.origin}/#invite=${created.token}`);
  }
  async function signOut() {
    await api("/auth/logout", "POST");
    endSession("");
  }

  // An invite link opened before signing in is used as soon as the player
  // has a name (guest) or an account.
  useEffect(() => {
    if (!canPlay || !invite || joining.current) return;
    joining.current = true;
    const token = inviteToken(invite);
    queueMicrotask(() => {
      void run(async () => {
        try {
          await join({ token });
        } finally {
          joining.current = false;
          sessionStorage.removeItem("cardplay_invite");
          setInvite("");
        }
      });
    });
    // run is recreated each render and only reads refs and setters.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [canPlay, invite]);

  // Every waiting room offers a fresh invite link right away.
  useEffect(() => {
    if (!view) return;
    const room = view.room;
    if (
      room.status !== "waiting" ||
      view.members.length >= room.capacity ||
      linkFor.current === room.id
    )
      return;
    linkFor.current = room.id;
    void newLink(room.id).catch(() => undefined);
    // newLink only uses setters and the room id.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view?.room.id, view?.room.status]);

  const roomMatch = match && match.room_id === selected ? match : null;
  const showTable = !!roomMatch && liveMatch(roomMatch.status);
  const showResult =
    !!roomMatch && !showTable && dismissedMatch !== roomMatch.match_id;
  const unread = chatVisible
    ? 0
    : chat.filter((m) => m.id > chatBoundary && m.user_id !== user?.id).length;

  const chatSection =
    selected && user ? (
      <ChatPanel
        messages={chat}
        me={user.id}
        busy={busy}
        panelRef={chatPanel}
        logRef={chatLog}
        onSend={async (body) => {
          let sent = false;
          await run(async () => {
            if (chatDraft.current?.body !== body)
              chatDraft.current = { body, id: crypto.randomUUID() };
            await api(`/rooms/${selected}/chat`, "POST", {
              body,
              client_id: chatDraft.current.id,
            });
            chatDraft.current = null;
            sent = true;
            // Show the message even if the socket is reconnecting.
            await loadChat(selected, false);
          });
          return sent;
        }}
        onReport={(msg) =>
          void run(async () => {
            const reason = window.prompt("Why are you reporting this message?");
            if (!reason?.trim()) return;
            await api(`/rooms/${selected}/chat/${msg.id}/report`, "POST", {
              reason: reason.trim().slice(0, 500),
            });
            setNotice("Message reported. Thank you.");
          })
        }
        onMute={(msg) =>
          void run(async () => {
            await api(`/mutes/${msg.user_id}`, "PUT");
            await Promise.all([refresh(), loadChat(selected, true)]);
            setNotice(
              `${msg.display_name} is muted. Unmute them under Friends.`,
            );
          })
        }
      />
    ) : null;
  const alertsSection = (
    <>
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
      {replaced && (
        <div className="notice" role="status">
          This match is open in another tab or window, which now controls your
          seat.{" "}
          <button
            className="text-button"
            onClick={() => {
              setReplaced(false);
              setSocketNonce((n) => n + 1);
            }}
          >
            Play here instead
          </button>
        </div>
      )}
    </>
  );

  if (loading)
    return (
      <div className="app felt">
        <p className="splash" role="status">
          Finding your seat…
        </p>
      </div>
    );

  // The live table takes the whole screen.
  if (
    user &&
    selected &&
    showTable &&
    roomMatch &&
    roomMatch.game_id === "trump"
  ) {
    return (
      <div className="app-table">
        <TrumpTable
          state={roomMatch as unknown as TrumpMatch}
          me={user.id}
          busy={gameBusy || replaced}
          roomName={view?.room.name}
          teamNames={
            view ? [view.room.team_a ?? "", view.room.team_b ?? ""] : undefined
          }
          connection={connection}
          alerts={alertsSection}
          chat={chatSection}
          unread={unread}
          onCommand={command}
          onLeave={() =>
            void run(async () => {
              await api(`/matches/${roomMatch.match_id}/leave`, "POST");
              setNotice("You left the match. It ended with no winner.");
            })
          }
        />
      </div>
    );
  }

  if (user && selected && showTable && roomMatch) {
    return (
      <div className="app-table">
        {Object.keys(cards).length === 0 ? (
          <p className="splash" role="status">
            Loading cards…
          </p>
        ) : (
          <GameTable
            state={roomMatch}
            me={user.id}
            cards={cards}
            busy={gameBusy || replaced}
            roomName={view?.room.name}
            connection={connection}
            alerts={alertsSection}
            chat={chatSection}
            unread={unread}
            onCommand={command}
            onLeave={() =>
              void run(async () => {
                await api(`/matches/${roomMatch.match_id}/leave`, "POST");
                setNotice("You left the match. It ended with no winner.");
              })
            }
            onVote={(vote) =>
              void run(async () => {
                await api(`/matches/${roomMatch.match_id}/abandon`, "POST", {
                  vote,
                });
              })
            }
          />
        )}
      </div>
    );
  }

  let body;
  if (!user) {
    body = (
      <EntryScreen
        busy={busy}
        invited={!!invite}
        run={run}
        onUser={setUser}
        setNotice={setNotice}
      />
    );
  } else if (!canPlay) {
    body = (
      <VerifyScreen
        busy={busy}
        run={run}
        onUser={setUser}
        onSignOut={() => void run(signOut)}
        setNotice={setNotice}
      />
    );
  } else if (selected) {
    body = (
      <RoomScreen
        user={user}
        view={view}
        busy={busy}
        connection={connection}
        link={link}
        friends={friends.filter((f) => f.status === "accepted")}
        result={
          showResult &&
          roomMatch &&
          roomMatch.game_id !== "trump" &&
          Object.keys(cards).length > 0 ? (
            <section
              className="panel match-panel"
              aria-label="Last match result"
            >
              <div className="section-heading">
                <h2>Last match</h2>
                <button
                  className="text-button"
                  onClick={() => setDismissedMatch(roomMatch.match_id)}
                >
                  Hide result
                </button>
              </div>
              <GameTable
                state={roomMatch}
                me={user.id}
                cards={cards}
                busy
                onCommand={command}
                onLeave={() => undefined}
                onVote={() => undefined}
              />
            </section>
          ) : undefined
        }
        chat={chatSection}
        unreadBadge={
          unread > 0 && (
            <button
              className="unread-badge"
              onClick={() =>
                chatPanel.current?.scrollIntoView({ behavior: "smooth" })
              }
            >
              {unread} new chat message{unread === 1 ? "" : "s"} ↓
            </button>
          )
        }
        onNewLink={() => void run(() => newLink(selected))}
        onInviteFriend={(friend) =>
          void run(async () => {
            await api(`/rooms/${selected}/invitations`, "POST", {
              target_id: friend.id,
            });
            setNotice(`Invitation sent to @${friend.handle}.`);
          })
        }
        onReady={(ready) =>
          void run(async () => {
            await api(`/rooms/${selected}/ready`, "PUT", { ready });
          })
        }
        onStart={() =>
          void run(async () => {
            await api(`/rooms/${selected}/matches`, "POST");
            setDismissedMatch(roomMatch?.match_id ?? null);
          })
        }
        onLeave={() =>
          void run(async () => {
            await api(`/rooms/${selected}/leave`, "POST");
            openRoom(null);
            await refresh();
            setNotice("You left the room.");
          })
        }
        onClose={() =>
          void run(async () => {
            if (!window.confirm("Close this room for everyone?")) return;
            await api(`/rooms/${selected}`, "DELETE");
            openRoom(null);
            await refresh();
            setNotice("Room closed.");
          })
        }
        onUpdate={(name, capacity, teams) =>
          void run(async () => {
            await api(`/rooms/${selected}`, "PATCH", {
              name,
              capacity,
              ...(teams ? { team_a: teams[0], team_b: teams[1] } : {}),
            });
            setNotice("Room updated.");
          })
        }
        onSeat={(userID, seat) =>
          void run(async () => {
            await api(`/rooms/${selected}/members/${userID}/seat`, "PUT", {
              seat,
            });
            setNotice("Seats changed. Everyone needs to ready up again.");
          })
        }
        onMakeHost={(id, display) =>
          void run(async () => {
            await api(`/rooms/${selected}/host`, "PUT", { user_id: id });
            setNotice(`${display} is now the host.`);
          })
        }
        onKick={(id, display) =>
          void run(async () => {
            if (!window.confirm(`Remove ${display} from this room?`)) return;
            await api(`/rooms/${selected}/members/${id}`, "DELETE");
            setNotice(`${display} was removed.`);
          })
        }
      />
    );
  } else if (game) {
    body = (
      <GameLobby
        game={game}
        user={user}
        busy={busy}
        invite={invite}
        onBack={() => setGame(null)}
        onCreate={(name) =>
          void run(async () => {
            await applyName(name);
            const room = await api<Room>("/rooms", "POST", {
              name: `${name}'s table`.slice(0, 80),
              capacity: game.seats ?? 5,
              game: game.id,
            });
            await refresh();
            openRoom(room.id);
          })
        }
        onJoin={(text, name) =>
          void run(async () => {
            const token = inviteToken(text);
            if (!/^[0-9a-f]{64}$/.test(token))
              throw new Error("That doesn't look like an invite link.");
            await applyName(name);
            await join({ token });
          })
        }
      />
    );
  } else {
    body = (
      <GamesScreen
        user={user}
        rooms={roomList}
        invitations={invitations}
        busy={busy}
        onPick={(g) => setGame(g)}
        onOpenRoom={(id) => openRoom(id)}
        onJoinInvitation={(id) => void run(() => join({ invitation_id: id }))}
      />
    );
  }

  return (
    <div className="app felt">
      <header className="appbar">
        <button
          type="button"
          className="brand"
          onClick={() => {
            if (selected) return;
            setGame(null);
          }}
          aria-label="CardPlay home"
        >
          <span className="brand-mark" aria-hidden="true">
            ♣
          </span>{" "}
          CardPlay
        </button>
        {user && canPlay && (
          <nav className="appbar-actions" aria-label="Account">
            {selected && (
              <button
                type="button"
                className="btn-ghost small"
                onClick={() => openRoom(null)}
              >
                Games
              </button>
            )}
            {!guest && (
              <button
                type="button"
                className="btn-ghost small"
                onClick={() => setSheet("friends")}
              >
                Friends
              </button>
            )}
            <button
              type="button"
              className="user-chip"
              onClick={() => setSheet("account")}
              aria-label={`Account: ${user.display_name}`}
            >
              <span className="avatar small">
                {user.display_name.slice(0, 1).toUpperCase()}
              </span>
              <span className="user-chip-name">{user.display_name}</span>
              {guest && <span className="pill">Guest</span>}
            </button>
          </nav>
        )}
      </header>
      <div className="app-alerts">{alertsSection}</div>
      {body}
      {user && sheet === "account" && (
        <AccountSheet
          user={user}
          busy={busy}
          run={run}
          onClose={() => setSheet(null)}
          onUser={setUser}
          onSignOut={() => void run(signOut)}
          onEnded={endSession}
          setNotice={setNotice}
        />
      )}
      {user && sheet === "friends" && (
        <FriendsSheet
          user={user}
          friends={friends}
          blocks={blocks}
          mutes={mutes}
          busy={busy}
          run={run}
          refresh={refresh}
          onClose={() => setSheet(null)}
          setNotice={setNotice}
        />
      )}
    </div>
  );
}
