"use client";

import { useCallback, useEffect, useState } from "react";
import { client } from "@/lib/api/client";
import type { Run } from "@/lib/api/types";
import { ChartCanvas, type DrawFn } from "./chart-canvas";

const HEIGHT = 120;

// Below this the y-axis gutter costs more than the labels are worth, so the
// bars run edge to edge instead.
const AXIS_MIN_WIDTH = 140;

function aggregateByDay(runs: Run[]): Map<string, number> {
  const map = new Map<string, number>();
  for (const r of runs) {
    if (!r.started_at || !r.cost_usd) continue;
    const day = r.started_at.slice(0, 10);
    map.set(day, (map.get(day) || 0) + r.cost_usd);
  }
  return map;
}

function last14Days(): string[] {
  const days: string[] = [];
  const now = new Date();
  for (let i = 13; i >= 0; i--) {
    const d = new Date(now);
    d.setDate(d.getDate() - i);
    days.push(d.toISOString().slice(0, 10));
  }
  return days;
}

export function DailySpend() {
  const [runs, setRuns] = useState<Run[]>([]);

  useEffect(() => {
    client.listRuns({ limit: 500 }).then(setRuns).catch(() => {});
  }, []);

  const draw = useCallback<DrawFn>(
    (ctx, W, H) => {
      const dailyMap = aggregateByDay(runs);
      const days = last14Days();
      const values = days.map((d) => dailyMap.get(d) || 0);
      const maxVal = Math.max(0.01, ...values);

      const axes = W >= AXIS_MIN_WIDTH;
      const PAD_L = axes ? 36 : 2;
      const PAD_R = axes ? 12 : 2;
      const PAD_T = 8;
      const PAD_B = axes ? 22 : 4;
      const plotW = W - PAD_L - PAD_R;
      const plotH = H - PAD_T - PAD_B;
      if (plotW <= 0 || plotH <= 0) return;

      // Grid
      ctx.strokeStyle = "#262523";
      ctx.lineWidth = 1;
      for (let i = 1; i <= 3; i++) {
        const y = PAD_T + plotH - (plotH * i) / 3;
        ctx.beginPath();
        ctx.moveTo(PAD_L, y);
        ctx.lineTo(W - PAD_R, y);
        ctx.stroke();
      }

      if (axes) {
        // Y-axis labels
        ctx.fillStyle = "#6B6560";
        ctx.font = "9px monospace";
        ctx.textAlign = "right";
        ctx.textBaseline = "middle";
        for (let i = 0; i <= 3; i++) {
          const val = (maxVal * i) / 3;
          const y = PAD_T + plotH - (plotH * i) / 3;
          ctx.fillText(`$${val.toFixed(val >= 1 ? 0 : 2)}`, PAD_L - 6, y);
        }
      }

      // Bars. The gap collapses before the bars do, so a narrow panel still
      // shows fourteen distinguishable columns instead of overflowing one.
      const slot = plotW / days.length;
      const barGap = slot > 8 ? 3 : slot > 4 ? 1 : 0;
      const barW = Math.max(1, (plotW - barGap * (days.length - 1)) / days.length);
      const totalBarSpace = barW * days.length + barGap * (days.length - 1);
      const offsetX = PAD_L + (plotW - totalBarSpace) / 2;

      for (let i = 0; i < days.length; i++) {
        const x = offsetX + i * (barW + barGap);
        const barH = (values[i] / maxVal) * plotH;
        const y = PAD_T + plotH - barH;

        ctx.fillStyle = values[i] > 0 ? "#5C5347" : "#262523";
        ctx.fillRect(x, y, barW, barH || 2);
      }

      if (!axes) return;

      // X-axis labels, thinned until the "MM-DD" stamps stop colliding.
      ctx.fillStyle = "#6B6560";
      ctx.font = "8px monospace";
      ctx.textAlign = "center";
      ctx.textBaseline = "top";
      const LABEL_W = 34; // "MM-DD" at 8px mono, plus breathing room
      const step = Math.max(1, Math.ceil(LABEL_W / (barW + barGap)));
      let lastRight = -Infinity;
      for (let i = 0; i < days.length; i += step) {
        const center = offsetX + i * (barW + barGap) + barW / 2;
        // Nudge a label that would leave the canvas back inside it, then drop
        // it if that nudge has walked it into the previous label.
        const x = Math.min(Math.max(center, LABEL_W / 2), W - LABEL_W / 2);
        if (x - LABEL_W / 2 < lastRight) continue;
        lastRight = x + LABEL_W / 2;
        ctx.fillText(days[i].slice(5), x, H - PAD_B + 6);
      }
    },
    [runs],
  );

  const totalSpend = runs.reduce((s, r) => s + (r.cost_usd || 0), 0);

  return (
    <div className="bg-surface px-4 py-3 min-w-0 overflow-hidden">
      <div className="flex items-center justify-between gap-2 mb-2">
        <span className="text-[9px] font-bold tracking-widest text-dim uppercase truncate">
          DAILY SPEND
        </span>
        <span className="text-[11px] font-bold text-dim tabular-nums shrink-0">
          ${totalSpend.toFixed(2)} total
        </span>
      </div>
      <ChartCanvas height={HEIGHT} draw={draw} />
    </div>
  );
}
