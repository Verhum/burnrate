"use client";

import { useUsageStore } from "@/stores/usage-store";

export interface BurnState {
  /** 5h window utilization, rounded to a whole percent. */
  pct: number;
  /** Window is spent: utilization has hit 100. */
  maxed: boolean;
  /** ISO timestamp the 5h window resets at, if known. */
  resetsAt: string | null;
  /** ISO timestamp the 7-day (weekly) window resets at, if known. */
  weeklyResetsAt: string | null;
}

/**
 * Single source of truth for how "burned" the current session window is.
 *
 * `maxed` deliberately keys off utilization alone. The API's session-limit
 * `is_active` flag is not a signal that the window is spent — it can be true
 * at 20% — so keying off it turned the UI red early (see #50).
 */
export function useBurnState(): BurnState {
  const usage = useUsageStore((s) => s.usage);

  const pct = Math.round(usage?.five_hour_util ?? 0);

  return {
    pct,
    maxed: pct >= 100,
    resetsAt: usage?.five_hour_resets_at || null,
    weeklyResetsAt: usage?.seven_day_resets_at || null,
  };
}
