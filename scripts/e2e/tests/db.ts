import pg from "pg";

// Direct database access for test fixtures and exact privacy checks. Tests
// run only against a throwaway database (see README.md).
export async function withDB<T>(fn: (c: pg.Client) => Promise<T>): Promise<T> {
  const client = new pg.Client({ connectionString: process.env.DATABASE_URL });
  await client.connect();
  try {
    return await fn(client);
  } finally {
    await client.end();
  }
}

type Player = {
  user_id: string;
  hand: string[];
  bank: string[];
  sets: { id: string; color: string; cards: string[]; house?: string; hotel?: string }[];
  unassigned: string[];
  detached: string[];
  incoming: string[];
};
type EngineState = {
  players: Player[];
  active: number;
  draw: string[];
  discard: string[];
  next_set_id: number;
  pending?: { card: string; doublers?: string[]; spent?: string[] };
};

export type SeatLayout = {
  hand?: string[];
  bank?: string[];
  sets?: { color: string; cards: string[] }[];
};

// Replaces the deal of a match that has not moved yet (revision 0, when every
// card is in a hand or the draw pile). Listed cards go where the layout
// says; every other card returns to the draw pile in its original order, so
// the engine's card-conservation invariants still hold. Layouts are keyed by
// "active" and "other" seat. Returns the active seat's user ID.
export async function stageDeal(matchID: string, layout: { active: SeatLayout; other: SeatLayout }) {
  return withDB(async (c) => {
    const { rows } = await c.query("SELECT state FROM game_snapshots WHERE match_id=$1 AND revision=0", [matchID]);
    const rev = await c.query("SELECT revision FROM matches WHERE id=$1", [matchID]);
    if (rows.length !== 1 || rev.rows[0].revision !== "0") throw new Error("stageDeal needs a match at revision 0");
    const s = rows[0].state as EngineState;
    const pool = [...s.players.flatMap((p) => p.hand), ...s.draw];
    const take = (id: string) => {
      const i = pool.indexOf(id);
      if (i < 0) throw new Error(`card ${id} is not available to stage`);
      pool.splice(i, 1);
      return id;
    };
    for (const [seat, p] of s.players.entries()) {
      const l = seat === s.active ? layout.active : layout.other;
      p.hand = (l.hand ?? []).map(take);
      p.bank = (l.bank ?? []).map(take);
      p.sets = (l.sets ?? []).map((set) => ({ id: `set-${++s.next_set_id}`, color: set.color, cards: set.cards.map(take) }));
      p.unassigned = [];
      p.detached = [];
      p.incoming = [];
    }
    s.draw = pool;
    await c.query("UPDATE game_snapshots SET state=$2 WHERE match_id=$1 AND revision=0", [matchID, JSON.stringify(s)]);
    return s.players[s.active].user_id;
  });
}

// Moves a match's turn clock back so the next server sweep (every 5 s)
// finds the awaited player out of time.
export async function expireTurnClock(matchID: string) {
  await withDB((c) => c.query("UPDATE matches SET awaiting_since=now()-interval '10 minutes' WHERE id=$1", [matchID]));
}

// Exact hidden-card check. For every match.state frame a player received,
// load the stored snapshot of that revision and fail if the frame contains a
// card that was then in another player's hand and had never been public.
export async function hiddenCardLeaks(matchID: string, players: { name: string; userID: string; frames: string[] }[]) {
  const snaps = await withDB(async (c) => {
    const { rows } = await c.query("SELECT revision, state FROM game_snapshots WHERE match_id=$1 ORDER BY revision", [matchID]);
    return rows.map((r) => ({ revision: Number(r.revision), state: r.state as EngineState }));
  });
  const everPublic = new Set<string>();
  const hiddenAt = new Map<number, Map<string, string[]>>();
  // Go encodes empty lists as null.
  const list = (ids?: string[] | null) => ids ?? [];
  for (const { revision, state } of snaps) {
    for (const p of state.players)
      for (const id of [
        ...list(p.bank),
        ...list(p.unassigned),
        ...list(p.detached),
        ...list(p.incoming),
        ...list(p.sets).flatMap((x) => [...list(x.cards), x.house ?? "", x.hotel ?? ""]),
      ])
        if (id) everPublic.add(id);
    for (const id of [...list(state.discard), state.pending?.card ?? "", ...list(state.pending?.doublers), ...list(state.pending?.spent)])
      if (id) everPublic.add(id);
    hiddenAt.set(revision, new Map(state.players.map((p) => [p.user_id, list(p.hand).filter((id) => !everPublic.has(id))])));
  }
  const leaks: string[] = [];
  let checked = 0;
  for (const p of players)
    for (const raw of p.frames) {
      const m = JSON.parse(raw);
      if (m.type !== "match.state" || m.match_id !== matchID) continue;
      const hidden = hiddenAt.get(m.payload.revision);
      if (!hidden) throw new Error(`snapshot ${m.payload.revision} is missing; cannot check privacy`);
      checked++;
      for (const q of players)
        if (q !== p) for (const id of hidden.get(q.userID) ?? []) if (raw.includes(`"${id}"`)) leaks.push(`${p.name} saw ${q.name}'s hidden ${id} at revision ${m.payload.revision}`);
    }
  if (checked === 0) throw new Error("no match states were checked");
  return leaks;
}
