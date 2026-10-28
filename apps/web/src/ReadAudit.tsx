import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { api, type ReadAuditRecord } from "./api.ts";
import { card, Problem, pageLede, pageTitle, SkeletonRows } from "./ui.tsx";

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
    <>
      <header>
        <h1 className={pageTitle}>{copy.title}</h1>
        <p className={pageLede}>{copy.description}</p>
      </header>
      {failed ? (
        <Problem message={copy.failed} />
      ) : records === null ? (
        <SkeletonRows count={3} />
      ) : records.length === 0 ? (
        <p className="text-aui-muted">{copy.empty}</p>
      ) : (
        <ol className="flex flex-col gap-3">
          {records.map((r, i) => (
            <li key={`${r.at}-${i}`} className={`${card} min-w-0 break-words p-4`}>
              <p>{r.subject}</p>
              <p className="text-aui-muted">
                {r.project || "~"}
                {r.thread ? ` / ${r.thread}` : ""}
              </p>
              <p className="text-aui-sm text-aui-muted">{r.route}</p>
              <time className="text-aui-sm text-aui-muted" dateTime={r.at}>
                {new Date(r.at).toLocaleString()}
              </time>
            </li>
          ))}
        </ol>
      )}
    </>
  );
}
