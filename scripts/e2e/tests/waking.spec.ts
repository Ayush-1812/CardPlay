import { expect, test } from "@playwright/test";
import { newPlayer } from "./helpers";

// A free host parks the API when nobody has played for a while. The first
// visitor then meets a gateway error until it starts. The client must wait
// that out and say so, rather than claiming the site is unreachable.
test("a sleeping server is waited out, not reported as unreachable", async ({ browser }) => {
  const p = await newPlayer(browser, "waker");
  let asleep = true;
  let attempts = 0;

  // The gateway in front of a sleeping Render service answers 502 until the
  // container is up; here it does so for the first three reads.
  await p.page.route("**/api/v1/**", async (route) => {
    if (asleep && route.request().method() === "GET") {
      attempts++;
      if (attempts >= 3) asleep = false;
      await route.fulfill({
        status: 502,
        contentType: "text/html",
        body: "<html><body>Bad Gateway</body></html>",
      });
      return;
    }
    await route.continue();
  });

  await p.page.goto("/");
  // While it waits, the screen explains the delay instead of failing.
  await expect(p.page.getByText(/Starting the server/)).toBeVisible();
  await expect(p.page.getByText(/Cannot reach CardPlay/)).toHaveCount(0);

  // Once the server answers, the app carries on normally.
  await expect(p.page.getByRole("button", { name: "Play as guest" })).toBeVisible({ timeout: 30000 });
  await expect(p.page.getByText(/Starting the server/)).toHaveCount(0);
  expect(attempts, "the read should have been retried").toBeGreaterThanOrEqual(3);

  // And the retried read really did reach the server: a guest can sign in.
  await p.page.getByLabel("Your name").fill(p.name);
  await p.page.getByRole("button", { name: "Play as guest" }).click();
  await expect(p.page.getByRole("heading", { name: "Choose a game" })).toBeVisible();
  expect(p.errors).toEqual([]);
  await p.context.close();
});
