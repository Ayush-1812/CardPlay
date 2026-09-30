import { spawn } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

// The API runs outside Playwright's webServer so a test can kill and restart
// it mid-match. Its PID is shared through a file between setup, tests and
// teardown, which run in different processes.
const pidFile = join(tmpdir(), "cardplay-e2e-api.pid");
export const API_ADDR = process.env.E2E_API_ADDR ?? "127.0.0.1:18080";

export function apiEnv(base: string): NodeJS.ProcessEnv {
  return {
    ...process.env,
    APP_ENV: "test",
    HTTP_ADDR: API_ADDR,
    APP_ORIGIN: base,
    SMTP_ADDR: process.env.E2E_SMTP_ADDR ?? "127.0.0.1:1026",
    MAIL_FROM: "hello@cardplay.test",
    LOG_FORMAT: "json",
  };
}

export async function startAPI(base: string) {
  const child = spawn(process.env.E2E_API_BIN!, ["serve"], { env: apiEnv(base), stdio: "ignore", detached: true });
  child.unref();
  writeFileSync(pidFile, String(child.pid));
  for (let i = 0; i < 100; i++) {
    try {
      if ((await fetch(`http://${API_ADDR}/readyz`)).ok) return;
    } catch {}
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error("API did not become ready");
}

export async function stopAPI() {
  let pid: number;
  try {
    pid = Number(readFileSync(pidFile, "utf8"));
  } catch {
    return;
  }
  try {
    process.kill(pid);
  } catch {}
  for (let i = 0; i < 50; i++) {
    try {
      await fetch(`http://${API_ADDR}/healthz`);
    } catch {
      return;
    }
    await new Promise((r) => setTimeout(r, 100));
  }
}
