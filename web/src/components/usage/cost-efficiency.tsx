"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { client } from "@/lib/api/client";
import type { CostEfficiency, CostEfficiencyPoint } from "@/lib/api/types";

/**
 * Categorical series palette, dark steps, in fixed slot order. Validated as a
 * set against the #1C1B19 chart surface (lightness band, chroma floor, adjacent
 * CVD separation >= 8, normal-vision separation >= 15, contrast >= 3:1).
 *
 * The order is the colorblind-safety mechanism, not decoration — do not reorder
 * or extend it. A model's slot comes from its index in the server's `models`
 * list, which is computed over all history, so narrowing the range never
 * repaints the models that survive the filter.
 */
const SERIES_COLORS = ["#3987e5", "#d95926", "#199e70", "#c98500", "#d55181"];

/** Models past the palette fold into one gray "other" series rather than being
 *  given a generated hue. */
const OTHER_COLOR = "#8A8378";
const OTHER_LABEL = "other";
const MAX_SERIES = SERIES_COLORS.length;

const RANGES = [
  { label: "7D", value: 7 },
  { label: "30D", value: 30 },
  { label: "90D", value: 90 },
];

type Metric = "task" | "line";

const METRICS: Record<
  Metric,
  { key: keyof CostEfficiencyPoint; title: string; unit: string }
> = {
  task: { key: "cost_per_task", title: "COST PER TASK", unit: "$/task" },
  line: { key: "cost_per_line", title: "COST PER LINE OF CODE", unit: "$/line" },
};

/** A point at exactly 0 has no denominator (no tasks, or no measurable lines),
 *  so it is a gap in the line rather than a value on the axis. */
type SeriesPoint = number | null;

interface Series {
  model: string;
  label: string;
  color: string;
  values: SeriesPoint[]; // one slot per day in the x-axis range, null = gap
  total: CostEfficiencyPoint | null;
}

/**
 * shortModel drops the redundant `claude-` prefix and the release-date suffix.
 * Two snapshots of the same version are still distinct series server-side; the
 * label just stops being wide enough to shove the chart's plot area aside.
 */
function shortModel(model: string): string {
  return model.replace(/^claude-/, "").replace(/-\d{8}$/, "");
}

/** dayRange lists the UTC days the chart spans, oldest first, so the x-axis is
 *  time-linear even on days nothing ran. */
function dayRange(days: number): string[] {
  const out: string[] = [];
  const today = new Date();
  for (let i = days - 1; i >= 0; i--) {
    const d = new Date(
      Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate())
    );
    d.setUTCDate(d.getUTCDate() - i);
    out.push(d.toISOString().slice(0, 10));
  }
  return out;
}

/**
 * formatMoney keeps decimal notation all the way down. Cost per line lands
 * around a tenth of a cent, and `$1.5e-3` is not a figure anyone reads at a
 * glance — four decimal places is.
 */
function formatMoney(v: number): string {
  if (v === 0) return "--";
  if (v >= 100) return `$${Math.round(v)}`;
  if (v >= 10) return `$${v.toFixed(1)}`;
  if (v >= 1) return `$${v.toFixed(2)}`;
  if (v >= 0.1) return `$${v.toFixed(3)}`;
  if (v >= 0.0001) return `$${v.toFixed(4)}`;
  return `$${v.toFixed(6)}`;
}

/**
 * formatAxis is formatMoney for gridlines: a real zero at the origin (an axis
 * origin is a value, not missing data) and no padding zeros, since a tick only
 * has to be read, not lined up against the tick below it.
 */
function formatAxis(v: number): string {
  if (v === 0) return "$0";
  return formatMoney(v).replace(/(\.\d*?)0+$/, "$1").replace(/\.$/, "");
}

/**
 * niceScale picks an axis maximum and tick step from the 1/2/2.5/5 family, so
 * gridlines land on round money instead of on max/3.
 */
function niceScale(max: number): { max: number; step: number } {
  if (max <= 0) return { max: 0, step: 0 };
  const exp = Math.floor(Math.log10(max));
  for (const mult of [1, 2, 2.5, 5, 10]) {
    const step = mult * Math.pow(10, exp - 1);
    const ticks = Math.ceil(max / step);
    if (ticks >= 2 && ticks <= 5) return { max: ticks * step, step };
  }
  const step = Math.pow(10, exp);
  return { max: Math.ceil(max / step) * step, step };
}

function formatDay(day: string): string {
  return day.slice(5); // MM-DD
}

/**
 * buildSeries turns the flat (day, model) points into one dense value array per
 * model, folding everything past the palette into a single "other" series whose
 * points are re-derived as cost / denominator across the folded models — a mean
 * of per-model ratios would weight a one-task model like a fifty-task one.
 */
function buildSeries(
  data: CostEfficiency,
  days: string[],
  metric: Metric
): Series[] {
  const named = data.models.slice(0, MAX_SERIES);
  const folded = new Set(data.models.slice(MAX_SERIES));
  const dayIndex = new Map(days.map((d, i) => [d, i]));

  const ratio = (cost: number, denom: number): SeriesPoint =>
    denom > 0 ? cost / denom : null;
  const denomOf = (p: CostEfficiencyPoint) =>
    metric === "task" ? p.tasks : p.lines_changed;

  const out: Series[] = named.map((model, i) => ({
    model,
    label: shortModel(model),
    color: SERIES_COLORS[i],
    values: days.map(() => null),
    total: data.totals.find((t) => t.model === model) ?? null,
  }));
  const byModel = new Map(out.map((s) => [s.model, s]));

  // "other" accumulates cost and denominator per day before becoming a ratio.
  const otherCost = days.map(() => 0);
  const otherDenom = days.map(() => 0);

  for (const p of data.points) {
    const i = dayIndex.get(p.date);
    if (i === undefined) continue;
    if (folded.has(p.model)) {
      otherCost[i] += p.cost_usd;
      otherDenom[i] += denomOf(p);
      continue;
    }
    const s = byModel.get(p.model);
    if (s) s.values[i] = ratio(p.cost_usd, denomOf(p));
  }

  if (folded.size > 0) {
    const totals = data.totals.filter((t) => folded.has(t.model));
    const cost = totals.reduce((a, t) => a + t.cost_usd, 0);
    const tasks = totals.reduce((a, t) => a + t.tasks, 0);
    const linesChanged = totals.reduce((a, t) => a + t.lines_changed, 0);
    out.push({
      model: OTHER_LABEL,
      label: `${OTHER_LABEL} (${folded.size})`,
      color: OTHER_COLOR,
      values: days.map((_, i) => ratio(otherCost[i], otherDenom[i])),
      total: {
        date: "",
        model: OTHER_LABEL,
        runs: totals.reduce((a, t) => a + t.runs, 0),
        tasks,
        cost_usd: cost,
        lines_added: totals.reduce((a, t) => a + t.lines_added, 0),
        lines_removed: totals.reduce((a, t) => a + t.lines_removed, 0),
        lines_changed: linesChanged,
        cost_per_task: tasks > 0 ? cost / tasks : 0,
        cost_per_line: linesChanged > 0 ? cost / linesChanged : 0,
      },
    });
  }

  // A series with nothing to draw in this range is not a legend entry.
  return out.filter((s) => s.values.some((v) => v !== null && v > 0));
}

export function CostEfficiencyPanel() {
  const [days, setDays] = useState(30);
  const [data, setData] = useState<CostEfficiency | null>(null);
  const [showTable, setShowTable] = useState(false);

  useEffect(() => {
    client
      .getCostEfficiency(days)
      .then(setData)
      .catch(() => {});
  }, [days]);

  const dayList = useMemo(() => dayRange(days), [days]);
  const taskSeries = useMemo(
    () => (data ? buildSeries(data, dayList, "task") : []),
    [data, dayList]
  );
  const lineSeries = useMemo(
    () => (data ? buildSeries(data, dayList, "line") : []),
    [data, dayList]
  );

  // The two charts share a series set; the legend is drawn from whichever knows
  // about more models so a model that only has line data still gets a swatch.
  const legend = taskSeries.length >= lineSeries.length ? taskSeries : lineSeries;
  const hasData = taskSeries.length > 0 || lineSeries.length > 0;

  return (
    <div className="bg-surface px-4 py-3">
      <div className="flex items-center justify-between mb-2">
        <span className="text-[9px] font-bold tracking-widest text-dim uppercase">
          COST EFFICIENCY
        </span>
        <div className="flex items-center gap-3">
          <button
            onClick={() => setShowTable((v) => !v)}
            className={`px-1.5 py-0.5 text-[8px] font-bold tracking-wider font-mono cursor-pointer border-none transition-colors ${
              showTable
                ? "bg-elevated text-dim"
                : "bg-transparent text-muted hover:text-dim"
            }`}
          >
            TABLE
          </button>
          <div className="flex gap-1">
            {RANGES.map((r) => (
              <button
                key={r.value}
                onClick={() => setDays(r.value)}
                className={`px-1.5 py-0.5 text-[8px] font-bold tracking-wider font-mono cursor-pointer border-none transition-colors ${
                  days === r.value
                    ? "bg-elevated text-dim"
                    : "bg-transparent text-muted hover:text-dim"
                }`}
              >
                {r.label}
              </button>
            ))}
          </div>
        </div>
      </div>

      {!hasData ? (
        <div className="h-[120px] flex items-center justify-center text-[11px] text-muted">
          {data ? "No completed runs in this range" : "Loading..."}
        </div>
      ) : (
        <>
          <Legend series={legend} />
          {showTable ? (
            <SeriesTable series={legend} />
          ) : (
            <div className="flex flex-col gap-3">
              <LineChart
                title={METRICS.task.title}
                unit={METRICS.task.unit}
                days={dayList}
                series={taskSeries}
              />
              <LineChart
                title={METRICS.line.title}
                unit={METRICS.line.unit}
                days={dayList}
                series={lineSeries}
              />
            </div>
          )}
        </>
      )}
    </div>
  );
}

function Legend({ series }: { series: Series[] }) {
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1 mb-2">
      {series.map((s) => (
        <div key={s.model} className="flex items-center gap-1.5">
          <span
            className="inline-block w-[8px] h-[8px] shrink-0"
            style={{ backgroundColor: s.color }}
            aria-hidden
          />
          <span className="text-[9px] font-mono text-dim">{s.label}</span>
          {s.total && (
            <span className="text-[9px] font-mono text-muted tabular-nums">
              {formatMoney(s.total.cost_per_task)}/task ·{" "}
              {formatMoney(s.total.cost_per_line)}/line
            </span>
          )}
        </div>
      ))}
    </div>
  );
}

/** SeriesTable is the non-visual reading of the same numbers, so identity and
 *  magnitude are never carried by color alone. */
function SeriesTable({ series }: { series: Series[] }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-[10px] font-mono">
        <thead>
          <tr className="text-muted text-left">
            <th className="font-normal py-1 pr-3">MODEL</th>
            <th className="font-normal py-1 pr-3 text-right">RUNS</th>
            <th className="font-normal py-1 pr-3 text-right">TASKS</th>
            <th className="font-normal py-1 pr-3 text-right">SPEND</th>
            <th className="font-normal py-1 pr-3 text-right">LINES</th>
            <th className="font-normal py-1 pr-3 text-right">$/TASK</th>
            <th className="font-normal py-1 text-right">$/LINE</th>
          </tr>
        </thead>
        <tbody className="text-dim tabular-nums">
          {series.map((s) => (
            <tr key={s.model} className="border-t border-elevated">
              <td className="py-1 pr-3">
                <span className="flex items-center gap-1.5">
                  <span
                    className="inline-block w-[8px] h-[8px] shrink-0"
                    style={{ backgroundColor: s.color }}
                    aria-hidden
                  />
                  {s.label}
                </span>
              </td>
              <td className="py-1 pr-3 text-right">{s.total?.runs ?? 0}</td>
              <td className="py-1 pr-3 text-right">{s.total?.tasks ?? 0}</td>
              <td className="py-1 pr-3 text-right">
                {formatMoney(s.total?.cost_usd ?? 0)}
              </td>
              <td className="py-1 pr-3 text-right">
                {(s.total?.lines_changed ?? 0).toLocaleString()}
              </td>
              <td className="py-1 pr-3 text-right">
                {formatMoney(s.total?.cost_per_task ?? 0)}
              </td>
              <td className="py-1 text-right">
                {formatMoney(s.total?.cost_per_line ?? 0)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="text-[9px] font-mono text-muted mt-2">
        Totals sum each day&apos;s bucket, so a task worked on two days counts
        twice.
      </p>
    </div>
  );
}

const PAD_L = 48;
const PAD_R = 12;
const PAD_T = 8;
const PAD_B = 22;
const HEIGHT = 130;

/** Advance width of the 9px monospace the chart labels in. Used to reserve room
 *  for direct labels before drawing, so they never overhang the canvas. */
const CHAR_W = 5.5;
/** Least vertical gap between two direct labels before they are pushed apart. */
const LABEL_PITCH = 11;
/** Past this many series the end labels cannot be laid out without collisions,
 *  so identity falls to the legend and the table alone. */
const MAX_DIRECT_LABELS = 4;

const GRID = "#262523";
const AXIS_TEXT = "#6B6560";
const SURFACE = "#1C1B19";

interface EndLabel {
  label: string;
  color: string;
  x: number;
  y: number; // the data point
  labelY: number; // where the text sits, after collision resolution
}

/**
 * layoutEndLabels places one label per series at its latest value, then pushes
 * overlapping rows apart while leaving the markers on the data. Series that
 * converge — which is exactly what a cost comparison converging looks like —
 * would otherwise print their labels on top of each other.
 */
function layoutEndLabels(
  series: Series[],
  xAt: (i: number) => number,
  yAt: (v: number) => number,
  plotH: number
): EndLabel[] {
  const out: EndLabel[] = [];
  for (const s of series) {
    let last = -1;
    for (let i = s.values.length - 1; i >= 0; i--) {
      if (s.values[i] !== null) {
        last = i;
        break;
      }
    }
    if (last < 0) continue;
    const y = yAt(s.values[last] as number);
    out.push({ label: s.label, color: s.color, x: xAt(last), y, labelY: y });
  }

  // Top-down sweep: each label keeps at least LABEL_PITCH below the one above it.
  out.sort((a, b) => a.y - b.y);
  const top = PAD_T + 4;
  const bottom = PAD_T + plotH - 2;
  let cursor = top;
  for (const l of out) {
    l.labelY = Math.max(l.labelY, cursor);
    cursor = l.labelY + LABEL_PITCH;
  }
  // If the sweep ran past the plot, walk back up from the bottom.
  if (out.length > 0 && out[out.length - 1].labelY > bottom) {
    cursor = bottom;
    for (let i = out.length - 1; i >= 0; i--) {
      out[i].labelY = Math.min(out[i].labelY, cursor);
      cursor = out[i].labelY - LABEL_PITCH;
    }
  }
  return out;
}

interface Hover {
  dayIndex: number;
  x: number;
  y: number;
}

function LineChart({
  title,
  unit,
  days,
  series,
}: {
  title: string;
  unit: string;
  days: string[];
  series: Series[];
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  const [hover, setHover] = useState<Hover | null>(null);

  // Track the container width so the chart reflows with the panel instead of
  // being sized once at mount.
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const measure = () => setWidth(container.getBoundingClientRect().width);
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(container);
    return () => ro.disconnect();
  }, []);

  const { peak, scale } = useMemo(() => {
    let m = 0;
    for (const s of series) {
      for (const v of s.values) if (v !== null && v > m) m = v;
    }
    return { peak: m, scale: niceScale(m) };
  }, [series]);
  const maxVal = scale.max;

  const labelled = series.length <= MAX_DIRECT_LABELS;
  const padR = useMemo(() => {
    if (!labelled) return PAD_R;
    const widest = Math.max(0, ...series.map((s) => s.label.length));
    return PAD_R + 8 + widest * CHAR_W;
  }, [series, labelled]);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || width <= 0 || maxVal <= 0) return;

    const dpr = window.devicePixelRatio || 1;
    canvas.width = width * dpr;
    canvas.height = HEIGHT * dpr;
    canvas.style.width = `${width}px`;
    canvas.style.height = `${HEIGHT}px`;

    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

    const plotW = width - PAD_L - padR;
    const plotH = HEIGHT - PAD_T - PAD_B;
    const xAt = (i: number) =>
      days.length <= 1 ? PAD_L + plotW / 2 : PAD_L + (i / (days.length - 1)) * plotW;
    const yAt = (v: number) => PAD_T + plotH - (v / maxVal) * plotH;

    ctx.clearRect(0, 0, width, HEIGHT);

    // Recessive grid + y-axis labels, on round money rather than on max/3.
    ctx.strokeStyle = GRID;
    ctx.lineWidth = 1;
    ctx.fillStyle = AXIS_TEXT;
    ctx.font = "9px monospace";
    ctx.textAlign = "right";
    ctx.textBaseline = "middle";
    for (let v = 0; v <= maxVal + scale.step / 2; v += scale.step) {
      const y = yAt(v);
      ctx.beginPath();
      ctx.moveTo(PAD_L, y);
      ctx.lineTo(PAD_L + plotW, y);
      ctx.stroke();
      ctx.fillText(formatAxis(v), PAD_L - 6, y);
    }

    // X-axis labels, thinned to whatever fits.
    ctx.textAlign = "center";
    ctx.textBaseline = "top";
    const labelEvery = Math.max(1, Math.ceil(days.length / Math.max(1, Math.floor(plotW / 44))));
    for (let i = 0; i < days.length; i += labelEvery) {
      ctx.fillText(formatDay(days[i]), xAt(i), HEIGHT - PAD_B + 5);
    }

    // Crosshair sits under the marks so it never obscures them.
    if (hover) {
      const x = xAt(hover.dayIndex);
      ctx.strokeStyle = "#343230";
      ctx.beginPath();
      ctx.moveTo(x, PAD_T);
      ctx.lineTo(x, PAD_T + plotH);
      ctx.stroke();
    }

    // Direct labels are laid out before anything is drawn: two series that end
    // close together would otherwise print on top of each other, so their label
    // rows are pushed apart while the markers stay on the data.
    const endLabels = labelled ? layoutEndLabels(series, xAt, yAt, plotH) : [];

    for (const s of series) {
      ctx.strokeStyle = s.color;
      ctx.lineWidth = 2;
      ctx.lineJoin = "round";
      ctx.lineCap = "round";

      // A null is a gap, not a zero: break the path rather than dropping the
      // line to the axis on a day the model did not run.
      let drawing = false;
      ctx.beginPath();
      for (let i = 0; i < s.values.length; i++) {
        const v = s.values[i];
        if (v === null) {
          drawing = false;
          continue;
        }
        const x = xAt(i);
        const y = yAt(v);
        if (drawing) ctx.lineTo(x, y);
        else ctx.moveTo(x, y);
        drawing = true;
      }
      ctx.stroke();

      // An isolated day has no segment to draw, so mark it explicitly.
      for (let i = 0; i < s.values.length; i++) {
        const v = s.values[i];
        if (v === null) continue;
        const prev = i > 0 ? s.values[i - 1] : null;
        const next = i < s.values.length - 1 ? s.values[i + 1] : null;
        if (prev !== null || next !== null) continue;
        ctx.beginPath();
        ctx.arc(xAt(i), yAt(v), 2.5, 0, Math.PI * 2);
        ctx.fillStyle = s.color;
        ctx.fill();
      }

    }

    // Direct labels last, so a marker is never painted over by a later line.
    ctx.font = "9px monospace";
    ctx.textBaseline = "middle";
    ctx.textAlign = "left";
    for (const l of endLabels) {
      // A surface ring keeps end markers readable where two series converge.
      ctx.beginPath();
      ctx.arc(l.x, l.y, 4.5, 0, Math.PI * 2);
      ctx.fillStyle = SURFACE;
      ctx.fill();
      ctx.beginPath();
      ctx.arc(l.x, l.y, 3, 0, Math.PI * 2);
      ctx.fillStyle = l.color;
      ctx.fill();

      // A label pushed off its own point gets a leader so it stays attached.
      if (Math.abs(l.labelY - l.y) > 1) {
        ctx.strokeStyle = GRID;
        ctx.lineWidth = 1;
        ctx.beginPath();
        ctx.moveTo(l.x + 4, l.y);
        ctx.lineTo(l.x + 7, l.labelY);
        ctx.stroke();
      }
      ctx.fillStyle = AXIS_TEXT;
      ctx.fillText(l.label, l.x + 8, l.labelY);
    }
  }, [series, days, width, maxVal, scale, padR, labelled, hover]);

  const onMove = (e: React.MouseEvent<HTMLDivElement>) => {
    const container = containerRef.current;
    if (!container || width <= 0 || days.length === 0) return;
    const rect = container.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const plotW = width - PAD_L - padR;
    const t = Math.min(1, Math.max(0, (x - PAD_L) / Math.max(1, plotW)));
    setHover({
      dayIndex: Math.round(t * (days.length - 1)),
      x,
      y: e.clientY - rect.top,
    });
  };

  const hovered =
    hover !== null
      ? series
          .map((s) => ({ s, v: s.values[hover.dayIndex] }))
          .filter((e): e is { s: Series; v: number } => e.v !== null)
      : [];

  return (
    <div>
      <div className="flex items-baseline justify-between mb-1">
        <span className="text-[8px] font-bold uppercase tracking-wider text-muted font-mono">
          {title}
        </span>
        <span className="text-[8px] font-mono text-muted">{unit}</span>
      </div>
      <div
        ref={containerRef}
        className="relative w-full"
        onMouseMove={onMove}
        onMouseLeave={() => setHover(null)}
      >
        {peak <= 0 ? (
          <div
            className="flex items-center justify-center text-[11px] text-muted"
            style={{ height: HEIGHT }}
          >
            Nothing measurable in this range
          </div>
        ) : (
          <canvas ref={canvasRef} />
        )}
        {hover && hovered.length > 0 && (
          <div
            className="pointer-events-none absolute z-10 bg-elevated px-2 py-1 shadow-lg"
            style={{
              left: Math.min(Math.max(hover.x + 10, 0), Math.max(0, width - 150)),
              top: 4,
            }}
          >
            <p className="text-[9px] font-mono text-dim mb-0.5">
              {days[hover.dayIndex]}
            </p>
            {hovered.map(({ s, v }) => (
              <p
                key={s.model}
                className="text-[9px] font-mono text-muted flex items-center gap-1.5 whitespace-nowrap"
              >
                <span
                  className="inline-block w-[6px] h-[6px] shrink-0"
                  style={{ backgroundColor: s.color }}
                  aria-hidden
                />
                {s.label}
                <span className="text-dim tabular-nums">{formatMoney(v)}</span>
              </p>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
