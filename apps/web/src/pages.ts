import { defaultCopy } from "@humagen/components";
import type { ComponentType } from "react";
import { lazyPage, lazyView, sparing, whenIdle } from "./lazy.tsx";
import type { Route } from "./route.ts";

/*
 * Every page Home does not show, read when it is opened (lazy.tsx). Home
 * keeps the shell, the sidebar, the composer, its greeting and the line of
 * what waits on the person; nothing here may be imported by those files
 * except through this one (entry-graph.test.ts checks). A page is named for
 * its loading state as its own header names it.
 */

const nav = defaultCopy.navigation;

export const ActionPolicies = lazyPage(
  () => import("./ActionPolicies.tsx"),
  (m) => m.ActionPolicies,
  { title: () => nav.policies },
);
export const AgentBuilderStart = lazyPage(
  () => import("./AgentBuilder.tsx"),
  (m) => m.AgentBuilderStart,
);
export const AgentCreate = lazyPage(
  () => import("./AgentCreate.tsx"),
  (m) => m.AgentCreate,
);
export const AgentControls = lazyPage(
  () => import("./AgentControls.tsx"),
  (m) => m.AgentControls,
  { title: () => nav.controls },
);
export const AgentDetail = lazyPage(
  () => import("./AgentDetail.tsx"),
  (m) => m.AgentDetail,
);
export const AgentNetwork = lazyPage(
  () => import("./AgentNetwork.tsx"),
  (m) => m.AgentNetwork,
  { title: () => nav.network },
);
export const AgentOperations = lazyPage(
  () => import("./AgentOperations.tsx"),
  (m) => m.AgentOperations,
  { frame: "rows" },
);
export const AgentStore = lazyPage(
  () => import("./AgentStore.tsx"),
  (m) => m.AgentStore,
  { title: () => defaultCopy.agentStore.title },
);
export const Agents = lazyPage(
  () => import("./Agents.tsx"),
  (m) => m.Agents,
  {
    title: () => nav.agents,
  },
);
export const Assignments = lazyPage(
  () => import("./Assignments.tsx"),
  (m) => m.Assignments,
  {
    title: () => nav.assignments,
  },
);
export const Audit = lazyPage(
  () => import("./Audit.tsx"),
  (m) => m.Audit,
  {
    title: () => defaultCopy.audit.title,
  },
);
export const Automations = lazyPage(
  () => import("./Automations.tsx"),
  (m) => m.Automations,
  {
    title: () => defaultCopy.automations.title,
  },
);
export const Catalog = lazyPage(
  () => import("./Catalog.tsx"),
  (m) => m.Catalog,
  {
    title: () => defaultCopy.catalog.title,
  },
);
export const Connections = lazyPage(
  () => import("./Connections.tsx"),
  (m) => m.Connections,
  {
    title: () => "Plugins",
  },
);
export const YourConnections = lazyPage(
  () => import("./Connections.tsx"),
  (m) => m.YourConnections,
  { title: () => nav.connections },
);
export const ContextHealth = lazyPage(
  () => import("./ContextHealth.tsx"),
  (m) => m.ContextHealth,
  { title: () => defaultCopy.sourceOperations.health },
);
export const CorpusPage = lazyPage(
  () => import("./Corpora.tsx"),
  (m) => m.CorpusPage,
);
export const KnowledgePage = lazyPage(
  () => import("./Corpora.tsx"),
  (m) => m.KnowledgePage,
  {
    title: () => defaultCopy.corpora.title,
  },
);
export const DirectoryPage = lazyPage(
  () => import("./Directory.tsx"),
  (m) => m.DirectoryPage,
  { title: () => defaultCopy.directory.title },
);
export const ForkProject = lazyPage(
  () => import("./ForkProject.tsx"),
  (m) => m.ForkProject,
  {
    frame: "none",
  },
);
export const Health = lazyPage(
  () => import("./Health.tsx"),
  (m) => m.Health,
  {
    frame: "rows",
  },
);
export const Integrations = lazyPage(
  () => import("./Integrations.tsx"),
  (m) => m.Integrations,
  { title: () => nav.integrations },
);
export const ItemPage = lazyPage(
  () => import("./ItemPage.tsx"),
  (m) => m.ItemPage,
);
export const Knowledge = lazyPage(
  () => import("./Knowledge.tsx"),
  (m) => m.Knowledge,
);
export const MemoryLibrary = lazyPage(
  () => import("./MemoryLibrary.tsx"),
  (m) => m.MemoryLibrary as ComponentType<NonNullable<Parameters<typeof m.MemoryLibrary>[0]>>,
  { title: () => nav.memory },
);
export const MessageFocus = lazyPage(
  () => import("./MessageFocus.tsx"),
  (m) => m.MessageFocus,
);
export const Monitor = lazyPage(
  () => import("./Monitor.tsx"),
  (m) => m.Monitor,
);
export const NotificationsPage = lazyPage(
  () => import("./Notifications.tsx"),
  (m) => m.NotificationsPage,
  { title: () => defaultCopy.notifications.title },
);
export const PersonalPreferences = lazyPage(
  () => import("./PersonalPreferences.tsx"),
  (m) => m.PersonalPreferences,
);
export const ProjectPage = lazyPage(
  () => import("./Projects.tsx"),
  (m) => m.ProjectPage,
);
export const ProjectsPage = lazyPage(
  () => import("./Projects.tsx"),
  (m) => m.ProjectsPage,
  {
    title: () => defaultCopy.projects.title,
  },
);
export const QuickSwitcher = lazyPage(
  () => import("./QuickSwitcher.tsx"),
  (m) => m.QuickSwitcher,
  { frame: "none" },
);
export const Schedules = lazyPage(
  () => import("./Schedules.tsx"),
  (m) => m.Schedules,
);
export const Settings = lazyPage(
  () => import("./Settings.tsx"),
  (m) => m.Settings,
);
export const SettingsFrame = lazyPage(
  () => import("./Settings.tsx"),
  (m) => m.SettingsFrame,
);
export const SkillsLibrary = lazyPage(
  () => import("./SkillsLibrary.tsx"),
  (m) => m.SkillsLibrary as ComponentType<NonNullable<Parameters<typeof m.SkillsLibrary>[0]>>,
  { title: () => nav.skills },
);
export const Storage = lazyPage(
  () => import("./Storage.tsx"),
  (m) => m.Storage,
  {
    title: () => nav.artifacts,
  },
);
export const Usage = lazyPage(
  () => import("./Usage.tsx"),
  (m) => m.Usage,
  {
    title: () => nav.usage,
  },
);
export const WorkspaceGraph = lazyPage(
  () => import("./WorkspaceGraph.tsx"),
  (m) => m.WorkspaceGraph,
);

// The composer's light, which an idle bar does not show (ComposerLight.tsx).
export const VoiceLevel = lazyView(
  () => import("./ComposerLight.tsx"),
  (m) => m.VoiceLevel,
);
export const SendRing = lazyView(
  () => import("./ComposerLight.tsx"),
  (m) => m.SendRing,
);

// A conversation's two views. Chat frames them itself, in the conversation's own loading state.
export const ThreadRoom = lazyView(
  () => import("./Room.tsx"),
  (m) => m.ThreadRoom,
);
export const RunsConversation = lazyView(
  () => import("./ChatConversation.tsx"),
  (m) => m.RunsConversation,
);

type Preloads = { preload: () => Promise<void> }[];

/** The pages an address opens, by its page: what to read ahead when a link to it is pointed at. */
function viewsOf(route: Route): Preloads {
  switch (route.page) {
    case "conversation":
      return [ThreadRoom];
    case "projects":
      return [ProjectsPage];
    case "project":
      return [ProjectPage];
    case "agents":
      return [Agents];
    case "agent-store":
      return [AgentStore];
    case "agent":
      return [AgentDetail, AgentOperations];
    case "agent-setup":
      return [AgentBuilderStart];
    case "agent-create":
      return [AgentCreate];
    case "automations":
      return [Automations];
    case "schedules":
      return [Schedules];
    case "artifacts":
      return [Storage];
    case "settings":
      return [Settings, SettingsFrame];
    case "you":
      return [PersonalPreferences];
    case "connections":
      return [Connections];
    case "knowledge":
    case "corpus":
      return [KnowledgePage];
    case "knowledge-graph":
      return [WorkspaceGraph];
    case "context":
      return [Knowledge];
    case "inbox":
    case "notifications":
      return [NotificationsPage];
    case "monitor":
      return [Monitor, Health];
    case "usage":
      return [Usage, SettingsFrame];
    case "audit":
      return [Audit, SettingsFrame];
    case "directory":
      return [DirectoryPage, SettingsFrame];
    case "action-policy":
      return [ActionPolicies, SettingsFrame];
    case "agent-control":
      return [AgentControls, SettingsFrame];
    case "agent-network":
      return [AgentNetwork];
    case "assignments":
      return [Assignments];
    case "operations":
      return [Integrations, ContextHealth];
    case "library":
      return [SkillsLibrary, MemoryLibrary, Catalog, Storage, Knowledge];
    case "customize":
      return [SkillsLibrary, Agents, Connections];
    case "message":
      return [MessageFocus];
    case "item":
      return [ItemPage];
    default:
      return [];
  }
}

/** Read the code of the page at an address: the one being opened, beside its data. */
export function preloadRoute(route: Route): void {
  for (const view of viewsOf(route)) void view.preload();
}

/**
 * Read ahead the page a link leads to, when it is pointed at or focused.
 * Not for a person saving data or on a slow connection: pointing is not asking.
 */
export function preloadLink(route: Route): void {
  if (!sparing()) preloadRoute(route);
}

/**
 * Read ahead what the sidebar leads to and the search, once Home is on
 * screen and the browser is idle, so going there waits on nothing. A person
 * saving data, or on a slow connection, is not sent what they did not ask for.
 */
export function preloadWhenIdle(): () => void {
  if (sparing()) return () => {};
  return whenIdle(() => {
    for (const view of [
      ThreadRoom,
      QuickSwitcher,
      ProjectsPage,
      Agents,
      Automations,
      Storage,
      Settings,
    ])
      void view.preload();
  });
}

/**
 * A new session's environment and repository pickers, in the composer's
 * footer on Home: read when Home draws them, so their data code is not Home's.
 */
export const SessionSetupPickers = lazyPage(
  () => import("./SessionPickers.tsx"),
  (m) => m.SessionSetupPickers,
  { frame: "none" },
);

/** The composer's + menu, read when it is first opened (Plus.tsx). */
export const PlusMenu = lazyPage(
  () => import("./Plus.tsx"),
  (m) => m.PlusMenu,
  { frame: "none" },
);
