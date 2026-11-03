import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import type { ReviewedPull } from "../../../packages/client/src/generated/types.ts";
import { api } from "./api.ts";
import { age } from "./conversationList.ts";
import { href, type Route } from "./route.ts";
import { AgentMark, button, field, Page, Problem } from "./ui.tsx";

const words = defaultCopy.pullRequests;
type PullRoute = Extract<Route, { page: "pull-requests" }>;
const columns = [
  { id: "running", title: words.running },
  { id: "waiting", title: words.waiting },
  { id: "done", title: words.done },
] as const;
export function pullConversation(item: ReviewedPull): Route {
  const ref = `pull:${item.repo}#${item.number}`;
  return { page: "conversation", id: item.thread, panel: "workspace", tabs: [ref], active: ref };
}
export function PullRequests({
  route,
  go,
}: {
  route: PullRoute;
  go: (r: Route, opts?: { replace?: boolean }) => void;
}) {
  const [pulls, setPulls] = useState<ReviewedPull[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const query = new URLSearchParams();
  for (const key of ["q", "repo", "agent", "status"] as const)
    if (route[key]) query.set(key, route[key]);
  const search = query.toString();
  useEffect(() => {
    let active = true;
    setLoading(true);
    setError("");
    api
      .reviewedPulls(search)
      .then((r) => {
        if (active) setPulls(r.pulls);
      })
      .catch((e) => {
        if (active) setError(String(e));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [search]);
  const change = (patch: Partial<PullRoute>) => go({ ...route, ...patch }, { replace: true });
  const card = (p: ReviewedPull) => (
    <a
      key={p.run}
      href={href(pullConversation(p))}
      onClick={(e) => {
        e.preventDefault();
        go(pullConversation(p));
      }}
      className="block rounded-aui border border-aui-border bg-aui-surface p-4 hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
    >
      <div className="text-aui-xs text-aui-muted">
        {p.repo} #{p.number}
      </div>
      <h3 className="my-2 line-clamp-3 font-medium">{p.title}</h3>
      <div className="flex items-center gap-2 text-aui-sm">
        <AgentMark name={p.agent} size={20} />
        {p.agent}
        <time className="ml-auto text-aui-xs text-aui-muted" dateTime={p.created_at}>
          {age(p.created_at, Date.now())}
        </time>
      </div>
      <div className="mt-3 flex flex-wrap gap-3 text-aui-xs">
        <span>{words.counts(p.errors, p.warnings, p.infos)}</span>
        <span className="text-aui-muted">{words.ci(p.ci)}</span>
      </div>
    </a>
  );
  return (
    <Page title={words.title}>
      <p className="mb-5 text-aui-sm text-aui-muted">{words.intro}</p>
      <div className="mb-6 flex flex-wrap items-end gap-3">
        <label className="min-w-48 flex-1 text-aui-sm">
          {words.search}
          <input
            className={`${field} mt-1 w-full`}
            value={route.q ?? ""}
            onChange={(e) => change({ q: e.target.value || undefined })}
            placeholder={words.searchHint}
          />
        </label>
        <label className="text-aui-sm">
          {words.status}
          <select
            className={`${field} mt-1 block`}
            value={route.status ?? ""}
            onChange={(e) =>
              change({ status: (e.target.value as PullRoute["status"]) || undefined })
            }
          >
            <option value="">{words.all}</option>
            {columns.map((c) => (
              <option key={c.id} value={c.id}>
                {c.title}
              </option>
            ))}
          </select>
        </label>
        <label className="text-aui-sm">
          {words.repository}
          <input
            className={`${field} mt-1 block`}
            value={route.repo ?? ""}
            onChange={(e) => change({ repo: e.target.value || undefined })}
            placeholder={words.repoHint}
          />
        </label>
        <label className="text-aui-sm">
          {words.agent}
          <input
            className={`${field} mt-1 block`}
            value={route.agent ?? ""}
            onChange={(e) => change({ agent: e.target.value || undefined })}
          />
        </label>
        <button
          type="button"
          aria-pressed={!route.display}
          onClick={() => change({ display: undefined })}
          className={button}
        >
          {words.board}
        </button>
        <button
          type="button"
          aria-pressed={route.display === "list"}
          onClick={() => change({ display: "list" })}
          className={button}
        >
          {words.list}
        </button>
      </div>
      {error && <Problem message={error} />}
      {loading ? (
        <p role="status">{words.loading}</p>
      ) : pulls.length === 0 ? (
        <p role="status">{words.empty}</p>
      ) : route.display === "list" ? (
        <div className="space-y-3">{pulls.map(card)}</div>
      ) : (
        <div className="grid gap-5 lg:grid-cols-3">
          {columns.map((c) => (
            <section key={c.id} aria-label={c.title}>
              <h2 className="mb-3 text-aui-sm font-medium">
                {c.title}{" "}
                <span className="text-aui-muted">
                  {pulls.filter((p) => p.status === c.id).length}
                </span>
              </h2>
              <div className="space-y-3">{pulls.filter((p) => p.status === c.id).map(card)}</div>
            </section>
          ))}
        </div>
      )}
    </Page>
  );
}
