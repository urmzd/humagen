import { defaultCopy } from "@warp/components";
import { CheckIcon, FileIcon, FolderIcon, MessageIcon, XIcon } from "@warp/render-web";
import { type KeyboardEvent, useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { coworking, type ProjectKindSpec, type ProjectView } from "./api.ts";
import { ProjectChoices } from "./ProjectPicker.tsx";
import { above, titleOf } from "./projects.ts";
import { AgentMark, SearchField } from "./ui.tsx";

const words = defaultCopy.chat;

/** How a message no one is tagged in goes to the agents selected: the best fit of them, or every one. */
export type Mode = "best_fit" | "all";

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
 * MAX_SELECTED, and All of Them for MAX_ALL at most, past which the best
 * fit answers. Null when the selection is full.
 */
export function withAgent(s: Selection, name: string): Selection | null {
  if (s.agents.includes(name)) return s;
  if (s.agents.length >= MAX_SELECTED) return null;
  const agents = [...s.agents, name];
  return { ...s, agents, mode: s.mode === "all" && agents.length > MAX_ALL ? "best_fit" : s.mode };
}

export const emptySelection: Selection = { agents: [], mode: "best_fit", project: null, channel: "general" };

/** The chat bar's prompt for a selection: how to bring an agent in, or who answers. */
export function placeholderFor(s: Pick<Selection, "agents" | "mode">): string {
  if (s.agents.length === 0) return words.placeholder;
  if (s.agents.length === 1) return words.placeholderOne(s.agents[0] ?? "");
  return s.mode === "all" ? words.placeholderAll(s.agents.length) : words.placeholderBest(s.agents.length);
}

/** Every item of the menu that the arrow keys move between, in order. */
const ITEM = "[data-plus-item]:not(:disabled)";

const row =
  "flex min-h-10 w-full items-center gap-2.5 rounded-aui px-2.5 py-1.5 text-left text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent aria-checked:bg-aui-hover";

/**
 * The + menu: attach files, check the agents an untagged message goes to,
 * and pick where the conversation lives (a project, then one of its
 * channels, general first). The arrow keys move between items, Space
 * checks, Enter or Escape closes. Still: it waits on a person.
 */
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
}: {
  /** The agents that may answer where the conversation lives. Null while they load. */
  agents: { name: string; description?: string }[] | null;
  selection: Selection;
  onChange: (next: Selection) => void;
  onAttach: () => void;
  onClose: () => void;
  /** Closes it as focus moves elsewhere, leaving focus where it went. */
  onLeave?: () => void;
  /** The project tree, or null while it loads. Left out, the menu offers no project. */
  projects?: ProjectView[] | null;
  kinds?: ProjectKindSpec[];
  canCreateProject?: boolean;
  onProjectCreated?: (p: ProjectView) => void;
  /** Why the project can't change here, such as an agent having run in it. */
  projectFixed?: string;
}) {
  const [query, setQuery] = useState("");
  const [choosing, setChoosing] = useState(false);
  // Back from choosing a project, focus returns to the project's button, so
  // the arrow keys and Escape keep working in the menu.
  const projectButton = useRef<HTMLButtonElement>(null);
  const chose = useRef(false);
  useEffect(() => {
    if (choosing) chose.current = true;
    else if (chose.current) {
      chose.current = false;
      (projectButton.current?.disabled ? search.current : projectButton.current)?.focus();
    }
  }, [choosing]);
  const [channels, setChannels] = useState<string[] | null>(null);
  const panel = useRef<HTMLDivElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const offersProject = projects !== undefined;
  const project = selection.project;

  // It opens toward the side of the chat bar with more room, and no taller
  // than that room, so on a phone it never runs off the screen.
  const [place, setPlace] = useState<{ up: boolean; max: number }>({ up: true, max: 448 });
  useLayoutEffect(() => {
    const bar = panel.current?.parentElement?.closest("form") ?? panel.current?.parentElement;
    if (!bar) return;
    const r = bar.getBoundingClientRect();
    const above = r.top - 8;
    const below = window.innerHeight - r.bottom - 8;
    const up = above >= below;
    setPlace({ up, max: Math.max(160, Math.min(448, up ? above : below)) });
  }, []);

  useEffect(() => {
    search.current?.focus();
  }, []);

  // The channels of the project picked, general first.
  useEffect(() => {
    if (!offersProject || project === null) return setChannels(null);
    let live = true;
    coworking
      .channels(project)
      .then((cs) => {
        if (!live) return;
        const names = cs.filter((c) => !c.archived).map((c) => c.name);
        setChannels(["general", ...names.filter((n) => n !== "general")]);
      })
      .catch(() => live && setChannels(["general"]));
    return () => {
      live = false;
    };
  }, [offersProject, project]);

  const full = selection.agents.length >= MAX_SELECTED;
  const toggle = (name: string) => {
    if (selection.agents.includes(name)) return onChange({ ...selection, agents: selection.agents.filter((a) => a !== name) });
    const next = withAgent(selection, name);
    if (next) onChange(next);
  };

  const q = query.trim().toLowerCase();
  const shown = (agents ?? []).filter((a) => !q || a.name.toLowerCase().includes(q) || (a.description ?? "").toLowerCase().includes(q));

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.nativeEvent.isComposing) return;
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      if (choosing) return setChoosing(false);
      return onClose();
    }
    if (choosing) return;
    const items = [...(panel.current?.querySelectorAll<HTMLElement>(ITEM) ?? [])];
    const at = items.indexOf(document.activeElement as HTMLElement);
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      // Down from the search goes to the agents it narrows.
      if (e.key === "ArrowDown" && document.activeElement === search.current) {
        (panel.current?.querySelector<HTMLElement>('[data-plus-item="agent"]:not(:disabled)') ?? items[0])?.focus();
        return;
      }
      const next = e.key === "ArrowDown" ? (at + 1) % items.length : (at <= 0 ? items.length : at) - 1;
      items[next]?.focus();
      return;
    }
    // Enter closes the menu, from an agent's row as from the search; Space
    // checks. Any other button, such as the search's Clear, does its own job.
    const target = e.target as HTMLElement;
    if (e.key === "Enter" && target.dataset.plusItem !== "action" && (target.tagName !== "BUTTON" || target.dataset.plusItem === "agent")) {
      e.preventDefault();
      onClose();
    }
  };

  const chosen = project ? projects?.find((p) => p.name === project) : undefined;

  return (
    // biome-ignore lint/a11y/noStaticElementInteractions: the panel moves focus between its items with the arrow keys
    <div
      ref={panel}
      role="dialog"
      data-layer="menu"
      aria-label={words.menu}
      onKeyDown={onKeyDown}
      // Focus leaving the menu, by Tab or otherwise, closes it.
      onBlur={(e) => {
        const to = e.relatedTarget as HTMLElement | null;
        // The + button closes the menu itself, on its click.
        if (to && !panel.current?.contains(to) && !to.closest("[data-plus-button]")) (onLeave ?? onClose)();
      }}
      style={{ maxHeight: place.max }}
      className={`absolute left-0 z-30 flex w-full max-w-sm flex-col overflow-y-auto rounded-aui border border-aui-border bg-aui-surface p-1.5 shadow-lg ${place.up ? "bottom-full mb-1" : "top-full mt-1"}`}
    >
      {choosing && projects !== undefined ? (
        <ProjectChoices
          projects={projects}
          kinds={kinds}
          value={project}
          canCreate={canCreateProject}
          onChange={(name) => onChange({ ...selection, project: name, channel: "general" })}
          onCreated={(p) => onProjectCreated?.(p)}
          close={() => setChoosing(false)}
        />
      ) : (
        <>
          <button type="button" data-plus-item="action" className={row} onClick={onAttach}>
            <FileIcon size={16} className="text-aui-muted" />
            {words.attach}
          </button>
          <div className="my-1 h-px bg-aui-border" />
          <p className="px-2.5 pt-1 text-aui-xs font-medium text-aui-muted">{words.agents}</p>
          <div className="p-1">
            <SearchField label={words.findAgent} value={query} onChange={setQuery} inputRef={search} />
          </div>
          {agents === null ? (
            <p role="status" className="px-2.5 py-2 text-aui-xs text-aui-muted">
              {words.agentsLoading}
            </p>
          ) : shown.length === 0 ? (
            <p role="status" className="px-2.5 py-2 text-aui-xs text-aui-muted">
              {agents.length === 0 ? words.noAgents : words.noAgentMatch}
            </p>
          ) : (
            <div role="group" aria-label={words.agents} className="flex flex-col">
              {shown.map((a) => {
                const on = selection.agents.includes(a.name);
                return (
                  <button
                    key={a.name}
                    type="button"
                    role="checkbox"
                    aria-checked={on}
                    aria-disabled={!on && full ? true : undefined}
                    data-plus-item="agent"
                    className={row}
                    onClick={() => toggle(a.name)}
                  >
                    <AgentMark name={a.name} size={16} />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate">{a.name}</span>
                      {a.description && <span className="block truncate text-aui-xs text-aui-muted">{a.description}</span>}
                    </span>
                    <span aria-hidden className="inline-flex size-4 shrink-0 items-center justify-center">
                      {on && <CheckIcon size={14} />}
                    </span>
                  </button>
                );
              })}
              {full && <p className="px-2.5 py-1 text-aui-xs text-aui-muted">{words.maxAgents(MAX_SELECTED)}</p>}
            </div>
          )}
          {offersProject && (
            <>
              <div className="my-1 h-px bg-aui-border" />
              <p className="px-2.5 pt-1 text-aui-xs font-medium text-aui-muted">{words.project}</p>
              <button
                type="button"
                ref={projectButton}
                data-plus-item="action"
                className={row}
                disabled={!!projectFixed}
                title={projectFixed}
                onClick={() => setChoosing(true)}
              >
                {project === "" ? <MessageIcon size={16} className="text-aui-muted" /> : <FolderIcon size={16} className="text-aui-muted" />}
                <span className="min-w-0 flex-1">
                  <span className="block truncate">{project === null ? words.chooseProject : project === "" ? words.projectless : chosen ? titleOf(chosen) : project}</span>
                  {chosen && above(chosen).length > 0 && <span className="block truncate text-aui-xs text-aui-muted">{above(chosen).join(" › ")}</span>}
                </span>
              </button>
              {projectFixed && <p className="px-2.5 pb-1 text-aui-xs text-aui-muted">{projectFixed}</p>}
              {project !== null && channels && channels.length > 0 && (
                <div role="radiogroup" aria-label={words.channel} className="flex flex-wrap gap-1 px-2.5 py-1">
                  {channels.map((c) => (
                    <button
                      key={c}
                      type="button"
                      role="radio"
                      aria-checked={selection.channel === c}
                      data-plus-item="channel"
                      disabled={!!projectFixed}
                      onClick={() => onChange({ ...selection, channel: c })}
                      className="rounded-aui px-1.5 py-0.5 text-aui-sm text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-checked:text-aui-fg aria-checked:underline disabled:opacity-50"
                    >
                      #{c}
                    </button>
                  ))}
                </div>
              )}
            </>
          )}
        </>
      )}
    </div>
  );
}

const chip = "inline-flex h-7 max-w-full items-center gap-1.5 rounded-full border border-aui-border pr-1 pl-2 text-aui-xs text-aui-fg";
const remove =
  "inline-flex size-6 shrink-0 items-center justify-center rounded-full text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent";

/**
 * What the + menu selected, as chips in the chat bar's bottom row: each
 * agent with its color, the project with its path (and channel when not
 * general), each removable. With two agents or more, a text control says
 * how an untagged message is routed: Best Fit or All of Them.
 */
export function SelectionChips({
  selection,
  projects,
  workspace,
  onChange,
  projectFixed,
}: {
  selection: Selection;
  projects?: ProjectView[] | null;
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
  const where = project === null ? null : project === "" ? (workspace || words.projectless) : p ? titleOf(p) : project;
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
        <li className={chip} title={path.length > 0 ? [...path, where].join(" › ") : undefined}>
          <FolderIcon size={13} className="shrink-0 text-aui-muted" />
          <span className="truncate">{label}</span>
          {!projectFixed && (
            <button type="button" className={remove} aria-label={words.removeProject(label)} onClick={() => onChange({ ...selection, project: null, channel: "general" })}>
              <XIcon size={12} />
            </button>
          )}
        </li>
      )}
      {agents.length >= 2 && (
        <li className="inline-flex flex-wrap items-center" role="group" aria-label={words.routing}>
          {(["best_fit", "all"] as const).map((m) => {
            // Past three agents, All of Them is not offered, and says why when pressed.
            const off = m === "all" && agents.length > MAX_ALL;
            return (
              <button
                key={m}
                type="button"
                aria-pressed={mode === m}
                aria-disabled={off || undefined}
                aria-describedby={off ? why : undefined}
                title={m === "best_fit" ? words.bestFitHint : off ? words.allMax(MAX_ALL) : words.allHint}
                onClick={() => (off ? setExplained(true) : onChange({ ...selection, mode: m }))}
                className="min-h-6 rounded-aui px-1.5 py-1 text-aui-xs text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:text-aui-fg aria-pressed:underline aria-disabled:opacity-50"
              >
                {m === "best_fit" ? words.bestFit : words.all}
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
