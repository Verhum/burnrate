"use client";

import { useEffect, useState } from "react";
import { client } from "@/lib/api/client";
import type { Run } from "@/lib/api/types";
import { useElementWidth } from "@/hooks/use-element-width";

const WEEKS = 13;
const DAYS = 7;
const DAY_LABELS = ["Mon", "", "Wed", "", "Fri", "", ""];
const GAP = 2;
const LABEL_W = 26;
const CELL_MAX = 12;
const CELL_MIN = 5;

/**
 * Largest square cell that fits thirteen weeks plus the weekday gutter into
 * `width`. The grid shrinks rather than overflowing its panel.
 */
function cellSize(width: number): number {
  if (width <= 0) return CELL_MAX;
  const usable = width - LABEL_W - WEEKS * GAP;
  return Math.max(CELL_MIN, Math.min(CELL_MAX, Math.floor(usable / WEEKS)));
}

function getDateGrid(): string[][] {
  const grid: string[][] = [];
  const today = new Date();
  const dayOfWeek = (today.getDay() + 6) % 7; // Mon=0

  for (let w = WEEKS - 1; w >= 0; w--) {
    const week: string[] = [];
    for (let d = 0; d < DAYS; d++) {
      const offset = w * 7 + (dayOfWeek - d);
      const date = new Date(today);
      date.setDate(today.getDate() - offset);
      week.push(date.toISOString().slice(0, 10));
    }
    grid.push(week);
  }
  return grid;
}

function aggregateCost(runs: Run[]): Map<string, number> {
  const map = new Map<string, number>();
  for (const r of runs) {
    if (!r.started_at || !r.cost_usd) continue;
    const day = r.started_at.slice(0, 10);
    map.set(day, (map.get(day) || 0) + r.cost_usd);
  }
  return map;
}

function costToLevel(cost: number, max: number): number {
  if (cost === 0) return 0;
  if (max === 0) return 0;
  const ratio = cost / max;
  if (ratio < 0.15) return 1;
  if (ratio < 0.4) return 2;
  if (ratio < 0.7) return 3;
  return 4;
}

const LEVEL_COLORS = [
  "#1C1B19", // 0: empty
  "#3D3A36", // 1: low
  "#5C5347", // 2: medium
  "#8A8378", // 3: high
  "#F59E0B", // 4: peak
];

export function ContributionGrid() {
  const [runs, setRuns] = useState<Run[]>([]);
  const [tooltip, setTooltip] = useState<{ day: string; cost: number; x: number; y: number } | null>(null);
  const [gridRef, width] = useElementWidth<HTMLDivElement>();

  useEffect(() => {
    client.listRuns({ limit: 1000 }).then(setRuns).catch(() => {});
  }, []);

  const costMap = aggregateCost(runs);
  const maxCost = Math.max(0, ...Array.from(costMap.values()));
  const grid = getDateGrid();
  const cell = cellSize(width);

  const totalDays = Array.from(costMap.keys()).length;
  const totalCost = Array.from(costMap.values()).reduce((a, b) => a + b, 0);

  // Month labels
  const monthLabels: { label: string; col: number }[] = [];
  let lastMonth = "";
  grid.forEach((week, wi) => {
    const mid = week[3] || week[0];
    const month = new Date(mid).toLocaleString("en", { month: "short" });
    if (month !== lastMonth) {
      monthLabels.push({ label: month, col: wi });
      lastMonth = month;
    }
  });

  return (
    <div className="bg-surface px-4 py-3 min-w-0 overflow-hidden">
      <div className="flex items-center justify-between gap-2 mb-3">
        <span className="text-[9px] font-bold tracking-widest text-dim uppercase truncate">
          TOKEN ACTIVITY
        </span>
        <span className="text-[11px] text-dim shrink-0">
          {totalDays} active days &middot; ${totalCost.toFixed(2)}
        </span>
      </div>

      <div ref={gridRef} className="relative w-full min-w-0">
        {/* Month labels */}
        <div className="mb-1 h-3">
          {monthLabels.map((m) => (
            <span
              key={`${m.label}-${m.col}`}
              className="text-[8px] text-muted font-mono absolute"
              style={{ left: LABEL_W + GAP + m.col * (cell + GAP) }}
            >
              {m.label}
            </span>
          ))}
        </div>

        <div className="flex mt-2" style={{ gap: GAP }}>
          {/* Day labels */}
          <div className="flex flex-col shrink-0" style={{ gap: GAP, width: LABEL_W }}>
            {DAY_LABELS.map((label, i) => (
              <div
                key={i}
                className="text-[8px] text-muted font-mono flex items-center"
                style={{ height: cell }}
              >
                {label}
              </div>
            ))}
          </div>

          {/* Grid */}
          {grid.map((week, wi) => (
            <div key={wi} className="flex flex-col shrink-0" style={{ gap: GAP }}>
              {week.map((day) => {
                const cost = costMap.get(day) || 0;
                const level = costToLevel(cost, maxCost);
                const isToday = day === new Date().toISOString().slice(0, 10);
                return (
                  <div
                    key={day}
                    style={{
                      width: cell,
                      height: cell,
                      backgroundColor: LEVEL_COLORS[level],
                      outline: isToday ? "1px solid #8A8378" : "none",
                      cursor: cost > 0 ? "pointer" : "default",
                    }}
                    onMouseEnter={(e) => {
                      const rect = e.currentTarget.getBoundingClientRect();
                      setTooltip({ day, cost, x: rect.left, y: rect.top });
                    }}
                    onMouseLeave={() => setTooltip(null)}
                  />
                );
              })}
            </div>
          ))}
        </div>

        {/* Legend */}
        <div className="flex items-center gap-1 mt-3 justify-end">
          <span className="text-[8px] text-muted font-mono mr-1">Less</span>
          {LEVEL_COLORS.map((color, i) => (
            <div
              key={i}
              className="shrink-0"
              style={{ width: cell, height: cell, backgroundColor: color }}
            />
          ))}
          <span className="text-[8px] text-muted font-mono ml-1">More</span>
        </div>
      </div>

      {/* Tooltip */}
      {tooltip && tooltip.cost > 0 && <GridTooltip {...tooltip} cell={cell} />}
    </div>
  );
}

const TOOLTIP_W = 120; // generous upper bound for "$1234.56  2026-07-25"

function GridTooltip({
  day,
  cost,
  x,
  y,
  cell,
}: {
  day: string;
  cost: number;
  x: number;
  y: number;
  cell: number;
}) {
  // Flip to the left of the cell when the default position would run off the
  // right edge of the window.
  const right = x + cell + 4;
  const left = right + TOOLTIP_W > window.innerWidth ? Math.max(4, x - TOOLTIP_W - 4) : right;

  return (
    <div
      className="fixed z-50 bg-elevated px-2 py-1 pointer-events-none whitespace-nowrap"
      style={{ left, top: y - 4 }}
    >
      <span className="text-[10px] text-primary font-mono">${cost.toFixed(2)}</span>
      <span className="text-[9px] text-muted font-mono ml-1.5">{day}</span>
    </div>
  );
}
