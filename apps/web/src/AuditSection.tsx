import type { ReactNode } from "react";
import { card } from "./ui.tsx";

/**
 * One record kind on the Audit page: a card headed by its name, how many it
 * holds and its actions, with its rows on hairlines inside.
 */
export function AuditSection({
  id,
  title,
  count,
  actions,
  children,
}: {
  id: string;
  title: string;
  count?: number;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section aria-labelledby={`${id}-title`} className={`${card} flex min-w-0 flex-col gap-3`}>
      <header className="flex min-h-9 flex-wrap items-center justify-between gap-x-3 gap-y-2">
        <h2 id={`${id}-title`} className="flex items-baseline gap-2 font-medium text-aui-base">
          {title}
          {count !== undefined && (
            <span className="font-normal text-aui-sm tabular-nums text-aui-muted">{count}</span>
          )}
        </h2>
        {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
      </header>
      {children}
    </section>
  );
}

/** The rows of a section, on hairlines, flush with its header. */
export const auditRows = "flex flex-col divide-y divide-[var(--humagen-rule)]";
/** One row: what happened on the left, when and its actions on the right. */
export const auditRow =
  "flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2 py-3 first:pt-0 last:pb-0";
