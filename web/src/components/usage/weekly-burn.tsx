"use client";

import { useUsageStore } from "@/stores/usage-store";
import { useNow } from "@/hooks/use-now";
import { formatLongCountdown, formatResetDay } from "@/lib/format";

export function WeeklyBurn() {
  const usage = useUsageStore((s) => s.usage);
  const sevenDayUtil = usage?.seven_day_util ?? 0;
  const scoped = usage?.scoped_weekly ?? [];
  const pct = Math.round(sevenDayUtil);
  const resetsAtStr = usage?.seven_day_resets_at || null;
  const resetLabel = formatResetDay(resetsAtStr);
  // Minute granularity is enough — this countdown is measured in days.
  const now = useNow(30_000);
  const resetIn = resetsAtStr ? formatLongCountdown(new Date(resetsAtStr), now) : "--";

  return (
    <section className="bg-surface px-6 py-4 mt-0.5">
      <div className="flex items-center justify-between mb-3">
        <span className="text-[9px] font-bold tracking-widest text-dim uppercase">WEEKLY BURN</span>
        {resetLabel && (
          <span className="text-[9px] font-bold tracking-widest text-muted uppercase tabular-nums">
            RESETS IN {resetIn}
          </span>
        )}
      </div>

      <div className="flex flex-col gap-2.5">
        <UtilRow label="7D" percent={pct} />
        {scoped.map((entry) => {
          const model = entry.model || "";
          const percent = Math.round(entry.percent ?? 0);
          const short = model.replace(/^claude-/, "").replace(/-\d+$/, "");
          return <UtilRow key={model} label={short} percent={percent} />;
        })}
      </div>

      {resetLabel && (
        <div className="mt-2 text-[9px] text-muted tracking-wide">
          (weekly resets at {resetLabel})
        </div>
      )}
    </section>
  );
}

function UtilRow({ label, percent }: { label: string; percent: number }) {
  return (
    <div className="flex items-center gap-3">
      <span className="text-[11px] font-bold text-dim tracking-wider min-w-[100px] truncate">{label}</span>
      <div className="relative flex-1 h-2.5 bg-elevated overflow-hidden">
        <div
          className="absolute inset-y-0 left-0 bg-warm transition-[width] duration-1000 ease-out"
          style={{ width: `${Math.min(percent, 100)}%` }}
        />
      </div>
      <span className="text-[11px] font-bold text-dim tabular-nums min-w-9 text-right">{percent}%</span>
    </div>
  );
}
