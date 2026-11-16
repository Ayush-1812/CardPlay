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

// A challenge cannot be solved by replaying requests through the proxy.
test("a hosting challenge is reported without startup retries", async ({
  browser,
}) => {
  const p = await newPlayer(browser, "oddity");
  let served = 0;

  await p.page.route("**/api/v1/**", async (route) => {
    if (route.request().method() === "GET") {
      served++;
      // 403 is what an edge bot-check returns, with an HTML body.
      await route.fulfill(gatewayPage(403));
      return;
    }
    await route.continue();
  });

  await p.page.goto("/");
  await expect(
    p.page.getByText(/hosting provider is blocking this connection/),
  ).toBeVisible();
  await expect(p.page.getByText(/Starting the server/)).toHaveCount(0);
  expect(served).toBe(1);
  expect(p.errors).toEqual([]);
  await p.context.close();
});

for (const method of ["GET", "POST"]) {
  test(`a hosting rate limit on ${method} is reported without startup retries`, async ({
    browser,
  }) => {
    const p = await newPlayer(browser, "limited");
    let attempts = 0;
    await p.page.route("**/api/v1/**", async (route) => {
      if (method === "POST" && route.request().method() === "GET") {
        await route.fulfill({
          status: 401,
          json: { error: { code: "UNAUTHENTICATED" } },
        });
        return;
      }
      attempts++;
      await route.fulfill({
        status: 429,
        contentType: "text/plain",
        headers: { "X-Render-Routing": "hibernate-rate-limited" },
        body: "Too Many Requests",
      });
    });
    await p.page.goto("/");
    if (method === "POST") {
      await p.page.getByLabel("Your name").fill(p.name);
      await p.page.getByRole("button", { name: "Play as guest" }).click();
    }
    await expect(
      p.page.getByText(/temporarily rate-limited by its hosting provider/),
    ).toBeVisible();
    await expect(p.page.getByText(/Starting the server/)).toHaveCount(0);
    expect(attempts).toBe(1);
    expect(p.errors).toEqual([]);
    await p.context.close();
  });
}

test("a gateway timeout never repeats a guest creation", async ({
  browser,
}) => {
  const p = await newPlayer(browser, "timeout");
  let attempts = 0;
  await p.page.route("**/api/v1/**", async (route) => {
    if (route.request().method() === "GET") {
      await route.fulfill({
        status: 401,
        json: { error: { code: "UNAUTHENTICATED" } },
      });
      return;
    }
    attempts++;
    await route.fulfill(gatewayPage(504));
  });
  await p.page.goto("/");
  await p.page.getByLabel("Your name").fill(p.name);
  await p.page.getByRole("button", { name: "Play as guest" }).click();
  await expect(
    p.page.getByText(/did not return a valid response/),
  ).toBeVisible();
  expect(attempts).toBe(1);
  expect(p.errors).toEqual([]);
  await p.context.close();
});

test("a cold start lasting over a minute can recover", async ({ browser }) => {
  const p = await newPlayer(browser, "slow-start");
  await p.page.clock.install();
  let ready = false;
  await p.page.route("**/api/v1/**", (route) =>
    route.fulfill(
      ready
        ? { status: 401, json: { error: { code: "UNAUTHENTICATED" } } }
        : gatewayPage(502),
    ),
  );
  await p.page.goto("/");
  await expect(p.page.getByText(/Starting the server/)).toBeVisible();
  await p.page.clock.runFor(61000);
  await expect(p.page.getByText(/Starting the server/)).toBeVisible();
  ready = true;
  await p.page.clock.runFor(16000);
  await expect(
    p.page.getByRole("button", { name: "Play as guest" }),
  ).toBeVisible();
  await expect(p.page.getByText(/did not respond/)).toHaveCount(0);
  expect(p.errors).toEqual([]);
  await p.context.close();
});

for (const stalledPart of ["headers", "body"]) {
  test(`a stalled response ${stalledPart} cannot leave the screen loading forever`, async ({
    browser,
  }) => {
    const p = await newPlayer(browser, "stalled");
    await p.page.clock.install();
    await p.page.addInitScript((part) => {
      window.fetch = async (_input, init) => {
        const aborted = () => new DOMException("Aborted", "AbortError");
        if (part === "headers") {
          return new Promise<Response>((_resolve, reject) => {
            init?.signal?.addEventListener("abort", () => reject(aborted()));
          });
        }
        return new Response(
          new ReadableStream({
            start(controller) {
              init?.signal?.addEventListener("abort", () =>
                controller.error(aborted()),
              );
            },
          }),
        );
      };
    }, stalledPart);
    await p.page.goto("/");
    await expect(p.page.getByText("Finding your seat…")).toBeVisible();
    await p.page.clock.runFor(91000);
    await expect(
      p.page.getByText(/did not respond within 90 seconds/),
    ).toBeVisible();
    await expect(
      p.page.getByRole("button", { name: "Play as guest" }),
    ).toBeVisible();
    expect(p.errors).toEqual([]);
    await p.context.close();
  });
}

// An outage ends within the budget, and does not poison later manual attempts.
test("a server that never answers gives an honest message", async ({
  browser,
}) => {
  const p = await newPlayer(browser, "downer");
  await p.page.clock.install();
  await p.page.route("**/api/v1/**", (route) =>
    route.fulfill(gatewayPage(502)),
  );

  await p.page.goto("/");
  await expect(p.page.getByText(/Starting the server/)).toBeVisible();
  await p.page.clock.runFor(91000);
  await expect(
    p.page.getByText(/did not respond within 90 seconds/),
  ).toBeVisible();
  let attempts = 0;
  await p.page.route("**/api/v1/auth/guest", (route) => {
    attempts++;
    return route.fulfill(
      attempts === 1
        ? gatewayPage(503)
        : {
            status: 503,
            json: { error: { code: "TEST", message: "Reached the API again" } },
          },
    );
  });
  await p.page.getByLabel("Your name").fill(p.name);
  await p.page.getByRole("button", { name: "Play as guest" }).click();
  await expect(p.page.getByText(/Starting the server/)).toBeVisible();
  await p.page.clock.runFor(2000);
  await expect(p.page.getByText("Reached the API again")).toBeVisible();
  expect(attempts).toBe(2);
  expect(p.errors).toEqual([]);
  await p.context.close();
});
