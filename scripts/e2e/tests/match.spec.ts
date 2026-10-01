import { expect, test, type Page } from "@playwright/test";
import { startAPI, stopAPI } from "./api";
import {
  createRoom,
  expectNoLeaks,
  joinInvitation,
  latestState,
  makeFriends,
  newPlayer,
  registerViaAPI,
  tapHandCard,
  type Player,
} from "./helpers";

type Card = { id: string; kind: string; value: number };

async function catalog(page: Page) {
  const res = await page.request.get("/api/v1/games/monopoly-deal/cards");
  const items = ((await res.json()) as { items: Card[] }).items;
  return Object.fromEntries(items.map((c) => [c.id, c]));
}

const isProperty = (c: Card) => ["property", "wild", "rainbow_wild"].includes(c.kind);

// Waits until this player has received a match.state newer than `revision`.
async function newerState(p: Player, revision: number) {
  try {
    await expect.poll(() => latestState(p)?.revision ?? -1, { timeout: 20_000 }).toBeGreaterThan(revision);
  } catch (e) {
    console.log(`${p.name} socket log:\n${p.log.slice(-40).join("\n")}`);
    throw e;
  }
  return latestState(p);
}

// One turn through the UI, like a careful beginner: lay properties first,
// otherwise bank money. A card with one sensible home goes down with a tap;
// a wild card with a choice picks the first (best) destination. The turn ends by itself
// after three plays; with fewer, or more than seven cards, the player ends
// it and returns the excess.
async function takeTurn(p: Player, cards: Record<string, Card>) {
  let st = latestState(p);
  const seat = st.view.self.seat;
  for (let play = 0; play < 3; play++) {
    if (st.status !== "playing" || st.view.public.active !== seat || st.view.public.plays_left === 0) break;
    const hand: string[] = st.view.self.hand;
    let id = hand.find((c) => isProperty(cards[c]));
    if (!id) id = hand.find((c) => cards[c].kind === "money");
    if (!id) break;
    await tapHandCard(p, id);
    // A card with one sensible home is played at once; otherwise a sheet asks.
    const before = st.revision;
    const sheet = p.page.getByRole("dialog");
    await expect.poll(async () => latestState(p).revision > before || (await sheet.count()) > 0).toBe(true);
    if (latestState(p).revision === before) await sheet.locator(".dest-tile").first().click();
    st = await newerState(p, st.revision);
  }
  if (st.status !== "playing" || st.view.public.active !== seat) return st;
  const hand: string[] = st.view.self.hand;
  if (st.view.public.plays_left > 0)
    await p.page.locator(".platter-actions").getByRole("button", { name: "End turn" }).click();
  if (hand.length > 7) {
    const sheet = p.page.getByRole("dialog", { name: "End turn" });
    for (let k = 0; k < hand.length - 7; k++) await sheet.locator(".card-choice:not(.on)").first().click();
    await sheet.getByRole("button", { name: /and end turn$/ }).click();
  }
  return newerState(p, st.revision);
}

test.describe.serial("a complete two-player match", () => {
  let host: Player, guest: Player;
  let cards: Record<string, Card>;

  test.beforeAll(async ({ browser }) => {
    host = await newPlayer(browser, "hana");
    guest = await newPlayer(browser, "gus");
    for (const p of [host, guest]) {
      await p.page.goto("/");
      await registerViaAPI(p);
    }
    await makeFriends(host, guest);
    cards = await catalog(host.page);
  });
  test.afterAll(async () => {
    for (const p of [host, guest]) await p?.context.close();
  });

  test("lobby: invite, ready, start", async () => {
    await host.page.reload();
    await createRoom(host, "Head to head", 2);
    await host.page.locator(".invite-panel .friend-row", { hasText: `@${guest.handle}` }).getByRole("button", { name: "Invite" }).click();
    await joinInvitation(guest, "Head to head");
    for (const p of [host, guest]) {
      await p.page.getByRole("button", { name: "I'm ready" }).click();
      await expect(p.page.getByRole("button", { name: "Not ready yet" })).toBeVisible();
    }
    await expect(host.page.getByText("Everyone is ready")).toBeVisible();
    await host.page.getByRole("button", { name: "Start match" }).click();
    for (const p of [host, guest]) {
      await expect(p.page.locator(".game-table")).toBeVisible();
      await expect.poll(() => latestState(p)?.status).toBe("playing");
    }
    await expectNoLeaks([host, guest]);
  });

  test("chat during the match shows an unread count", async () => {
    await guest.page.locator(".chat-toggle").click();
    await guest.page.locator("#chat-body").fill("good luck");
    await guest.page.locator(".chat-form button").click();
    await expect(host.page.locator(".chat-toggle .unread-badge")).toHaveText(/1 new chat message/);
    await host.page.locator(".chat-toggle").click();
    await expect(host.page.locator(".chat-drawer.open").getByText("good luck")).toBeVisible();
    // The Chat button stays reachable above the open drawer and closes it;
    // the drawer's own close button works too.
    await host.page.locator(".chat-toggle").click();
    await guest.page.getByRole("button", { name: "Close chat" }).click();
    for (const p of [host, guest]) await expect(p.page.locator(".chat-drawer.open")).toHaveCount(0);
  });

  test("refresh returns to the same seat and hand", async () => {
    const before = latestState(guest).view.self.hand;
    await guest.page.reload();
    await expect(guest.page.locator(".game-table")).toBeVisible();
    await expect.poll(() => latestState(guest)?.status).toBe("playing");
    expect(latestState(guest).view.self.hand).toEqual(before);
  });

  test("a dropped connection pauses the match; reconnecting resumes it", async () => {
    test.setTimeout(3 * 60_000);
    await guest.context.setOffline(true);
    await guest.page.evaluate(() => {
      // Offline blocks new connections; drop the open socket as a real outage would.
      for (const s of (window as unknown as { __sockets?: WebSocket[] }).__sockets ?? []) s.close();
    });
    await expect(host.page.locator(".game-status strong")).toHaveText(/^Paused/, { timeout: 90_000 });
    await guest.context.setOffline(false);
    for (const p of [host, guest]) await expect(p.page.locator(".game-status strong")).not.toHaveText(/^Paused/, { timeout: 60_000 });
  });

  test("a second tab takes the seat; the first can take it back", async () => {
    const tab2 = await host.context.newPage();
    tab2.on("dialog", (d) => void d.accept());
    await tab2.goto("/");
    // A new tab starts on the games screen, which offers to rejoin the table.
    await tab2.locator(".rejoin-row", { hasText: "Head to head" }).click();
    await expect(tab2.locator(".game-table")).toBeVisible();
    await expect(host.page.getByText(/open in another tab/)).toBeVisible();
    await host.page.bringToFront();
    await host.page.getByRole("button", { name: "Play here instead" }).click();
    await expect(tab2.getByText(/open in another tab/)).toBeVisible();
    await expect(host.page.locator(".game-status strong")).not.toHaveText(/^Paused/);
    await tab2.close();
  });

  test("a server restart mid-match loses nothing", async () => {
    test.setTimeout(3 * 60_000);
    const hands = [host, guest].map((p) => JSON.stringify(latestState(p).view.self.hand));
    const revision = latestState(host).revision;
    await stopAPI();
    await expect(host.page.locator(".table-conn")).toHaveText("Reconnecting");
    // While disconnected, moves are locked and the table says why.
    for (const p of [host, guest]) {
      await expect(p.page.locator(".table-offline")).toBeVisible();
      for (const card of await p.page.locator(".hand-card").all()) await expect(card).toBeDisabled();
      const endTurn = p.page.locator(".platter-actions").getByRole("button", { name: "End turn" });
      if (await endTurn.count()) await expect(endTurn).toBeDisabled();
    }
    const seen = [host, guest].map((p) => p.frames.length);
    await startAPI(process.env.E2E_BASE!);
    for (const [i, p] of [host, guest].entries()) {
      // Wait for a state pushed by the restarted server, not the one on screen.
      await expect
        .poll(() => p.frames.slice(seen[i]).some((f) => f.includes('"type":"match.state"')), { timeout: 60_000 })
        .toBe(true);
      await expect(p.page.locator(".table-conn")).toHaveText("Connected");
      await expect(p.page.locator(".game-status strong")).not.toHaveText(/^Paused/, { timeout: 60_000 });
      expect(latestState(p).revision).toBeGreaterThanOrEqual(revision);
    }
    expect([host, guest].map((p) => JSON.stringify(latestState(p).view.self.hand))).toEqual(hands);
  });

  test("play to a winner through the UI", async () => {
    test.setTimeout(12 * 60_000);
    const players = [host, guest];
    for (let turn = 0; turn < 200; turn++) {
      const st = latestState(host);
      if (st.status !== "playing") break;
      const active = players.find((p) => latestState(p).view.self.seat === st.view.public.active)!;
      await expect(active.page.locator(".game-status strong")).toHaveText(/^Your turn/);
      const after = await takeTurn(active, cards);
      // The other browser follows live.
      const other = players.find((p) => p !== active)!;
      await expect.poll(() => latestState(other)?.revision ?? -1).toBeGreaterThanOrEqual(after.revision);
    }
    const final = latestState(host);
    expect(final.status, "the match should end with a winner within 200 turns").toBe("finished");
    const winner = final.participants.find((x: { user_id: string }) => x.user_id === final.winner_id);
    for (const p of players) await expect(p.page.locator(".game-status.result")).toContainText(`${winner.display_name} won with three full sets`);
    await expectNoLeaks(players);
    for (const p of players) expect(p.errors, `${p.name} page errors`).toEqual([]);
  });
});
