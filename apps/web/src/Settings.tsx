import { defaultCopy } from "@warp/components";
import { type FormEvent, type MouseEvent, useRef, useState } from "react";
import { AccessExplain } from "./AccessExplain.tsx";
import { href, type SettingsSection } from "./route.ts";
import { ApiError, api, type Me, type ModelPolicy, type Preferences, type Revision, type Settings as Workspace } from "./api.ts";
import { ModelChain, POLICY, chainProblem, starterPolicy } from "./ModelChain.tsx";
import { BillingSettings } from "./Billing.tsx";
import { ClientSettings } from "./Clients.tsx";
import type { PrefsRead } from "./preferences.ts";
import { agentOptions, button, card, field, Label, Menu, Problem, primary, Segmented, SkeletonRows, Switch, pageLede, pageTitle } from "./ui.tsx";

export type Section = SettingsSection;

const accents: { id: string; label: string }[] = [
  { id: "purple", label: "Purple" },
  { id: "blue", label: "Blue" },
  { id: "yellow", label: "Yellow" },
  { id: "teal", label: "Teal" },
];

/** A person's picture, or their initial when they have none. */
export function Avatar({ name, prefs, size = 28 }: { name: string; prefs: Preferences | null; size?: number }) {
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
  onConnections,
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
  /** Opens Your connections, the tools that act as you. */
  onConnections?: () => void;
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

  const fail = (fallback: string) => (e: unknown) => setProblem(e instanceof ApiError ? e.message : fallback);
  // A save sends every field, so none is sent before the saved ones are read.
  const savePrefs = (patch: Partial<Pick<Preferences, "display_name" | "theme" | "accent">>) => {
    if (!read) return;
    setProblem(null);
    api
      .savePreferences({ display_name: prefs.display_name, theme: prefs.theme, accent: prefs.accent, ...patch })
      .then((next) => {
        onPrefs(next);
        if (patch.display_name !== undefined) setDraft(null);
      })
      .catch(fail("Your preferences could not be saved."));
  };
  // In place of a control that would save over what is not read yet.
  const notRead =
    prefsRead === "failed" ? <Problem message="Your preferences could not be read. Reload to try again." /> : <SkeletonRows count={2} />;
  const saveWorkspace = (patch: Partial<Workspace>) => {
    if (!workspace) return;
    setProblem(null);
    api
      .saveSettings({ ...workspace, ...patch })
      .then(onWorkspace)
      .catch(fail("The workspace settings could not be saved."));
  };

  const sections: { id: Section; label: string }[] = [
    { id: "profile", label: "Profile" },
    { id: "appearance", label: "Appearance" },
    { id: "workspace", label: "Workspace" },
    { id: "routing", label: "Agent routing" },
    { id: "models", label: "Models" },
    { id: "limits", label: "Limits" },
    { id: "connections", label: "Connections" },
    { id: "access", label: "Access" },
  ];

  return (
    <div className="warp-fade flex min-h-0 flex-1 flex-col md:flex-row">
      <nav aria-label="Settings sections" className="flex shrink-0 gap-0.5 overflow-x-auto border-aui-border border-b p-2 md:w-52 md:flex-col md:border-r md:border-b-0 md:p-3">
        {sections.map((s) => (
          <button
            key={s.id}
            type="button"
            aria-current={section === s.id ? "page" : undefined}
            onClick={() => {
              setSection(s.id);
              setProblem(null);
            }}
            className="h-8 shrink-0 rounded-aui px-2.5 text-left text-aui-sm text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-[current=page]:bg-aui-hover aria-[current=page]:text-aui-fg"
          >
            {s.label}
          </button>
        ))}
      </nav>

      <div className="aui-scroll min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex w-full max-w-3xl flex-col gap-4 px-5 pt-8 pb-12 md:px-10">
          <Problem message={problem} />

          {section === "routing" && (
            <>
              <header className="pb-2">
                <h1 className={pageTitle}>Agent routing</h1>
                <p className={pageLede}>A message goes to the agent it tags. One that tags none goes to the default where it is posted, which can be @auto.</p>
              </header>
              <section className={`${card} flex flex-col gap-3`}>
                <h2 className="text-aui-sm font-medium text-aui-fg">@auto</h2>
                <p className="text-aui-sm text-aui-muted">
                  Matches a message to the names, descriptions and instructions of the agents available where it is posted.
                  Names and descriptions carry more weight. When agents tie or none match, the nearest default that names an
                  agent answers.
                </p>
                <p className="text-aui-sm text-aui-muted">
                  {defaultCopy.workspaceDefault} answers a message that tags no agent, unless a project sets its own. It lives on the
                  workspace's Agents tab.
                </p>
                <a href={href({ page: "project", name: "", tab: "agents" })} className={`${button} self-start`} onClick={follow(onOpenWorkspaceAgents)}>
                  {defaultCopy.workspaceDefault}
                </a>
              </section>
              <section className={`${card} flex flex-col gap-3`}>
                <h2 className="text-aui-sm font-medium text-aui-fg">Agent capabilities</h2>
                <p className="text-aui-xs text-aui-muted">Edit descriptions and instructions in Agents to change how requests are matched.</p>
                {agents.length === 0 && <p className="text-aui-sm text-aui-muted">No agents configured.</p>}
                {agents.map((a) => (
                  <div key={a.spec.name}>
                    <p className="text-aui-sm font-medium text-aui-fg">{a.spec.name}</p>
                    <p className="text-aui-xs text-aui-muted">{a.spec.description || "Matches using its name and instructions."}</p>
                  </div>
                ))}
              </section>
            </>
          )}

          {section === "profile" && (
            <>
              <header className="pb-2">
                <h1 className={pageTitle}>Profile</h1>
                <p className={pageLede}>How you appear to yourself in this console. Only you see these.</p>
              </header>
              <section className={`${card} flex flex-wrap items-center gap-4`}>
                <Avatar name={shown} prefs={prefs} size={72} />
                <div className="flex min-w-0 flex-1 flex-col gap-2">
                  <p className="text-aui-sm text-aui-fg">Profile picture</p>
                  <p className="text-aui-xs text-aui-muted">A PNG, JPEG or WebP image up to 512 KB.</p>
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
                        api.uploadPhoto(picked).then(onPrefs).catch(fail("The picture could not be uploaded."));
                      }}
                    />
                    <button type="button" className={button} onClick={() => file.current?.click()}>
                      {prefs.has_photo ? "Change Picture" : "Upload Picture"}
                    </button>
                    {prefs.has_photo && (
                      <button
                        type="button"
                        className={button}
                        onClick={() => api.removePhoto().then(onPrefs).catch(fail("The picture could not be removed."))}
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
                  <Label text="Display name" hint={`Leave empty to use the name from sign-in${who.name ? `: ${who.name}` : ""}.`}>
                    <input className={field} value={name} maxLength={80} onChange={(e) => setDraft(e.target.value)} />
                  </Label>
                  <div className="flex justify-end">
                    <button type="submit" className={primary} disabled={name === prefs.display_name}>
                      Save Name
                    </button>
                  </div>
                </form>
              ) : (
                <section className={card} aria-label="Display name">
                  {notRead}
                </section>
              )}
              {onConnections && (
                <section className={`${card} flex flex-wrap items-center justify-between gap-3`}>
                  <div className="flex min-w-0 flex-col gap-1">
                    <p className="text-aui-sm text-aui-fg">Your connections</p>
                    <p className="text-aui-xs text-aui-muted">The tools that act as you, and the credential you gave each.</p>
                  </div>
                  <a href={href({ page: "connections" })} className={button} onClick={follow(onConnections)}>
                    Open Your Connections
                  </a>
                </section>
              )}
            </>
          )}

          {section === "appearance" && (
            <>
              <header className="pb-2">
                <h1 className={pageTitle}>Appearance</h1>
                <p className={pageLede}>These follow you to any browser you sign in from.</p>
              </header>
              {!read && <section className={card}>{notRead}</section>}
              {read && (
                <section className={`${card} flex flex-col gap-4`}>
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <span>
                      <span className="block text-aui-sm text-aui-fg">Theme</span>
                      <span className="block text-aui-xs text-aui-muted">System follows your device.</span>
                    </span>
                    <Segmented
                      label="Theme"
                      value={prefs.theme}
                      onChange={(theme) => savePrefs({ theme })}
                      options={[
                        { id: "system", label: "System" },
                        { id: "light", label: "Light" },
                        { id: "dark", label: "Dark" },
                      ]}
                    />
                  </div>
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <span>
                      <span className="block text-aui-sm text-aui-fg">Accent</span>
                      <span className="block text-aui-xs text-aui-muted">Used for focus, selection and progress.</span>
                    </span>
                    <Segmented label="Accent" value={prefs.accent} onChange={(accent) => savePrefs({ accent })} options={accents} />
                  </div>
                </section>
              )}
            </>
          )}

          {section === "models" && (
            <>
              <header className="pb-2">
                <h1 className={pageTitle}>Models</h1>
                <p className={pageLede}>
                  Policies say which models agents run on, in order, and which errors move a call to the next. An agent
                  that follows a policy picks up a change at its next call, so one edit moves every agent on it off a
                  failing provider. {canAdmin ? "" : "Only an admin can change these."}
                </p>
              </header>
              {workspace && (
                <ModelPolicies
                  key={JSON.stringify(workspace.model_policies ?? [])}
                  saved={workspace.model_policies ?? []}
                  agents={agents}
                  canEdit={canAdmin}
                  onSave={(model_policies) => saveWorkspace({ model_policies })}
                />
              )}
            </>
          )}

          {section === "workspace" && (
            <>
              <header className="pb-2">
                <h1 className={pageTitle}>Workspace</h1>
                <p className={pageLede}>
                  Shared by everyone in {who.tenant}. {canAdmin ? "" : "Only an admin can change these."}
                </p>
              </header>
              {!workspace ? null : (
                <>
                  <section className={`${card} flex flex-col gap-3`}>
                    <Label
                      text="Title agent"
                      hint="After a conversation's first turn, this agent is asked to name it. It needs a real model: nothing happens on the offline provider."
                    >
                      <Menu
                        label="Naming agent"
                        value={workspace.title_agent}
                        disabled={!canAdmin}
                        options={agentOptions(agents.map((a) => a.spec.name), "None")}
                        onChange={(next) => saveWorkspace({ title_agent: next })}
                      />
                    </Label>
                    <Label
                      text="Memory agent"
                      hint="After each turn, this agent is asked what the answering agent should remember. What it writes is shown on that agent's Memory tab, where anyone who can edit the agent can remove it."
                    >
                      <Menu
                        label="Memory agent"
                        value={workspace.memory_agent}
                        disabled={!canAdmin}
                        options={agentOptions(agents.map((a) => a.spec.name), "None")}
                        onChange={(next) => saveWorkspace({ memory_agent: next })}
                      />
                    </Label>
                    <Switch
                      label="Keep what the memory agent writes without asking"
                      hint="Off, every note it writes waits for a person to approve it. On, notes for the workspace and company projects still wait for someone who can govern projects. Notes that carry personal data are refused either way."
                      checked={workspace.memory_auto_approve}
                      disabled={!canAdmin || !workspace.memory_agent}
                      onChange={(on) => saveWorkspace({ memory_auto_approve: on })}
                    />
                  </section>

                  <section className={`${card} flex flex-col gap-3`}>
                    <Switch
                      label="Carry the conversation forward"
                      hint="Send the agent answering the next turn what was said before it. Off sends only the new message."
                      checked={workspace.history === "recent"}
                      disabled={!canAdmin}
                      onChange={(on) => saveWorkspace({ history: on ? "recent" : "none" })}
                    />
                    <Label
                      text="Keep at most (characters)"
                      hint="When a conversation is longer, the oldest turns are left out of what the agent is sent. They are dropped, not summarized, and stay on record."
                    >
                      <input
                        className={field}
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
                    </Label>
                  </section>

                  {/* Projects have their own page. The workspace is the top of their tree. */}
                  <section className={`${card} flex flex-col gap-3`} aria-label="Projects">
                    <h3 className="text-aui-sm font-medium text-aui-fg">Projects</h3>
                    <p className="text-aui-xs text-aui-muted">
                      Projects are made and changed on their own page. The workspace is the top of the tree: its instructions
                      and notes reach every agent.
                    </p>
                    <div className="flex flex-wrap gap-2">
                      <a href={href({ page: "projects" })} className={button} onClick={follow(onOpenProjects)}>
                        Open Projects
                      </a>
                      <a href={href({ page: "project", name: "", tab: "inbox" })} className={button} onClick={follow(onOpenWorkspace)}>
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

          {section === "connections" && <ClientSettings canWrite={me.permissions.includes("clients:write")} />}

          {section === "access" && (
            <>
              <header className="pb-2">
                <h1 className={pageTitle}>Access</h1>
                <p className={pageLede}>What you are signed in as and what you may do. Your identity provider decides this.</p>
              </header>
              <section className={`${card} flex flex-col gap-2`}>
                <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1.5 text-aui-sm">
                  <dt className="text-aui-muted">Signed in as</dt>
                  <dd className="text-aui-fg">{who.email || who.name || who.subject}</dd>
                  <dt className="text-aui-muted">Workspace</dt>
                  <dd className="text-aui-fg">{who.tenant}</dd>
                  <dt className="text-aui-muted">Roles</dt>
                  <dd className="text-aui-fg">{who.roles?.length ? who.roles.join(", ") : "None"}</dd>
                  <dt className="text-aui-muted">Sign-in</dt>
                  <dd className="text-aui-fg">
                    {who.subject === "dev" ? "Dev mode. No sign-in, and everyone on this machine is an admin." : "Through your identity provider"}
                  </dd>
                </dl>
              </section>
              <section className={`${card} flex flex-col gap-2`}>
                <h3 className="text-aui-sm font-medium text-aui-fg">You may</h3>
                <ul className="flex flex-wrap gap-1.5">
                  {me.permissions.map((p) => (
                    <li key={p} className="rounded-full border border-aui-border px-2 py-0.5 font-aui-mono text-aui-2xs text-aui-muted">
                      {p}
                    </li>
                  ))}
                </ul>
              </section>
              <AccessExplain name={shown} permission={explain} onPermission={onExplain} />
            </>
          )}
        </div>
      </div>
    </div>
  );
}

/** Every model policy in the workspace, edited together and saved at once. */
function ModelPolicies({
  saved,
  agents,
  canEdit,
  onSave,
}: {
  saved: ModelPolicy[];
  agents: Revision[];
  canEdit: boolean;
  onSave: (next: ModelPolicy[]) => void;
}) {
  const [draft, setDraft] = useState<ModelPolicy[]>(saved);
  const dirty = JSON.stringify(draft) !== JSON.stringify(saved);
  const users = (name: string) => agents.filter((a) => a.spec.provider === POLICY && a.spec.model === name).map((a) => a.spec.name);
  const edit = (i: number, patch: Partial<ModelPolicy>) => setDraft((d) => d.map((p, j) => (j === i ? { ...p, ...patch } : p)));
  const problem =
    draft.map((p) => (!/^[a-z0-9][a-z0-9-]{0,62}$/.test(p.name) ? `"${p.name}" is not a valid name: lowercase letters, digits and dashes.` : chainProblem(p.models, p.fallback_on ?? []))).find(Boolean) ??
    (new Set(draft.map((p) => p.name)).size !== draft.length ? "Two policies share a name." : null);

  return (
    <div className="flex flex-col gap-6">
      {draft.length === 0 && <p className={`${card} text-aui-sm text-aui-muted`}>No policies yet. Agents each name their own models.</p>}
      {draft.map((p, i) => {
        const by = users(saved[i]?.name ?? p.name);
        return (
          <section key={i} className="flex flex-col gap-3 border-aui-border border-t pt-5 first:border-t-0 first:pt-0">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-[14rem_1fr_auto] sm:items-end">
              <Label text="Name" hint={by.length > 0 ? "Agents follow it by name, so it cannot change while they do." : undefined}>
                <input className={field} value={p.name} disabled={!canEdit || by.length > 0} onChange={(e) => edit(i, { name: e.target.value })} />
              </Label>
              <Label text="Description">
                <input className={field} maxLength={300} value={p.description ?? ""} disabled={!canEdit} onChange={(e) => edit(i, { description: e.target.value })} />
              </Label>
              {canEdit && (
                <button
                  type="button"
                  className={button}
                  disabled={by.length > 0}
                  title={by.length > 0 ? `Used by ${by.join(", ")}` : undefined}
                  onClick={() => setDraft((d) => d.filter((_, j) => j !== i))}
                >
                  Remove
                </button>
              )}
            </div>
            <p className="text-aui-xs text-aui-muted">{by.length === 0 ? "No agent follows it yet." : `Followed by ${by.join(", ")}.`}</p>
            <ModelChain
              label={`Models of ${p.name}`}
              models={p.models}
              fallbackOn={p.fallback_on ?? []}
              editable={canEdit}
              onChange={(models, fallback_on) => edit(i, { models, fallback_on: fallback_on.length > 0 ? fallback_on : undefined })}
            />
          </section>
        );
      })}
      {canEdit && (
        <div className="flex flex-wrap items-center justify-between gap-2 border-aui-border border-t pt-4">
          <button type="button" className={button} onClick={() => setDraft((d) => [...d, starterPolicy(d.map((p) => p.name))])}>
            Add Policy
          </button>
          <span className="flex items-center gap-2">
            {problem && <span className="text-aui-xs text-aui-bad">{problem}</span>}
            {dirty && (
              <button type="button" className={button} onClick={() => setDraft(saved)}>
                Discard
              </button>
            )}
            <button type="button" className={primary} disabled={!dirty || problem !== null} onClick={() => onSave(draft)}>
              Save Policies
            </button>
          </span>
        </div>
      )}
    </div>
  );
}
