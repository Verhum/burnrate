"use client";

import { useCallback, useRef, useState } from "react";

/**
 * Tracks an element's rendered width via ResizeObserver.
 *
 * Canvas charts have to be sized in explicit pixels, so they cannot reflow on
 * their own. Measuring once on mount leaves the canvas stuck at whatever width
 * the window happened to have back then — shrink the window and the canvas
 * stays wide, overflowing its panel and giving the whole page a horizontal
 * scrollbar. Observing the container keeps the drawn size and the laid-out
 * size in step in both directions.
 *
 * Returns a callback ref so it also picks up elements that mount later (a
 * chart that renders a placeholder until its data arrives, say).
 */
export function useElementWidth<T extends HTMLElement>(): [
  (node: T | null) => void,
  number,
] {
  const [width, setWidth] = useState(0);
  const observer = useRef<ResizeObserver | null>(null);

  const ref = useCallback((node: T | null) => {
    observer.current?.disconnect();
    observer.current = null;
    if (!node) return;

    // Round down: a fractional container width rounded up is enough to push
    // the canvas past its parent and reintroduce the scrollbar.
    const measure = () => setWidth(Math.floor(node.getBoundingClientRect().width));
    measure();

    const ro = new ResizeObserver(measure);
    ro.observe(node);
    observer.current = ro;
  }, []);

  return [ref, width];
}
