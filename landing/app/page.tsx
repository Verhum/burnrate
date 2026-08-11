import { TabCarousel } from "@/components/tab-carousel";
import { DownloadLink } from "@/components/download-link";

export default function HomePage() {
  return (
    <main className="min-h-dvh">
      {/* ── Nav ── */}
      <header className="sticky top-0 z-50 border-b border-raised bg-bg/80 backdrop-blur-md">
        <div className="mx-auto flex h-14 max-w-5xl items-center justify-between px-5">
          <a href="/" className="flex items-center gap-2.5 text-sm font-semibold tracking-wide text-dim">
            <BurnrateIcon size={16} />
            burnrate
          </a>
          <DownloadLink
            href="#download"
            event="nav_download"
            className="text-xs text-dim transition hover:text-amber"
          >
            Download
          </DownloadLink>
        </div>
      </header>

      {/* ── Hero ── */}
      <section className="relative overflow-hidden">
        <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_80%_50%_at_50%_-20%,rgba(245,158,11,0.08),transparent)]" />
        <div className="relative mx-auto max-w-5xl px-5 py-24 text-center md:py-32">
          <div className="mx-auto mb-8">
            <BurnrateIcon size={48} />
          </div>
          <h1 className="text-4xl font-bold leading-[1.1] tracking-tight text-primary sm:text-5xl md:text-7xl">
            Autonomous task runner<br />
            for <span className="text-amber">Claude Code.</span>
          </h1>
          <p className="mx-auto mt-6 max-w-lg text-base leading-relaxed text-dim font-sans">
            Queue Tasks. Wake up to draft PRs. All cross-session.
          </p>
          <DownloadLink
            href="#download"
            event="hero_download"
            className="mt-8 inline-flex items-center gap-2 border-2 border-amber bg-amber px-8 py-3.5 text-sm font-semibold text-bg transition hover:bg-gold hover:border-gold"
          >
            <DownloadIcon />
            Download for macOS
          </DownloadLink>
        </div>
      </section>

      {/* ── The Loop ── */}
      <section className="border-y border-raised">
        <div className="mx-auto max-w-5xl px-5 py-24 md:py-32">
          <div className="flex flex-col items-center gap-6 md:flex-row md:items-stretch md:gap-0">
            <div className="w-full flex-1 border-2 border-raised bg-surface p-8">
              <span className="text-3xl font-bold text-amber">1</span>
              <h3 className="mt-2 text-xl font-bold text-primary">Add</h3>
              <p className="mt-2 text-sm leading-relaxed text-dim font-sans">
                Describe the task. Pick the repo.
              </p>
            </div>

            <div className="hidden items-center px-3 text-amber md:flex">&rarr;</div>

            <div className="w-full flex-1 border-2 border-raised bg-surface p-8">
              <span className="text-3xl font-bold text-amber">2</span>
              <h3 className="mt-2 text-xl font-bold text-primary">Execute</h3>
              <p className="mt-2 text-sm leading-relaxed text-dim font-sans">
                Quota opens. Agents launch in isolated worktrees.
              </p>
            </div>

            <div className="hidden flex-col items-center justify-center gap-2 px-4 text-amber md:flex">
              <span>&rarr;</span>
              <span className="text-[10px] font-bold uppercase tracking-[2px] text-amber/60">loop</span>
              <span>&larr;</span>
            </div>

            <div className="w-full flex-1 border-2 border-raised bg-surface p-8">
              <span className="text-3xl font-bold text-amber">3</span>
              <h3 className="mt-2 text-xl font-bold text-primary">Review</h3>
              <p className="mt-2 text-sm leading-relaxed text-dim font-sans">
                Each task ships a branch and draft PR. Review and comment.
              </p>
            </div>

            <div className="hidden items-center px-3 text-amber md:flex">&rarr;</div>

            <div className="w-full flex-1 border-2 border-amber/30 bg-surface p-8">
              <span className="text-3xl font-bold text-sage">4</span>
              <h3 className="mt-2 text-xl font-bold text-primary">Done</h3>
              <p className="mt-2 text-sm leading-relaxed text-dim font-sans">
                Approve the PR. Ship to prod.
              </p>
            </div>
          </div>
        </div>
      </section>

      {/* ── App carousel ── */}
      <section className="border-b border-raised">
        <div className="mx-auto max-w-5xl px-5 py-20 md:py-24">
          <TabCarousel />
        </div>
      </section>

      {/* ── Download ── */}
      <section id="download" className="relative overflow-hidden">
        <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_60%_40%_at_50%_100%,rgba(245,158,11,0.06),transparent)]" />
        <div className="relative mx-auto max-w-5xl px-5 py-24 text-center md:py-32">
          <h2 className="text-3xl font-bold text-primary sm:text-4xl">
            Start burning.
          </h2>
          <p className="mx-auto mt-4 max-w-sm text-sm text-dim font-sans">
            macOS only. Apple Silicon. Works with Claude Code Max and Teams.
          </p>
          <DownloadLink
            href="/api/releases/latest"
            event="download"
            className="mt-8 inline-flex items-center gap-2 border-2 border-amber bg-amber px-8 py-3.5 text-sm font-semibold text-bg transition hover:bg-gold hover:border-gold"
          >
            <DownloadIcon />
            Download .dmg
          </DownloadLink>
        </div>
      </section>

      {/* ── Footer ── */}
      <footer className="border-t border-raised">
        <div className="mx-auto flex max-w-5xl flex-col gap-4 px-5 py-6 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-2 text-xs text-muted">
            <BurnrateIcon size={14} />
            burnrate
          </div>
          <p className="text-[11px] text-muted font-sans">
            Made by{" "}
            <a
              href="https://ver-hum.com"
              target="_blank"
              rel="noopener noreferrer"
              className="text-dim transition hover:text-amber"
            >
              Verifiably Human Inc
            </a>
          </p>
        </div>
        <div className="mx-auto max-w-5xl px-5 pb-6">
          <p className="text-[11px] leading-relaxed text-muted font-sans">
            burnrate is an independent tool that drives the Claude Code CLI you install
            yourself. It is not affiliated with, endorsed by, or sponsored by Anthropic.
            Claude and Claude Code are trademarks of Anthropic. macOS and Apple are
            trademarks of Apple Inc.
          </p>
        </div>
      </footer>
    </main>
  );
}

function BurnrateIcon({ size = 16 }: { size?: number }) {
  const bar = Math.round(size * 0.3125);
  return (
    <div className="relative inline-block" style={{ width: size, height: size }}>
      <span
        className="absolute block bg-dim"
        style={{ top: (size - bar) / 2, left: 0, width: size, height: bar }}
      />
      <span
        className="absolute block bg-dim"
        style={{ top: 0, left: (size - bar) / 2, width: bar, height: size }}
      />
    </div>
  );
}

// Apple restricts the Apple logo to its own approved badges, so the download
// buttons use a generic glyph and say "macOS" in words instead.
function DownloadIcon({ size = 16 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
    >
      <path d="M12 3v12" />
      <path d="m7 11 5 5 5-5" />
      <path d="M4 20h16" />
    </svg>
  );
}
