"use client";

import { useEffect } from "react";
import { useUsageStore } from "@/stores/usage-store";
import { useNow } from "@/hooks/use-now";
import { formatCountdown, formatLongCountdown, formatResetDay } from "@/lib/format";
import { Card, CardBody, Spinner } from "@/components/ui";
import { BurnChart } from "./burn-chart";
import { CostEfficiencyPanel } from "./cost-efficiency";
import { DailySpend } from "./daily-spend";
import { ContributionGrid } from "./contribution-grid";
import { UsageLeaderboard } from "./usage-leaderboard";
import { UsageStreak } from "./usage-streak";
import { UsageAchievements } from "./usage-achievements";

export function UsageDashboard() {
  const { usage, fetchUsage } = useUsageStore();
  const now = useNow(1000);

  useEffect(() => {
    fetchUsage();
  }, [fetchUsage]);

  const fiveHourUtil = usage?.five_hour_util ?? 0;
  const sevenDayUtil = usage?.seven_day_util ?? 0;
  const resetsAtStr = usage?.five_hour_resets_at || null;
  const scopedWeekly = usage?.scoped_weekly ?? [];
  const weeklyResetsAtStr = usage?.seven_day_resets_at || null;

  const countdown = resetsAtStr ? formatCountdown(new Date(resetsAtStr), now) : "--";
  const weeklyCountdown = weeklyResetsAtStr
    ? formatLongCountdown(new Date(weeklyResetsAtStr), now)
    : "--";

  if (!usage) {
    return (
      <Card>
        <CardBody className="flex items-center justify-center py-8">
          <Spinner size="md" />
        </CardBody>
      </Card>
    );
  }

  const fiveHourPct = Math.round(fiveHourUtil);
  const sevenDayPct = Math.round(sevenDayUtil);
  const weeklyResetLabel = formatResetDay(weeklyResetsAtStr);

  return (
    <div className="flex flex-col" style={{ gap: "2px" }}>
      {/* API Utilization */}
      <div className="bg-surface px-4 py-3 min-w-0 overflow-hidden">
        <p className="text-[9px] font-bold uppercase tracking-widest text-dim font-mono mb-2">
          API UTILIZATION
        </p>
        <div className="flex flex-col gap-1.5">
          <UtilRow label="5H WINDOW" pct={fiveHourPct} />
          <UtilRow label="7D WINDOW" pct={sevenDayPct} />
          <div className="flex items-center gap-2 mt-0.5">
            <span className="text-[10px] font-mono text-muted min-w-[90px]">5H RESETS IN</span>
            <span className="text-[10px] font-mono text-dim tabular-nums">{countdown}</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-[10px] font-mono text-muted min-w-[90px]">7D RESETS IN</span>
            <span className="text-[10px] font-mono text-dim tabular-nums">{weeklyCountdown}</span>
            {weeklyResetLabel && (
              <span className="text-[10px] font-mono text-muted">({weeklyResetLabel})</span>
            )}
          </div>
        </div>
      </div>

      {/* Weekly by Model */}
      {scopedWeekly.length > 0 && (
        <div className="bg-surface px-4 py-3 min-w-0 overflow-hidden">
          <p className="text-[9px] font-bold uppercase tracking-widest text-dim font-mono mb-2">
            WEEKLY BY MODEL
          </p>
          <div className="flex flex-col gap-1.5">
            {scopedWeekly.map((entry) => {
              const model = entry.model || "unknown";
              const pct = Math.round(entry.percent ?? 0);
              const short = model.replace(/^claude-/, "").replace(/-\d+$/, "");
              return <UtilRow key={model} label={short.toUpperCase()} pct={pct} />;
            })}
          </div>
        </div>
      )}

      {/* Activity Streak */}
      <UsageStreak />

      {/* Achievements */}
      <UsageAchievements />

      {/* Usage Leaderboard */}
      <UsageLeaderboard />

      {/* Burn Rate Chart */}
      <BurnChart />

      {/* Daily Spend */}
      <DailySpend />

      {/* Cost per task / per line of code, by model, over time */}
      <CostEfficiencyPanel />

      {/* Contribution Grid */}
      <ContributionGrid />
    </div>
  );
}

function UtilRow({ label, pct }: { label: string; pct: number }) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-[10px] font-mono text-muted min-w-[90px] tracking-wider truncate">{label}</span>
      <div className="relative flex-1 min-w-0 h-[14px] bg-elevated overflow-hidden">
        <div
          className="absolute inset-y-0 left-0 bg-warm transition-[width] duration-1000 ease-out"
          style={{ width: `${Math.min(pct, 100)}%` }}
        />
      </div>
      <span className="text-[10px] font-mono text-dim tabular-nums min-w-[36px] text-right">
        {pct}%
      </span>
    </div>
  );
}
