import { defaultCopy } from "@warp/components";
import { type MouseEvent, useEffect, useState } from "react";
import { ApiError, api, type Person, type ProjectKindSpec, type ProjectView, type Revision } from "./api.ts";
import { nameOfAddress } from "./availability.ts";
import { factsOf } from "./Projects.tsx";
import { kindWord, titleOf } from "./projects.ts";
import { DIRECTORY_TABS, type DirectoryTab, href } from "./route.ts";
import { AgentMark, button, Page, Problem, rows, SearchField, SkeletonRows, Tabs } from "./ui.tsx";

const words = defaultCopy.directory;

/** A plain click goes there in place; a modified one is the browser's. */
const follow = (go: () => void) => (e: MouseEvent<HTMLAnchorElement>) => {
  if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  e.preventDefault();
  go();
};

/**
 * The Directory: who and what is in the workspace, for everyone in it. People
 * by name and handle with their own picture, agents by color with where each
 * lives and is enabled, and the project tree with what each project holds.
 * Each tab is shown to those who may read it.
 */
export function DirectoryPage({
  tab,
  onTab,
  can,
  agents,
  homes,
  enabledIn,
  projects,
  kinds,
  workspaceName,
  onOpenAgent,
  onOpenProject,
}: {
  tab: DirectoryTab;
  onTab: (tab: DirectoryTab) => void;
  can: (permission: string) => boolean;
  agents: Revision[];
  homes: Record<string, string>;
  enabledIn: Record<string, string[]>;
  /** The tree, null while it loads. */
  projects: ProjectView[] | null;
  /** The workspace's own kinds, so a project of one is called by its title. */
  kinds: ProjectKindSpec[];
  workspaceName: string;
  onOpenAgent: (name: string) => void;
  onOpenProject: (name: string) => void;
}) {
  const readable: Record<DirectoryTab, boolean> = {
    people: can("people:read"),
    agents: can("agents:read"),
    projects: can("projects:read"),
  };
  const tabs = DIRECTORY_TABS.filter((t) => readable[t]);
  const shown = readable[tab] ? tab : (tabs[0] ?? tab);
  const title = (name: string) => {
    if (name === "") return workspaceName;
    const p = (projects ?? []).find((x) => x.name === name);
    return p ? titleOf(p) : name;
  };
  return (
    <Page
      title={words.title}
      lede={words.lede}
      tabs={tabs.length > 1 ? <Tabs label={words.title} value={shown} tabs={tabs.map((id) => ({ id, label: words.tabs[id] }))} onChange={onTab} /> : undefined}
    >
      {shown === "people" && readable.people && <People />}
      {shown === "agents" && readable.agents && (
        <Agents agents={agents} homes={homes} enabledIn={enabledIn} title={title} onOpen={onOpenAgent} onOpenProject={onOpenProject} />
      )}
      {shown === "projects" && readable.projects && (
        <Projects projects={projects} kinds={kinds} homes={homes} workspaceName={workspaceName} onOpen={onOpenProject} />
      )}
    </Page>
  );
}

/** A person's picture: their own photo, or a still, neutral circle. Never a letter in a box. */
function Face({ person, size = 32 }: { person: Person; size?: number }) {
  if (person.picture) {
    return <img src={person.picture} alt="" width={size} height={size} className="shrink-0 rounded-full object-cover" style={{ width: size, height: size }} />;
  }
  return <span aria-hidden className="shrink-0 rounded-full bg-aui-hover" style={{ width: size, height: size }} />;
}

function People() {
  const [q, setQ] = useState("");
  const [people, setPeople] = useState<Person[] | null>(null);
  const [next, setNext] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    const timer = window.setTimeout(() => {
      api
        .people({ q: q.trim() || undefined })
        .then((page) => {
          if (!live) return;
          setPeople(page.people);
          setNext(page.next);
          setProblem(null);
        })
        .catch((e) => live && setProblem(e instanceof ApiError ? e.message : words.failed));
    }, 200);
    return () => {
      live = false;
      window.clearTimeout(timer);
    };
  }, [q]);

  const more = () => {
    if (!next) return;
    api
      .people({ q: q.trim() || undefined, before: next })
      .then((page) => {
        setPeople((was) => [...(was ?? []), ...page.people]);
        setNext(page.next);
      })
      .catch((e) => setProblem(e instanceof ApiError ? e.message : words.failed));
  };

  return (
    <section className="flex flex-col gap-4" aria-label={words.tabs.people}>
      <SearchField label={words.search} value={q} onChange={setQ} />
      <Problem message={problem} />
      {people === null ? (
        <SkeletonRows count={5} />
      ) : people.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">{q.trim() ? words.noMatch : words.empty.people}</p>
      ) : (
        <ul className={rows}>
          {people.map((p) => (
            <li key={p.handle} className="flex items-center gap-3 py-2.5">
              <Face person={p} />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-aui-sm text-aui-fg">{p.name || words.handle(p.handle)}</span>
                <span className="block truncate text-aui-xs text-aui-muted">
                  {words.handle(p.handle)} · {words.roles(p.roles)}
                </span>
              </span>
            </li>
          ))}
        </ul>
      )}
      {next && (
        <button type="button" className={`${button} self-start`} onClick={more}>
          {words.more}
        </button>
      )}
    </section>
  );
}

function Agents({
  agents,
  homes,
  enabledIn,
  title,
  onOpen,
  onOpenProject,
}: {
  agents: Revision[];
  homes: Record<string, string>;
  enabledIn: Record<string, string[]>;
  title: (name: string) => string;
  onOpen: (name: string) => void;
  onOpenProject: (name: string) => void;
}) {
  if (agents.length === 0) return <p className="text-aui-sm text-aui-muted">{words.empty.agents}</p>;
  return (
    <ul className={rows} aria-label={words.tabs.agents}>
      {agents.map((a) => {
        const name = a.spec.name;
        const home = homes[name];
        const enabled = (enabledIn[name] ?? []).map(nameOfAddress);
        const where = enabled.includes("") && !home ? words.enabledEverywhere : enabled.length > 0 ? words.enabledIn(enabled.map(title).join(", ")) : words.notEnabled;
        return (
          <li key={name} className="flex items-center gap-3 py-2.5">
            <AgentMark name={name} size={24} />
            <span className="min-w-0 flex-1">
              <a href={href({ page: "agent", name, tab: "access" })} onClick={follow(() => onOpen(name))} className="block truncate text-aui-sm leading-6 text-aui-fg hover:underline">
                {words.handle(name)}
              </a>
              <span className="block truncate text-aui-xs text-aui-muted">
                {home ? (
                  <>
                    {defaultCopy.agentPlace.livesIn("")}
                    <a href={href({ page: "project", name: home, tab: "agents" })} onClick={follow(() => onOpenProject(home))} className="inline-block min-h-6 leading-6 hover:underline">
                      {title(home)}
                    </a>
                  </>
                ) : (
                  words.platformAgent
                )}
                {!home && ` · ${where}`}
              </span>
              {a.spec.description && <span className="block truncate text-aui-xs text-aui-muted">{a.spec.description}</span>}
            </span>
          </li>
        );
      })}
    </ul>
  );
}

function Projects({
  projects,
  kinds,
  homes,
  workspaceName,
  onOpen,
}: {
  projects: ProjectView[] | null;
  kinds: ProjectKindSpec[];
  homes: Record<string, string>;
  workspaceName: string;
  onOpen: (name: string) => void;
}) {
  if (projects === null) return <SkeletonRows count={5} />;
  const living = (name: string) =>
    Object.entries(homes)
      .filter(([, h]) => h === name)
      .map(([a]) => a)
      .sort();
  return (
    <section className="flex flex-col gap-3" aria-label={words.tabs.projects}>
      <p className="text-aui-sm text-aui-fg">
        <a href={href({ page: "project", name: "", tab: "inbox" })} onClick={follow(() => onOpen(""))} className="inline-block min-h-6 leading-6 hover:underline">
          {workspaceName}
        </a>
      </p>
      {projects.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">{words.empty.projects}</p>
      ) : (
        <ul className={rows}>
          {projects.map((p) => {
            const here = living(p.name);
            return (
              <li key={p.name} className="flex items-start gap-3 py-2.5" style={{ paddingLeft: `${Math.min(p.depth - 1, 3) * 16}px` }}>
                <span className="min-w-0 flex-1">
                  <a href={href({ page: "project", name: p.name, tab: "inbox" })} onClick={follow(() => onOpen(p.name))} className="block truncate text-aui-sm leading-6 text-aui-fg hover:underline">
                    {titleOf(p)}
                  </a>
                  <span className="block truncate text-aui-xs text-aui-muted">
                    {[kindWord(p.kind, kinds), factsOf(p), words.holds(p.schedules, p.notes)].filter(Boolean).join(" · ")}
                  </span>
                  {here.length > 0 && (
                    <span className="flex flex-wrap items-center gap-1.5 pt-1 text-aui-xs text-aui-muted">
                      {here.map((a) => (
                        <span key={a} className="inline-flex items-center gap-1">
                          <AgentMark name={a} size={12} />
                          {a}
                        </span>
                      ))}
                    </span>
                  )}
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
