import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import type {
  LibraryScope,
  LibrarySkill,
  LibrarySkillDefinition,
  SkillUsage,
} from "../../../packages/client/src/generated/types.ts";
import { api, type ProjectView, type Revision } from "./api.ts";
import { AgentMark, card, field, Page, Problem, SkeletonRows } from "./ui.tsx";

const blank: LibrarySkillDefinition = {
  name: "",
  description: "",
  instructions: "",
  tools: [],
  inputs: "",
  outputs: "",
};
function definitionOf(row: LibrarySkill): LibrarySkillDefinition {
  return {
    name: row.name,
    description: row.description,
    instructions: row.instructions,
    tools: [...row.tools],
    inputs: row.inputs ?? "",
    outputs: row.outputs ?? "",
    argument_hint: row.argument_hint ?? "",
  };
}
export interface SkillsLibraryLocation {
  scope: "personal" | "workspace" | "project";
  project?: string;
  skill?: string;
  version?: number;
}
export function SkillsLibrary({
  onLocationChange,
}: {
  onLocationChange?: (location: SkillsLibraryLocation) => void;
} = {}) {
  const copy = defaultCopy.skillsLibrary;
  const [scope, setScope] = useState<LibraryScope>(() => {
    const query = new URLSearchParams(window.location.search);
    const kind = query.get("scope");
    return kind === "workspace"
      ? { kind }
      : kind === "project"
        ? { kind, project: query.get("project") ?? "" }
        : { kind: "personal" };
  });
  const [requestedSkill] = useState(() => new URLSearchParams(window.location.search).get("skill"));
  const [requestedVersion] = useState(() => {
    const raw = new URLSearchParams(window.location.search).get("version");
    if (raw === null) return undefined;
    const version = Number(raw);
    return Number.isSafeInteger(version) && version > 0 && version <= 32 ? version : 0;
  });
  const [skills, setSkills] = useState<LibrarySkill[] | null>(null);
  const [history, setHistory] = useState<LibrarySkill[]>([]);
  const [selected, setSelected] = useState<LibrarySkill | null>(null);
  const [definition, setDefinition] = useState<LibrarySkillDefinition>(blank);
  const [projects, setProjects] = useState<ProjectView[]>([]);
  const [agents, setAgents] = useState<Revision[]>([]);
  const [personal, setPersonal] = useState("");
  const [canWrite, setCanWrite] = useState(false);
  const [scopeOwner, setScopeOwner] = useState(false);
  const mayWrite = canWrite && (scope.kind === "personal" || scopeOwner);
  const [agent, setAgent] = useState("");
  const [request, setRequest] = useState("");
  const [testThread, setTestThread] = useState("");
  const [uses, setUses] = useState<SkillUsage | null>(null);
  const [usesFailed, setUsesFailed] = useState(false);
  const [testPassed, setTestPassed] = useState(false);
  const [testPending, setTestPending] = useState(false);
  const [testUses, setTestUses] = useState<{ id: string; thread: string; agent: string }[]>([]);
  const [testProject, setTestProject] = useState("");
  const [destination, setDestination] = useState<LibraryScope>({ kind: "project", project: "" });
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const failed = (error: unknown) =>
    setProblem(error instanceof Error ? error.message : copy.failed);
  useEffect(() => {
    let live = true;
    Promise.all([api.me(), api.projects(), api.agents()])
      .then(([me, places, workers]) => {
        if (!live) return;
        setProjects(places);
        setAgents(workers);
        setPersonal(me.personal_project?.replace(/^~/, "") ?? "");
        setCanWrite(me.permissions.includes("projects:write"));
        if (workers[0]) setAgent(workers[0].spec.name);
      })
      .catch((error) => {
        if (live) failed(error);
      });
    return () => {
      live = false;
    };
  }, []);
  useEffect(() => {
    let live = true;
    setSkills(null);
    setProblem(null);
    setHistory([]);
    setSelected(null);
    setDefinition(blank);
    setTestThread("");
    if (scope.kind === "project" && !scope.project) {
      setSkills([]);
      return () => {
        live = false;
      };
    }
    api
      .librarySkills(scope)
      .then((rows) => {
        if (live) {
          setSkills(rows);
          if (requestedSkill && rows.some((row) => row.id === requestedSkill))
            void inspect(requestedSkill, false, requestedVersion);
        }
      })
      .catch((error) => {
        if (live) failed(error);
      });
    return () => {
      live = false;
    };
  }, [scope.kind, scope.project, refresh]);
  useEffect(() => {
    let live = true;
    setScopeOwner(false);
    if (scope.kind !== "personal") {
      api
        .project(scope.project ?? "")
        .then((detail) => {
          if (live) setScopeOwner(detail.project.actions.includes("notes"));
        })
        .catch(() => {});
    }
    return () => {
      live = false;
    };
  }, [scope.kind, scope.project]);

  useEffect(() => {
    let live = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    setTestUses([]);
    setTestPassed(false);
    setTestPending(false);
    const read = async () => {
      if (!selected) return;
      const results = await Promise.allSettled(
        selected.tests.slice(0, 10).map((id) => api.run(id)),
      );
      if (!live) return;
      const readable = results.flatMap((result) =>
        result.status === "fulfilled" ? [result.value] : [],
      );
      setTestPassed(readable.some((run) => run.status === "completed"));
      const pending = readable.some((run) =>
        ["accepted", "running", "parked"].includes(run.status),
      );
      setTestPending(pending);
      setTestUses(
        readable.flatMap((run) =>
          run.thread ? [{ id: run.id, thread: run.thread, agent: run.agent }] : [],
        ),
      );
      if (pending) timer = setTimeout(() => void read(), 1000);
    };
    if (selected) void read();
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [selected]);

  useEffect(() => {
    let live = true;
    setUses(null);
    setUsesFailed(false);
    if (selected)
      api
        .skillAgents(selected.id, selected.version)
        .then((value) => {
          if (live) setUses(value);
        })
        .catch(() => {
          if (live) setUsesFailed(true);
        });
    return () => {
      live = false;
    };
  }, [selected]);
  const inspect = async (id: string, sync = true, version?: number) => {
    setBusy(true);
    setProblem(null);
    try {
      const rows = await api.librarySkill(id);
      setHistory(rows);
      const row = version === undefined ? rows.at(-1) : rows.find((row) => row.version === version);
      if (row) {
        setSelected(row);
        setDefinition(definitionOf(row));
        setTestThread("");
        setTestProject(row.scope.kind === "personal" ? personal : (row.scope.project ?? ""));
        if (sync)
          onLocationChange?.({
            scope: scope.kind,
            project: scope.project,
            skill: row.id,
            version: row.version,
          });
      } else {
        setSelected(null);
        setProblem(copy.versionUnavailable);
      }
    } catch (error) {
      failed(error);
    } finally {
      setBusy(false);
    }
  };
  const save = async () => {
    setBusy(true);
    setProblem(null);
    try {
      const row = await api.draftSkill(scope, definition, history.at(-1)?.version ?? 0);
      setSkills(await api.librarySkills(scope));
      setHistory(await api.librarySkill(row.id));
      setSelected(row);
      onLocationChange?.({
        scope: scope.kind,
        project: scope.project,
        skill: row.id,
        version: row.version,
      });
    } catch (error) {
      failed(error);
    } finally {
      setBusy(false);
    }
  };
  const test = async () => {
    if (!selected) return;
    setBusy(true);
    setProblem(null);
    try {
      const run = await api.testSkill(
        selected.id,
        selected.version,
        agent,
        testProject || "~",
        request,
      );
      setTestThread(run.thread);
      const revisions = await api.librarySkill(selected.id);
      setHistory(revisions);
      const tested = revisions.find((row) => row.version === selected.version);
      if (tested) setSelected(tested);
    } catch (error) {
      failed(error);
    } finally {
      setBusy(false);
    }
  };
  const publish = async () => {
    if (!selected) return;
    setBusy(true);
    setProblem(null);
    try {
      const row = await api.publishSkill(selected.id, selected.version);
      setSelected(row);
      setHistory(await api.librarySkill(row.id));
      setSkills(await api.librarySkills(scope));
    } catch (error) {
      failed(error);
    } finally {
      setBusy(false);
    }
  };
  const promote = async () => {
    if (!selected) return;
    setBusy(true);
    setProblem(null);
    try {
      const target = await api.librarySkills(destination);
      const version = target.find((row) => row.name === selected.name)?.version ?? 0;
      await api.promoteSkill(selected.id, selected.version, destination, version);
      setScope(destination);
      onLocationChange?.({ scope: destination.kind, project: destination.project });
    } catch (error) {
      failed(error);
    } finally {
      setBusy(false);
    }
  };
  const scopeControl = (value: LibraryScope, onChange: (scope: LibraryScope) => void) => (
    <div className="flex flex-wrap items-end gap-4">
      <label>
        {copy.scope}
        <select
          className={`${field} mt-2`}
          value={value.kind}
          onChange={(e) =>
            onChange(
              e.target.value === "project"
                ? { kind: "project", project: projects[0]?.name ?? "" }
                : { kind: e.target.value as "personal" | "workspace" },
            )
          }
        >
          {(["personal", "project", "workspace"] as const).map((kind) => (
            <option key={kind} value={kind}>
              {copy[kind]}
            </option>
          ))}
        </select>
      </label>
      {value.kind === "project" && (
        <label>
          {copy.projectName}
          <select
            className={`${field} mt-2 max-w-full`}
            value={value.project ?? ""}
            onChange={(e) => onChange({ ...value, project: e.target.value })}
          >
            <option value="">{copy.projectName}</option>
            {projects.map((project) => (
              <option key={project.name} value={project.name}>
                {project.title || project.name}
              </option>
            ))}
          </select>
        </label>
      )}
    </div>
  );
  return (
    <Page title={copy.title} lede={copy.description}>
      {scopeControl(scope, (next) => {
        setScope(next);
        onLocationChange?.({ scope: next.kind, project: next.project });
      })}
      {problem && <Problem message={problem} />}
      <div className="grid min-w-0 gap-6 lg:grid-cols-[minmax(12rem,1fr)_minmax(0,3fr)]">
        <aside className="min-w-0">
          <button
            type="button"
            disabled={!mayWrite || busy}
            className="mb-3 min-h-11 text-aui-accent disabled:opacity-50"
            onClick={() => {
              setSelected(null);
              setHistory([]);
              setDefinition(blank);
              onLocationChange?.({ scope: scope.kind, project: scope.project });
            }}
          >
            {copy.new}
          </button>
          {skills === null ? (
            <SkeletonRows count={3} />
          ) : skills.length === 0 ? (
            <p className="text-aui-muted">{copy.empty}</p>
          ) : (
            <ul className="space-y-2">
              {skills.map((skill) => (
                <li key={skill.id}>
                  <button
                    type="button"
                    onClick={() => void inspect(skill.id)}
                    disabled={busy}
                    aria-pressed={selected?.id === skill.id}
                    className={`${card} min-h-11 w-full break-words p-3 text-left`}
                  >
                    <strong className="block">{skill.name}</strong>
                    <span className="text-aui-sm text-aui-muted">
                      {copy.version} {skill.version} ·{" "}
                      {skill.published_at ? copy.published : copy.draft}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </aside>
        <div className="min-w-0 space-y-6">
          <form
            className={`${card} min-w-0 space-y-4 p-4`}
            onSubmit={(e) => {
              e.preventDefault();
              void save();
            }}
          >
            {selected && (
              <label>
                {copy.version}
                <select
                  className={`${field} mt-2 block max-w-full`}
                  value={selected.version}
                  disabled={busy}
                  onChange={(e) => {
                    const row = history.find((value) => value.version === Number(e.target.value));
                    if (row) {
                      setSelected(row);
                      setDefinition(definitionOf(row));
                      onLocationChange?.({
                        scope: scope.kind,
                        project: scope.project,
                        skill: row.id,
                        version: row.version,
                      });
                    }
                  }}
                >
                  {history.map((row) => (
                    <option key={row.version} value={row.version}>
                      {row.version}, {row.published_at ? copy.published : copy.draft}
                    </option>
                  ))}
                </select>
              </label>
            )}
            {(
              [
                ["name", copy.name, 64],
                ["description", copy.purpose, 1024],
                ["inputs", copy.inputs, 4000],
                ["outputs", copy.outputs, 4000],
              ] as const
            ).map(([key, label, max]) => (
              <label key={key} className="block">
                {label}
                <input
                  className={`${field} mt-2 w-full`}
                  maxLength={max}
                  required={key === "name" || key === "description"}
                  disabled={!mayWrite || busy || (key === "name" && !!selected)}
                  value={definition[key] ?? ""}
                  onChange={(e) => setDefinition({ ...definition, [key]: e.target.value })}
                />
              </label>
            ))}
            <label className="block">
              {copy.steps}
              <textarea
                className={`${field} mt-2 min-h-40 w-full`}
                required
                maxLength={8000}
                disabled={!mayWrite || busy}
                value={definition.instructions ?? ""}
                onChange={(e) => setDefinition({ ...definition, instructions: e.target.value })}
              />
            </label>
            <label className="block">
              {copy.tools}
              <textarea
                className={`${field} mt-2 min-h-20 w-full`}
                disabled={!mayWrite || busy}
                value={definition.tools.join("\n")}
                onChange={(e) =>
                  setDefinition({
                    ...definition,
                    tools: e.target.value
                      .split("\n")
                      .map((tool) => tool.trim())
                      .filter(Boolean),
                  })
                }
              />
              <span className="mt-2 block text-aui-sm text-aui-muted">{copy.toolsHint}</span>
            </label>
            {!mayWrite && <p className="text-aui-muted">{copy.readOnly}</p>}
            <button
              type="submit"
              disabled={!mayWrite || busy}
              className="min-h-11 text-aui-accent disabled:opacity-50"
            >
              {copy.save}
            </button>
          </form>
          {selected && (
            <>
              <form
                className={`${card} space-y-4 p-4`}
                onSubmit={(e) => {
                  e.preventDefault();
                  void test();
                }}
              >
                <h2 className="font-medium">{copy.test}</h2>
                <p className="text-aui-sm text-aui-muted">{copy.testHint}</p>
                <label className="block">
                  {copy.agent}
                  <select
                    className={`${field} mt-2 block max-w-full`}
                    value={agent}
                    onChange={(e) => setAgent(e.target.value)}
                  >
                    {agents.map((row) => (
                      <option key={row.spec.name} value={row.spec.name}>
                        {row.spec.name}
                      </option>
                    ))}
                  </select>
                </label>
                {selected.scope.kind === "workspace" && (
                  <label className="block">
                    {copy.project}
                    <select
                      className={`${field} mt-2 block max-w-full`}
                      value={testProject}
                      onChange={(e) => setTestProject(e.target.value)}
                    >
                      <option value="">{copy.workspace}</option>
                      {projects.map((project) => (
                        <option key={project.name} value={project.name}>
                          {project.title || project.name}
                        </option>
                      ))}
                    </select>
                  </label>
                )}
                <label className="block">
                  {copy.request}
                  <textarea
                    className={`${field} mt-2 min-h-20 w-full`}
                    required
                    maxLength={32000}
                    value={request}
                    onChange={(e) => setRequest(e.target.value)}
                  />
                </label>
                {!selected.published_at && !testPassed && (
                  <p className="text-aui-sm text-aui-muted">
                    {testPending ? copy.testPending : copy.publishNeedsTest}
                  </p>
                )}
                <div className="flex flex-wrap gap-4">
                  <button
                    type="submit"
                    disabled={!mayWrite || busy || !agent}
                    className="min-h-11 text-aui-accent disabled:opacity-50"
                  >
                    {copy.test}
                  </button>
                  <button
                    type="button"
                    disabled={!mayWrite || busy || !!selected.published_at || !testPassed}
                    className="min-h-11 text-aui-accent disabled:opacity-50"
                    onClick={() => void publish()}
                  >
                    {copy.publish}
                  </button>
                  {testThread && (
                    <a
                      className="inline-flex min-h-11 items-center text-aui-accent"
                      href={`/conversations/${encodeURIComponent(testThread)}`}
                    >
                      {copy.testRun}
                    </a>
                  )}
                </div>
              </form>

              <section className={`${card} space-y-3 p-4`} aria-label={copy.usedBy}>
                <h2 className="font-medium">{copy.usedBy}</h2>
                {usesFailed ? (
                  <Problem message={copy.usesFailed} />
                ) : uses === null ? (
                  <output>{copy.loadingUses}</output>
                ) : (
                  <>
                    {uses.agents.length === 0 ? (
                      <p className="text-aui-sm text-aui-muted">{copy.noUses}</p>
                    ) : (
                      <ul className="space-y-2">
                        {uses.agents.map((use) => (
                          <li key={use.agent} className="flex flex-wrap items-center gap-3">
                            <AgentMark name={use.agent} />
                            <a
                              className="inline-flex min-h-11 items-center text-aui-accent"
                              href={`/agents/${encodeURIComponent(use.agent)}/context`}
                              target="_blank"
                              rel="noreferrer"
                            >
                              {use.agent}
                            </a>
                            {use.needs_review && (
                              <span className="text-aui-sm text-aui-muted">
                                {copy.bindingNeedsReview}
                              </span>
                            )}
                          </li>
                        ))}
                      </ul>
                    )}
                    {uses.more && <p className="text-aui-sm text-aui-muted">{copy.moreUses}</p>}
                  </>
                )}
              </section>
              {testUses.length > 0 && (
                <section className={`${card} space-y-3 p-4`}>
                  <h2 className="font-medium">{copy.testUses}</h2>
                  <ul className="space-y-2">
                    {testUses.map((run) => (
                      <li key={run.id} className="flex flex-wrap gap-4">
                        <a
                          className="inline-flex min-h-11 items-center text-aui-accent"
                          href={`/agents/${encodeURIComponent(run.agent)}`}
                        >
                          {run.agent}
                        </a>
                        <a
                          className="inline-flex min-h-11 items-center text-aui-accent"
                          href={`/conversations/${encodeURIComponent(run.thread)}`}
                        >
                          {copy.testRun}
                        </a>
                      </li>
                    ))}
                  </ul>
                </section>
              )}
              {selected.from && (
                <p className="break-words text-aui-sm text-aui-muted">
                  {copy.provenance}: {selected.from.name}, {copy.version} {selected.from.version}
                </p>
              )}
              <section className={`${card} space-y-4 p-4`}>
                <h2 className="font-medium">{copy.promotion}</h2>
                <p className="text-aui-sm text-aui-muted">{copy.promotionHint}</p>
                {scopeControl(destination, setDestination)}
                <button
                  type="button"
                  disabled={
                    !mayWrite ||
                    busy ||
                    !selected.published_at ||
                    (destination.kind === "project" && !destination.project)
                  }
                  className="min-h-11 text-aui-accent disabled:opacity-50"
                  onClick={() => void promote()}
                >
                  {copy.promote}
                </button>
              </section>
            </>
          )}
        </div>
      </div>
      <button
        type="button"
        className="min-h-11 text-aui-accent"
        disabled={busy}
        onClick={() => setRefresh((n) => n + 1)}
      >
        {copy.refresh}
      </button>
    </Page>
  );
}
