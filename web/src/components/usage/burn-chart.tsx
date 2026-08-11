"use client";

import { useCallback, useEffect, useState } from "react";
import { client } from "@/lib/api/client";
import type { UsageSnapshot } from "@/lib/api/types";
import { ChartCanvas, type DrawFn } from "./chart-canvas";

const HEIGHT = 160;

// Below this the y-axis gutter costs more than the labels are worth, so the
// plot runs edge to edge instead.
const AXIS_MIN_WIDTH = 140;

export function BurnChart() {
  const [history, setHistory] = useState<UsageSnapshot[]>([]);
  const [hours, setHours] = useState(24);

  useEffect(() => {
    client
      .getUsageHistory(hours)
      .then((h) => setHistory(h ?? []))
      .catch(() => {});
  }, [hours]);

  const draw = useCallback<DrawFn>(
    (ctx, W, H) => {
      if (history.length < 2) return;

      const axes = W >= AXIS_MIN_WIDTH;
      const PAD_L = axes ? 34 : 2;
      const PAD_R = axes ? 10 : 2;
      const PAD_T = 8;
      const PAD_B = axes ? 22 : 4;
      const plotW = W - PAD_L - PAD_R;
      const plotH = H - PAD_T - PAD_B;
      if (plotW <= 0 || plotH <= 0) return;

      const times = history.map((s) => new Date(s.captured_at!).getTime());
      const vals = history.map((s) => s.five_hour_util);
      const minT = times[0];
      const maxT = times[times.length - 1];
      const span = maxT - minT || 1;
      const maxV = Math.max(100, ...vals);

      const toX = (t: number) => PAD_L + ((t - minT) / span) * plotW;
      const toY = (v: number) => PAD_T + plotH - (v / maxV) * plotH;

      // Grid lines
      ctx.strokeStyle = "#262523";
      ctx.lineWidth = 1;
      for (const pct of [25, 50, 75, 100]) {
        const y = toY(pct);
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
        for (const pct of [0, 25, 50, 75, 100]) {
          ctx.fillText(`${pct}`, PAD_L - 6, toY(pct));
        }

        // X-axis labels. Each needs ~70px of room, and the outermost two are
        // anchored to the plot edges so they cannot spill past them.
        ctx.textBaseline = "top";
        const steps = Math.max(1, Math.min(5, Math.floor(plotW / 70)));
        for (let i = 0; i <= steps; i++) {
          const t = minT + (span * i) / steps;
          const d = new Date(t);
          const label = `${d.getHours().toString().padStart(2, "0")}:${d
            .getMinutes()
            .toString()
            .padStart(2, "0")}`;
          ctx.textAlign = i === 0 ? "left" : i === steps ? "right" : "center";
          ctx.fillText(label, toX(t), H - PAD_B + 6);
        }
      }

      // Area fill
      ctx.beginPath();
      ctx.moveTo(toX(times[0]), toY(0));
      for (let i = 0; i < times.length; i++) {
        ctx.lineTo(toX(times[i]), toY(vals[i]));
      }
      ctx.lineTo(toX(times[times.length - 1]), toY(0));
      ctx.closePath();
      ctx.fillStyle = "rgba(92, 83, 71, 0.15)";
      ctx.fill();

      // Line
      ctx.beginPath();
      ctx.moveTo(toX(times[0]), toY(vals[0]));
      for (let i = 1; i < times.length; i++) {
        ctx.lineTo(toX(times[i]), toY(vals[i]));
      }
      ctx.strokeStyle = "#5C5347";
      ctx.lineWidth = 1.5;
      ctx.stroke();

      // Current value dot, pulled inside the plot so it is not half-clipped.
      const lastX = Math.min(toX(times[times.length - 1]), W - PAD_R - 3);
      const lastY = toY(vals[vals.length - 1]);
      ctx.beginPath();
      ctx.arc(lastX, lastY, 3, 0, Math.PI * 2);
      ctx.fillStyle = "#8A8378";
      ctx.fill();
    },
    [history],
  );

  const RANGES = [
    { label: "6H", value: 6 },
    { label: "24H", value: 24 },
    { label: "3D", value: 72 },
    { label: "7D", value: 168 },
  ];

  return (
    <div className="bg-surface px-4 py-3 min-w-0 overflow-hidden">
      <div className="flex items-center justify-between gap-2 mb-2">
        <span className="text-[9px] font-bold tracking-widest text-dim uppercase truncate">
          BURN RATE
        </span>
        <div className="flex gap-1 shrink-0">
          {RANGES.map((r) => (
            <button
              key={r.value}
              onClick={() => setHours(r.value)}
              className={`px-1.5 py-0.5 text-[8px] font-bold tracking-wider font-mono cursor-pointer border-none transition-colors ${
                hours === r.value
                  ? "bg-elevated text-dim"
                  : "bg-transparent text-muted hover:text-dim"
              }`}
            >
              {r.label}
            </button>
          ))}
        </div>
      </div>
      {history.length < 2 ? (
        <div
          className="flex items-center justify-center text-[11px] text-muted"
          style={{ height: HEIGHT }}
        >
          Collecting data...
        </div>
      ) : (
        <ChartCanvas height={HEIGHT} draw={draw} />
      )}
    </div>
  );
}
