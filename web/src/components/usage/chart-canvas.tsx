"use client";

import { useEffect, useRef } from "react";
import { useElementWidth } from "@/hooks/use-element-width";
import { setupCanvas } from "@/lib/canvas";

export type DrawFn = (
  ctx: CanvasRenderingContext2D,
  width: number,
  height: number,
) => void;

/**
 * A canvas that always matches the width of the column it sits in.
 *
 * The canvas is taken out of flow so its explicit pixel width can never feed
 * back into the layout it was measured from; the wrapper alone decides how
 * wide the chart is, and `useElementWidth` redraws whenever that changes.
 * `draw` must be memoized — it is an effect dependency.
 */
export function ChartCanvas({ height, draw }: { height: number; draw: DrawFn }) {
  const [containerRef, width] = useElementWidth<HTMLDivElement>();
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = setupCanvas(canvas, width, height);
    if (!ctx) return;
    draw(ctx, width, height);
  }, [draw, width, height]);

  return (
    <div
      ref={containerRef}
      className="relative w-full min-w-0 overflow-hidden"
      style={{ height }}
    >
      <canvas ref={canvasRef} className="absolute inset-0 block" />
    </div>
  );
}
