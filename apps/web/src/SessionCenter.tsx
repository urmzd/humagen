import { defaultCopy } from "@humagen/components";
import { CheckIcon, ClockIcon } from "@humagen/render-web";
import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";
import { type AlertRuleConfig, api, type ProjectView } from "./api.ts";
import { agentShown } from "./branding.ts";
import {
  AgentTag,
  FilterSelect,
  follow,
  ItemCard,
  itemGrid,
  PriorityPill,
  StatusPill,
  Tag,
} from "./cards.tsx";
import { type ProjectRef, readableProject } from "./projects.ts";
import { href, type Route, SESSION_PLACES, type SessionPlaceFilter } from "./route.ts";
import {
  type AgentSessionSummary,
  type Alert,
  alertActionLabel,
  alertDestination,
  alertPage,
  alertPriority,
  attentionAlerts,
  holdsAnAlertOnly,
  isRootSession,
  openAlert,
  orderAlerts,
  sessionCenter,
  sessionSource,
} from "./sessionCenter.ts";
import { kindWord, type StorageTransport, type Stored, transport as storage } from "./storage.ts";
import { referenceOf } from "./trace.ts";
import {
  ago,
  button,
  Confirm,
  LoadMore,
  Page,
  Problem,
  SearchField,
  Segmented,
  SkeletonRows,
  Tabs,
  textAction,
} from "./ui.tsx";

const places = defaultCopy.places;
const words = defaultCopy.attention;

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

/** How many of the newest artifacts Home shows. The rest are under Artifacts. */
const HOME_MADE = 3;
/** One set of tracks for the header and rows. The action column fits all three controls. */
const alertRow =
  "grid grid-cols-[1.5rem_minmax(0,1fr)] items-center gap-x-3 gap-y-2 px-4 @3xl:pointer-fine:grid-cols-[1.5rem_minmax(0,1fr)_6.5rem_4.25rem_5.5rem_14.5rem]";
const alertActions =
  "col-start-2 grid min-w-0 grid-cols-[repeat(2,minmax(0,max-content))] items-center gap-2 @lg:flex @3xl:pointer-fine:col-start-auto @3xl:pointer-fine:justify-end";
/**
 * A row's lesser action. In the table it is its icon, named by a label that
 * shows on hover and on keyboard focus; in a stacked row, and on touch where
 * nothing hovers, the label is written beside the icon.
 */
function RowAction({
  label,
  icon,
  disabled,
  onClick,
}: {
  label: string;
  icon: ReactNode;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
      className="group/action relative inline-flex min-h-8 min-w-8 items-center justify-center gap-1.5 whitespace-nowrap rounded-aui px-1.5 text-aui-sm text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-50 pointer-coarse:min-h-11 pointer-coarse:min-w-11 @3xl:pointer-fine:size-8"
    >
      {icon}
      <span aria-hidden className="truncate @3xl:pointer-fine:hidden">
        {label}
      </span>
      <span
        aria-hidden
        className="humagen-glass-pop pointer-events-none absolute right-0 bottom-full z-20 mb-1 hidden whitespace-nowrap rounded-aui px-2 py-1 text-aui-2xs text-aui-fg @3xl:pointer-fine:group-hover/action:block @3xl:pointer-fine:group-focus-visible/action:block"
      >
        {label}
      </span>
    </button>
  );
}
/**
 * An alert's title: up to two lines in a stacked row, where touch has no
 * tooltip to carry the rest, and one line in the table, where rows keep one height.
 */
const alertTitleLines = "line-clamp-2 break-words @3xl:pointer-fine:line-clamp-1";
const alertCheck =
  "inline-flex size-6 items-center justify-center self-start pointer-coarse:size-11 @3xl:pointer-fine:self-center";

const failure = (error: unknown) =>
  error instanceof Error ? error.message : "This could not be loaded. Try again.";
const priorityLabel = { urgent: "Urgent", normal: "Normal", low: "Low" };
type SessionState = "working" | "waiting" | "done" | "failed" | "stopped";
/**
 * The alerts that wait on the reader, and how many. The count is asked for on
 * its own first, a small bounded read, so Home's line and the badge show it
 * without waiting for the alerts; once they are read the count is theirs.
 */
export function useSessionAttention(canRead: boolean, tick: unknown) {
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [count, setCount] = useState(0);
  const request = useRef(0);
  // Whether the alerts have been read: until then the count comes by itself.
  const listed = useRef(false);
  const [problem, setProblem] = useState<string>();
  const refresh = useCallback(() => {
    const generation = ++request.current;
    if (!canRead) {
      setAlerts([]);
      setCount(0);
      return;
    }
    if (!listed.current)
      sessionCenter.waiting().then(
        (n) => {
          if (generation === request.current && !listed.current) setCount(n);
        },
        () => {},
      );
    sessionCenter.alerts({ mine: "true", state: "open" }).then(
      (rows) => {
        if (generation !== request.current) return;
        const mine = orderAlerts(rows.filter((a) => !!a.session));
        listed.current = true;
        setAlerts(mine);
        setCount(attentionAlerts(mine).length);
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
  return { alerts, problem, refresh, count };
}
export function AlertMark({ alert }: { alert: Alert }) {
  return <PriorityPill priority={alertPriority(alert)} />;
}
/**
 * One session a person started, as a card. What its agents did is inside it,
 * under each response, so the card opens it and lists nothing beneath.
 */
export function SessionRow({
  session,
  go,
  alerts = [],
  projects = [],
  workspaceName,
}: {
  projects?: ProjectRef[];
  workspaceName?: string;
  session: AgentSessionSummary;
  go: (route: Route) => void;
  alerts?: Alert[];
}) {
  const attention = orderAlerts(alerts.filter((a) => a.session === session.id && openAlert(a)))[0];
  return (
    <li className="min-w-0">
      <ItemCard
        title={session.title || agentShown(session.agent)}
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
      />
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
  // Newest first. An alert marks its session's row and does not move it; a
  // session that only holds an alert is in Alerts, not here.
  const shown = (sessions ?? []).filter(
    (s) =>
      !holdsAnAlertOnly(s.conversation) &&
      (!route.q || `${s.title} ${s.agent}`.toLowerCase().includes(route.q.toLowerCase())) &&
      (!route.place || placeOfSession(s) === route.place),
  );
  shown.sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at));
  return (
    <Page title="Sessions">
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
        <ul className={itemGrid}>
          {shown.map((s) => (
            <SessionRow
              key={s.id}
              session={s}
              go={go}
              alerts={alerts}
              projects={projects}
              workspaceName={workspaceName}
            />
          ))}
        </ul>
      ) : (
        <p className="text-aui-sm text-aui-muted">No sessions match these filters.</p>
      )}
      {next && <LoadMore busy={busy} onMore={() => void more()} />}
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
          // An alert with no session (knowledge maintenance before a workspace has a
          // model) stays here when its action names the page it is about.
          if (generation === request.current)
            setRows(orderAlerts(r.filter((a) => !!a.session || !!alertPage(a))));
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
  const picked = shown.filter((a) => selected.has(a.id));
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
          {words.updated(undo.length)}{" "}
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
              label={words.stateLabel}
              value={route.state ?? "open"}
              options={(["open", "snoozed", "resolved"] as const).map((id) => ({
                id,
                label: words.states[id],
              }))}
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
          {!rows && !problem ? (
            <SkeletonRows count={4} />
          ) : shown.length ? (
            // Laid out as a table, so alerts line up: what it is, how pressing, when, and what to do.
            // It is a list to a screen reader: each row is its own grid with the same tracks,
            // and a narrow or touch row stacks, which a real table cannot do.
            <div className="humagen-glass overflow-hidden rounded-aui-lg">
              <div
                className={`${alertRow} min-h-11 border-aui-border border-b py-2 text-aui-2xs text-aui-muted`}
              >
                <label className={alertCheck}>
                  {canWrite && (
                    <input
                      type="checkbox"
                      aria-label={words.table.selectAll}
                      checked={picked.length === shown.length}
                      ref={(box) => {
                        if (box)
                          box.indeterminate = picked.length > 0 && picked.length < shown.length;
                      }}
                      onChange={(e) =>
                        setSelected(e.target.checked ? new Set(shown.map((a) => a.id)) : new Set())
                      }
                    />
                  )}
                </label>
                {picked.length > 0 ? (
                  <span className="col-span-full col-start-2 flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1">
                    <span className="text-aui-fg">{words.table.selected(picked.length)}</span>
                    <button
                      type="button"
                      className={textAction}
                      disabled={busy}
                      onClick={() => void change(picked, "resolved")}
                    >
                      {words.markSelectedDone}
                    </button>
                    <button
                      type="button"
                      className={textAction}
                      disabled={busy}
                      onClick={() => void change(picked, "snoozed")}
                    >
                      {words.snoozeSelected}
                    </button>
                  </span>
                ) : (
                  <>
                    <span aria-hidden>{words.table.alert}</span>
                    <span aria-hidden className="hidden @3xl:pointer-fine:block">
                      {words.table.source}
                    </span>
                    <span aria-hidden className="hidden @3xl:pointer-fine:block">
                      {words.table.priority}
                    </span>
                    <span aria-hidden className="hidden @3xl:pointer-fine:block">
                      {words.table.when}
                    </span>
                  </>
                )}
              </div>
              <ul aria-label={words.table.label}>
                {shown.map((a) => {
                  const to = alertDestination(a);
                  const label = alertActionLabel(a);
                  const title = "min-w-0 max-w-full font-medium text-aui-sm text-aui-fg";
                  return (
                    <li
                      key={a.id}
                      className={`${alertRow} border-aui-border border-b py-3 last:border-b-0 ${selected.has(a.id) ? "bg-aui-hover" : ""}`}
                    >
                      <label className={alertCheck}>
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
                      </label>
                      <span className="flex min-w-0 flex-col items-start gap-1">
                        {to ? (
                          // An alert with no session (maintenance before a workspace has a model) opens its page.
                          <a
                            href={href(to)}
                            onClick={(event) => follow(event, () => go(to))}
                            // The title is as tall as a pointer needs, and on a touch screen as a finger does.
                            className={`${title} rounded-aui-sm underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent inline-flex min-h-6 items-center pointer-coarse:min-h-11`}
                            title={a.title}
                          >
                            <span className={alertTitleLines}>{a.title}</span>
                          </a>
                        ) : (
                          <span className={`${title} ${alertTitleLines}`} title={a.title}>
                            {a.title}
                          </span>
                        )}
                      </span>
                      <span className="col-start-2 flex min-w-0 items-center gap-3 @3xl:pointer-fine:contents">
                        <span className="min-w-0 truncate text-aui-xs text-aui-muted">
                          {a.kind[0]?.toUpperCase()}
                          {a.kind.slice(1).replaceAll("_", " ")}
                        </span>
                        <span className="shrink-0">
                          <AlertMark alert={a} />
                        </span>
                        <span className="shrink-0 truncate text-aui-xs text-aui-muted">
                          {ago(a.at)}
                        </span>
                      </span>
                      <span className={alertActions}>
                        <button
                          type="button"
                          className={`${button} col-span-2 w-[9.5rem] max-w-full`}
                          title={label}
                          onClick={() => {
                            const page = alertPage(a) ?? to;
                            if (page) go(page);
                          }}
                        >
                          <span className="min-w-0 truncate">{label}</span>
                        </button>
                        {canWrite && (
                          <>
                            <RowAction
                              label={words.markDone}
                              icon={<CheckIcon size={16} />}
                              disabled={busy}
                              onClick={() => void change([a], "resolved")}
                            />
                            <RowAction
                              label={words.snooze}
                              icon={<ClockIcon size={16} />}
                              disabled={busy}
                              onClick={() => void change([a], "snoozed")}
                            />
                          </>
                        )}
                      </span>
                    </li>
                  );
                })}
              </ul>
            </div>
          ) : (
            <p className="text-aui-sm text-aui-muted">{words.empty[route.state ?? "open"]}</p>
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
/**
 * Under the chat bar: one strip of the newest things the person's agents
 * made. Alerts are counted on the Alerts entry of the menu, so Home does not
 * repeat them. With nothing made, Home is the greeting and the chat bar.
 */
export function HomeSessionGlance({
  go,
  tick,
  onHasSessions,
  canArtifacts = true,
  via = storage,
}: {
  go: (route: Route) => void;
  tick: unknown;
  onHasSessions?: (has: boolean) => void;
  /** False when the person may not read artifacts, so none are asked for. */
  canArtifacts?: boolean;
  via?: Pick<StorageTransport, "list">;
}) {
  const [made, setMade] = useState<Stored[]>([]);
  const [madeFailed, setMadeFailed] = useState(false);
  // Whether the person has started anything yet, which decides the starters above.
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick asks for a fresh read
  useEffect(() => {
    if (!onHasSessions) return;
    let live = true;
    sessionCenter.sessions({ roots: "true", mine: "true", limit: "1" }).then(
      (r) => {
        if (live) onHasSessions(r.sessions.length > 0);
      },
      () => {},
    );
    return () => {
      live = false;
    };
  }, [tick, onHasSessions]);
  // A strip that could not be read says so in one quiet line: Home still works without it.
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick asks for a fresh read
  useEffect(() => {
    if (!canArtifacts) {
      setMade([]);
      setMadeFailed(false);
      return;
    }
    let live = true;
    via.list({ project: "", agent: "", kind: "", q: "", limit: HOME_MADE }).then(
      (r) => {
        if (!live) return;
        setMade(r.artifacts.slice(0, HOME_MADE));
        setMadeFailed(false);
      },
      () => {
        // What was read before stays; only a strip with nothing to show says it failed.
        if (live) setMadeFailed(true);
      },
    );
    return () => {
      live = false;
    };
  }, [tick, canArtifacts, via]);
  const failed = madeFailed && made.length === 0;
  if (made.length === 0 && !failed) return null;
  const c = defaultCopy.home;
  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 px-4 pt-6">
      {failed && <p className="text-center text-aui-xs text-aui-muted">{c.madeFailed}</p>}
      {made.length > 0 && (
        <section aria-labelledby="home-made">
          <h2 id="home-made" className="pb-2 font-medium text-aui-xs text-aui-muted">
            {c.recentlyMade}
          </h2>
          <ul className="grid gap-3 sm:grid-cols-3">
            {made.map((file) => {
              const to: Route = { page: "artifacts", artifact: file.id };
              return (
                <li key={file.id} className="min-w-0">
                  <ItemCard
                    title={file.title}
                    href={href(to)}
                    onOpen={() => go(to)}
                    time={<time dateTime={file.created_at}>{ago(file.created_at)}</time>}
                    facts={
                      <>
                        <Tag>{kindWord[file.kind]}</Tag>
                        {file.agent && <AgentTag name={file.agent} />}
                      </>
                    }
                  />
                </li>
              );
            })}
          </ul>
        </section>
      )}
    </div>
  );
}
