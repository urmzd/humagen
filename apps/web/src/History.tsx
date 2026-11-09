import { defaultCopy } from "@humagen/components";
import {
  ClockIcon,
  CodeIcon,
  ListChecksIcon,
  MessageIcon,
  MessageSquareIcon,
} from "@humagen/render-web";
import { type ReactNode, useEffect, useMemo, useState } from "react";
import type { ReviewedPull } from "../../../packages/client/src/generated/types.ts";
import { api, type ThreadSummary } from "./api.ts";
import { age } from "./conversationList.ts";
import { daysAgo } from "./format.ts";
import { type HistorySource, type HistoryStatus, href, type Route } from "./route.ts";
import {
  AgentMark,
  button,
  Empty,
  Page,
  Problem,
  rows,
  SearchField,
  Segmented,
  SkeletonRows,
} from "./ui.tsx";

const words = defaultCopy.history;
type HistoryRoute = Extract<Route, { page: "history" }>;

/** Which run origins each source covers. A conversation's origin is where its first run came from. */
const ORIGINS: Record<Exclude<HistorySource, "all">, string[]> = {
  chat: ["api"],
  github: ["github"],
  slack: ["slack"],
  scheduled: ["cron", "manual"],
  assignments: ["assignment"],
};

/** One glyph per source, each different, so a row says where it came from at a glance. */
const ICONS: Record<Exclude<HistorySource, "all">, ReactNode> = {
  chat: <MessageIcon size={15} />,
  github: <CodeIcon size={15} />,
  slack: <MessageSquareIcon size={15} />,
  scheduled: <ClockIcon size={15} />,
  assignments: <ListChecksIcon size={15} />,
};

export const HISTORY_SOURCES = [
  "all",
  "chat",
  "github",
  "slack",
  "scheduled",
  "assignments",
] as const;

/** The source a conversation belongs to, from its origin. */
export function sourceOf(origin: string): Exclude<HistorySource, "all"> {
  for (const [source, origins] of Object.entries(ORIGINS))
    if (origins.includes(origin)) return source as Exclude<HistorySource, "all">;
  return "chat";
}

/** A pull request review opens on its conversation with the pull request's tab in front. */
export function pullConversation(item: ReviewedPull): Route {
  const ref = `pull:${item.repo}#${item.number}`;
  return { page: "conversation", id: item.thread, panel: "workspace", tabs: [ref], active: ref };
}

/** Whether a conversation matches the filters the server cannot apply. */
export function keeps(
  t: ThreadSummary,
  route: Pick<HistoryRoute, "q" | "source" | "status">,
  pull?: ReviewedPull,
): boolean {
  if (route.source && route.source !== "all" && sourceOf(t.origin) !== route.source) return false;
  if (route.status === "needs" && !(t.waiting ?? 0)) return false;
  if (route.status === "running" && t.active === 0) return false;
  if (route.status === "failed" && t.failed === 0) return false;
  const q = route.q?.trim().toLowerCase();
  if (!q) return true;
  const hay = [t.title, t.last, ...t.agents, pull && `${pull.repo} #${pull.number}`]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
  return hay.includes(q);
}

/** Today, Yesterday, This week, or the month, for the heading a row sits under. */
export function dayOf(iso: string, now: Date): string {
  const d = new Date(iso);
  const days = daysAgo(iso, now.getTime());
  if (days <= 0) return words.today;
  if (days === 1) return words.yesterday;
  if (days < 7) return words.thisWeek;
  return d.toLocaleDateString(undefined, { month: "long", year: "numeric" });
}

const PAGE = 50;

export function History({
  route,
  go,
}: {
  route: HistoryRoute;
  go: (r: Route, opts?: { replace?: boolean }) => void;
}) {
  const [threads, setThreads] = useState<ThreadSummary[]>([]);
  const [next, setNext] = useState<string | null>(null);
  const [pulls, setPulls] = useState<Map<string, ReviewedPull>>(new Map());
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [more, setMore] = useState(false);
  const source = route.source ?? "all";
  // One origin is filtered by the server; a source of several is filtered here.
  const origin = source !== "all" && ORIGINS[source].length === 1 ? ORIGINS[source][0] : undefined;
  const standing =
    route.status === "running" || route.status === "failed" ? route.status : undefined;

  useEffect(() => {
    let live = true;
    setLoading(true);
    setError("");
    api
      .threadsPage({ limit: PAGE, origin, standing })
      .then((r) => {
        if (!live) return;
        setThreads(r.threads);
        setNext(r.next ?? null);
      })
      .catch((e) => live && setError(String(e)))
      .finally(() => live && setLoading(false));
    return () => {
      live = false;
    };
  }, [origin, standing]);

  // Reviews add what a pull request row shows: its repository, number and findings.
  useEffect(() => {
    if (source !== "all" && source !== "github") return;
    let live = true;
    api
      .reviewedPulls("")
      .then((r) => {
        if (live) setPulls(new Map(r.pulls.map((p) => [p.thread, p])));
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [source]);

  const loadMore = () => {
    if (!next) return;
    setMore(true);
    api
      .threadsPage({ limit: PAGE, before: next, origin, standing })
      .then((r) => {
        setThreads((have) => [...have, ...r.threads]);
        setNext(r.next ?? null);
      })
      .catch((e) => setError(String(e)))
      .finally(() => setMore(false));
  };

  const change = (patch: Partial<HistoryRoute>) => go({ ...route, ...patch }, { replace: true });
  const shown = useMemo(
    () => threads.filter((t) => keeps(t, route, pulls.get(t.id))),
    [threads, route, pulls],
  );
  const groups = useMemo(() => {
    const now = new Date();
    const out: { day: string; items: ThreadSummary[] }[] = [];
    for (const t of shown) {
      const day = dayOf(t.updated_at, now);
      const last = out.at(-1);
      if (last?.day === day) last.items.push(t);
      else out.push({ day, items: [t] });
    }
    return out;
  }, [shown]);

  const row = (t: ThreadSummary) => {
    const pull = pulls.get(t.id);
    const to: Route = pull ? pullConversation(pull) : { page: "conversation", id: t.id };
    const kind = sourceOf(t.origin);
    const dot =
      (t.waiting ?? 0) > 0
        ? { label: words.needs, tone: "bg-aui-warn" }
        : t.active > 0
          ? { label: words.running, tone: "bg-aui-accent" }
          : t.failed > 0
            ? { label: words.failed, tone: "bg-aui-danger" }
            : null;
    return (
      <li key={t.id}>
        <a
          href={href(to)}
          onClick={(e) => {
            if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
            e.preventDefault();
            go(to);
          }}
          className="group flex items-start gap-3 px-1 py-3 transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent motion-reduce:transition-none"
        >
          <span
            role="img"
            className="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-aui border border-[var(--humagen-rule)] text-aui-muted"
            aria-label={words.sources[kind]}
            title={words.sources[kind]}
          >
            {ICONS[kind]}
          </span>
          <span className="min-w-0 flex-1">
            <span className="flex items-baseline gap-2">
              <span className="truncate font-medium text-aui-fg">{t.title || words.untitled}</span>
              {pull && (
                <span className="shrink-0 text-aui-xs text-aui-muted">
                  {pull.repo} #{pull.number}
                </span>
              )}
            </span>
            {(t.last || pull) && (
              <span className="mt-0.5 block truncate text-aui-sm text-aui-muted">
                {pull
                  ? defaultCopy.pullRequests.counts(pull.errors, pull.warnings, pull.infos)
                  : t.last}
              </span>
            )}
          </span>
          <span className="flex shrink-0 items-center gap-2 pt-0.5">
            {dot && (
              <span className="flex items-center gap-1.5 text-aui-xs text-aui-muted">
                <span aria-hidden className={`size-1.5 rounded-full ${dot.tone}`} />
                {dot.label}
              </span>
            )}
            <span className="flex -space-x-1">
              {t.agents.slice(0, 3).map((a) => (
                <AgentMark key={a} name={a} size={20} />
              ))}
            </span>
            <time className="w-8 text-right text-aui-xs text-aui-muted" dateTime={t.updated_at}>
              {age(t.updated_at, Date.now())}
            </time>
          </span>
        </a>
      </li>
    );
  };

  return (
    <Page title={words.title} lede={words.lede}>
      <div className="mb-5 flex flex-col gap-3 sm:flex-row sm:items-center">
        <SearchField
          label={words.search}
          value={route.q ?? ""}
          onChange={(q) => change({ q: q || undefined })}
          placeholder={words.searchHint}
          className="sm:w-72"
        />
        <Segmented<HistorySource>
          label={words.source}
          value={source}
          options={HISTORY_SOURCES.map((id) => ({ id, label: words.sources[id] }))}
          onChange={(s) => change({ source: s === "all" ? undefined : s })}
        />
        <Segmented<HistoryStatus | "any">
          label={words.status}
          value={route.status ?? "any"}
          options={[
            { id: "any", label: words.any },
            { id: "needs", label: words.needs },
            { id: "running", label: words.running },
            { id: "failed", label: words.failed },
          ]}
          onChange={(s) => change({ status: s === "any" ? undefined : s })}
        />
      </div>
      <Problem message={error || null} />
      {loading ? (
        <SkeletonRows count={6} height="3.25rem" />
      ) : shown.length === 0 ? (
        <Empty title={words.empty}>{words.emptyHint}</Empty>
      ) : (
        <div className="flex flex-col gap-6">
          {groups.map((g) => (
            <section key={g.day} aria-label={g.day}>
              <h2 className="mb-1 text-aui-xs font-medium text-aui-muted">{g.day}</h2>
              <ul className={rows}>{g.items.map(row)}</ul>
            </section>
          ))}
          {next && (
            <button
              type="button"
              className={`${button} self-start`}
              disabled={more}
              onClick={loadMore}
            >
              {more ? words.loading : words.more}
            </button>
          )}
        </div>
      )}
    </Page>
  );
}
