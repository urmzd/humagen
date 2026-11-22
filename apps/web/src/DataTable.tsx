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

/** Where every cell after this one hides, it ends the row: round it there, not square. */
const ends = {
  sm: "max-sm:rounded-r-aui-lg",
  md: "max-md:rounded-r-aui-lg",
  lg: "max-lg:rounded-r-aui-lg",
};
function endOf(later: { hide?: keyof typeof ends }[], control: boolean): string {
  if (control || !later.length || later.some((c) => !c.hide)) return "";
  const bp = (["sm", "md", "lg"] as const).find((b) => later.some((c) => c.hide === b));
  return bp ? ends[bp] : "";
}

/**
 * Things to compare, as one table built from shadcn's Table: a quiet header,
 * then each thing as a soft rounded row with space between rows. The first
 * column holds the row's link and the whole row opens it; a trailing control,
 * such as the ⋯ menu, stays clickable above that link. Every list of things
 * a person compares and opens uses this: Sessions, Projects, Alerts.
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
  const cell =
    "bg-[color-mix(in_oklab,var(--aui-hover)_55%,transparent)] px-4 py-3 transition-colors group-hover:bg-aui-hover group-data-[current=true]:bg-aui-hover first:rounded-l-aui-lg last:rounded-r-aui-lg";
  return (
    <Table aria-label={label} className="border-separate border-spacing-y-1.5 text-aui-sm">
      <TableHeader className="[&_tr]:border-0">
        <TableRow className="border-0 hover:bg-transparent">
          {columns.map((c) => (
            <TableHead
              key={c.name}
              className={`h-6 px-4 font-normal text-aui-2xs text-aui-muted ${c.hide ? hidden[c.hide] : ""} ${c.className ?? ""}`}
            >
              {c.name}
            </TableHead>
          ))}
          {control && (
            <TableHead className="h-6 w-10 px-2">
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
              className="group relative border-0 hover:bg-transparent"
            >
              <TableCell
                className={`${cell} w-full max-w-0 ${endOf(rest, !!control)} ${first.className ?? ""}`}
              >
                <a
                  href={to.href}
                  onClick={(event) => follow(event, to.onOpen)}
                  aria-current={to.current ? "true" : undefined}
                  className="block truncate font-medium text-aui-fg outline-none -my-3 py-3 after:absolute after:inset-0 after:rounded-aui-lg focus-visible:after:outline-2 focus-visible:after:outline-aui-accent"
                >
                  {first.cell(row)}
                </a>
              </TableCell>
              {rest.map((c, i) => (
                <TableCell
                  key={c.name}
                  className={`${cell} text-aui-muted text-aui-xs ${c.hide ? hidden[c.hide] : ""} ${endOf(rest.slice(i + 1), !!control)} ${c.className ?? ""}`}
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
