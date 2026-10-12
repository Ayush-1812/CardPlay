import { expect, test } from "@playwright/test";
import { newPlayer } from "./helpers";

// A free host parks the API when nobody has played for a while, and while it
// starts the router in front of it answers with an HTML error page instead of
// the API's JSON. The client must wait that out rather than tell the player
// the site is broken.

// Replies a gateway sends while a service is starting: an HTML body, never
// the JSON the API itself would return.
const gatewayPage = (status: number) => ({
  status,
  contentType: "text/html",
  body: "<html><body>Service Unavailable</body></html>",
});

test("a sleeping server is waited out on a read", async ({ browser }) => {
  const p = await newPlayer(browser, "waker");
  let asleep = true;
  let attempts = 0;

  await p.page.route("**/api/v1/**", async (route) => {
    if (asleep && route.request().method() === "GET") {
      attempts++;
      if (attempts >= 3) asleep = false;
      await route.fulfill(gatewayPage(502));
      return;
    }
    await route.continue();
  });

  await p.page.goto("/");
  await expect(p.page.getByText(/Starting the server/)).toBeVisible();
  await expect(
    p.page.getByText(/unexpected response|Cannot reach CardPlay/),
  ).toHaveCount(0);
  await expect(
    p.page.getByRole("button", { name: "Play as guest" }),
  ).toBeVisible({ timeout: 30000 });
  await expect(p.page.getByText(/Starting the server/)).toHaveCount(0);
  expect(attempts, "the read should have been retried").toBeGreaterThanOrEqual(
    3,
  );
  expect(p.errors).toEqual([]);
  await p.context.close();
});

// The failure a player actually hit: the page loaded, and the first action
// after it -- signing in as a guest, a POST -- met the sleeping server.
test("a write that never reached the server is retried, not reported as broken", async ({
  browser,
}) => {
  const p = await newPlayer(browser, "writer");
  let refusals = 0;

  await p.page.route("**/api/v1/auth/guest", async (route) => {
    if (refusals < 2) {
      refusals++;
      // 503 from the router: the request never reached the application, so
      // repeating it cannot create two guests.
      await route.fulfill(gatewayPage(503));
      return;
    }
    await route.continue();
  });

  await p.page.goto("/");
  await p.page.getByLabel("Your name").fill(p.name);
  await p.page.getByRole("button", { name: "Play as guest" }).click();

  // It must get through to the game, not stop at an error.
  await expect(
    p.page.getByRole("heading", { name: "Choose a game" }),
  ).toBeVisible({ timeout: 40000 });
  await expect(p.page.getByText(/unexpected response/)).toHaveCount(0);
  expect(refusals, "the write should have been refused twice first").toBe(2);
  expect(p.errors).toEqual([]);
  await p.context.close();
});

// An error page with a status outside the usual gateway set still is not JSON,
// so it is handled the same way rather than shown as "unexpected response".
test("any non-JSON reply is treated as the server being unavailable", async ({
  browser,
}) => {
  const p = await newPlayer(browser, "oddity");
  let served = 0;

  await p.page.route("**/api/v1/**", async (route) => {
    if (route.request().method() === "GET" && served < 2) {
      served++;
      // 403 is what an edge bot-check returns, with an HTML body.
      await route.fulfill(gatewayPage(403));
      return;
    }
    await route.continue();
  });

  await p.page.goto("/");
  await expect(
    p.page.getByRole("button", { name: "Play as guest" }),
  ).toBeVisible({ timeout: 30000 });
  await expect(p.page.getByText(/unexpected response/)).toHaveCount(0);
  expect(served).toBe(2);
  expect(p.errors).toEqual([]);
  await p.context.close();
});

// When it really will not come back, the message says what is happening and
// what to do, instead of blaming the response.
test("a server that never answers gives an honest message", async ({
  browser,
}) => {
  const p = await newPlayer(browser, "downer");
  await p.page.route("**/api/v1/**", (route) =>
    route.fulfill(gatewayPage(502)),
  );

  await p.page.goto("/");
  await expect(
    p.page.getByText(/CardPlay is starting up and did not answer/),
  ).toBeVisible({
    timeout: 90000,
  });
  await expect(p.page.getByText(/unexpected response/)).toHaveCount(0);
  await p.context.close();
});
