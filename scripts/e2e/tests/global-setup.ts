import { startAPI, stopAPI } from "./api";

export default async function globalSetup() {
  for (const name of ["E2E_API_BIN", "DATABASE_URL"])
    if (!process.env[name]) throw new Error(`Set ${name} (see scripts/e2e/README.md)`);
  await stopAPI();
  await startAPI(process.env.E2E_BASE!);
  return async () => stopAPI();
}
