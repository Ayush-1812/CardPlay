import { defineConfig } from "@playwright/test";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

// Release browser suite. Playwright starts an isolated Mailpit and a
// production build of the web client; global setup starts the API so tests
// can restart it. See README.md. Use a throwaway database.
const need = (name: string) => {
  const v = process.env[name];
  if (!v) throw new Error(`Set ${name} (see scripts/e2e/README.md)`);
  return v;
};
const WEB_PORT = process.env.E2E_WEB_PORT ?? "3100";
const MAIL_SMTP = process.env.E2E_SMTP_ADDR ?? "127.0.0.1:1026";
const MAIL_HTTP = process.env.E2E_MAIL_HTTP ?? "127.0.0.1:8026";
const BASE = `http://127.0.0.1:${WEB_PORT}`;
process.env.E2E_BASE = BASE;
process.env.E2E_MAIL_API = `http://${MAIL_HTTP}`;

export default defineConfig({
  testDir: "./tests",
  globalSetup: "./tests/global-setup.ts",
  // Specs share one API and database, and each test already drives several
  // concurrent players, so files run one at a time.
  workers: 1,
  fullyParallel: false,
  timeout: 5 * 60_000,
  expect: { timeout: 20_000 },
  retries: 0,
  reporter: [["list"], ["html", { open: "never", outputFolder: "playwright-report" }]],
  use: {
    baseURL: BASE,
    channel: process.env.E2E_CHANNEL ?? "msedge",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    actionTimeout: 20_000,
  },
  webServer: [
    // Without E2E_MAILPIT_BIN an already running Mailpit (for example a CI
    // service container) must listen on E2E_SMTP_ADDR / E2E_MAIL_HTTP.
    ...(process.env.E2E_MAILPIT_BIN
      ? [
          {
            command: `"${process.env.E2E_MAILPIT_BIN}" --smtp ${MAIL_SMTP} --listen ${MAIL_HTTP} --database "${join(mkdtempSync(join(tmpdir(), "cardplay-mail-")), "mail.db")}"`,
            url: `http://${MAIL_HTTP}/api/v1/info`,
            reuseExistingServer: false,
            timeout: 30_000,
          },
        ]
      : []),
    {
      // A production build made with API_INTERNAL_URL pointing at E2E_API_ADDR.
      command: `npx next start -p ${WEB_PORT} -H 127.0.0.1`,
      cwd: need("E2E_WEB_DIR"),
      url: BASE,
      reuseExistingServer: false,
      timeout: 60_000,
    },
  ],
});
