"use client";

import { useEffect, useState, useCallback } from "react";
import { client } from "@/lib/api/client";
import type { UsageSnapshot } from "@/lib/api/types";

const WIDTH = 300;
const HEIGHT = 40;
const PADDING_X = 2;
const PADDING_Y = 2;

const LINE_COLOR = "#5C5347";   // warm
const GRID_COLOR = "#4A4640";   // muted

export function UtilSparkline() {
  const [points, setPoints] = useState<
    { time: number; util: number }[]
  >([]);

  const fetchHistory = useCallback(async () => {
    try {
      const snapshots: UsageSnapshot[] = await client.getUsageHistory(5);
      if (!Array.isArray(snapshots) || snapshots.length === 0) return;
      const mapped = snapshots.map((s) => ({
        time: new Date(s.captured_at ?? "").getTime(),
        util: s.five_hour_util ?? 0,
      }));
      // Sort by time ascending
      mapped.sort((a, b) => a.time - b.time);
      setPoints(mapped);
    } catch {
      // endpoint may not exist yet — silently ignore
    }
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async fetch: setState runs after an await, not synchronously
    fetchHistory();
    const id = setInterval(fetchHistory, 5000);
    return () => clearInterval(id);
  }, [fetchHistory]);

  if (points.length < 2) return null;

  const minTime = points[0].time;
  const maxTime = points[points.length - 1].time;
  const timeRange = maxTime - minTime || 1;

  const toX = (t: number) =>
    PADDING_X + ((t - minTime) / timeRange) * (WIDTH - 2 * PADDING_X);
  const toY = (u: number) =>
    PADDING_Y + ((100 - u) / 100) * (HEIGHT - 2 * PADDING_Y);

  const pathD = points
    .map((p, i) => {
      const x = toX(p.time);
      const y = toY(Math.min(Math.max(p.util, 0), 100));
      return `${i === 0 ? "M" : "L"} ${x.toFixed(1)} ${y.toFixed(1)}`;
    })
    .join(" ");

  // Grid lines at 25%, 50%, 75%
  const gridLines = [25, 50, 75].map((pct) => {
    const y = toY(pct);
    return (
      <line
        key={pct}
        x1={PADDING_X}
        y1={y}
        x2={WIDTH - PADDING_X}
        y2={y}
        stroke={GRID_COLOR}
        strokeWidth={0.5}
      />
    );
  });

  return (
    <svg
      width={WIDTH}
      height={HEIGHT}
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      className="block"
      style={{ maxWidth: "100%" }}
    >
      {gridLines}
      <path d={pathD} fill="none" stroke={LINE_COLOR} strokeWidth={1.5} />
    </svg>
  );
}
