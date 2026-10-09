import { expect, test } from "@playwright/test";
import { withDB } from "./db";
import { createRoom, latestState, newPlayer, playAsGuest, type Player } from "./helpers";

// Guests on a phone (touch, 412px wide) and a desktop: the invite link, the
// table by touch, chat, rotation, leaving, and the room going away.
test.describe.serial("mobile play as guests", () => {
  let phone: Player, desk: Player;
  let roomID: string;

  test.beforeAll(async ({ browser, playwright }) => {
    phone = await newPlayer(browser, "phoebe", { ...playwright.devices["Pixel 7"] });
    desk = await newPlayer(browser, "dex", { viewport: { width: 1280, height: 900 } });
    await playAsGuest(desk);
    await createRoom(desk, "Pocket game", 2);
  });
  test.afterAll(async () => {
    for (const p of [phone, desk]) await p?.context.close();
  });

  const noSideScroll = async (p: Player) => {
    const [scroll, width] = await p.page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
    expect(scroll, "page must not scroll sideways").toBeLessThanOrEqual(width);
  };
  const endTurnIfMine = async (p: Player) => {
    const st = latestState(p);
    if (st.view.public.active !== st.view.self.seat) return;
    const hand: string[] = st.view.self.hand;
    await p.page.locator(".platter-actions").getByRole("button", { name: "End turn" }).click();
    if (hand.length > 7) {
      const sheet = p.page.getByRole("dialog", { name: "End turn" });
      for (let k = 0; k < hand.length - 7; k++) await sheet.locator(".card-choice:not(.on)").first().click();
      await sheet.getByRole("button", { name: /and end turn$/ }).click();
    }
  };

  test("a phone guest joins from the invite link with just a name", async () => {
    const link = await desk.page.getByLabel("Invite link").inputValue();
    await phone.page.goto(link);
    await expect(phone.page.getByText("You're invited to a table")).toBeVisible();
    await noSideScroll(phone);
    await phone.page.getByLabel("Your name").fill(phone.name);
    await phone.page.getByRole("button", { name: "Join as guest" }).tap();
    await expect(phone.page.getByRole("heading", { level: 1, name: "Pocket game" })).toBeVisible();
    await noSideScroll(phone);
    await phone.page.getByRole("button", { name: "I'm ready" }).tap();
    await desk.page.getByRole("button", { name: "I'm ready" }).click();
    await desk.page.getByRole("button", { name: "Start match" }).click();
    await expect(phone.page.locator(".game-table")).toBeVisible();
    await expect.poll(() => latestState(phone)?.status).toBe("playing");
    roomID = latestState(phone).room_id;
  });

  test("the table fits the phone; cards work by tap", async () => {
    await noSideScroll(phone);
    await expect(phone.page.locator(".hand-rail")).toBeVisible();
    // Make sure it is the phone's turn: the desktop player ends theirs first.
    if (latestState(phone).view.public.active !== latestState(phone).view.self.seat) {
      await endTurnIfMine(desk);
      await expect(phone.page.locator(".game-status strong")).toHaveText(/^Your turn/);
    }
    const st = latestState(phone);
    const cards = (await (await phone.page.request.get("/api/v1/games/monopoly-deal/cards")).json()).items as {
      id: string;
      kind: string;
    }[];
    const kind = (id: string) => cards.find((c) => c.id === id)?.kind;
    const hand: string[] = st.view.self.hand;
    // A card that needs a choice opens a sheet that fits the screen.
    const choice = hand.findIndex((id) => ["action", "rent", "rent_any"].includes(kind(id) ?? ""));
    const index = choice >= 0 ? choice : 0;
    await phone.page.locator(`.hand-card[data-card="${hand[index]}"]`).tap();
    if (choice >= 0) {
      const sheet = phone.page.getByRole("dialog");
      await expect(sheet).toBeVisible();
      const viewport = phone.page.viewportSize()!;
      // The sheet slides up; measure once it has settled.
      await expect
        .poll(async () => {
          const box = (await sheet.boundingBox())!;
          return box.x >= 0 && box.x + box.width <= viewport.width + 1 && box.y + box.height <= viewport.height + 1;
        })
        .toBe(true);
      await sheet.getByRole("button", { name: /^Bank as/ }).tap();
    }
    await expect.poll(() => latestState(phone).revision).toBeGreaterThan(st.revision);
    await endTurnIfMine(phone);
    await expect(desk.page.locator(".game-status strong")).toHaveText(/^Your turn/);
    await noSideScroll(phone);
  });

  test("chat opens as a full sheet on the phone and sends", async () => {
    await phone.page.locator(".chat-toggle").tap();
    await phone.page.locator("#chat-body").fill("from my phone");
    await phone.page.locator(".chat-form button").tap();
    await desk.page.locator(".chat-toggle").click();
    await expect(desk.page.locator(".chat-drawer.open").getByText("from my phone")).toBeVisible();
    await phone.page.locator(".quick-replies").getByRole("button", { name: "👍" }).tap();
    await expect(desk.page.getByRole("log", { name: "Room chat" }).getByText("👍")).toBeVisible();
    await noSideScroll(phone);
    // On a phone the drawer covers the table; closing it returns there.
    const back = phone.page.getByRole("button", { name: "Close chat" });
    await expect(back).toHaveText(/Back to table/);
    await back.tap();
    await expect(phone.page.getByRole("button", { name: "Leave match" })).toBeVisible();
  });

  test("rotating to landscape keeps the table usable", async () => {
    await phone.page.setViewportSize({ width: 915, height: 412 });
    await expect(phone.page.locator(".game-table")).toBeVisible();
    await noSideScroll(phone);
    await phone.page.setViewportSize({ width: 412, height: 915 });
  });

  test("leaving ends the match for everyone with no winner", async () => {
    phone.page.once("dialog", (d) => void d.accept());
    await phone.page.getByRole("button", { name: "Leave match" }).tap();
    for (const p of [phone, desk]) {
      await expect(p.page.getByText(/left\. No winner|You left the match/).first()).toBeVisible();
      await expect(p.page.getByRole("button", { name: "I'm ready" })).toBeVisible();
    }
  });

  test("the room is deleted when the last player leaves", async () => {
    const stored = () =>
      withDB(async (c) => Number((await c.query("SELECT count(*) FROM rooms WHERE id=$1", [roomID])).rows[0].count));
    await phone.page.getByRole("button", { name: "Leave room", exact: true }).tap();
    await expect(phone.page.getByRole("heading", { name: "Choose a game" })).toBeVisible();
    expect(await stored()).toBe(1);
    await desk.page.getByRole("button", { name: "Leave room", exact: true }).click();
    await expect(desk.page.getByRole("heading", { name: "Choose a game" })).toBeVisible();
    expect(await stored()).toBe(0);
    await expect(desk.page.locator(".rejoin-row")).toHaveCount(0);
    for (const p of [phone, desk]) expect(p.errors, `${p.name} page errors`).toEqual([]);
  });
});

test("a guest can sign out, which ends the guest", async ({ browser }) => {
  const guest = await newPlayer(browser, "gwen");
  await playAsGuest(guest);
  // Friends need an account, so guests are not offered them.
  await expect(guest.page.getByRole("button", { name: "Friends", exact: true })).toHaveCount(0);
  await guest.page.getByRole("button", { name: `Account: ${guest.name}` }).click();
  guest.page.once("dialog", (d) => void d.accept());
  await guest.page.getByRole("dialog", { name: "Your account" }).getByRole("button", { name: "Sign out" }).click();
  await expect(guest.page.getByRole("button", { name: "Play as guest" })).toBeVisible();
  expect((await guest.page.request.get("/api/v1/me")).status()).toBe(401);
  expect(guest.errors).toEqual([]);
  await guest.context.close();
});
