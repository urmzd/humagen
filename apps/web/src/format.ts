import type { Run } from "./adapter.ts";

/** Micro-USD as dollars. Zero is an unmetered run, not a free one. */
export function usd(micro: number): string {
  if (micro === 0) return "Unmetered";
  return new Intl.NumberFormat(undefined, { style: "currency", currency: "USD" }).format(
    micro / 1_000_000,
  );
}

/** A duration a person can read at a glance: 840ms, 4.2s, 3m 05s. */
export function took(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)}s`;
  const m = Math.floor(ms / 60_000);
  const s = Math.round((ms % 60_000) / 1000);
  return `${m}m ${String(s).padStart(2, "0")}s`;
}

/** The last line of an error. Runtime errors nest, and the cause is at the end. */
export function cause(error: string | undefined): string {
  const lines = (error ?? "").split("\n").filter((l) => l.trim());
  return lines[lines.length - 1]?.trim() ?? "";
}

/** A run that is over, however it ended: it finished, it failed, or it was stopped from outside. */
export function isEnded(run: Pick<Run, "status">): boolean {
  return run.status === "completed" || run.status === "failed" || run.status === "stopped";
}

/** How long a run took, or has taken so far. */
export function elapsed(run: Run): number {
  return Math.max(
    (isEnded(run) ? Date.parse(run.updated_at) : Date.now()) - Date.parse(run.created_at),
    0,
  );
}

/** Micro-USD as dollars, with zero shown as $0.00. For budgets, where zero is a real amount. */
export function dollars(micro: number): string {
  return new Intl.NumberFormat(undefined, { style: "currency", currency: "USD" }).format(
    micro / 1_000_000,
  );
}

/** 1536 as "1.5 KB". Whole bytes below a kilobyte, one decimal above. */
export function bytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 ** 2) return `${(n / 1024).toFixed(n < 10 * 1024 ? 1 : 0)} KB`;
  if (n < 1024 ** 3) return `${(n / 1024 ** 2).toFixed(1)} MB`;
  return `${(n / 1024 ** 3).toFixed(1)} GB`;
}

/*
 * How long ago something happened, counted one way everywhere: a row's age,
 * a message's time and the day a list files it under must never disagree.
 *
 * - Under a day, whole minutes and hours that have passed (never rounded up).
 * - From a day on, calendar days in the reader's own time zone: anything
 *   dated yesterday is one day old, whatever the hour, so it reads "1d" or
 *   "yesterday" under the heading Yesterday.
 */

const startOfDay = (ms: number) => {
  const d = new Date(ms);
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
};

/** Whole calendar days from `iso` to `now` in local time: 0 today, 1 yesterday, negative ahead. */
export function daysAgo(iso: string, now: number = Date.now()): number {
  return Math.round((startOfDay(now) - startOfDay(Date.parse(iso))) / 86_400_000);
}

type Span = { n: number; unit: "minute" | "hour" | "day" };

/** The distance between `iso` and `now`, by the rule above. Null under a minute, or when `iso` is no date. */
function span(iso: string, now: number): Span | null {
  const seconds = Math.abs(now - Date.parse(iso)) / 1000;
  if (!Number.isFinite(seconds) || seconds < 60) return null;
  if (seconds < 3600) return { n: Math.floor(seconds / 60), unit: "minute" };
  if (seconds < 86_400) return { n: Math.floor(seconds / 3600), unit: "hour" };
  // A full day apart is at least one calendar day apart.
  return { n: Math.max(1, Math.abs(daysAgo(iso, now))), unit: "day" };
}

/** How long ago, in the fewest characters: now, 5m, 3h, 2d, 3w, 9mo, 2y. */
export function age(iso: string, now: number = Date.now()): string {
  const s = Date.parse(iso) > now ? null : span(iso, now);
  if (!s) return "now";
  if (s.unit === "minute") return `${s.n}m`;
  if (s.unit === "hour") return `${s.n}h`;
  if (s.n < 7) return `${s.n}d`;
  if (s.n < 30) return `${Math.floor(s.n / 7)}w`;
  if (s.n < 365) return `${Math.floor(s.n / 30)}mo`;
  return `${Math.floor(s.n / 365)}y`;
}

const relative = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

/** How long ago, or how far ahead, in words: just now, 5 minutes ago, yesterday, in 3 days. */
export function ago(iso: string, now: number = Date.now()): string {
  const s = span(iso, now);
  if (!s) return "just now";
  return relative.format(Date.parse(iso) > now ? s.n : -s.n, s.unit);
}
