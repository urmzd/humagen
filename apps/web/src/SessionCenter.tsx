import { useCallback, useEffect, useRef, useState } from "react";
import { type AlertRuleConfig, api, type ProjectView } from "./api.ts";
import { type ProjectRef, readableProject } from "./projects.ts";
import { href, type Route } from "./route.ts";
import {
  type AgentSessionSummary,
  type Alert,
  alertPriority,
  attentionAlerts,
  isRootSession,
  openAlert,
  orderAlerts,
  sessionCenter,
  sessionSource,
} from "./sessionCenter.ts";
import {
  ago,
  button,
  Confirm,
  Page,
  Problem,
  SearchField,
  Segmented,
  SkeletonRows,
  Tabs,
  textAction,
} from "./ui.tsx";

const failure = (error: unknown) =>
  error instanceof Error ? error.message : "This could not be loaded. Try again.";
const priorityLabel = { urgent: "Urgent", normal: "Normal", low: "Low" };
type SessionState = "working" | "waiting" | "done" | "failed" | "stopped";
/** A filter that sizes to its value, so filters sit on one line beside the search. */
const quietSelect =
  "h-9 w-auto rounded-aui border border-aui-border bg-transparent px-3 text-aui-sm text-aui-fg outline-none focus-visible:outline-2 focus-visible:outline-aui-accent";
export function useSessionAttention(canRead: boolean, tick: unknown) {
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const request = useRef(0);
  const [problem, setProblem] = useState<string>();
  const refresh = useCallback(() => {
    const generation = ++request.current;
    if (!canRead) {
      setAlerts([]);
      return;
    }
    sessionCenter.alerts({ mine: "true", state: "open" }).then(
      (rows) => {
        if (generation !== request.current) return;
        setAlerts(orderAlerts(rows.filter((a) => !!a.session)));
        setProblem(undefined);
      },
      (e) => {
        if (generation === request.current) setProblem(failure(e));
      },
    );
  }, [canRead]);
  useEffect(() => {
    refresh();
    const timer = window.setInterval(refresh, 30_000);
    return () => {
      request.current++;
      window.clearInterval(timer);
    };
  }, [refresh, tick]);
  return { alerts, problem, refresh, count: attentionAlerts(alerts).length };
}
export function AlertMark({ alert }: { alert: Alert }) {
  const p = alertPriority(alert);
  return (
    <span className={`shrink-0 text-aui-xs ${p === "urgent" ? "text-aui-bad" : "text-aui-muted"}`}>
      <span aria-hidden>{p === "urgent" ? "! " : p === "normal" ? "• " : "· "}</span>
      {priorityLabel[p]}
    </span>
  );
}
function SessionRow({
  session,
  go,
  nested = false,
  alerts = [],
  tick,
  projects = [],
  workspaceName,
}: {
  projects?: ProjectRef[];
  workspaceName?: string;
  session: AgentSessionSummary;
  go: (route: Route) => void;
  nested?: boolean;
  alerts?: Alert[];
  tick?: unknown;
}) {
  const [children, setChildren] = useState<AgentSessionSummary[]>();
  const [expanded, setExpanded] = useState(false);
  const [childCursor, setChildCursor] = useState("");
  const [loadingChildren, setLoadingChildren] = useState(false);
  const [problem, setProblem] = useState<string>();
  const loadChildren = async (after?: string) => {
    setLoadingChildren(true);
    setProblem(undefined);
    try {
      const r = await sessionCenter.sessions({ parent: session.id, ...(after ? { after } : {}) });
      setChildren((old) => (after ? [...(old ?? []), ...r.sessions] : r.sessions));
      setChildCursor(r.next);
    } catch (e) {
      setProblem(failure(e));
    } finally {
      setLoadingChildren(false);
    }
  };
  const expand = () => {
    setExpanded((v) => !v);
    if (!children) void loadChildren();
  };
  // A session event refreshes expanded children without collapsing the tree.
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick is the event invalidation signal.
  useEffect(() => {
    if (expanded) void loadChildren();
  }, [tick]);
  const attention = orderAlerts(alerts.filter((a) => a.session === session.id && openAlert(a)))[0];
  return (
    <li className={nested ? "ml-5 border-l border-aui-border pl-3" : "border-b border-aui-border"}>
      <div className="flex min-h-14 items-center gap-2 py-2">
        <button
          type="button"
          aria-label={`${expanded ? "Collapse" : "Show"} children of ${session.title || session.agent}`}
          aria-expanded={expanded}
          onClick={expand}
          className="size-9 shrink-0 rounded-aui hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
        >
          {expanded ? "⌄" : "›"}
        </button>
        <a
          href={href({ page: "conversation", id: session.id })}
          onClick={(e) => {
            if (e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey) {
              e.preventDefault();
              go({ page: "conversation", id: session.id });
            }
          }}
          className="min-w-0 flex-1 rounded-aui px-1 py-1 focus-visible:outline-2 focus-visible:outline-aui-accent"
        >
          <span className="block truncate text-aui-sm font-medium">
            {session.title || session.agent}
          </span>
          <span className="block truncate text-aui-xs text-aui-muted">
            {session.agent} · {readableProject(session.project, projects, workspaceName)} ·{" "}
            {sessionSource(session)}
          </span>
        </a>
        {attention && <AlertMark alert={attention} />}
        <span className="text-aui-xs text-aui-muted">{session.status}</span>
        <time className="shrink-0 text-aui-xs text-aui-muted" dateTime={session.updated_at}>
          {ago(session.updated_at)}
        </time>
      </div>
      {expanded && (
        <>
          <Problem message={problem ?? null} />
          {problem && (
            <button
              type="button"
              className={textAction}
              onClick={() => void loadChildren(childCursor || undefined)}
            >
              Try again
            </button>
          )}
          {!children && !problem ? (
            <SkeletonRows count={1} />
          ) : children?.length ? (
            <ul>
              {children.map((child) => (
                <SessionRow
                  key={child.id}
                  session={child}
                  projects={projects}
                  workspaceName={workspaceName}
                  go={go}
                  nested
                  alerts={alerts}
                  tick={tick}
                />
              ))}
            </ul>
          ) : (
            <p className="pb-3 pl-12 text-aui-xs text-aui-muted">No child sessions.</p>
          )}
          {childCursor && (
            <button
              type="button"
              className={textAction}
              disabled={loadingChildren}
              onClick={() => void loadChildren(childCursor)}
            >
              {loadingChildren ? "Loading…" : "More child sessions"}
            </button>
          )}
        </>
      )}
    </li>
  );
}
export function SessionsPage({
  route,
  go,
  projects = [],
  workspaceName,
  alerts = [],
  tick,
}: {
  route: Extract<Route, { page: "sessions" }>;
  go: (route: Route) => void;
  projects?: ProjectView[];
  workspaceName?: string;
  alerts?: Alert[];
  tick: unknown;
}) {
  const [sessions, setSessions] = useState<AgentSessionSummary[]>();
  const [next, setNext] = useState("");
  const [problem, setProblem] = useState<string>();
  const [busy, setBusy] = useState(false);
  const filters = {
    roots: "true",
    mine: "true",
    ...(route.status ? { status: route.status } : {}),
    ...(route.project ? { project: route.project } : {}),
    ...(route.agent ? { agent: route.agent } : {}),
  };
  const key = JSON.stringify(filters);
  const loadedKey = useRef("");
  const currentKey = useRef(key);
  currentKey.current = key;
  useEffect(() => {
    let live = true;
    if (loadedKey.current !== key) {
      setSessions(undefined);
      setNext("");
    }
    loadedKey.current = key;
    setProblem(undefined);
    sessionCenter.sessions(JSON.parse(key)).then(
      (r) => {
        if (live) {
          setSessions(r.sessions.filter(isRootSession));
          setNext(r.next);
        }
      },
      (e) => {
        if (live) setProblem(failure(e));
      },
    );
    return () => {
      live = false;
    };
  }, [key, tick]);
  const more = async () => {
    setBusy(true);
    try {
      const r = await sessionCenter.sessions({ ...filters, after: next });
      if (currentKey.current !== key) return;
      setSessions((old) => [...(old ?? []), ...r.sessions.filter(isRootSession)]);
      setNext(r.next);
    } catch (e) {
      setProblem(failure(e));
    } finally {
      setBusy(false);
    }
  };
  const shown = (sessions ?? []).filter(
    (s) => !route.q || `${s.title} ${s.agent}`.toLowerCase().includes(route.q.toLowerCase()),
  );
  const urgency = (id: string) => {
    const a = orderAlerts(alerts.filter((a) => a.session === id && openAlert(a)))[0];
    return a ? (alertPriority(a) === "urgent" ? 0 : alertPriority(a) === "normal" ? 1 : 2) : 3;
  };
  shown.sort((a, b) => urgency(a.id) - urgency(b.id) || b.updated_at.localeCompare(a.updated_at));
  return (
    <Page title="Sessions" lede="Your work, with each agent's child sessions kept together.">
      <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center">
        <SearchField
          label="Search sessions"
          value={route.q ?? ""}
          onChange={(q) => go({ ...route, q: q || undefined })}
          placeholder="Search by title or agent"
          className="sm:w-72"
        />
        <Segmented<SessionState | "any">
          label="Session state"
          value={route.status ?? "any"}
          options={[
            { id: "any", label: "Any" },
            { id: "working", label: "Working" },
            { id: "waiting", label: "Waiting" },
            { id: "done", label: "Done" },
            { id: "failed", label: "Failed" },
          ]}
          onChange={(v) => go({ ...route, status: v === "any" ? undefined : v })}
        />
        {projects.length > 1 && (
          <select
            className={quietSelect}
            aria-label="Session project"
            value={route.project ?? ""}
            onChange={(e) => go({ ...route, project: e.target.value || undefined })}
          >
            <option value="">All projects</option>
            {projects.map((p) => (
              <option key={p.name} value={p.name}>
                {readableProject(p.name, projects, workspaceName)}
              </option>
            ))}
          </select>
        )}
      </div>
      <Problem message={problem ?? null} />
      {!sessions && !problem ? (
        <SkeletonRows count={4} />
      ) : shown.length ? (
        <ul>
          {shown.map((s) => (
            <SessionRow
              key={s.id}
              session={s}
              go={go}
              alerts={alerts}
              tick={tick}
              projects={projects}
              workspaceName={workspaceName}
            />
          ))}
        </ul>
      ) : (
        <p className="text-aui-sm text-aui-muted">No sessions match these filters.</p>
      )}
      {next && (
        <button type="button" className={button} disabled={busy} onClick={() => void more()}>
          {busy ? "Loading…" : "Load more"}
        </button>
      )}
    </Page>
  );
}
export function AlertsPage({
  route,
  go,
  canWrite,
  onChanged,
  tick,
}: {
  route: Extract<Route, { page: "alerts" }>;
  go: (route: Route) => void;
  canWrite: boolean;
  onChanged: () => void;
  tick: unknown;
}) {
  const [rows, setRows] = useState<Alert[]>();
  const request = useRef(0);
  const [rules, setRules] = useState<AlertRuleConfig[]>();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [problem, setProblem] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [undo, setUndo] = useState<Alert[]>([]);
  const [remove, setRemove] = useState<AlertRuleConfig>();
  const key = JSON.stringify({
    state: route.state ?? "open",
    mine: route.mine === false ? "false" : "true",
    ...(route.priority ? { priority: route.priority } : {}),
    ...(route.source ? { source: route.source } : {}),
  });
  const refresh = useCallback(() => {
    const generation = ++request.current;
    if (route.tab === "watching")
      api.alertRuleConfigs().then(
        (r) => {
          if (generation === request.current) setRules(r);
        },
        (e) => {
          if (generation === request.current) setProblem(failure(e));
        },
      );
    else
      sessionCenter.alerts(JSON.parse(key)).then(
        (r) => {
          if (generation === request.current) setRows(orderAlerts(r.filter((a) => !!a.session)));
        },
        (e) => {
          if (generation === request.current) setProblem(failure(e));
        },
      );
  }, [key, route.tab]);
  useEffect(() => {
    setRows(undefined);
    setSelected(new Set());
    refresh();
    return () => {
      request.current++;
    };
  }, [refresh, tick]);
  const change = async (
    items: Alert[],
    state: "open" | "resolved" | "snoozed",
    restore = false,
  ) => {
    setBusy(true);
    setProblem(undefined);
    const done: Alert[] = [];
    try {
      for (const a of items) {
        await sessionCenter.change(
          a.id,
          restore
            ? {
                state: a.state ?? "open",
                ...(a.snoozed_until ? { snoozed_until: a.snoozed_until } : {}),
              }
            : {
                state,
                ...(state === "snoozed"
                  ? { snoozed_until: new Date(Date.now() + 60 * 60 * 1000).toISOString() }
                  : {}),
              },
        );
        done.push(a);
      }
      setSelected(new Set());
    } catch (e) {
      setProblem(failure(e));
    } finally {
      setUndo(
        restore ? items.filter((item) => !done.some((updated) => updated.id === item.id)) : done,
      );
      setBusy(false);
      refresh();
      onChanged();
    }
  };
  const action = (promise: Promise<unknown>) => {
    setProblem(undefined);
    promise.then(
      () => {
        refresh();
        onChanged();
      },
      (e) => setProblem(failure(e)),
    );
  };
  const shown = (rows ?? []).filter((a) => !route.source || a.kind === route.source);
  return (
    <Page
      title="Alerts"
      tabs={
        <Tabs
          label="Alerts sections"
          value={route.tab ?? "open"}
          tabs={[
            { id: "open", label: "Alerts" },
            { id: "watching", label: "Watching" },
          ]}
          onChange={(tab) => go({ ...route, tab: tab === "watching" ? "watching" : undefined })}
        />
      }
    >
      <Problem message={problem ?? null} />
      {undo.length > 0 && (
        <p role="status" className="flex items-center gap-3 text-aui-sm">
          Updated {undo.length} {undo.length === 1 ? "alert" : "alerts"}.{" "}
          <button
            type="button"
            className={textAction}
            disabled={busy}
            onClick={() => void change(undo, "open", true)}
          >
            Undo
          </button>
        </p>
      )}
      {route.tab === "watching" ? (
        <>
          {!rules ? (
            <SkeletonRows count={3} />
          ) : rules.length ? (
            <ul>
              {rules.map((rule) => (
                <li
                  key={rule.name}
                  className="flex items-center gap-3 border-b border-aui-border py-3"
                >
                  <span className="min-w-0 flex-1">
                    <span className="block text-aui-sm font-medium">{rule.name}</span>
                    <span className="text-aui-xs text-aui-muted">
                      {rule.description || rule.kind} · {rule.paused ? "Paused" : "Watching"}
                    </span>
                  </span>
                  {canWrite && (
                    <>
                      <button
                        className={button}
                        type="button"
                        onClick={() => action(api.setAlertRulePaused(rule, !rule.paused))}
                      >
                        {rule.paused ? "Resume" : "Pause"}
                      </button>
                      <button className={textAction} type="button" onClick={() => setRemove(rule)}>
                        Delete
                      </button>
                    </>
                  )}
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-aui-sm text-aui-muted">
              No standing alerts. Ask an agent what you want to watch.
            </p>
          )}
        </>
      ) : (
        <>
          <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center">
            <Segmented<"open" | "snoozed" | "resolved">
              label="Alert state"
              value={route.state ?? "open"}
              options={[
                { id: "open", label: "Open" },
                { id: "snoozed", label: "Snoozed" },
                { id: "resolved", label: "Resolved" },
              ]}
              onChange={(v) => go({ ...route, state: v })}
            />
            <Segmented<"any" | keyof typeof priorityLabel>
              label="Alert priority"
              value={route.priority ?? "any"}
              options={[
                { id: "any", label: "Any priority" },
                { id: "urgent", label: "Urgent" },
                { id: "normal", label: "Normal" },
                { id: "low", label: "Low" },
              ]}
              onChange={(v) => go({ ...route, priority: v === "any" ? undefined : v })}
            />
            <div className="flex gap-2 sm:ml-auto">
              <select
                className={quietSelect}
                aria-label="Alert audience"
                value={route.mine === false ? "everyone" : "mine"}
                onChange={(e) =>
                  go({ ...route, mine: e.target.value === "everyone" ? false : undefined })
                }
              >
                <option value="mine">Mine</option>
                <option value="everyone">Everyone's</option>
              </select>
              <select
                className={quietSelect}
                aria-label="Alert source"
                value={route.source ?? ""}
                onChange={(e) => go({ ...route, source: e.target.value || undefined })}
              >
                <option value="">All sources</option>
                {[...new Set((rows ?? []).map((a) => a.kind))].map((kind) => (
                  <option key={kind} value={kind}>
                    {kind[0]?.toUpperCase()}
                    {kind.slice(1).replaceAll("_", " ")}
                  </option>
                ))}
              </select>
            </div>
          </div>
          {canWrite && selected.size > 0 && (
            <div className="flex gap-2">
              <button
                type="button"
                className={button}
                disabled={busy}
                onClick={() =>
                  void change(
                    shown.filter((a) => selected.has(a.id)),
                    "resolved",
                  )
                }
              >
                Resolve selected
              </button>
              <button
                type="button"
                className={button}
                disabled={busy}
                onClick={() =>
                  void change(
                    shown.filter((a) => selected.has(a.id)),
                    "snoozed",
                  )
                }
              >
                Snooze selected for 1 hour
              </button>
            </div>
          )}
          {!rows && !problem ? (
            <SkeletonRows count={4} />
          ) : shown.length ? (
            <ul>
              {shown.map((a) => (
                <li
                  key={a.id}
                  className="flex flex-wrap items-center gap-3 border-b border-aui-border py-3"
                >
                  {canWrite && (
                    <input
                      type="checkbox"
                      aria-label={`Select ${a.title}`}
                      checked={selected.has(a.id)}
                      onChange={(e) =>
                        setSelected((old) => {
                          const next = new Set(old);
                          if (e.target.checked) next.add(a.id);
                          else next.delete(a.id);
                          return next;
                        })
                      }
                    />
                  )}
                  <AlertMark alert={a} />
                  <a
                    className="min-w-0 flex-1 rounded-aui focus-visible:outline-2 focus-visible:outline-aui-accent"
                    href={href({ page: "conversation", id: a.session!, message: a.message })}
                    onClick={(e) => {
                      if (e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey) {
                        e.preventDefault();
                        go({ page: "conversation", id: a.session!, message: a.message });
                      }
                    }}
                  >
                    <span className="block text-aui-sm font-medium">{a.title}</span>
                    <span className="block text-aui-xs text-aui-muted">
                      {a.agent || "Session"} · {ago(a.at)}
                      {a.action ? ` · ${a.action}` : ""}
                    </span>
                  </a>
                  {canWrite && (
                    <>
                      <button
                        type="button"
                        disabled={busy}
                        className={textAction}
                        onClick={() => void change([a], "resolved")}
                      >
                        Resolve
                      </button>
                      <button
                        type="button"
                        disabled={busy}
                        className={textAction}
                        onClick={() => void change([a], "snoozed")}
                      >
                        Snooze 1h
                      </button>
                    </>
                  )}
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-aui-sm text-aui-muted">
              No {route.state ?? "open"} alerts match these filters.
            </p>
          )}
        </>
      )}
      {remove && (
        <Confirm
          question={`Delete ${remove.name}? This standing alert will stop watching for changes.`}
          action="Delete"
          onKeep={() => setRemove(undefined)}
          onConfirm={() => {
            action(api.deleteAlertRule(remove.name));
            setRemove(undefined);
          }}
        />
      )}
    </Page>
  );
}
export function HomeSessionGlance({
  alerts,
  go,
  canWrite,
  onChanged,
  tick,
  onHasSessions,
  projects = [],
  workspaceName,
}: {
  projects?: ProjectRef[];
  workspaceName?: string;
  alerts: Alert[];
  go: (route: Route) => void;
  canWrite: boolean;
  onChanged: () => void;
  tick: unknown;
  onHasSessions?: (has: boolean) => void;
}) {
  const [sessions, setSessions] = useState<AgentSessionSummary[]>();
  const [problem, setProblem] = useState<string>();
  const [undo, setUndo] = useState<Alert>();
  useEffect(() => {
    let live = true;
    sessionCenter.sessions({ roots: "true", mine: "true", limit: "3" }).then(
      (r) => {
        if (live) {
          setSessions(r.sessions.filter(isRootSession));
          onHasSessions?.(r.sessions.length > 0);
        }
      },
      (e) => {
        if (live) setProblem(failure(e));
      },
    );
    return () => {
      live = false;
    };
  }, [tick, onHasSessions]);
  const items = orderAlerts(attentionAlerts(alerts)).slice(0, 3);
  const resolve = async (a: Alert) => {
    try {
      await sessionCenter.change(a.id, { state: "resolved" });
      setUndo(a);
      onChanged();
    } catch (e) {
      setProblem(failure(e));
    }
  };
  return (
    <div className="mx-auto w-full max-w-3xl px-4 pt-6">
      <Problem message={problem ?? null} />
      {undo && (
        <p role="status" className="mb-2 text-aui-xs">
          Alert resolved.{" "}
          <button
            type="button"
            className={textAction}
            onClick={() => {
              void sessionCenter.change(undo.id, { state: "open" }).then(
                () => {
                  setUndo(undefined);
                  onChanged();
                },
                (e) => setProblem(failure(e)),
              );
            }}
          >
            Undo
          </button>
        </p>
      )}
      {items.length ? (
        <>
          <ul>
            {items.map((a) => (
              <li key={a.id} className="flex items-center gap-3 py-2">
                <AlertMark alert={a} />
                <button
                  className="min-w-0 flex-1 truncate text-left text-aui-sm hover:underline"
                  type="button"
                  onClick={() => go({ page: "conversation", id: a.session!, message: a.message })}
                >
                  {a.title}
                  <span className="ml-2 text-aui-xs text-aui-muted">{a.agent || "Session"}</span>
                </button>
                {canWrite && (
                  <button
                    className="size-9 rounded-aui hover:bg-aui-hover"
                    type="button"
                    aria-label={`Resolve ${a.title}`}
                    onClick={() => void resolve(a)}
                  >
                    ×
                  </button>
                )}
              </li>
            ))}
          </ul>
          <button className={textAction} type="button" onClick={() => go({ page: "alerts" })}>
            Alert center
          </button>
        </>
      ) : sessions?.length ? (
        <>
          <h2 className="pb-1 text-aui-xs font-medium text-aui-muted">Continue</h2>
          <ul>
            {sessions.slice(0, 3).map((s) => (
              <li key={s.id}>
                <button
                  className="flex min-h-10 w-full items-center gap-2 rounded-aui text-left text-aui-sm hover:bg-aui-hover"
                  type="button"
                  onClick={() => go({ page: "conversation", id: s.id })}
                >
                  <span className="min-w-0 flex-1">
                    <span className="block truncate">{s.title || s.agent}</span>
                    <span className="block truncate text-aui-xs text-aui-muted">
                      {readableProject(s.project, projects, workspaceName)} · {s.status}
                    </span>
                  </span>
                  <span className="text-aui-xs text-aui-muted">
                    {s.agent} · {ago(s.updated_at)}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </>
      ) : null}
    </div>
  );
}
