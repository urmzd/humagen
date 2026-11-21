import { defaultCopy } from "@humagen/components";
import { ChevronRightIcon, DownloadIcon, RefreshIcon } from "@humagen/render-web";
import { Badge } from "@humagen/render-web/ui/badge";
import { Button } from "@humagen/render-web/ui/button";
import { DateRangePicker } from "@humagen/render-web/ui/date-range-picker";
import { Input } from "@humagen/render-web/ui/input";
import { Label } from "@humagen/render-web/ui/label";
import { NativeSelect } from "@humagen/render-web/ui/native-select";
import { useEffect, useState } from "react";
import type { AuditRecord } from "../../../packages/client/src/generated/types.ts";
import { AuditSection } from "./AuditSection.tsx";
import { api, type ReadAuditRecord } from "./api.ts";
import { auditCSV, changesBeside } from "./audit.ts";
import { ReadAudit } from "./ReadAudit.tsx";
import { href } from "./route.ts";
import { Page, Problem, SkeletonRows } from "./ui.tsx";

/**
 * Workspace changes and authorized break-glass reads on one Audit page.
 */
export function Audit({
  reads = false,
}: {
  /** Shows break-glass reads, for an admin. */
  reads?: boolean;
}) {
  const [rows, setRows] = useState<AuditRecord[] | null>(null);
  const [listed, setListed] = useState<ReadAuditRecord[]>([]);
  const [failed, setFailed] = useState(false);
  const [query, setQuery] = useState<Record<string, string>>({});
  const [actor, setActor] = useState("");
  const [domain, setDomain] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [invalidRange, setInvalidRange] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const copy = defaultCopy.audit;
  useEffect(() => {
    let live = true;
    setFailed(false);
    setRows(null);
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
      const end = to ? new Date(to) : new Date();
      const start = from ? new Date(from) : new Date(end.getTime() - 24 * 60 * 60 * 1000);
      const duration = end.getTime() - start.getTime();
      if (!Number.isFinite(duration) || duration <= 0 || duration > 31 * 24 * 60 * 60 * 1000)
        throw new Error("range");
      setInvalidRange(false);
      setQuery({
        ...(actor.trim() ? { actor: actor.trim() } : {}),
        ...(domain ? { resource: domain } : {}),
        ...(from ? { from: start.toISOString() } : {}),
        ...(to ? { to: end.toISOString() } : {}),
      });
    } catch {
      setInvalidRange(true);
    }
  };
  // A read the section above lists is not a second row here. The export keeps every row.
  const changes = rows && reads ? changesBeside(rows, listed) : rows;
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
                ? href({ page: "project", name: row.project ?? "" })
                : undefined;
  const humanize = (value: string) =>
    value.replace(/[_.:-]+/g, " ").replace(/^./, (letter) => letter.toUpperCase());
  return (
    <Page title={copy.title}>
      {reads && <ReadAudit onListed={setListed} />}
      <AuditSection
        id="changes"
        title={copy.changes}
        count={changes?.length}
        actions={
          <>
            <Button variant="ghost" size="sm" onClick={() => setRefresh((value) => value + 1)}>
              <RefreshIcon aria-hidden="true" />
              {copy.refresh}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={!rows?.length || failed}
              onClick={download}
            >
              <DownloadIcon aria-hidden="true" />
              {copy.export}
            </Button>
          </>
        }
      >
        <form
          onSubmit={(event) => {
            event.preventDefault();
            apply();
          }}
          className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-[1fr_1fr_1.5fr_auto] xl:items-end"
        >
          <div className="space-y-2">
            <Label htmlFor="audit-actor">{copy.actor}</Label>
            <Input
              id="audit-actor"
              maxLength={512}
              value={actor}
              onChange={(event) => setActor(event.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="audit-domain">{copy.domain}</Label>
            <NativeSelect
              id="audit-domain"
              value={domain}
              onChange={(event) => setDomain(event.target.value)}
            >
              <option value="">{copy.allDomains}</option>
              {Object.entries(copy.domains).map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </NativeSelect>
          </div>
          <div className="space-y-2">
            <Label>{copy.dateRange}</Label>
            <DateRangePicker
              from={from}
              to={to}
              onChange={(start, end) => {
                setFrom(start);
                setTo(end);
                setInvalidRange(false);
              }}
              labels={{
                label: copy.dateRange,
                empty: copy.rangeDefault,
                from: copy.from,
                to: copy.to,
                time: copy.time,
                clear: copy.clearRange,
              }}
            />
          </div>
          <Button type="submit">{copy.apply}</Button>
        </form>
        {invalidRange && <Problem message={copy.invalidRange} />}
        {failed ? (
          <Problem message={copy.failed} />
        ) : !changes ? (
          <SkeletonRows count={3} />
        ) : changes.length === 0 ? (
          <p className="text-aui-muted">{copy.empty}</p>
        ) : (
          <ol className="-mx-4 divide-y divide-[var(--humagen-rule)] border-[var(--humagen-rule)] border-t">
            {changes.map((row) => (
              <li key={row.id} className="min-w-0 break-words px-4 py-3">
                <div className="flex flex-wrap items-start justify-between gap-2 sm:flex-nowrap">
                  <div className="min-w-48 flex-1 sm:min-w-0">
                    <h3 className="text-sm font-medium">{humanize(row.action)}</h3>
                    <div className="mt-1 flex min-w-0 flex-wrap gap-x-3 gap-y-1 text-sm text-aui-muted [&>span]:min-w-0 [&>span]:[overflow-wrap:anywhere]">
                      <span>{row.actor}</span>
                      {row.project && <span>{row.project}</span>}
                      <time dateTime={row.at}>{new Date(row.at).toLocaleString()}</time>
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <Badge
                      variant="outline"
                      className={
                        row.outcome === "failed"
                          ? "text-aui-bad"
                          : row.outcome === "pending"
                            ? "text-aui-warn"
                            : "text-aui-muted"
                      }
                    >
                      {copy.outcomes[row.outcome] ?? row.outcome}
                    </Badge>
                    {resource(row) && (
                      <Button asChild variant="ghost" size="sm">
                        <a href={resource(row)}>
                          {copy.open}
                          <ChevronRightIcon aria-hidden="true" />
                        </a>
                      </Button>
                    )}
                  </div>
                </div>
                <details className="group mt-1">
                  <summary className="flex w-fit cursor-pointer list-none items-center rounded-md text-xs text-aui-muted focus-visible:outline-2 focus-visible:outline-aui-accent">
                    <span className="inline-flex min-h-9 items-center gap-1 pointer-coarse:min-h-11">
                      <ChevronRightIcon
                        aria-hidden="true"
                        className="size-3.5 transition-transform group-open:rotate-90"
                      />
                      {copy.details}
                    </span>
                  </summary>
                  <dl className="mt-1 grid gap-2 rounded-lg bg-aui-muted/5 p-3 text-xs sm:grid-cols-2">
                    {[
                      [copy.eventID, row.id],
                      [copy.resourceID, row.resource_id],
                      [copy.domain, `${row.resource} / ${row.scope}`],
                      [copy.route, row.route],
                      [copy.rule, row.rule],
                    ].map(([label, value]) =>
                      value ? (
                        <div key={label} className="min-w-0">
                          <dt className="text-aui-muted">{label}</dt>
                          <dd className="mt-1 break-all font-aui-mono">{value}</dd>
                        </div>
                      ) : null,
                    )}
                  </dl>
                  {(
                    [
                      [copy.before, row.before],
                      [copy.after, row.after],
                    ] as const
                  ).map(([label, fields]) =>
                    fields && Object.keys(fields).length > 0 ? (
                      <details
                        key={label}
                        className="mt-2 rounded-lg border border-aui-border px-3"
                      >
                        <summary className="cursor-pointer py-3 text-xs font-medium focus-visible:outline-2 focus-visible:outline-aui-accent">
                          {label}
                        </summary>
                        <dl className="space-y-2 pb-3">
                          {Object.entries(fields).map(([key, value]) => (
                            <div key={key}>
                              <dt className="text-xs text-aui-muted">{key}</dt>
                              <dd className="mt-1 whitespace-pre-wrap text-sm">{value}</dd>
                            </div>
                          ))}
                        </dl>
                      </details>
                    ) : null,
                  )}
                </details>
              </li>
            ))}
          </ol>
        )}
      </AuditSection>
    </Page>
  );
}
