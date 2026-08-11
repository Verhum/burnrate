"use client";

import { useBurnState } from "@/hooks/use-burn-state";
import { useNow } from "@/hooks/use-now";
import { formatCountdown, formatLongCountdown, formatResetDay } from "@/lib/format";

export function BurnRateBar() {
  const { pct, maxed, resetsAt: resetsAtStr, weeklyResetsAt } = useBurnState();
  const now = useNow(1000);

  const countdown = resetsAtStr ? formatCountdown(new Date(resetsAtStr), now) : "--";
  const weeklyLabel = formatResetDay(weeklyResetsAt);
  const weeklyIn = weeklyResetsAt ? formatLongCountdown(new Date(weeklyResetsAt), now) : "--";

  // Gray while the window still has headroom; red only once it's spent.
  const textClass = maxed ? "text-danger" : "text-dim";
  const barClass = maxed ? "bg-danger" : "bg-dim";

  return (
    <section data-tour="burn-bar" className={`px-6 py-3 mt-0.5 flex items-center gap-5 ${maxed ? "bg-danger/30" : "bg-surface"}`}>
      {/* Percentage */}
      <div className="flex items-baseline gap-2">
        <span className={`text-4xl font-extrabold tabular-nums tracking-tight leading-none transition-colors duration-500 ${textClass}`}>
          {pct}<span className="text-lg font-bold">%</span>
        </span>
      </div>

      {/* Bar */}
      <div className="flex-1 min-w-0">
        <div className="relative h-5 bg-elevated overflow-hidden">
          <div
            className={`absolute inset-y-0 left-0 ${barClass} transition-[width,background-color] duration-1000 ease-out`}
            style={{ width: `${Math.min(pct, 100)}%` }}
          />
        </div>
        <div className="flex items-center gap-2 mt-1 text-[10px]">
          {/* The countdown never wraps — "58m 7s" split across two lines
              reflows the whole bar every second. */}
          <span
            className={`font-bold tabular-nums whitespace-nowrap shrink-0 transition-colors duration-500 ${textClass}`}
          >
            {countdown}
          </span>
          {weeklyLabel && (
            <span
              className="text-muted tracking-wide truncate min-w-0"
              title={`7-day window resets ${weeklyLabel} (in ${weeklyIn})`}
            >
              (weekly resets at {weeklyLabel} &middot; <span className="tabular-nums">{weeklyIn}</span>)
            </span>
          )}
        </div>
      </div>
    </section>
  );
}
