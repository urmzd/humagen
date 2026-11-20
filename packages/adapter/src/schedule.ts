import { type Copy, defaultCopy } from "@humagen/components/copy";
import { type Field, timeToken } from "@humagen/core";

/**
 * Words a model wrote, made inert as the shared components' `untrusted` makes
 * them: a link's markup and a time token are broken with a zero-width space.
 */
function untrusted(text: string): string {
  return text.replaceAll("](", "]​(").replaceAll("{{", "{​{");
}

/**
 * A schedule an agent proposes from a conversation, as its approval card
 * shows it. The platform's create_schedule and update_schedule take a name, a
 * cron read in UTC, what each run checks, an agent (the asking one when left
 * out) and where it posts; the card says each in words, with when it would
 * first run. Pure: the time is given.
 */

/** The run tools whose approval cards preview a schedule. */
export const SCHEDULE_PROPOSALS = ["create_schedule", "update_schedule"] as const;

type When = Copy["schedules"]["when"];

/** A cron expression in words, for the common shapes and steps: "Every 15 minutes". Otherwise the expression itself. */
export function cronWords(expression: string, words: When = defaultCopy.schedules.when): string {
  const parts = expression.trim().split(/\s+/);
  const [min, hour, dom, month, dow] = parts;
  const num = (s: string | undefined, max: number) =>
    s !== undefined && /^\d+$/.test(s) && Number(s) <= max;
  const step = (s: string | undefined, max: number) => {
    const m = /^\*\/(\d+)$/.exec(s ?? "");
    return m && Number(m[1]) >= 1 && Number(m[1]) <= max ? Number(m[1]) : undefined;
  };
  if (parts.length !== 5 || dom !== "*" || month !== "*") return expression;
  if (hour === "*" && dow === "*") {
    if (min === "*" || step(min, 59) === 1) return words.everyMinute;
    const n = step(min, 59);
    if (n !== undefined) return words.everyMinutes(n);
    if (!num(min, 59)) return expression;
    return Number(min) === 0 ? words.hourly : words.hourlyAt(Number(min));
  }
  if (!num(min, 59)) return expression;
  const hours = step(hour, 23);
  if (hours !== undefined && dow === "*" && Number(min) === 0) return words.everyHours(hours);
  if (!num(hour, 23)) return expression;
  const at = `${(hour as string).padStart(2, "0")}:${(min as string).padStart(2, "0")}`;
  if (dow === "*") return words.daily(at);
  if (num(dow, 7)) return words.weekly(Number(dow) % 7, at);
  if (dow === "1-5") return words.weekdays(at);
  if (dow === "0,6" || dow === "6,0") return words.weekends(at);
  return expression;
}

/** The values one cron field allows, or undefined when it cannot be read. */
function field(spec: string, lo: number, hi: number): Set<number> | undefined {
  const out = new Set<number>();
  for (const part of spec.split(",")) {
    const m = /^(\*|(\d+)(?:-(\d+))?)(?:\/(\d+))?$/.exec(part);
    if (!m) return undefined;
    const from = m[1] === "*" ? lo : Number(m[2]);
    const to = m[1] === "*" ? hi : m[3] !== undefined ? Number(m[3]) : m[4] ? hi : from;
    const by = m[4] ? Number(m[4]) : 1;
    if (from < lo || to > hi || from > to || by < 1) return undefined;
    for (let v = from; v <= to; v += by) out.add(v);
  }
  return out;
}

/**
 * When a cron expression, read in UTC, next fires after a time, or undefined
 * when it cannot be read or does not fire within a year. A day fires when the
 * day of the month or the weekday allows it, as cron reads two restricted
 * day fields.
 */
export function nextRun(expression: string, after: Date): Date | undefined {
  const parts = expression.trim().split(/\s+/);
  if (parts.length !== 5) return undefined;
  const [mins, hours, doms, months, dows] = [
    field(parts[0] as string, 0, 59),
    field(parts[1] as string, 0, 23),
    field(parts[2] as string, 1, 31),
    field(parts[3] as string, 1, 12),
    field(parts[4] as string, 0, 7),
  ];
  if (!mins || !hours || !doms || !months || !dows) return undefined;
  if (dows.has(7)) dows.add(0);
  const anyDom = parts[2] === "*";
  const anyDow = parts[4] === "*";
  const dayOk = (d: Date) => {
    const byDom = doms.has(d.getUTCDate());
    const byDow = dows.has(d.getUTCDay());
    if (anyDom || anyDow) return byDom && byDow;
    return byDom || byDow;
  };
  const t = new Date(after.getTime());
  t.setUTCSeconds(0, 0);
  t.setUTCMinutes(t.getUTCMinutes() + 1);
  const end = after.getTime() + 366 * 24 * 60 * 60 * 1000;
  while (t.getTime() <= end) {
    if (!months.has(t.getUTCMonth() + 1)) {
      t.setUTCMonth(t.getUTCMonth() + 1, 1);
      t.setUTCHours(0, 0, 0, 0);
    } else if (!dayOk(t)) {
      t.setUTCDate(t.getUTCDate() + 1);
      t.setUTCHours(0, 0, 0, 0);
    } else if (!hours.has(t.getUTCHours())) {
      t.setUTCHours(t.getUTCHours() + 1, 0, 0, 0);
    } else if (!mins.has(t.getUTCMinutes())) {
      t.setUTCMinutes(t.getUTCMinutes() + 1, 0, 0);
    } else {
      return t;
    }
  }
  return undefined;
}

const str = (v: unknown) => (typeof v === "string" ? v.trim() : "");

/**
 * The card's fields for a proposed schedule: its name, what it checks, how
 * often, which agent, where it posts and when it would next run. Undefined for
 * any other tool, whose card shows its arguments.
 */
export function schedulePreview(
  tool: string,
  args: Record<string, unknown> | undefined,
  asking: string | undefined,
  now: Date = new Date(),
  copy: Copy = defaultCopy,
): Field[] | undefined {
  if (!args || !(SCHEDULE_PROPOSALS as readonly string[]).includes(tool)) return undefined;
  const words = copy.schedules.proposal;
  const cron = str(args.cron).split(/\s+/).join(" ");
  const post = str(args.post).toLowerCase();
  const to = untrusted(str(args.to));
  const posts =
    post === "here"
      ? words.postsHere
      : post === "slack"
        ? words.postsSlack(to)
        : post === "github"
          ? words.postsGitHub(to)
          : post === "inbox"
            ? words.postsInbox
            : words.postsNowhere;
  const next = nextRun(cron, now);
  const fields: Field[] = [
    { label: words.name, value: untrusted(str(args.name)) },
    { label: words.checks, value: untrusted(str(args.check)) },
    { label: words.every, value: untrusted(cronWords(cron, copy.schedules.when)) },
    { label: words.agent, value: untrusted(str(args.agent) || asking || "") },
    { label: words.posts, value: posts },
    ...(next ? [{ label: words.nextRun, value: timeToken(next.toISOString()) }] : []),
  ];
  return fields.filter((f) => f.value !== "");
}
