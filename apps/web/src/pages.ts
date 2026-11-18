import { defaultCopy } from "@humagen/components";
import { lazyPage, lazyView, whenIdle } from "./lazy.tsx";
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
  () => import("./ActionPolicies.tsx").then((m) => m.ActionPolicies),
  { title: () => nav.policies },
);
export const AgentBuilderStart = lazyPage(() =>
  import("./AgentBuilder.tsx").then((m) => m.AgentBuilderStart),
);
export const AgentControls = lazyPage(
  () => import("./AgentControls.tsx").then((m) => m.AgentControls),
  { title: () => nav.controls },
);
export const AgentDetail = lazyPage(() => import("./AgentDetail.tsx").then((m) => m.AgentDetail));
export const AgentNetwork = lazyPage(
  () => import("./AgentNetwork.tsx").then((m) => m.AgentNetwork),
  { title: () => nav.network },
);
export const AgentOperations = lazyPage(
  () => import("./AgentOperations.tsx").then((m) => m.AgentOperations),
  { frame: "rows" },
);
export const Agents = lazyPage(() => import("./Agents.tsx").then((m) => m.Agents), {
  title: () => nav.agents,
});
export const Assignments = lazyPage(() => import("./Assignments.tsx").then((m) => m.Assignments), {
  title: () => nav.assignments,
});
export const Audit = lazyPage(() => import("./Audit.tsx").then((m) => m.Audit), {
  title: () => defaultCopy.audit.title,
});
export const Automations = lazyPage(() => import("./Automations.tsx").then((m) => m.Automations), {
  title: () => defaultCopy.automations.title,
});
export const Catalog = lazyPage(() => import("./Catalog.tsx").then((m) => m.Catalog), {
  title: () => defaultCopy.catalog.title,
});
export const Connections = lazyPage(() => import("./Connections.tsx").then((m) => m.Connections), {
  title: () => "Connections",
});
export const YourConnections = lazyPage(
  () => import("./Connections.tsx").then((m) => m.YourConnections),
  { title: () => nav.connections },
);
export const ContextHealth = lazyPage(
  () => import("./ContextHealth.tsx").then((m) => m.ContextHealth),
  { title: () => defaultCopy.sourceOperations.health },
);
export const CorpusPage = lazyPage(() => import("./Corpora.tsx").then((m) => m.CorpusPage));
export const KnowledgePage = lazyPage(() => import("./Corpora.tsx").then((m) => m.KnowledgePage), {
  title: () => defaultCopy.corpora.title,
});
export const DirectoryPage = lazyPage(
  () => import("./Directory.tsx").then((m) => m.DirectoryPage),
  { title: () => defaultCopy.directory.title },
);
export const ForkProject = lazyPage(() => import("./ForkProject.tsx").then((m) => m.ForkProject), {
  frame: "none",
});
export const Health = lazyPage(() => import("./Health.tsx").then((m) => m.Health), {
  frame: "rows",
});
export const Integrations = lazyPage(
  () => import("./Integrations.tsx").then((m) => m.Integrations),
  { title: () => nav.integrations },
);
export const ItemPage = lazyPage(() => import("./ItemPage.tsx").then((m) => m.ItemPage));
export const Knowledge = lazyPage(() => import("./Knowledge.tsx").then((m) => m.Knowledge));
export const MemoryLibrary = lazyPage<
  NonNullable<Parameters<typeof import("./MemoryLibrary.tsx").MemoryLibrary>[0]>
>(() => import("./MemoryLibrary.tsx").then((m) => m.MemoryLibrary), { title: () => nav.memory });
export const MessageFocus = lazyPage(() =>
  import("./MessageFocus.tsx").then((m) => m.MessageFocus),
);
export const Monitor = lazyPage(() => import("./Monitor.tsx").then((m) => m.Monitor));
export const NotificationsPage = lazyPage(
  () => import("./Notifications.tsx").then((m) => m.NotificationsPage),
  { title: () => defaultCopy.notifications.title },
);
export const PersonalPreferences = lazyPage(() =>
  import("./PersonalPreferences.tsx").then((m) => m.PersonalPreferences),
);
export const ProjectPage = lazyPage(() => import("./Projects.tsx").then((m) => m.ProjectPage));
export const ProjectsPage = lazyPage(() => import("./Projects.tsx").then((m) => m.ProjectsPage), {
  title: () => defaultCopy.projects.title,
});
export const QuickSwitcher = lazyPage(
  () => import("./QuickSwitcher.tsx").then((m) => m.QuickSwitcher),
  { frame: "none" },
);
export const Schedules = lazyPage(() => import("./Schedules.tsx").then((m) => m.Schedules));
export const Settings = lazyPage(() => import("./Settings.tsx").then((m) => m.Settings));
export const SettingsFrame = lazyPage(() => import("./Settings.tsx").then((m) => m.SettingsFrame));
export const SkillsLibrary = lazyPage<
  NonNullable<Parameters<typeof import("./SkillsLibrary.tsx").SkillsLibrary>[0]>
>(() => import("./SkillsLibrary.tsx").then((m) => m.SkillsLibrary), { title: () => nav.skills });
export const Storage = lazyPage(() => import("./Storage.tsx").then((m) => m.Storage), {
  title: () => nav.artifacts,
});
export const Usage = lazyPage(() => import("./Usage.tsx").then((m) => m.Usage), {
  title: () => nav.usage,
});
export const WorkspaceGraph = lazyPage(() =>
  import("./WorkspaceGraph.tsx").then((m) => m.WorkspaceGraph),
);

// The composer's light, which an idle bar does not show (ComposerLight.tsx).
export const WorkingBeam = lazyPage(
  () => import("./ComposerLight.tsx").then((m) => m.WorkingBeam),
  { frame: "none" },
);
export const VoiceLevel = lazyPage(() => import("./ComposerLight.tsx").then((m) => m.VoiceLevel), {
  frame: "none",
});
export const SendRing = lazyView(() => import("./ComposerLight.tsx").then((m) => m.SendRing));

// A conversation's two views. Chat frames them itself, in the conversation's own loading state.
export const ThreadRoom = lazyView(() => import("./Room.tsx").then((m) => m.ThreadRoom));
export const RunsConversation = lazyView(() =>
  import("./ChatConversation.tsx").then((m) => m.RunsConversation),
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
    case "agent":
      return [AgentDetail, AgentOperations];
    case "agent-setup":
      return [AgentBuilderStart];
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

/** Read ahead the code of the page an address opens. */
export function preloadRoute(route: Route): void {
  for (const view of viewsOf(route)) void view.preload();
}

/**
 * Read ahead what the sidebar leads to and the search, once Home is on
 * screen and the browser is idle, so going there waits on nothing. A person
 * who asked their browser to save data is not sent what they did not ask for.
 */
export function preloadWhenIdle(): () => void {
  const saving = (navigator as Navigator & { connection?: { saveData?: boolean } }).connection
    ?.saveData;
  if (saving) return () => {};
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
