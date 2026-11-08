import { defaultCopy } from "@humagen/components";
import {
  BellIcon,
  BotIcon,
  BrainIcon,
  CheckCircleIcon,
  ClipboardListIcon,
  GaugeIcon,
  ListChecksIcon,
  MonitorIcon,
  MoonIcon,
  PaletteIcon,
  PauseIcon,
  PlugIcon,
  ScrollTextIcon,
  ServerIcon,
  ShieldIcon,
  SlidersIcon,
  SunIcon,
  TrashIcon,
  UserIcon,
  UsersIcon,
} from "@humagen/render-web";
import { Input } from "@humagen/render-web/ui/input";
import { NativeSelect } from "@humagen/render-web/ui/native-select";
import { Switch as SettingSwitch } from "@humagen/render-web/ui/switch";
import {
  type FormEvent,
  Fragment,
  type MouseEvent,
  type ReactNode,
  useEffect,
  useRef,
  useState,
} from "react";
import { AccessExplain } from "./AccessExplain.tsx";
import { doing } from "./access.ts";
import {
  ApiError,
  api,
  type Me,
  type Preferences,
  type Revision,
  type RouterSettings,
  type Settings as Workspace,
} from "./api.ts";
import { BillingSettings } from "./Billing.tsx";
import { ClientSettings } from "./Clients.tsx";
import { DeletionQueue } from "./DeletionQueue.tsx";
import { PersonalPreferences } from "./PersonalPreferences.tsx";
import type { PrefsRead } from "./preferences.ts";
import { ReadAudit } from "./ReadAudit.tsx";
import { href, type Route, type SettingsSection } from "./route.ts";
import { SettingsFormRow } from "./SettingsFormRow.tsx";
import {
  agentOptions,
  button,
  card,
  field,
  Label,
  Problem,
  pageLede,
  pageTitle,
  primary,
  Segmented,
  SkeletonRows,
  useStrip,
} from "./ui.tsx";

export type Section = SettingsSection;

/** A person's picture, or their initial when they have none. */
export function Avatar({
  name,
  prefs,
  size = 28,
}: {
  name: string;
  prefs: Preferences | null;
  size?: number;
}) {
  if (prefs?.has_photo) {
    return (
      <img
        src={`/api/preferences/photo?v=${prefs.photo_version}`}
        alt=""
        width={size}
        height={size}
        className="shrink-0 rounded-full object-cover"
        style={{ width: size, height: size }}
      />
    );
  }
  return (
    <span
      aria-hidden
      className="inline-flex shrink-0 items-center justify-center rounded-full bg-aui-hover font-medium text-aui-fg"
      style={{ width: size, height: size, fontSize: Math.round(size * 0.42) }}
    >
      {(name.trim()[0] ?? "?").toUpperCase()}
    </span>
  );
}

/** A plain link's click: in place on a plain click, left to the browser on a modified one. */
const follow = (go: () => void) => (e: MouseEvent<HTMLAnchorElement>) => {
  if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  e.preventDefault();
  go();
};

export function Settings({
  me,
  prefs,
  prefsRead = "read",
  workspace,
  agents,
  projects,
  canAdmin,
  section,
  onSection,
  explain,
  onExplain,
  onPrefs,
  onWorkspace,
  onOpenProjects,
  onOpenWorkspace,
  onOpenWorkspaceAgents = onOpenWorkspace,
  onNavigate,
}: {
  me: Me;
  /** The saved preferences once prefsRead is "read"; until then, defaults that only draw the page. */
  prefs: Preferences;
  prefsRead?: PrefsRead;
  workspace: Workspace | null;
  agents: Revision[];
  /** The workspace's project names, for limits. */
  projects: string[];
  canAdmin: boolean;
  /** The section on screen. The sidebar links to some of them, so the caller holds it. */
  section: Section;
  onSection: (next: Section) => void;
  /** The permission the Access section explains. */
  explain: string;
  onExplain: (permission: string) => void;
  onPrefs: (next: Preferences) => void;
  onWorkspace: (next: Workspace) => void;
  onOpenProjects: () => void;
  /** Opens the workspace's own project, where its instructions and notes are. */
  onOpenWorkspace: () => void;
  /** Opens the workspace's Agents tab, where the Workspace Default is. */
  onOpenWorkspaceAgents?: () => void;
  onNavigate?: (route: Route) => void;
}) {
  const setSection = onSection;
  const [problem, setProblem] = useState<string | null>(null);
  // What the person typed, or null to show the saved name. A copy taken once
  // would be the empty default on a page opened before the saved name is read.
  const [draft, setDraft] = useState<string | null>(null);
  const name = draft ?? prefs.display_name;
  const file = useRef<HTMLInputElement>(null);
  const who = me.principal;
  const shown = prefs.display_name || who.name || who.email || who.subject;
  const read = prefsRead === "read";

  const fail = (fallback: string) => (e: unknown) =>
    setProblem(e instanceof ApiError ? e.message : fallback);
  // A save sends every field, so none is sent before the saved ones are read.
  const savePrefs = (patch: Partial<Pick<Preferences, "display_name" | "theme" | "accent">>) => {
    if (!read) return;
    setProblem(null);
    api
      .savePreferences({
        display_name: prefs.display_name,
        theme: prefs.theme,
        accent: prefs.accent,
        ...patch,
      })
      .then((next) => {
        onPrefs(next);
        if (patch.display_name !== undefined) setDraft(null);
      })
      .catch(fail("Your preferences could not be saved."));
  };
  // In place of a control that would save over what is not read yet.
  const notRead =
    prefsRead === "failed" ? (
      <Problem message="Your preferences could not be read. Reload to try again." />
    ) : (
      <SkeletonRows count={2} />
    );
  const saveWorkspace = (patch: Partial<Workspace>) => {
    if (!workspace) return;
    setProblem(null);
    api
      .saveSettings({ ...workspace, ...patch })
      .then(onWorkspace)
      .catch(fail("The workspace settings could not be saved."));
  };

  const canReadAudit =
    me.principal.roles?.includes("admin") && me.permissions.includes("projects:govern");
  const canGovern = me.permissions.includes("projects:govern");

  return (
    <div className="humagen-fade flex min-h-0 flex-1 flex-col md:flex-row">
      <SettingsNav
        me={me}
        section={section}
        onSection={(next) => {
          setSection(next);
          setProblem(null);
        }}
        onNavigate={onNavigate}
      />
      <div className="aui-scroll min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex w-full max-w-3xl flex-col gap-4 px-5 pt-8 pb-12 md:px-10">
          <Problem message={problem} />
          {section === "notifications" && (
            <PersonalPreferences section="notifications" onSaved={onPrefs} />
          )}
          {section === "read-audit" &&
            (canReadAudit ? (
              <ReadAudit />
            ) : (
              <>
                <h1 className={pageTitle}>{defaultCopy.readAudit.title}</h1>
                <Problem message={defaultCopy.readAudit.denied} />
              </>
            ))}

          {section === "deletions" &&
            (canGovern ? (
              <DeletionQueue />
            ) : (
              <>
                <h1 className={pageTitle}>{defaultCopy.deletions.title}</h1>
                <Problem message={defaultCopy.deletions.denied} />
              </>
            ))}

          {section === "routing" && (
            <>
              <header className="pb-2">
                <h1 className={pageTitle}>Agent routing</h1>
                <p className={pageLede}>
                  A message goes to the agents it tags. One that tags none goes to the default where
                  it is posted. When that is Everyone decides, each agent available there decides
                  for itself whether to answer, and each that does answers in the same thread. When
                  none does, the message waits for a person to answer or tag an agent.
                </p>
              </header>
              {workspace && (
                <RouterControl
                  key={JSON.stringify(workspace.router ?? null)}
                  saved={workspace.router ?? { enabled: false }}
                  canEdit={canAdmin}
                  onSave={(router) => saveWorkspace({ router })}
                />
              )}
              <section className={`${card} flex flex-col gap-3`}>
                <h2 className="text-aui-sm font-medium text-aui-fg">
                  {defaultCopy.workspaceDefault}
                </h2>
                <p className="text-aui-sm text-aui-muted">
                  {defaultCopy.workspaceDefault} answers a message that tags no agent, unless a
                  project sets its own. It lives on the workspace's Agents tab.
                </p>
                <a
                  href={href({ page: "project", name: "", tab: "agents" })}
                  className={`${button} self-start`}
                  onClick={follow(onOpenWorkspaceAgents)}
                >
                  {defaultCopy.workspaceDefault}
                </a>
              </section>
              <section className={`${card} flex flex-col gap-3`}>
                <h2 className="text-aui-sm font-medium text-aui-fg">Agent capabilities</h2>
                <p className="text-aui-xs text-aui-muted">
                  Each agent reads its description first when deciding whether to take a message.
                  Edit them in Agents.
                </p>
                {agents.length === 0 && (
                  <p className="text-aui-sm text-aui-muted">No agents configured.</p>
                )}
                {agents.map((a) => (
                  <div key={a.spec.name}>
                    <p className="text-aui-sm font-medium text-aui-fg">{a.spec.name}</p>
                    <p className="text-aui-xs text-aui-muted">
                      {a.spec.tagged_only
                        ? "Only when tagged."
                        : a.spec.description || "Decides using its name and instructions."}
                    </p>
                  </div>
                ))}
              </section>
            </>
          )}

          {section === "profile" && (
            <>
              <header className="pb-2">
                <h1 className={pageTitle}>Profile</h1>
                <p className={pageLede}>
                  How you appear to yourself in this console. Only you see these.
                </p>
              </header>
              <section className={`${card} flex flex-wrap items-center gap-4`}>
                <Avatar name={shown} prefs={prefs} size={72} />
                <div className="flex min-w-0 flex-1 flex-col gap-2">
                  <p className="text-aui-sm text-aui-fg">Profile picture</p>
                  <p className="text-aui-xs text-aui-muted">
                    A PNG, JPEG or WebP image up to 512 KB.
                  </p>
                  <div className="flex flex-wrap gap-2">
                    <input
                      ref={file}
                      type="file"
                      accept="image/png,image/jpeg,image/webp"
                      className="sr-only"
                      aria-label="Choose a profile picture"
                      onChange={(e) => {
                        const picked = e.target.files?.[0];
                        e.target.value = "";
                        if (!picked) return;
                        setProblem(null);
                        api
                          .uploadPhoto(picked)
                          .then(onPrefs)
                          .catch(fail("The picture could not be uploaded."));
                      }}
                    />
                    <button type="button" className={button} onClick={() => file.current?.click()}>
                      {prefs.has_photo ? "Change Picture" : "Upload Picture"}
                    </button>
                    {prefs.has_photo && (
                      <button
                        type="button"
                        className={button}
                        onClick={() =>
                          api
                            .removePhoto()
                            .then(onPrefs)
                            .catch(fail("The picture could not be removed."))
                        }
                      >
                        Remove
                      </button>
                    )}
                  </div>
                </div>
              </section>
              {read ? (
                <form
                  className={`${card} flex flex-col gap-3`}
                  onSubmit={(e: FormEvent) => {
                    e.preventDefault();
                    savePrefs({ display_name: name });
                  }}
                >
                  <Label
                    text="Display name"
                    hint={`Leave empty to use the name from sign-in${who.name ? `: ${who.name}` : ""}.`}
                  >
                    <input
                      className={field}
                      value={name}
                      maxLength={80}
                      onChange={(e) => setDraft(e.target.value)}
                    />
                  </Label>
                  <div className="flex justify-end">
                    <button
                      type="submit"
                      className={primary}
                      disabled={name === prefs.display_name}
                    >
                      Save Name
                    </button>
                  </div>
                </form>
              ) : (
                <section className={card} aria-label="Display name">
                  {notRead}
                </section>
              )}
            </>
          )}

          {section === "appearance" && (
            <>
              <header className="pb-2">
                <h1 className={pageTitle}>Appearance</h1>
              </header>
              {!read && <section className={card}>{notRead}</section>}
              {read && (
                <section className={`${card} flex flex-wrap items-center justify-between gap-3`}>
                  <span className="text-aui-sm text-aui-fg">Theme</span>
                  <Segmented
                    label="Theme"
                    value={prefs.theme}
                    onChange={(theme) => savePrefs({ theme })}
                    options={[
                      { id: "system", label: "System", icon: <MonitorIcon size={14} /> },
                      { id: "light", label: "Light", icon: <SunIcon size={14} /> },
                      { id: "dark", label: "Dark", icon: <MoonIcon size={14} /> },
                    ]}
                  />
                </section>
              )}
            </>
          )}

          {section === "workspace" && (
            <>
              <header className="pb-2">
                <h1 className={pageTitle}>Workspace</h1>
                <p className={pageLede}>
                  Shared by everyone in {who.tenant}.{" "}
                  {canAdmin ? "" : "Only an admin can change these."}
                </p>
              </header>
              <section className="flex items-start gap-3 rounded-aui-lg border border-aui-border bg-aui-surface p-4">
                <ServerIcon size={20} className="mt-0.5 shrink-0 text-aui-muted" />
                <div className="min-w-0">
                  <h2 className="text-aui-sm font-medium">
                    {defaultCopy.preferences.ownershipTitle}
                  </h2>
                  <p className="mt-1 text-aui-xs leading-5 text-aui-muted">
                    {defaultCopy.preferences.ownershipDescription}
                  </p>
                </div>
              </section>
              {!workspace ? null : (
                <>
                  <section>
                    <SettingsFormRow
                      id="workspace-title-agent"
                      label="Title agent"
                      icon={<BotIcon size={18} />}
                      description="After a conversation's first turn, this agent is asked to name it. It needs a real model: nothing happens on the offline provider."
                    >
                      <NativeSelect
                        id="workspace-title-agent"
                        aria-label="Naming agent"
                        aria-describedby="workspace-title-agent-hint"
                        value={workspace.title_agent}
                        disabled={!canAdmin}
                        onChange={(event) => saveWorkspace({ title_agent: event.target.value })}
                      >
                        {agentOptions(
                          agents.map((a) => a.spec.name),
                          "None",
                        ).map((option) => (
                          <option key={option.value} value={option.value}>
                            {option.label}
                          </option>
                        ))}
                      </NativeSelect>
                    </SettingsFormRow>
                    <SettingsFormRow
                      id="workspace-memory-agent"
                      label="Memory agent"
                      icon={<BrainIcon size={18} />}
                      description="After each turn, this agent is asked what the answering agent should remember. What it writes is shown on that agent's Memory tab, where anyone who can edit the agent can remove it."
                    >
                      <NativeSelect
                        id="workspace-memory-agent"
                        aria-describedby="workspace-memory-agent-hint"
                        value={workspace.memory_agent}
                        disabled={!canAdmin}
                        onChange={(event) => saveWorkspace({ memory_agent: event.target.value })}
                      >
                        {agentOptions(
                          agents.map((a) => a.spec.name),
                          "None",
                        ).map((option) => (
                          <option key={option.value} value={option.value}>
                            {option.label}
                          </option>
                        ))}
                      </NativeSelect>
                    </SettingsFormRow>
                    <SettingsFormRow
                      id="workspace-memory-approve"
                      label="Keep what the memory agent writes without asking"
                      description="Off, every note it writes waits for a person to approve it. On, notes for the workspace and company projects still wait for someone who can govern projects. Notes that carry personal data are refused either way."
                      compact
                    >
                      <SettingSwitch
                        id="workspace-memory-approve"
                        aria-describedby="workspace-memory-approve-hint"
                        checked={workspace.memory_auto_approve}
                        disabled={!canAdmin || !workspace.memory_agent}
                        onCheckedChange={(on) => saveWorkspace({ memory_auto_approve: on })}
                      />
                    </SettingsFormRow>
                  </section>

                  <section>
                    <SettingsFormRow
                      id="workspace-history"
                      label="Carry the conversation forward"
                      description="Send the agent answering the next turn what was said before it. Off sends only the new message."
                      compact
                    >
                      <SettingSwitch
                        id="workspace-history"
                        aria-describedby="workspace-history-hint"
                        checked={workspace.history === "recent"}
                        disabled={!canAdmin}
                        onCheckedChange={(on) => saveWorkspace({ history: on ? "recent" : "none" })}
                      />
                    </SettingsFormRow>
                    <SettingsFormRow
                      id="workspace-history-limit"
                      label="Keep at most (characters)"
                      description="When a conversation is longer, the oldest turns are left out of what the agent is sent. They are dropped, not summarized, and stay on record."
                    >
                      <Input
                        id="workspace-history-limit"
                        aria-describedby="workspace-history-limit-hint"
                        type="number"
                        min={1000}
                        max={200000}
                        step={1000}
                        disabled={!canAdmin || workspace.history !== "recent"}
                        defaultValue={workspace.history_limit}
                        key={workspace.history_limit}
                        onBlur={(e) => {
                          const n = Number(e.target.value);
                          if (n !== workspace.history_limit) saveWorkspace({ history_limit: n });
                        }}
                      />
                    </SettingsFormRow>
                  </section>

                  {/* Projects have their own page. The workspace is the top of their tree. */}
                  <section className={`${card} flex flex-col gap-3`} aria-label="Projects">
                    <h3 className="text-aui-sm font-medium text-aui-fg">Projects</h3>
                    <p className="text-aui-xs text-aui-muted">
                      Projects are made and changed on their own page. The workspace is the top of
                      the tree: its instructions and notes reach every agent.
                    </p>
                    <div className="flex flex-wrap gap-2">
                      <a
                        href={href({ page: "projects" })}
                        className={button}
                        onClick={follow(onOpenProjects)}
                      >
                        Open Projects
                      </a>
                      <a
                        href={href({ page: "project", name: "", tab: "inbox" })}
                        className={button}
                        onClick={follow(onOpenWorkspace)}
                      >
                        Open the Workspace
                      </a>
                    </div>
                  </section>
                </>
              )}
            </>
          )}

          {section === "limits" && (
            <BillingSettings
              projects={projects}
              agents={agents.map((a) => a.spec.name)}
              canWrite={me.permissions.includes("billing:write")}
            />
          )}

          {section === "connections" && (
            <ClientSettings canWrite={me.permissions.includes("clients:write")} />
          )}

          {section === "access" && (
            <>
              <header className="pb-2">
                <h1 className={pageTitle}>Access</h1>
                <p className={pageLede}>
                  What you are signed in as and what you may do. Your identity provider decides
                  this.
                </p>
              </header>
              <section className={`${card} flex flex-col gap-2`}>
                <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1.5 text-aui-sm">
                  <dt className="text-aui-muted">Signed in as</dt>
                  <dd className="text-aui-fg">{who.email || who.name || who.subject}</dd>
                  <dt className="text-aui-muted">Workspace</dt>
                  <dd className="text-aui-fg">
                    {workspace?.workspace_name || defaultCopy.projects.workspace}
                  </dd>
                  <dt className="text-aui-muted">Roles</dt>
                  <dd className="text-aui-fg">
                    {who.roles?.length ? who.roles.join(", ") : "None"}
                  </dd>
                  <dt className="text-aui-muted">Sign-in</dt>
                  <dd className="text-aui-fg">
                    {who.subject === "dev"
                      ? "Dev mode. No sign-in, and everyone on this machine is an admin."
                      : "Through your identity provider"}
                  </dd>
                </dl>
              </section>
              <section className={`${card} flex flex-col gap-2`}>
                <h3 className="text-aui-sm font-medium text-aui-fg">You may</h3>
                <ul className="flex flex-wrap gap-1.5">
                  {me.permissions.map((p) => (
                    <li
                      key={p}
                      className="rounded-full border border-aui-border px-2 py-0.5 text-aui-xs text-aui-muted"
                    >
                      {doing[p] || "Additional workspace access"}
                    </li>
                  ))}
                </ul>
                <details className="text-aui-xs text-aui-muted">
                  <summary className="cursor-pointer">Technical permission names</summary>
                  <p className="mt-2 break-words font-aui-mono">{me.permissions.join(", ")}</p>
                </details>
              </section>
              <AccessExplain name={shown} permission={explain} onPermission={onExplain} />
            </>
          )}
        </div>
      </div>
    </div>
  );
}

/**
 * Whether agents decide for themselves to take a message that names no agent,
 * and the model that asks them. The provider is one this deployment declares;
 * its key comes from an environment variable or a secrets manager, never from
 * here.
 */
function RouterControl({
  saved,
  canEdit,
  onSave,
}: {
  saved: RouterSettings;
  canEdit: boolean;
  onSave: (next: RouterSettings) => void;
}) {
  const [draft, setDraft] = useState<RouterSettings>(saved);
  const [providers, setProviders] = useState<string[] | null>(null);
  useEffect(() => {
    let live = true;
    api
      .modelProviders()
      .then(
        (all) =>
          live &&
          setProviders(all.filter((p) => p.declared && p.name !== "offline").map((p) => p.name)),
      )
      .catch(() => live && setProviders([]));
    return () => {
      live = false;
    };
  }, []);
  const dirty = JSON.stringify(draft) !== JSON.stringify(saved);
  const problem =
    draft.enabled && !draft.provider
      ? "Choose a provider."
      : draft.enabled && !draft.model?.trim()
        ? "Name a model."
        : null;
  const choices = providers ?? [];
  // A saved provider stays listed even when this deployment no longer declares it, so the field shows what is saved.
  const listed =
    draft.provider && !choices.includes(draft.provider) ? [draft.provider, ...choices] : choices;
  return (
    <section className={`${card} flex flex-col gap-3`} aria-label="Agents decide for themselves">
      <SettingsFormRow
        id="workspace-router"
        label="Agents decide for themselves"
        description="On, every agent available where an untagged message is posted is asked, all at once, whether it should answer. Off, no agent takes a message on its own."
        compact
      >
        <SettingSwitch
          id="workspace-router"
          aria-describedby="workspace-router-hint"
          checked={draft.enabled}
          disabled={!canEdit}
          onCheckedChange={(enabled) => setDraft((d) => ({ ...d, enabled }))}
        />
      </SettingsFormRow>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Label text="Provider" hint="One this deployment declares.">
          <NativeSelect
            aria-label="Router provider"
            value={draft.provider ?? ""}
            disabled={!canEdit || !draft.enabled}
            onChange={(e) => setDraft((d) => ({ ...d, provider: e.target.value || undefined }))}
          >
            <option value="">
              {providers === null
                ? "Loading providers"
                : listed.length === 0
                  ? "No provider declared"
                  : "Choose a provider"}
            </option>
            {listed.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </NativeSelect>
        </Label>
        <Label text="Model" hint="A small, fast model is enough: each agent answers yes or no.">
          <input
            className={field}
            maxLength={200}
            value={draft.model ?? ""}
            disabled={!canEdit || !draft.enabled}
            onChange={(e) => setDraft((d) => ({ ...d, model: e.target.value || undefined }))}
          />
        </Label>
      </div>
      <p className="text-aui-xs text-aui-muted">
        No credential is stored here. The provider's key comes from an environment variable or a
        secrets manager.
      </p>
      {canEdit && (
        <div className="flex flex-wrap items-center justify-end gap-2">
          {problem && dirty && <span className="text-aui-xs text-aui-bad">{problem}</span>}
          {dirty && (
            <button type="button" className={button} onClick={() => setDraft(saved)}>
              Discard
            </button>
          )}
          <button
            type="button"
            className={primary}
            disabled={!dirty || problem !== null}
            onClick={() => onSave(draft)}
          >
            Save Routing
          </button>
        </div>
      )}
    </section>
  );
}

// Workspace pages shown inside workspace settings. Schedules and Projects live in the sidebar.
const linked = [
  {
    label: "People",
    icon: <UsersIcon size={16} />,
    route: { page: "directory", tab: "people" } as Route,
    permission: "people:read",
  },
  {
    label: "Usage",
    icon: <ClipboardListIcon size={16} />,
    route: { page: "usage" } as Route,
    permission: "runs:read",
  },
  {
    label: "Health",
    icon: <CheckCircleIcon size={16} />,
    route: { page: "monitor", tab: "health" } as Route,
    permission: "alerts:read",
  },
  {
    label: "Policies",
    icon: <SlidersIcon size={16} />,
    route: { page: "action-policy" } as Route,
    permission: "agents:read",
  },
  {
    label: "Audit",
    icon: <ListChecksIcon size={16} />,
    route: { page: "audit" } as Route,
    permission: "projects:govern",
  },
  {
    label: "Agent controls",
    icon: <PauseIcon size={16} />,
    route: { page: "agent-control" } as Route,
    permission: "agents:write",
  },
];

/** Your own settings; every other section is the workspace's. */
const yours: Section[] = ["profile", "notifications", "appearance"];
/** Pages shown inside Your settings. */
const yourPages = [
  { label: "Connections", icon: <PlugIcon size={16} />, route: { page: "connections" } as Route },
];

/**
 * Where Workspace settings opens for someone: its first section for an admin,
 * else the first workspace page they may read, else nowhere.
 */
export function workspaceSettingsRoute(
  can: (permission: string) => boolean,
  roles: string[] = [],
): Route | null {
  if (roles.includes("admin") || can("agents:write") || can("projects:govern"))
    return { page: "settings", section: "workspace" } as Route;
  return linked.find((item) => can(item.permission))?.route ?? null;
}

/** The settings sections, and the workspace pages that live inside settings. */
export function SettingsNav({
  me,
  section,
  active,
  onSection,
  onNavigate,
}: {
  me: Me;
  section?: Section;
  /** The label of a linked page on screen, which then shows as current. */
  active?: string;
  onSection: (next: Section) => void;
  onNavigate?: (route: Route) => void;
}) {
  const canReadAudit =
    me.principal.roles?.includes("admin") && me.permissions.includes("projects:govern");
  const canGovern = me.permissions.includes("projects:govern");
  const admin =
    me.principal.roles?.includes("admin") ||
    me.permissions.includes("agents:write") ||
    me.permissions.includes("projects:govern");
  type Entry = { id: Section; label: string; icon: ReactNode };
  // Your own settings first; workspace settings only for someone who can change them.
  const personal: Entry[] = [
    { id: "profile", label: "Profile", icon: <UserIcon size={16} /> },
    { id: "notifications", label: "Notifications", icon: <BellIcon size={16} /> },
    { id: "appearance", label: "Appearance", icon: <PaletteIcon size={16} /> },
  ];
  const workspaceEntries: Entry[] = admin
    ? [
        { id: "workspace", label: "Workspace", icon: <ServerIcon size={16} /> },
        { id: "routing", label: "Agent routing", icon: <BotIcon size={16} /> },
        { id: "limits", label: "Limits", icon: <GaugeIcon size={16} /> },
        { id: "access", label: "Access", icon: <ShieldIcon size={16} /> },
        ...(canReadAudit
          ? [
              {
                id: "read-audit" as const,
                label: defaultCopy.readAudit.label,
                icon: <ScrollTextIcon size={16} />,
              },
            ]
          : []),
        ...(canGovern
          ? [
              {
                id: "deletions" as const,
                label: defaultCopy.deletions.title,
                icon: <TrashIcon size={16} />,
              },
            ]
          : []),
      ]
    : [];
  // Your settings and the workspace's are two places: the open section says which.
  const scope =
    (!active && section && yours.includes(section)) ||
    (active && yourPages.some((p) => p.label === active))
      ? "you"
      : "workspace";
  const groups = [
    scope === "you"
      ? { title: "Your settings", entries: personal }
      : { title: "Workspace settings", entries: workspaceEntries },
  ];
  const sectionLink =
    "inline-flex min-h-9 pointer-coarse:min-h-11 shrink-0 items-center gap-2.5 whitespace-nowrap rounded-aui px-2.5 text-left text-aui-sm text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-[current=page]:bg-aui-hover aria-[current=page]:text-aui-fg";
  // On a phone the sections are a row that scrolls inside itself, kept on the open one.
  const strip = useStrip<HTMLElement>(active ?? section ?? "");
  return (
    <>
      {/* The rule is drawn on a frame around the sections, so it runs the full width while the row's ends fade. */}
      {/* On a phone the row scrolls inside the 16px gutter, never under the screen's edge. */}
      <div className="shrink-0 border-aui-border border-b px-4 md:w-52 md:border-r md:border-b-0 md:px-0">
        <nav
          ref={strip}
          aria-label="Settings sections"
          className="humagen-strip flex gap-0.5 overflow-x-auto py-2 md:flex-col md:p-3"
        >
          {groups.map((g) => (
            <Fragment key={g.title}>
              <p className="hidden px-2.5 pt-3 pb-1 text-aui-xs font-medium text-aui-muted first:pt-0 md:block">
                {g.title}
              </p>
              {g.entries.map((s) => (
                <button
                  key={s.id}
                  type="button"
                  aria-current={!active && section === s.id ? "page" : undefined}
                  onClick={() => onSection(s.id)}
                  className={sectionLink}
                >
                  {s.icon}
                  {s.label}
                </button>
              ))}
              {scope === "you" &&
                onNavigate &&
                yourPages.map((item) => (
                  <a
                    key={item.label}
                    href={href(item.route)}
                    onClick={follow(() => onNavigate(item.route))}
                    aria-current={active === item.label ? "page" : undefined}
                    className={sectionLink}
                  >
                    {item.icon}
                    {item.label}
                  </a>
                ))}
              {scope === "workspace" &&
                onNavigate &&
                linked
                  .filter((item) => me.permissions.includes(item.permission))
                  .map((item) => (
                    <a
                      key={item.label}
                      href={href(item.route)}
                      onClick={follow(() => onNavigate(item.route))}
                      aria-current={active === item.label ? "page" : undefined}
                      className={sectionLink}
                    >
                      {item.icon}
                      {item.label}
                    </a>
                  ))}
            </Fragment>
          ))}
        </nav>
      </div>
    </>
  );
}

/** A workspace page shown inside settings, beside the settings sections. */
export function SettingsFrame({
  me,
  active,
  onSection,
  onNavigate,
  children,
}: {
  me: Me;
  active: string;
  onSection: (next: Section) => void;
  onNavigate: (route: Route) => void;
  children: ReactNode;
}) {
  return (
    <div className="humagen-fade flex min-h-0 flex-1 flex-col md:flex-row">
      <SettingsNav me={me} active={active} onSection={onSection} onNavigate={onNavigate} />
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">{children}</div>
    </div>
  );
}
