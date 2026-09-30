import { expect, test } from "@playwright/test";
import { createRoom, latestState, makeFriends, newPlayer, registerViaAPI, type Player } from "./helpers";

// A phone player (touch, 412px wide) against a desktop player.
test.describe.serial("mobile play", () => {
  let phone: Player, desk: Player;

  test.beforeAll(async ({ browser, playwright }) => {
    phone = await newPlayer(browser, "phoebe", { ...playwright.devices["Pixel 7"] });
    desk = await newPlayer(browser, "dex", { viewport: { width: 1280, height: 900 } });
    for (const p of [phone, desk]) {
      await p.page.goto("/");
      await registerViaAPI(p);
    }
    await makeFriends(desk, phone);
  });
  test.afterAll(async () => {
    for (const p of [phone, desk]) await p?.context.close();
  });

  const noSideScroll = async (p: Player) => {
    const [scroll, width] = await p.page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
    expect(scroll, "page must not scroll sideways").toBeLessThanOrEqual(width);
  };

  test("lobby fits the phone and works by touch", async () => {
    await noSideScroll(phone);
    await desk.page.reload();
    await createRoom(desk, "Pocket game", 2);
    await desk.page.locator(".friend-row", { hasText: `@${phone.handle}` }).getByRole("button", { name: "Invite" }).click();
    await phone.page.reload();
    await phone.page.locator(".friend-row", { hasText: "Pocket game" }).getByRole("button", { name: "Join →" }).tap();
    await expect(phone.page.getByRole("heading", { level: 1, name: "Pocket game" })).toBeVisible();
    await noSideScroll(phone);
    for (const p of [phone, desk]) await p.page.getByRole("button", { name: "I'm ready" }).click();
    await desk.page.getByRole("button", { name: "Start match" }).click();
    await expect(phone.page.locator(".game-table")).toBeVisible();
    await expect.poll(() => latestState(phone)?.status).toBe("playing");
  });

  test("the table fits the phone; cards open by tap", async () => {
    await noSideScroll(phone);
    await expect(phone.page.locator(".hand-rail")).toBeVisible();
    // Make sure it is the phone's turn: the desktop player ends theirs first.
    if (latestState(phone).view.public.active !== latestState(phone).view.self.seat) {
      await desk.page.locator(".platter-actions").getByRole("button", { name: "End turn" }).click();
      await expect(phone.page.locator(".game-status strong")).toHaveText(/^Your turn/);
    }
    const rev = latestState(phone).revision;
    await phone.page.locator(".hand-card").first().tap();
    const dialog = phone.page.locator(".hand-actions");
    await expect(dialog).toBeVisible();
    const box = await dialog.boundingBox();
    const viewport = phone.page.viewportSize()!;
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(viewport.width + 1);
    const action = dialog.getByRole("button", { name: /^(Play property|Bank as)/ }).first();
    await action.tap();
    await expect.poll(() => latestState(phone).revision).toBeGreaterThan(rev);
    await phone.page.locator(".platter-actions").getByRole("button", { name: "End turn" }).tap();
    if (latestState(phone).view.self.hand.length > 7) {
      const picker = phone.page.locator(".dialog");
      for (let k = 0; k < latestState(phone).view.self.hand.length - 7; k++) await picker.locator(".pick:not(.on)").first().tap();
      await picker.getByRole("button", { name: /and end turn$/ }).tap();
    }
    await expect(desk.page.locator(".game-status strong")).toHaveText(/^Your turn/);
    await noSideScroll(phone);
  });

  test("chat drawer opens and sends on the phone", async () => {
    await phone.page.locator(".chat-toggle").tap();
    await phone.page.locator("#chat-body").fill("from my phone");
    await phone.page.locator(".chat-form button").tap();
    await desk.page.locator(".chat-toggle").click();
    await expect(desk.page.locator(".chat-drawer.open").getByText("from my phone")).toBeVisible();
    await noSideScroll(phone);
    // On a phone the drawer covers the table; closing it returns there.
    await phone.page.getByRole("button", { name: "Close chat" }).tap();
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
    for (const p of [phone, desk]) expect(p.errors, `${p.name} page errors`).toEqual([]);
  });
});
