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
export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`/api/v1${path}`, {
      method,
      credentials: "same-origin",
      cache: "no-store",
      headers:
        body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new APIError(
      "NETWORK",
      "Cannot reach CardPlay. Check your connection and try again.",
      0,
    );
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
