import { randomBytes } from "node:crypto";
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const env = path.join(root, ".env");
if (existsSync(env)) {
  console.log(".env already exists; left unchanged.");
} else {
  const password = randomBytes(24).toString("hex");
  const seed = randomBytes(18).toString("hex");
  writeFileSync(env, `APP_ENV=development\nHTTP_ADDR=:8080\nAPP_ORIGIN=http://localhost:3000\nPOSTGRES_USER=cardplay\nPOSTGRES_DB=cardplay\nPOSTGRES_PASSWORD=${password}\nDATABASE_URL=postgres://cardplay:${password}@127.0.0.1:55432/cardplay?sslmode=disable\nSEED_PASSWORD=${seed}\nSMTP_ADDR=localhost:1025\nMAIL_FROM=hello@cardplay.test\nAPI_INTERNAL_URL=http://127.0.0.1:8080\n`, { mode: 0o600, flag: "wx" });
  console.log("Created ignored .env with random development credentials. Read SEED_PASSWORD locally to sign in; do not share it.");
}
mkdirSync(path.join(root, ".local"), { recursive: true });
