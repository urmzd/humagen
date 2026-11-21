import { defaultCopy } from "@humagen/components";
import type { ResponseState } from "@humagen/core";
import {
  ChevronDownIcon,
  ChevronRightIcon,
  DiffIcon,
  ExternalLinkIcon,
  FolderIcon,
  GitHubIcon,
  MonitorIcon,
  PlugIcon,
  ServerIcon,
} from "@humagen/render-web";
import { type ReactNode, useEffect, useId, useState } from "react";
import type { Run } from "./adapter.ts";
import { api, type ToolCredential } from "./api.ts";
import type { PaneTab } from "./pane.ts";
import { href, type WorkbenchTabId } from "./route.ts";
import {
  type ContextUse,
  contextUse,
  localChanges,
  type PullFacts,
  type PullRef,
  pullFacts,
  pullKey,
  pullsOf,
  reposOf,
  type SandboxState,
  sandboxState,
} from "./sessionContext.ts";
import type { SessionSummary } from "./sessionSummary.ts";
import { menuRow, UsageMeter } from "./ui.tsx";
import { costWords, count } from "./usage.ts";
import { type ConversationSandboxes, workspaceFiles } from "./workspace-files.ts";

const words = defaultCopy.sessionPanel;

const row = `${menuRow()} justify-between`;

/** One row of the panel: a mark, what it is, a short value, and where it leads. */
export function Row({
  mark,
  label,
  sub,
  value,
  onOpen,
  href: link,
  testId,
  expanded,
  controls,
}: {
  mark: ReactNode;
  label: ReactNode;
  /** A second line under the label, as a pull request's numbers. */
  sub?: ReactNode;
  value?: ReactNode;
  onOpen?: () => void;
  href?: string;
  testId?: string;
  /** The row opens a part of the panel in place, which is open or not. */
  expanded?: boolean;
  /** The part it opens. */
  controls?: string;
}) {
  const toggles = expanded !== undefined;
  const body = (
    <>
      <span className="flex min-w-0 items-center gap-2">
        <span className="flex size-5 shrink-0 items-center justify-center self-start pt-0.5 text-aui-muted">
          {mark}
        </span>
        <span className="flex min-w-0 flex-col">
          <span className="min-w-0 truncate">{label}</span>
          {sub && <span className="min-w-0 truncate text-aui-muted text-aui-xs">{sub}</span>}
        </span>
      </span>
      <span className="flex min-w-0 max-w-[55%] shrink-0 items-center gap-1 text-aui-muted text-aui-xs">
        {value !== undefined && <span className="min-w-0 truncate">{value}</span>}
        {toggles ? (
          <ChevronDownIcon
            size={12}
            aria-hidden
            className={`shrink-0 transition-transform motion-reduce:transition-none ${expanded ? "" : "-rotate-90"}`}
          />
        ) : link ? (
          <ExternalLinkIcon size={12} aria-hidden className="shrink-0" />
        ) : (
          onOpen && <ChevronRightIcon size={12} aria-hidden className="shrink-0" />
        )}
      </span>
    </>
  );
  const tall = sub ? "py-1.5" : "";
  if (link)
    return (
      <li>
        <a
          href={link}
          target="_blank"
          rel="noreferrer"
          className={`${row} ${tall}`}
          data-testid={testId}
        >
          {body}
        </a>
      </li>
    );
  return (
    <li>
      <button
        type="button"
        onClick={onOpen}
        disabled={!onOpen}
        aria-expanded={toggles ? expanded : undefined}
        aria-controls={toggles ? controls : undefined}
        className={`${row} ${tall}`}
        data-testid={testId}
      >
        {body}
      </button>
    </li>
  );
}

/** A titled group of rows. A section with nothing to say is left out by its caller. */
export function Section({
  label,
  children,
  testId,
  quiet = false,
}: {
  label: string;
  children: ReactNode;
  testId?: string;
  /** The rows name the section themselves, so its heading is for assistive technology only. */
  quiet?: boolean;
}) {
  return (
    <section
      aria-label={label}
      className="flex flex-col gap-0.5"
      data-testid="details-section"
      data-section={testId}
    >
      <h3 className={quiet ? "sr-only" : "px-2.5 pb-1 text-aui-muted text-aui-xs"}>{label}</h3>
      <ul className="flex flex-col">{children}</ul>
    </section>
  );
}

/** Where the panel's rows lead: a workbench tab, or a tab of the Workspace pane. */
export interface PanelOpen {
  tab: (id: WorkbenchTabId, ref?: string) => void;
  workspace?: (tab: PaneTab) => void;
}

/** The session's sandbox as its API reports it, read again on Try Again. */
export function useSandboxes(thread: string | undefined): {
  boxes: ConversationSandboxes | undefined;
  state: SandboxState;
  retry: () => void;
} {
  const [boxes, setBoxes] = useState<ConversationSandboxes>();
  const [failed, setFailed] = useState(false);
  const [tick, setTick] = useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick reads it again.
  useEffect(() => {
    if (!thread) return;
    let on = true;
    setFailed(false);
    setBoxes(undefined);
    workspaceFiles
      .sandboxes(thread)
      .then((b) => on && setBoxes(b))
      .catch(() => on && setFailed(true));
    return () => {
      on = false;
    };
  }, [thread, tick]);
  return { boxes, state: sandboxState(boxes, failed), retry: () => setTick((t) => t + 1) };
}

const stateTone: Record<SandboxState, string> = {
  isolated: "bg-aui-ok",
  host: "bg-aui-muted",
  none: "bg-aui-muted",
  unreachable: "bg-aui-bad",
  loading: "bg-aui-muted",
};

/** Each pull request's facts, read once per page from the GitHub API through the platform. */
function usePulls(pulls: readonly PullRef[]): Record<string, PullFacts | null | undefined> {
  const key = pulls.map(pullKey).join(",");
  const [facts, setFacts] = useState<Record<string, PullFacts | null | undefined>>({});
  // biome-ignore lint/correctness/useExhaustiveDependencies: keyed by the pulls' keys.
  useEffect(() => {
    let on = true;
    for (const p of pulls) {
      const k = pullKey(p);
      api
        .githubPull(p.repo, p.number)
        .then((pull) => on && setFacts((f) => ({ ...f, [k]: pullFacts(pull) })))
        .catch(() => on && setFacts((f) => ({ ...f, [k]: null })));
    }
    return () => {
      on = false;
    };
  }, [key]);
  return facts;
}

/**
 * Environment: the sandbox and where it stands, its files, the browser when
 * the session used one, and the repositories and branches its pull requests
 * are on. The sandbox opens the Terminal, its files the Workspace pane.
 */
export function EnvironmentSection({
  thread,
  summary,
  pulls,
  facts,
  open,
}: {
  thread: string | undefined;
  summary: SessionSummary;
  pulls: readonly PullRef[];
  facts: Readonly<Record<string, PullFacts | null | undefined>>;
  open: PanelOpen;
}) {
  const { boxes, state, retry } = useSandboxes(thread);
  const browser = summary.environment.some((e) => e.kind === "browser");
  const repos = reposOf(
    pulls,
    Object.fromEntries(Object.entries(facts).map(([k, v]) => [k, v ?? undefined])),
  );
  const files = boxes?.session.files;
  return (
    <Section label={defaultCopy.details.environment} testId="environment">
      <Row
        testId="panel-sandbox"
        mark={<ServerIcon size={16} />}
        label={words.sandbox}
        sub={
          state === "none"
            ? words.sandboxHint.none
            : state === "unreachable"
              ? words.sandboxHint.unreachable
              : undefined
        }
        value={
          <span className="inline-flex items-center gap-1.5" data-state={state}>
            <span aria-hidden className={`size-2 rounded-full ${stateTone[state]}`} />
            {words.sandboxState[state]}
          </span>
        }
        onOpen={() => open.tab("terminal")}
      />
      {state === "unreachable" && (
        <li className="pl-9.5">
          <button
            type="button"
            onClick={retry}
            className="inline-flex min-h-9 pointer-coarse:min-h-11 items-center rounded-aui text-aui-sm text-aui-fg underline underline-offset-4 focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            {words.retry}
          </button>
        </li>
      )}
      {open.workspace && state !== "unreachable" && (
        <Row
          testId="panel-files"
          mark={<FolderIcon size={16} />}
          label={words.files}
          value={files === undefined ? undefined : defaultCopy.details.files(files)}
          onOpen={() => open.workspace?.({ kind: "files" })}
        />
      )}
      {browser && (
        <Row
          testId="panel-browser"
          mark={<MonitorIcon size={16} />}
          label={words.browser}
          onOpen={() => open.tab("browser")}
        />
      )}
      {repos.map((r) => (
        <RepoRows key={r.repo} repo={r.repo} branches={r.branches} />
      ))}
    </Section>
  );
}

function RepoRows({ repo, branches }: { repo: string; branches: string[] }) {
  return (
    <>
      <Row
        testId="panel-repo"
        mark={<GitHubIcon size={16} />}
        label={words.repo(repo)}
        href={`https://github.com/${repo}`}
      />
      {branches.map((b) => (
        <Row
          key={b}
          testId="panel-branch"
          mark={<BranchMark />}
          label={b}
          href={`https://github.com/${repo}/tree/${encodeURIComponent(b).replace(/%2F/g, "/")}`}
        />
      ))}
    </>
  );
}

/** A branch, drawn as two commits joined. */
function BranchMark() {
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      width={16}
      height={16}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <circle cx="6" cy="6" r="2.5" />
      <circle cx="6" cy="18" r="2.5" />
      <circle cx="18" cy="8" r="2.5" />
      <path d="M6 8.5v7M18 10.5c0 4-6 3.5-10.5 6" />
    </svg>
  );
}

/** Context: how full the window is at the latest model call, opening to what the session used. */
export function ContextSection({ runs }: { runs: readonly Run[] }) {
  const use = contextUse(runs);
  const [expanded, setExpanded] = useState(false);
  const id = useId();
  return (
    <Section label={words.context} testId="context" quiet>
      <Row
        testId="panel-context"
        mark={<GaugeMark use={use} />}
        label={words.context}
        value={
          use?.percent != null ? (
            <span className="inline-flex items-center gap-2">
              <span className="tabular-nums">{words.contextOf(use.percent)}</span>
              <span className="w-14">
                <UsageMeter
                  size="mini"
                  label={words.context}
                  spent={use.used}
                  limit={use.window}
                  format={count}
                  testId="panel-context-meter"
                />
              </span>
            </span>
          ) : (
            words.contextNone
          )
        }
        expanded={use ? expanded : undefined}
        controls={id}
        onOpen={use ? () => setExpanded((e) => !e) : undefined}
      />
      {use && expanded && <ContextBreakdown id={id} use={use} />}
    </Section>
  );
}

function GaugeMark({ use }: { use: ContextUse | undefined }) {
  return (
    <span aria-hidden className="text-aui-muted">
      <svg
        width={16}
        height={16}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth={2}
      >
        <circle cx="12" cy="12" r="9" opacity={0.35} />
        {use?.percent != null && use.percent > 0 && (
          <circle
            cx="12"
            cy="12"
            r="9"
            strokeDasharray={`${(use.percent / 100) * 56.5} 56.5`}
            transform="rotate(-90 12 12)"
          />
        )}
      </svg>
    </span>
  );
}

function ContextBreakdown({ id, use }: { id: string; use: ContextUse }) {
  const t = use.total;
  const r = words.contextRows;
  const rows: [string, string][] = [
    [
      r.window,
      use.window ? words.contextUsed(count(use.used), count(use.window)) : count(use.used),
    ],
    ...(use.model ? ([[r.model, use.model]] as [string, string][]) : []),
    [r.prompt, count(t.prompt_tokens ?? 0)],
    [r.cached, count(t.cached_prompt_tokens ?? 0)],
    [r.output, count(t.completion_tokens ?? 0)],
    [r.calls, count(t.model_calls ?? 0)],
    [r.cost, costWords(t)],
  ];
  return (
    <li id={id} data-testid="panel-context-breakdown">
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 px-2.5 pt-1 pb-2 pl-9.5 text-aui-sm">
        {rows.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-aui-muted">{k}</dt>
            <dd className="min-w-0 truncate text-right tabular-nums text-aui-fg">{v}</dd>
          </div>
        ))}
      </dl>
    </li>
  );
}

/**
 * Connectors: the tools that act as the reader and whether each is
 * connected, with those this session used marked. It opens to the list; the
 * list ends with the way to manage them.
 */
export function ConnectorsSection({ summary }: { summary: SessionSummary }) {
  const [list, setList] = useState<ToolCredential[] | null>();
  const [expanded, setExpanded] = useState(false);
  const id = useId();
  useEffect(() => {
    let on = true;
    api
      .toolCredentials()
      .then((c) => on && setList(c.providers))
      .catch(() => on && setList(null));
    return () => {
      on = false;
    };
  }, []);
  const used = summary.used.connections.map((c) => c.label);
  const connected = (list ?? []).filter((c) => c.connected);
  // A connector the session used that is not one of the reader's own, as GitHub through the workspace's app.
  const others = used.filter(
    (u) => !(list ?? []).some((c) => c.provider.toLowerCase() === u.toLowerCase()),
  );
  const total = connected.length + others.length;
  if (list === undefined && used.length === 0) return null;
  if (list !== null && list !== undefined && list.length === 0 && used.length === 0) return null;
  return (
    <Section label={words.connectors} testId="connectors" quiet>
      <Row
        testId="panel-connectors"
        mark={<PlugIcon size={16} />}
        label={words.connectors}
        value={list === null ? words.connectorsFailed : words.connected(total)}
        expanded={expanded}
        controls={id}
        onOpen={() => setExpanded((e) => !e)}
      />
      {expanded && (
        <li id={id}>
          <ul className="flex flex-col pl-7" data-testid="panel-connector-list">
            {others.map((name) => (
              <ConnectorRow key={name} name={name} state={words.connectorUsed} tone="ok" />
            ))}
            {(list ?? []).map((c) => (
              <ConnectorRow
                key={c.provider}
                name={c.provider}
                state={
                  used.some((u) => u.toLowerCase() === c.provider.toLowerCase())
                    ? words.connectorUsed
                    : c.connected
                      ? words.connectorReady
                      : words.connectorOff
                }
                tone={c.connected ? "ok" : "off"}
              />
            ))}
            <li>
              <a
                href={href({ page: "connections" })}
                className="inline-flex min-h-9 items-center rounded-aui px-2.5 text-aui-sm text-aui-fg underline underline-offset-4 focus-visible:outline-2 focus-visible:outline-aui-accent"
              >
                {words.manageConnectors}
              </a>
            </li>
          </ul>
        </li>
      )}
    </Section>
  );
}

function ConnectorRow({ name, state, tone }: { name: string; state: string; tone: "ok" | "off" }) {
  return (
    <li
      className="flex min-h-9 items-center justify-between gap-2 px-2.5 text-aui-sm"
      data-testid="panel-connector"
    >
      <span className="inline-flex min-w-0 items-center gap-2">
        <span
          aria-hidden
          className={`size-2 shrink-0 rounded-full ${tone === "ok" ? "bg-aui-ok" : "border border-aui-muted"}`}
        />
        <span className="truncate capitalize">{name}</span>
      </span>
      <span className="shrink-0 text-aui-muted text-aui-xs">{state}</span>
    </li>
  );
}

/** How many pull requests show before Show More. */
export const PULLS_SHOWN = 5;

/**
 * Changes: the files the session wrote, which open the Changes tab, and the
 * pull requests it made or read, each with the lines it adds and removes and
 * how its checks stand. A pull request opens on GitHub.
 */
export function PanelChanges({
  pulls,
  facts,
  local,
  onLocal,
}: {
  pulls: readonly PullRef[];
  facts: Readonly<Record<string, PullFacts | null | undefined>>;
  local: { files: number; additions: number; deletions: number };
  onLocal: () => void;
}) {
  const [all, setAll] = useState(false);
  if (pulls.length === 0 && local.files === 0) return null;
  const shown = all ? pulls : pulls.slice(0, PULLS_SHOWN);
  return (
    <Section label={words.changes} testId="changes">
      {local.files > 0 && (
        <Row
          testId="panel-local-changes"
          mark={<DiffIcon size={16} />}
          label={words.filesChanged(local.files)}
          value={<Counts additions={local.additions} deletions={local.deletions} />}
          onOpen={onLocal}
        />
      )}
      {shown.map((p) => {
        const f = facts[pullKey(p)];
        return (
          <Row
            key={pullKey(p)}
            testId="panel-pull"
            mark={<PullMark state={f?.checks.state} />}
            label={f?.title ?? words.pull(p.repo, p.number)}
            sub={
              <span className="inline-flex items-center gap-2">
                <span>{words.pull(p.repo, p.number)}</span>
                {f && <Counts additions={f.additions} deletions={f.deletions} />}
              </span>
            }
            value={<ChecksWords facts={f} />}
            href={p.url}
          />
        );
      })}
      {!all && pulls.length > PULLS_SHOWN && (
        <li>
          <button
            type="button"
            onClick={() => setAll(true)}
            className="inline-flex min-h-9 items-center rounded-aui px-2.5 text-aui-muted text-aui-sm hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            {words.showMore(pulls.length - PULLS_SHOWN)}
          </button>
        </li>
      )}
    </Section>
  );
}

function Counts({ additions, deletions }: { additions: number; deletions: number }) {
  return (
    <span className="inline-flex gap-1 tabular-nums">
      <span className="text-aui-ok">+{count(additions)}</span>
      <span className="text-aui-bad">−{count(deletions)}</span>
    </span>
  );
}

function ChecksWords({ facts }: { facts: PullFacts | null | undefined }) {
  if (facts === undefined) return null;
  if (facts === null) return <span data-checks="unknown">{words.ci.unknown}</span>;
  const c = facts.checks;
  return (
    <span
      data-checks={c.state}
      className={
        c.state === "failing" ? "text-aui-bad" : c.state === "passing" ? "text-aui-ok" : ""
      }
    >
      {c.state === "failing" ? words.ci.failing(c.failing) : words.ci[c.state]}
    </span>
  );
}

function PullMark({ state }: { state: PullFacts["checks"]["state"] | undefined }) {
  return (
    <span
      className={state === "failing" ? "text-aui-bad" : state === "passing" ? "text-aui-ok" : ""}
    >
      <GitHubIcon size={16} />
    </span>
  );
}

/** The new sections of the panel, in the reference's order after Progress and Agents. */
export function SessionSections({
  thread,
  runs,
  responses,
  summary,
  open,
  outputs,
}: {
  thread: string | undefined;
  runs: readonly Run[];
  responses: readonly ResponseState[] | undefined;
  summary: SessionSummary;
  open: PanelOpen;
  /** Details' own Outputs section, drawn between Connectors and Changes. */
  outputs?: ReactNode;
}) {
  const pulls = pullsOf(summary);
  const facts = usePulls(pulls);
  return (
    <>
      <EnvironmentSection
        thread={thread}
        summary={summary}
        pulls={pulls}
        facts={facts}
        open={open}
      />
      <div className="-my-1 flex flex-col border-[var(--humagen-rule)] border-y py-1">
        <ContextSection runs={runs} />
        <ConnectorsSection summary={summary} />
      </div>
      {outputs}
      <PanelChanges
        pulls={pulls}
        facts={facts}
        local={localChanges(responses)}
        onLocal={() => open.tab("changes")}
      />
    </>
  );
}
