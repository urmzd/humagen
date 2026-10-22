import { defaultCopy } from "@warp/components";
import { type FormEvent, useState } from "react";
import type { Run } from "./adapter.ts";
import { inWords, presets } from "./cron.ts";
import { ApiError, api, type Revision, type Schedule, type ScheduleView } from "./api.ts";
import { type ProjectRef, projectOptions, titleIn } from "./projects.ts";
import { deliverFor, deliverOptions } from "./recap.ts";
import { AgentMark, agentOptions, ago, button, card, Chip, Empty, field, Label, Menu, Page, Problem, primary, rows, Section, textLink } from "./ui.tsx";

/** In UTC, because that is the zone the expression is written in. */
function when(iso: string): string {
  const at = new Date(iso).toLocaleString(undefined, {
    timeZone: "UTC",
    weekday: "short",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
  return `${at} UTC`;
}

const outcome: Record<Run["status"], { fill: string; word: string }> = {
  completed: { fill: "bg-aui-ok", word: "done" },
  failed: { fill: "bg-aui-bad", word: "failed" },
  stopped: { fill: "bg-aui-neutral", word: "stopped" },
  parked: { fill: "bg-aui-warn", word: "waiting" },
  running: { fill: "bg-aui-neutral", word: "running" },
  accepted: { fill: "bg-aui-neutral", word: "queued" },
};

/**
 * The last occurrences, oldest to newest, one mark each. Height separates a
 * failure from a success as well as color does, and every mark is a button
 * that names its outcome.
 */
function History({ runs, onOpenRun }: { runs: Run[]; onOpenRun?: (id: string) => void }) {
  if (runs.length === 0) return <span className="text-aui-xs text-aui-muted">Has not run yet</span>;
  const ordered = runs.slice().reverse();
  const failed = runs.filter((r) => r.status === "failed").length;
  return (
    <div className="flex items-end gap-3">
      <ul className="flex h-5 items-end gap-0.5" aria-label="Recent runs, oldest first">
        {ordered.map((r) => (
          <li key={r.id} className="flex h-full items-end">
            {onOpenRun ? (
            <button
              type="button"
              onClick={() => onOpenRun(r.id)}
              title={`${outcome[r.status].word}, ${ago(r.created_at)}`}
              aria-label={`Run ${ago(r.created_at)}, ${outcome[r.status].word}`}
              className={`w-1.5 rounded-t-sm ${outcome[r.status].fill} ${
                r.status === "failed" ? "h-full" : "h-3"
              } hover:opacity-70 focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-aui-accent`}
            />
            ) : (
              <span
                title={`${outcome[r.status].word}, ${ago(r.created_at)}`}
                className={`w-1.5 rounded-t-sm ${outcome[r.status].fill} ${r.status === "failed" ? "h-full" : "h-3"}`}
              />
            )}
          </li>
        ))}
      </ul>
      <span className="text-aui-xs text-aui-muted">
        Last {runs.length}: {runs.length - failed} ok, {failed} failed
      </span>
    </div>
  );
}

const copy = defaultCopy.schedules;

/** Create a schedule, or change one. Every field is checked by the server before anything is replaced. */
function ScheduleForm({
  editing,
  agents,
  projects,
  onClose,
  onSaved,
}: {
  editing?: ScheduleView;
  agents: Revision[];
  projects: ProjectRef[];
  onClose: () => void;
  onSaved: (name: string) => void;
}) {
  const [draft, setDraft] = useState<Schedule>(
    editing
      ? {
          name: editing.name,
          expression: editing.expression,
          agent: editing.agent,
          text: editing.text,
          project: editing.project,
          description: editing.description,
          deliver: editing.deliver,
        }
      : { name: "", expression: "0 9 * * 1-5", agent: "", text: "" },
  );
  const [problem, setProblem] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const set = (patch: Partial<Schedule>) => setDraft((d) => ({ ...d, ...patch }));
  const agent = draft.agent || agents[0]?.spec.name || "";

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setProblem(null);
    setSaving(true);
    const body = { ...draft, agent, project: draft.project || undefined, description: draft.description?.trim() || undefined };
    (editing ? saveSchedule(editing.name, body) : api.createSchedule(body))
      .then(() => onSaved(body.name))
      .catch((err) => setProblem(err instanceof ApiError ? err.message : "The schedule could not be saved."))
      .finally(() => setSaving(false));
  };

  return (
    <form onSubmit={submit} className={`${card} flex flex-col gap-4`}>
      <h2 className="text-aui-base font-medium text-aui-fg">{editing ? `Change ${editing.name}` : "New schedule"}</h2>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <Label text="Name" hint={editing ? "A schedule's name cannot change." : "Letters, digits, dashes."}>
          <input className={field} value={draft.name} required disabled={!!editing} onChange={(e) => set({ name: e.target.value })} />
        </Label>
        <Label text="Agent" hint="Who gets the request.">
          <Menu label="Agent" value={agent} options={agentOptions(agents.map((a) => a.spec.name))} onChange={(next) => set({ agent: next })} />
        </Label>
        <Label text="Project" hint="Where its conversations are filed. A project's shared memory is sent with each one.">
          <Menu
            label="Project"
            value={draft.project ?? ""}
            options={[{ value: "", label: "No project" }, ...projectOptions(projects)]}
            onChange={(next) => set({ project: next || undefined, deliver: deliverFor(next || undefined, draft.deliver) })}
          />
        </Label>
      </div>
      <Label text="When" hint={`${inWords(draft.expression)}. Fields: minute hour day month weekday, in UTC.`}>
        <input className={`${field} font-aui-mono`} value={draft.expression} required onChange={(e) => set({ expression: e.target.value })} />
      </Label>
      <div className="-mt-2 flex flex-wrap gap-1.5">
        {presets.map((p) => (
          <button
            key={p.expression}
            type="button"
            aria-pressed={draft.expression === p.expression}
            className={`${button} h-6 px-2 text-aui-xs aria-pressed:border-aui-accent`}
            onClick={() => set({ expression: p.expression })}
          >
            {p.label}
          </button>
        ))}
      </div>
      <Label text="What to ask" hint="Nobody is typing when a schedule fires, so this is the whole message the agent gets.">
        <textarea className={`${field} min-h-20`} value={draft.text} required onChange={(e) => set({ text: e.target.value })} />
      </Label>
      <Label text="Description" hint="Why it exists, for the people reading this page. The agent is not shown it.">
        <input className={field} maxLength={500} value={draft.description ?? ""} onChange={(e) => set({ description: e.target.value })} />
      </Label>
      <Label text={copy.deliver} hint={copy.deliverHint}>
        <Menu
          label={copy.deliver}
          value={draft.deliver ?? ""}
          options={deliverOptions(draft.project, copy, !editing)}
          onChange={(next) => set({ deliver: next === "inbox" || next === "project" ? next : undefined })}
        />
      </Label>
      <Problem message={problem} />
      <div className="flex justify-end gap-2">
        <button type="button" className={button} onClick={onClose}>
          Cancel
        </button>
        <button type="submit" className={primary} disabled={saving}>
          {editing ? "Save Changes" : "Create Schedule"}
        </button>
      </div>
    </form>
  );
}

/** Change a schedule in place, keeping its history and whether it is paused. */
function saveSchedule(name: string, body: Schedule): Promise<unknown> {
  const { name: _, ...edit } = body;
  return api.updateSchedule(name, edit);
}

/**
 * Every schedule, one row each, and one schedule on its own page. A row reads
 * left to right: what it is and whether it is on, who it asks, where its tasks
 * go, when it fires, and how the last ones went.
 */
export function Schedules({
  schedules,
  agents,
  projects = [],
  selected = null,
  onSelect,
  canWrite,
  onChanged,
  onOpenRun,
  onOpenAgent,
}: {
  schedules: ScheduleView[];
  agents: Revision[];
  /** Projects a schedule can file its tasks in, each named by its title. */
  projects?: ProjectRef[];
  /** The open schedule, by name. null lists them all. */
  selected?: string | null;
  onSelect?: (name: string | null) => void;
  canWrite: boolean;
  onChanged: () => void;
  onOpenRun: (id: string) => void;
  onOpenAgent?: (name: string) => void;
}) {
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [own, setOwn] = useState<string | null>(null);
  const open = onSelect ? selected : own;
  const select = (name: string | null) => {
    setEditing(false);
    setProblem(null);
    if (onSelect) onSelect(name);
    else setOwn(name);
  };

  const fail = (fallback: string) => (e: unknown) => setProblem(e instanceof ApiError ? e.message : fallback);
  const act = (p: Promise<unknown>, fallback: string, after?: () => void) => {
    setProblem(null);
    p.then(() => {
      onChanged();
      after?.();
    }).catch(fail(fallback));
  };
  const runNow = (s: ScheduleView) => {
    setProblem(null);
    api
      .runSchedule(s.name)
      .then((run) => {
        onChanged();
        onOpenRun(run.id);
      })
      .catch(fail("The schedule could not be run."));
  };

  if (open) {
    const s = schedules.find((x) => x.name === open);
    if (!s) {
      return (
        <Page title={`No schedule named ${open}`} lede="It may have been deleted." back={{ label: "All schedules", onClick: () => select(null) }}>
          {null}
        </Page>
      );
    }
    const failed = s.recent.filter((r) => r.status === "failed").length;
    return (
      <Page
        back={{ label: "All schedules", onClick: () => select(null) }}
        title={s.name}
        lede={s.description || undefined}
        meta={
          <>
            <Chip>{s.paused ? "Paused" : "Active"}</Chip>
            <span>{inWords(s.expression)}</span>
            {s.created_by && <span>Made by {s.created_by_name || s.created_by}</span>}
          </>
        }
        actions={
          canWrite ? (
            <>
              {!s.managed && (
                <button type="button" className={button} onClick={() => act(api.deleteSchedule(s.name), "The schedule could not be deleted.", () => select(null))}>
                  Delete
                </button>
              )}
              <button type="button" className={button} onClick={() => act(api.pauseSchedule(s.name, !s.paused), "The schedule could not be changed.")}>
                {s.paused ? "Resume" : "Pause"}
              </button>
              {!editing && !s.managed && (
                <button type="button" className={button} onClick={() => setEditing(true)}>
                  Edit
                </button>
              )}
              <button type="button" className={primary} onClick={() => runNow(s)}>
                Run Now
              </button>
            </>
          ) : undefined
        }
      >
        <Problem message={problem} />
        {s.managed && <p className="text-aui-xs text-aui-muted">{copy.managed}</p>}
        {editing ? (
          <ScheduleForm
            editing={s}
            agents={agents}
            projects={projects}
            onClose={() => setEditing(false)}
            onSaved={() => {
              setEditing(false);
              onChanged();
            }}
          />
        ) : (
          <dl className={`${card} grid grid-cols-1 gap-x-8 gap-y-3 text-aui-sm sm:grid-cols-[8rem_1fr]`}>
            <dt className="text-aui-xs text-aui-muted">Agent</dt>
            <dd className="flex items-center gap-2 text-aui-fg">
              <AgentMark name={s.agent} size={14} />
              {onOpenAgent ? (
                <button type="button" className={textLink} onClick={() => onOpenAgent(s.agent)}>
                  {s.agent}
                </button>
              ) : (
                s.agent
              )}
            </dd>
            <dt className="text-aui-xs text-aui-muted">Project</dt>
            <dd className="text-aui-fg">{s.project ? titleIn(projects, s.project) : <span className="text-aui-muted">None</span>}</dd>
            <dt className="text-aui-xs text-aui-muted">When</dt>
            <dd className="text-aui-fg">
              {inWords(s.expression)} <code className="font-aui-mono text-aui-xs text-aui-muted">{s.expression}</code>
            </dd>
            <dt className="text-aui-xs text-aui-muted">Asks</dt>
            <dd className="whitespace-pre-wrap text-aui-fg">{s.text}</dd>
          </dl>
        )}
        <Section title="Upcoming">
          {s.paused ? (
            <p className="text-aui-sm text-aui-muted">Paused. It will not fire until resumed.</p>
          ) : s.next.length === 0 ? (
            <p className="text-aui-sm text-aui-muted">Never, with this expression.</p>
          ) : (
            <ul className="flex flex-col gap-1 text-aui-sm text-aui-fg">
              {s.next.map((n) => (
                <li key={n}>{when(n)}</li>
              ))}
            </ul>
          )}
        </Section>
        <Section
          title="Recent runs"
          hint={s.recent.length > 0 ? `Last ${s.recent.length}: ${s.recent.length - failed} ok, ${failed} failed. Each opens the conversation it ran in.` : undefined}
        >
          {s.recent.length === 0 ? (
            <p className="text-aui-sm text-aui-muted">Has not run yet.</p>
          ) : (
            <ul className={rows}>
              {s.recent.map((r) => (
                <li key={r.id}>
                  <button
                    type="button"
                    onClick={() => onOpenRun(r.id)}
                    className="flex w-full items-center gap-3 px-1 py-2 text-left text-aui-sm hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                  >
                    <span aria-hidden className={`size-2 shrink-0 rounded-full ${outcome[r.status].fill}`} />
                    <span className="w-20 shrink-0 text-aui-fg">{outcome[r.status].word}</span>
                    <span className="min-w-0 flex-1 truncate text-aui-muted">{when(r.created_at)}</span>
                    <span className="shrink-0 text-aui-xs text-aui-muted">{ago(r.created_at)}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </Section>
      </Page>
    );
  }

  return (
    <Page
      title="Schedules"
      lede="A schedule sends an agent the same request on a timer. Each time it fires is a conversation you can open, filed in its project."
      actions={
        canWrite && agents.length > 0 && !adding ? (
          <button type="button" className={primary} onClick={() => setAdding(true)}>
            New Schedule
          </button>
        ) : undefined
      }
    >
      <Problem message={problem} />
      {adding && (
        <ScheduleForm
          agents={agents}
          projects={projects}
          onClose={() => setAdding(false)}
          onSaved={(name) => {
            setAdding(false);
            onChanged();
            select(name);
          }}
        />
      )}

      {schedules.length === 0 && !adding ? (
        <Empty title="No schedules">{agents.length === 0 ? "Define an agent first." : "Nothing runs on a timer yet."}</Empty>
      ) : (
        <ul className={rows} aria-label="Schedules">
          {schedules.map((s) => (
            <li key={s.name} className="warp-enter">
              <button
                type="button"
                onClick={() => select(s.name)}
                className="grid w-full grid-cols-1 items-center gap-x-6 gap-y-2 px-1 py-3 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent @3xl:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_minmax(0,1.2fr)_minmax(0,1fr)]"
              >
                <span className="min-w-0">
                  <span className="flex items-center gap-2">
                    <span className="truncate text-aui-base font-medium text-aui-fg">{s.name}</span>
                    {s.paused && <Chip>Paused</Chip>}
                  </span>
                  <span className="block truncate text-aui-xs text-aui-muted">{s.text}</span>
                </span>
                <span className="flex min-w-0 items-center gap-2 text-aui-sm text-aui-fg">
                  <AgentMark name={s.agent} size={12} />
                  <span className="truncate">{s.agent}</span>
                  {s.project && <span className="truncate text-aui-xs text-aui-muted">in {titleIn(projects, s.project)}</span>}
                </span>
                <span className="min-w-0 text-aui-sm">
                  <span className="block truncate text-aui-fg">{inWords(s.expression)}</span>
                  <span className="block truncate text-aui-xs text-aui-muted">
                    {s.paused ? "Paused" : s.next[0] ? `Next ${when(s.next[0])}` : "Never fires"}
                  </span>
                </span>
                <span className="min-w-0">
                  <History runs={s.recent.slice(0, 12)} />
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </Page>
  );
}
