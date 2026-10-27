import { defaultCopy } from "@humagen/components";
import { BotIcon, CheckIcon, ChevronRightIcon, FileIcon, ProjectIcon, WrenchIcon, XIcon } from "@humagen/render-web";
import { type KeyboardEvent, type ReactNode, useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { coworking, type ProjectKindSpec, type ProjectView } from "./api.ts";
import { ProjectChoices } from "./ProjectPicker.tsx";
import { above, titleOf } from "./projects.ts";
import { AgentMark, SearchField, SearchIcon } from "./ui.tsx";

const words = defaultCopy.chat;

/** How a message no one is tagged in goes to the agents selected: each that decides to join, or every one. */
export type Mode = "join" | "all";

/**
 * What a conversation is set to, from the + menu: the agents an untagged
 * message goes to and how, and where the conversation lives. `project`
 * null is not chosen yet; "" is the workspace.
 */
export interface Selection {
  agents: string[];
  mode: Mode;
  project: string | null;
  channel: string;
}

/** The most agents a conversation selects, and the most that answer one message side by side (pkg/threads). */
export const MAX_SELECTED = 5;
export const MAX_ALL = 3;

/**
 * The selection with one more agent, as the platform takes it: at most
 * MAX_SELECTED, and All of Them for MAX_ALL at most, past which each
 * decides whether to join. Null when the selection is full.
 */
export function withAgent(s: Selection, name: string): Selection | null {
  if (s.agents.includes(name)) return s;
  if (s.agents.length >= MAX_SELECTED) return null;
  const agents = [...s.agents, name];
  return { ...s, agents, mode: s.mode === "all" && agents.length > MAX_ALL ? "join" : s.mode };
}

export const emptySelection: Selection = { agents: [], mode: "join", project: null, channel: "general" };

/** The chat bar's prompt for a selection: how to bring an agent in, or who answers. */
export function placeholderFor(s: Pick<Selection, "agents" | "mode">): string {
  if (s.agents.length === 0) return words.placeholder;
  if (s.agents.length === 1) return words.placeholderOne(s.agents[0] ?? "");
  return s.mode === "all" ? words.placeholderAll(s.agents.length) : words.placeholderJoin(s.agents.length);
}

/** Every item the arrow keys move between inside the active pane. */
const ITEM = "[data-plus-item]:not(:disabled)";
const row =
  "flex min-h-11 w-full min-w-0 items-center gap-2.5 rounded-aui px-2.5 py-2 text-left text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent aria-checked:bg-aui-hover disabled:cursor-default disabled:text-aui-muted disabled:hover:bg-transparent";

type Pane = "root" | "agents" | "projects" | "channels" | "skills" | "finder";
type Choice = { name: string; description?: string };
type Channels = { project: string; state: "loading" | "ready" | "failed"; names: string[] };
type ReturnFocus = "agents" | "projects" | "skills" | "finder" | "selected-project";

/** The composer groups choices by intent; channels belong to a chosen project. */
export function PlusMenu({
  agents,
  selection,
  onChange,
  onAttach,
  onClose,
  onLeave,
  projects,
  kinds,
  canCreateProject = false,
  onProjectCreated,
  projectFixed,
  personalProject,
  finder,
  skills,
  skillsFailed = false,
  onSkill,
}: {
  /** The agents that may answer where the conversation lives. Null while they load. */
  agents: Choice[] | null;
  selection: Selection;
  onChange: (next: Selection) => void;
  onAttach: () => void;
  onClose: () => void;
  /** Closes it as focus moves elsewhere, leaving focus where it went. */
  onLeave?: () => void;
  /** The readable project tree, or null while it loads. */
  projects?: ProjectView[] | null;
  personalProject?: string;
  finder?: ReactNode;
  kinds?: ProjectKindSpec[];
  canCreateProject?: boolean;
  onProjectCreated?: (p: ProjectView) => void;
  /** Why the conversation's project and channel cannot change. */
  projectFixed?: string;
  /** Resolved skills for this project, not the skill-management library. */
  skills?: Choice[] | null;
  skillsFailed?: boolean;
  /** Prepares the message through the host's existing skill-tag insertion. */
  onSkill?: (name: string) => void;
}) {
  const c = words.picker;
  const [pane, setPane] = useState<Pane>("root");
  const [backward, setBackward] = useState(false);
  const [agentQuery, setAgentQuery] = useState("");
  const [skillQuery, setSkillQuery] = useState("");
  const [channelProject, setChannelProject] = useState<string | null>(null);
  const [channels, setChannels] = useState<Channels | null>(null);
  const [channelRetry, setChannelRetry] = useState(0);
  const panel = useRef<HTMLDivElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const backButton = useRef<HTMLButtonElement>(null);
  const finderButton = useRef<HTMLButtonElement>(null);
  const categoryButtons = useRef<Partial<Record<"agents" | "projects" | "skills", HTMLButtonElement | null>>>({});
  const returnFocus = useRef<ReturnFocus | undefined>(undefined);
  const offersProject = projects !== undefined;
  const project = selection.project;
  const scope = pane === "channels" ? channelProject : project;
  const scopedChannels = channels?.project === scope ? channels : null;

  const projectName = (name: string | null) => name === null ? words.chooseProject
    : name === "" ? words.projectless
    : name === personalProject ? defaultCopy.projects.personalTitle
    : projects?.find((item) => item.name === name)?.title || name;

  const open = (next: Pane) => {
    setBackward(false);
    returnFocus.current = undefined;
    setPane(next);
  };
  const back = () => {
    setBackward(true);
    if (pane === "finder") {
      returnFocus.current = "finder";
      setPane("agents");
    } else if (pane === "channels") {
      returnFocus.current = "selected-project";
      setPane("projects");
    } else if (pane !== "root") {
      returnFocus.current = pane;
      setPane("root");
    } else onClose();
  };

  useEffect(() => {
    const wanted = returnFocus.current;
    returnFocus.current = undefined;
    if (pane === "root") {
      (wanted === "agents" || wanted === "projects" || wanted === "skills"
        ? categoryButtons.current[wanted]
        : panel.current?.querySelector<HTMLButtonElement>(ITEM))?.focus();
    } else if (wanted === "finder") finderButton.current?.focus();
    else if (wanted === "selected-project") {
      (panel.current?.querySelector<HTMLButtonElement>('button[aria-pressed="true"]')
        ?? panel.current?.querySelector<HTMLInputElement>("input")
        ?? backButton.current)?.focus();
    } else {
      (panel.current?.querySelector<HTMLInputElement>("input") ?? backButton.current)?.focus();
    }
  }, [pane]);

  // The popup uses the side of the composer with more available space.
  const [place, setPlace] = useState({ up: true, max: 448 });
  useLayoutEffect(() => {
    const bar = panel.current?.parentElement?.closest("form") ?? panel.current?.parentElement;
    if (!bar) return;
    const rect = bar.getBoundingClientRect();
    const above = rect.top - 8;
    const below = window.innerHeight - rect.bottom - 8;
    const up = above >= below;
    setPlace({ up, max: Math.max(160, Math.min(448, up ? above : below)) });
  }, []);

  // Associate each response with its project. A late response cannot fill a
  // different project's pane, and a refused read never invents a channel.
  useEffect(() => {
    if (!offersProject || scope === null) {
      setChannels(null);
      return;
    }
    let live = true;
    setChannels({ project: scope, state: "loading", names: [] });
    coworking.channels(scope).then((items) => {
      if (!live) return;
      const names = items.filter((item) => !item.archived).map((item) => item.name);
      names.sort((a, b) => a === "general" ? -1 : b === "general" ? 1 : a.localeCompare(b));
      setChannels({ project: scope, state: "ready", names });
    }).catch(() => {
      if (live) setChannels({ project: scope, state: "failed", names: [] });
    });
    return () => { live = false; };
  }, [offersProject, scope, channelRetry]);

  useEffect(() => {
    if (pane !== "channels" || projectFixed || scopedChannels?.state !== "ready") return;
    if (scopedChannels.names.length === 1 && scopedChannels.names[0] === "general") {
      setPane("root");
      onClose();
    }
  }, [pane, projectFixed, scopedChannels, onClose]);

  const chooseProject = (name: string) => {
    if (projectFixed) return;
    if (name !== selection.project) onChange({ ...selection, project: name, channel: "general" });
    setChannelProject(name);
    open("channels");
  };
  const chooseChannel = (name: string, close = true) => {
    if (projectFixed || channelProject === null || scopedChannels?.state !== "ready" || !scopedChannels.names.includes(name)) return;
    onChange({ ...selection, project: channelProject, channel: name });
    if (close) onClose();
  };
  const full = selection.agents.length >= MAX_SELECTED;
  const toggle = (name: string) => {
    if (selection.agents.includes(name)) return onChange({ ...selection, agents: selection.agents.filter((agent) => agent !== name) });
    const next = withAgent(selection, name);
    if (next) onChange(next);
  };
  const matches = (choice: Choice, query: string) => !query || choice.name.toLowerCase().includes(query) || (choice.description ?? "").toLowerCase().includes(query);
  const shownAgents = (agents ?? []).filter((agent) => matches(agent, agentQuery.trim().toLowerCase()));
  const shownSkills = (skills ?? []).filter((skill) => matches(skill, skillQuery.trim().toLowerCase()));

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.nativeEvent.isComposing || event.defaultPrevented) return;
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      back();
      return;
    }
    const target = event.target as HTMLElement;
    const editing = target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target.isContentEditable;
    if ((pane === "projects" || pane === "finder") && target === backButton.current && event.key === "ArrowDown") {
      event.preventDefault();
      panel.current?.querySelector<HTMLInputElement>("input")?.focus();
      return;
    }
    if (event.key === "ArrowLeft" && pane !== "root" && !editing && target.getAttribute("role") !== "radio") {
      event.preventDefault();
      back();
      return;
    }
    if (pane === "projects" || pane === "finder") return;
    const items = [...(panel.current?.querySelectorAll<HTMLElement>(ITEM) ?? [])];
    if (pane === "channels" && target.getAttribute("role") === "radio" && ["ArrowDown", "ArrowUp", "ArrowRight", "ArrowLeft"].includes(event.key)) {
      const radios = items.filter((item) => item.getAttribute("role") === "radio");
      const at = radios.indexOf(target);
      const direction = event.key === "ArrowDown" || event.key === "ArrowRight" ? 1 : -1;
      const next = radios[(at + direction + radios.length) % radios.length];
      if (next?.dataset.channel) {
        event.preventDefault();
        next.focus();
        chooseChannel(next.dataset.channel, false);
      }
      return;
    }
    if (event.key === "ArrowRight" && pane === "root" && target.dataset.category) {
      event.preventDefault();
      target.click();
      return;
    }
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      if (items.length === 0) return;
      event.preventDefault();
      const choices = items.filter((item) => item.dataset.plusItem === (pane === "agents" ? "agent" : "skill"));
      if (document.activeElement === search.current) {
        (event.key === "ArrowDown" ? choices[0] ?? items[0] : backButton.current)?.focus();
        return;
      }
      if (event.key === "ArrowUp" && target === choices[0]) {
        search.current?.focus();
        return;
      }
      if (event.key === "ArrowDown" && target === backButton.current && search.current) {
        search.current.focus();
        return;
      }
      const at = items.indexOf(document.activeElement as HTMLElement);
      items[event.key === "ArrowDown" ? (at + 1) % items.length : (at <= 0 ? items.length : at) - 1]?.focus();
      return;
    }
    if (event.key === "Enter" && (target.dataset.plusItem === "skill" || target.dataset.plusItem === "channel")) {
      event.preventDefault();
      target.click();
      return;
    }
    if (event.key === "Enter" && pane === "agents" && (target === search.current || target.dataset.plusItem === "agent")) {
      event.preventDefault();
      onClose();
    } else if (event.key === "Enter" && target instanceof HTMLInputElement) {
      event.preventDefault();
    }
  };

  const heading = pane === "agents" ? words.agents
    : pane === "projects" ? defaultCopy.navigation.projects
    : pane === "skills" ? defaultCopy.navigation.skills
    : pane === "channels" ? c.channelsIn(projectName(channelProject))
    : undefined;
  const header = pane !== "root" && <div className="mb-1 flex min-w-0 items-center gap-2 border-b border-aui-border pb-1">
    <button ref={backButton} type="button" data-plus-item="action" className="inline-flex min-h-11 shrink-0 items-center gap-2 rounded-aui px-2.5 py-2 text-aui-xs text-aui-muted hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent" onClick={back}>
      <ChevronRightIcon size={16} className="rotate-180" />{c.back}
    </button>
    {heading && <h2 className="min-w-0 truncate text-aui-sm font-medium">{heading}</h2>}
  </div>;

  return (
    // biome-ignore lint/a11y/noStaticElementInteractions: the dialog moves focus between its controls with the arrow keys
    <div ref={panel} role="dialog" data-layer="menu" aria-label={words.menu} onKeyDown={onKeyDown}
      onBlur={(event) => {
        const to = event.relatedTarget as HTMLElement | null;
        if (to && !panel.current?.contains(to) && !to.closest("[data-plus-button]")) (onLeave ?? onClose)();
      }}
      style={{ maxHeight: place.max }}
      data-plus-menu
      className={`motion-safe:animate-aui-pop absolute left-0 z-30 flex w-full min-w-0 max-w-sm flex-col overflow-x-hidden overflow-y-auto rounded-aui border border-aui-border bg-aui-surface p-1.5 shadow-lg ${place.up ? "bottom-full mb-1 origin-bottom-left" : "top-full mt-1 origin-top-left"}`}
    >
      <div key={pane} data-direction={backward ? "back" : "forward"} className="min-w-0 motion-safe:animate-aui-pane data-[direction=back]:motion-safe:animate-aui-pane-back">
      {header}
      {pane === "root" && <>
        <button type="button" data-plus-item="action" className={row} onClick={onAttach}><FileIcon size={16} className="text-aui-muted" />{words.attach}</button>
        <button type="button" ref={(button) => { categoryButtons.current.agents = button; }} data-plus-item="action" data-category="agents" aria-haspopup="dialog" className={row} onClick={() => open("agents")}>
          <BotIcon size={16} className="text-aui-muted" /><span className="min-w-0 flex-1">{words.agents}</span>
          {selection.agents.length > 0 && <span className="truncate text-aui-xs text-aui-muted">{c.selected(selection.agents.length)}</span>}<ChevronRightIcon size={16} className="text-aui-muted" />
        </button>
        {offersProject && <>
          <button type="button" ref={(button) => { categoryButtons.current.projects = button; }} data-plus-item="action" data-category="projects" aria-haspopup="dialog" className={row} disabled={!!projectFixed} title={projectFixed} onClick={() => open("projects")}>
            <ProjectIcon size={16} className="text-aui-muted" /><span className="min-w-0 flex-1">{defaultCopy.navigation.projects}</span><span className="max-w-[45%] truncate text-aui-xs text-aui-muted">{projectName(project)}</span><ChevronRightIcon size={16} className="text-aui-muted" />
          </button>
          {projectFixed && <p className="px-2.5 pb-2 text-aui-xs text-aui-muted">{projectFixed}</p>}
        </>}
        {onSkill && <button type="button" ref={(button) => { categoryButtons.current.skills = button; }} data-plus-item="action" data-category="skills" aria-haspopup="dialog" className={row} onClick={() => open("skills")}>
          <WrenchIcon size={16} className="text-aui-muted" /><span className="min-w-0 flex-1">{defaultCopy.navigation.skills}</span><ChevronRightIcon size={16} className="text-aui-muted" />
        </button>}
      </>}
      {pane === "agents" && <>
        <div className="p-1"><SearchField label={words.findAgent} value={agentQuery} onChange={setAgentQuery} inputRef={search} /></div>
        {agents === null ? <p role="status" className="px-2.5 py-3 text-aui-xs text-aui-muted">{words.agentsLoading}</p>
          : shownAgents.length === 0 ? <p role="status" className="px-2.5 py-3 text-aui-xs text-aui-muted">{agents.length === 0 ? words.noAgents : words.noAgentMatch}</p>
          : <div role="group" aria-label={words.agents} className="flex flex-col">
            {shownAgents.map((agent) => {
              const checked = selection.agents.includes(agent.name);
              return <button key={agent.name} type="button" role="checkbox" aria-checked={checked} aria-disabled={!checked && full || undefined} data-plus-item="agent" className={row} onClick={() => toggle(agent.name)}>
                <AgentMark name={agent.name} size={16} /><span className="min-w-0 flex-1"><span className="block truncate">{agent.name}</span>{agent.description && <span className="block truncate text-aui-xs text-aui-muted">{agent.description}</span>}</span>
                <span aria-hidden className="inline-flex size-4 shrink-0 items-center justify-center">{checked && <CheckIcon size={14} />}</span>
              </button>;
            })}
            {full && <p className="px-2.5 py-1 text-aui-xs text-aui-muted">{words.maxAgents(MAX_SELECTED)}</p>}
          </div>}
        {finder && !projectFixed && <div className="mt-1 border-t border-aui-border pt-1"><button type="button" ref={finderButton} data-plus-item="action" className={row} onClick={() => open("finder")}><SearchIcon size={16} className="text-aui-muted" />{defaultCopy.home.findHelp.title}<ChevronRightIcon size={16} className="ml-auto text-aui-muted" /></button></div>}
      </>}
      {pane === "projects" && projects !== undefined && (projectFixed ? <p className="px-2.5 py-3 text-aui-xs text-aui-muted">{projectFixed}</p> : <ProjectChoices projects={projects} kinds={kinds} personalProject={personalProject} value={project} canCreate={canCreateProject} onChange={chooseProject} onCreated={(created) => onProjectCreated?.(created)} close={() => {}} />)}
      {pane === "channels" && <>
        {projectFixed && <p className="px-2.5 py-3 text-aui-xs text-aui-muted">{projectFixed}</p>}
        {!scopedChannels || scopedChannels.state === "loading" ? <p role="status" className="px-2.5 py-3 text-aui-xs text-aui-muted">{c.channelsLoading}</p>
          : scopedChannels.state === "failed" ? <div className="px-2.5 py-2"><p role="alert" className="text-aui-xs text-aui-muted">{c.channelsFailed}</p><button type="button" data-plus-item="action" className={`${row} mt-1`} onClick={() => setChannelRetry((value) => value + 1)}>{c.retry}</button></div>
          : scopedChannels.names.length === 0 ? <p role="status" className="px-2.5 py-3 text-aui-xs text-aui-muted">{c.noChannels}</p>
          : (scopedChannels.names.length > 1 || scopedChannels.names[0] !== "general") && <div role="radiogroup" aria-label={c.channelsIn(projectName(channelProject))} className="flex min-w-0 flex-col">
            {scopedChannels.names.map((name) => <button key={name} type="button" role="radio" aria-checked={selection.project === channelProject && selection.channel === name} data-plus-item="channel" data-channel={name} disabled={!!projectFixed} className={row} onClick={() => chooseChannel(name)}><span className="min-w-0 flex-1 truncate">#{name}</span><span aria-hidden className="inline-flex size-4 shrink-0 items-center justify-center">{selection.channel === name && <CheckIcon size={14} />}</span></button>)}
          </div>}
      </>}
      {pane === "skills" && <>
        <div className="p-1"><SearchField label={c.findSkill} value={skillQuery} onChange={setSkillQuery} inputRef={search} /></div>
        {skillsFailed ? <p role="alert" className="px-2.5 py-3 text-aui-xs text-aui-muted">{c.skillsFailed}</p>
          : skills == null ? <p role="status" className="px-2.5 py-3 text-aui-xs text-aui-muted">{c.skillsLoading}</p>
          : shownSkills.length === 0 ? <p role="status" className="px-2.5 py-3 text-aui-xs text-aui-muted">{skills.length === 0 ? c.noSkills : c.noSkillMatch}</p>
          : <div role="group" aria-label={defaultCopy.navigation.skills} className="flex flex-col">{shownSkills.map((skill) => <button key={skill.name} type="button" data-plus-item="skill" className={row} onClick={() => { onSkill?.(skill.name); onClose(); }}><WrenchIcon size={16} className="text-aui-muted" /><span className="min-w-0 flex-1"><span className="block truncate">{skill.name}</span>{skill.description && <span className="block truncate text-aui-xs text-aui-muted">{skill.description}</span>}</span></button>)}</div>}
      </>}
      {pane === "finder" && finder && <div data-plus-discovery>{finder}</div>}
      </div>
    </div>
  );
}

const chip = "inline-flex min-h-7 max-w-full min-w-0 items-center gap-1.5 pr-1 text-aui-xs text-aui-muted";
const remove =
  "inline-flex size-6 shrink-0 items-center justify-center rounded-aui-sm text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent";

/**
 * What the + menu selected, as text controls below the composer: each
 * agent with its color, the project with its path (and channel when not
 * general), each removable. With two agents or more, a text control says
 * how an untagged message is routed: Each Decides or All of Them.
 */
export function SelectionChips({
  selection,
  projects,
  workspace,
  onChange,
  projectFixed,
  personalProject,
  projectControl,
}: {
  projectControl?: ReactNode;
  selection: Selection;
  projects?: ProjectView[] | null;
  personalProject?:string;
  workspace?: string;
  onChange: (next: Selection) => void;
  /** The project can't be removed, as when an agent has run in the conversation. */
  projectFixed?: boolean;
}) {
  const { agents, mode, project, channel } = selection;
  const why = useId();
  // Why All of Them is not offered, said in words once it is pressed.
  const [explained, setExplained] = useState(false);
  const p = project ? projects?.find((x) => x.name === project) : undefined;
  const where = project === null ? null : project === "" ? (workspace || words.projectless) : p ? titleOf(p) : project===personalProject?defaultCopy.projects.personalTitle:project;
  const path = p ? above(p) : [];
  const label = where === null ? null : channel && channel !== "general" ? `${where} / #${channel}` : where;
  if (agents.length === 0 && label === null) return null;
  return (
    <ul aria-label={words.selected} className="flex min-w-0 flex-wrap items-center gap-1">
      {agents.map((a) => (
        <li key={a} className={chip}>
          <AgentMark name={a} size={14} />
          <span className="truncate">{a}</span>
          <button type="button" className={remove} aria-label={words.removeAgent(a)} onClick={() => onChange({ ...selection, agents: agents.filter((x) => x !== a) })}>
            <XIcon size={12} />
          </button>
        </li>
      ))}
      {label !== null && (
        <li className={projectControl ? "min-w-0 max-w-full" : chip} title={path.length > 0 ? [...path, where].join(" › ") : undefined}>
          {projectControl ?? <>
          <ProjectIcon size={13} className="shrink-0 text-aui-muted" />
          <span className="truncate">{label}</span>
          {!projectFixed && (
            <button type="button" className={remove} aria-label={words.removeProject(label)} onClick={() => onChange({ ...selection, project: null, channel: "general" })}>
              <XIcon size={12} />
            </button>
          )}
          </>}
        </li>
      )}
      {agents.length >= 2 && (
        <li className="inline-flex flex-wrap items-center" role="group" aria-label={words.routing}>
          {(["join", "all"] as const).map((m) => {
            // Past three agents, All of Them is not offered, and says why when pressed.
            const off = m === "all" && agents.length > MAX_ALL;
            return (
              <button
                key={m}
                type="button"
                aria-pressed={mode === m}
                aria-disabled={off || undefined}
                aria-describedby={off ? why : undefined}
                title={m === "join" ? words.joinHint : off ? words.allMax(MAX_ALL) : words.allHint}
                onClick={() => (off ? setExplained(true) : onChange({ ...selection, mode: m }))}
                className="min-h-6 rounded-aui px-1.5 py-1 text-aui-xs text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:text-aui-fg aria-pressed:underline aria-disabled:opacity-50"
              >
                {m === "join" ? words.join : words.all}
              </button>
            );
          })}
          {agents.length > MAX_ALL && (
            <span id={why} role="status" className={explained ? "px-1.5 text-aui-xs text-aui-muted" : "sr-only"}>
              {words.allMax(MAX_ALL)}
            </span>
          )}
        </li>
      )}
    </ul>
  );
}
