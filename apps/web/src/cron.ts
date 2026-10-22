/**
 * Schedule expressions in words. A schedule's expression is cron, read in
 * UTC: minute hour day month weekday. Pure, so plain modules and their tests
 * can use it too.
 */

/** The shapes offered when making a schedule, each with its words. */
export const presets: { label: string; expression: string }[] = [
  { label: "Every minute", expression: "* * * * *" },
  { label: "Hourly", expression: "0 * * * *" },
  { label: "Daily at 09:00 UTC", expression: "0 9 * * *" },
  { label: "Weekdays at 09:00 UTC", expression: "0 9 * * 1-5" },
];

const days = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

/** A cron expression in words, for the common shapes: "Mondays at 08:09 UTC". Otherwise the expression itself. */
export function inWords(expression: string): string {
  const preset = presets.find((p) => p.expression === expression);
  if (preset) return preset.label;
  const [min, hour, dom, month, dow] = expression.trim().split(/\s+/);
  const num = (s?: string) => s !== undefined && /^\d+$/.test(s);
  if (!num(min) || Number(min) > 59 || dom !== "*" || month !== "*") return expression;
  if (hour === "*" && dow === "*") return `Hourly at minute ${Number(min)}`;
  if (!num(hour) || Number(hour) > 23) return expression;
  const at = `${(hour as string).padStart(2, "0")}:${(min as string).padStart(2, "0")} UTC`;
  if (dow === "*") return `Daily at ${at}`;
  if (num(dow) && Number(dow) <= 7) return `${days[Number(dow) % 7]}s at ${at}`;
  if (dow === "1-5") return `Weekdays at ${at}`;
  if (dow === "0,6" || dow === "6,0") return `Weekends at ${at}`;
  return expression;
}
