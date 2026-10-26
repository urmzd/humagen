import { defaultCopy } from "@humagen/components";
import type {
  LibraryScope,
  LibrarySkill,
  LibrarySkillDefinition,
} from "../../../packages/client/src/generated/types.ts";
import { useEffect, useState } from "react";
import { api, type ProjectView, type Revision } from "./api.ts";
import { card, field, pageLede, pageTitle, Problem, SkeletonRows } from "./ui.tsx";

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
export function SkillsLibrary() {
  const copy = defaultCopy.skillsLibrary;
  const [scope, setScope] = useState<LibraryScope>({ kind: "personal" });
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
        if (live) setSkills(rows);
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
  const inspect = async (id: string) => {
    setBusy(true);
    setProblem(null);
    try {
      const rows = await api.librarySkill(id);
      setHistory(rows);
      const row = rows.at(-1);
      if (row) {
        setSelected(row);
        setDefinition(definitionOf(row));
        setTestThread("");
        setTestProject(row.scope.kind === "personal" ? personal : (row.scope.project ?? ""));
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
      setHistory(await api.librarySkill(selected.id));
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
    <section className="min-w-0 space-y-5">
      <header>
        <h1 className={pageTitle}>{copy.title}</h1>
        <p className={pageLede}>{copy.description}</p>
      </header>
      {scopeControl(scope, setScope)}
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
                  className="ml-2 min-h-11 bg-aui-bg"
                  value={selected.version}
                  onChange={(e) => {
                    const row = history.find((value) => value.version === Number(e.target.value));
                    if (row) {
                      setSelected(row);
                      setDefinition(definitionOf(row));
                    }
                  }}
                >
                  {history.map((row) => (
                    <option key={row.version} value={row.version}>
                      {row.version} · {row.published_at ? copy.published : copy.draft}
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
                    className={`${field} ml-2`}
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
                      className={`${field} ml-2 max-w-full`}
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
                    disabled={!mayWrite || busy || !!selected.published_at}
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
              {selected.from && (
                <p className="break-words text-aui-sm text-aui-muted">
                  {copy.provenance}: {selected.from.name} · {copy.version} {selected.from.version}
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
    </section>
  );
}
