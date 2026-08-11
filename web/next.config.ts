import type { NextConfig } from "next";

// Where `next dev` proxies /api and /health. The dev stack runs the daemon on
// DEV_PORT (see the root Makefile), so this is an override rather than a
// constant; the default matches a plain `burnrate serve`.
const apiOrigin = process.env.BURNRATE_API_ORIGIN || "http://localhost:9112";

const nextConfig: NextConfig = {
  // Do NOT set `distDir: "out"`. `output: "export"` writes the static export to
  // `out/` regardless, and `out/` is a `//go:embed all:out` target (web.go) plus
  // Tauri's `frontendDist`. Pointing distDir at it too puts Next's *build* dir
  // inside the embed: `next dev` writes `<distDir>/dev/_events_<pid>.json` and
  // deletes it again, so a concurrent `go build` walks the embed tree and dies
  // with "embed out/dev/_events_*.json: no such file or directory". Leaving
  // distDir at the default keeps that churn in the gitignored `.next/`.
  output: "export",
  // Only applies during `next dev` — ignored by static export.
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${apiOrigin}/api/:path*`,
      },
      {
        source: "/health",
        destination: `${apiOrigin}/health`,
      },
    ];
  },
};

export default nextConfig;
