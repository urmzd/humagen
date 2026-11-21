import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { AuditSection, auditRow, auditRows } from "./AuditSection.tsx";
import { api, type ReadAuditRecord } from "./api.ts";
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
                <h3 className="break-words font-medium text-sm">{r.subject}</h3>
                {/* The same line a workspace change has under it: where, how, and exactly when. */}
                <p className="mt-1 flex min-w-0 flex-wrap gap-x-3 gap-y-1 text-aui-muted text-sm">
                  <span className="min-w-0 [overflow-wrap:anywhere]">
                    {r.project || "~"}
                    {r.thread ? ` / ${r.thread}` : ""}
                  </span>
                  {r.route && (
                    <span className="min-w-0 font-aui-mono [overflow-wrap:anywhere]">
                      {r.route}
                    </span>
                  )}
                  <time dateTime={r.at}>{new Date(r.at).toLocaleString()}</time>
                </p>
              </div>
            </li>
          ))}
        </ol>
      )}
    </AuditSection>
  );
}
