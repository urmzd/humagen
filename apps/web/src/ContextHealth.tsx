import { defaultCopy } from "@humagen/components";
import { useCallback, useEffect, useState } from "react";
import { api, type Source } from "./api.ts";
import { SourceLifecycle, SourceSyncSettings } from "./SourceLifecycle.tsx";
import { ago, button, Chip, field, Label, Page, Problem } from "./ui.tsx";

const words = defaultCopy.sourceOperations;
const lifecycleWords = defaultCopy.sourceLifecycle;
function snapshot(source: Source) {
  return !!source.lifecycle?.snapshot || (!source.lifecycle && source.documents > 0);
}

export function ContextHealth({
  canWrite,
  selected,
  onSelect,
  onOpenSource,
}: {
  canWrite: boolean;
  selected?: string;
  onSelect?: (name: string | undefined) => void;
  onOpenSource?: (name: string) => void;
  onOpenAgent?: (name: string) => void;
  onOpenProject?: (name: string) => void;
}) {
  const [sources, setSources] = useState<Source[] | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState("");
  const [local, setLocal] = useState<string | undefined>(selected);
  const load = useCallback(() => {
    setProblem(null);
    api
      .sources()
      .then(setSources)
      .catch((error) => setProblem(error instanceof Error ? error.message : words.readFailed));
  }, []);
  useEffect(load, [load]);
  useEffect(() => setLocal(selected), [selected]);
  const pick = (name: string | undefined) => {
    setLocal(name);
    onSelect?.(name);
  };
  const current = sources?.find((source) => source.name === (selected ?? local));
  const groups = [
    {
      value: "importing",
      label: words.importing,
      keep: (s: Source) => s.lifecycle?.status === "importing",
    },
    {
      value: "indexing",
      label: words.indexing,
      keep: (s: Source) => s.lifecycle?.status === "indexing",
    },
    {
      value: "in_sync",
      label: words.live,
      keep: (s: Source) =>
        !snapshot(s) && (s.lifecycle?.status === "available" || s.lifecycle?.status === "in_sync"),
    },
    { value: "snapshot", label: words.snapshots, keep: snapshot },
    {
      value: "needs_attention",
      label: words.attention,
      keep: (s: Source) => s.lifecycle?.status === "needs_attention",
    },
  ];
  const rows = sources?.filter(
    (source) =>
      (!query ||
        `${source.name} ${source.description ?? ""} ${source.lifecycle?.origin ?? ""}`
          .toLowerCase()
          .includes(query.toLowerCase())) &&
      (!filter || groups.find((group) => group.value === filter)?.keep(source)),
  );
  return (
    <Page
      title={words.health}
      lede={words.healthHint}
      actions={
        <button type="button" className={button} onClick={load}>
          {words.refresh}
        </button>
      }
    >
      <Problem message={problem} />
      <p className="text-aui-xs text-aui-muted">{words.healthMissing}</p>
      {!sources && !problem && <output>{words.loading}</output>}
      {sources && (
        <>
          <dl className="flex flex-wrap gap-5">
            {groups.map((group) => (
              <div key={group.value}>
                <dt className="text-aui-xs text-aui-muted">{group.label}</dt>
                <dd className="text-aui-xl tabular-nums">{sources.filter(group.keep).length}</dd>
              </div>
            ))}
          </dl>
          <div className="mt-5 grid max-w-2xl grid-cols-1 gap-3 sm:grid-cols-2">
            <Label text={words.search}>
              <input
                className={field}
                type="search"
                value={query}
                maxLength={200}
                onChange={(e) => setQuery(e.target.value)}
              />
            </Label>
            <Label text={words.filter}>
              <select
                aria-label={words.filter}
                className={field}
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
              >
                <option value="">{words.all}</option>
                {groups.map((group) => (
                  <option key={group.value} value={group.value}>
                    {group.label}
                  </option>
                ))}
              </select>
            </Label>
          </div>
          <div className="mt-5 grid min-w-0 grid-cols-1 items-start gap-6 @4xl:grid-cols-[minmax(0,1fr)_minmax(20rem,1fr)]">
            <section className="min-w-0" aria-label={words.sources}>
              {rows?.length === 0 && <p className="text-aui-sm text-aui-muted">{words.empty}</p>}
              <ul className="divide-y divide-aui-border">
                {rows?.map((source) => (
                  <li key={source.name}>
                    <button
                      type="button"
                      aria-pressed={current?.name === source.name}
                      className="flex min-h-16 w-full min-w-0 flex-col gap-2 rounded-aui px-2 py-4 text-left hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                      onClick={() => pick(source.name)}
                    >
                      <span className="break-words text-aui-sm font-medium">{source.name}</span>
                      <span className="break-words text-aui-xs text-aui-muted">
                        {source.lifecycle?.provider ?? lifecycleWords.providers.manual}
                        {source.lifecycle?.origin ? `: ${source.lifecycle.origin}` : ""}
                      </span>
                      <span className="flex flex-wrap gap-2">
                        <Chip>
                          {snapshot(source)
                            ? words.snapshot
                            : lifecycleWords.modes[source.lifecycle?.mode ?? "indexed"]}
                        </Chip>
                        <Chip>
                          {source.lifecycle
                            ? lifecycleWords.status[source.lifecycle.status]
                            : source.documents
                              ? lifecycleWords.status.available
                              : lifecycleWords.status.connected}
                        </Chip>
                      </span>
                      <span className="text-aui-xs text-aui-muted">
                        {words.lastGood}:{" "}
                        {source.lifecycle?.last_good_at
                          ? ago(source.lifecycle.last_good_at)
                          : words.never}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            </section>
            {current ? (
              <section className="flex min-w-0 flex-col gap-6" aria-label={words.details}>
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <h2 className="break-words text-aui-lg font-semibold">{current.name}</h2>
                  {onOpenSource && (
                    <button
                      className={button}
                      type="button"
                      onClick={() => onOpenSource(current.name)}
                    >
                      {words.sourceDetail}
                    </button>
                  )}
                </div>
                <SourceLifecycle
                  key={`${current.name}:${current.updated_at}`}
                  source={current}
                  canWrite={canWrite}
                  onChanged={load}
                />
                <SourceSyncSettings
                  key={`${current.name}:${current.updated_at}:sync`}
                  source={current}
                  canWrite={canWrite}
                  onChanged={load}
                />
              </section>
            ) : (
              <p className="text-aui-sm text-aui-muted">{words.noSelection}</p>
            )}
          </div>
        </>
      )}
    </Page>
  );
}
