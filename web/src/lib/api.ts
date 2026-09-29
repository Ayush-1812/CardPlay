export type User = { id: string; handle: string; display_name: string; email_verified: boolean };
export type Room = { id: string; host_id: string; name: string; capacity: number; status: string; revision: number; rules_version: string };
export type Member = { id: string; handle: string; display_name: string; seat: number; ready: boolean };
export type RoomView = { room: Room; members: Member[] };
export type ChatMessage = { id: number; user_id: string; display_name: string; body: string; created_at: string; client_id: string };
export type Friend = { id: string; handle: string; display_name: string; status: string; requester_id: string; recipient_id: string };
export type Invitation = { id: string; room_id: string; name: string; expires_at: string };
export class APIError extends Error {
  constructor(public code: string, message: string, public status: number) { super(message); }
}
export async function api<T>(path: string, method = "GET", body?: unknown): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    method, credentials: "same-origin", cache: "no-store",
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (response.status === 204) return undefined as T;
  const data = await response.json();
  if (!response.ok) throw new APIError(data.error?.code ?? "INTERNAL", data.error?.message ?? "Something went wrong", response.status);
  return data as T;
}
