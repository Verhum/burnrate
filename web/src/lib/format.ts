export function formatDuration(ms: number): string {
  const sec = Math.floor(ms / 1000);
  const m = Math.floor(sec / 60);
  const h = Math.floor(m / 60);
  const remM = m % 60;
  const remS = sec % 60;
  if (h > 0) return `${h}h ${remM}m`;
  if (m > 0) return `${remM}m ${remS}s`;
  return `${remS}s`;
}

export function formatRelativeTime(isoStr: string): string {
  if (!isoStr) return "";
  const d = new Date(isoStr);
  if (isNaN(d.getTime())) return "";
  const now = Date.now();
  const diffMs = now - d.getTime();
  if (diffMs >= 0 && diffMs < 7200000) {
    const mins = Math.floor(diffMs / 60000);
    if (mins < 1) return "just now";
    if (mins < 60) return mins === 1 ? "1 minute ago" : `${mins} minutes ago`;
    const h = Math.floor(mins / 60);
    const m = mins % 60;
    return `${h}h ${m}m ago`;
  }
  const yyyy = d.getFullYear();
  const mo = String(d.getMonth() + 1).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  return `${yyyy}-${mo}-${dd} ${hh}:${mm}`;
}

/**
 * Wall-clock label for a run's start, e.g. "2:14pm" for today and
 * "Jul 24 2:14pm" for an earlier day (with the year once it isn't this one).
 *
 * Elapsed time answers "how long has this been going"; a start timestamp
 * answers "when did it start", which is the one that lines a run up against a
 * session window, a usage chart, or a `run-<id>.jsonl` timestamp.
 */
export function formatStartTime(isoStr: string | null | undefined): string {
  if (!isoStr) return "-";
  const d = new Date(isoStr);
  if (isNaN(d.getTime())) return "-";
  const now = new Date();
  const time = formatClock(d);
  if (
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate()
  ) {
    return time;
  }
  const month = d.toLocaleDateString("en-US", { month: "short" });
  const day = `${month} ${d.getDate()}`;
  if (d.getFullYear() !== now.getFullYear()) {
    return `${day} ${d.getFullYear()} ${time}`;
  }
  return `${day} ${time}`;
}

/**
 * Full local timestamp for a `title` tooltip — the unabbreviated form of
 * `formatStartTime`, seconds included, since that is the resolution the run
 * log is written at.
 */
export function formatTimestamp(isoStr: string | null | undefined): string {
  if (!isoStr) return "";
  const d = new Date(isoStr);
  if (isNaN(d.getTime())) return "";
  const yyyy = d.getFullYear();
  const mo = String(d.getMonth() + 1).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  const ss = String(d.getSeconds()).padStart(2, "0");
  return `${yyyy}-${mo}-${dd} ${hh}:${mm}:${ss}`;
}

/** "2:14pm" — the same 12-hour shape `formatResetDay` uses, minutes always shown. */
function formatClock(d: Date): string {
  const hour12 = d.getHours() % 12 || 12;
  const suffix = d.getHours() >= 12 ? "pm" : "am";
  return `${hour12}:${String(d.getMinutes()).padStart(2, "0")}${suffix}`;
}

export function isActiveRun(status: string): boolean {
  return status === "starting" || status === "running" || status === "resuming";
}

export function formatCountdown(resetsAt: Date | null, now: number = Date.now()): string {
  if (!resetsAt) return "--";
  const diff = resetsAt.getTime() - now;
  if (diff <= 0) return "now";
  const h = Math.floor(diff / 3600000);
  const m = Math.floor((diff % 3600000) / 60000);
  const s = Math.floor((diff % 60000) / 1000);
  return (h > 0 ? `${h}h ` : "") + `${m}m ${s}s`;
}

/**
 * Wall-clock label for a reset instant, e.g. "Wed 4am" / "Wed 4:30am".
 * Minutes are dropped when they're zero — the 7d window almost always
 * resets on the hour and "Wed 4am" reads faster than "Wed 04:00".
 */
export function formatResetDay(iso: string | null | undefined): string | null {
  if (!iso) return null;
  const d = new Date(iso);
  if (isNaN(d.getTime())) return null;
  const day = d.toLocaleDateString("en-US", { weekday: "short" });
  const mins = d.getMinutes();
  const hour12 = d.getHours() % 12 || 12;
  const suffix = d.getHours() >= 12 ? "pm" : "am";
  const time = mins === 0 ? `${hour12}${suffix}` : `${hour12}:${String(mins).padStart(2, "0")}${suffix}`;
  return `${day} ${time}`;
}

/**
 * Coarse countdown for multi-day spans (the 7d window). Unlike
 * `formatCountdown` this never ticks seconds — days out, they're noise.
 */
export function formatLongCountdown(target: Date | null, now: number = Date.now()): string {
  if (!target) return "--";
  const diff = target.getTime() - now;
  if (diff <= 0) return "now";
  const d = Math.floor(diff / 86400000);
  const h = Math.floor((diff % 86400000) / 3600000);
  const m = Math.floor((diff % 3600000) / 60000);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}
