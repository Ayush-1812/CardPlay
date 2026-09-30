// Browser error reporting. Reports go to the API, which logs bounded fields;
// nothing is sent to third parties. Each page load sends at most ten reports
// and never the same message twice.
const sent = new Set<string>();

export type ClientErrorKind = "error" | "unhandledrejection" | "render";

export function reportError(
  kind: ClientErrorKind,
  error: unknown,
  digest?: string,
) {
  const message =
    error instanceof Error ? `${error.name}: ${error.message}` : String(error);
  const key = `${kind}:${message}`;
  if (sent.has(key) || sent.size >= 10) return;
  sent.add(key);
  const body = JSON.stringify({
    kind,
    message: message.slice(0, 500),
    // Path only: query strings and invite fragments stay in the browser.
    page: window.location.pathname,
    stack: error instanceof Error ? (error.stack ?? "").slice(0, 2000) : "",
    digest: digest ?? "",
  });
  void fetch("/api/v1/client-errors", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body,
    keepalive: true,
  }).catch(() => {});
}
