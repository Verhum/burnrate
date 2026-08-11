"use client";

import { useEffect, useState } from "react";

/**
 * Ticking clock for countdown rendering: epoch ms, refreshed every
 * `intervalMs`. Countdowns derive from this during render rather than
 * writing formatted strings back into state from an effect.
 */
export function useNow(intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);

  return now;
}
