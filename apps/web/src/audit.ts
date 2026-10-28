import type { AuditRecord } from "../../../packages/client/src/generated/types.ts";

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
