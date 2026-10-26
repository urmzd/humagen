import { defaultCopy } from "@humagen/components";
import type { AuditRecord } from "../../../packages/client/src/generated/types.ts";
import { useEffect, useState } from "react";
import { api } from "./api.ts";
import { card, pageLede, pageTitle, Problem, SkeletonRows } from "./ui.tsx";

/** A bounded audit window. Before/after snapshots are server-redacted. */
export function Audit() {
  const [rows, setRows] = useState<AuditRecord[] | null>(null);
  const [failed, setFailed] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const copy = defaultCopy.audit;
  useEffect(() => {
    let live = true;
    setFailed(false);
    api
      .audit()
      .then((value) => {
        if (live) setRows(value);
      })
      .catch(() => {
        if (live) setFailed(true);
      });
    return () => {
      live = false;
    };
  }, [refresh]);
  return (
    <section className="min-w-0 space-y-5">
      <header>
        <h1 className={pageTitle}>{copy.title}</h1>
        <p className={pageLede}>{copy.description}</p>
      </header>
      <button
        type="button"
        onClick={() => setRefresh((n) => n + 1)}
        className="min-h-11 text-aui-accent"
      >
        {copy.refresh}
      </button>
      {failed ? (
        <Problem message={copy.failed} />
      ) : rows === null ? (
        <SkeletonRows count={3} />
      ) : rows.length === 0 ? (
        <p className="text-aui-muted">{copy.empty}</p>
      ) : (
        <ol className="space-y-3">
          {rows.map((row) => (
            <li key={row.id} className={`${card} min-w-0 break-words p-4`}>
              <div className="flex flex-wrap items-baseline justify-between gap-2">
                <h2 className="font-medium">{row.action}</h2>
                <span className="text-aui-sm text-aui-muted">{row.outcome}</span>
              </div>
              <p>{row.resource_id}</p>
              <p className="text-aui-sm text-aui-muted">
                {row.actor} · {row.scope}
                {row.project ? ` / ${row.project}` : ""}
              </p>
              {row.route && (
                <p className="text-aui-sm text-aui-muted">
                  {copy.route}: {row.route}
                </p>
              )}
              {row.rule && (
                <p className="text-aui-sm text-aui-muted">
                  {copy.rule}: {row.rule}
                </p>
              )}
              <time dateTime={row.at} className="text-aui-sm text-aui-muted">
                {new Date(row.at).toLocaleString()}
              </time>
              {(
                [
                  [copy.before, row.before],
                  [copy.after, row.after],
                ] as const
              ).map(([label, fields]) =>
                fields && Object.keys(fields).length > 0 ? (
                  <details key={label} className="mt-3">
                    <summary className="min-h-11 cursor-pointer">{label}</summary>
                    <dl className="space-y-2">
                      {Object.entries(fields).map(([key, value]) => (
                        <div key={key}>
                          <dt className="text-aui-sm text-aui-muted">{key}</dt>
                          <dd className="whitespace-pre-wrap">{value}</dd>
                        </div>
                      ))}
                    </dl>
                  </details>
                ) : null,
              )}
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}
