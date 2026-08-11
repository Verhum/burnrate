/**
 * Sizes a canvas for the current device pixel ratio and hands back a context
 * whose coordinate space is CSS pixels, already cleared.
 *
 * Uses setTransform rather than scale so repeated draws onto the same canvas
 * cannot compound the DPR scale factor.
 *
 * Returns null when there is nothing worth drawing into (zero/unmeasured
 * width, or no 2D context available).
 */
export function setupCanvas(
  canvas: HTMLCanvasElement,
  cssWidth: number,
  cssHeight: number,
): CanvasRenderingContext2D | null {
  const w = Math.floor(cssWidth);
  const h = Math.floor(cssHeight);
  if (w <= 0 || h <= 0) return null;

  const dpr = window.devicePixelRatio || 1;
  canvas.width = Math.round(w * dpr);
  canvas.height = Math.round(h * dpr);
  canvas.style.width = `${w}px`;
  canvas.style.height = `${h}px`;

  const ctx = canvas.getContext("2d");
  if (!ctx) return null;

  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, w, h);
  return ctx;
}
