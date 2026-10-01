import { expect, type Browser, type BrowserContext, type Page } from "@playwright/test";
import { randomBytes } from "node:crypto";
import { hiddenCardLeaks } from "./db";

export const PASSWORD = "e2e-password-" + randomBytes(6).toString("hex");

export type Player = {
  handle: string;
  email: string;
  name: string;
  context: BrowserContext;
  page: Page;
  // Every WebSocket frame this player received, for privacy checks.
  frames: string[];
  // Uncaught page errors; each spec asserts there are none.
  errors: string[];
  // Timestamped socket events, printed when a step fails.
  log: string[];
};

// Reads a token from the newest message sent to `email` in the test Mailpit.
export async function mailToken(email: string, subject: RegExp): Promise<string> {
  const api = process.env.E2E_MAIL_API!;
  for (let i = 0; i < 50; i++) {
    const res = await fetch(`${api}/api/v1/search?query=${encodeURIComponent(`to:"${email}"`)}`);
    const list = (await res.json()) as { messages: { ID: string; Subject: string }[] };
    const msg = list.messages.find((m) => subject.test(m.Subject));
    if (msg) {
      const full = (await (await fetch(`${api}/api/v1/message/${msg.ID}`)).json()) as { Text: string };
      const token = full.Text.match(/token:\s*\r?\n(\S+)/)?.[1];
      if (token) return token;
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(`no "${subject}" mail for ${email}`);
}

export function uniqueHandle(prefix: string) {
  return `${prefix}_${randomBytes(4).toString("hex")}`;
}

export async function newPlayer(browser: Browser, prefix: string, options: Parameters<Browser["newContext"]>[0] = {}): Promise<Player> {
  const context = await browser.newContext(options);
  // Track sockets so a test can drop them the way a network outage would.
  await context.addInitScript(() => {
    const Orig = window.WebSocket;
    const w = window as unknown as { __sockets: WebSocket[] };
    w.__sockets = [];
    window.WebSocket = class extends Orig {
      constructor(...args: ConstructorParameters<typeof WebSocket>) {
        super(...args);
        w.__sockets.push(this);
      }
    };
  });
  const frames: string[] = [];
  const errors: string[] = [];
  const t0 = Date.now();
  const log: string[] = [];
  const note = (s: string) => log.push(`${((Date.now() - t0) / 1000).toFixed(2)}s ${s}`);
  const watch = (page: Page) => {
    page.on("websocket", (ws) => {
      note("ws open");
      ws.on("framereceived", (f) => {
        frames.push(String(f.payload));
        const m = JSON.parse(String(f.payload));
        note(`<- ${m.type}${m.payload?.code ? " " + m.payload.code : ""}${m.payload?.revision != null ? " rev " + m.payload.revision : ""}`);
      });
      ws.on("framesent", (f) => note(`-> ${JSON.parse(String(f.payload)).type}`));
      ws.on("close", () => note("ws close"));
      ws.on("socketerror", (e) => note(`ws error ${e}`));
    });
    page.on("pageerror", (e) => errors.push(e.message));
  };
  context.on("page", watch);
  const page = await context.newPage();
  const handle = uniqueHandle(prefix);
  return { handle, email: `${handle}@e2e.test`, name: prefix, context, page, frames, errors, log };
}

// Registration through the real form, verification with the emailed token,
// then sign-in.
export async function registerViaUI(p: Player) {
  const { page } = p;
  await page.goto("/");
  await page.getByRole("button", { name: "Create an account (adds friends)" }).click();
  // The guest name field also uses display_name; fill the account form.
  const form = page.locator(".auth-box");
  await form.locator('input[name="display_name"]').fill(p.name);
  await form.locator('input[name="handle"]').fill(p.handle);
  await form.locator('input[name="email"]').fill(p.email);
  await form.locator('input[name="password"]').fill(PASSWORD);
  await page.getByRole("button", { name: "Create account →" }).click();
  await expect(page.getByText("Check your email for a verification token.")).toBeVisible();
  const token = await mailToken(p.email, /Verify your CardPlay account/);
  await page.locator('input[name="token"]').fill(token);
  await page.getByRole("button", { name: "Verify email →" }).click();
  await expect(page.getByText("Email verified. You can sign in now.")).toBeVisible();
  await signInForm(p);
}

async function signInForm(p: Player) {
  const form = p.page.locator(".auth-box");
  const { page } = p;
  await form.locator('input[name="email"]').fill(p.email);
  await form.locator('input[name="password"]').fill(PASSWORD);
  await page.getByRole("button", { name: "Take your seat →" }).click();
  await expect(page.getByRole("heading", { name: "Choose a game" })).toBeVisible();
}

export async function login(p: Player) {
  await p.page.goto("/");
  await p.page.getByRole("button", { name: "Sign in", exact: true }).click();
  await signInForm(p);
}

// Plays as a guest: just a name. Returns once the games screen shows.
export async function playAsGuest(p: Player) {
  await p.page.goto("/");
  await p.page.getByLabel("Your name").fill(p.name);
  await p.page.getByRole("button", { name: "Play as guest" }).click();
  await expect(p.page.getByRole("heading", { name: "Choose a game" })).toBeVisible();
}

// Registration through the API the browser uses, for supporting players.
export async function registerViaAPI(p: Player) {
  const res = await p.page.request.post("/api/v1/auth/register", {
    data: { email: p.email, password: PASSWORD, handle: p.handle, display_name: p.name },
    headers: { Origin: new URL(p.page.url() || "http://127.0.0.1:3100").origin },
  });
  expect(res.status()).toBeLessThan(300);
  const token = await mailToken(p.email, /Verify your CardPlay account/);
  const verify = await p.page.request.post("/api/v1/auth/verify", {
    data: { token },
    headers: { Origin: new URL(p.page.url() || "http://127.0.0.1:3100").origin },
  });
  expect(verify.status()).toBe(200);
  await login(p);
}

// Friend request and acceptance through the Friends sheet.
export async function makeFriends(a: Player, b: Player) {
  await a.page.getByRole("button", { name: "Friends", exact: true }).click();
  const sheet = a.page.getByRole("dialog", { name: "Friends" });
  await sheet.locator("#friend-handle").fill(b.handle);
  await sheet.getByRole("button", { name: "Find", exact: true }).click();
  await sheet.getByRole("button", { name: "Request", exact: true }).click();
  await expect(a.page.getByText("Friend request sent.")).toBeAttached();
  await a.page.keyboard.press("Escape");
  await b.page.reload();
  await b.page.getByRole("button", { name: "Friends", exact: true }).click();
  const row = b.page.getByRole("dialog", { name: "Friends" }).locator(".friend-row", { hasText: `@${a.handle}` });
  await expect(row.getByText("Wants to be friends")).toBeVisible();
  await row.getByRole("button", { name: "Accept" }).click();
  await expect(row.getByText("Friend", { exact: true })).toBeVisible();
  await b.page.keyboard.press("Escape");
}

// Creates a Monopoly Deal room from the games screen, then names and sizes
// it in Room settings.
export async function createRoom(host: Player, name: string, seats: number) {
  const { page } = host;
  await page.getByRole("button", { name: "Monopoly Deal" }).click();
  await page.getByRole("button", { name: "Create room" }).click();
  await expect(page.getByRole("heading", { level: 1, name: /'s table$/ })).toBeVisible();
  await page.getByText("Room settings").click();
  await page.locator('.room-settings input[name="name"]').fill(name);
  await page.locator('.room-settings select[name="capacity"]').selectOption(String(seats));
  await page.locator(".room-settings").getByRole("button", { name: "Save" }).click();
  await expect(page.getByRole("heading", { level: 1, name })).toBeVisible();
  await expect(page.locator(".room-head .pill")).toHaveText("Connected");
}

// Joins a room from a friend's invitation on the games screen.
export async function joinInvitation(p: Player, roomName: string) {
  await p.page.reload();
  await p.page.locator(".rejoin-row", { hasText: roomName }).getByRole("button", { name: "Join →" }).click();
  await expect(p.page.getByRole("heading", { level: 1, name: roomName })).toBeVisible();
}

// Taps a hand card the way a keyboard user would (cards overlap in the
// fan). Cards are found by id, so a hand that is still re-rendering cannot
// send the tap to the wrong card.
export async function tapHandCard(p: Player, id: string) {
  const card = p.page.locator(`.hand-card[data-card="${id}"]`);
  await expect(card).toBeEnabled();
  await card.focus();
  await card.press("Enter");
}

// The newest match.state this player received.
export function latestState(p: Player): any {
  for (let i = p.frames.length - 1; i >= 0; i--) {
    const m = JSON.parse(p.frames[i]);
    if (m.type === "match.state") return m.payload;
  }
  return null;
}

export function userIDOf(p: Player): string {
  const st = latestState(p);
  return st.view.public.players[st.view.self.seat].user_id;
}

// Fails if any player ever received a card hidden in another player's hand
// (exact, checked against the stored snapshot of every revision).
export async function expectNoLeaks(players: Player[]) {
  const matchID = latestState(players[0]).match_id;
  const leaks = await hiddenCardLeaks(
    matchID,
    players.map((p) => ({ name: p.name, userID: userIDOf(p), frames: p.frames })),
  );
  expect(leaks).toEqual([]);
}

export const statusText = (p: Player) => p.page.locator(".game-status strong").innerText();
