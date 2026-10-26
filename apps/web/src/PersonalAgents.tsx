import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { api, type AgentControl, type ProjectView, type Revision } from "./api.ts";
import { AgentMark, card, field, Problem, SkeletonRows } from "./ui.tsx";

export function PersonalAgents() {
  const copy = defaultCopy.personalAgents;
  const [agents, setAgents] = useState<Revision[] | null>(null);
  const [projects, setProjects] = useState<ProjectView[]>([]);
  const [controls, setControls] = useState<Record<string, AgentControl>>({});
  const [canWrite, setCanWrite] = useState(false);
  const [failed, setFailed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [destination, setDestination] = useState<Record<string, string>>({});
  const [review, setReview] = useState<string>();
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    let live = true;
    setFailed(false);
    Promise.all([api.me(), api.agentList(), api.projects()])
      .then(async ([me, list, places]) => {
        const home = me.personal_project?.replace(/^~/, "");
        const mine = list.agents.filter((row) => home && list.homes?.[row.spec.name] === home);
        const states = await Promise.all(mine.map((row) => api.agentControl(row.spec.name)));
        if (live) {
          setAgents(mine);
          setProjects(
            places.filter((project) => !project.personal_owner && project.actions.includes("edit")),
          );
          setCanWrite(me.permissions.includes("agents:write"));
          setControls(
            Object.fromEntries(
              mine.flatMap((row, index) => {
                const control = states[index];
                return control ? [[row.spec.name, control]] : [];
              }),
            ),
          );
        }
      })
      .catch(() => {
        if (live) setFailed(true);
      });
    return () => {
      live = false;
    };
  }, [refresh]);
  const pause = async (name: string) => {
    const current = controls[name];
    if (!current) return;
    setBusy(true);
    setFailed(false);
    try {
      const control = await api.saveAgentControl(name, {
        version: current.version,
        paused: !current.paused,
        manager: current.manager,
        stop_rules: current.stop_rules,
      });
      setControls({ ...controls, [name]: control });
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  const move = async (name: string) => {
    if (!destination[name]) return;
    setBusy(true);
    setFailed(false);
    try {
      await api.setAgentHome(name, destination[name]);
      setReview(undefined);
      setRefresh((value) => value + 1);
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="min-w-0 space-y-5">
      {failed && <Problem message={copy.failed} />}
      <p className="text-aui-muted">{copy.description}</p>
      {canWrite && (
        <a className="inline-flex min-h-11 items-center text-aui-accent" href="/agents/new">
          {copy.create}
        </a>
      )}
      {agents === null ? (
        <SkeletonRows count={3} />
      ) : agents.length === 0 ? (
        <p className="text-aui-muted">{copy.empty}</p>
      ) : (
        <ul className="space-y-4">
          {agents.map((row) => (
            <li key={row.spec.name} className={`${card} min-w-0 space-y-4 p-4`}>
              <div className="flex flex-wrap items-center gap-3">
                <AgentMark name={row.spec.name} />
                <h2 className="font-medium">{row.spec.name}</h2>
                <a
                  className="inline-flex min-h-11 items-center text-aui-accent"
                  href={`/agents/${encodeURIComponent(row.spec.name)}`}
                >
                  {copy.settings}
                </a>
                <button
                  type="button"
                  disabled={!canWrite || busy || !controls[row.spec.name]}
                  className="min-h-11 text-aui-accent disabled:opacity-50"
                  onClick={() => void pause(row.spec.name)}
                >
                  {controls[row.spec.name]?.paused ? copy.resume : copy.pause}
                </button>
              </div>
              <details>
                <summary className="min-h-11 cursor-pointer">{copy.add}</summary>
                <label className="block">
                  {copy.project}
                  <select
                    className={`${field} mt-2 max-w-full`}
                    disabled={!canWrite || busy || review === row.spec.name}
                    aria-label={copy.project}
                    value={destination[row.spec.name] ?? ""}
                    onChange={(event) =>
                      setDestination({ ...destination, [row.spec.name]: event.target.value })
                    }
                  >
                    <option value="">{copy.project}</option>
                    {projects.map((project) => (
                      <option key={project.name} value={project.name}>
                        {project.title || project.name}
                      </option>
                    ))}
                  </select>
                </label>
                {review === row.spec.name ? (
                  <div className="mt-4 space-y-3">
                    <p>{copy.audience}</p>
                    <div className="flex flex-wrap gap-4">
                      <button
                        type="button"
                        disabled={busy}
                        className="min-h-11 text-aui-accent"
                        onClick={() => void move(row.spec.name)}
                      >
                        {copy.confirm}
                      </button>
                      <button
                        type="button"
                        disabled={busy}
                        className="min-h-11 text-aui-muted"
                        onClick={() => setReview(undefined)}
                      >
                        {copy.cancel}
                      </button>
                    </div>
                  </div>
                ) : (
                  <button
                    type="button"
                    disabled={!canWrite || busy || !destination[row.spec.name]}
                    className="min-h-11 text-aui-accent disabled:opacity-50"
                    onClick={() => setReview(row.spec.name)}
                  >
                    {copy.review}
                  </button>
                )}
              </details>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
