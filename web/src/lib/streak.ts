// Display helpers for the streak panel. Day keys are YYYY-MM-DD in UTC and
// are parsed by hand — `new Date("2026-01-03")` then reading local getters
// shifts the day for anyone west of UTC.

const MONTHS = [
  "Jan",
  "Feb",
  "Mar",
  "Apr",
  "May",
  "Jun",
  "Jul",
  "Aug",
  "Sep",
  "Oct",
  "Nov",
  "Dec",
];

export function formatStreakDay(day: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day);
  if (!m) return day;
  const month = MONTHS[Number(m[2]) - 1];
  return month ? `${month} ${Number(m[3])}` : day;
}

export function formatStreakRange(start?: string, end?: string): string {
  if (!start || !end) return "";
  if (start === end) return formatStreakDay(start);
  return `${formatStreakDay(start)}–${formatStreakDay(end)}`;
}

/** 950 -> "950", 12_345 -> "12.3k", 234_567 -> "235k". */
export function formatCompact(n: number): string {
  const abs = Math.abs(n);
  if (abs < 1000) return String(n);
  if (abs < 100_000) {
    const v = (n / 1000).toFixed(1).replace(/\.0$/, "");
    return `${v}k`;
  }
  return `${Math.round(n / 1000)}k`;
}

/** Cents matter under $100; commas matter over it. */
export function formatStreakSpend(usd: number): string {
  if (usd < 100) return `$${usd.toFixed(2)}`;
  return `$${Math.round(usd).toLocaleString("en-US")}`;
}
