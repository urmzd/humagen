import { FolderIcon, FolderPlusIcon } from "@warp/render-web";
import { type FormEvent, useEffect, useState } from "react";
import { ApiError, api, type Note, type ProjectView, type Revision, type ScheduleView, type ThreadSummary } from "./api.ts";
import { ScopeLimits } from "./Billing.tsx";
import { AgentMark, agentOptions, ago, button, field, Label, Menu, Page, Popover, Problem, primary } from "./ui.tsx";

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
                  <span className="min-w-0 flex-1 truncate">{p.name}</span>
                </span>
                {p.description && <span className="line-clamp-2 text-aui-sm text-aui-muted">{p.description}</span>}
                <span className="text-aui-2xs text-aui-muted tabular-nums">
                  {/* What is happening or waiting comes first. */}
                  {[
                    p.active > 0 && `${p.active} running`,
                    p.proposed > 0 && `${p.proposed} to review`,
                    `${p.conversations} ${p.conversations === 1 ? "task" : "tasks"}`,
                    p.updated_at && `updated ${ago(p.updated_at)}`,
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

/** What every agent in the project is sent, and what is waiting to become that. */
function SharedMemory({ project, canWrite, onChanged }: { project: string; canWrite: boolean; onChanged: () => void }) {
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
      {canWrite && (
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

/** One project: its conversations, what it knows, and what runs in it on a timer. */
export function ProjectPage({
  project,
  threads,
  schedules,
  agents,
  canWrite,
  canSchedule,
  canLimit,
  tick,
  onNewChat,
  onOpenThread,
  onChanged,
  onSchedulesChanged,
  onDeleted,
}: {
  project: ProjectView;
  threads: ThreadSummary[];
  schedules: ScheduleView[];
  agents: Revision[];
  canWrite: boolean;
  canSchedule: boolean;
  /** May change what the project is allowed to use. */
  canLimit: boolean;
  /** Changes when runs do, so usage is read again. */
  tick?: number;
  onNewChat: () => void;
  onOpenThread: (id: string) => void;
  onChanged: () => void;
  onSchedulesChanged: () => void;
  onDeleted: () => void;
}) {
  const mine = threads.filter((t) => t.project === project.name);
  const timed = schedules.filter((s) => s.project === project.name);
  const [draft, setDraft] = useState({ name: "", expression: "0 9 * * 1-5", agent: "", text: "" });
  const [adding, setAdding] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const agent = draft.agent || agents[0]?.spec.name || "";
  const fail = (fallback: string) => (e: unknown) => setProblem(e instanceof ApiError ? e.message : fallback);

  return (
    <Page
      title={project.name}
      lede={project.description || "Tasks, shared memory and schedules that belong together."}
      actions={
        canWrite ? (
          <>
            <button
              type="button"
              className={button}
              onClick={() => api.deleteProject(project.name).then(onDeleted).catch(fail("The project could not be deleted."))}
            >
              Delete project
            </button>
            <button type="button" className={primary} onClick={onNewChat}>
              New conversation
            </button>
          </>
        ) : undefined
      }
    >
      <Problem message={problem} />

      <section className={section}>
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
                  <AgentMark
                    name={t.agents[t.agents.length - 1] ?? "?"}
                    size={14}
                    state={t.active > 0 ? "thinking" : t.failed > 0 ? "error" : "idle"}
                  />
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

      <SharedMemory project={project.name} canWrite={canWrite} onChanged={onChanged} />

      <section className={section}>
        <h2 className={heading}>Limits</h2>
        <ScopeLimits key={project.name} scope={{ kind: "project", name: project.name }} canWrite={canLimit} tick={tick} />
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
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
              <Label text="Name">
                <input className={field} value={draft.name} required onChange={(e) => setDraft({ ...draft, name: e.target.value })} />
              </Label>
              <Label text="Send to agent">
                <Menu label="Send to agent" value={agent} options={agentOptions(agents.map((a) => a.spec.name))} onChange={(v) => setDraft({ ...draft, agent: v })} />
              </Label>
              <Label text="How often" hint="minute hour day month weekday, in UTC">
                <input
                  className={`${field} font-aui-mono`}
                  value={draft.expression}
                  required
                  onChange={(e) => setDraft({ ...draft, expression: e.target.value })}
                />
              </Label>
            </div>
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
    </Page>
  );
}
