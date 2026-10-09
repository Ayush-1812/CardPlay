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
const GATEWAY_ERRORS = new Set([502, 503, 504]);
const WAKE_BACKOFF_MS = [1000, 2000, 4000, 8000, 12000, 16000];

let onWaking: ((waking: boolean) => void) | null = null;

/** Called while the client is waiting for a sleeping server to answer. */
export function setWakingListener(fn: ((waking: boolean) => void) | null) {
  onWaking = fn;
}

const sleep = (ms: number) => new Promise((done) => setTimeout(done, ms));

export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  let response: Response;
  // Only reads are retried. A write may have reached the server even when the
  // answer did not, so resending one could repeat it.
  const attempts = method === "GET" ? WAKE_BACKOFF_MS.length : 0;
  let waking = false;
  const settle = () => {
    if (waking) {
      waking = false;
      onWaking?.(false);
    }
  };
  for (let attempt = 0; ; attempt++) {
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
      if (attempt < attempts) {
        if (!waking) {
          waking = true;
          onWaking?.(true);
        }
        await sleep(WAKE_BACKOFF_MS[attempt]);
        continue;
      }
      settle();
      throw new APIError(
        "NETWORK",
        "Cannot reach CardPlay. Check your connection and try again.",
        0,
      );
    }
    if (GATEWAY_ERRORS.has(response.status) && attempt < attempts) {
      if (!waking) {
        waking = true;
        onWaking?.(true);
      }
      await sleep(WAKE_BACKOFF_MS[attempt]);
      continue;
    }
    settle();
    break;
  }
  if (response.status === 204) return undefined as T;
  let data;
  try {
    data = await response.json();
  } catch {
    throw new APIError(
      "BAD_RESPONSE",
      "CardPlay returned an unexpected response. Please try again.",
      response.status,
    );
  }
  if (!response.ok)
    throw new APIError(
      data.error?.code ?? "INTERNAL",
      data.error?.message ?? "Something went wrong",
      response.status,
    );
  return data as T;
}
