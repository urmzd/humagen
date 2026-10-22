import { defaultCopy } from "@warp/components";

/**
 * Schedule expressions in words. A schedule's expression is cron, read in
 * UTC: minute hour day month weekday. The words are the copy table's.
 */

const words = defaultCopy.schedules.when;

/** A cron expression in words, for the common shapes: "Mondays at 08:09 UTC". Otherwise the expression itself. */
export function inWords(expression: string): string {
  const [min, hour, dom, month, dow, ...rest] = expression.trim().split(/\s+/);
  const num = (s?: string) => s !== undefined && /^\d+$/.test(s);
  if (rest.length > 0 || dom !== "*" || month !== "*") return expression;
  if (min === "*" && hour === "*" && dow === "*") return words.everyMinute;
  if (!num(min) || Number(min) > 59) return expression;
  if (hour === "*" && dow === "*") return Number(min) === 0 ? words.hourly : words.hourlyAt(Number(min));
  if (!num(hour) || Number(hour) > 23) return expression;
  const at = `${(hour as string).padStart(2, "0")}:${(min as string).padStart(2, "0")}`;
  if (dow === "*") return words.daily(at);
  if (num(dow) && Number(dow) <= 7) return words.weekly(Number(dow) % 7, at);
  if (dow === "1-5") return words.weekdays(at);
  if (dow === "0,6" || dow === "6,0") return words.weekends(at);
  return expression;
}

/** The shapes offered when making a schedule, each with its words. */
export const presets: { label: string; expression: string }[] = ["* * * * *", "0 * * * *", "0 9 * * *", "0 9 * * 1-5"].map((expression) => ({
  expression,
  label: inWords(expression),
}));
