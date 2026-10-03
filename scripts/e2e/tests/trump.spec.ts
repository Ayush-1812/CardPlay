import { expect, test } from "@playwright/test";
import { latestState, newPlayer, playAsGuest, type Player } from "./helpers";

// Four guests play Trump through the real screens: the table, the trump
// choice with delegation, a trick, the counters, and the accessibility
// affordances the brief asks for.
test.describe.serial("trump at a four-seat table", () => {
  const players: Player[] = [];
  let link: string;

  const pub = (p: Player) => latestState(p).view.public;
  const self = (p: Player) => latestState(p).view.self;
  const seatOf = (p: Player) => self(p).seat;
  const byTurn = () => players.find((p) => seatOf(p) === pub(players[0]).turn)!;

  test.beforeAll(async ({ browser }) => {
    for (const name of ["ava", "ben", "cara", "dan"]) {
      players.push(
        await newPlayer(browser, name, {
          viewport: { width: 1280, height: 900 },
        }),
      );
    }
  });
  test.afterAll(async () => {
    for (const p of players) await p?.context.close();
  });

  test("a Trump room seats exactly four", async () => {
    const host = players[0];
    await playAsGuest(host);
    await host.page.getByRole("button", { name: /Trump/ }).click();
    await expect(
      host.page.getByRole("heading", { level: 1, name: "Trump" }),
    ).toBeVisible();
    await host.page.getByRole("button", { name: "Create room" }).click();
    await expect(
      host.page.getByRole("heading", { level: 1, name: /'s table$/ }),
    ).toBeVisible();
    await expect(host.page.locator(".room-head .pill")).toHaveText("Connected");
    link = await host.page.getByLabel("Invite link").inputValue();
    // The room was created for four seats, not Monopoly's five.
    await expect(host.page.getByText("1/4")).toBeVisible();
  });

  test("teams can be renamed and players moved between them", async () => {
    const host = players[0];
    for (const p of players.slice(1)) {
      await p.page.goto(link);
      await p.page.getByLabel("Your name").fill(p.name);
      await p.page.getByRole("button", { name: "Join as guest" }).click();
      await expect(p.page.locator(".room-head .pill")).toHaveText("Connected");
    }
    // The lobby shows the two teams and who is on them.
    await expect(host.page.locator(".team-column")).toHaveCount(2);
    await expect(host.page.locator(".team-column").first()).toContainText(
      "Team A",
    );

    // Rename both teams.
    await host.page.locator('input[name="team_a"]').fill("Tigers");
    await host.page.locator('input[name="team_b"]').fill("Lions");
    await host.page
      .locator(".teams-panel")
      .getByRole("button", { name: "Save team names" })
      .click();
    for (const p of players) {
      await expect(p.page.locator(".team-column").first()).toContainText(
        "Tigers",
      );
      await expect(p.page.locator(".team-column").last()).toContainText(
        "Lions",
      );
    }

    // Move the player in seat 2 to seat 1: they swap, changing both teams.
    const before = await host.page.locator(".team-column").first().innerText();
    const second = host.page
      .locator(".team-column")
      .last()
      .locator("select")
      .first();
    await second.selectOption("0");
    await expect
      .poll(
        async () =>
          (await host.page.locator(".team-column").first().innerText()) !==
          before,
      )
      .toBe(true);
    // Everyone must confirm the new seating.
    for (const p of players) {
      await expect(
        p.page.getByRole("button", { name: "I'm ready" }),
      ).toBeVisible();
    }
  });

  test("the other three join and the host starts", async () => {
    for (const p of players)
      await p.page.getByRole("button", { name: "I'm ready" }).click();
    await players[0].page.getByRole("button", { name: "Start match" }).click();
    for (const p of players) {
      await expect(p.page.locator(".trump-table")).toBeVisible();
      await expect.poll(() => latestState(p)?.status).toBe("playing");
    }
  });

  test("the table shows seats, teams, counters and the toss result", async () => {
    const host = players[0];
    // Four seats, each naming its team; the partner is marked.
    await expect(host.page.locator(".tseat")).toHaveCount(4);
    await expect(host.page.locator(".tseat-top .tseat-team")).toContainText(
      "partner",
    );
    await expect(host.page.locator(".counter")).toHaveCount(2);
    await expect(host.page.locator(".counter.mine")).toHaveCount(1);
    await expect(host.page.locator(".counter").first()).toContainText("0 / 7");
    // The toss result: the entitled team is marked as choosing.
    await expect(host.page.locator(".tseat.deciding")).toHaveCount(2);
    // No trump yet, said in words as well as a symbol.
    await expect(host.page.locator(".trump-indicator")).toContainText(
      "No trump yet",
    );
    // Five cards each, hidden from everyone else.
    for (const p of players) {
      await expect(p.page.locator(".trump-hand .tcard")).toHaveCount(5);
      expect(self(p).hand).toHaveLength(5);
    }
  });

  test("the toss and the renamed teams are visible to everyone", async () => {
    const entitled = pub(players[0]).entitled_team;
    const expected = entitled === 0 ? "Tigers" : "Lions";
    for (const p of players) {
      const banner = p.page.locator(".toss-banner");
      await expect(banner).toBeVisible();
      await expect(banner).toContainText("Toss");
      await expect(banner).toContainText(expected);
      await expect(banner).toContainText("chooses the trump suit");
      // The counters carry the chosen names too.
      const counters = await p.page.locator(".counter").allInnerTexts();
      expect(counters.join(" ").toLowerCase()).toContain("tigers");
      expect(counters.join(" ").toLowerCase()).toContain("lions");
    }
  });

  test("chat is closed while the trump is chosen", async () => {
    const host = players[0];
    await expect(
      host.page.getByRole("button", { name: /^Chat/ }),
    ).toBeDisabled();
    await expect(
      host.page.getByText(/Chat is closed while the trump is chosen/),
    ).toBeVisible();
  });

  test("the choice can be passed to the partner, who then picks", async () => {
    const entitled = pub(players[0]).entitled_team;
    const first = players.find((p) => seatOf(p) % 2 === entitled)!;
    const partner = players.find((p) => seatOf(p) === (seatOf(first) + 2) % 4)!;
    // Both of the entitled team are offered the choice.
    await expect(
      first.page.getByRole("button", { name: "My teammate will choose trump" }),
    ).toBeVisible();
    await first.page
      .getByRole("button", { name: "My teammate will choose trump" })
      .click();
    // The partner must now choose and cannot pass it back.
    await expect(
      partner.page.getByRole("heading", { name: "Choose the trump suit" }),
    ).toBeVisible();
    await expect(
      partner.page.getByText(/Your partner passed the choice to you/),
    ).toBeVisible();
    await expect(
      partner.page.getByRole("button", {
        name: "My teammate will choose trump",
      }),
    ).toHaveCount(0);
    // Suits are named, not only coloured, and show how many are in hand.
    const hearts = partner.page.getByRole("button", { name: /Hearts/ });
    await expect(hearts).toContainText("in hand");
    await hearts.click();
    for (const p of players) {
      await expect(p.page.locator(".trump-indicator")).toContainText("Hearts");
      await expect.poll(() => self(p).hand.length).toBe(13);
    }
    // The player who chose leads.
    expect(pub(partner).turn).toBe(seatOf(partner));
  });

  test("chat reopens once play starts", async () => {
    await expect(
      players[0].page.getByRole("button", { name: /^Chat/ }),
    ).toBeEnabled();
    await expect(players[0].page.getByText(/Chat is closed/)).toHaveCount(0);
  });

  test("legal cards are highlighted and illegal ones explain themselves", async () => {
    const turn = byTurn();
    await expect(turn.page.locator(".hand-card-btn")).toHaveCount(13);
    await expect(turn.page.locator(".hand-status")).toContainText("Your turn");
    // On a lead every card is legal.
    await expect(turn.page.locator(".hand-card-btn.legal")).toHaveCount(13);
    // Cards carry rank, suit symbol and an accessible full name.
    const first = turn.page.locator(".hand-card-btn").first();
    await expect(first).toHaveAttribute(
      "aria-label",
      /of (spades|hearts|diamonds|clubs)/,
    );
    // Someone not on turn sees no legal card and is told whose turn it is.
    const waiting = players.find((p) => seatOf(p) !== pub(turn).turn)!;
    await expect(waiting.page.locator(".hand-card-btn.legal")).toHaveCount(0);
    await expect(waiting.page.locator(".hand-status")).toContainText(
      "Waiting for",
    );
  });

  test("the hand is ordered: trump first, then its colour, then the rest", async () => {
    const trump = pub(players[0]).trump!;
    const sameColour = {
      spades: "clubs",
      clubs: "spades",
      hearts: "diamonds",
      diamonds: "hearts",
    }[trump];
    const order = [
      trump,
      sameColour,
      ...["spades", "hearts", "diamonds", "clubs"].filter(
        (x) => x !== trump && x !== sameColour,
      ),
    ];
    const ranks = [
      "2",
      "3",
      "4",
      "5",
      "6",
      "7",
      "8",
      "9",
      "10",
      "j",
      "q",
      "k",
      "a",
    ];
    for (const p of players) {
      const shown = await p.page
        .locator(".hand-card-btn")
        .evaluateAll((els) =>
          els.map((el) => el.getAttribute("data-card") ?? ""),
        );
      expect(shown).toHaveLength(13);
      const key = (id: string) => {
        const cut = id.lastIndexOf("-");
        return [
          order.indexOf(id.slice(0, cut)),
          -ranks.indexOf(id.slice(cut + 1)),
        ];
      };
      for (let i = 1; i < shown.length; i++) {
        const [sa, ra] = key(shown[i - 1]);
        const [sb, rb] = key(shown[i]);
        expect(
          sa < sb || (sa === sb && ra <= rb),
          `${shown[i - 1]} before ${shown[i]}`,
        ).toBe(true);
      }
    }
  });

  test("a trick plays out and the winner is shown", async () => {
    const leader = byTurn();
    const before = latestState(leader).revision;
    await leader.page.locator(".hand-card-btn.legal").first().click();
    await expect
      .poll(() => latestState(leader).revision)
      .toBeGreaterThan(before);
    await expect(leader.page.locator(".trick-card")).toHaveCount(1);
    await expect(leader.page.locator(".trick-who").first()).toContainText(
      "Led",
    );
    await expect(leader.page.locator(".lead-note")).toContainText("Lead suit");

    // A player holding the lead suit must follow it: those cards are legal
    // and the others explain why they are not.
    for (let i = 0; i < 3; i++) {
      const turn = byTurn();
      const illegal = turn.page.locator(".hand-card-btn.illegal").first();
      if (await illegal.count()) {
        await expect(illegal).toHaveAttribute(
          "aria-label",
          /must follow|cannot be played/,
        );
      }
      const rev = latestState(turn).revision;
      await turn.page.locator(".hand-card-btn.legal").first().click();
      await expect.poll(() => latestState(turn).revision).toBeGreaterThan(rev);
    }
    // The four cards are collected towards the team that won them, then the
    // table is left empty for the next lead.
    const any = players[0];
    await expect(any.page.locator(".trick-card")).toHaveCount(4);
    await expect(any.page.locator(".trick-card.won")).toHaveCount(1);
    await expect(any.page.locator(".trick-card.collecting")).toHaveCount(4);
    await expect(any.page.locator(".trick-card")).toHaveCount(0, {
      timeout: 5000,
    });
    await expect(any.page.locator(".trick-empty")).toBeVisible();
    // Each team's won tricks are stacked on its own side.
    await expect(any.page.locator(".counter-pile .trick-set")).toHaveCount(1);
    await expect(any.page.locator(".counter-pile .pile-card")).toHaveCount(4);
    await expect(
      any.page.locator(".tseat-flag", { hasText: "Leads next" }),
    ).toHaveCount(1);
    // One team now has a trick.
    const counters = await any.page.locator(".counter-value").allInnerTexts();
    expect(counters.join(" ")).toMatch(/1 \/ 7/);
  });

  test("the round runs to seven tricks and the winners choose next", async () => {
    // Play on, lowest legal card each time, until a team reaches seven.
    for (let card = 0; card < 52 && pub(players[0]).phase === "play"; card++) {
      const turn = byTurn();
      const rev = latestState(turn).revision;
      await turn.page.locator(".hand-card-btn.legal").first().click();
      await expect.poll(() => latestState(turn).revision).toBeGreaterThan(rev);
    }
    const final = pub(players[0]);
    expect(final.phase).toBe("round_over");
    // Exactly seven tricks ends it, and the remaining cards are not played.
    const winner = final.tricks[0] >= 7 ? 0 : 1;
    expect(final.tricks[winner]).toBe(7);
    expect(final.tricks[0] + final.tricks[1]).toBeLessThanOrEqual(13);
    expect(final.rounds_won[winner]).toBe(1);
    expect(final.rounds_won[1 - winner]).toBe(0);

    // Everyone sees the result sheet with the score and who chooses next.
    for (const p of players) {
      const sheet = p.page.getByRole("dialog", { name: "Round result" });
      await expect(sheet).toBeVisible();
      await expect(sheet).toContainText("tricks");
      await expect(sheet).toContainText("chooses next");
    }
    // Ready up: the round restarts only when all four are ready.
    let revision = latestState(players[0]).revision;
    for (const p of players.slice(0, 3)) {
      await expect.poll(() => latestState(p).revision).toBeGreaterThanOrEqual(revision);
      await p.page
        .getByRole("button", { name: /Ready for next round/ })
        .click();
      await expect.poll(() => latestState(p).revision).toBeGreaterThan(revision);
      revision = latestState(p).revision;
    }
    expect(pub(players[0]).round).toBe(1);
    await expect
      .poll(() => latestState(players[3]).revision)
      .toBeGreaterThanOrEqual(revision);
    await players[3].page
      .getByRole("button", { name: /Ready for next round/ })
      .click();
    for (const p of players) await expect.poll(() => pub(p).round).toBe(2);

    const second = pub(players[0]);
    // The winning team chooses the next trump, with a fresh five-card deal.
    expect(second.entitled_team).toBe(winner);
    expect(second.tricks).toEqual([0, 0]);
    expect(second.rounds_won[winner]).toBe(1);
    for (const p of players) expect(self(p).hand).toHaveLength(5);
  });

  test("the second round's trump can be chosen directly, without delegating", async () => {
    const entitled = pub(players[0]).entitled_team;
    const chooser = players.find((p) => seatOf(p) % 2 === entitled)!;
    await expect(
      chooser.page.getByRole("heading", { name: "Choose the trump suit" }),
    ).toBeVisible();
    await chooser.page.getByRole("button", { name: /Spades/ }).click();
    for (const p of players) {
      await expect(p.page.locator(".trump-indicator")).toContainText("Spades");
      await expect.poll(() => self(p).hand.length).toBe(13);
    }
    // The player who chose leads, without having delegated.
    expect(pub(chooser).turn).toBe(seatOf(chooser));
    expect(pub(chooser).delegated).toBe(false);
  });

  test("the phone layout fits and has no sideways scroll", async () => {
    const p = players[0];
    await p.page.setViewportSize({ width: 412, height: 915 });
    await expect(p.page.locator(".trump-table")).toBeVisible();
    // The hand is a horizontal scroller, which makes
    // documentElement.scrollWidth unreliable in Chromium: it absorbs the
    // scroller content. What matters is that the page itself cannot be
    // scrolled sideways and the body is no wider than the screen.
    const probe = await p.page.evaluate(() => {
      window.scrollTo(400, 0);
      const moved = window.scrollX;
      window.scrollTo(0, 0);
      return {
        moved,
        bodyWidth: document.body.scrollWidth,
        viewport: window.innerWidth,
      };
    });
    expect(probe.moved, "the page must not scroll sideways").toBe(0);
    expect(probe.bodyWidth, "the body must fit the screen").toBeLessThanOrEqual(
      probe.viewport,
    );
    // The hand still scrolls inside its own rail, so all 13 cards are reachable.
    const rail = await p.page.evaluate(() => {
      const el = document.querySelector(".trump-hand-rail")!;
      return { clientWidth: el.clientWidth, scrollWidth: el.scrollWidth };
    });
    expect(rail.scrollWidth).toBeGreaterThan(rail.clientWidth);
    await expect(p.page.locator(".counter")).toHaveCount(2);
    await p.page.setViewportSize({ width: 1280, height: 900 });
  });

  test("no page errors anywhere", async () => {
    for (const p of players)
      expect(p.errors, `${p.name} page errors`).toEqual([]);
  });
});
