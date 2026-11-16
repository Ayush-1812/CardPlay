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
  // How many 52-card decks the game is dealt from. Only Bluff reads it.
  deck_count: number;
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
// up to a minute. Until it answers, the gateway in front of it can return
// 502, 503 or 504, or drop the connection. Reads can be retried; writes need
// more care because a gateway timeout may follow a committed request.
// The API answers with JSON, even for errors. A non-JSON body can also be a
// hosting rate limit or security challenge, which retrying will not resolve.
const WAKE_BACKOFF_MS = [1000, 2000, 4000, 8000, 12000, 16000];
// Render's cold start takes about a minute. Include time spent fetching the
// response body in a bounded 90-second budget, with a fresh budget per action.
const WAKE_WINDOW_MS = 90000;

let onWaking: ((waking: boolean) => void) | null = null;

/** Called while the client is waiting for a sleeping server to answer. */
export function setWakingListener(fn: ((waking: boolean) => void) | null) {
  onWaking = fn;
}

const sleep = (ms: number) => new Promise((done) => setTimeout(done, ms));

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

// Only ordinary gateway failures get startup retries. A write is never
// repeated after a timeout: the application may have already applied it.
function worthRetrying(method: string, status: number): boolean {
  return (
    status === 502 || status === 503 || (method === "GET" && status === 504)
  );
}

export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const deadline = Date.now() + WAKE_WINDOW_MS;
  const mayWait = () => Date.now() < deadline;
  const backoff = (attempt: number) =>
    sleep(
      Math.min(
        WAKE_BACKOFF_MS[Math.min(attempt, WAKE_BACKOFF_MS.length - 1)],
        Math.max(0, deadline - Date.now()),
      ),
    );
  let waking = false;
  const announce = (active: boolean) => {
    if (waking === active) return;
    waking = active;
    onWaking?.(active);
  };

  for (let attempt = 0; ; attempt++) {
    if (!mayWait()) {
      announce(false);
      throw new APIError(
        "UNAVAILABLE",
        "CardPlay did not respond within 90 seconds. Please try again.",
        0,
      );
    }
    let response: Response;
    let answer: Answer;
    const controller = new AbortController();
    const timeout = setTimeout(
      () => controller.abort(),
      Math.max(0, deadline - Date.now()),
    );
    try {
      response = await fetch(`/api/v1${path}`, {
        signal: controller.signal,
        method,
        credentials: "same-origin",
        cache: "no-store",
        headers:
          body === undefined
            ? undefined
            : { "Content-Type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      answer =
        response.status === 204 ? { gateway: false } : await read(response);
    } catch {
      // The request never left, or the connection broke. Repeating a write
      // here could apply it twice, so only reads are tried again.
      if (controller.signal.aborted) {
        announce(false);
        throw new APIError(
          "UNAVAILABLE",
          "CardPlay did not respond within 90 seconds. Please try again.",
          0,
        );
      }
      if (method === "GET" && mayWait()) {
        announce(true);
        await backoff(attempt);
        continue;
      }
      announce(false);
      throw new APIError(
        "NETWORK",
        "Cannot reach CardPlay. Check your connection and try again.",
        0,
      );
    } finally {
      clearTimeout(timeout);
    }

    if (response.status === 204) {
      announce(false);
      return undefined as T;
    }

    if (answer.gateway) {
      // Render can refuse a sleeping service with a plain-text 429, and
      // Cloudflare can return an HTML challenge. Neither means it is starting.
      if (response.status === 429) {
        announce(false);
        throw new APIError(
          "RATE_LIMITED",
          "CardPlay is temporarily rate-limited by its hosting provider. Please wait a few minutes before trying again.",
          response.status,
        );
      }
      if (
        response.status === 403 ||
        response.headers.get("cf-mitigated") === "challenge"
      ) {
        announce(false);
        throw new APIError(
          "ACCESS_BLOCKED",
          "CardPlay's hosting provider is blocking this connection. Please try again later.",
          response.status,
        );
      }
      if (worthRetrying(method, response.status) && mayWait()) {
        announce(true);
        await backoff(attempt);
        continue;
      }
      announce(false);
      throw new APIError(
        "UNAVAILABLE",
        worthRetrying(method, response.status)
          ? "CardPlay did not respond within 90 seconds. Please try again."
          : "CardPlay did not return a valid response. Please try again later.",
        response.status,
      );
    }

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
