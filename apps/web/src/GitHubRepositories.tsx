import { defaultCopy } from "@humagen/components";
import { SearchIcon } from "@humagen/render-web";
import { toast } from "@humagen/render-web/ui/sonner";
import { type FormEvent, useCallback, useEffect, useState } from "react";
import type {
  GitHubRepoAutoReview,
  GitHubRepository,
  GitHubRepositoryInput,
  GitHubRepositories as Repositories,
} from "../../../packages/client/src/generated/types.ts";
import { api, type Client, call } from "./api.ts";
import { Tag } from "./cards.tsx";
import { DataTable } from "./DataTable.tsx";
import { href, type Route } from "./route.ts";
import { button, card, field, Label, Page, Problem, primary, SkeletonRows, Switch } from "./ui.tsx";

const words = defaultCopy.githubRepos;

/** A repository of a GitHub app, as owner and name path segments. */
function at(client: string, repo: string): string {
  const [owner = "", name = ""] = repo.split("/");
  return `/api/clients/${encodeURIComponent(client)}/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(name)}`;
}

/** The API this page reads and writes, kept with it so Home does not load it. */
const repos = {
  list: (client: string) =>
    call<Repositories>("GET", `/api/clients/${encodeURIComponent(client)}/repositories`),
  get: (client: string, repo: string) => call<GitHubRepository>("GET", at(client, repo)),
  save: (client: string, repo: string, input: GitHubRepositoryInput) =>
    call<GitHubRepository>("PUT", at(client, repo), input),
  reset: (client: string, repo: string, version: number) =>
    call<GitHubRepository>("DELETE", `${at(client, repo)}?version=${version}`),
};

type Review = "off" | "opened" | "pushes" | "both";

function reviewOf(a: GitHubRepoAutoReview): Review {
  return a.opened && a.pushes ? "both" : a.opened ? "opened" : a.pushes ? "pushes" : "off";
}

function autoReviewOf(r: Review): GitHubRepoAutoReview {
  return { opened: r === "opened" || r === "both", pushes: r === "pushes" || r === "both" };
}

const agentWords = (agent?: string) => (agent ? `@${agent}` : words.noAgent);

/**
 * A GitHub app's repositories under Apps: each one it covers, what applies
 * there, and, opened, that repository's own settings. A repository with no
 * settings follows the app, whose defaults a deployment may set from its
 * environment.
 */
export function GitHubRepositories({
  client,
  repo,
  canWrite,
  onNavigate,
}: {
  client: string;
  repo?: string;
  canWrite: boolean;
  onNavigate: (route: Route) => void;
}) {
  return repo ? (
    <RepositorySettings
      key={`${client}/${repo}`}
      client={client}
      repo={repo}
      canWrite={canWrite}
      onNavigate={onNavigate}
    />
  ) : (
    <RepositoryList client={client} onNavigate={onNavigate} />
  );
}

function RepositoryList({
  client,
  onNavigate,
}: {
  client: string;
  onNavigate: (route: Route) => void;
}) {
  const [data, setData] = useState<Repositories | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [name, setName] = useState<string | null>(null);
  const load = useCallback(() => {
    setProblem(null);
    setData(null);
    repos
      .list(client)
      .then(setData)
      .catch((e: unknown) => setProblem(e instanceof Error ? e.message : words.failed));
    // The app's own name heads the path, so two GitHub apps are told apart.
    api
      .clients()
      .then((r) => setName(r.clients.find((c) => c.id === client)?.name ?? null))
      .catch(() => setName(null));
  }, [client]);
  useEffect(load, [load]);
  const q = query.trim().toLowerCase();
  const rows = (data?.repositories ?? []).filter(
    (r) => !q || r.repository.toLowerCase().includes(q),
  );
  const open = (r: GitHubRepository): Route => ({
    page: "repositories",
    client,
    repo: r.repository,
  });
  return (
    <Page
      title={words.title}
      back={{
        label: name ?? words.back,
        onClick: () => onNavigate({ page: "connections", scope: "apps", provider: "github" }),
      }}
    >
      <div className="flex min-w-0 flex-col gap-4">
        {problem ? (
          <Problem
            message={problem}
            action={
              <button type="button" className={button} onClick={load}>
                {words.retry}
              </button>
            }
          />
        ) : data === null ? (
          <SkeletonRows count={5} height="2.75rem" />
        ) : data.repositories.length === 0 ? (
          <div className="flex flex-col items-start gap-4 py-10">
            <p className="humagen-display text-aui-lg font-medium text-aui-fg">{words.empty}</p>
            <button type="button" className={button} onClick={load}>
              {words.checkAgain}
            </button>
          </div>
        ) : (
          <>
            {!data.listed && <p className="text-aui-sm text-aui-muted">{words.notListed}</p>}
            {data.repositories.length > 8 && (
              <label className="flex h-9 min-w-0 max-w-md items-center gap-2 rounded-aui border border-aui-border bg-aui-bg px-3 text-aui-muted transition-colors focus-within:border-aui-accent pointer-coarse:h-11">
                <SearchIcon size={14} />
                <input
                  type="search"
                  aria-label={words.search}
                  placeholder={words.search}
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  className="min-w-0 flex-1 bg-transparent text-aui-sm text-aui-fg outline-none placeholder:text-aui-muted"
                />
              </label>
            )}
            {rows.length === 0 ? (
              <output className="block text-aui-sm text-aui-muted">{words.noMatch}</output>
            ) : (
              <DataTable
                label={words.title}
                rows={rows}
                rowKey={(r) => r.repository.toLowerCase()}
                columns={[
                  {
                    name: words.columns.repository,
                    cell: (r) => (
                      <span className="flex min-w-0 items-center gap-2">
                        <span className="truncate">{r.repository}</span>
                        {r.settings && (
                          <span className="hidden shrink-0 sm:inline-flex">
                            <Tag>{words.custom}</Tag>
                          </span>
                        )}
                      </span>
                    ),
                  },
                  {
                    name: words.columns.review,
                    // A table cell keeps to one line by default; this one
                    // wraps, so a phone row ends inside the page.
                    className: "max-sm:whitespace-normal",
                    cell: (r) => <span>{words.review[reviewOf(r.auto_review)]}</span>,
                  },
                  {
                    name: words.columns.agent,
                    hide: "sm",
                    cell: (r) => <span className="whitespace-nowrap">{agentWords(r.agent)}</span>,
                  },
                  {
                    name: words.columns.reads,
                    hide: "md",
                    cell: (r) => (
                      <span className="whitespace-nowrap">
                        {r.cross_surface_reads ? words.readable : words.ownRuns}
                      </span>
                    ),
                  },
                ]}
                link={(r) => ({ href: href(open(r)), onOpen: () => onNavigate(open(r)) })}
              />
            )}
            {data.truncated && <p className="text-aui-xs text-aui-muted">{words.truncated}</p>}
          </>
        )}
      </div>
    </Page>
  );
}

/** The agents a client may route to: every agent, or the ones it names. */
function routable(c: Client | null, all: string[]): string[] {
  if (!c) return [];
  if (c.agents?.includes("*")) return all;
  return [...new Set([c.default_agent ?? "", ...(c.agents ?? [])])].filter(Boolean);
}

function RepositorySettings({
  client,
  repo,
  canWrite,
  onNavigate,
}: {
  client: string;
  repo: string;
  canWrite: boolean;
  onNavigate: (route: Route) => void;
}) {
  const [view, setView] = useState<GitHubRepository | null>(null);
  const [defaults, setDefaults] = useState<Repositories["defaults"] | null>(null);
  const [app, setApp] = useState<Client | null>(null);
  const [agents, setAgents] = useState<string[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [conflict, setConflict] = useState(false);
  const [busy, setBusy] = useState(false);
  const [review, setReview] = useState<Review | "">("");
  const [agent, setAgent] = useState("");
  const [instructions, setInstructions] = useState("");
  const [reads, setReads] = useState<string[]>([]);
  const [crossSurface, setCrossSurface] = useState(true);
  const fill = (v: GitHubRepository) => {
    const s = v.settings;
    setView(v);
    setReview(s?.auto_review ? reviewOf(s.auto_review) : "");
    setAgent(s?.agent ?? "");
    setInstructions(s?.instructions ?? "");
    setReads(s?.reads ?? []);
    // This app's own setting; whether the workspace keeps it is kept_by.
    setCrossSurface(s?.cross_surface_reads !== false);
  };
  const load = useCallback(() => {
    setProblem(null);
    setConflict(false);
    setView(null);
    repos
      .get(client, repo)
      .then(fill)
      .catch((e: unknown) => setProblem(e instanceof Error ? e.message : words.failed));
    repos
      .list(client)
      .then((r) => setDefaults(r.defaults))
      .catch(() => setDefaults(null));
    api
      .clients()
      .then((r) => setApp(r.clients.find((c) => c.id === client) ?? null))
      .catch(() => setApp(null));
  }, [client, repo]);
  useEffect(load, [load]);
  useEffect(() => {
    if (!canWrite) return;
    let live = true;
    api
      .agents()
      .then((list) => live && setAgents(list.map((r) => r.spec.name)))
      .catch(() => live && setAgents([]));
    return () => {
      live = false;
    };
  }, [canWrite]);
  const list: Route = { page: "repositories", client };
  const act = async (fn: () => Promise<GitHubRepository>, done: string) => {
    if (busy) return;
    setBusy(true);
    setProblem(null);
    setConflict(false);
    try {
      fill(await fn());
      toast.success(done);
    } catch (e) {
      const message = e instanceof Error ? e.message : words.failed;
      if (/changed since/i.test(message)) setConflict(true);
      else setProblem(message);
    } finally {
      setBusy(false);
    }
  };
  const save = (event: FormEvent) => {
    event.preventDefault();
    if (!view) return;
    void act(
      () =>
        repos.save(client, repo, {
          version: view.version,
          auto_review: review ? autoReviewOf(review) : undefined,
          agent,
          instructions,
          reads: reads as ("pull_diff" | "linked_issues")[],
          cross_surface_reads: crossSurface,
          // Not shown here, so saved as it was read.
          approve: view.settings?.approve === "never" ? "never" : undefined,
        }),
      words.form.saved,
    );
  };
  const reset = () => {
    if (!view?.settings) return;
    void act(() => repos.reset(client, repo, view.version), words.form.wasReset);
  };
  const choices = routable(app, agents);
  // Other apps that keep this repository to its own runs: then it is kept in
  // the whole workspace, whatever this app's switch says.
  const others = (view?.kept_by ?? []).filter((name) => name !== app?.name);
  const appReview = defaults ? words.review[reviewOf(defaults.auto_review)] : "…";
  const appAgent = defaults ? agentWords(defaults.agent) : "…";
  const toggleRead = (read: string, on: boolean) =>
    setReads((prev) => (on ? [...new Set([...prev, read])] : prev.filter((r) => r !== read)));
  return (
    <Page
      title={repo}
      back={{
        label: app?.name ?? words.title,
        onClick: () => onNavigate(list),
      }}
    >
      {problem && !view ? (
        <Problem
          message={problem}
          action={
            <button type="button" className={button} onClick={load}>
              {words.retry}
            </button>
          }
        />
      ) : view === null ? (
        <SkeletonRows count={6} height="2.5rem" />
      ) : (
        <form aria-label={repo} onSubmit={save} className="flex min-w-0 max-w-2xl flex-col gap-5">
          {!canWrite && <p className="text-aui-sm text-aui-muted">{words.form.readOnly}</p>}
          <Problem message={problem} />
          {conflict && (
            <Problem
              message={words.form.conflict}
              action={
                <button type="button" className={button} onClick={load}>
                  {words.form.reload}
                </button>
              }
            />
          )}
          <fieldset disabled={!canWrite || busy} className="flex min-w-0 flex-col gap-5">
            <div className="grid min-w-0 gap-4 sm:grid-cols-2">
              <Label text={words.form.review}>
                <select
                  className={field}
                  value={review}
                  onChange={(e) => setReview(e.target.value as Review | "")}
                >
                  <option value="">{words.follow(appReview)}</option>
                  {(["off", "opened", "pushes", "both"] as const).map((r) => (
                    <option key={r} value={r}>
                      {words.review[r]}
                    </option>
                  ))}
                </select>
              </Label>
              <Label text={words.form.agent}>
                <select className={field} value={agent} onChange={(e) => setAgent(e.target.value)}>
                  <option value="">{words.follow(appAgent)}</option>
                  {agent && !choices.includes(agent) && <option value={agent}>@{agent}</option>}
                  {choices.map((a) => (
                    <option key={a} value={a}>
                      @{a}
                    </option>
                  ))}
                </select>
              </Label>
            </div>
            <Label text={words.form.instructions}>
              <textarea
                className={`${field} h-auto min-h-28 py-2 pointer-coarse:h-auto`}
                value={instructions}
                maxLength={4000}
                onChange={(e) => setInstructions(e.target.value)}
              />
            </Label>
            <div
              role="group"
              aria-labelledby="repo-reads"
              className={`${card} flex min-w-0 flex-col gap-3 [&>label]:items-center pointer-coarse:gap-1 pointer-coarse:pb-2`}
            >
              <span id="repo-reads" className="text-aui-xs font-medium text-aui-muted">
                {words.form.reads}
              </span>
              <Switch
                label={words.form.readDiff}
                checked={reads.includes("pull_diff")}
                onChange={(on) => toggleRead("pull_diff", on)}
              />
              <Switch
                label={words.form.readIssues}
                checked={reads.includes("linked_issues")}
                onChange={(on) => toggleRead("linked_issues", on)}
              />
            </div>
            <div className={card}>
              <Switch
                label={words.form.crossSurface}
                hint={others.length > 0 ? words.form.keptBy(others) : words.form.crossSurfaceHint}
                checked={crossSurface}
                onChange={setCrossSurface}
              />
            </div>
          </fieldset>
          {canWrite && (
            <div className="flex flex-wrap justify-end gap-2">
              {view.settings && (
                <button type="button" className={button} disabled={busy} onClick={reset}>
                  {words.form.reset}
                </button>
              )}
              <button type="submit" className={primary} disabled={busy}>
                {busy ? words.form.saving : words.form.save}
              </button>
            </div>
          )}
        </form>
      )}
    </Page>
  );
}
