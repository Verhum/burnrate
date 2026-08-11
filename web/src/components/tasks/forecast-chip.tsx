"use client";

import type { ForecastEntry, ReasonCode } from "@/lib/api/types";

/**
 * The scheduler's verdict for one task.
 *
 * The text comes from the server (`reason`), rendered once by the scheduling
 * policy, so this chip cannot describe the queue differently from the daemon.
 * Colour is keyed on the stable `reason_code`, never on parsing the text.
 *
 * Only palette tokens declared in globals.css `@theme` exist — see web/AGENTS.md.
 */
export function ForecastChip({ forecast }: { forecast: ForecastEntry }) {
  const cls = `text-[9px] font-bold uppercase tracking-wider px-2 py-0.5 ${toneFor(
    forecast.reason_code,
  )}`;
  return (
    <span className={cls} title={forecast.reason}>
      {forecast.reason}
    </span>
  );
}

function toneFor(code: ReasonCode): string {
  switch (code) {
    // About to run, or already running: this is the healthy path.
    case "ready":
    case "running":
      return "bg-sage/20 text-sage";

    // Waiting on quota. Nothing is wrong, but nothing will move until a limit
    // resets, so it earns the attention colour.
    case "session_exhausted":
    case "weekly_exhausted":
    case "weekly_backoff":
      return "bg-amber/15 text-amber";

    // Something needs a human: the task is out of attempts, or the daemon
    // cannot see usage data to schedule against.
    case "attempt_cap":
    case "stale_usage_data":
    case "no_usage_data":
      return "bg-danger/15 text-danger";

    // Ordinary queueing — deliberately quiet.
    case "workers_busy":
    case "queue_empty":
    default:
      return "bg-muted/40 text-dim";
  }
}
