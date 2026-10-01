import type { NextConfig } from "next";

const api = process.env.API_INTERNAL_URL ?? "http://127.0.0.1:8080";
const dev = process.env.NODE_ENV !== "production";
// Set when the API is published on its own origin, so the browser opens the
// WebSocket there instead of through this server. The CSP has to allow it.
const apiOrigin = (process.env.NEXT_PUBLIC_API_ORIGIN ?? "").replace(/\/$/, "");
const socketOrigins = apiOrigin
  ? ` ${apiOrigin} ${apiOrigin.replace(/^http/, "ws")}`
  : "";

// Next.js hydration uses inline scripts, so script-src needs 'unsafe-inline'
// (and 'unsafe-eval' in development). Everything else is locked to this
// origin: no framing, plugins, foreign form targets or third-party requests.
const csp = [
  "default-src 'self'",
  `script-src 'self' 'unsafe-inline'${dev ? " 'unsafe-eval'" : ""}`,
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data:",
  "font-src 'self'",
  `connect-src 'self'${socketOrigins}`,
  "object-src 'none'",
  "base-uri 'self'",
  "form-action 'self'",
  "frame-ancestors 'none'",
].join("; ");

const securityHeaders = [
  { key: "Content-Security-Policy", value: csp },
  { key: "X-Frame-Options", value: "DENY" },
  { key: "X-Content-Type-Options", value: "nosniff" },
  // Invite tokens travel in the URL fragment, which is never sent; keep
  // referrers minimal anyway.
  { key: "Referrer-Policy", value: "no-referrer" },
  {
    key: "Permissions-Policy",
    value: "camera=(), microphone=(), geolocation=(), payment=()",
  },
  { key: "Cross-Origin-Opener-Policy", value: "same-origin" },
  // Set ENABLE_HSTS=1 when building for an HTTPS-only deployment.
  ...(process.env.ENABLE_HSTS === "1"
    ? [
        {
          key: "Strict-Transport-Security",
          value: "max-age=31536000; includeSubDomains",
        },
      ]
    : []),
];

const config: NextConfig = {
  // Standalone is for the Docker image. Platforms that build the app
  // themselves, such as Vercel, use their own output and reject it.
  output: process.env.STANDALONE_BUILD === "1" ? "standalone" : undefined,
  poweredByHeader: false,
  async headers() {
    return [{ source: "/:path*", headers: securityHeaders }];
  },
  async rewrites() {
    return [
      { source: "/api/:path*", destination: `${api}/api/:path*` },
      { source: "/ws", destination: `${api}/ws` },
    ];
  },
};
export default config;
