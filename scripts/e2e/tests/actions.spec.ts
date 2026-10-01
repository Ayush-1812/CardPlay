import { expect, test } from "@playwright/test";
import { expireTurnClock, stageDeal } from "./db";
import {
  createRoom,
  expectNoLeaks,
  joinInvitation,
  latestState,
  makeFriends,
  newPlayer,
  registerViaAPI,
  tapHandCard,
  userIDOf,
  type Player,
} from "./helpers";

// Action cards, responses, payments, placement and moving cards, through
// the real sheets. The deal is staged at revision 0 (before any move) so
// every card needed is in hand; everything after that goes through the UI.
// Turns end by themselves after the third play.
test.describe.serial("action cards through the UI", () => {
  let one: Player, two: Player;
  // a: the player whose turn it is first; b: the other one.
  let a: Player, b: Player;
  let match: string;

  const pub = (p: Player) => latestState(p).view.public;
  const me = (p: Player) => pub(p).players[latestState(p).view.self.seat];
  const rev = (p: Player) => latestState(p)?.revision ?? -1;
  const myTurn = (p: Player) => pub(p).active === latestState(p).view.self.seat;
  // Waits until both players have seen the state after the next move.
  const settle = async (from: number) => {
    for (const p of [a, b]) await expect.poll(() => rev(p)).toBeGreaterThan(from);
  };
  const dialog = (p: Player, name?: string) => p.page.getByRole("dialog", name ? { name } : undefined);

  test.beforeAll(async ({ browser }) => {
    one = await newPlayer(browser, "ada");
    two = await newPlayer(browser, "bo");
    for (const p of [one, two]) {
      await p.page.goto("/");
      await registerViaAPI(p);
    }
    await makeFriends(one, two);
    await createRoom(one, "Action table", 2);
    await one.page.locator(".invite-panel .friend-row", { hasText: `@${two.handle}` }).getByRole("button", { name: "Invite" }).click();
    await joinInvitation(two, "Action table");
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

  test("a multicolor wild offers only the sets it can join, best first", async () => {
    await tapHandCard(a, "wild-multicolor-1");
    const sheet = dialog(a);
    await expect(sheet.getByRole("heading", { name: "Where should this card go?" })).toBeVisible();
    // The incomplete light blue set and "Decide later"; no new set of every
    // color, and the complete dark blue set has no room.
    await expect(sheet.locator(".dest-tile")).toHaveCount(2);
    const best = sheet.locator(".dest-tile").first();
    await expect(best).toContainText("Light blue set");
    await expect(best).toContainText("Best");
    await expect(best).toContainText("Completes the set");
    await expect(sheet.locator(".dest-tile").last()).toContainText("Decide later");
    await a.page.keyboard.press("Escape");
    await expect(dialog(a)).toHaveCount(0);
  });

  test("It's My Birthday: the target pays from one Action! sheet", async () => {
    const r = rev(a);
    await tapHandCard(a, "action-birthday-1");
    await dialog(a).getByRole("button", { name: "Everyone pays you 2M" }).click();
    await settle(r);
    const prompt = dialog(b, "Your response");
    await expect(prompt.getByRole("heading", { name: "Action!" })).toBeVisible();
    await expect(prompt).toContainText("birthday: pay 2M");
    const pay = prompt.getByRole("button", { name: /^Pay/ });
    await expect(pay).toBeDisabled();
    await prompt.getByRole("button", { name: "2M, 2M" }).click();
    const r2 = rev(b);
    await prompt.getByRole("button", { name: "Pay 2M" }).click();
    await settle(r2);
    expect(me(a).bank_value).toBe(7);
    expect(me(b).bank).toEqual(["money-1m-2"]);
    expect(pub(a).phase).toBe("play");
  });

  test("Debt Collector blocked by Just Say No; the source lets it stand", async () => {
    const r = rev(a);
    await tapHandCard(a, "action-debt_collector-1");
    // One opponent: no player to choose.
    await dialog(a).getByRole("button", { name: "Collect 5M from a player" }).click();
    await settle(r);
    const r2 = rev(b);
    await dialog(b, "Your response").getByRole("button", { name: "Just Say No!" }).click();
    await settle(r2);
    const counter = dialog(a, "Your response");
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

  test("Sly Deal: one stealable card is named on the button; it lands by itself", async () => {
    const r = rev(a);
    await tapHandCard(a, "action-sly_deal-1");
    // One player with one card outside a complete set: no picker at all.
    await dialog(a).getByRole("button", { name: `Steal St. James Place from ${b.name}` }).click();
    await settle(r);
    const prompt = dialog(b, "Your response");
    await expect(prompt).toContainText("wants to steal your property");
    await expect(prompt.locator(".trade-preview")).toContainText("They take");
    const r2 = rev(b);
    await prompt.getByRole("button", { name: "Accept" }).click();
    await settle(r2);
    // A one-color property has one home, so nobody is asked where it goes.
    await expect(dialog(a, "Place a received card")).toHaveCount(0);
    expect(me(a).sets.some((s: { color: string }) => s.color === "orange")).toBe(true);
    expect(me(b).sets.some((s: { color: string }) => s.color === "orange")).toBe(false);
    // Three plays spent and seven cards or fewer: the turn ended by itself.
    await expect.poll(() => myTurn(b)).toBe(true);
    await expect(b.page.locator(".game-status strong")).toHaveText(/^Your turn/);
  });

  test("Rent: one matching set charges in one tap; paid properties land by themselves", async () => {
    const r = rev(b);
    await tapHandCard(b, "rent-light_blue-brown-1");
    // Only the brown set matches, so there is no set to choose.
    await dialog(b).getByRole("button", { name: "Charge Brown rent (2M)" }).click();
    await settle(r);
    const payment = dialog(a, "Your response");
    await expect(payment).toContainText("charges you 2M rent");
    await payment.getByRole("button", { name: /^Oriental Avenue/ }).click();
    await payment.getByRole("button", { name: /^Vermont Avenue/ }).click();
    const r2 = rev(a);
    await payment.getByRole("button", { name: "Pay 2M" }).click();
    await settle(r2);
    await expect(dialog(b, "Place a received card")).toHaveCount(0);
    const lightBlue = me(b).sets.find((s: { color: string }) => s.color === "light_blue");
    expect(lightBlue.cards.sort()).toEqual(["property-oriental-avenue", "property-vermont-avenue"]);
    expect(pub(b).phase).toBe("play");
  });

  test("Tap a wild on your table to move it, free", async () => {
    const plays = pub(b).plays_left;
    await b.page.locator(".my-board").getByRole("button", { name: "Move Wild Light blue/Brown" }).click();
    const sheet = dialog(b, "Move Wild Light blue/Brown");
    // Only the light blue set: splitting brown into a new set is not offered.
    await expect(sheet.locator(".dest-tile")).toHaveCount(1);
    const r = rev(b);
    await sheet.getByRole("button", { name: /^Light blue set/ }).click();
    await expect.poll(() => rev(b)).toBeGreaterThan(r);
    const sets = me(b).sets as { color: string; cards: string[]; complete: boolean }[];
    expect(sets.find((s) => s.color === "light_blue")).toMatchObject({ complete: true });
    expect(sets.find((s) => s.color === "brown")?.cards).toEqual(["property-mediterranean-avenue"]);
    expect(pub(b).plays_left).toBe(plays);
  });

  test("Forced Deal: with one card on each side the swap is a single button", async () => {
    const r = rev(b);
    await tapHandCard(b, "action-forced_deal-1");
    await dialog(b)
      .getByRole("button", { name: `Swap your Mediterranean Avenue for ${a.name}'s St. James Place` })
      .click();
    await settle(r);
    const r2 = rev(a);
    await dialog(a, "Your response").getByRole("button", { name: "Accept" }).click();
    await settle(r2);
    for (const p of [a, b]) await expect(dialog(p, "Place a received card")).toHaveCount(0);
    await expect.poll(() => pub(b).phase).toBe("play");
    const colors = (p: Player) => (me(p).sets as { color: string }[]).map((x) => x.color).sort();
    expect(colors(a)).toEqual(["brown", "dark_blue"]);
    expect(colors(b)).toEqual(["light_blue", "orange"]);
  });

  test("Deal Breaker takes a complete set; the third play ends the turn", async () => {
    const r = rev(b);
    await tapHandCard(b, "action-deal_breaker-1");
    // One complete set to take: named on the button.
    await dialog(b).getByRole("button", { name: `Take ${a.name}'s Dark blue set` }).click();
    await settle(r);
    await expect(dialog(a, "Your response")).toContainText("take your complete set");
    const r2 = rev(a);
    await dialog(a, "Your response").getByRole("button", { name: "Accept" }).click();
    await settle(r2);
    const darkBlue = (me(b).sets as { color: string; complete: boolean }[]).find((x) => x.color === "dark_blue");
    expect(darkBlue?.complete).toBe(true);
    expect((me(a).sets as { color: string }[]).some((x) => x.color === "dark_blue")).toBe(false);
    await expect.poll(() => myTurn(a)).toBe(true);
  });

  test("money banks with a single tap", async () => {
    const before = me(a).bank_value;
    const r = rev(a);
    await tapHandCard(a, "money-1m-1");
    await expect.poll(() => rev(a)).toBeGreaterThan(r);
    await expect(dialog(a)).toHaveCount(0);
    expect(me(a).bank_value).toBe(before + 1);
  });

  test("an idle player's turn is finished by the table when the clock runs out", async () => {
    await expect(a.page.locator(".turn-clock")).toContainText(/You \d:\d\d/);
    await expect(b.page.locator(".turn-clock")).toContainText(/\d:\d\d/);
    const r = rev(b);
    await expireTurnClock(match);
    // The server sweep runs every 5 seconds.
    await expect.poll(() => rev(b), { timeout: 20_000 }).toBeGreaterThan(r);
    expect(myTurn(b)).toBe(true);
    await expect(b.page.locator(".game-status strong")).toHaveText(/^Your turn/);
    await expect(b.page.getByText(/ran out of time, so the table made the default move/).first()).toBeAttached();
  });

  test("no hidden card reached the other player at any revision", async () => {
    await expectNoLeaks([a, b]);
    for (const p of [a, b]) expect(p.errors, `${p.name} page errors`).toEqual([]);
  });
});
