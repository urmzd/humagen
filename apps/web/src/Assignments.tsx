import { defaultCopy } from "@humagen/components";
import { useCallback, useEffect, useState } from "react";
import { type Assignment, type AssignmentInput, api } from "./api.ts";
import { AgentMark, field, Label, Page, Problem, primary } from "./ui.tsx";

const words = defaultCopy.operations;

function inputOf(a: Assignment): AssignmentInput {
  return {
    id: a.id,
    agent: a.agent,
    project: a.project,
    thread: a.thread,
    owner: a.owner,
    goal: a.goal,
    done_when: a.done_when,
    due_at: a.due_at,
    cron: a.cron,
    state: a.state,
    blocker: a.blocker,
    version: a.version,
  };
}

export function Assignments({
  agents,
  projects,
  subject,
  canWrite,
  onRun,
  onConversation,
}: {
  agents: string[];
  projects: { name: string; title: string }[];
  subject: string;
  canWrite: boolean;
  onRun: (id: string) => void;
  onConversation?: (id: string) => void;
}) {
  const [items, setItems] = useState<Assignment[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [creating, setCreating] = useState(false);
  const [goal, setGoal] = useState("");
  const [done, setDone] = useState("");
  const [agent, setAgent] = useState("");
  const [project, setProject] = useState("");
  const [cron, setCron] = useState("");
  const [due, setDue] = useState("");
  const refresh = useCallback(async () => {
    try {
      setItems(await api.assignments());
    } catch (e) {
      setProblem(e instanceof Error ? e.message : words.loadFailed);
    }
  }, []);
  useEffect(() => {
    void refresh();
    const timer = setInterval(() => void refresh(), 5000);
    return () => clearInterval(timer);
  }, [refresh]);
  const act = async (fn: () => Promise<unknown>) => {
    if (busy) return;
    setBusy(true);
    setProblem(null);
    try {
      await fn();
      await refresh();
    } catch (e) {
      setProblem(e instanceof Error ? e.message : words.failed);
    } finally {
      setBusy(false);
    }
  };
  const create = () =>
    act(async () => {
      await api.saveAssignment({
        id: crypto.randomUUID(),
        agent,
        project,
        owner: subject,
        goal,
        done_when: done,
        cron,
        due_at: due ? new Date(due).toISOString() : undefined,
        state: "requested",
        version: 0,
      });
      setCreating(false);
      setGoal("");
      setDone("");
    });
  const state = (a: Assignment, next: Assignment["state"]) =>
    act(() => api.saveAssignment({ ...inputOf(a), state: next }));
  return (
    <Page
      title={words.assignments}
      lede={words.assignmentsHint}
      actions={
        canWrite ? (
          <button className={primary} type="button" onClick={() => setCreating(!creating)}>
            {words.newAssignment}
          </button>
        ) : undefined
      }
    >
      <Problem message={problem} />
      {creating && (
        <form
          className="mb-6 flex max-w-2xl flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            void create();
          }}
        >
          <Label text={words.agent}>
            <select
              className={field}
              required
              value={agent}
              onChange={(e) => setAgent(e.target.value)}
            >
              <option value="">{words.none}</option>
              {agents.map((a) => (
                <option key={a} value={a}>
                  {a}
                </option>
              ))}
            </select>
          </Label>
          <Label text={words.project}>
            <select className={field} value={project} onChange={(e) => setProject(e.target.value)}>
              <option value="">{words.workspace}</option>
              {projects
                .filter((p) => p.name !== "")
                .map((p) => (
                  <option key={p.name} value={p.name}>
                    {p.title}
                  </option>
                ))}
            </select>
          </Label>
          <Label text={words.goal}>
            <textarea
              className={field}
              required
              maxLength={8000}
              value={goal}
              onChange={(e) => setGoal(e.target.value)}
            />
          </Label>
          <Label text={words.doneWhen}>
            <textarea
              className={field}
              required
              maxLength={4000}
              value={done}
              onChange={(e) => setDone(e.target.value)}
            />
          </Label>
          <Label text={words.due}>
            <input
              className={field}
              type="datetime-local"
              value={due}
              onChange={(e) => setDue(e.target.value)}
            />
          </Label>
          <Label text={words.cron}>
            <input
              className={field}
              maxLength={128}
              value={cron}
              onChange={(e) => setCron(e.target.value)}
            />
          </Label>
          <button className={primary} type="submit" disabled={busy}>
            {words.create}
          </button>
        </form>
      )}
      {items.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">{words.empty}</p>
      ) : (
        <ul className="divide-y divide-aui-line">
          {items.map((a) => (
            <li key={a.id} className="flex flex-col gap-3 py-5">
              <div className="flex flex-wrap items-center gap-2">
                <AgentMark name={a.agent} />
                <span className="font-medium">{a.agent}</span>
                <span className="text-aui-xs text-aui-muted">{words.states[a.state]}</span>
              </div>
              <h2 className="text-aui-base font-medium">{a.goal}</h2>
              <p className="text-aui-sm text-aui-muted">
                {words.doneWhen}: {a.done_when}
              </p>
              <dl className="flex flex-wrap gap-x-5 gap-y-2 text-aui-xs text-aui-muted">
                <div>
                  <dt className="inline">{words.owner}: </dt>
                  <dd className="inline">{a.owner}</dd>
                </div>
                <div>
                  <dt className="inline">{words.project}: </dt>
                  <dd className="inline">{a.project || words.workspace}</dd>
                </div>
                {a.due_at && !a.due_at.startsWith("0001") && (
                  <div>
                    <dt className="inline">{words.due}: </dt>
                    <dd className="inline">
                      <time dateTime={a.due_at}>{new Date(a.due_at).toLocaleString()}</time>
                    </dd>
                  </div>
                )}
                {a.cron && (
                  <div>
                    <dt className="inline">{words.standing}: </dt>
                    <dd className="inline">{a.cron}</dd>
                  </div>
                )}
              </dl>
              {a.blocker && (
                <output className="text-aui-sm">
                  {words.blocker}: {a.blocker}
                </output>
              )}
              <div className="flex flex-wrap gap-4 text-aui-sm">
                {canWrite &&
                  a.owner === subject &&
                  a.state !== "cancelled" &&
                  a.state !== "in_progress" && (
                    <>
                      {a.state !== "result" && (
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() =>
                            void act(async () => {
                              const r = await api.runAssignment(a.id);
                              onRun(r.id);
                            })
                          }
                        >
                          {a.state === "blocked" ? words.retry : words.run}
                        </button>
                      )}
                      {a.cron && a.state !== "watching" && (
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() => void state(a, "watching")}
                        >
                          {words.watch}
                        </button>
                      )}
                      {a.state !== "result" && (
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() => void state(a, "cancelled")}
                        >
                          {words.cancel}
                        </button>
                      )}
                    </>
                  )}
                {a.run_id && (
                  <button type="button" onClick={() => onRun(a.run_id as string)}>
                    {words.openRun}
                  </button>
                )}
                {a.thread && onConversation && (
                  <button type="button" onClick={() => onConversation(a.thread as string)}>
                    {words.conversation}
                  </button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
    </Page>
  );
}
