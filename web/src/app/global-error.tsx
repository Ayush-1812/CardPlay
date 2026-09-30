"use client";
import { useEffect } from "react";
import { reportError } from "../lib/report";

// Replaces the root layout when it fails, so it renders its own document.
export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => reportError("render", error, error.digest), [error]);
  return (
    <html lang="en">
      <body>
        <main style={{ padding: 24, fontFamily: "system-ui, sans-serif" }}>
          <h1>Something went wrong</h1>
          <p>Your match is saved on the server. Reload to return to it.</p>
          <button type="button" onClick={reset}>
            Try again
          </button>
        </main>
      </body>
    </html>
  );
}
