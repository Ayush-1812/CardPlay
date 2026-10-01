// Pure helpers for the table's one-tap interactions: where a property card
// can go, what can be stolen or paid with, and how to move a single tabled
// card. They only describe choices; the server validates every move.

import {
  CardInfo,
  Cards,
  Color,
  COLOR_NAMES,
  COLORS,
  PropertySet,
  PublicPlayer,
  RENT_LADDER,
  SET_SIZE,
} from "../../lib/game";

export type Destination = {
  // "set:<id>", "new:<color>" or "unassigned", as the placement payload needs.
  value: string;
  kind: "set" | "new" | "unassigned";
  color?: Color;
  label: string;
  // Cards in the set before and after this card joins.
  before: number;
  after: number;
  size: number;
  rentBefore: number;
  rentAfter: number;
  completes: boolean;
};

const rentFor = (color: Color, count: number, anchored: boolean) =>
  anchored && count > 0
    ? RENT_LADDER[color][Math.min(count, SET_SIZE[color]) - 1]
    : 0;

// A set earns rent only with at least one card that is not a multicolor wild.
const anchored = (ids: string[], cards: Cards) =>
  ids.some((id) => cards[id]?.kind !== "rainbow_wild");

// Every legal destination for a property card, best first: own sets the card
// completes or brings closest to completion, then (for a multicolor wild)
// unassigned, then new sets of each legal color. ownSetID keeps a card's
// current set in the list when reorganizing.
export function destinations(
  card: CardInfo,
  id: string,
  sets: PropertySet[],
  cards: Cards,
  ownSetID?: string,
): Destination[] {
  const colors: Color[] =
    card.kind === "rainbow_wild" ? COLORS : (card.colors ?? []);
  const existing: Destination[] = [];
  for (const set of sets) {
    if (!colors.includes(set.color)) continue;
    const mine = set.id === ownSetID;
    if (!mine && set.cards.length >= SET_SIZE[set.color]) continue;
    const before = mine ? set.cards.length - 1 : set.cards.length;
    const after = before + 1;
    const others = set.cards.filter((c) => c !== id);
    existing.push({
      value: `set:${set.id}`,
      kind: "set",
      color: set.color,
      label: `${COLOR_NAMES[set.color]} set`,
      before,
      after,
      size: SET_SIZE[set.color],
      rentBefore: rentFor(set.color, before, anchored(others, cards)),
      rentAfter: rentFor(set.color, after, anchored([...others, id], cards)),
      completes:
        after === SET_SIZE[set.color] && anchored([...others, id], cards),
    });
  }
  existing.sort(
    (a, b) =>
      Number(b.completes) - Number(a.completes) ||
      a.size - a.after - (b.size - b.after),
  );
  const out = [...existing];
  if (card.kind === "rainbow_wild")
    out.push({
      value: "unassigned",
      kind: "unassigned",
      label: "Decide later",
      before: 0,
      after: 0,
      size: 0,
      rentBefore: 0,
      rentAfter: 0,
      completes: false,
    });
  for (const color of colors)
    out.push({
      value: `new:${color}`,
      kind: "new",
      color,
      label: `New ${COLOR_NAMES[color]} set`,
      before: 0,
      after: 1,
      size: SET_SIZE[color],
      rentBefore: 0,
      rentAfter: rentFor(color, 1, card.kind !== "rainbow_wild"),
      completes: SET_SIZE[color] === 1,
    });
  return out;
}

// The destinations worth offering (owner decision 2026-10-01): one per color,
// the best set of that color with room, or a new set only when there is none.
// A multicolor wild is offered the sets it can join and "Decide later", never
// a new set of every color. Moving a card out of ownSetID never offers a new
// set of the same color, which would only split it.
export function placements(
  card: CardInfo,
  id: string,
  sets: PropertySet[],
  cards: Cards,
  ownSetID?: string,
): Destination[] {
  const own = sets.find((s) => s.id === ownSetID);
  const all = destinations(card, id, sets, cards, ownSetID).filter(
    (d) => d.value !== `set:${ownSetID}`,
  );
  const out: Destination[] = [];
  const seen = new Set<string>();
  for (const d of all) {
    if (d.kind === "unassigned") {
      out.push(d);
      continue;
    }
    if (!d.color || seen.has(d.color)) continue;
    if (
      d.kind === "new" &&
      (card.kind === "rainbow_wild" || d.color === own?.color)
    )
      continue;
    seen.add(d.color);
    out.push(d);
  }
  return out;
}

export function destinationPayload(value: string) {
  if (value.startsWith("set:")) return { set: value.slice(4) };
  if (value.startsWith("new:")) return { color: value.slice(4) };
  return {};
}

// Cards Sly Deal and Forced Deal may take: properties outside complete sets,
// unassigned wilds, and (when allowed) detached buildings.
export function stealable(p: PublicPlayer, buildings: boolean): string[] {
  return [
    ...p.sets.filter((s) => !s.complete).flatMap((s) => s.cards),
    ...p.unassigned,
    ...(buildings ? p.detached : []),
  ];
}

// Tabled cards a player may pay with, money first, lowest value first.
export function payable(p: PublicPlayer, cards: Cards): string[] {
  const value = (id: string) => cards[id]?.value ?? 0;
  const bank = [...p.bank].sort((a, b) => value(a) - value(b));
  const props = [
    ...p.sets.flatMap((s) => [
      ...s.cards.filter((c) => cards[c]?.kind !== "rainbow_wild"),
      ...(s.house ? [s.house] : []),
      ...(s.hotel ? [s.hotel] : []),
    ]),
    ...p.detached,
  ].sort((a, b) => value(a) - value(b));
  return [...bank, ...props];
}

// The set (if any) holding a tabled card, for captions and moves.
export function setOf(p: PublicPlayer, id: string): PropertySet | undefined {
  return p.sets.find(
    (s) => s.cards.includes(id) || s.house === id || s.hotel === id,
  );
}

type Layout = {
  sets: {
    id?: string;
    color: string;
    cards: string[];
    house?: string;
    hotel?: string;
  }[];
  unassigned: string[];
  detached: string[];
};

// The whole-area layout that moves one property card to a destination,
// for the server's atomic rearrange. Buildings on a set that is no longer
// complete become detached; an emptied set disappears.
export function moveCard(
  me: PublicPlayer,
  id: string,
  dest: string,
  cards: Cards,
): Layout {
  const sets: Layout["sets"] = me.sets.map((s) => ({
    id: s.id,
    color: s.color as string,
    cards: s.cards.filter((c) => c !== id),
    house: s.house,
    hotel: s.hotel,
  }));
  const unassigned = me.unassigned.filter((c) => c !== id);
  const detached = [...me.detached];
  if (dest.startsWith("set:")) {
    sets.find((s) => s.id === dest.slice(4))?.cards.push(id);
  } else if (dest.startsWith("new:")) {
    sets.push({ color: dest.slice(4), cards: [id] });
  } else {
    unassigned.push(id);
  }
  const kept = [];
  for (const s of sets) {
    const complete =
      s.cards.length === SET_SIZE[s.color as Color] && anchored(s.cards, cards);
    if (!complete || s.cards.length === 0) {
      if (s.house) detached.push(s.house);
      if (s.hotel) detached.push(s.hotel);
      s.house = undefined;
      s.hotel = undefined;
    }
    if (s.cards.length > 0) kept.push(s);
  }
  return {
    sets: kept.map((s) => ({
      ...(s.id ? { id: s.id } : {}),
      color: s.color,
      cards: s.cards,
      ...(s.house ? { house: s.house } : {}),
      ...(s.hotel ? { hotel: s.hotel } : {}),
    })),
    unassigned,
    detached,
  };
}
