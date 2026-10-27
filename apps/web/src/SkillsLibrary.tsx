import { defaultCopy } from "@humagen/components";
import { useEffect, useRef, useState } from "react";
import { zipSync } from "fflate";
import { Markdown } from "@humagen/primitives";
import {
  WebSurface,
  FolderIcon,
  ProjectIcon,
  SearchIcon,
  PlusIcon,
  UploadIcon,
} from "@humagen/render-web";
import { ResourceBrowser, FileBrowser, Inspector } from "@humagen/web";
import { Button } from "@humagen/render-web/ui/button";
import { Input } from "@humagen/render-web/ui/input";
import { Textarea } from "@humagen/render-web/ui/textarea";
import { NativeSelect } from "@humagen/render-web/ui/native-select";
import { Badge } from "@humagen/render-web/ui/badge";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@humagen/render-web/ui/tabs";
import { Label } from "@humagen/render-web/ui/label";
import {
  importSkillFolder,
  skillFiles,
  skillFileText,
  skillFileBytes,
  textSkillFile,
  validSkillPath,
} from "./skill-files.ts";
import type {
  LibraryScope,
  LibrarySkill,
  LibrarySkillDefinition,
  SkillUsage,
} from "../../../packages/client/src/generated/types.ts";
import { api, type ProjectAgentsView, type ProjectView, type Revision } from "./api.ts";
import { availableOf } from "./availability.ts";
import { AgentMark, Page, Problem, SkeletonRows } from "./ui.tsx";

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
    files: skillFiles(row),
  };
}
export interface SkillsLibraryLocation {
  scope: "personal" | "workspace" | "project";
  project?: string;
  skill?: string;
  version?: number;
  file?: string;
  libraryTab?: "overview" | "contents" | "versions";
}
export function SkillsLibrary({
  onLocationChange,
  file,
  libraryTab,
}: {
  file?: string;
  libraryTab?: "overview" | "contents" | "versions";
  onLocationChange?: (location: SkillsLibraryLocation) => void;
} = {}) {
  const copy = defaultCopy.skillsLibrary;
  const [tab, setTab] = useState<"overview" | "contents" | "versions">(libraryTab ?? "overview");
  const [path, setPath] = useState(file ?? "SKILL.md");
  const [source, setSource] = useState(false);
  const [creating, setCreating] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [fileOpen, setFileOpen] = useState(false);
  const [newName, setNewName] = useState("");
  const [newPurpose, setNewPurpose] = useState("");
  const [newPath, setNewPath] = useState("");
  const [query, setQuery] = useState("");
  const [dirty, setDirty] = useState(false);
  const [loadFailed, setLoadFailed] = useState(false);
  const upload = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (file !== undefined) setPath(file);
  }, [file]);
  useEffect(() => {
    if (libraryTab !== undefined) setTab(libraryTab);
  }, [libraryTab]);
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
  const [homes, setHomes] = useState<Record<string, string>>({});
  const [availableAt, setAvailableAt] = useState<{
    project: string;
    view: ProjectAgentsView;
  } | null>(null);
  const [availabilityFailed, setAvailabilityFailed] = useState(false);
  const [canTest, setCanTest] = useState(false);
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
    api
      .me()
      .then(async (me) => {
        if (!live) return;
        setCanTest(me.permissions.includes("runs:write"));
        setPersonal(me.personal_project?.replace(/^~/, "") ?? "");
        setCanWrite(me.permissions.includes("projects:write"));
        await Promise.allSettled([
          me.permissions.includes("projects:read")
            ? api.projects().then((places) => {
                if (live) setProjects(places);
              })
            : Promise.resolve(),
          me.permissions.includes("agents:read")
            ? api.agentList().then((workers) => {
                if (live) {
                  setAgents(workers.agents);
                  setHomes(workers.homes ?? {});
                  if (workers.agents[0]) setAgent(workers.agents[0].spec.name);
                }
              })
            : Promise.resolve(),
        ]);
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
    setLoadFailed(false);
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
        if (live) {
          setLoadFailed(true);
          failed(error);
        }
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
        setDirty(false);
        setCreating(false);
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
      setDefinition(definitionOf(row));
      setDirty(false);
      setCreating(false);
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
  const testDestination =
    selected?.scope.kind === "personal"
      ? personal
      : selected?.scope.kind === "project"
        ? (selected.scope.project ?? "")
        : testProject;
  const destinationReady = !!selected && (selected.scope.kind !== "personal" || personal !== "");
  const available =
    destinationReady && availableAt?.project === testDestination ? availableAt.view : null;
  const eligibleAgents = available
    ? availableOf(
        agents.map((row) => row.spec.name),
        available,
        homes,
      )
    : [];
  const eligibleKey = eligibleAgents.join("\0");
  useEffect(() => {
    let live = true;
    setAvailableAt(null);
    setAvailabilityFailed(false);
    if (destinationReady)
      api
        .projectAgents(testDestination)
        .then((view) => {
          if (live) setAvailableAt({ project: testDestination, view });
        })
        .catch(() => {
          if (live) setAvailabilityFailed(true);
        });
    return () => {
      live = false;
    };
  }, [testDestination, destinationReady]);
  useEffect(() => {
    if (available)
      setAgent((current) =>
        eligibleAgents.includes(current) ? current : (eligibleAgents[0] ?? ""),
      );
  }, [eligibleKey, available]);
  const test = async () => {
    if (!selected) return;
    if (!destinationReady) {
      setProblem(copy.testProjectUnavailable);
      return;
    }
    if (!eligibleAgents.includes(agent)) {
      setProblem(copy.noTestAgents);
      return;
    }
    setBusy(true);
    setProblem(null);
    try {
      const run = await api.testSkill(
        selected.id,
        selected.version,
        agent,
        testDestination || "~",
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
  const scopeID = scope.kind === "project" ? `project:${scope.project ?? ""}` : scope.kind;
  const scopes = [
    { id: "personal", label: copy.personal, icon: <FolderIcon /> },
    { id: "workspace", label: copy.workspace, icon: <FolderIcon /> },
    ...projects
      .filter((project) => project.name !== "" && !project.personal_owner)
      .map((project) => ({
        id: `project:${project.name}`,
        label: project.title || project.name,
        group: copy.projects,
        icon: <ProjectIcon />,
      })),
  ];
  const changeScope = (id: string) => {
    const next: LibraryScope = id.startsWith("project:")
      ? { kind: "project", project: id.slice(8) }
      : { kind: id as "personal" | "workspace" };
    setScope(next);
    setCreating(false);
    setDirty(false);
    onLocationChange?.({ scope: next.kind, project: next.project });
  };
  const locate = (nextTab: typeof tab, nextPath = path) => {
    setTab(nextTab);
    setPath(nextPath);
    if (selected)
      onLocationChange?.({
        scope: scope.kind,
        project: scope.project,
        skill: selected.id,
        version: requestedVersion === undefined ? undefined : selected.version,
        libraryTab: nextTab,
        ...(nextTab === "contents" ? { file: nextPath } : {}),
      });
  };
  const files = skillFiles(definition);
  const current = files.find((file) => file.path === path);
  const currentText = current ? skillFileText(current) : null;
  const visible = (skills ?? []).filter((row) =>
    `${row.name} ${row.description}`.toLowerCase().includes(query.toLowerCase().trim()),
  );
  const detail = !!selected || creating;
  const updateFile = (value: string) => {
    setDefinition({
      ...definition,
      files: files.map((file) => (file.path === path ? textSkillFile(path, value) : file)),
    });
    setDirty(true);
  };
  const importFolder = async (picked: File[]) => {
    if (!mayWrite || busy) return;
    setBusy(true);
    setProblem(null);
    try {
      const folder = await importSkillFolder(picked);
      const existing = skills?.find((row) => row.name === folder.name);
      const revisions = existing ? await api.librarySkill(existing.id) : [];
      setHistory(revisions);
      setSelected(existing ?? null);
      setDefinition({
        name: folder.name,
        description: existing?.description ?? "",
        instructions: existing?.instructions ?? "",
        tools: existing?.tools ?? [],
        files: folder.files,
      });
      setCreating(true);
      setDirty(true);
      setTab("contents");
      setPath("SKILL.md");
      setSource(true);
    } catch (error) {
      failed(error);
    } finally {
      setBusy(false);
    }
  };
  const download = () => {
    const archive = zipSync(
      Object.fromEntries(
        files.map((file) => [`${definition.name}/${file.path}`, skillFileBytes(file)]),
      ),
    );
    const url = URL.createObjectURL(
      new Blob([new Uint8Array(archive)], { type: "application/zip" }),
    );
    const link = document.createElement("a");
    link.href = url;
    link.download = `${definition.name}.zip`;
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  };
  return (
    <Page
      title={copy.title}
      lede={copy.description}
      wide
      actions={
        !detail && (
          <>
            <Button
              variant="outline"
              disabled={!mayWrite || busy}
              onClick={() => setCreateOpen(true)}
            >
              <PlusIcon />
              {copy.new}
            </Button>
            <Button disabled={!mayWrite || busy} onClick={() => upload.current?.click()}>
              <UploadIcon />
              {copy.import}
            </Button>
          </>
        )
      }
    >
      <input
        ref={upload}
        type="file"
        disabled={!mayWrite || busy}
        multiple
        {...{ webkitdirectory: "" }}
        className="sr-only"
        tabIndex={-1}
        aria-label={copy.import}
        onChange={(event) => {
          void importFolder(Array.from(event.target.files ?? []));
          event.target.value = "";
        }}
      />
      {problem && !createOpen && !fileOpen && <Problem message={problem} />}
      <ResourceBrowser
        disabled={busy || dirty}
        scopes={scopes}
        selectedScope={scopeID}
        scopeLabel={copy.scope}
        onScopeChange={changeScope}
      >
        <div className="flex min-w-0 flex-col gap-5">
          {!detail ? (
            <>
              <div className="relative">
                <SearchIcon className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
                <Input
                  type="search"
                  aria-label={copy.search}
                  placeholder={copy.search}
                  value={query}
                  className="pl-9"
                  onChange={(event) => setQuery(event.target.value)}
                />
              </div>
              <p className="text-sm text-muted-foreground">{copy.folderHint}</p>
              {loadFailed ? (
                <Button
                  variant="outline"
                  className="self-start"
                  onClick={() => setRefresh((n) => n + 1)}
                >
                  {copy.retry}
                </Button>
              ) : skills === null ? (
                <SkeletonRows count={3} />
              ) : visible.length === 0 ? (
                <p className="py-10 text-sm text-muted-foreground">
                  {query ? copy.noMatches : copy.empty}
                </p>
              ) : (
                <ul className="divide-y divide-border">
                  {visible.map((skill) => (
                    <li key={skill.id}>
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => void inspect(skill.id)}
                        className="flex min-h-20 w-full items-center gap-4 rounded-md px-3 py-4 text-left hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring"
                      >
                        <span className="flex size-10 shrink-0 items-center justify-center rounded-lg border border-border bg-card">
                          <FolderIcon size={20} />
                        </span>
                        <span className="min-w-0 flex-1">
                          <strong className="block truncate text-sm font-medium">
                            {skill.name}
                          </strong>
                          <span className="mt-1 block truncate text-sm text-muted-foreground">
                            {skill.description}
                          </span>
                        </span>
                        <span className="flex shrink-0 flex-col items-end gap-1 text-xs text-muted-foreground">
                          <span>
                            {copy.version} {skill.version}
                          </span>
                          <Badge variant="secondary">
                            {skill.published_at ? copy.published : copy.draft}
                          </Badge>
                        </span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </>
          ) : (
            <>
              <div className="flex flex-wrap items-center justify-between gap-3">
                <Button
                  variant="ghost"
                  disabled={dirty || busy}
                  onClick={() => {
                    setSelected(null);
                    setCreating(false);
                    setHistory([]);
                    onLocationChange?.({ scope: scope.kind, project: scope.project });
                  }}
                >
                  {copy.back}
                </Button>
                <div className="flex flex-wrap items-center gap-2">
                  <Button variant="outline" onClick={download}>
                    {copy.download}
                  </Button>
                  <Button disabled={!mayWrite || busy || !dirty} onClick={() => void save()}>
                    {copy.save}
                  </Button>
                  {dirty && (
                    <Button
                      variant="ghost"
                      disabled={busy}
                      onClick={() => {
                        if (selected) setDefinition(definitionOf(selected));
                        else {
                          setCreating(false);
                          setDefinition(blank);
                        }
                        setDirty(false);
                      }}
                    >
                      {copy.discardChanges}
                    </Button>
                  )}
                </div>
              </div>
              <header className="flex items-start gap-3">
                <FolderIcon size={24} />
                <div className="min-w-0 flex-1">
                  <h2 className="break-words text-xl font-semibold">{definition.name}</h2>
                  <p className="mt-1 text-sm text-muted-foreground">
                    {selected ? `${copy.version} ${selected.version}` : copy.draft}
                  </p>
                </div>
                <Badge variant={dirty ? "outline" : "secondary"}>
                  {dirty ? copy.changes : copy.clean}
                </Badge>
              </header>
              <Tabs value={tab} onValueChange={(value) => locate(value as typeof tab)}>
                <TabsList variant="line">
                  <TabsTrigger value="overview">{copy.overview}</TabsTrigger>
                  <TabsTrigger value="contents">{copy.contents}</TabsTrigger>
                  <TabsTrigger value="versions">{copy.versions}</TabsTrigger>
                </TabsList>
                <TabsContent value="overview" className="space-y-6 pt-4">
                  <p className="max-w-[70ch] text-sm leading-6">
                    {selected?.description || definition.description}
                  </p>
                  <WebSurface>
                    <Markdown text={selected?.instructions || definition.instructions || ""} />
                  </WebSurface>
                  {selected && (
                    <section
                      className="space-y-3 border-t border-border pt-5"
                      aria-label={copy.usedBy}
                    >
                      <h3 className="text-sm font-medium">{copy.usedBy}</h3>
                      {usesFailed ? (
                        <Problem message={copy.usesFailed} />
                      ) : uses === null ? (
                        <output className="text-sm text-muted-foreground">
                          {copy.loadingUses}
                        </output>
                      ) : (
                        <>
                          <p className="text-sm text-muted-foreground">
                            {uses.agents.length === 0 ? copy.noUses : ""}
                          </p>
                          <ul>
                            {uses.agents.map((use) => (
                              <li key={use.agent} className="flex items-center gap-2 py-2 text-sm">
                                <AgentMark name={use.agent} />
                                <a
                                  className="underline underline-offset-4"
                                  target="_blank"
                                  rel="noreferrer"
                                  href={`/agents/${encodeURIComponent(use.agent)}/context`}
                                >
                                  {use.agent}
                                </a>
                                {use.needs_review && (
                                  <Badge variant="outline">{copy.bindingNeedsReview}</Badge>
                                )}
                              </li>
                            ))}
                          </ul>
                          {uses.more && (
                            <p className="text-sm text-muted-foreground">{copy.moreUses}</p>
                          )}
                        </>
                      )}
                    </section>
                  )}
                </TabsContent>
                <TabsContent value="contents" className="pt-4">
                  <FileBrowser
                    paths={files.map((file) => file.path)}
                    selected={path}
                    onSelect={(next) => locate("contents", next)}
                    label={copy.files}
                  >
                    <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
                      <span
                        className="min-w-0 flex-1 truncate text-sm text-muted-foreground"
                        title={path}
                      >
                        {path}
                      </span>
                      <Button
                        variant="ghost"
                        size="sm"
                        aria-pressed={source}
                        onClick={() => setSource(!source)}
                      >
                        {source ? copy.preview : copy.source}
                      </Button>
                      {mayWrite && (
                        <Button variant="ghost" size="sm" onClick={() => setFileOpen(true)}>
                          <PlusIcon />
                          {copy.addFile}
                        </Button>
                      )}
                    </div>
                    {!current ? (
                      <p className="p-5 text-sm text-muted-foreground">{copy.selectedMissing}</p>
                    ) : currentText === null ? (
                      <p className="p-5 text-sm text-muted-foreground">{copy.binaryFile}</p>
                    ) : source ? (
                      <Textarea
                        aria-label={copy.editor}
                        value={currentText}
                        disabled={!mayWrite || busy}
                        maxLength={128 * 1024}
                        className="min-h-96 max-h-[65vh] resize-y rounded-none border-0 p-5 font-mono text-sm shadow-none focus-visible:ring-inset"
                        onChange={(event) => updateFile(event.target.value)}
                      />
                    ) : (
                      <div className="min-h-80 p-5">
                        {path.endsWith(".md") ? (
                          <WebSurface>
                            <Markdown
                              text={
                                path === "SKILL.md"
                                  ? currentText.replace(/^---\r?\n[\s\S]*?\r?\n---(?:\r?\n|$)/, "")
                                  : currentText
                              }
                            />
                          </WebSurface>
                        ) : (
                          <pre className="overflow-x-auto whitespace-pre-wrap break-words font-mono text-sm">
                            {currentText}
                          </pre>
                        )}
                      </div>
                    )}
                    {mayWrite && current && path !== "SKILL.md" && (
                      <div className="border-t border-border px-4 py-2">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            setDefinition({
                              ...definition,
                              files: files.filter((file) => file.path !== path),
                            });
                            setDirty(true);
                            locate("contents", "SKILL.md");
                          }}
                        >
                          {copy.removeFile}
                        </Button>
                      </div>
                    )}
                  </FileBrowser>
                  <details className="mt-5 text-sm">
                    <summary className="cursor-pointer py-2 font-medium">{copy.tools}</summary>
                    <label className="mt-2 flex flex-col gap-2">
                      <span className="text-muted-foreground">{copy.toolsHint}</span>
                      <Textarea
                        aria-label={copy.tools}
                        disabled={!mayWrite || busy}
                        value={definition.tools.join("\n")}
                        onChange={(event) => {
                          setDefinition({
                            ...definition,
                            tools: event.target.value
                              .split("\n")
                              .map((tool) => tool.trim())
                              .filter(Boolean),
                          });
                          setDirty(true);
                        }}
                      />
                    </label>
                  </details>
                  {!mayWrite && (
                    <p className="mt-3 text-sm text-muted-foreground">{copy.readOnly}</p>
                  )}
                </TabsContent>
                <TabsContent value="versions" className="space-y-6 pt-4">
                  <ul className="divide-y divide-border">
                    {history.map((row) => (
                      <li
                        key={row.version}
                        className="flex items-center justify-between gap-3 py-3"
                      >
                        <span className="text-sm">
                          {copy.version} {row.version}
                          <span className="ml-3 text-xs text-muted-foreground">
                            {row.published_at ? copy.published : copy.draft}
                          </span>
                        </span>
                        <Button
                          variant="outline"
                          size="sm"
                          aria-label={`${copy.preview} ${copy.version} ${row.version}`}
                          disabled={busy || dirty || selected?.version === row.version}
                          onClick={() => void inspect(row.id, true, row.version)}
                        >
                          {copy.preview}
                        </Button>
                      </li>
                    ))}
                  </ul>
                  {selected && (
                    <>
                      <form
                        className="space-y-4 border-t border-border pt-5"
                        onSubmit={(event) => {
                          event.preventDefault();
                          void test();
                        }}
                      >
                        <h3 className="text-sm font-medium">{copy.test}</h3>
                        <p className="text-sm text-muted-foreground">{copy.testHint}</p>
                        {selected.scope.kind === "workspace" && (
                          <label className="flex max-w-md flex-col gap-2 text-sm">
                            {copy.project}
                            <NativeSelect
                              className="w-full"
                              value={testProject}
                              onChange={(event) => setTestProject(event.target.value)}
                            >
                              <option value="">{copy.workspace}</option>
                              {projects.map((project) => (
                                <option key={project.name} value={project.name}>
                                  {project.title || project.name}
                                </option>
                              ))}
                            </NativeSelect>
                          </label>
                        )}
                        <label className="flex max-w-md flex-col gap-2 text-sm">
                          {copy.agent}
                          <NativeSelect
                            className="w-full"
                            value={agent}
                            disabled={busy || !available}
                            onChange={(event) => setAgent(event.target.value)}
                          >
                            {eligibleAgents.map((name) => (
                              <option key={name}>{name}</option>
                            ))}
                          </NativeSelect>
                        </label>
                        {availabilityFailed && <Problem message={copy.testAgentsUnavailable} />}
                        {!availabilityFailed && !available && (
                          <output className="text-sm text-muted-foreground">
                            {copy.testAgentsLoading}
                          </output>
                        )}
                        {available && eligibleAgents.length === 0 && (
                          <p className="text-sm text-muted-foreground">{copy.noTestAgents}</p>
                        )}
                        <label className="flex flex-col gap-2 text-sm">
                          {copy.request}
                          <Textarea
                            required
                            value={request}
                            onChange={(event) => setRequest(event.target.value)}
                          />
                        </label>
                        <div className="flex flex-wrap items-center gap-3">
                          <Button
                            variant="outline"
                            type="submit"
                            disabled={
                              !canTest ||
                              !mayWrite ||
                              busy ||
                              !request.trim() ||
                              !eligibleAgents.includes(agent) ||
                              dirty
                            }
                          >
                            {copy.test}
                          </Button>
                          {testThread && (
                            <a
                              className="text-sm underline underline-offset-4"
                              href={`/conversations/${encodeURIComponent(testThread)}`}
                            >
                              {copy.testRun}
                            </a>
                          )}
                        </div>
                      </form>
                      {testPending && (
                        <p className="text-sm text-muted-foreground">{copy.testPending}</p>
                      )}
                      <div className="flex flex-wrap items-center gap-3">
                        <Button
                          disabled={
                            !mayWrite || busy || dirty || !testPassed || !!selected.published_at
                          }
                          onClick={() => void publish()}
                        >
                          {copy.publish}
                        </Button>
                        {!testPassed && (
                          <p className="text-sm text-muted-foreground">{copy.publishNeedsTest}</p>
                        )}
                      </div>
                      {testUses.length > 0 && (
                        <ul>
                          {testUses.map((run) => (
                            <li key={run.id} className="flex flex-wrap gap-3 py-2 text-sm">
                              <a
                                className="underline"
                                href={`/agents/${encodeURIComponent(run.agent)}`}
                              >
                                {run.agent}
                              </a>
                              <a
                                className="underline"
                                href={`/conversations/${encodeURIComponent(run.thread)}`}
                              >
                                {copy.testRun}
                              </a>
                            </li>
                          ))}
                        </ul>
                      )}
                      {selected.from && (
                        <p className="text-sm text-muted-foreground">
                          {copy.provenance}: {selected.from.name}, {copy.version}{" "}
                          {selected.from.version}
                        </p>
                      )}
                      <section className="space-y-3 border-t border-border pt-5">
                        <h3 className="text-sm font-medium">{copy.promotion}</h3>
                        <p className="text-sm text-muted-foreground">{copy.promotionHint}</p>
                        <label className="flex max-w-md flex-col gap-2 text-sm">
                          {copy.scope}
                          <NativeSelect
                            className="w-full"
                            value={
                              destination.kind === "project"
                                ? `project:${destination.project}`
                                : destination.kind
                            }
                            onChange={(event) =>
                              setDestination(
                                event.target.value.startsWith("project:")
                                  ? { kind: "project", project: event.target.value.slice(8) }
                                  : { kind: event.target.value as "personal" | "workspace" },
                              )
                            }
                          >
                            <option value="workspace">{copy.workspace}</option>
                            <option value="personal">{copy.personal}</option>
                            {projects.map((project) => (
                              <option key={project.name} value={`project:${project.name}`}>
                                {project.title || project.name}
                              </option>
                            ))}
                          </NativeSelect>
                        </label>
                        <Button
                          variant="outline"
                          disabled={
                            !mayWrite ||
                            busy ||
                            dirty ||
                            !selected.published_at ||
                            (destination.kind === "project" && !destination.project)
                          }
                          onClick={() => void promote()}
                        >
                          {copy.promote}
                        </Button>
                      </section>
                    </>
                  )}
                </TabsContent>
              </Tabs>
            </>
          )}
        </div>
      </ResourceBrowser>
      <Inspector open={createOpen} title={copy.newFolder} onClose={() => setCreateOpen(false)}>
        {createOpen && (<form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault();
            const next = {
              ...blank,
              name: newName.trim(),
              description: newPurpose.trim(),
              instructions: `# ${newName.trim()}\n\n${copy.initialInstructions}`,
            };
            setDefinition({ ...next, files: skillFiles(next) });
            setSelected(null);
            setHistory([]);
            setCreating(true);
            setDirty(true);
            setTab("contents");
            setPath("SKILL.md");
            setSource(true);
            setCreateOpen(false);
          }}
        >
          <div className="space-y-2">
            <Label htmlFor="skill-name">{copy.name}</Label>
            <Input
              id="skill-name"
              required
              maxLength={64}
              pattern="[a-z][a-z0-9]*(-[a-z0-9]+)*"
              value={newName}
              onChange={(event) => setNewName(event.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="skill-purpose">{copy.purpose}</Label>
            <Textarea
              id="skill-purpose"
              required
              maxLength={1024}
              value={newPurpose}
              onChange={(event) => setNewPurpose(event.target.value)}
            />
          </div>
          <Button type="submit">{copy.newFolder}</Button>
        </form>)}
      </Inspector>
      <Inspector open={fileOpen} title={copy.addFile} onClose={() => setFileOpen(false)}>
        {fileOpen && (<form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault();
            if (
              !validSkillPath(newPath) ||
              files.some((file) => file.path === newPath) ||
              files.length >= 64
            ) {
              setProblem(copy.invalidFilePath);
              return;
            }
            setDefinition({ ...definition, files: [...files, textSkillFile(newPath, "")] });
            setDirty(true);
            setPath(newPath);
            setSource(true);
            setFileOpen(false);
            setNewPath("");
          }}
        >
          <label className="flex flex-col gap-2 text-sm">
            {copy.filePath}
            <Input required value={newPath} onChange={(event) => setNewPath(event.target.value)} />
            <span className="text-muted-foreground">{copy.filePathHint}</span>
          </label>
          <Problem message={problem} />
          <Button type="submit">{copy.addFile}</Button>
        </form>)}
      </Inspector>
    </Page>
  );
}
