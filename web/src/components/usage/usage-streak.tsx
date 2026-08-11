"use client";

import { useEffect, useState, type ReactNode } from "react";
import { client } from "@/lib/api/client";
import type { StreakData } from "@/lib/api/types";
import {
  formatCompact,
  formatStreakDay,
  formatStreakRange,
  formatStreakSpend,
} from "@/lib/streak";

export function UsageStreak() {
  const [data, setData] = useState<StreakData | null>(null);

  useEffect(() => {
    client.getStreak().then(setData).catch(() => {});
  }, []);

  if (!data || data.total_runs === 0) return null;

  const alive = data.current_streak > 0;
  const longestRange = formatStreakRange(data.longest_start, data.longest_end);
  const perDay =
    data.active_days > 0 ? data.total_cost_usd / data.active_days : 0;

  return (
    <div className="bg-surface px-4 py-3 min-w-0 overflow-hidden">
      <div className="flex items-center justify-between gap-2 mb-2">
        <p className="text-[9px] font-bold uppercase tracking-widest text-dim font-mono">
          STREAK
        </p>
        {data.first_day && (
          <span className="text-[9px] font-mono text-muted shrink-0">
            since {formatStreakDay(data.first_day)}
          </span>
        )}
      </div>

      <div className="flex items-end justify-between gap-4 mb-2.5 min-w-0">
        <div className="flex items-baseline gap-2 min-w-0">
          <span
            className={`text-[32px] leading-none font-bold font-mono tabular-nums ${
              alive ? "text-gold" : "text-muted"
            }`}
          >
            {data.current_streak}
          </span>
          <span className="text-[9px] font-bold font-mono uppercase tracking-wider text-muted">
            day streak
          </span>
        </div>
        <div className="text-right shrink-0">
          <p className="text-[8px] font-bold uppercase tracking-wider text-muted font-mono">
            LONGEST
          </p>
          <p className="text-[11px] font-bold font-mono tabular-nums text-primary">
            {data.longest_streak}d
            {longestRange && (
              <span className="font-normal text-dim"> {longestRange}</span>
            )}
          </p>
        </div>
      </div>

      <div className="grid grid-cols-3 gap-x-4 gap-y-1.5">
        <Stat label="LIFETIME" value={formatStreakSpend(data.total_cost_usd)} />
        <Stat label="TASKS" value={formatCompact(data.total_tasks)} />
        <Stat label="RUNS" value={formatCompact(data.total_runs)} />
        <Stat label="ACTIVE DAYS" value={String(data.active_days)} />
        <Stat label="$/DAY" value={formatStreakSpend(perDay)} />
        <Stat
          label="LINES"
          value={
            <>
              <span className="text-sage">
                +{formatCompact(data.lines_added)}
              </span>{" "}
              <span className="text-danger">
                −{formatCompact(data.lines_removed)}
              </span>
            </>
          }
        />
      </div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="min-w-0">
      <p className="text-[8px] font-bold uppercase tracking-wider text-muted font-mono">
        {label}
      </p>
      <p className="text-[11px] font-bold font-mono tabular-nums text-primary truncate">
        {value}
      </p>
    </div>
  );
}
