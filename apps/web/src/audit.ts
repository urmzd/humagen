import type { AuditRecord } from "../../../packages/client/src/generated/types.ts";
import type { ReadAuditRecord } from "./api.ts";

/**
 * The most records either audit endpoint returns when no limit is asked for.
 * A list this long may not be all there is, so it is not counted in a heading.
 */
export const auditLimit = 100;

/** The action GET /api/audit gives a break-glass read, which GET /api/read-audit also returns. */
const breakGlassRead = "project.break_glass_read";

const readKey = (who: string, project = "", thread = "", route = "", at = "") =>
  [who, project, thread, route, Date.parse(at)].join("\n");

/**
 * The changes without the break-glass reads the Break-glass reads section
 * lists, so a read shows once on the page. A read that section does not list,
 * such as one from an earlier window, stays among the changes.
 */
export function changesBeside(rows: AuditRecord[], listed: ReadAuditRecord[]): AuditRecord[] {
  if (listed.length === 0) return rows;
  const shown = new Set(listed.map((r) => readKey(r.subject, r.project, r.thread, r.route, r.at)));
  return rows.filter(
    (row) =>
      row.action !== breakGlassRead ||
      !shown.has(readKey(row.actor, row.project, row.thread, row.route, row.at)),
  );
}

/** Spreadsheet formulas stay inert, including after leading whitespace. */
function cell(value: string): string {
  const plain = /^\s*[=+\-@]/.test(value) ? `'${value}` : value;
  return `"${plain.replaceAll('"', '""')}"`;
}
export function auditCSV(rows: AuditRecord[], columns: string[]): string {
  const lines = [
    columns,
    ...rows.map((row) => [
      row.id,
      row.at,
      row.actor,
      row.action,
      row.resource,
      row.resource_id,
      row.outcome,
      row.rule ?? "",
      row.route ?? "",
      row.before ? JSON.stringify(row.before) : "",
      row.after ? JSON.stringify(row.after) : "",
    ]),
  ];
  return `${lines.map((row) => row.map(cell).join(",")).join("\r\n")}\r\n`;
}
