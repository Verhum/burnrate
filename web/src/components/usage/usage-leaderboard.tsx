"use client";

import { useEffect, useState } from "react";
import { client } from "@/lib/api/client";
import type {
  LeaderboardData,
  FastestBurnEntry,
  HighestDailyEntry,
  MaxBurnRateEntry,
  MostTasksDailyEntry,
} from "@/lib/api/types";

function formatBurnDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  const mo = String(d.getMonth() + 1).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  return `${mo}-${dd} ${hh}:${mm}`;
}

function formatDay(dateStr: string): string {
  const d = new Date(dateStr + "T00:00:00");
  return d.toLocaleDateString("en", { month: "short", day: "numeric" });
}

function formatDollars(v: number): string {
  if (v >= 10) return `$${Math.round(v)}`;
  if (v >= 1) return `$${v.toFixed(1)}`;
  return `$${v.toFixed(2)}`;
}

const RANK_COLORS = ["#F59E0B", "#8A8378", "#6B6560", "#5C5347", "#3D3A36"];
const TODAY_COLOR = "#F97316";

export function UsageLeaderboard() {
  const [data, setData] = useState<LeaderboardData | null>(null);

  useEffect(() => {
    client.getLeaderboard().then(setData).catch(() => {});
  }, []);

  const burns = data?.fastest_burns ?? [];
  const daily = data?.highest_daily_spend ?? [];
  const burnRates = data?.max_burn_rates ?? [];
  const tasksDays = data?.most_tasks_daily ?? [];

  if (!burns.length && !daily.length && !burnRates.length && !tasksDays.length)
    return null;

  return (
    <div className="bg-surface px-4 py-3 min-w-0 overflow-hidden">
      <p className="text-[9px] font-bold uppercase tracking-widest text-dim font-mono mb-3">
        USAGE LEADERBOARD
      </p>

      <div className="flex flex-col gap-4">
        {burns.length > 0 && (
          <FastestBurnsTable entries={burns} today={data?.today_fastest_burn} />
        )}

        {(daily.length > 0 || burnRates.length > 0 || tasksDays.length > 0) && (
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            {daily.length > 0 ? (
              <HighestDailyTable
                entries={daily}
                today={data?.today_daily_spend}
              />
            ) : (
              <div />
            )}
            {burnRates.length > 0 ? (
              <MaxBurnRateTable
                entries={burnRates}
                today={data?.today_max_burn_rate}
              />
            ) : (
              <div />
            )}
            {tasksDays.length > 0 ? (
              <MostTasksDailyTable
                entries={tasksDays}
                today={data?.today_most_tasks}
              />
            ) : (
              <div />
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function TodaySeparator() {
  return (
    <div className="flex items-center gap-1 py-0.5">
      <div className="flex-1 border-t border-dashed" style={{ borderColor: TODAY_COLOR, opacity: 0.4 }} />
      <span className="text-[7px] font-mono font-bold uppercase" style={{ color: TODAY_COLOR, opacity: 0.6 }}>
        today
      </span>
      <div className="flex-1 border-t border-dashed" style={{ borderColor: TODAY_COLOR, opacity: 0.4 }} />
    </div>
  );
}

function FastestBurnsTable({
  entries,
  today,
}: {
  entries: FastestBurnEntry[];
  today?: FastestBurnEntry;
}) {
  const showTodayBelow = today && today.rank > entries.length;
  const refDuration = entries[entries.length - 1]?.duration_s ?? 1;

  return (
    <div className="min-w-0">
      <p className="text-[8px] font-bold uppercase tracking-wider text-muted font-mono mb-1.5">
        FASTEST TO 100%
      </p>
      <div className="flex flex-col gap-0.5">
        {entries.map((e) => {
          const isToday = !!e.is_today;
          const rankColor = isToday
            ? TODAY_COLOR
            : RANK_COLORS[e.rank - 1] ?? RANK_COLORS[4];
          const textColor = isToday ? TODAY_COLOR : undefined;
          return (
            <div key={e.rank} className="flex items-center gap-2 py-0.5">
              <span
                className="text-[11px] font-bold font-mono min-w-[16px] text-right"
                style={{ color: rankColor }}
              >
                {e.rank}
              </span>
              <span
                className="text-[11px] font-bold font-mono tabular-nums"
                style={{ color: textColor }}
              >
                {formatBurnDuration(e.duration_s)}
              </span>
              <div className="relative flex-1 min-w-0 h-[10px] bg-elevated overflow-hidden">
                <div
                  className="absolute inset-y-0 left-0 bg-danger"
                  style={{
                    width: `${Math.min(100, (refDuration / Math.max(1, e.duration_s)) * 100)}%`,
                  }}
                />
                <div
                  className="absolute inset-y-0 left-0 transition-[width] duration-1000 ease-out"
                  style={{
                    width: `${Math.min(100, (refDuration / Math.max(1, e.duration_s)) * 100)}%`,
                    backgroundColor: rankColor,
                    opacity: 0.6,
                  }}
                />
              </div>
              <span
                className="text-[9px] font-mono tabular-nums shrink-0 text-right"
                style={{ color: textColor ?? undefined }}
              >
                {formatDate(e.started_at)}
              </span>
            </div>
          );
        })}
        {showTodayBelow && today && (
          <>
            <TodaySeparator />
            <div className="flex items-center gap-2 py-0.5">
              <span
                className="text-[11px] font-bold font-mono min-w-[16px] text-right"
                style={{ color: TODAY_COLOR }}
              >
                {today.rank}
              </span>
              <span
                className="text-[11px] font-bold font-mono tabular-nums"
                style={{ color: TODAY_COLOR }}
              >
                {formatBurnDuration(today.duration_s)}
              </span>
              <div className="relative flex-1 min-w-0 h-[10px] bg-elevated overflow-hidden">
                <div
                  className="absolute inset-y-0 left-0"
                  style={{
                    width: `${Math.min(100, (refDuration / Math.max(1, today.duration_s)) * 100)}%`,
                    backgroundColor: TODAY_COLOR,
                    opacity: 0.6,
                  }}
                />
              </div>
              <span
                className="text-[9px] font-mono tabular-nums shrink-0 text-right"
                style={{ color: TODAY_COLOR }}
              >
                {formatDate(today.started_at)}
              </span>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function HighestDailyTable({
  entries,
  today,
}: {
  entries: HighestDailyEntry[];
  today?: HighestDailyEntry;
}) {
  const allForMax = today && today.rank > entries.length ? [...entries, today] : entries;
  const maxSpend = Math.max(...allForMax.map((e) => e.peak_spend), 0.01);
  const showTodayBelow = today && today.rank > entries.length;

  return (
    <div className="min-w-0">
      <p className="text-[8px] font-bold uppercase tracking-wider text-muted font-mono mb-1.5">
        TOP SPEND DAYS
      </p>
      <div className="flex flex-col gap-0.5">
        {entries.map((e) => {
          const isToday = !!e.is_today;
          const rankColor = isToday
            ? TODAY_COLOR
            : RANK_COLORS[e.rank - 1] ?? RANK_COLORS[4];
          const textColor = isToday ? TODAY_COLOR : undefined;
          return (
            <div key={e.rank} className="flex items-center gap-1.5 py-0.5">
              <span
                className="text-[11px] font-bold font-mono min-w-[14px] text-right"
                style={{ color: rankColor }}
              >
                {e.rank}
              </span>
              <span
                className="text-[11px] font-bold font-mono tabular-nums min-w-[36px]"
                style={{ color: textColor }}
              >
                {formatDollars(e.peak_spend)}
              </span>
              <div className="relative flex-1 min-w-0 h-[10px] bg-elevated overflow-hidden">
                <div
                  className="absolute inset-y-0 left-0 bg-danger"
                  style={{ width: `${(e.peak_spend / maxSpend) * 100}%` }}
                />
                <div
                  className="absolute inset-y-0 left-0 transition-[width] duration-1000 ease-out"
                  style={{
                    width: `${(e.peak_spend / maxSpend) * 100}%`,
                    backgroundColor: rankColor,
                    opacity: 0.6,
                  }}
                />
              </div>
              <span
                className="text-[9px] font-mono tabular-nums shrink-0 text-right"
                style={{ color: textColor ?? undefined }}
              >
                {formatDay(e.date)}
              </span>
            </div>
          );
        })}
        {showTodayBelow && today && (
          <>
            <TodaySeparator />
            <div className="flex items-center gap-1.5 py-0.5">
              <span
                className="text-[11px] font-bold font-mono min-w-[14px] text-right"
                style={{ color: TODAY_COLOR }}
              >
                {today.rank}
              </span>
              <span
                className="text-[11px] font-bold font-mono tabular-nums min-w-[36px]"
                style={{ color: TODAY_COLOR }}
              >
                {formatDollars(today.peak_spend)}
              </span>
              <div className="relative flex-1 min-w-0 h-[10px] bg-elevated overflow-hidden">
                <div
                  className="absolute inset-y-0 left-0"
                  style={{
                    width: `${(today.peak_spend / maxSpend) * 100}%`,
                    backgroundColor: TODAY_COLOR,
                    opacity: 0.6,
                  }}
                />
              </div>
              <span
                className="text-[9px] font-mono tabular-nums shrink-0 text-right"
                style={{ color: TODAY_COLOR }}
              >
                {formatDay(today.date)}
              </span>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function MaxBurnRateTable({
  entries,
  today,
}: {
  entries: MaxBurnRateEntry[];
  today?: MaxBurnRateEntry;
}) {
  const allForMax = today && today.rank > entries.length ? [...entries, today] : entries;
  const maxRate = Math.max(...allForMax.map((e) => e.rate_per_h), 0.01);
  const showTodayBelow = today && today.rank > entries.length;

  return (
    <div className="min-w-0">
      <p className="text-[8px] font-bold uppercase tracking-wider text-muted font-mono mb-1.5">
        MAX $/HR BURN
      </p>
      <div className="flex flex-col gap-0.5">
        {entries.map((e) => {
          const isToday = !!e.is_today;
          const rankColor = isToday
            ? TODAY_COLOR
            : RANK_COLORS[e.rank - 1] ?? RANK_COLORS[4];
          const textColor = isToday ? TODAY_COLOR : undefined;
          return (
            <div key={e.rank} className="flex items-center gap-1.5 py-0.5">
              <span
                className="text-[11px] font-bold font-mono min-w-[14px] text-right"
                style={{ color: rankColor }}
              >
                {e.rank}
              </span>
              <span
                className="text-[11px] font-bold font-mono tabular-nums min-w-[40px]"
                style={{ color: textColor }}
              >
                {formatDollars(e.rate_per_h)}/h
              </span>
              <div className="relative flex-1 min-w-0 h-[10px] bg-elevated overflow-hidden">
                <div
                  className="absolute inset-y-0 left-0 bg-danger"
                  style={{ width: `${(e.rate_per_h / maxRate) * 100}%` }}
                />
                <div
                  className="absolute inset-y-0 left-0 transition-[width] duration-1000 ease-out"
                  style={{
                    width: `${(e.rate_per_h / maxRate) * 100}%`,
                    backgroundColor: rankColor,
                    opacity: 0.6,
                  }}
                />
              </div>
              <span
                className="text-[9px] font-mono tabular-nums shrink-0 text-right"
                style={{ color: textColor ?? undefined }}
              >
                {formatDay(e.date)}
              </span>
            </div>
          );
        })}
        {showTodayBelow && today && (
          <>
            <TodaySeparator />
            <div className="flex items-center gap-1.5 py-0.5">
              <span
                className="text-[11px] font-bold font-mono min-w-[14px] text-right"
                style={{ color: TODAY_COLOR }}
              >
                {today.rank}
              </span>
              <span
                className="text-[11px] font-bold font-mono tabular-nums min-w-[40px]"
                style={{ color: TODAY_COLOR }}
              >
                {formatDollars(today.rate_per_h)}/h
              </span>
              <div className="relative flex-1 min-w-0 h-[10px] bg-elevated overflow-hidden">
                <div
                  className="absolute inset-y-0 left-0"
                  style={{
                    width: `${(today.rate_per_h / maxRate) * 100}%`,
                    backgroundColor: TODAY_COLOR,
                    opacity: 0.6,
                  }}
                />
              </div>
              <span
                className="text-[9px] font-mono tabular-nums shrink-0 text-right"
                style={{ color: TODAY_COLOR }}
              >
                {formatDay(today.date)}
              </span>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function MostTasksDailyTable({
  entries,
  today,
}: {
  entries: MostTasksDailyEntry[];
  today?: MostTasksDailyEntry;
}) {
  const allForMax = today && today.rank > entries.length ? [...entries, today] : entries;
  const maxCount = Math.max(...allForMax.map((e) => e.count), 1);
  const showTodayBelow = today && today.rank > entries.length;

  return (
    <div className="min-w-0">
      <p className="text-[8px] font-bold uppercase tracking-wider text-muted font-mono mb-1.5">
        MOST TASKS / DAY
      </p>
      <div className="flex flex-col gap-0.5">
        {entries.map((e) => {
          const isToday = !!e.is_today;
          const rankColor = isToday
            ? TODAY_COLOR
            : RANK_COLORS[e.rank - 1] ?? RANK_COLORS[4];
          const textColor = isToday ? TODAY_COLOR : undefined;
          return (
            <div key={e.rank} className="flex items-center gap-1.5 py-0.5">
              <span
                className="text-[11px] font-bold font-mono min-w-[14px] text-right"
                style={{ color: rankColor }}
              >
                {e.rank}
              </span>
              <span
                className="text-[11px] font-bold font-mono tabular-nums min-w-[24px]"
                style={{ color: textColor }}
              >
                {e.count}
              </span>
              <div className="relative flex-1 min-w-0 h-[10px] bg-elevated overflow-hidden">
                <div
                  className="absolute inset-y-0 left-0 bg-danger"
                  style={{ width: `${(e.count / maxCount) * 100}%` }}
                />
                <div
                  className="absolute inset-y-0 left-0 transition-[width] duration-1000 ease-out"
                  style={{
                    width: `${(e.count / maxCount) * 100}%`,
                    backgroundColor: rankColor,
                    opacity: 0.6,
                  }}
                />
              </div>
              <span
                className="text-[9px] font-mono tabular-nums shrink-0 text-right"
                style={{ color: textColor ?? undefined }}
              >
                {formatDay(e.date)}
              </span>
            </div>
          );
        })}
        {showTodayBelow && today && (
          <>
            <TodaySeparator />
            <div className="flex items-center gap-1.5 py-0.5">
              <span
                className="text-[11px] font-bold font-mono min-w-[14px] text-right"
                style={{ color: TODAY_COLOR }}
              >
                {today.rank}
              </span>
              <span
                className="text-[11px] font-bold font-mono tabular-nums min-w-[24px]"
                style={{ color: TODAY_COLOR }}
              >
                {today.count}
              </span>
              <div className="relative flex-1 min-w-0 h-[10px] bg-elevated overflow-hidden">
                <div
                  className="absolute inset-y-0 left-0"
                  style={{
                    width: `${(today.count / maxCount) * 100}%`,
                    backgroundColor: TODAY_COLOR,
                    opacity: 0.6,
                  }}
                />
              </div>
              <span
                className="text-[9px] font-mono tabular-nums shrink-0 text-right"
                style={{ color: TODAY_COLOR }}
              >
                {formatDay(today.date)}
              </span>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
