import { expect, test, type Page } from "@playwright/test";
import { newPlayer, playAsGuest, type Player } from "./helpers";

// The how-to-play guides, from a new player's point of view: they must be
// reachable before choosing a game, while waiting in a room, and from the
// table itself -- and they must actually say how the game works.

// What each guide has to cover. If a guide loses one of these, someone
// sits down not knowing how to take their turn.
const MUST_SAY: Record<string, (string | RegExp)[]> = {
  "Monopoly Deal": [
    /three complete property sets/i,
    /draw 2 cards/i,
    /up to 3/i,
    /never pay from your hand/i,
    /Just Say No/i,
  ],
  Trump: [
    /7 of the 13 tricks/i,
    /opposite you are a team/i,
    /must play the same suit/i,
    /highest trump wins/i,
    /chat and voice are closed/i,
  ],
  Bluff: [
    /first player with no cards/i,
    /Ace, 2, 3/i,
    /cannot pass/i,
    /five seconds/i,
    /picks up the whole pile/i,
  ],
};

async function readsWell(page: Page, game: string) {
  const dialog = page.getByRole("dialog", { name: `How to play ${game}` });
  await expect(dialog).toBeVisible();
  // The shape every guide shares.
  for (const heading of [
    "What you are trying to do",
    "In short",
    "What you see on screen",
    "Worth knowing",
  ])
    await expect(dialog.getByRole("heading", { name: heading })).toBeVisible();
  for (const phrase of MUST_SAY[game])
    await expect(dialog).toContainText(phrase);
  // Written for someone new: no jargon from the codebase.
  const text = await dialog.innerText();
  for (const word of ["revision", "websocket", "projection", "payload"])
    expect(
      text.toLowerCase(),
      `a guide should not mention "${word}"`,
    ).not.toContain(word);
}

test.describe.serial("how to play", () => {
  let p: Player;

  test.beforeAll(async ({ browser }) => {
    p = await newPlayer(browser, "reader", {
      viewport: { width: 1280, height: 900 },
    });
    await playAsGuest(p);
  });
  test.afterAll(async () => {
    await p?.context.close();
  });

  test("every playable game has a guide on the games screen", async () => {
    for (const game of ["Monopoly Deal", "Trump", "Bluff"]) {
      await p.page
        .getByRole("button", { name: `How to play ${game}` })
        .first()
        .click();
      await readsWell(p.page, game);
      await p.page.keyboard.press("Escape");
      await expect(
        p.page.getByRole("dialog", { name: `How to play ${game}` }),
      ).toHaveCount(0);
    }
  });

  test("opening the rules does not start a game by accident", async () => {
    await p.page
      .getByRole("button", { name: "How to play Trump" })
      .first()
      .click();
    await expect(
      p.page.getByRole("dialog", { name: "How to play Trump" }),
    ).toBeVisible();
    await p.page.keyboard.press("Escape");
    // Still on the games screen, not in Trump's lobby.
    await expect(
      p.page.getByRole("heading", { name: "Choose a game" }),
    ).toBeVisible();
  });

  test("the guide is there in the lobby and in the room", async () => {
    await p.page.getByRole("button", { name: "Bluff", exact: true }).click();
    await p.page
      .getByRole("button", { name: "How to play Bluff" })
      .first()
      .click();
    await readsWell(p.page, "Bluff");
    await p.page.keyboard.press("Escape");

    await p.page.getByRole("button", { name: "Create room" }).click();
    await expect(
      p.page.getByRole("heading", { level: 1, name: /'s table$/ }),
    ).toBeVisible();
    await p.page.getByRole("button", { name: "How to play" }).first().click();
    await readsWell(p.page, "Bluff");
    await p.page.keyboard.press("Escape");
  });

  test("and at the table, mid-game", async ({ browser }) => {
    // Three players, so a Bluff match can actually start.
    const others: Player[] = [];
    const link = await p.page.getByLabel("Invite link").inputValue();
    for (const name of ["r2", "r3"]) {
      const other = await newPlayer(browser, name);
      await other.page.goto(link);
      await other.page.getByLabel("Your name").fill(other.name);
      await other.page.getByRole("button", { name: "Join as guest" }).click();
      await expect(other.page.locator(".room-head .pill")).toHaveText(
        "Connected",
      );
      others.push(other);
    }
    await p.page.getByText("Room settings").click();
    await p.page
      .locator('.room-settings select[name="capacity"]')
      .selectOption("3");
    await p.page
      .locator(".room-settings")
      .getByRole("button", { name: "Save" })
      .click();
    for (const player of [p, ...others])
      await player.page.getByRole("button", { name: "I'm ready" }).click();
    await p.page.getByRole("button", { name: "Start match" }).click();
    await expect(p.page.locator(".bluff-table")).toBeVisible();

    // The rules open over the table and close back to it.
    await p.page.getByRole("button", { name: "Rules" }).click();
    await readsWell(p.page, "Bluff");
    await p.page.keyboard.press("Escape");
    await expect(p.page.locator(".bluff-table")).toBeVisible();
    await expect(p.page.locator(".hand-card").first()).toBeVisible();

    expect(p.errors, "page errors").toEqual([]);
    for (const other of others) await other.context.close();
  });
});
