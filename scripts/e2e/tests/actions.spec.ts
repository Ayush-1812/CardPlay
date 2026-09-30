import { expect, test, type Locator } from "@playwright/test";
import { expireTurnClock, stageDeal } from "./db";
import { createRoom, expectNoLeaks, latestState, makeFriends, newPlayer, registerViaAPI, userIDOf, type Player } from "./helpers";

// Action cards, responses, payments, placement and reorganizing, through the
// real screens. The deal is staged at revision 0 (before any move) so every
// card needed is in hand; everything after that is played through the UI.
test.describe.serial("action cards through the UI", () => {
  let one: Player, two: Player;
  // a: the player whose turn it is first; b: the other one.
  let a: Player, b: Player;
  let match: string;

  const pub = (p: Player) => latestState(p).view.public;
  const me = (p: Player) => pub(p).players[latestState(p).view.self.seat];
  const rev = (p: Player) => latestState(p)?.revision ?? -1;
  // Waits until both players have seen the state after the next move.
  const settle = async (from: number) => {
    for (const p of [a, b]) await expect.poll(() => rev(p)).toBeGreaterThan(from);
  };
  const handCard = (p: Player, id: string): Locator => {
    const i = latestState(p).view.self.hand.indexOf(id);
    if (i < 0) throw new Error(`${p.name} does not hold ${id}`);
    return p.page.locator(".hand-card").nth(i);
  };
  const useCard = async (p: Player, id: string) => {
    const card = handCard(p, id);
    await card.focus();
    await card.press("Enter");
    return p.page.locator(".hand-actions");
  };

  test.beforeAll(async ({ browser }) => {
    one = await newPlayer(browser, "ada");
    two = await newPlayer(browser, "bo");
    for (const p of [one, two]) {
      await p.page.goto("/");
      await registerViaAPI(p);
    }
    await makeFriends(one, two);
    await one.page.reload();
    await createRoom(one, "Action table", 2);
    await one.page.locator(".friend-row", { hasText: `@${two.handle}` }).getByRole("button", { name: "Invite" }).click();
    await two.page.reload();
    await two.page.locator(".friend-row", { hasText: "Action table" }).getByRole("button", { name: "Join →" }).click();
    for (const p of [one, two]) await p.page.getByRole("button", { name: "I'm ready" }).click();
    await one.page.getByRole("button", { name: "Start match" }).click();
    for (const p of [one, two]) await expect.poll(() => latestState(p)?.status).toBe("playing");
    match = latestState(one).match_id;
    const activeUser = await stageDeal(match, {
      active: {
        hand: ["action-birthday-1", "action-debt_collector-1", "action-sly_deal-1", "money-1m-1", "wild-multicolor-1"],
        bank: ["money-5m-1"],
        sets: [
          { color: "light_blue", cards: ["property-oriental-avenue", "property-vermont-avenue"] },
          { color: "dark_blue", cards: ["property-park-place", "property-boardwalk"] },
        ],
      },
      other: {
        hand: ["action-just_say_no-1", "rent-light_blue-brown-1", "action-forced_deal-1", "action-deal_breaker-1", "money-3m-1"],
        bank: ["money-1m-2", "money-2m-1"],
        sets: [
          { color: "brown", cards: ["property-mediterranean-avenue", "wild-light_blue-brown-1"] },
          { color: "orange", cards: ["property-st-james-place"] },
        ],
      },
    });
    // Forget frames of the replaced deal, then load the staged one.
    for (const p of [one, two]) {
      p.frames.length = 0;
      await p.page.reload();
      await expect.poll(() => latestState(p)?.status).toBe("playing");
    }
    [a, b] = userIDOf(one) === activeUser ? [one, two] : [two, one];
  });
  test.afterAll(async () => {
    for (const p of [one, two]) await p?.context.close();
  });

  test("a multicolor wild defaults to the set closest to completion", async () => {
    const dialog = await useCard(a, "wild-multicolor-1");
    await expect(dialog.locator("select")).toHaveValue(/^set:/);
    await expect(dialog.locator("select option:checked")).toHaveText("Light blue set (2/3)");
    await a.page.keyboard.press("Escape");
  });

  test("It's My Birthday: accept, then choose payment", async () => {
    const r = rev(a);
    await (await useCard(a, "action-birthday-1")).getByRole("button", { name: "Everyone pays you 2M" }).click();
    await settle(r);
    const response = b.page.getByRole("region", { name: "Your response" });
    await expect(response).toContainText("It's My Birthday on you");
    await response.getByRole("button", { name: "Accept" }).click();
    const payment = b.page.getByRole("region", { name: "Payment" });
    await expect(payment).toContainText("You owe 2M");
    await expect(payment.getByRole("button", { name: /^Pay/ })).toBeDisabled();
    await payment.getByRole("button", { name: "2M, 2M" }).click();
    const r2 = rev(b);
    await payment.getByRole("button", { name: "Pay 2M" }).click();
    await settle(r2);
    expect(me(a).bank_value).toBe(7);
    expect(me(b).bank).toEqual(["money-1m-2"]);
    expect(pub(a).phase).toBe("play");
  });

  test("Debt Collector blocked by Just Say No; the source lets it stand", async () => {
    const r = rev(a);
    await (await useCard(a, "action-debt_collector-1")).getByRole("button", { name: "Collect 5M" }).click();
    await settle(r);
    const response = b.page.getByRole("region", { name: "Your response" });
    const r2 = rev(b);
    await response.getByRole("button", { name: "Just Say No!" }).click();
    await settle(r2);
    const counter = a.page.getByRole("region", { name: "Your response" });
    await expect(counter).toContainText("said Just Say No");
    // The source holds no Just Say No, so only letting it stand is offered.
    await expect(counter.getByRole("button", { name: "Just Say No!" })).toHaveCount(0);
    const r3 = rev(a);
    await counter.getByRole("button", { name: "Let it stand" }).click();
    await settle(r3);
    expect(me(b).bank_value).toBe(1);
    expect(latestState(b).view.self.hand).not.toContain("action-just_say_no-1");
    expect(pub(a).center_pile).toEqual(expect.arrayContaining(["action-debt_collector-1", "action-just_say_no-1"]));
  });

  test("Sly Deal: steal a property, then place the received card", async () => {
    const r = rev(a);
    const dialog = await useCard(a, "action-sly_deal-1");
    await dialog.getByLabel("Take").selectOption("property-st-james-place");
    await dialog.getByRole("button", { name: "Steal" }).click();
    await settle(r);
    const r2 = rev(b);
    await b.page.getByRole("region", { name: "Your response" }).getByRole("button", { name: "Accept" }).click();
    await settle(r2);
    const placement = a.page.getByRole("region", { name: "Place a received card" });
    await expect(placement).toContainText("St. James Place");
    await expect(placement.locator("select")).toHaveValue("new:orange");
    const r3 = rev(a);
    await placement.getByRole("button", { name: "Place card" }).click();
    await settle(r3);
    expect(me(a).sets.some((s: { color: string }) => s.color === "orange")).toBe(true);
    expect(me(b).sets.some((s: { color: string }) => s.color === "orange")).toBe(false);
    // Three plays used: end the turn.
    expect(pub(a).plays_left).toBe(0);
    const r4 = rev(a);
    await a.page.locator(".platter-actions").getByRole("button", { name: "End turn" }).click();
    await settle(r4);
    expect(pub(b).active).toBe(latestState(b).view.self.seat);
  });

  test("Rent: the payer gives properties, the collector places them", async () => {
    const r = rev(b);
    const dialog = await useCard(b, "rent-light_blue-brown-1");
    await dialog.getByLabel("Charge rent on").selectOption({ label: "Brown (2 cards, 2M)" });
    await dialog.getByRole("button", { name: "Charge rent to everyone" }).click();
    await settle(r);
    const r2 = rev(a);
    await a.page.getByRole("region", { name: "Your response" }).getByRole("button", { name: "Accept" }).click();
    await settle(r2);
    const payment = a.page.getByRole("region", { name: "Payment" });
    await expect(payment).toContainText("You owe 2M");
    await payment.getByRole("button", { name: "Oriental Avenue, 1M" }).click();
    await payment.getByRole("button", { name: "Vermont Avenue, 1M" }).click();
    const r3 = rev(a);
    await payment.getByRole("button", { name: "Pay 2M" }).click();
    await settle(r3);
    const placement = b.page.getByRole("region", { name: "Place a received card" });
    for (let i = 0; i < 2; i++) {
      const r4 = rev(b);
      await placement.getByRole("button", { name: "Place card" }).click();
      await settle(r4);
    }
    const lightBlue = me(b).sets.find((s: { color: string }) => s.color === "light_blue");
    expect(lightBlue.cards.sort()).toEqual(["property-oriental-avenue", "property-vermont-avenue"]);
    expect(pub(b).phase).toBe("play");
  });

  test("Reorganize: move a wild card between sets without using a play", async () => {
    const plays = pub(b).plays_left;
    await b.page.locator(".platter-actions").getByRole("button", { name: "Reorganize properties" }).click();
    const dialog = b.page.getByRole("dialog", { name: "Reorganize properties" });
    await dialog.getByLabel("Wild Light blue/Brown").selectOption({ label: "Light blue set (2/3)" });
    const r = rev(b);
    await dialog.getByRole("button", { name: "Save arrangement" }).click();
    await settle(r);
    const sets = me(b).sets as { color: string; cards: string[]; complete: boolean }[];
    expect(sets.find((s) => s.color === "light_blue")).toMatchObject({ complete: true });
    expect(sets.find((s) => s.color === "brown")?.cards).toEqual(["property-mediterranean-avenue"]);
    expect(pub(b).plays_left).toBe(plays);
  });

  test("Forced Deal: swap properties; both players place what they received", async () => {
    const r = rev(b);
    const dialog = await useCard(b, "action-forced_deal-1");
    await dialog.getByLabel("Take").selectOption("property-st-james-place");
    await dialog.getByLabel("Give").selectOption("property-mediterranean-avenue");
    await dialog.getByRole("button", { name: "Swap" }).click();
    await settle(r);
    const r2 = rev(a);
    await a.page.getByRole("region", { name: "Your response" }).getByRole("button", { name: "Accept" }).click();
    await settle(r2);
    for (const p of [a, b]) {
      const r3 = rev(p);
      await p.page.getByRole("region", { name: "Place a received card" }).getByRole("button", { name: "Place card" }).click();
      await expect.poll(() => rev(p)).toBeGreaterThan(r3);
    }
    await expect.poll(() => pub(b).phase).toBe("play");
    const colors = (p: Player) => (me(p).sets as { color: string }[]).map((x) => x.color).sort();
    expect(colors(a)).toEqual(["brown", "dark_blue"]);
    expect(colors(b)).toEqual(["light_blue", "orange"]);
  });

  test("Deal Breaker: take a complete set", async () => {
    const r = rev(b);
    const dialog = await useCard(b, "action-deal_breaker-1");
    await dialog.getByLabel("Complete set").selectOption({ label: "Dark blue" });
    await dialog.getByRole("button", { name: "Take the set" }).click();
    await settle(r);
    const r2 = rev(a);
    await a.page.getByRole("region", { name: "Your response" }).getByRole("button", { name: "Accept" }).click();
    await settle(r2);
    const darkBlue = (me(b).sets as { color: string; complete: boolean }[]).find((x) => x.color === "dark_blue");
    expect(darkBlue?.complete).toBe(true);
    expect((me(a).sets as { color: string }[]).some((x) => x.color === "dark_blue")).toBe(false);
    expect(pub(b).plays_left).toBe(0);
  });

  test("an idle player's turn is finished by the table when the clock runs out", async () => {
    await expect(b.page.locator(".turn-clock")).toContainText(/You \d:\d\d/);
    await expect(a.page.locator(".turn-clock")).toContainText(/\d:\d\d/);
    const r = rev(a);
    await expireTurnClock(match);
    // The server sweep runs every 5 seconds.
    await expect.poll(() => rev(a), { timeout: 20_000 }).toBeGreaterThan(r);
    expect(pub(a).active).toBe(latestState(a).view.self.seat);
    await expect(a.page.locator(".game-status strong")).toHaveText(/^Your turn/);
    await expect(a.page.getByText(/ran out of time, so the table made the default move/).first()).toBeVisible();
  });

  test("no hidden card reached the other player at any revision", async () => {
    await expectNoLeaks([a, b]);
    for (const p of [a, b]) expect(p.errors, `${p.name} page errors`).toEqual([]);
  });
});
