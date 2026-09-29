// Cross-platform .env loader. Does not echo values or interpolate a shell string.
import { existsSync, readFileSync } from "node:fs";
import { spawn } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const env = { ...process.env };
if (existsSync(path.join(root, ".env"))) {
  for (const line of readFileSync(path.join(root, ".env"), "utf8").split(/\r?\n/)) {
    if (!line || line.startsWith("#")) continue;
    const i = line.indexOf("=");
    if (i > 0 && !(line.slice(0, i) in env)) env[line.slice(0, i)] = line.slice(i + 1);
  }
}
let [command, ...args] = process.argv.slice(2);
if (!command) throw new Error("Usage: node scripts/run.mjs COMMAND [ARGS...]");
if (command === "go" && process.platform === "win32" && existsSync("C:/Program Files/Go/bin/go.exe")) command = "C:/Program Files/Go/bin/go.exe";
const child = spawn(command, args, { cwd: root, env, stdio: "inherit", shell: false });
child.on("error", (error) => { console.error(error.message); process.exitCode = 1; });
child.on("exit", (code) => { process.exitCode = code ?? 1; });
