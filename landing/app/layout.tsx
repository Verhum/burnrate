import type { Metadata, Viewport } from "next";
import { Analytics } from "@vercel/analytics/next";
import "./globals.css";

const SITE_URL = "https://burnthemtokens.com";

export const metadata: Metadata = {
  metadataBase: new URL(SITE_URL),
  title: "Burn Rate — Never Waste a Session Window",
  description:
    "Autonomous task runner for Claude Code. Queue tasks, quota resets, work ships — even while you sleep.",
  openGraph: {
    type: "website",
    url: SITE_URL,
    siteName: "Burn Rate",
    title: "Burn Rate — Never Waste a Session Window",
    description:
      "Autonomous task runner for Claude Code. Queue tasks, quota resets, work ships.",
  },
  twitter: {
    card: "summary_large_image",
    title: "Burn Rate",
    description:
      "Autonomous task runner for Claude Code. Queue tasks, quota resets, work ships.",
  },
};

export const viewport: Viewport = {
  themeColor: "#0F0E0D",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className="antialiased">
      <body>
        {children}
        <Analytics />
      </body>
    </html>
  );
}
