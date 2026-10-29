import { defaultCopy } from "@humagen/components";
import { DatabaseIcon, SearchIcon } from "@humagen/render-web";
import { Button, buttonVariants } from "@humagen/render-web/ui/button";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@humagen/render-web/ui/empty";
import { Input } from "@humagen/render-web/ui/input";
import { useCallback, useEffect, useState } from "react";
import { api, type Source } from "./api.ts";
import { href } from "./route.ts";
import { SourceLifecycle, SourceSyncSettings } from "./SourceLifecycle.tsx";
import { ago, Chip, Page, Problem, SkeletonRows } from "./ui.tsx";

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
        <Button variant="outline" onClick={load}>
          {words.refresh}
        </Button>
      }
    >
      <Problem message={problem} />
      {!sources && !problem && <SkeletonRows count={4} />}
      {sources?.length === 0 ? (
        <Empty className="min-h-80 border border-border bg-card/30">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <DatabaseIcon />
            </EmptyMedia>
            <EmptyTitle>{words.healthEmptyTitle}</EmptyTitle>
            <EmptyDescription>{words.healthEmptyHint}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <a
              className={buttonVariants({ variant: canWrite ? "default" : "outline" })}
              href={href({ page: "library", section: "sources" })}
            >
              {canWrite ? words.addSources : words.browseSources}
            </a>
          </EmptyContent>
        </Empty>
      ) : (
        sources && (
          <>
            <div role="group" className="flex flex-wrap gap-2" aria-label={words.filter}>
              <Button
                variant={filter === "" ? "secondary" : "ghost"}
                size="sm"
                aria-pressed={filter === ""}
                onClick={() => setFilter("")}
              >
                {words.all}
                <span className="tabular-nums text-muted-foreground">{sources.length}</span>
              </Button>
              {groups.map((group) => (
                <Button
                  key={group.value}
                  variant={filter === group.value ? "secondary" : "ghost"}
                  size="sm"
                  aria-pressed={filter === group.value}
                  onClick={() => setFilter(filter === group.value ? "" : group.value)}
                >
                  {group.label}
                  <span className="tabular-nums text-muted-foreground">
                    {sources.filter(group.keep).length}
                  </span>
                </Button>
              ))}
            </div>
            <div className="min-w-0 max-w-md">
              <label className="flex min-w-0 flex-col gap-2 text-sm font-medium">
                {words.search}
                <span className="relative">
                  <SearchIcon className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    className="pl-9"
                    type="search"
                    value={query}
                    maxLength={200}
                    onChange={(event) => setQuery(event.target.value)}
                  />
                </span>
              </label>
            </div>
            {selected && !current && <Problem message={words.selectedMissing} />}
            <div
              className={`grid min-w-0 grid-cols-1 items-start gap-6 ${current ? "@4xl:grid-cols-[minmax(0,1fr)_minmax(20rem,1fr)]" : ""}`}
            >
              <section className="min-w-0" aria-label={words.sources}>
                {rows?.length === 0 ? (
                  <p className="py-8 text-sm text-muted-foreground">{words.healthNoMatches}</p>
                ) : (
                  <ul className="divide-y divide-border border-y border-border">
                    {rows?.map((source) => (
                      <li key={source.name}>
                        <button
                          type="button"
                          aria-pressed={current?.name === source.name}
                          onClick={() => pick(source.name)}
                          className="flex min-h-24 w-full min-w-0 items-start gap-3 rounded-md px-3 py-4 text-left hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring aria-pressed:bg-accent"
                        >
                          <DatabaseIcon className="mt-1" />
                          <span className="flex min-w-0 flex-1 flex-col gap-2">
                            <span className="break-words text-sm font-medium">{source.name}</span>
                            <span className="break-words text-xs text-muted-foreground">
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
                            <span className="text-xs text-muted-foreground">
                              {words.lastGood}:{" "}
                              {source.lifecycle?.last_good_at
                                ? ago(source.lifecycle.last_good_at)
                                : words.never}
                            </span>
                          </span>
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </section>
              {current && (
                <section className="flex min-w-0 flex-col gap-6" aria-label={words.details}>
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <h2 className="break-words text-base font-semibold">{current.name}</h2>
                    {onOpenSource && (
                      <Button variant="outline" onClick={() => onOpenSource(current.name)}>
                        {words.sourceDetail}
                      </Button>
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
              )}
            </div>
          </>
        )
      )}
    </Page>
  );
}
