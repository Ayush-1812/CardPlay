export type User = {
  id: string;
  handle: string;
  display_name: string;
  email: string;
  email_verified: boolean;
  // A name-only guest: plays and chats, no email, password or friends.
  is_guest?: boolean;
};
export type Room = {
  id: string;
  host_id: string;
  name: string;
  game_id: string;
  team_a: string;
  team_b: string;
  capacity: number;
  status: string;
  revision: number;
  rules_version: string;
};
export type Member = {
  id: string;
  handle: string;
  display_name: string;
  seat: number;
  ready: boolean;
};
export type MatchSummary = {
  id: string;
  status: "playing" | "paused" | "finished" | "abandoned";
  winner_id?: string;
  end_reason?: string;
  created_at: string;
  finished_at?: string;
};
export type RoomView = { room: Room; members: Member[]; match?: MatchSummary };
export type ChatMessage = {
  id: number;
  user_id: string;
  display_name: string;
  body: string;
  created_at: string;
  client_id: string;
};
export type Friend = {
  id: string;
  handle: string;
  display_name: string;
  status: string;
  requester_id: string;
  recipient_id: string;
};
export type PublicUser = { id: string; handle: string; display_name: string };
export type CreatedInvitation = {
  id: string;
  target_id: string | null;
  expires_at: string;
  revoked_at: string | null;
  accepted_at: string | null;
};
export type Invitation = {
  id: string;
  room_id: string;
  name: string;
  expires_at: string;
};
export class APIError extends Error {
  constructor(
    public code: string,
    message: string,
    public status: number,
  ) {
    super(message);
  }
}
// A host that sleeps when idle, which free tiers do, needs a cold start of
// up to a minute. Until it answers, the gateway in front of it returns 502,
// 503 or 504, or drops the connection. None of those mean the request was
// handled, so a read can simply be asked again.
// The API always answers with JSON, even for errors. A body that is not JSON
// therefore came from something in front of it: the host's router while the
// service starts, a gateway timing out, or an edge error page. Those replies
// mean the request never reached the application.
const WAKE_BACKOFF_MS = [1000, 2000, 4000, 8000, 12000, 16000];
// How long the client keeps waiting for a service that is starting. The
// window is shared by every request, so a screen that loads three things
// waits about this long in total rather than this long three times over.
const WAKE_WINDOW_MS = 45000;

// When the current wait ends; 0 when nothing is known to be starting.
let wakeUntil = 0;

let onWaking: ((waking: boolean) => void) | null = null;

/** Called while the client is waiting for a sleeping server to answer. */
export function setWakingListener(fn: ((waking: boolean) => void) | null) {
  onWaking = fn;
}

const sleep = (ms: number) => new Promise((done) => setTimeout(done, ms));

// mayWait opens the shared window on the first refusal and keeps it open
// until it expires.
function mayWait(): boolean {
  const now = Date.now();
  if (wakeUntil === 0) {
    wakeUntil = now + WAKE_WINDOW_MS;
    return true;
  }
  return now < wakeUntil;
}

type Answer = { gateway: boolean; data?: { error?: APIErrorBody } };
type APIErrorBody = { code?: string; message?: string };

// read returns the parsed body, or marks the reply as coming from a gateway
// rather than from the API.
async function read(response: Response): Promise<Answer> {
  const text = await response.text();
  if (text.trim() === "") return { gateway: true };
  try {
    return { gateway: false, data: JSON.parse(text) };
  } catch {
    return { gateway: true };
  }
}

// A read can always be repeated. A write is repeated only when the reply
// proves it never ran: a gateway refusing the request because the service is
// down or starting. A timeout is not such a proof -- the application may have
// received and applied it -- so those are reported instead of resent.
function worthRetrying(method: string, status: number): boolean {
  if (method === "GET") return true;
  return status !== 504 && status !== 408;
}

export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  let waking = false;
  const announce = (active: boolean) => {
    if (waking === active) return;
    waking = active;
    onWaking?.(active);
  };

  for (let attempt = 0; ; attempt++) {
    const last = attempt >= WAKE_BACKOFF_MS.length;
    let response: Response;
    try {
      response = await fetch(`/api/v1${path}`, {
        method,
        credentials: "same-origin",
        cache: "no-store",
        headers:
          body === undefined
            ? undefined
            : { "Content-Type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    } catch {
      // The request never left, or the connection broke. Repeating a write
      // here could apply it twice, so only reads are tried again.
      if (!last && method === "GET" && mayWait()) {
        announce(true);
        await sleep(WAKE_BACKOFF_MS[attempt]);
        continue;
      }
      announce(false);
      throw new APIError(
        "NETWORK",
        "Cannot reach CardPlay. Check your connection and try again.",
        0,
      );
    }

    if (response.status === 204) {
      wakeUntil = 0;
      announce(false);
      return undefined as T;
    }

    const answer = await read(response);
    if (answer.gateway) {
      if (!last && worthRetrying(method, response.status) && mayWait()) {
        announce(true);
        await sleep(WAKE_BACKOFF_MS[attempt]);
        continue;
      }
      announce(false);
      throw new APIError(
        "UNAVAILABLE",
        "CardPlay is starting up and did not answer in time. Wait a few seconds and try again.",
        response.status,
      );
    }

    wakeUntil = 0;
    announce(false);
    if (!response.ok) {
      throw new APIError(
        answer.data?.error?.code ?? "INTERNAL",
        answer.data?.error?.message ?? "Something went wrong",
        response.status,
      );
    }
    return answer.data as T;
  }
}
