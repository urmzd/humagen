import { defaultCopy } from "@humagen/components";
import { useCallback, useEffect, useRef, useState } from "react";
import { type AlertRuleConfig, api, type ProjectView } from "./api.ts";
import { AgentTag, FilterSelect, ItemCard, PriorityPill, StatusPill, Tag } from "./cards.tsx";
import { type ProjectRef, readableProject } from "./projects.ts";
import { href, type Route, SESSION_PLACES, type SessionPlaceFilter } from "./route.ts";
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
import { referenceOf } from "./trace.ts";
import {
  ago,
  button,
  Confirm,
  Page,
  Problem,
  primary,
  SearchField,
  Segmented,
  SkeletonRows,
  Tabs,
  textAction,
} from "./ui.tsx";

const places = defaultCopy.places;

/** Where a root session started, as the place filter names it. */
export function placeOfSession(s: AgentSessionSummary): SessionPlaceFilter | undefined {
  const where = sessionSource(s);
  return where === "Slack"
    ? "slack"
    : where === "GitHub"
      ? "github"
      : where === "Scheduled"
        ? "schedule"
        : where === "Console"
          ? "console"
          : s.parent.startsWith("slack://")
            ? "slack"
            : s.parent.startsWith("github://")
              ? "github"
              : s.parent.startsWith("schedule://")
                ? "schedule"
                : undefined;
}

const failure = (error: unknown) =>
  error instanceof Error ? error.message : "This could not be loaded. Try again.";
const priorityLabel = { urgent: "Urgent", normal: "Normal", low: "Low" };
type SessionState = "working" | "waiting" | "done" | "failed" | "stopped";
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
  return <PriorityPill priority={alertPriority(alert)} />;
}
export function SessionRow({
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
    <li className={nested ? "ml-4 border-aui-border border-l pt-3 pl-4" : ""}>
      <ItemCard
        title={session.title || session.agent}
        href={href({ page: "conversation", id: session.id })}
        time={<time dateTime={session.updated_at}>{ago(session.updated_at)}</time>}
        onOpen={() => go({ page: "conversation", id: session.id })}
        facts={
          <>
            <StatusPill status={session.status} />
            {attention && <AlertMark alert={attention} />}
            <AgentTag name={session.agent} />
            <Tag>{readableProject(session.project, projects, workspaceName)}</Tag>
            <Tag>{sessionSource(session)}</Tag>
          </>
        }
        actions={
          <button
            type="button"
            aria-label={`${expanded ? "Collapse" : "Show"} children of ${session.title || session.agent}`}
            aria-expanded={expanded}
            onClick={expand}
            className={textAction}
          >
            {expanded ? "Hide child sessions" : "Child sessions"}
          </button>
        }
      />
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
            <p className="pt-2 pl-4 text-aui-xs text-aui-muted">No child sessions.</p>
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
    // An item's sessions are the ones rooted at its address.
    ...(route.item ? { parent: route.item } : {}),
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
    (s) =>
      (!route.q || `${s.title} ${s.agent}`.toLowerCase().includes(route.q.toLowerCase())) &&
      (!route.place || placeOfSession(s) === route.place),
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
        <FilterSelect
          label={places.label}
          value={route.place ?? ""}
          onChange={(v) =>
            go({
              ...route,
              place: (SESSION_PLACES as readonly string[]).includes(v)
                ? (v as SessionPlaceFilter)
                : undefined,
            })
          }
          options={[
            { value: "", label: places.any },
            ...SESSION_PLACES.map((p) => ({ value: p, label: places[p] })),
          ]}
        />
        {route.item && (
          <span className="inline-flex items-center gap-2 text-aui-sm">
            <a
              href={href({ page: "item", address: route.item })}
              onClick={(event) => {
                event.preventDefault();
                if (route.item) go({ page: "item", address: route.item });
              }}
              className="rounded-aui underline decoration-aui-border underline-offset-2 hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
            >
              {places.item(referenceOf(route.item).label)}
            </a>
            <button
              type="button"
              className={textAction}
              onClick={() => go({ ...route, item: undefined })}
            >
              {places.clearItem}
            </button>
          </span>
        )}
        {projects.length > 1 && (
          <div className="sm:ml-auto">
            <FilterSelect
              label="Session project"
              value={route.project ?? ""}
              onChange={(v) => go({ ...route, project: v || undefined })}
              options={[
                { value: "", label: "All projects" },
                ...projects.map((p) => ({
                  value: p.name,
                  label: readableProject(p.name, projects, workspaceName),
                })),
              ]}
            />
          </div>
        )}
      </div>
      <Problem message={problem ?? null} />
      {!sessions && !problem ? (
        <SkeletonRows count={4} />
      ) : shown.length ? (
        <ul className="flex flex-col gap-3">
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
                      {rule.description || rule.kind} | {rule.paused ? "Paused" : "Watching"}
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
              <FilterSelect<"mine" | "everyone">
                label="Alert audience"
                value={route.mine === false ? "everyone" : "mine"}
                onChange={(v) => go({ ...route, mine: v === "everyone" ? false : undefined })}
                options={[
                  { value: "mine", label: "Mine" },
                  { value: "everyone", label: "Everyone's" },
                ]}
              />
              <FilterSelect
                label="Alert source"
                value={route.source ?? ""}
                onChange={(v) => go({ ...route, source: v || undefined })}
                options={[
                  { value: "", label: "All sources" },
                  ...[...new Set((rows ?? []).map((a) => a.kind))].map((kind) => ({
                    value: kind,
                    label: `${kind[0]?.toUpperCase() ?? ""}${kind.slice(1).replaceAll("_", " ")}`,
                  })),
                ]}
              />
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
            <ul className="flex flex-col gap-3">
              {shown.map((a) => (
                <li key={a.id}>
                  <ItemCard
                    title={a.title}
                    href={href({ page: "conversation", id: a.session!, message: a.message })}
                    time={ago(a.at)}
                    onOpen={() => go({ page: "conversation", id: a.session!, message: a.message })}
                    facts={
                      <>
                        {canWrite && (
                          <label className="inline-flex size-6 shrink-0 cursor-pointer items-center justify-center pointer-coarse:size-11">
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
                          </label>
                        )}
                        <AlertMark alert={a} />
                        {a.agent && <AgentTag name={a.agent} />}
                        <Tag>
                          {a.kind[0]?.toUpperCase()}
                          {a.kind.slice(1).replaceAll("_", " ")}
                        </Tag>
                      </>
                    }
                    actions={
                      <>
                        <button
                          type="button"
                          className={primary}
                          onClick={() =>
                            go({ page: "conversation", id: a.session!, message: a.message })
                          }
                        >
                          {a.action || "Open"}
                        </button>
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
                      </>
                    }
                  />
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
    <div className="mx-auto w-full max-w-4xl px-4 pt-8">
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
      {items.length > 0 && (
        <section aria-labelledby="home-needs" className="pb-6">
          <div className="flex items-center justify-between pb-2">
            <h2 id="home-needs" className="font-medium text-aui-xs text-aui-muted">
              Needs you
            </h2>
            <button className={textAction} type="button" onClick={() => go({ page: "alerts" })}>
              All alerts
            </button>
          </div>
          <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {items.map((a) => (
              <li key={a.id}>
                <ItemCard
                  title={a.title}
                  time={ago(a.at)}
                  onOpen={() => go({ page: "conversation", id: a.session!, message: a.message })}
                  facts={
                    <>
                      <PriorityPill priority={alertPriority(a)} />
                      {a.agent && <AgentTag name={a.agent} />}
                    </>
                  }
                  actions={
                    <>
                      <button
                        type="button"
                        className={primary}
                        onClick={() =>
                          go({ page: "conversation", id: a.session!, message: a.message })
                        }
                      >
                        {a.action || "Open"}
                      </button>
                      {canWrite && (
                        <button
                          type="button"
                          className={textAction}
                          aria-label={`Resolve ${a.title}`}
                          onClick={() => void resolve(a)}
                        >
                          Resolve
                        </button>
                      )}
                    </>
                  }
                />
              </li>
            ))}
          </ul>
        </section>
      )}
      {items.length === 0 && sessions && sessions.length > 0 && (
        <section aria-labelledby="home-continue">
          <div className="flex items-center justify-between pb-2">
            <h2 id="home-continue" className="font-medium text-aui-xs text-aui-muted">
              Continue
            </h2>
            <button className={textAction} type="button" onClick={() => go({ page: "sessions" })}>
              All sessions
            </button>
          </div>
          <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {sessions.slice(0, 3).map((s) => (
              <li key={s.id}>
                <ItemCard
                  title={s.title || s.agent}
                  time={ago(s.updated_at)}
                  onOpen={() => go({ page: "conversation", id: s.id })}
                  facts={
                    <>
                      <StatusPill status={s.status} />
                      <AgentTag name={s.agent} />
                      <Tag>{readableProject(s.project, projects, workspaceName)}</Tag>
                    </>
                  }
                />
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
