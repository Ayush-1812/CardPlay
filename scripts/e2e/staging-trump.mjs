// Staging smoke test: a complete four-player Trump match against the
// production artifacts (the release binary and the standalone web server).
// Run with the staging stack already listening; see docs/12-deployment.md.
//
//   BASE=http://127.0.0.1:3110 node staging-trump.mjs
import { chromium } from "@playwright/test";

const BASE = process.env.BASE ?? "http://127.0.0.1:3110";
const problems = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "PASS" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) problems.push(name);
};

const browser = await chromium.launch({ channel: "msedge", headless: true });

async function player(name, invite) {
  const context = await browser.newContext();
  const page = await context.newPage();
  const errors = [];
  const states = [];
  page.on("pageerror", (e) => errors.push(String(e)));
  page.on("websocket", (ws) =>
    ws.on("framereceived", (f) => {
      try {
        const m = JSON.parse(String(f.payload));
        if (m.type === "match.state") states.push(m.payload);
      } catch {}
    }),
  );
  page.setDefaultTimeout(30000);
  await page.goto(invite ?? BASE);
  await page.getByLabel("Your name").fill(name);
  await page.getByRole("button", { name: invite ? "Join as guest" : "Play as guest" }).click();
  return { name, context, page, errors, states, latest: () => states[states.length - 1] };
}

const host = await player("stage-a");
await host.page.getByRole("button", { name: /Trump/ }).click();
await host.page.getByRole("button", { name: "Create room" }).click();
await host.page.locator(".room-head .pill").getByText("Connected").waitFor();
const link = await host.page.getByLabel("Invite link").inputValue();
check("a Trump room was created on staging", !!link);

const players = [host];
for (const name of ["stage-b", "stage-c", "stage-d"]) {
  const p = await player(name, link);
  await p.page.locator(".room-head .pill").getByText("Connected").waitFor();
  players.push(p);
}
check("four players joined", players.length === 4);

for (const p of players) await p.page.getByRole("button", { name: "I'm ready" }).click();
await host.page.getByRole("button", { name: "Start match" }).click();
for (const p of players) await p.page.locator(".trump-table").waitFor();
check("the match started for all four", true);

const pub = (p) => p.latest().view.public;
const self = (p) => p.latest().view.self;
const seat = (p) => self(p).seat;
const poll = async (fn, what, ms = 30000) => {
  const until = Date.now() + ms;
  while (Date.now() < until) {
    if (fn()) return true;
    await new Promise((r) => setTimeout(r, 120));
  }
  throw new Error("timed out waiting for " + what);
};

await poll(() => players.every((p) => p.latest()?.view), "state");
check(
  "five cards each before the trump is chosen",
  players.every((p) => self(p).hand.length === 5),
  players.map((p) => self(p).hand.length).join(","),
);
check(
  "teams are seats 0+2 against 1+3",
  pub(host).players.every((s) => s.team === s.seat % 2),
);

// Trump selection, by delegation.
const entitled = pub(host).entitled_team;
const first = players.find((p) => seat(p) % 2 === entitled);
const partner = players.find((p) => seat(p) === (seat(first) + 2) % 4);
await first.page.getByRole("button", { name: "My teammate will choose trump" }).click();
await poll(() => pub(partner).delegated, "delegation");
await partner.page.getByRole("button", { name: /Hearts/ }).click();
await poll(() => players.every((p) => self(p).hand.length === 13), "full hands");
check("delegation worked and every hand is now 13", pub(host).trump === "hearts");
check("the player who chose leads", pub(partner).turn === seat(partner));

// Play the round out.
let plays = 0;
while (pub(host).phase === "play" && plays < 52) {
  const turn = players.find((p) => seat(p) === pub(host).turn);
  const before = turn.latest().revision;
  await turn.page.locator(".hand-card-btn.legal").first().click();
  await poll(() => turn.latest().revision > before, "a played card");
  plays++;
}
const final = pub(host);
const winner = final.tricks[0] >= 7 ? 0 : 1;
check("the round ended at exactly seven tricks", final.tricks[winner] === 7, `${final.tricks[0]}–${final.tricks[1]} after ${plays} cards`);
check("the round was recorded in history", final.history.length === 1 && final.rounds_won[winner] === 1);

// Ready up and confirm the winners choose next.
for (const p of players) await p.page.getByRole("button", { name: /Ready for next round/ }).click();
await poll(() => pub(host).round === 2, "round 2");
check("the winning team chooses the next trump", pub(host).entitled_team === winner);
check("a fresh five-card deal", players.every((p) => self(p).hand.length === 5));

// Reconnect mid-selection: the hand and the toss must survive.
const reconnect = players[2];
const handBefore = [...self(reconnect).hand].sort().join(",");
await reconnect.page.reload();
await reconnect.page.locator(".trump-table").waitFor();
await poll(() => self(reconnect).hand.length === 5, "state after reload");
check(
  "a refresh restores the same private hand",
  [...self(reconnect).hand].sort().join(",") === handBefore,
);
check("a refresh does not re-toss", pub(reconnect).entitled_team === winner);

const noise = players.flatMap((p) => p.errors);
check("no page errors", noise.length === 0, noise.slice(0, 2).join(" | "));

for (const p of players) await p.context.close();
await browser.close();
console.log(problems.length ? `\nFAILED: ${problems.join(", ")}` : "\nStaging match completed cleanly");
process.exit(problems.length ? 1 : 0);
