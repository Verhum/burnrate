"use client";

import { useState } from "react";

const TABS = [
  { id: "tasks", label: "Tasks", caption: "Review queue, task list, and weekly burn at a glance." },
  { id: "runs", label: "Runs", caption: "Every agent run — status, cost, turns, and duration." },
  { id: "usage", label: "Usage", caption: "Utilization bars, leaderboards, burn rate chart, and daily spend." },
  { id: "config", label: "Config", caption: "All settings in one place. Edit and save." },
];

export function TabCarousel() {
  const [active, setActive] = useState(0);

  const prev = () => setActive((i) => (i - 1 + TABS.length) % TABS.length);
  const next = () => setActive((i) => (i + 1) % TABS.length);

  return (
    <div>
      {/* Image area with left/right arrows */}
      <div className="relative border-2 border-raised bg-surface">
        {TABS.map((t, i) => (
          <img
            key={t.id}
            src={`/tab-${t.id}.png`}
            alt={`burnrate ${t.label} tab`}
            width={1440}
            height={900}
            className={`w-full block ${i === active ? "" : "hidden"}`}
            loading={i === 0 ? "eager" : "lazy"}
          />
        ))}

        {/* Left arrow */}
        <button
          onClick={prev}
          className="absolute left-0 top-1/2 -translate-y-1/2 -translate-x-1/2 flex h-10 w-10 items-center justify-center border-2 border-raised bg-elevated text-dim transition hover:text-amber hover:border-amber/40 cursor-pointer"
          aria-label="Previous tab"
        >
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="square">
            <path d="M10 3L5 8L10 13" />
          </svg>
        </button>

        {/* Right arrow */}
        <button
          onClick={next}
          className="absolute right-0 top-1/2 -translate-y-1/2 translate-x-1/2 flex h-10 w-10 items-center justify-center border-2 border-raised bg-elevated text-dim transition hover:text-amber hover:border-amber/40 cursor-pointer"
          aria-label="Next tab"
        >
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="square">
            <path d="M6 3L11 8L6 13" />
          </svg>
        </button>
      </div>

      {/* Tab pills below with gap */}
      <div className="mt-5 flex justify-center gap-3">
        {TABS.map((t, i) => (
          <button
            key={t.id}
            onClick={() => setActive(i)}
            className={`px-4 py-2 text-xs font-bold uppercase tracking-[3px] border-2 transition cursor-pointer ${
              i === active
                ? "border-amber bg-elevated text-amber"
                : "border-raised bg-surface text-muted hover:border-dim hover:text-primary"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      <p className="mt-3 text-center text-xs text-muted font-sans">
        {TABS[active].caption}
      </p>
    </div>
  );
}
