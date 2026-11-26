import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@humagen/render-web/ui/table";
import type { ReactNode } from "react";
import { follow } from "./cards.tsx";

/** One column: its name, what each row shows in it, and below which width it hides. */
export type Column<T> = {
  name: string;
  cell: (row: T) => ReactNode;
  /** Hidden below this breakpoint, so a phone keeps what matters. */
  hide?: "sm" | "md" | "lg";
  className?: string;
};

const hidden = {
  sm: "hidden sm:table-cell",
  md: "hidden md:table-cell",
  lg: "hidden lg:table-cell",
};

/**
 * Things to compare, as one table built from shadcn's Table: a quiet header
 * on a rule, then each thing as a row with a rule under it and a fill while
 * the pointer is on it. The first column holds the row's link and the whole
 * row opens it; a trailing control, such as the ⋯ menu, stays clickable
 * above that link. Every list of things a person compares and opens uses
 * this: Sessions, Projects, repositories. Alerts draws the same row.
 */
export function DataTable<T>({
  label,
  rows,
  rowKey,
  columns,
  link,
  control,
  controlLabel = "Actions",
}: {
  label: string;
  rows: T[];
  rowKey: (row: T) => string;
  /** The first column is the row's name and holds its link. */
  columns: Column<T>[];
  link: (row: T) => { href: string; onOpen?: () => void; current?: boolean };
  /** A control at the end of each row, above the row's link. */
  control?: (row: T) => ReactNode;
  controlLabel?: string;
}) {
  const [first, ...rest] = columns;
  if (!first) return null;
  // The rules run the width of the page's column; the words stand a little in from its edges.
  const cell = "px-3 py-3 first:pl-2 last:pr-2";
  return (
    <Table aria-label={label} className="text-aui-sm">
      <TableHeader>
        <TableRow className="hover:bg-transparent">
          {columns.map((c) => (
            <TableHead
              key={c.name}
              className={`h-9 px-3 font-normal text-aui-xs text-aui-muted first:pl-2 last:pr-2 ${c.hide ? hidden[c.hide] : ""} ${c.className ?? ""}`}
            >
              {c.name}
            </TableHead>
          ))}
          {control && (
            <TableHead className="h-9 w-10 px-2">
              <span className="sr-only">{controlLabel}</span>
            </TableHead>
          )}
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row) => {
          const to = link(row);
          return (
            <TableRow
              key={rowKey(row)}
              data-current={to.current ? "true" : undefined}
              className="group relative hover:bg-aui-hover data-[current=true]:bg-aui-hover"
            >
              <TableCell className={`${cell} w-full max-w-0 ${first.className ?? ""}`}>
                <a
                  href={to.href}
                  onClick={(event) => follow(event, to.onOpen)}
                  aria-current={to.current ? "true" : undefined}
                  className="block truncate font-medium text-aui-fg outline-none -my-3 py-3 after:absolute after:inset-0 focus-visible:after:outline-2 focus-visible:after:outline-aui-accent"
                >
                  {first.cell(row)}
                </a>
              </TableCell>
              {rest.map((c) => (
                <TableCell
                  key={c.name}
                  className={`${cell} text-aui-muted text-aui-xs ${c.hide ? hidden[c.hide] : ""} ${c.className ?? ""}`}
                >
                  {c.cell(row)}
                </TableCell>
              ))}
              {control && (
                <TableCell className={`${cell} relative z-10 w-10 px-2`}>{control(row)}</TableCell>
              )}
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}
