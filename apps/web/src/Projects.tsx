import { defaultCopy } from "@warp/components";
import { FolderIcon, FolderPlusIcon } from "@warp/render-web";
import { type FormEvent, useCallback, useEffect, useState } from "react";
import {
  ApiError,
  api,
  type Note,
  type ProjectDetail,
  type ProjectView,
  type Revision,
  type ScheduleView,
  type ThreadSummary,
} from "./api.ts";
import type { Source } from "./mail.ts";
import { addressOf, ProjectHistory, ProjectInbox, ProjectInstructions, ProjectLimits, ProjectPeople, titleOf } from "./ProjectTabs.tsx";
import type { ProjectTab } from "./route.ts";
import { PROJECT_TABS } from "./route.ts";
import { AgentMark, agentOptions, ago, button, field, Label, Menu, Page, Problem, primary, SkeletonRows, Tabs } from "./ui.tsx";

/**
 * Every project, as cards. A project is opened from here, and made from here:
 * the button turns into a one-line form.
 */
export function ProjectsPage({
  projects,
  canCreate,
  onOpen,
  onCreated,
}: {
  projects: ProjectView[];
  canCreate: boolean;
  onOpen: (name: string) => void;
  onCreated: (name: string) => void;
}) {
  const [adding, setAdding] = useState(false);
  const [saving, setSaving] = useState(false);
  const [name, setName] = useState("");
  const [problem, setProblem] = useState<string | null>(null);

  const create = (e: FormEvent) => {
    e.preventDefault();
    if (saving) return;
    const chosen = name.trim();
    setSaving(true);
    setProblem(null);
    api
      .createProject(chosen, "")
      .then(() => {
        onCreated(chosen);
        setName("");
        setAdding(false);
      })
      .catch((err) => setProblem(err instanceof ApiError ? err.message : "The project could not be created."))
      .finally(() => setSaving(false));
  };

  return (
    <Page
      title="Projects"
      lede="Tasks, shared memory and schedules that belong together."
      actions={
        canCreate && !adding ? (
          <button type="button" className={primary} onClick={() => setAdding(true)}>
            <FolderPlusIcon size={16} /> New Project
          </button>
        ) : undefined
      }
    >
      {adding && (
        <form onSubmit={create} className="flex flex-wrap items-center gap-2">
          <input
            // biome-ignore lint/a11y/noAutofocus: the form appears because the person asked to type a name
            autoFocus
            aria-label="Project name"
            className={`${field} h-9 max-w-xs flex-1`}
            placeholder="Project name"
            value={name}
            required
            maxLength={63}
            disabled={saving}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") setAdding(false);
            }}
          />
          <button type="submit" className={primary} disabled={saving || !name.trim()}>
            {saving ? "Creating…" : "Create Project"}
          </button>
          <button type="button" className={button} onClick={() => setAdding(false)}>
            Cancel
          </button>
          {problem && (
            <p role="alert" className="w-full text-aui-xs text-aui-muted">
              {problem}
            </p>
          )}
        </form>
      )}
      {projects.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">No projects yet.{canCreate ? " Make one to keep related tasks together." : ""}</p>
      ) : (
        <ul className="grid gap-3 @2xl:grid-cols-2 @5xl:grid-cols-3">
          {projects.map((p) => (
            <li key={p.name}>
              <button
                type="button"
                onClick={() => onOpen(p.name)}
                className="warp-glass flex h-full w-full flex-col gap-1.5 rounded-aui-lg p-4 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
              >
                <span className="flex items-center gap-2 text-aui-base font-medium text-aui-fg">
                  <FolderIcon size={18} />
                  <span className="min-w-0 flex-1 truncate">{titleOf(p)}</span>
                </span>
                <span className="text-aui-2xs text-aui-muted">
                  {[
                    defaultCopy.project.kind(p.kind),
                    p.path.length > 1 ? defaultCopy.project.in(p.path.slice(0, -1).map((s) => s.title || s.name).join(" › ")) : "",
                    p.account?.customer ?? "",
                    p.archived ? "Archived" : "",
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </span>
                {p.description && <span className="line-clamp-2 text-aui-sm text-aui-muted">{p.description}</span>}
                <span className="text-aui-2xs text-aui-muted tabular-nums">
                  {/* What is happening or waiting comes first. */}
                  {[
                    p.active > 0 && `${p.active} running`,
                    p.proposed > 0 && `${p.proposed} to review`,
                    `${p.conversations} ${p.conversations === 1 ? "task" : "tasks"}`,
                    p.active_at && `active ${ago(p.active_at)}`,
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </Page>
  );
}

const section = "flex flex-col gap-2 border-aui-border border-t pt-5";
const heading = "text-aui-base font-medium text-aui-fg";

/**
 * What every agent in the project is sent, and what is waiting to become that.
 * An owner decides what is kept; a member's note is a proposal for them.
 */
function SharedMemory({ project, canWrite, canPropose, onChanged }: { project: string; canWrite: boolean; canPropose: boolean; onChanged: () => void }) {
  const [notes, setNotes] = useState<Note[]>([]);
  const [text, setText] = useState("");
  const [picked, setPicked] = useState<Set<string>>(() => new Set());
  const [problem, setProblem] = useState<string | null>(null);

  const load = () => {
    api
      .projectMemory(project)
      .then(setNotes)
      .catch(() => setNotes([]));
    onChanged();
  };
  // biome-ignore lint/correctness/useExhaustiveDependencies: reload when the project changes
  useEffect(() => {
    api
      .projectMemory(project)
      .then(setNotes)
      .catch(() => setNotes([]));
  }, [project]);

  const kept = notes.filter((n) => n.status === "approved");
  const proposed = notes.filter((n) => n.status === "pending");
  const fail = (fallback: string) => (e: unknown) => setProblem(e instanceof ApiError ? e.message : fallback);

  return (
    <section className={section}>
      <h2 className={heading}>Shared memory</h2>
      <p className="max-w-xl text-aui-sm text-aui-muted">
        Every agent answering in this project is sent these, in every conversation. Keep them few and lasting.
      </p>
      <Problem message={problem} />
      {proposed.length > 0 && (
        <div className="flex flex-col gap-2 rounded-aui-lg border border-aui-border p-3">
          <h3 className="text-aui-sm font-medium text-aui-fg">Proposed ({proposed.length})</h3>
          <p className="text-aui-xs text-aui-muted">Not in use yet. Tick what the project should keep.</p>
          <ul>
            {proposed.map((n) => (
              <li key={n.id} className="border-aui-border border-t py-2 first:border-t-0">
                <label className="flex items-start gap-2.5 text-aui-sm text-aui-fg">
                  <input
                    type="checkbox"
                    className="mt-1"
                    disabled={!canWrite}
                    checked={picked.has(n.id)}
                    onChange={(e) =>
                      setPicked((p) => {
                        const next = new Set(p);
                        if (e.target.checked) next.add(n.id);
                        else next.delete(n.id);
                        return next;
                      })
                    }
                  />
                  <span>{n.text}</span>
                </label>
              </li>
            ))}
          </ul>
          {canWrite && (
            <div className="flex flex-wrap justify-end gap-2">
              <button
                type="button"
                className={button}
                onClick={() =>
                  Promise.all(
                    proposed.filter((n) => picked.size === 0 || picked.has(n.id)).map((n) => api.removeProjectMemory(project, n.id)),
                  )
                    .then(() => {
                      setPicked(new Set());
                      load();
                    })
                    .catch(fail("Those could not be discarded."))
                }
              >
                {picked.size > 0 ? `Discard ${picked.size} Selected` : "Discard All"}
              </button>
              <button
                type="button"
                className={primary}
                disabled={picked.size === 0}
                onClick={() =>
                  api
                    .approveProjectMemory(project, [...picked])
                    .then(() => {
                      setPicked(new Set());
                      load();
                    })
                    .catch(fail("Those could not be kept."))
                }
              >
                Keep {picked.size > 0 ? `${picked.size} Selected` : "Selected"}
              </button>
            </div>
          )}
        </div>
      )}
      {kept.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">Nothing shared yet.</p>
      ) : (
        <ul>
          {kept.map((n) => (
            <li key={n.id} className="warp-enter flex items-start justify-between gap-3 border-aui-border border-t py-2 first:border-t-0">
              <span className="min-w-0">
                <span className="block text-aui-sm text-aui-fg">{n.text}</span>
                <span className="block text-aui-xs text-aui-muted">
                  {n.source === "person" ? `Written by ${n.by}` : "Proposed by the memory agent and approved"}, {ago(n.at)}
                </span>
              </span>
              {canWrite && (
                <button type="button" className={button} onClick={() => api.removeProjectMemory(project, n.id).then(load).catch(fail("That could not be removed."))}>
                  Forget
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {(canWrite || canPropose) && !canWrite && <p className="text-aui-xs text-aui-muted">{defaultCopy.project.proposeHint}</p>}
      {(canWrite || canPropose) && (
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            setProblem(null);
            api
              .addProjectMemory(project, text)
              .then(() => {
                setText("");
                load();
              })
              .catch(fail("The note could not be saved."));
          }}
        >
          <input
            aria-label="Something everyone in this project should know"
            className={field}
            value={text}
            maxLength={300}
            required
            placeholder="A lasting fact, such as: The launch date is the first Tuesday of November."
            onChange={(e) => setText(e.target.value)}
          />
          <button type="submit" className={button}>
            Share
          </button>
        </form>
      )}
    </section>
  );
}

/** A project's kind and place, in words: "Account · in Field · You own it". */
function metaOf(p: ProjectView, parent: ProjectView | undefined, kinds: ProjectView["kinds"], workspace?: string): string {
  const kind = kinds?.find((k) => k.name === p.kind);
  return [
    p.name === "" ? "Workspace" : defaultCopy.project.kind(p.kind, kind?.title),
    parent && parent.name !== "" ? defaultCopy.project.in(titleOf(parent, workspace)) : "",
    defaultCopy.project.yourRole(p.access.role),
    p.archived ? defaultCopy.roles.archived : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

/**
 * One project, as tabs: its inbox (where it opens), its conversations and what
 * runs in it, what agents here are sent, who holds a role in it, the limits
 * that hold it, and what changed. What a person may do follows their role.
 */
export function ProjectPage({
  name,
  tab,
  message,
  me,
  workspace,
  threads,
  schedules,
  agents,
  canSchedule,
  canBilling,
  canPropose,
  muted,
  tick,
  inboxTick,
  onTab,
  onMessage,
  onBack,
  onOpenProject,
  onNewChat,
  onOpenThread,
  onOpenSource,
  onChanged,
  onSchedulesChanged,
  onMute,
  onDeleted,
}: {
  /** The project's name, "" or "~" for the workspace. */
  name: string;
  tab: ProjectTab;
  message?: string;
  me: string;
  workspace?: string;
  threads: ThreadSummary[];
  schedules: ScheduleView[];
  agents: Revision[];
  canSchedule: boolean;
  /** May change any limit, with billing:write. */
  canBilling: boolean;
  /** Holds projects:write at all. */
  canPropose: boolean;
  muted: string[];
  tick?: number;
  inboxTick: unknown;
  onTab: (tab: ProjectTab) => void;
  onMessage: (id: string | null) => void;
  onBack: (parent: string | null) => void;
  onOpenProject: (name: string) => void;
  onNewChat: (project: string) => void;
  onOpenThread: (id: string) => void;
  onOpenSource: (source: Source) => void;
  onChanged: () => void;
  onSchedulesChanged: () => void;
  onMute: (project: string, muted: boolean) => void;
  onDeleted: () => void;
}) {
  const [detail, setDetail] = useState<ProjectDetail | null>(null);
  const [missing, setMissing] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [version, setVersion] = useState(0);
  const address = addressOf(name === "~" ? "" : name);
  const reload = useCallback(() => setVersion((v) => v + 1), []);
  // biome-ignore lint/correctness/useExhaustiveDependencies: read again when runs change or this page changes it
  useEffect(() => {
    api
      .project(address)
      .then((d) => {
        setDetail(d);
        setMissing(false);
      })
      .catch((err) => (err instanceof ApiError && err.status === 404 ? setMissing(true) : setProblem(err instanceof ApiError ? err.message : "The project could not be loaded.")));
  }, [address, tick, version]);

  if (missing) {
    return (
      <Page title={`No project named ${name}`} lede="It may have been deleted, or you may not be allowed to see it." back={{ label: defaultCopy.project.allProjects, onClick: () => onBack(null) }}>
        <span />
      </Page>
    );
  }
  if (!detail) {
    return (
      <Page title={name === "~" ? workspace || "Workspace" : name}>
        <Problem message={problem} />
        <SkeletonRows count={4} />
      </Page>
    );
  }
  const p = detail.project;
  const parent = detail.ancestors[detail.ancestors.length - 1];
  const root = detail.ancestors[0] ?? (p.name === "" ? p : undefined);
  const changed = () => {
    reload();
    onChanged();
  };
  const fail = (fallback: string) => (e: unknown) => setProblem(e instanceof ApiError ? e.message : fallback);
  const tabs = PROJECT_TABS.map((id) => ({ id, label: defaultCopy.project.tabs[id] }));
  const isMuted = muted.includes(p.name);

  return (
    <Page
      back={{ label: parent ? titleOf(parent, workspace) : defaultCopy.project.allProjects, onClick: () => onBack(parent ? addressOf(parent.name) : null) }}
      icon={<FolderIcon size={28} />}
      title={titleOf(p, workspace)}
      meta={
        <>
          <span>{metaOf(p, parent, root?.kinds, workspace)}</span>
          {detail.ancestors.length > 1 && (
            <nav aria-label="Where it sits" className="flex flex-wrap items-center gap-1">
              {detail.ancestors.map((a) => (
                <button key={a.name} type="button" className="hover:text-aui-fg" onClick={() => onOpenProject(addressOf(a.name))}>
                  {titleOf(a, workspace)} ›
                </button>
              ))}
            </nav>
          )}
        </>
      }
      lede={p.description || undefined}
      tabs={<Tabs label="Project sections" value={tab} onChange={onTab} tabs={tabs} />}
      actions={
        <>
          {p.name !== "" && (
            <button type="button" className={button} onClick={() => onMute(p.name, !isMuted)}>
              {isMuted ? defaultCopy.announcements.unmute : defaultCopy.announcements.mute}
            </button>
          )}
          {p.actions.includes("archive") && (
            <button
              type="button"
              className={button}
              onClick={() =>
                api
                  .updateProject(address, { version: p.version, title: p.title, description: p.description, account: p.account, instructions: p.instructions, archived: !p.archived })
                  .then(changed)
                  .catch(fail("The project could not be changed."))
              }
            >
              {p.archived ? defaultCopy.roles.unarchive : defaultCopy.roles.archive}
            </button>
          )}
          {p.actions.includes("delete") && (
            <button type="button" className={button} onClick={() => api.deleteProject(address).then(onDeleted).catch(fail("The project could not be deleted."))}>
              {defaultCopy.project.deleteProject}
            </button>
          )}
          {canPropose && (
            <button type="button" className={primary} onClick={() => onNewChat(p.name)}>
              {defaultCopy.project.newTask}
            </button>
          )}
        </>
      }
    >
      <Problem message={problem} />
      {isMuted && <p className="text-aui-xs text-aui-muted">{defaultCopy.announcements.muted}</p>}
      {tab === "inbox" && (
        <ProjectInbox project={p} me={me} signal={inboxTick} message={message} onMessage={onMessage} onOpen={onOpenSource} onChanged={onChanged} />
      )}
      {tab === "overview" && (
        <Overview
          detail={detail}
          threads={threads}
          schedules={schedules}
          agents={agents}
          canSchedule={canSchedule}
          workspace={workspace}
          onOpenProject={onOpenProject}
          onOpenThread={onOpenThread}
          onChanged={changed}
          onSchedulesChanged={onSchedulesChanged}
        />
      )}
      {tab === "context" && (
        <div className="flex flex-col gap-8">
          <ProjectInstructions key={p.version} project={p} ancestors={detail.ancestors} workspace={workspace} onSaved={changed} />
          <SharedMemory project={address} canWrite={p.actions.includes("notes")} canPropose={p.actions.includes("propose")} onChanged={changed} />
        </div>
      )}
      {tab === "people" && <ProjectPeople key={p.version} project={p} onChanged={changed} />}
      {tab === "limits" && <ProjectLimits detail={detail} canBilling={canBilling} tick={tick} workspace={workspace} />}
      {tab === "history" && <ProjectHistory project={p} tick={tick} />}
    </Page>
  );
}

/** A project's conversations, the projects below it, and what runs in it on a timer. */
function Overview({
  detail,
  threads,
  schedules,
  agents,
  canSchedule,
  workspace,
  onOpenProject,
  onOpenThread,
  onChanged,
  onSchedulesChanged,
}: {
  detail: ProjectDetail;
  threads: ThreadSummary[];
  schedules: ScheduleView[];
  agents: Revision[];
  canSchedule: boolean;
  workspace?: string;
  onOpenProject: (name: string) => void;
  onOpenThread: (id: string) => void;
  onChanged: () => void;
  onSchedulesChanged: () => void;
}) {
  const project = detail.project;
  const mine = threads.filter((t) => (t.project ?? "") === project.name);
  const timed = schedules.filter((s) => (s.project ?? "") === project.name);
  const [draft, setDraft] = useState({ name: "", expression: "0 9 * * 1-5", agent: "", text: "" });
  const [adding, setAdding] = useState(false);
  const [sub, setSub] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const agent = draft.agent || agents[0]?.spec.name || "";
  const fail = (fallback: string) => (e: unknown) => setProblem(e instanceof ApiError ? e.message : fallback);

  return (
    <div className="grid grid-cols-1 items-start gap-8 @4xl:grid-cols-[minmax(0,1fr)_20rem]">
      <section className="flex flex-col gap-2">
        <Problem message={problem} />
        <h2 className={heading}>Tasks ({mine.length})</h2>
        {mine.length === 0 ? (
          <p className="text-aui-sm text-aui-muted">Start a task here to keep its conversation, execution history and shared context in this project.</p>
        ) : (
          <ul>
            {mine.map((t) => (
              <li key={t.id} className="warp-enter border-aui-border border-t first:border-t-0">
                <button
                  type="button"
                  onClick={() => onOpenThread(t.id)}
                  className="flex w-full items-center gap-3 py-2 text-left text-aui-sm hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                >
                  <AgentMark name={t.agents[t.agents.length - 1] ?? "?"} size={14} state={t.active > 0 ? "thinking" : t.failed > 0 ? "error" : "idle"} />
                  <span className="min-w-0 flex-1 truncate text-aui-fg">{t.title}</span>
                  <span className="shrink-0 text-aui-xs text-aui-muted">
                    {t.origin === "cron" || t.origin === "manual" ? "Scheduled, " : ""}
                    {t.active > 0 ? "In progress" : t.failed > 0 ? "Needs attention" : "Finished"} · {ago(t.updated_at)}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      <aside className="flex flex-col gap-8">
        <section className={section}>
          <div className="flex items-center justify-between gap-3">
            <h2 className={heading}>{defaultCopy.project.subProjects}</h2>
            {project.actions.includes("create") && sub === null && (
              <button type="button" className={button} onClick={() => setSub("")}>
                <FolderPlusIcon size={16} /> {defaultCopy.project.newSubProject}
              </button>
            )}
          </div>
          {sub !== null && (
            <form
              className="flex gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                api
                  .createProject(sub.trim(), "", addressOf(project.name))
                  .then(() => {
                    setSub(null);
                    onChanged();
                  })
                  .catch(fail("The project could not be created."));
              }}
            >
              <input aria-label="Sub-project name" className={field} required maxLength={63} value={sub} onChange={(e) => setSub(e.target.value)} />
              <button type="submit" className={primary} disabled={!sub.trim()}>
                Create
              </button>
            </form>
          )}
          {detail.children.length === 0 ? (
            <p className="text-aui-sm text-aui-muted">{defaultCopy.project.noSubProjects}</p>
          ) : (
            <ul>
              {detail.children.map((c) => (
                <li key={c.name} className="border-aui-border border-t first:border-t-0">
                  <button type="button" className="flex w-full items-center gap-2 py-2 text-left text-aui-sm hover:bg-aui-hover" onClick={() => onOpenProject(c.name)}>
                    <FolderIcon size={14} />
                    <span className="min-w-0 flex-1 truncate">{titleOf(c, workspace)}</span>
                    <span className="text-aui-xs text-aui-muted">{defaultCopy.project.kind(c.kind)}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </section>

        <section className={section}>
          <div className="flex items-center justify-between gap-3">
            <h2 className={heading}>Scheduled ({timed.length})</h2>
            {canSchedule && agents.length > 0 && !adding && (
              <button type="button" className={button} onClick={() => setAdding(true)}>
                New Schedule
              </button>
            )}
          </div>
          <p className="max-w-xl text-aui-sm text-aui-muted">Each time one fires, its run is filed in this project and is sent what the project knows.</p>
          {adding && (
            <form
              className="flex flex-col gap-3 rounded-aui-lg border border-aui-border p-3"
              onSubmit={(e) => {
                e.preventDefault();
                setProblem(null);
                api
                  .createSchedule({ ...draft, agent, project: project.name })
                  .then(() => {
                    setDraft({ ...draft, name: "", text: "" });
                    setAdding(false);
                    onSchedulesChanged();
                    onChanged();
                  })
                  .catch(fail("The schedule could not be saved."));
              }}
            >
              <Label text="Name">
                <input className={field} value={draft.name} required onChange={(e) => setDraft({ ...draft, name: e.target.value })} />
              </Label>
              <Label text="Send to agent">
                <Menu label="Send to agent" value={agent} options={agentOptions(agents.map((a) => a.spec.name))} onChange={(v) => setDraft({ ...draft, agent: v })} />
              </Label>
              <Label text="How often" hint="minute hour day month weekday, in UTC">
                <input className={`${field} font-aui-mono`} value={draft.expression} required onChange={(e) => setDraft({ ...draft, expression: e.target.value })} />
              </Label>
              <Label text="What to ask">
                <textarea className={`${field} min-h-14`} value={draft.text} required onChange={(e) => setDraft({ ...draft, text: e.target.value })} />
              </Label>
              <div className="flex justify-end gap-2">
                <button type="button" className={button} onClick={() => setAdding(false)}>
                  Cancel
                </button>
                <button type="submit" className={primary}>
                  Create Schedule
                </button>
              </div>
            </form>
          )}
          {timed.length === 0 && !adding ? (
            <p className="text-aui-sm text-aui-muted">Nothing runs on a timer here.</p>
          ) : (
            <ul>
              {timed.map((s) => (
                <li key={s.name} className="flex items-center justify-between gap-3 border-aui-border border-t py-2 first:border-t-0">
                  <span className="min-w-0">
                    <span className="block text-aui-sm text-aui-fg">
                      {s.name}
                      <span className="pl-2 text-aui-xs text-aui-muted">
                        <code className="font-aui-mono">{s.expression}</code> to {s.agent}
                        {s.paused ? ", paused" : ""}
                      </span>
                    </span>
                    <span className="block truncate text-aui-xs text-aui-muted">{s.text}</span>
                  </span>
                  {canSchedule && (
                    <button
                      type="button"
                      className={button}
                      onClick={() =>
                        api
                          .runSchedule(s.name)
                          .then((run) => {
                            onSchedulesChanged();
                            onOpenThread(run.thread);
                          })
                          .catch(fail("The schedule could not be run."))
                      }
                    >
                      Run Now
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </section>
      </aside>
    </div>
  );
}
