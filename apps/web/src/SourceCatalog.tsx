import { defaultCopy } from "@humagen/components";
import {
  ChevronRightIcon,
  DatabaseIcon,
  FileIcon,
  GoogleDriveIcon,
  PlugIcon,
  SlackIcon,
} from "@humagen/render-web";
import { Badge } from "@humagen/render-web/ui/badge";
import { useCallback, useEffect, useState } from "react";
import { ApiError, api, type CatalogConnectorView, type CatalogEntry } from "./api.ts";
import { ago, button, Chip, field, Problem } from "./ui.tsx";

export type SourceChoice = "manual" | "drive" | "notion" | "gcs" | "s3";

/**
 * The catalog of connected systems from `/api/catalog`: connectors, their
 * tables and columns, never data. An owner crawls, publishes and withdraws
 * their own connector. It renders nothing while no connector is visible.
 */
export function CatalogBrowser() {
  const words = defaultCopy.sourceCatalog.catalog;
  const [connectors, setConnectors] = useState<CatalogConnectorView[]>([]);
  const [entries, setEntries] = useState<CatalogEntry[]>([]);
  const [q, setQ] = useState("");
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);

  const load = useCallback((query: string) => {
    api.catalog(query.trim() || undefined).then(
      (page) => {
        setConnectors(page.connectors ?? []);
        setEntries(page.entries ?? []);
      },
      () => {},
    );
  }, []);

  useEffect(() => {
    load(q);
  }, [load, q]);

  const act = async (key: string, fn: () => Promise<unknown>) => {
    setBusy(key);
    setProblem(null);
    try {
      await fn();
      load(q);
    } catch (e) {
      setProblem(e instanceof ApiError ? e.message : words.failed);
    } finally {
      setBusy(null);
    }
  };

  if (connectors.length === 0 && entries.length === 0 && !q.trim()) return null;

  return (
    <section className="space-y-3 border-t border-aui-border pt-4" aria-label={words.label}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-medium">{words.title}</h3>
        <input
          type="search"
          aria-label={words.search}
          placeholder={words.searchPlaceholder}
          className={`${field} max-w-xs`}
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
      </div>
      <Problem message={problem} />
      {connectors.length > 0 && (
        <ul className="space-y-2">
          {connectors.map((c) => {
            const published = !!c.published && !c.withdrawn;
            return (
              <li
                key={c.name}
                className="flex flex-wrap items-center justify-between gap-2 rounded-aui border border-aui-border p-3 text-aui-sm"
              >
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-medium text-aui-fg">{c.name}</span>
                    <Chip>{c.system}</Chip>
                    <Chip>{published ? words.published : words.private}</Chip>
                    {c.project && <span className="text-aui-xs text-aui-muted">{c.project}</span>}
                  </div>
                  <p className="mt-0.5 flex flex-wrap gap-x-3 text-aui-xs text-aui-muted">
                    <span>{words.entries(c.entries)}</span>
                    {c.crawled_at && <span>{words.crawled(ago(c.crawled_at))}</span>}
                    {c.last_error && <span>{c.last_error}</span>}
                  </p>
                </div>
                {c.mine && (
                  <div className="flex flex-wrap gap-1.5">
                    <button
                      type="button"
                      className={button}
                      disabled={busy !== null}
                      onClick={() =>
                        void act(`crawl:${c.name}`, () => api.crawlCatalogConnector(c.name))
                      }
                    >
                      {busy === `crawl:${c.name}` ? words.crawling : words.crawl}
                    </button>
                    <button
                      type="button"
                      className={button}
                      disabled={busy !== null}
                      onClick={() =>
                        void act(`publish:${c.name}`, () =>
                          published
                            ? api.withdrawCatalogConnector(c.name)
                            : api.publishCatalogConnector(c.name),
                        )
                      }
                    >
                      {published ? words.withdraw : words.publish}
                    </button>
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}
      {entries.length > 0 && (
        <ul className="divide-y divide-aui-border rounded-aui border border-aui-border">
          {entries.map((e) => {
            const key = `${e.connector}:${e.path}`;
            const columns = e.schema ?? [];
            return (
              <li key={key} className="p-3 text-aui-sm">
                <button
                  type="button"
                  className="flex w-full items-start justify-between gap-2 text-left"
                  aria-expanded={columns.length > 0 ? !!open[key] : undefined}
                  onClick={() => setOpen((prev) => ({ ...prev, [key]: !prev[key] }))}
                >
                  <span className="min-w-0">
                    <span className="font-aui-mono text-aui-xs font-medium text-aui-fg">
                      {e.title || e.path}
                    </span>
                    {e.description && (
                      <span className="mt-0.5 block text-aui-xs text-aui-muted">
                        {e.description}
                      </span>
                    )}
                  </span>
                  <span className="flex shrink-0 gap-2 text-aui-2xs text-aui-muted">
                    <span>{e.connector}</span>
                    <span>{e.kind}</span>
                  </span>
                </button>
                {open[key] && columns.length > 0 && (
                  <dl
                    aria-label={words.columns(e.path)}
                    className="mt-2 grid grid-cols-[auto_auto_1fr] gap-x-3 gap-y-1 border-t border-aui-border pt-2 text-aui-xs"
                  >
                    {columns.map((col) => (
                      <div key={col.name} className="contents">
                        <dt className="font-aui-mono text-aui-fg">{col.name}</dt>
                        <dd className="font-aui-mono text-aui-muted">{col.type ?? ""}</dd>
                        <dd className="text-aui-muted">{col.description ?? ""}</dd>
                      </div>
                    ))}
                  </dl>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

/** Display only native creation paths as available; tool access is a separate capability. */
export function SourceCatalog({ onSelect }: { onSelect: (provider: SourceChoice) => void }) {
  const copy = defaultCopy.sourceCatalog;
  return (
    <div className="space-y-6">
      <section className="space-y-2" aria-label={copy.available}>
        <h3 className="text-sm font-medium">{copy.available}</h3>
        {(
          [
            {
              id: "manual",
              title: copy.manual,
              description: copy.manualHint,
              icon: <FileIcon size={20} />,
            },
            {
              id: "drive",
              title: copy.drive,
              description: copy.driveHint,
              icon: <GoogleDriveIcon size={20} className="text-[#4285F4]" />,
            },
            {
              id: "notion",
              title: copy.notion,
              description: copy.notionHint,
              icon: <FileIcon size={20} />,
            },
          ] as const
        ).map((provider) => (
          <button
            key={provider.id}
            type="button"
            onClick={() => onSelect(provider.id)}
            className="flex min-h-20 w-full items-start gap-3 rounded-aui-lg border border-aui-border p-4 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent motion-reduce:transition-none"
          >
            <span className="mt-1 shrink-0 text-aui-muted">{provider.icon}</span>
            <span className="min-w-0 flex-1">
              <span className="block text-sm font-medium">{provider.title}</span>
              <span className="mt-1 block text-sm leading-6 text-aui-muted">
                {provider.description}
              </span>
            </span>
            <ChevronRightIcon size={16} className="mt-1 shrink-0 text-aui-muted" />
          </button>
        ))}
      </section>
      <section className="space-y-2" aria-label={copy.collectAndCatalog}>
        <h3 className="text-sm font-medium">{copy.collectAndCatalog}</h3>
        {(
          [
            { id: "gcs", title: "Google Cloud Storage" },
            { id: "s3", title: "Amazon S3" },
          ] as const
        ).map((provider) => (
          <button
            type="button"
            key={provider.id}
            onClick={() => onSelect(provider.id)}
            className="flex min-h-20 w-full items-start gap-3 rounded-aui-lg border border-aui-border p-4 text-left hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            <DatabaseIcon size={20} />
            <span>
              <span className="block text-sm font-medium">{provider.title}</span>
              <span className="mt-1 block text-sm text-aui-muted">{copy.storageHint}</span>
            </span>
          </button>
        ))}
      </section>
      <section
        className="space-y-3 border-t border-aui-border pt-4"
        aria-label={copy.moreProviders}
      >
        <h3 className="text-sm font-medium">{copy.catalogAndLive}</h3>
        {["Snowflake", "BigQuery"].map((provider) => (
          <div key={provider} className="flex items-start gap-3">
            <DatabaseIcon size={18} className="mt-1 shrink-0 text-aui-muted" />
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <h3 className="text-sm font-medium">{provider}</h3>
                <Badge variant="outline">{copy.connectorRequired}</Badge>
              </div>
              <p className="mt-1 text-sm leading-6 text-aui-muted">{copy.warehouseHint}</p>
            </div>
          </div>
        ))}
        <div className="flex items-start gap-3">
          <SlackIcon size={18} className="mt-1 shrink-0" />
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="text-sm font-medium">Slack</h3>
              <Badge variant="outline">{copy.connectorRequired}</Badge>
            </div>
            <p className="mt-1 text-sm leading-6 text-aui-muted">{copy.slackHint}</p>
          </div>
        </div>
        <div className="flex items-start gap-3">
          <PlugIcon size={18} className="mt-1 shrink-0 text-aui-muted" />
          <div>
            <h3 className="text-sm font-medium">{copy.mcp}</h3>
            <p className="mt-1 text-sm leading-6 text-aui-muted">{copy.mcpHint}</p>
          </div>
        </div>
      </section>
      <section className="space-y-2 border-t border-aui-border pt-4">
        <h3 className="text-sm font-medium">{copy.refreshTitle}</h3>
        <p className="text-sm leading-6 text-aui-muted">{copy.refreshHint}</p>
        <p className="text-sm leading-6 text-aui-muted">{copy.formatsHint}</p>
      </section>
    </div>
  );
}

/** The catalog's intro, held under the inspector header while the list scrolls. */
export function SourceCatalogLead() {
  return (
    <p className="text-sm leading-6 text-aui-muted">{defaultCopy.sourceCatalog.description}</p>
  );
}
