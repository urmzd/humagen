import { defaultCopy } from "@humagen/components";
import { type FormEvent, useEffect, useState } from "react";
import type { CatalogPage } from "../../../packages/client/src/generated/types.ts";
import { api } from "./api.ts";
import { href } from "./route.ts";
import { button, field, Label, Page, Problem } from "./ui.tsx";

/** Metadata discovery and owner actions share the server's visibility rules. */
export function Catalog({
  canWrite,
  query = "",
  onFilter,
}: {
  canWrite: boolean;
  query?: string;
  onFilter?: (query: string) => void;
}) {
  const copy = defaultCopy.catalog;
  const [page, setPage] = useState<CatalogPage>();
  const [draft, setDraft] = useState(query);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [version, setVersion] = useState(0);
  useEffect(() => setDraft(query), [query]);
  // biome-ignore lint/correctness/useExhaustiveDependencies: owner actions invalidate the catalog after a successful write.
  useEffect(() => {
    let active = true;
    setPage(undefined);
    setProblem(null);
    api
      .catalog(query || undefined)
      .then((value) => {
        if (active) setPage(value);
      })
      .catch(() => {
        if (active) setProblem(copy.failed);
      });
    return () => {
      active = false;
    };
  }, [query, version, copy.failed]);
  const search = (e: FormEvent) => {
    e.preventDefault();
    onFilter?.(draft);
  };
  const action = async (name: string, kind: "publish" | "withdraw" | "crawl") => {
    if (busy) return;
    setBusy(true);
    setProblem(null);
    try {
      if (kind === "crawl") await api.crawlCatalogConnector(name);
      else if (kind === "publish") await api.publishCatalogConnector(name);
      else await api.withdrawCatalogConnector(name);
      setVersion((v) => v + 1);
    } catch {
      setProblem(copy.actionFailed);
    } finally {
      setBusy(false);
    }
  };
  const entries = page?.entries ?? [];
  return (
    <Page title={copy.title} lede={copy.description}>
      <Problem message={problem} />
      <form onSubmit={search} className="mb-6 flex flex-wrap items-end gap-3">
        <div className="min-w-0 flex-1">
          <Label text={copy.query}>
            <input
              type="search"
              className={field}
              maxLength={1000}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
            />
          </Label>
        </div>
        <button className={button} type="submit">
          {copy.search}
        </button>
      </form>
      {!page && !problem && <output>{copy.loading}</output>}
      {page && (
        <div className="grid min-w-0 gap-6 lg:grid-cols-[16rem_minmax(0,1fr)]">
          <section aria-label={copy.connector} className="space-y-4">
            {page.connectors.map((c) => (
              <article key={c.name} className="min-w-0 border-b border-aui-border pb-4">
                <h2 className="break-words text-sm font-medium">{c.name}</h2>
                <p className="text-sm text-aui-muted">
                  {c.system} · {c.entries} {copy.count}
                </p>
                <p className="text-sm text-aui-muted">
                  {c.published ? copy.published : copy.private}
                </p>
                {c.last_error && (
                  <output className="mt-2 block text-sm text-aui-danger">
                    {copy.error}: {c.last_error}
                  </output>
                )}
                {c.mine && canWrite && (
                  <div className="mt-3 flex flex-wrap gap-2">
                    <button
                      type="button"
                      className={button}
                      disabled={busy}
                      onClick={() => void action(c.name, c.published ? "withdraw" : "publish")}
                    >
                      {c.published ? copy.withdraw : copy.publish}
                    </button>
                    {c.capabilities.includes("catalog") && (
                      <button
                        type="button"
                        className={button}
                        disabled={busy}
                        onClick={() => void action(c.name, "crawl")}
                      >
                        {copy.crawl}
                      </button>
                    )}
                  </div>
                )}
              </article>
            ))}
          </section>
          <section aria-label={copy.title} className="min-w-0 space-y-4">
            {!entries.length && (
              <div>
                <h2 className="text-base font-medium">{copy.empty}</h2>
                <p className="mt-2 text-sm text-aui-muted">
                  {page.connectors.length ? copy.noMatch : copy.emptyHint}
                </p>
                {!page.connectors.length && (
                  <a className={`${button} mt-3`} href={href({ page: "connections" })}>
                    Open Connections
                  </a>
                )}
              </div>
            )}
            {entries.map((entry) => (
              <article
                key={`${entry.connector}/${entry.path}`}
                className="min-w-0 border-b border-aui-border pb-5"
              >
                <h2 className="break-all font-mono text-sm font-medium">{entry.path}</h2>
                <p className="mt-1 text-sm text-aui-muted">
                  {entry.connector} · {entry.kind}
                </p>
                {entry.description && (
                  <p className="mt-2 whitespace-pre-wrap break-words text-sm">
                    {entry.description}
                  </p>
                )}
                {!!entry.schema?.length && (
                  <details className="mt-3">
                    <summary className="min-h-8 cursor-pointer py-2 text-sm pointer-coarse:min-h-11">
                      {copy.schema} ({entry.schema.length})
                    </summary>
                    <dl className="mt-2 space-y-2">
                      {entry.schema.map((column) => (
                        <div key={column.name} className="break-words text-sm">
                          <dt className="font-mono">
                            {column.name}{" "}
                            <span className="text-aui-muted">
                              {column.type} {column.mode}
                            </span>
                          </dt>
                          {column.description && (
                            <dd className="text-aui-muted">{column.description}</dd>
                          )}
                        </div>
                      ))}
                    </dl>
                  </details>
                )}
              </article>
            ))}
            {page.entries.length === 50 && <p className="text-sm text-aui-muted">{copy.more}</p>}
          </section>
        </div>
      )}
    </Page>
  );
}
