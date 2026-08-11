/** Burn-state colors. Kept in sync with `@theme` in app/globals.css. */
export const BURN_COLD = "#8A8378"; // --color-dim
export const BURN_HOT = "#EF4444"; // --color-danger

export function lerpColor(a: string, b: string, t: number): string {
  const parse = (hex: string) => [
    parseInt(hex.slice(1, 3), 16),
    parseInt(hex.slice(3, 5), 16),
    parseInt(hex.slice(5, 7), 16),
  ];
  const [r1, g1, b1] = parse(a);
  const [r2, g2, b2] = parse(b);
  const clamped = Math.min(Math.max(t, 0), 1);
  const channel = (from: number, to: number) =>
    Math.round(from + (to - from) * clamped)
      .toString(16)
      .padStart(2, "0");
  return `#${channel(r1, r2)}${channel(g1, g2)}${channel(b1, b2)}`;
}

/**
 * Gray at an untouched window, ramping to red as the session burns down.
 * `lerpColor` clamps, so anything at or past 100% is fully red.
 */
export function burnColor(pct: number): string {
  return lerpColor(BURN_COLD, BURN_HOT, pct / 100);
}
