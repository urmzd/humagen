import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AgentDetail } from "./AgentDetail.tsx";
import { Agents } from "./Agents.tsx";
import {
  type Alert,
  type AlertRule,
  ApiError,
  api,
  type Me,
  type Preferences,
  type ProjectView,
  type Revision,
  type ScheduleView,
  type Settings as Workspace,
  type Source,
  type SuiteView,
  type Summary,
  subscribe,
  type ThreadSummary,
} from "./api.ts";
import type { Run } from "./adapter.ts";
import { Chat } from "./Chat.tsx";
import { DirectoryPage } from "./Directory.tsx";
import { InboxPage } from "./InboxPage.tsx";
import { Knowledge } from "./Knowledge.tsx";
import { Monitor } from "./Monitor.tsx";
import { ProjectPage, ProjectsPage } from "./Projects.tsx";
import { Schedules } from "./Schedules.tsx";
import { Settings } from "./Settings.tsx";
import { Storage } from "./Storage.tsx";
import { DrawerButton, Sidebar } from "./Sidebar.tsx";
import { updatesOf } from "@warp/adapter";
import { HomeLines } from "./HomePage.tsx";
import { type Source as About, type PlatformMessage, transport as mail } from "./mail.ts";
import { type AgentTab, href, kindParam, type ProjectTab, type Route, type SettingsSection, useRoute } from "./route.ts";
import { Page, primary } from "./ui.tsx";

type Session = { state: "loading" } | { state: "signed-out" } | { state: "error"; message: string } | { state: "ready"; me: Me };

/** Where sign-in starts, coming back to this page after. */
function signInUrl(): string {
  return `/auth/login?return_to=${encodeURIComponent(window.location.pathname + window.location.search)}`;
}

export function App() {
  const [session, setSession] = useState<Session>({ state: "loading" });

  useEffect(() => {
    api
      .me()
      // A caller with no permission at all is sent null, not an empty list.
      .then((me) => setSession({ state: "ready", me: { ...me, permissions: me.permissions ?? [] } }))
      .catch((e) => {
        if (e instanceof ApiError && e.status === 401) {
          // Arriving signed out goes straight to sign-in. Signing out, below,
          // does not, or a provider that remembers the person would sign them
          // straight back in.
          window.location.assign(signInUrl());
          return;
        }
        setSession({ state: "error", message: "The platform is not reachable. Is the server running?" });
      });
  }, []);

  if (session.state === "loading") return <Centered>Loading</Centered>;
  if (session.state === "error") return <Centered>{session.message}</Centered>;
  if (session.state === "signed-out") {
    return (
      <Centered>
        <h1 className="text-aui-lg font-medium text-aui-fg">Warp</h1>
        <p className="text-aui-sm text-aui-muted">You are signed out.</p>
        <a className={primary} href={signInUrl()}>
          Sign In
        </a>
      </Centered>
    );
  }
  return <Console me={session.me} onSignedOut={() => setSession({ state: "signed-out" })} />;
}

function Centered({ children }: { children: React.ReactNode }) {
  return (
    <main className="flex h-full flex-col items-center justify-center gap-3 px-6 text-center text-aui-sm text-aui-muted">
      {children}
    </main>
  );
}

const SIDEBAR_KEY = "warp.sidebar";

/** The platform's own agents the console offers ways into. */
export const BUILTIN_ANALYST = "warp-analyst";
export const BUILTIN_BUILDER = "warp-builder";

/** A per-viewer convenience, so it lives in the browser and must never throw. */
function stored(key: string, fallback: boolean): boolean {
  try {
    const v = window.localStorage.getItem(key);
    return v === null ? fallback : v === "1";
  } catch {
    return fallback;
  }
}

function greeting(name: string): string {
  const h = new Date().getHours();
  const part = h < 12 ? "Good morning" : h < 18 ? "Good afternoon" : "Good evening";
  return name ? `${part}, ${name.split(" ")[0]}` : part;
}

/** Apply a person's theme and accent to the page. System follows the device. */
function useAppearance(prefs: Preferences | null) {
  useEffect(() => {
    const root = document.documentElement;
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const apply = () => {
      const theme = prefs?.theme ?? "system";
      root.dataset.theme = theme === "system" ? (media.matches ? "dark" : "light") : theme;
      root.dataset.accent = prefs?.accent ?? "purple";
    };
    apply();
    media.addEventListener("change", apply);
    return () => media.removeEventListener("change", apply);
  }, [prefs?.theme, prefs?.accent]);
}

function Console({ me, onSignedOut }: { me: Me; onSignedOut: () => void }) {
  const can = useCallback((p: string) => me.permissions.includes(p), [me]);
  const [route, go] = useRoute();
  const page = route.page;
  const thread = route.page === "task" ? route.id : null;
  // The inbox is the server's. This holds only its totals for the badge, a
  // counter that tells the page to look again, and a message to open first.
  const [inboxCounts, setInboxCounts] = useState({ open: 0, unread: 0 });
  const [inboxTick, setInboxTick] = useState(0);
  // The project tree, null until it is read, and the workspace's own view, which heads it.
  const [projects, setProjects] = useState<ProjectView[] | null>(null);
  const [root, setRoot] = useState<ProjectView | null>(null);
  const [open, setOpen] = useState(() => stored(SIDEBAR_KEY, window.innerWidth >= 768));
  const [runs, setRuns] = useState<Run[]>([]);
  const [threads, setThreads] = useState<ThreadSummary[]>([]);
  const [agents, setAgents] = useState<Revision[]>([]);
  // The agents the platform provides. They run like any other and cannot be edited.
  const [builtin, setBuiltin] = useState<string[]>([]);
  // Where each agent that lives in a project lives, and which projects enable each.
  const [homes, setHomes] = useState<Record<string, string>>({});
  const [enabledIn, setEnabledIn] = useState<Record<string, string[]>>({});
  // Who answers a message that tags no agent in the workspace itself: the Workspace Default.
  const [workspaceDefault, setWorkspaceDefault] = useState("");
  const [schedules, setSchedules] = useState<ScheduleView[]>([]);
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [rules, setRules] = useState<AlertRule[]>([]);
  const [sources, setSources] = useState<Source[]>([]);
  const [suites, setSuites] = useState<SuiteView[]>([]);
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [prefs, setPrefs] = useState<Preferences>({
    display_name: "",
    theme: "system",
    accent: "purple",
    has_photo: false,
    photo_version: 0,
    pinned: [],
    muted: [],
  });
  // The reader's own open announcements, for What's new on Home.
  const [announced, setAnnounced] = useState<PlatformMessage[]>([]);
  const [day, setDay] = useState<Summary | null>(null);
  const [live, setLive] = useState(false);
  // Bumped whenever a run starts or ends, so pages that aggregate refetch.
  const [tick, setTick] = useState(0);

  useAppearance(prefs);

  // On a phone the sidebar is a drawer: going somewhere from it puts it away.
  const navigate = (next: Route) => {
    go(next);
    if (window.innerWidth < 768) setOpen(false);
  };

  const toggle = () =>
    setOpen((was) => {
      try {
        window.localStorage.setItem(SIDEBAR_KEY, was ? "0" : "1");
      } catch {
        // The choice just does not persist.
      }
      return !was;
    });

  const loadAgents = useCallback(() => {
    if (!can("agents:read")) return;
    api
      .agentList()
      .then((r) => {
        setAgents(r.agents);
        setBuiltin(r.builtin ?? []);
        setHomes(r.homes ?? {});
        setEnabledIn(r.enabled_in ?? {});
      })
      .catch(() => {});
    api.settings().then(setWorkspace).catch(() => {});
    if (can("projects:read")) {
      api
        .projectAgents("")
        .then((v) => setWorkspaceDefault(v.default ?? ""))
        .catch(() => {});
    }
  }, [can]);
  const loadInbox = useCallback(() => {
    if (!can("runs:read")) return;
    api.inboxCount().then(setInboxCounts).catch(() => {});
    if (can("projects:read"))
      mail
        .list({ limit: 20, state: "open", kind: "announcement" })
        .then((page) => setAnnounced(page.messages))
        .catch(() => {});
    setInboxTick((t) => t + 1);
    // Settling a message settles its alert, which the Alerts badge counts.
    if (can("alerts:read")) api.alerts().then(setAlerts).catch(() => {});
  }, [can]);
  const loadProjects = useCallback(() => {
    if (!can("projects:read")) return;
    api.projects().then(setProjects).catch(() => setProjects((was) => was ?? []));
    api
      .project("")
      .then((d) => setRoot(d.project))
      .catch(() => {});
  }, [can]);
  const loadSources = useCallback(() => {
    if (can("knowledge:read")) api.sources().then(setSources).catch(() => {});
  }, [can]);
  const loadSuites = useCallback(() => {
    if (can("evals:read")) api.suites().then(setSuites).catch(() => {});
  }, [can]);
  const loadAlerts = useCallback(() => {
    if (!can("alerts:read")) return;
    api
      .alerts()
      .then(setAlerts)
      .catch(() => {});
    api.alertRules().then(setRules).catch(() => {});
  }, [can]);
  const loadSchedules = useCallback(() => {
    if (can("schedules:read")) api.schedules().then(setSchedules).catch(() => {});
  }, [can]);
  // Everything a run starting or ending can change.
  const loadRuns = useCallback(() => {
    if (!can("runs:read")) return;
    api.runs().then(setRuns).catch(() => {});
    api.threads().then(setThreads).catch(() => {});
    api.analytics("24h").then(setDay).catch(() => {});
    loadAlerts();
    loadSchedules();
    loadSuites();
    loadInbox();
    loadProjects();
    setTick((t) => t + 1);
  }, [can, loadAlerts, loadSchedules, loadSuites, loadInbox, loadProjects]);

  useEffect(() => {
    loadRuns();
    loadAgents();
    loadSources();
    // Preferences are optional: render immediately with defaults while they load.
    api.preferences().then(setPrefs).catch(() => {});
  }, [loadRuns, loadAgents, loadSources]);

  // One stream for the whole tenant keeps every list current. Lifecycle and
  // control events are rare next to text deltas, so the reload is coalesced
  // rather than run per event.
  const pending = useRef<number | undefined>(undefined);
  useEffect(() => {
    if (!can("runs:read")) return;
    const close = subscribe("agent", {
      onOpen: loadRuns,
      onState: setLive,
      onInbox: loadInbox,
      onEvent: (e) => {
        if (!e.kind.startsWith("lifecycle.") && !e.kind.startsWith("control.")) return;
        if (e.kind.startsWith("control.")) loadAgents();
        window.clearTimeout(pending.current);
        pending.current = window.setTimeout(loadRuns, 150);
      },
    });
    return () => {
      window.clearTimeout(pending.current);
      close();
    };
  }, [can, loadRuns, loadAgents, loadInbox]);

  // An eval in flight reports progress on its own record, not on the bus.
  const evalRunning = suites.some((s) => s.runs[0]?.status === "running");
  useEffect(() => {
    if (!evalRunning) return;
    const timer = window.setInterval(loadSuites, 700);
    return () => window.clearInterval(timer);
  }, [evalRunning, loadSuites]);

  const openThread = (id: string) => go({ page: "task", id });
  // A built-in can be asked when the workspace has it and the reader may start runs.
  const canAsk = (name: string) => can("runs:write") && agents.some((a) => a.spec.name === name);
  // An alert's link names the alert. What fired is resolved in the inbox, so it opens the message the alert raised.
  const alertLinked = route.page === "inbox" ? route.alert : undefined;
  // biome-ignore lint/correctness/useExhaustiveDependencies: once per linked alert
  useEffect(() => {
    if (!alertLinked) return;
    api
      .inboxFor(alertLinked)
      .then((message) => go({ page: "inbox", message: message ?? undefined, kind: message ? undefined : "alert" }, { replace: true }))
      .catch(() => go({ page: "inbox", kind: "alert" }, { replace: true }));
  }, [alertLinked]);
  // Opening a run opens the task it belongs to, where its history is kept.
  const openRun = (id: string) => {
    api
      .run(id)
      .then((r) => openThread(r.thread ?? id))
      .catch(() => openThread(id));
  };
  // A new conversation is home: the same empty composer, at the bare address.
  const newChat = (inProject?: string) => go({ page: "new", project: inProject });
  // "~" is how an address or a message names the workspace, which is the project "".
  // A project opens on its inbox.
  const openProject = (name: string, tab: ProjectTab = "inbox") => go({ page: "project", name: name === "~" ? "" : name, tab });
  const projectNames = useMemo(() => (projects ?? []).map((p) => p.name), [projects]);
  const workspaceName = workspace?.workspace_name || me.principal.tenant;
  const openAgent = (name: string | null, tab: AgentTab = "prompt") => go({ page: "agent", name, tab });
  const openSettings = (section: SettingsSection) => go({ page: "settings", section });
  // Pinning shows at once and is saved with the person's preferences. If the
  // save fails, the sidebar goes back to what the server has.
  const pin = (id: string, on: boolean) => {
    const next = on ? [id, ...prefs.pinned.filter((p) => p !== id)] : prefs.pinned.filter((p) => p !== id);
    const before = prefs;
    setPrefs({ ...prefs, pinned: next });
    api
      .savePreferences({ display_name: prefs.display_name, theme: prefs.theme, accent: prefs.accent, pinned: next })
      .then(setPrefs)
      .catch(() => setPrefs(before));
  };
  // What a message is about, opened where it lives.
  const openSource = (source: About) =>
    source.kind === "run"
      ? openRun(source.id)
      : source.kind === "thread"
        ? openThread(source.id)
        : source.kind === "agent"
          ? openAgent(source.id)
          : source.kind === "project"
            ? openProject(source.id)
            : source.kind === "artifact"
              ? go({ page: "artifacts", artifact: source.id })
              : go({ page: "monitor", tab: "rules" });
  // Muting shows at once and is saved with the person's preferences.
  const mute = (project: string, on: boolean) => {
    const next = on ? [...prefs.muted.filter((p) => p !== project), project] : prefs.muted.filter((p) => p !== project);
    const before = prefs;
    setPrefs({ ...prefs, muted: next });
    api
      .savePreferences({ display_name: prefs.display_name, theme: prefs.theme, accent: prefs.accent, muted: next })
      .then(setPrefs)
      .catch(() => setPrefs(before));
  };
  const stats = useMemo(() => new Map((day?.by_agent ?? []).map((g) => [g.name, g])), [day]);
  const agentName = route.page === "agent" ? route.name : null;
  const editing = agents.find((a) => a.spec.name === agentName) ?? null;
  const shownName = prefs?.display_name || me.principal.name || "";
  const updates = useMemo(() => {
    const titles = new Map((projects ?? []).map((p) => [p.name, p.title || p.name]));
    return updatesOf(
      announced,
      (name) => (name === "" ? workspaceName : (titles.get(name) ?? name)),
      (name, id) => href({ page: "project", name, tab: "inbox", message: id }),
    );
  }, [announced, projects, workspaceName]);

  return (
    <div className="flex h-full text-aui-fg">
      <Sidebar
        open={open}
        onToggle={toggle}
        route={route}
        go={navigate}
        me={me}
        prefs={prefs}
        can={can}
        threads={threads}
        pinned={prefs.pinned}
        onPin={pin}
        inboxCount={inboxCounts.open}
        live={live}
        onSignOut={() => api.logout().then(onSignedOut).catch(onSignedOut)}
      />
      <DrawerButton open={open} inboxCount={inboxCounts.open} onOpen={() => setOpen(true)} />
      {/* On a phone the drawer button sits in the corner, so the page starts below it. */}
      <main className="flex min-h-0 min-w-0 flex-1 flex-col max-md:pt-[calc(2.5rem+env(safe-area-inset-top))]">
        {(page === "new" || page === "task") && (
          <Chat
            key={thread ?? `new:${route.page === "new" ? `${route.project ?? ""}:${route.agent ?? ""}:${route.ask ?? ""}` : ""}`}
            threadId={thread}
            startIn={route.page === "new" ? route.project : undefined}
            startAgent={route.page === "new" ? route.agent : undefined}
            startAsk={route.page === "new" ? route.ask : undefined}
            summary={threads.find((t) => t.id === thread)}
            tick={tick}
            agents={agents}
            homes={homes}
            projects={can("projects:read") ? projects : []}
            canRun={can("runs:write")}
            canCreateProject={can("projects:write")}
            greeting={greeting(shownName)}
            home={
              <HomeLines
                needsYou={inboxCounts.open}
                updates={updates}
                onInbox={() => go({ page: "inbox" })}
                onUpdate={(u) => go({ page: "project", name: u.project, tab: "inbox", message: u.id })}
              />
            }
            onProjectCreated={(project) => {
              setProjects((current) => [...(current ?? []).filter((p) => p.name !== project.name), project]);
              loadProjects();
            }}
            onThread={(id) => go({ page: "task", id }, { replace: route.page === "new" })}
            onChanged={loadRuns}
          />
        )}
        {route.page === "projects" && (
          <ProjectsPage
            projects={projects}
            root={root}
            workspaceName={workspaceName}
            q={route.q ?? ""}
            kind={route.kind}
            onFilter={(q, kind) =>
              go({ page: "projects", q: q || undefined, kind: kindParam(kind) }, { replace: true })
            }
            canCreate={can("projects:write")}
            canGovern={can("projects:govern")}
            onOpen={openProject}
            onCreated={(project) => {
              loadProjects();
              openProject(project.name);
            }}
          />
        )}
        {route.page === "project" && (
          <ProjectPage
            key={route.name}
            name={route.name}
            tab={route.tab}
            onTab={(tab) => go({ page: "project", name: route.name, tab }, { replace: true })}
            message={route.message}
            onMessage={(id) => go({ page: "project", name: route.name, tab: "inbox", message: id ?? undefined }, { replace: true })}
            me={me.principal.subject}
            inboxTick={inboxTick}
            muted={prefs.muted}
            onMute={mute}
            onOpenSource={openSource}
            workspaceName={workspaceName}
            projects={projects ?? []}
            schedules={schedules}
            agents={agents}
            homes={homes}
            enabledIn={enabledIn}
            can={can}
            tick={tick}
            onOpen={openProject}
            onOpenAll={() => go({ page: "projects" })}
            onNewChat={() => newChat(route.name || undefined)}
            onOpenThread={openThread}
            onChanged={() => {
              loadProjects();
              loadInbox();
            }}
            onSchedulesChanged={loadSchedules}
            onAgentsChanged={loadAgents}
            onDeleted={() => {
              loadProjects();
              go({ page: "projects" }, { replace: true });
            }}
          />
        )}
        {route.page === "schedules" && (
          <Schedules
            schedules={schedules}
            agents={agents}
            projects={projectNames}
            selected={route.name ?? null}
            onSelect={(name) => go({ page: "schedules", name: name ?? undefined })}
            canWrite={can("schedules:write")}
            onChanged={loadSchedules}
            onOpenRun={openRun}
            onOpenAgent={(name) => openAgent(name)}
          />
        )}
        {route.page === "monitor" && (
          <Monitor
            tab={route.tab}
            onTab={(tab) => go({ page: "monitor", tab })}
            alerts={alerts}
            rules={rules}
            agents={agents}
            projects={projectNames}
            canWrite={can("alerts:write")}
            canAnalytics={can("runs:read")}
            tick={tick}
            onChanged={loadAlerts}
            onOpenRun={openRun}
            onOpenFired={can("runs:read") ? () => go({ page: "inbox", kind: "alert" }) : undefined}
            onOpenAlert={can("runs:read") ? (alert) => go({ page: "inbox", alert: alert.id }) : undefined}
            onAsk={canAsk(BUILTIN_ANALYST) ? (ask) => go({ page: "new", agent: BUILTIN_ANALYST, ask }) : undefined}
          />
        )}
        {route.page === "inbox" && (
          <InboxPage
            signal={inboxTick}
            viewer={shownName}
            openId={route.message ?? null}
            kind={route.kind ?? ""}
            onKind={(kind) => go({ page: "inbox", kind: kind || undefined }, { replace: true })}
            onCounts={setInboxCounts}
            onOpen={openSource}
          />
        )}
        {route.page === "context" && (
          <Knowledge
            sources={sources}
            agents={agents}
            suites={suites}
            selected={route.source ?? null}
            tab={route.tab ?? "documents"}
            onSelect={(source, tab) => go({ page: "context", source: source ?? undefined, tab })}
            canWrite={can("knowledge:write")}
            canEval={can("evals:write")}
            onChanged={loadSources}
            onSuitesChanged={loadSuites}
            onOpenAgent={(name) => openAgent(name, "context")}
          />
        )}
        {route.page === "agents" && (
          <Agents
            builtin={builtin}
            agents={agents}
            runs={runs}
            stats={stats}
            workspaceDefault={workspaceDefault}
            homes={homes}
            canWrite={can("agents:write")}
            onOpen={(name) => openAgent(name)}
            onNew={() => openAgent(null)}
            onBuild={canAsk(BUILTIN_BUILDER) ? () => go({ page: "new", agent: BUILTIN_BUILDER, ask: "Help me build an agent that " }) : undefined}
          />
        )}
        {route.page === "agent" &&
          (route.name !== null && agents.length > 0 && !editing ? (
            <Missing what="agent" name={route.name} onBack={() => go({ page: "agents" })} />
          ) : (
            <AgentDetail
              key={route.name ?? "new"}
              revision={editing}
              tab={route.tab}
              onTab={(tab) => go({ page: "agent", name: route.name, tab }, { replace: true })}
              agents={agents}
              sources={sources}
              suites={suites}
              rules={rules}
              alerts={alerts}
              projects={projectNames}
              tree={projects ?? []}
              place={route.name ? { home: homes[route.name], enabled_in: enabledIn[route.name] ?? [] } : undefined}
              workspaceName={workspaceName}
              stats={route.name ? stats.get(route.name) : undefined}
              workspaceDefault={workspaceDefault}
              canWrite={can("agents:write")}
              canGovern={can("projects:govern")}
              builtin={route.name ? builtin.includes(route.name) : false}
              canEval={can("evals:write")}
              canAlert={can("alerts:write")}
              canKnowledge={can("knowledge:write")}
              onBack={() => go({ page: "agents" })}
              onChanged={(name) => {
                loadAgents();
                go({ page: "agent", name, tab: route.tab }, { replace: true });
              }}
              onRulesChanged={loadAlerts}
              onSuitesChanged={loadSuites}
              onSourcesChanged={loadSources}
              onOpenRun={openRun}
              onOpenSource={(source) => go({ page: "context", source })}
              onOpenAlerts={() => go({ page: "monitor", tab: "rules" })}
              onOpenTask={openThread}
              onHomed={loadAgents}
              onOpenProject={openProject}
              policies={workspace?.model_policies ?? []}
              onManagePolicies={() => openSettings("models")}
            />
          ))}
        {route.page === "directory" && (
          <DirectoryPage
            tab={route.tab}
            onTab={(tab) => go({ page: "directory", tab }, { replace: true })}
            can={can}
            agents={agents}
            homes={homes}
            enabledIn={enabledIn}
            projects={projects}
            workspaceName={workspaceName}
            onOpenAgent={(name) => openAgent(name, "access")}
            onOpenProject={openProject}
          />
        )}
        {route.page === "artifacts" && (
          <Storage
            signal={tick}
            agents={agents.map((a) => a.spec.name)}
            projects={projectNames}
            canDelete={can("artifacts:write")}
            onOpenRun={openRun}
            onOpenThread={openThread}
            selectedId={route.artifact ?? null}
            onSelect={(artifact) => go({ page: "artifacts", artifact: artifact ?? undefined })}
          />
        )}
        {route.page === "settings" && (
          <Settings
            me={me}
            prefs={prefs}
            workspace={workspace}
            agents={agents}
            canAdmin={can("agents:write")}
            section={route.section}
            onSection={openSettings}
            explain={route.permission ?? "runs:read"}
            onExplain={(permission) => go({ page: "settings", section: "access", permission }, { replace: true })}
            onPrefs={setPrefs}
            onWorkspace={setWorkspace}
            projects={projectNames}
            onOpenProjects={() => go({ page: "projects" })}
            onOpenWorkspace={() => openProject("")}
            onOpenWorkspaceAgents={() => go({ page: "project", name: "", tab: "agents" })}
          />
        )}
      </main>
    </div>
  );
}

/** A link to something that is not there: deleted, renamed, or never readable. */
function Missing({ what, name, onBack }: { what: string; name: string; onBack: () => void }) {
  return (
    <Page title={`No ${what} named ${name}`} lede={`It may have been deleted, or you may not be allowed to see it.`} back={{ label: `All ${what === "agent" ? "agents" : "projects"}`, onClick: onBack }}>
      {null}
    </Page>
  );
}
