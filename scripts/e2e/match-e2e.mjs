import puppeteer from "puppeteer-core";
import { spawn } from "node:child_process";

// Live-match browser test: three real browser sessions against a running
// web client. See scripts/e2e/README.md for setup.
const env = (name, fallback) => process.env[name] ?? fallback;
const BASE = env("E2E_BASE", "http://127.0.0.1:3100");
const PASSWORD = env("E2E_PASSWORD");
const API_BIN = env("E2E_API_BIN");
const BROWSER = env("E2E_BROWSER", "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe");
if (!PASSWORD || !API_BIN || !process.env.DATABASE_URL) {
  throw new Error("Set E2E_PASSWORD, E2E_API_BIN and DATABASE_URL (see scripts/e2e/README.md)");
}
const API_ENV = { ...process.env, APP_ORIGIN: BASE, HTTP_ADDR: env("E2E_API_ADDR", "127.0.0.1:18080") };

const results = [];
const problems = [];
const check = (name, ok, detail = "") => {
  results.push(`${ok ? "PASS" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) problems.push(name);
};

let api;
async function startAPI() {
  api = spawn(API_BIN, ["serve"], { env: API_ENV, stdio: "ignore" });
  for (let i = 0; i < 50; i++) {
    try {
      if ((await fetch(`http://${API_ENV.HTTP_ADDR}/readyz`)).ok) return;
    } catch {}
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error("API did not start");
}
async function stopAPI() {
  api.kill();
  await new Promise((r) => api.once("exit", r));
}

const browser = await puppeteer.launch({
  executablePath: BROWSER,
  headless: true,
  args: ["--window-size=1280,900"],
});

// Each player: page, raw WebSocket frames received (for privacy checks).
async function player(name) {
  const context = await browser.createBrowserContext();
  const page = await context.newPage();
  await page.setViewport({ width: 1280, height: 900 });
  page.setDefaultTimeout(20000);
  await page.evaluateOnNewDocument(() => {
    const Orig = window.WebSocket;
    window.__sockets = [];
    window.WebSocket = class extends Orig {
      constructor(...args) {
        super(...args);
        window.__sockets.push(this);
      }
    };
  });
  const frames = [];
  const cdp = await page.createCDPSession();
  await cdp.send("Network.enable");
  cdp.on("Network.webSocketFrameReceived", (e) => frames.push(e.response.payloadData));
  page.on("pageerror", (e) => problems.push(`${name} page error: ${e.message}`));
  page.on("dialog", (d) => void d.accept(d.type() === "prompt" ? "spam" : undefined));
  return { name, page, frames, cdp, context };
}
const text = (p) => p.page.evaluate(() => document.body.innerText);
const waitText = (p, t, timeout = 20000) =>
  p.page.waitForFunction((s) => document.body.innerText.includes(s), { timeout, polling: 250 }, t);
const status = (p) => p.page.$eval(".game-status strong", (e) => e.textContent).catch(() => "");
const waitStatus = (p, pred, timeout = 20000) =>
  p.page.waitForFunction(
    (src) => {
      const s = document.querySelector(".game-status strong")?.textContent ?? "";
      return new Function("s", `return ${src}`)(s);
    },
    { timeout, polling: 250 },
    pred,
  );
const handIDs = (p) => {
  // The newest match.state this player received carries their own hand.
  for (let i = p.frames.length - 1; i >= 0; i--) {
    const m = JSON.parse(p.frames[i]);
    if (m.type === "match.state") return m.payload.view.self.hand;
  }
  return [];
};

async function login(p) {
  await p.page.goto(BASE);
  await p.page.locator('input[name="email"]').fill(`${p.name}@cardplay.test`);
  await p.page.locator('input[name="password"]').fill(PASSWORD);
  await p.page.locator("::-p-text(Take your seat)").click();
  await waitText(p, "GOOD TO HAVE YOU HERE");
}

// Take one simple turn through the UI: use one card (bank, or play a
// property), then end the turn, returning excess cards if needed.
async function takeTurn(p) {
  const before = await status(p);
  const cards = await p.page.$$(".hand-card:not([disabled])");
  for (const card of cards) {
    await card.click();
    const bank = await p.page.$("::-p-text(Bank as)");
    const prop = await p.page.$("::-p-text(Play property)");
    const button = bank ?? prop;
    if (button) {
      await button.click();
      await p.page.waitForFunction((b) => (document.querySelector(".game-status strong")?.textContent ?? "") !== b, { polling: 250 }, before);
      break;
    }
    await card.click(); // deselect and try the next card
  }
  const excess = await p.page.$$(".decision input[type=checkbox]:not([disabled])");
  const returnButton = await p.page.$("::-p-text(and end turn)");
  if (returnButton) {
    const need = Number((await p.page.$eval("::-p-text(and end turn)", (b) => b.textContent)).match(/Return (\d+)/)[1]);
    for (let i = 0; i < need; i++) {
      const boxes = await p.page.$$(".decision input[type=checkbox]:not([disabled]):not(:checked)");
      await boxes[0].click();
    }
    await p.page.locator("::-p-text(and end turn)").click();
  } else {
    void excess;
    await p.page.locator("::-p-text(End turn)").click();
  }
}

try {
  await startAPI();
  const alice = await player("alice");
  const bob = await player("bob");
  const carol = await player("carol");
  const everyone = [alice, bob, carol];
  for (const p of everyone) await login(p);
  check("Three players sign in", true);
  // Mutes persist by design; start each run with none.
  for (const p of everyone)
    await p.page.evaluate(async () => {
      const list = await (await fetch("/api/v1/mutes")).json();
      for (const u of list.items) await fetch(`/api/v1/mutes/${u.id}`, { method: "DELETE" });
    });

  // Lobby: 3-seat room, invite link, ready, start.
  await alice.page.locator('#new-room input[name="name"]').fill("Live match");
  await alice.page.select('#new-room select[name="capacity"]', "3");
  await alice.page.locator("::-p-text(Create private room)").click();
  await waitText(alice, "Connected");
  await alice.page.locator("::-p-text(Create an invite link)").click();
  await alice.page.waitForSelector("input[readonly]");
  const link = await alice.page.$eval("input[readonly]", (e) => e.value);
  for (const p of [bob, carol]) {
    await p.page.goto("about:blank");
    await p.page.goto(link);
    await p.page.waitForFunction(() => document.querySelector('input[name="token"]')?.value.length === 64);
    await p.page.locator("::-p-text(Join a room)").click();
    await waitText(p, "Connected");
  }
  for (const p of everyone) {
    await p.page.locator("::-p-text(I'm ready)").click();
    await p.page.waitForSelector("::-p-text(Not ready yet)");
  }
  await waitText(alice, "Everyone is ready");
  await alice.page.locator("::-p-text(Start match)").click();
  for (const p of everyone) await p.page.waitForSelector(".game-table");
  await Promise.all(everyone.map((p) => waitStatus(p, "!s.startsWith('Paused')")));
  check("Host starts; every browser shows the live table", true);

  // Private hands: no player ever receives another player's hand card IDs.
  const leaks = () => {
    const out = [];
    for (const p of everyone)
      for (const q of everyone)
        if (p !== q)
          for (const id of handIDs(q))
            if (p.frames.some((f) => f.includes(`"${id}"`))) out.push(`${p.name} saw ${q.name}'s ${id}`);
    return out;
  };
  let leaked = leaks();
  check("No hidden cards in other players' WebSocket frames (start)", leaked.length === 0, leaked.slice(0, 3).join("; "));

  // Six turns through the UI.
  for (let turn = 0; turn < 6; turn++) {
    const statuses = await Promise.all(everyone.map(status));
    const active = everyone[statuses.findIndex((s) => s.startsWith("Your turn"))];
    if (!active) throw new Error(`no active player: ${statuses.join(" | ")}`);
    await takeTurn(active);
    const next = everyone[(everyone.indexOf(active) + 1) % 3];
    // Seat order is room seat order; the next active player sees "Your turn".
    await Promise.all(everyone.map((p) => waitStatus(p, "!s.includes(" + JSON.stringify(active === p ? "Your turn" : active.name + "'s turn") + ")")));
    void next;
  }
  const afterTurns = await Promise.all(everyone.map(status));
  check("Six turns played through the UI; every browser follows live", afterTurns.filter((s) => s.startsWith("Your turn")).length === 1, afterTurns.join(" | "));
  leaked = leaks();
  check("No hidden cards leaked after six turns", leaked.length === 0, leaked.slice(0, 3).join("; "));

  // Chat during the match, with an unread badge when chat is off screen.
  // A short window with the chat panel scrolled out of view.
  await alice.page.setViewport({ width: 1280, height: 420 });
  await alice.page.evaluate(() => window.scrollTo(0, 0));
  await alice.page.waitForFunction(() => { const c = document.querySelector(".chat-panel"); return c && c.getBoundingClientRect().top > window.innerHeight; }, { polling: 250 });
  await new Promise((r) => setTimeout(r, 300));
  await bob.page.locator("#chat-body").fill("good luck all");
  await bob.page.locator(".chat-form button").click();
  await waitText(alice, "new chat message");
  check("Chat works mid-match and shows an unread badge", true);
  await alice.page.locator(".unread-badge").click();
  await waitText(alice, "good luck all");
  await alice.page.setViewport({ width: 1280, height: 900 });

  // Refresh restores the same seat and hand; others pause then resume.
  const bobHand = JSON.stringify(handIDs(bob));
  await bob.page.reload();
  await bob.page.waitForSelector(".game-table");
  await waitStatus(bob, "!s.startsWith('Paused')");
  const restored = JSON.stringify(handIDs(bob));
  check("Refresh returns to the table with the same hand", restored === bobHand);

  // Second tab takes control; the first can take it back.
  const tab2 = await alice.context.newPage();
  const tab2p = { name: "alice-tab2", page: tab2, frames: [] };
  tab2.on("dialog", (d) => void d.accept());
  await tab2.goto(BASE);
  await tab2.waitForSelector("::-p-text(Live match)");
  await tab2.locator("aside ::-p-text(Live match)").click();
  await tab2.waitForSelector(".game-table");
  await waitText(alice, "open in another tab");
  check("A second tab takes the seat; the first is told why", true);
  await alice.page.bringToFront(); // the user switches back to this tab
  await alice.page.locator("::-p-text(Play here instead)").click();
  await waitText(tab2p, "open in another tab");
  await alice.page.waitForSelector(".game-table");
  await waitStatus(alice, "!s.startsWith('Paused')");
  check("'Play here instead' takes the seat back", true);
  await tab2.close();

  // Network loss pauses the match for everyone; recovery resumes it.
  await carol.cdp.send("Network.emulateNetworkConditions", { offline: true, latency: 0, downloadThroughput: -1, uploadThroughput: -1 });
  // Offline blocks reconnects; the open socket drops as it would on a real outage.
  await carol.page.evaluate(() => window.__sockets.forEach((s) => s.close()));
  await waitStatus(alice, "s.startsWith('Paused') && s.includes('carol')", 90000);
  check("Losing the network pauses the match for everyone", true);
  await carol.cdp.send("Network.emulateNetworkConditions", { offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1 });
  await Promise.all(everyone.map((p) => waitStatus(p, "!s.startsWith('Paused')", 60000)));
  check("Reconnecting resumes the match", true);

  // Server restart mid-match: browsers reconnect and the state is intact.
  const snapshot = await Promise.all(everyone.map((p) => JSON.stringify(handIDs(p))));
  const statusBefore = await Promise.all(everyone.map(status));
  await stopAPI();
  await waitText(alice, "Reconnecting");
  await startAPI();
  await Promise.all(everyone.map((p) => waitText(p, "Connected", 60000)));
  await Promise.all(everyone.map((p) => waitStatus(p, "!s.startsWith('Paused')", 60000)));
  const afterRestart = await Promise.all(everyone.map((p) => JSON.stringify(handIDs(p))));
  const statusAfter = await Promise.all(everyone.map(status));
  check("Server restart: every browser reconnects with identical hands and turn", JSON.stringify(afterRestart) === JSON.stringify(snapshot) && JSON.stringify(statusAfter) === JSON.stringify(statusBefore), statusAfter.join(" | "));
  const active = everyone[statusAfter.findIndex((s) => s.startsWith("Your turn"))];
  await takeTurn(active);
  await waitStatus(active, "!s.startsWith('Your turn')");
  check("Play continues after the restart", true);

  // Chat moderation through the UI.
  await alice.page.locator(".message ::-p-text(Report)").click();
  await waitText(alice, "Message reported");
  await alice.page.locator(".message ::-p-text(Mute)").click();
  await waitText(alice, "is muted");
  const aliceChat = await alice.page.$eval(".chat-log", (e) => e.innerText);
  check("Report and mute from the chat", !aliceChat.includes("good luck all"));

  // Leaving abandons the match for everyone, with no winner.
  await carol.page.locator("::-p-text(Leave match)").click();
  await Promise.all(everyone.map((p) => waitText(p, "carol left. No winner")));
  await Promise.all(everyone.map((p) => p.page.waitForSelector("::-p-text(I'm ready)")));
  check("Leaving abandons the match; everyone returns to the lobby with the result", true);
  leaked = leaks();
  check("No hidden cards leaked across the whole session", leaked.length === 0, leaked.slice(0, 3).join("; "));
} catch (e) {
  problems.push(`script error: ${e.message}`);
} finally {
  await browser.close();
  if (api) await stopAPI().catch(() => undefined);
}
console.log(results.join("\n"));
if (problems.length) {
  console.log("\nPROBLEMS:\n" + problems.join("\n"));
  process.exitCode = 1;
}
