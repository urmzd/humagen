import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { AuditSection, auditRow, auditRows } from "./AuditSection.tsx";
import { api, type ReadAuditRecord } from "./api.ts";
import { MessageTime } from "./MessageTime.tsx";
import { Problem, SkeletonRows } from "./ui.tsx";

/** The last day's recorded exceptional reads, bounded by the server. */
export function ReadAudit() {
  const [records, setRecords] = useState<ReadAuditRecord[] | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let live = true;
    api
      .readAudit()
      .then((rows) => {
        if (live) setRecords(rows);
      })
      .catch(() => {
        if (live) setFailed(true);
      });
    return () => {
      live = false;
    };
  }, []);
  const copy = defaultCopy.readAudit;
  return (
    <AuditSection id="read-audit" title={copy.title} count={records?.length}>
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
                <p className="truncate text-aui-sm text-aui-fg">{r.subject}</p>
                <p className="flex min-w-0 flex-wrap gap-x-3 text-aui-xs text-aui-muted">
                  <span className="min-w-0 truncate">
                    {r.project || "~"}
                    {r.thread ? ` / ${r.thread}` : ""}
                  </span>
                  {r.route && <span className="min-w-0 truncate font-aui-mono">{r.route}</span>}
                </p>
              </div>
              <span className="shrink-0 text-aui-xs text-aui-muted">
                <MessageTime at={r.at} />
              </span>
            </li>
          ))}
        </ol>
      )}
    </AuditSection>
  );
}
