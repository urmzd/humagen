import { defaultCopy } from "@humagen/components";
import type { AuditRecord } from "../../../packages/client/src/generated/types.ts";
import { useEffect, useState } from "react";
import { api } from "./api.ts";
import { auditCSV } from "./audit.ts";
import { href } from "./route.ts";
import { card, field, Page, Problem, SkeletonRows } from "./ui.tsx";

export function Audit() {
  const [rows, setRows] = useState<AuditRecord[] | null>(null);
  const [failed, setFailed] = useState(false);
  const [query, setQuery] = useState<Record<string, string>>({});
  const [actor, setActor] = useState("");
  const [domain, setDomain] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [refresh, setRefresh] = useState(0);
  const copy = defaultCopy.audit;
  useEffect(() => {
    let live = true;
    setFailed(false);
    api
      .audit(query)
      .then((value) => {
        if (live) setRows(value);
      })
      .catch(() => {
        if (live) setFailed(true);
      });
    return () => {
      live = false;
    };
  }, [query, refresh]);
  const apply = () => {
    try {
      setQuery({
        ...(actor ? { actor } : {}),
        ...(domain ? { resource: domain } : {}),
        ...(from ? { from: new Date(from).toISOString() } : {}),
        ...(to ? { to: new Date(to).toISOString() } : {}),
      });
    } catch {
      setFailed(true);
    }
  };
  const download = () => {
    if (!rows) return;
    const url = URL.createObjectURL(
      new Blob([auditCSV(rows, copy.csvColumns)], { type: "text/csv;charset=utf-8" }),
    );
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = "humagen-audit.csv";
    anchor.click();
    URL.revokeObjectURL(url);
  };
  const resource = (row: AuditRecord) =>
    row.resource === "memory"
      ? `/library/memory?${new URLSearchParams({ scope: row.scope === "agent" ? "agent" : row.scope === "project" ? (row.project ? "project" : "workspace") : "personal", owner: row.agent ?? row.project ?? "" })}`
      : row.thread
        ? href({ page: "conversation", id: row.thread })
        : row.agent
          ? href({ page: "agent", name: row.agent, tab: "prompt" })
          : row.resource === "source"
            ? href({ page: "context", source: row.resource_id, tab: "documents" })
            : row.resource === "skill"
              ? `/library/skills?${new URLSearchParams({ scope: row.scope, project: row.project ?? "", skill: row.resource_id })}`
              : row.scope === "project"
                ? href({ page: "project", name: row.project ?? "", tab: "history" })
                : undefined;
  return (
    <Page title={copy.title} lede={copy.description}>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          apply();
        }}
        className="flex flex-wrap items-end gap-4"
      >
        <label>
          {copy.actor}
          <input
            className={`${field} mt-2`}
            maxLength={512}
            value={actor}
            onChange={(event) => setActor(event.target.value)}
          />
        </label>
        <label>
          {copy.domain}
          <select
            className={`${field} mt-2`}
            value={domain}
            onChange={(event) => setDomain(event.target.value)}
          >
            <option value="">{copy.allDomains}</option>
            {[
              "project",
              "agent",
              "skill",
              "memory",
              "source",
              "credential",
              "action_policy",
              "assignment",
              "call_policy",
            ].map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </select>
        </label>
        <label>
          {copy.from}
          <input
            type="datetime-local"
            className={`${field} mt-2 max-w-full`}
            value={from}
            onChange={(event) => setFrom(event.target.value)}
          />
        </label>
        <label>
          {copy.to}
          <input
            type="datetime-local"
            className={`${field} mt-2 max-w-full`}
            value={to}
            onChange={(event) => setTo(event.target.value)}
          />
        </label>
        <button type="submit" className="min-h-11 text-aui-accent">
          {copy.apply}
        </button>
      </form>
      <div className="flex flex-wrap gap-5">
        <button
          type="button"
          onClick={() => setRefresh((value) => value + 1)}
          className="min-h-11 text-aui-accent"
        >
          {copy.refresh}
        </button>
        <button
          type="button"
          disabled={!rows?.length}
          onClick={download}
          className="min-h-11 text-aui-accent disabled:opacity-50"
        >
          {copy.export}
        </button>
      </div>
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
                {row.actor}, {row.scope}
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
              {resource(row) && (
                <a
                  href={resource(row)}
                  className="ml-4 inline-flex min-h-11 items-center text-aui-sm text-aui-accent"
                >
                  {copy.open}
                </a>
              )}
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
    </Page>
  );
}
