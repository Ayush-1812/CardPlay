import { expect, test } from "@playwright/test";
import { latestState, newPlayer, playAsGuest, type Player } from "./helpers";

// Bluff through the real screens with four isolated sessions: the deal, a
// claim, a challenge both ways, a window that closes early, a window that
// runs out, and the hidden cards staying hidden.

test.describe.serial("bluff at a four-seat table", () => {
  const players: Player[] = [];
  let link: string;

  const pub = (p: Player) => latestState(p).view.public;
  const self = (p: Player) => latestState(p).view.self;
  const seatOf = (p: Player) => self(p).seat;
  // Whoever the server says is on turn, read once every client agrees.
  const onTurn = async () => {
    await expect
      .poll(() => new Set(players.map((p) => latestState(p).revision)).size)
      .toBe(1);
    const turn = pub(players[0]).turn;
    return players.find((p) => seatOf(p) === turn)!;
  };
  const other = (p: Player) => players.find((x) => x !== p)!;

  test.beforeAll(async ({ browser }) => {
    for (const name of ["bea", "bob", "bry", "bix"])
      players.push(
        await newPlayer(browser, name, {
          viewport: { width: 1280, height: 900 },
        }),
      );
  });
  test.afterAll(async () => {
    for (const p of players) await p?.context.close();
  });

  test("a Bluff room deals the whole deck", async () => {
    const host = players[0];
    await playAsGuest(host);
    await host.page.getByRole("button", { name: /^Bluff/ }).click();
    await expect(
      host.page.getByRole("heading", { level: 1, name: "Bluff" }),
    ).toBeVisible();
    await host.page.getByRole("button", { name: "Create room" }).click();
    await expect(
      host.page.getByRole("heading", { level: 1, name: /'s table$/ }),
    ).toBeVisible();
    link = await host.page.getByLabel("Invite link").inputValue();
    for (const p of players.slice(1)) {
      await p.page.goto(link);
      await p.page.getByLabel("Your name").fill(p.name);
      await p.page.getByRole("button", { name: "Join as guest" }).click();
      await expect(p.page.locator(".room-head .pill")).toHaveText("Connected");
    }
    for (const p of players)
      await p.page.getByRole("button", { name: "I'm ready" }).click();
    await players[0].page.getByRole("button", { name: "Start match" }).click();

    for (const p of players) {
      await expect(p.page.locator(".bluff-table")).toBeVisible();
      await expect.poll(() => latestState(p)?.status).toBe("playing");
      // Thirteen each at a four-handed table, and three opponents shown.
      await expect(p.page.locator(".hand-card")).toHaveCount(13);
      await expect(p.page.locator(".opponent")).toHaveCount(3);
      await expect(p.page.locator(".opponent-count").first()).toHaveText(
        "13 cards",
      );
      // The call starts at Aces and is impossible to miss.
      await expect(p.page.locator(".rank-call")).toContainText("Aces");
      await expect(p.page.locator(".pile-empty")).toBeVisible();
    }
  });

  test("a play announces its count and opens the window", async () => {
    const turn = await onTurn();
    await expect(turn.page.locator(".hand-status")).toContainText("Your turn");
    // Every card is selectable, matching the call or not.
    const cards = turn.page.locator(".hand-card:not([disabled])");
    await expect(cards).toHaveCount(13);
    await cards.nth(0).click();
    await cards.nth(1).click();
    await expect(turn.page.locator(".hand-card.chosen")).toHaveCount(2);
    await turn.page.getByRole("button", { name: /^Play 2 as / }).click();

    for (const p of players) {
      await expect(p.page.locator(".claim")).toContainText("played 2 Aces", {
        timeout: 15_000,
      });
      await expect(p.page.locator(".countdown-number")).toBeVisible();
    }
    // The player who made it cannot answer it; everyone else can.
    await expect(
      turn.page.getByRole("button", { name: "Call bluff" }),
    ).toHaveCount(0);
    await expect(
      other(turn).page.getByRole("button", { name: "Call bluff" }),
    ).toBeVisible();
    // Nobody may play while the window is open.
    await expect(
      players
        .find((p) => p !== turn)!
        .page.locator(".hand-card:not([disabled])"),
    ).toHaveCount(0);
  });

  test("calling it reveals that play and moves the pile", async () => {
    const claim = pub(players[0]).last!;
    const player = players.find((p) => seatOf(p) === claim.seat)!;
    const caller = players.find((p) => seatOf(p) !== claim.seat)!;
    const before = pub(players[0]).counts.slice();
    await caller.page.getByRole("button", { name: "Call bluff" }).click();

    for (const p of players) {
      await expect(p.page.locator(".result")).toBeVisible({ timeout: 15_000 });
      // Only the two cards of that play are face up.
      await expect(p.page.locator(".revealed .bcard")).toHaveCount(2);
      await expect(p.page.locator(".result-collect")).toContainText(
        "took the pile",
      );
    }
    // Whoever was wrong is holding more cards than before, and the pile is
    // empty again.
    const after = pub(players[0]);
    const collector = after.result!.collector;
    expect(after.counts[collector]).toBeGreaterThan(before[collector]);
    expect(after.pile_count).toBe(0);
    // Play continues after the player who was challenged, with the next
    // call, whoever won the challenge.
    expect(after.turn).toBe((claim.seat + 1) % players.length);
    expect(after.rank).toBe("2");
    expect(player).toBeTruthy();
  });

  test("the window closes as soon as everyone accepts", async () => {
    const turn = await onTurn();
    await turn.page.locator(".hand-card:not([disabled])").first().click();
    await turn.page.getByRole("button", { name: /^Play 1 as / }).click();
    const claim = await expect
      .poll(() => pub(turn).last?.id, { timeout: 15_000 })
      .toBeTruthy()
      .then(() => pub(turn).last!.id);

    const answerers = players.filter((p) => p !== turn);
    for (const p of answerers) {
      await p.page.getByRole("button", { name: "Accept" }).click();
      // Accepting binds: the controls go once the server has it, and this
      // player can no longer call the same play.
      await expect(
        p.page.getByRole("button", { name: "Call bluff" }),
      ).toHaveCount(0, { timeout: 10_000 });
    }
    // All three accepted, so the window ends without waiting out the clock.
    await expect
      .poll(() => pub(players[0]).phase, { timeout: 10_000 })
      .toBe("play");
    // Nothing was revealed: an accepted play stays face down.
    expect(pub(players[0]).result?.play_id).not.toBe(claim);
    expect(pub(players[0]).pile_count).toBeGreaterThan(0);
  });

  test("a window nobody answers runs out on its own", async () => {
    const turn = await onTurn();
    const before = pub(turn);
    await turn.page.locator(".hand-card:not([disabled])").first().click();
    await turn.page.getByRole("button", { name: /^Play 1 as / }).click();
    await expect
      .poll(() => pub(turn).phase, { timeout: 15_000 })
      .toBe("challenge");
    const player = pub(turn).last!.seat;
    // Nobody presses anything. The server closes the window.
    await expect
      .poll(() => pub(players[0]).phase, { timeout: 20_000 })
      .toBe("play");
    const after = pub(players[0]);
    expect(after.turn).toBe((player + 1) % players.length);
    expect(after.rank).not.toBe(before.rank);
  });

  test("nobody is shown another player's cards", async () => {
    // Cards move between hands, so "another player holds it now" proves
    // nothing. What must hold is the other way round: every card id that
    // ever reached a client is one that client either held itself or saw
    // turned face up by a challenge. Anything else would be a leak.
    const CARD = /"((?:a|[2-9]|10|j|q|k)-(?:spades|hearts|diamonds|clubs))"/g;
    for (const p of players) {
      const allowed = new Set<string>();
      for (const raw of p.frames) {
        let frame: {
          payload?: {
            view?: {
              self?: { hand?: string[] };
              public?: { result?: { cards?: string[] } };
            };
          };
        };
        try {
          frame = JSON.parse(raw);
        } catch {
          continue;
        }
        for (const id of frame.payload?.view?.self?.hand ?? []) allowed.add(id);
        for (const id of frame.payload?.view?.public?.result?.cards ?? [])
          allowed.add(id);
      }
      const seen = new Set<string>();
      for (const raw of p.frames)
        for (const match of raw.matchAll(CARD)) seen.add(match[1]);
      for (const id of seen)
        expect(
          allowed.has(id),
          `${p.name} received ${id}, which they never held and no challenge revealed`,
        ).toBe(true);
      // The check is not vacuous: this player did receive their own cards.
      expect(allowed.size).toBeGreaterThan(0);
    }
  });

  test("the table fits a phone, and nothing is hidden behind it", async () => {
    const p = players[0];
    await p.page.setViewportSize({ width: 390, height: 844 });
    await expect(p.page.locator(".hand-rail")).toBeInViewport();
    await expect(p.page.locator(".rank-call")).toBeInViewport();
    const probe = await p.page.evaluate(() => {
      window.scrollTo(400, 0);
      const moved = window.scrollX;
      window.scrollTo(0, 0);
      return {
        moved,
        page: document.documentElement.scrollHeight,
        view: document.documentElement.clientHeight,
      };
    });
    expect(probe.moved, "the page must not scroll sideways").toBe(0);
    expect(
      probe.page,
      "the table holds itself to one screen",
    ).toBeLessThanOrEqual(probe.view + 1);
    await p.page.setViewportSize({ width: 1280, height: 900 });
  });

  test("no page errors anywhere", async () => {
    for (const p of players)
      expect(p.errors, `${p.name} page errors`).toEqual([]);
  });
});

// A room may be played with more than one deck. Everything about the game
// is the same; there are simply more cards, so bigger claims are possible.
test.describe.serial("bluff with two decks", () => {
  const players: Player[] = [];
  const pub = (p: Player) => latestState(p).view.public;
  const self = (p: Player) => latestState(p).view.self;

  test.beforeAll(async ({ browser }) => {
    for (const name of ["dea", "deb", "dec"])
      players.push(await newPlayer(browser, name));
  });
  test.afterAll(async () => {
    for (const p of players) await p?.context.close();
  });

  test("two decks deal 104 cards and allow a claim of eight", async () => {
    const host = players[0];
    await playAsGuest(host);
    await host.page.getByRole("button", { name: /^Bluff/ }).click();
    await host.page.getByRole("button", { name: "Create room" }).click();
    await expect(
      host.page.getByRole("heading", { level: 1, name: /'s table$/ }),
    ).toBeVisible();
    const link = await host.page.getByLabel("Invite link").inputValue();

    // Three seats, two decks.
    await host.page.getByText("Room settings").click();
    await host.page
      .locator('.room-settings select[name="capacity"]')
      .selectOption("3");
    await host.page
      .locator('.room-settings select[name="decks"]')
      .selectOption("2");
    await host.page
      .locator(".room-settings")
      .getByRole("button", { name: "Save" })
      .click();
    await expect
      .poll(async () =>
        host.page.locator('.room-settings select[name="decks"]').inputValue(),
      )
      .toBe("2");

    for (const p of players.slice(1)) {
      await p.page.goto(link);
      await p.page.getByLabel("Your name").fill(p.name);
      await p.page.getByRole("button", { name: "Join as guest" }).click();
      await expect(p.page.locator(".room-head .pill")).toHaveText("Connected");
    }
    for (const p of players)
      await p.page.getByRole("button", { name: "I'm ready" }).click();
    await host.page.getByRole("button", { name: "Start match" }).click();
    for (const p of players)
      await expect(p.page.locator(".bluff-table")).toBeVisible();

    // 104 cards across three hands: 35, 35, 34 in some order.
    const counts = pub(players[0]).counts;
    expect(counts.reduce((a, b) => a + b, 0)).toBe(104);
    expect([...counts].sort((a, b) => b - a)).toEqual([35, 35, 34]);
    expect(pub(players[0]).decks).toBe(2);
    // A play may now claim up to eight, and the controls say so.
    expect(pub(players[0]).max_cards).toBe(8);
    const turn = players.find((p) => self(p).seat === pub(p).turn)!;
    await expect(
      turn.page.getByRole("button", { name: /Choose up to 8/ }),
    ).toBeVisible();

    // Eight cards can be selected and played as one claim.
    const cards = turn.page.locator(".hand-card:not([disabled])");
    for (let i = 0; i < 8; i++) await cards.nth(i).click();
    await expect(turn.page.locator(".hand-card.chosen")).toHaveCount(8);
    await turn.page.getByRole("button", { name: /^Play 8 as / }).click();
    for (const p of players)
      await expect(p.page.locator(".claim")).toContainText("played 8 Aces", {
        timeout: 15_000,
      });
  });

  test("no page errors anywhere", async () => {
    for (const p of players)
      expect(p.errors, `${p.name} page errors`).toEqual([]);
  });
});

// Playing to a winner, which is where the table changes shape: the result
// has to be shown, and the room has to let everyone go again.
test.describe.serial("bluff to the end", () => {
  const players: Player[] = [];
  const pub = (p: Player) => latestState(p).view.public;
  const self = (p: Player) => latestState(p).view.self;

  test.beforeAll(async ({ browser }) => {
    for (const name of ["fin", "fio", "fox"])
      players.push(await newPlayer(browser, name));
  });
  test.afterAll(async () => {
    for (const p of players) await p?.context.close();
  });

  test("a game runs to a winner, and the table says so", async () => {
    const host = players[0];
    await playAsGuest(host);
    await host.page.getByRole("button", { name: /^Bluff/ }).click();
    await host.page.getByRole("button", { name: "Create room" }).click();
    await expect(
      host.page.getByRole("heading", { level: 1, name: /'s table$/ }),
    ).toBeVisible();
    const link = await host.page.getByLabel("Invite link").inputValue();
    await host.page.getByText("Room settings").click();
    await host.page
      .locator('.room-settings select[name="capacity"]')
      .selectOption("3");
    await host.page
      .locator(".room-settings")
      .getByRole("button", { name: "Save" })
      .click();
    for (const p of players.slice(1)) {
      await p.page.goto(link);
      await p.page.getByLabel("Your name").fill(p.name);
      await p.page.getByRole("button", { name: "Join as guest" }).click();
      await expect(p.page.locator(".room-head .pill")).toHaveText("Connected");
    }
    for (const p of players)
      await p.page.getByRole("button", { name: "I'm ready" }).click();
    await host.page.getByRole("button", { name: "Start match" }).click();
    for (const p of players)
      await expect(p.page.locator(".bluff-table")).toBeVisible();

    // Play the maximum every turn and accept everything: the fastest way
    // to empty a hand.
    for (let move = 0; pub(players[0]).winner < 0 && move < 200; move++) {
      await expect
        .poll(() => new Set(players.map((p) => latestState(p).revision)).size, {
          timeout: 20_000,
        })
        .toBe(1);
      const state = pub(players[0]);
      if (state.phase === "play") {
        const turn = players.find((p) => self(p).may_play)!;
        const cards = turn.page.locator(".hand-card:not([disabled])");
        await expect(cards.first()).toBeEnabled({ timeout: 10_000 });
        const n = Math.min(state.max_cards, await cards.count());
        for (let i = 0; i < n; i++) await cards.nth(i).click();
        await turn.page.getByRole("button", { name: /^Play \d+ as / }).click();
        await expect
          .poll(() => pub(turn).phase, { timeout: 15_000 })
          .not.toBe("play");
      } else {
        for (const p of players) {
          const accept = p.page.getByRole("button", { name: "Accept" });
          if (await accept.isVisible().catch(() => false)) await accept.click();
        }
        await expect
          .poll(() => pub(players[0]).phase, { timeout: 20_000 })
          .not.toBe("challenge");
      }
    }
    expect(pub(players[0]).winner).toBeGreaterThanOrEqual(0);

    // The result is shown where the game happened, not left behind: every
    // player is told who won, and is offered the way back.
    const winner = pub(players[0]).winner;
    for (const p of players) {
      await expect(p.page.locator(".winner")).toBeVisible({ timeout: 15_000 });
      await expect(p.page.locator(".winner")).toContainText(
        self(p).seat === winner ? "You win" : "wins",
      );
      await expect(
        p.page.getByRole("button", { name: "Back to the room" }),
      ).toBeVisible();
      // Nobody is still being asked to play.
      await expect(p.page.locator(".hand-status")).not.toContainText(
        "Your turn",
      );
    }
    for (const p of players)
      expect(p.errors, `${p.name} page errors`).toEqual([]);
  });

  test("the room lets everyone play again", async () => {
    for (const p of players) {
      await p.page.getByRole("button", { name: "Back to the room" }).click();
      await expect(p.page.locator(".room-head")).toBeVisible({
        timeout: 15_000,
      });
      await p.page.getByRole("button", { name: "I'm ready" }).click();
    }
    await players[0].page.getByRole("button", { name: "Start match" }).click();
    for (const p of players)
      await expect(p.page.locator(".bluff-table")).toBeVisible({
        timeout: 20_000,
      });
    // A fresh deal: a full deck, the call back at Aces, nothing in the pile.
    const fresh = pub(players[0]);
    expect(fresh.counts.reduce((a, b) => a + b, 0)).toBe(52);
    expect(fresh.rank).toBe("a");
    expect(fresh.pile_count).toBe(0);
    expect(fresh.winner).toBe(-1);
    for (const p of players)
      expect(p.errors, `${p.name} page errors`).toEqual([]);
  });
});
