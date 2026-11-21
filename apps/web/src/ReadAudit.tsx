import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { api, type ReadAuditRecord } from "./api.ts";
import { auditLimit } from "./audit.ts";
import { MessageTime } from "./MessageTime.tsx";
import { Problem, Section, SkeletonRows } from "./ui.tsx";

/** The rows of the section, on hairlines, flush with its header. */
const auditRows = "flex flex-col divide-y divide-[var(--humagen-rule)]";
/** One row: what happened on the left, when on the right. */
const auditRow = "flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2 py-3 first:pt-0 last:pb-0";

/** The last day's recorded exceptional reads, bounded by the server. */
export function ReadAudit({
  onListed,
}: {
  /** Told which reads the section lists, so the page does not repeat them. */
  onListed?: (records: ReadAuditRecord[]) => void;
} = {}) {
  const [records, setRecords] = useState<ReadAuditRecord[] | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let live = true;
    api
      .readAudit()
      .then((rows) => {
        if (!live) return;
        setRecords(rows);
        onListed?.(rows);
      })
      .catch(() => {
        if (live) setFailed(true);
      });
    return () => {
      live = false;
    };
  }, [onListed]);
  const copy = defaultCopy.readAudit;
  const capped = (records?.length ?? 0) >= auditLimit;
  return (
    <Section
      box
      title={copy.title}
      count={capped ? undefined : records?.length}
      hint={capped ? defaultCopy.audit.limited : undefined}
    >
      {failed ? (
        <Problem message={copy.failed} />
      ) : records === null ? (
        <SkeletonRows count={2} />
      ) : records.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">{copy.empty}</p>
      ) : (
        <ol className={auditRows}>
          {records.map((r, i) => (
            <li key={`${r.at}-${i}`} className={auditRow}>
              <div className="min-w-0 flex-1">
                <p className="break-words text-aui-sm text-aui-fg">{r.subject}</p>
                <p className="flex min-w-0 flex-wrap gap-x-3 text-aui-xs text-aui-muted">
                  <span className="min-w-0 [overflow-wrap:anywhere]">
                    {r.project || "~"}
                    {r.thread ? ` / ${r.thread}` : ""}
                  </span>
                  {r.route && (
                    <span className="min-w-0 font-aui-mono [overflow-wrap:anywhere]">
                      {r.route}
                    </span>
                  )}
                </p>
              </div>
              <span className="shrink-0 text-aui-xs text-aui-muted">
                <MessageTime at={r.at} />
              </span>
            </li>
          ))}
        </ol>
      )}
    </Section>
  );
}
