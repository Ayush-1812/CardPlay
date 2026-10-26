import { expect, test, type Page } from "@playwright/test";
import { latestState, newPlayer, playAsGuest, type Player } from "./helpers";

// Every screen, on every shape of device. The sizes are the ones that break
// layouts: the smallest phone still in use, a tall modern phone, a phone
// held sideways (about 390px of height), and a tablet.
const SIZES = [
  { name: "320x568 small phone", width: 320, height: 568 },
  { name: "390x844 tall phone", width: 390, height: 844 },
  { name: "844x390 phone sideways", width: 844, height: 390 },
  { name: "768x1024 tablet", width: 768, height: 1024 },
];

// What the page says about its own size. The hand is a horizontal scroller,
// so documentElement.scrollWidth is unreliable in Chromium: what matters is
// that the page cannot be scrolled sideways and nothing sticks out of it.
async function measure(page: Page) {
  return page.evaluate(() => {
    const vw = document.documentElement.clientWidth;
    const vh = document.documentElement.clientHeight;
    window.scrollTo(500, 0);
    const moved = window.scrollX;
    window.scrollTo(0, 0);
    const inScroller = (el: Element) => {
      let node: Element | null = el.parentElement;
      while (node) {
        const s = getComputedStyle(node);
        if (
          (s.overflowX === "auto" || s.overflowX === "scroll") &&
          node.scrollWidth > node.clientWidth + 1
        )
          return true;
        node = node.parentElement;
      }
      return false;
    };
    const overflowing: string[] = [];
    document.querySelectorAll("*").forEach((el) => {
      const box = el.getBoundingClientRect();
      if (box.width === 0 || box.height === 0) return;
      // A closed drawer is parked off-screen and made inert; that is not
      // something a player can reach or scroll to.
      if (el.closest("[inert]")) return;
      if ((box.right > vw + 1 || box.left < -1) && !inScroller(el))
        overflowing.push(
          el.tagName.toLowerCase() +
            (typeof el.className === "string" && el.className.trim()
              ? "." + el.className.trim().split(/\s+/).slice(0, 2).join(".")
              : ""),
        );
    });
    // Anything a finger has to find must be at least 36px in both
    // directions. Cards are exempt: they overlap in a rail by design and
    // each already reserves a 44px tap target.
    const small: string[] = [];
    document
      .querySelectorAll(
        ".table-home, .text-button, .chat-close, .trump-actions button, .dock-bar button, .appbar-actions button",
      )
      .forEach((el) => {
        const box = el.getBoundingClientRect();
        if (box.width === 0 || box.height === 0) return;
        if (box.height < 36 || box.width < 36)
          small.push(
            `${el.className} ${Math.round(box.width)}x${Math.round(box.height)}`,
          );
      });
    return {
      moved,
      vw,
      vh,
      bodyWidth: document.body.scrollWidth,
      pageHeight: document.documentElement.scrollHeight,
      overflowing: [...new Set(overflowing)].slice(0, 6),
      small: [...new Set(small)].slice(0, 6),
    };
  });
}

// `fits` is for a live table, which must hold itself to one screen; a
// content page may be taller and scroll.
async function check(p: Player, where: string, fits: boolean) {
  for (const size of SIZES) {
    await p.page.setViewportSize({ width: size.width, height: size.height });
    // Let the layout settle after the resize before measuring it.
    await p.page.waitForTimeout(200);
    const m = await measure(p.page);
    const at = `${where} at ${size.name}`;
    expect(m.moved, `${at}: the page must not scroll sideways`).toBe(0);
    expect(
      m.bodyWidth,
      `${at}: the body must fit the screen`,
    ).toBeLessThanOrEqual(m.vw);
    expect(m.overflowing, `${at}: nothing may stick out sideways`).toEqual([]);
    expect(m.small, `${at}: controls must stay tappable`).toEqual([]);
    if (fits)
      expect(
        m.pageHeight,
        `${at}: a table holds itself to one screen`,
      ).toBeLessThanOrEqual(m.vh + 1);
  }
}

test.describe.serial("every screen fits every device", () => {
  const players: Player[] = [];
  let link: string;
  const pub = (p: Player) => latestState(p).view.public;
  const self = (p: Player) => latestState(p).view.self;

  test.beforeAll(async ({ browser }) => {
    for (const name of ["ra", "rb", "rc", "rd"])
      players.push(
        await newPlayer(browser, name, {
          viewport: { width: 390, height: 844 },
          hasTouch: true,
        }),
      );
  });
  test.afterAll(async () => {
    for (const p of players) await p?.context.close();
  });

  test("the games screen and the room", async () => {
    const host = players[0];
    await playAsGuest(host);
    await check(host, "the games screen", false);
    await host.page.getByRole("button", { name: "Trump", exact: true }).click();
    await check(host, "the Trump entry", false);
    await host.page.setViewportSize({ width: 390, height: 844 });
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
    await check(host, "a full room", false);
  });

  test("the Trump table holds itself to one screen", async () => {
    for (const p of players)
      await p.page.getByRole("button", { name: "I'm ready" }).click();
    await players[0].page.getByRole("button", { name: "Start match" }).click();
    for (const p of players)
      await expect(p.page.locator(".trump-table")).toBeVisible();
    await check(players[0], "trump selection", true);

    const entitled = pub(players[0]).entitled_team;
    const chooser = players.find((p) => self(p).seat % 2 === entitled)!;
    await chooser.page.setViewportSize({ width: 390, height: 844 });
    await check(chooser, "the trump choice", true);
    // The choice and the way to pass it on are on screen together, even on
    // the smallest phone: neither is a scroll away.
    for (const size of SIZES) {
      await chooser.page.setViewportSize(size);
      await expect(
        chooser.page.getByRole("button", { name: /Spades/ }),
      ).toBeInViewport();
      await expect(
        chooser.page.getByRole("button", {
          name: "My teammate will choose trump",
        }),
      ).toBeInViewport();
    }
    await chooser.page.setViewportSize({ width: 390, height: 844 });
    await chooser.page
      .getByRole("button", { name: /Hearts|Spades|Clubs|Diamonds/ })
      .first()
      .click();
    for (const p of players)
      await expect(p.page.locator(".trump-indicator")).toBeVisible();
    await check(players[0], "play", true);

    // The hand and the controls are in the dock, on screen at every size,
    // whatever the table above them is doing.
    for (const size of SIZES) {
      await players[0].page.setViewportSize(size);
      await expect(
        players[0].page.locator(".trump-hand-rail"),
      ).toBeInViewport();
      await expect(
        players[0].page.getByRole("button", { name: "Leave match" }),
      ).toBeInViewport();
    }
    await players[0].page.setViewportSize({ width: 390, height: 844 });
  });
});

test.describe.serial("the Monopoly table fits every device", () => {
  const players: Player[] = [];
  test.beforeAll(async ({ browser }) => {
    for (const name of ["rm", "rn"])
      players.push(
        await newPlayer(browser, name, {
          viewport: { width: 390, height: 844 },
          hasTouch: true,
        }),
      );
  });
  test.afterAll(async () => {
    for (const p of players) await p?.context.close();
  });

  test("a live table on a phone, sideways and on a tablet", async () => {
    const [host, guest] = players;
    await playAsGuest(host);
    await host.page
      .getByRole("button", { name: "Monopoly Deal", exact: true })
      .click();
    await host.page.getByRole("button", { name: "Create room" }).click();
    await expect(
      host.page.getByRole("heading", { level: 1, name: /'s table$/ }),
    ).toBeVisible();
    const link = await host.page.getByLabel("Invite link").inputValue();
    await guest.page.goto(link);
    await guest.page.getByLabel("Your name").fill(guest.name);
    await guest.page.getByRole("button", { name: "Join as guest" }).click();
    await expect(guest.page.locator(".room-head .pill")).toHaveText(
      "Connected",
    );
    await host.page.getByText("Room settings").click();
    await host.page
      .locator('.room-settings select[name="capacity"]')
      .selectOption("2");
    await host.page
      .locator(".room-settings")
      .getByRole("button", { name: "Save" })
      .click();
    for (const p of players)
      await p.page.getByRole("button", { name: "I'm ready" }).click();
    await host.page.getByRole("button", { name: "Start match" }).click();
    for (const p of players)
      await expect(p.page.locator(".game-table")).toBeVisible();

    await check(host, "the Monopoly table", true);
    // The hand stays docked at the bottom of the screen at every size.
    for (const size of SIZES) {
      await host.page.setViewportSize(size);
      await expect(host.page.locator(".hand-rail, .hand-fan")).toBeInViewport();
    }
    for (const p of players)
      expect(p.errors, `${p.name} page errors`).toEqual([]);
  });
});
