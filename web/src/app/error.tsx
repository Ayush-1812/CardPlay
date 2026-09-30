"use client";
import { useEffect } from "react";
import { reportError } from "../lib/report";

export default function ErrorPage({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => reportError("render", error, error.digest), [error]);
  return (
    <main>
      <section className="panel" role="alert">
        <h1>Something went wrong</h1>
        <p>
          The page hit an unexpected error. Your match is saved on the server;
          reloading brings you back to your seat.
        </p>
        <button type="button" className="primary" onClick={reset}>
          Try again
        </button>
      </section>
    </main>
  );
}
