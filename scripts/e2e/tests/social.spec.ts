import { expect, test } from "@playwright/test";
import { createRoom, joinInvitation, makeFriends, newPlayer, registerViaAPI, registerViaUI, type Player } from "./helpers";

// Registration, friends, rooms, both kinds of invitation, and lobby chat,
// each step done by real browser sessions.
test.describe.serial("accounts, friends, rooms, invitations and chat", () => {
  let ana: Player, ben: Player, cy: Player;

  test.beforeAll(async ({ browser }) => {
    ana = await newPlayer(browser, "ana");
    ben = await newPlayer(browser, "ben");
    cy = await newPlayer(browser, "cy");
  });
  test.afterAll(async () => {
    for (const p of [ana, ben, cy]) await p?.context.close();
  });

  test("registration with email verification, then sign-in", async () => {
    await registerViaUI(ana);
    await expect(ana.page.getByRole("button", { name: `Account: ${ana.name}` })).toBeVisible();
  });

  test("server-side validation; an unverified account cannot create rooms", async () => {
    await ben.page.goto("/");
    const origin = { Origin: new URL(ben.page.url()).origin };
    const short = await ben.page.request.post("/api/v1/auth/register", {
      data: { email: ben.email, password: "short", handle: ben.handle, display_name: "ben" },
      headers: origin,
    });
    expect(short.status()).toBe(400); // the browser's minLength is not the only check
    const other = await newPlayer(ben.context.browser()!, "una");
    await other.page.goto("/");
    const reg = await other.page.request.post("/api/v1/auth/register", {
      data: { email: other.email, password: "e2e-password-unverified", handle: other.handle, display_name: "una" },
      headers: origin,
    });
    expect(reg.status()).toBeLessThan(300);
    const login = await other.page.request.post("/api/v1/auth/login", {
      data: { email: other.email, password: "e2e-password-unverified" },
      headers: origin,
    });
    expect(login.status()).toBe(200);
    const room = await other.page.request.post("/api/v1/rooms", { data: { name: "nope", capacity: 2 }, headers: origin });
    expect(room.status()).toBe(403);
    await other.page.reload();
    await expect(other.page.getByRole("heading", { name: "One more step." })).toBeVisible();
    await other.context.close();
    await registerViaAPI(ben);
    await cy.page.goto("/");
    await registerViaAPI(cy);
  });

  test("friend request and acceptance", async () => {
    await makeFriends(ana, ben);
    await makeFriends(ana, cy);
    // Searching for an unknown handle reveals nothing.
    await ana.page.getByRole("button", { name: "Friends", exact: true }).click();
    const sheet = ana.page.getByRole("dialog", { name: "Friends" });
    await sheet.locator("#friend-handle").fill("nobody_" + Date.now());
    await sheet.getByRole("button", { name: "Find", exact: true }).click();
    await expect(ana.page.locator('[role="alert"]').first()).toBeAttached();
    await ana.page.keyboard.press("Escape");
  });

  test("room creation, a friend invitation and an invite link", async () => {
    await ana.page.reload();
    await createRoom(ana, "Friday cards", 3);
    // Direct invitation to a friend.
    const inviteBen = ana.page.locator(".invite-panel .friend-row", { hasText: `@${ben.handle}` }).getByRole("button", { name: "Invite" });
    await inviteBen.click();
    await expect(ana.page.getByText(`Invitation sent to @${ben.handle}.`)).toBeVisible();
    await joinInvitation(ben, "Friday cards");
    // The waiting room offers its invite link straight away.
    const link = await ana.page.getByLabel("Invite link").inputValue();
    expect(link).toMatch(/#invite=[0-9a-f]{64}$/);
    // Opening the link joins directly, even in a tab already on CardPlay.
    await cy.page.goto(link);
    await expect(cy.page.getByRole("heading", { level: 1, name: "Friday cards" })).toBeVisible();
    // Every member sees all three seats live.
    for (const p of [ana, ben, cy])
      for (const q of [ana, ben, cy]) await expect(p.page.locator(".seat-list").getByText(q.name)).toBeVisible();
    // The room is full, so no link is offered.
    await expect(ana.page.getByText("The table is full.")).toBeVisible();
  });

  test("room chat is delivered live and plain text only", async () => {
    await ben.page.locator("#chat-body").fill("hi <b>all</b> 👋");
    await ben.page.getByRole("button", { name: "Send", exact: true }).click();
    for (const p of [ana, cy]) await expect(p.page.locator('[aria-label="Room chat"]').getByText("hi <b>all</b> 👋")).toBeVisible();
    // Rendered as text, never as markup.
    expect(await ana.page.locator('[aria-label="Room chat"] b').count()).toBe(0);
    // Hidden direction overrides are refused.
    await cy.page.locator("#chat-body").fill("evil‮txt.exe");
    await cy.page.getByRole("button", { name: "Send", exact: true }).click();
    await expect(cy.page.locator('[role="alert"]').first()).toContainText(/plain text/);
  });

  test("report and mute from the chat", async () => {
    const message = ana.page.locator('[aria-label="Room chat"] .message', { hasText: "hi <b>all</b>" });
    ana.page.once("dialog", (d) => void d.accept("spam"));
    await message.getByRole("button", { name: "Report" }).click();
    await expect(ana.page.getByText("Message reported. Thank you.")).toBeVisible();
    await message.getByRole("button", { name: "Mute" }).click();
    await expect(ana.page.getByText(/is muted/)).toBeVisible();
    await expect(ana.page.locator('[aria-label="Room chat"]').getByText("hi <b>all</b>")).toHaveCount(0);
    // Quick replies send with one tap.
    await cy.page.locator(".quick-replies").getByRole("button", { name: "GG" }).click();
    await expect(ben.page.locator('[aria-label="Room chat"] .message', { hasText: "GG" })).toBeVisible();
    // Muting hides messages only for the one who muted.
    await expect(cy.page.locator('[aria-label="Room chat"]').getByText("hi <b>all</b>")).toBeVisible();
    for (const p of [ana, ben, cy]) expect(p.errors, `${p.name} page errors`).toEqual([]);
  });
});
