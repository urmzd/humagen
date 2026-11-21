import { defaultCopy } from "@humagen/components";
import { BotIcon, CheckIcon, FileIcon, ProjectIcon, WrenchIcon, XIcon } from "@humagen/render-web";
import { type KeyboardEvent, type ReactNode, useLayoutEffect, useRef, useState } from "react";
import type { ProjectKindSpec, ProjectView } from "./api.ts";
import { PaneMenu, type PaneMenuRow, paneMenuRow as row } from "./PaneMenu.tsx";
import { above, titleOf } from "./projects.ts";
import { AgentMark, SearchField, SearchIcon } from "./ui.tsx";

const words = defaultCopy.chat;

/**
 * Where a conversation the + menu sets up lives: `project` null is not
 * chosen yet, "" is the workspace. Its agents are not kept here: they are
 * the agents its draft mentions (`mentionsIn`), so the words are the one
 * place they are kept.
 */
import type { Selection } from "./plusSelection.ts";

export { emptySelection, mentionPicks, type Selection } from "./plusSelection.ts";

type Choice = { name: string; description?: string };

/** The composer groups choices by intent: files, the agents a message goes to, and skills. */
export function PlusMenu({
  agents,
  mentioned = [],
  onMention,
  onAttach,
  onClose,
  onLeave,
  projectFixed,
  finder,
  skills,
  skillsFailed = false,
  onSkill,
}: {
  /** The agents that may answer where the conversation lives. Null while they load. */
  agents: Choice[] | null;
  /** The agents the draft mentions, each shown as picked. */
  mentioned?: string[];
  /** Mentions an agent in the draft, or takes its mention out when it is mentioned. */
  onMention?: (name: string) => void;
  onAttach: () => void;
  onClose: () => void;
  /** Closes it as focus moves elsewhere, leaving focus where it went. */
  onLeave?: () => void;
  finder?: ReactNode;
  /** Why the conversation's project and channel cannot change. */
  projectFixed?: string;
  /** Resolved skills for this project, not the skill-management library. */
  skills?: Choice[] | null;
  skillsFailed?: boolean;
  /** Prepares the message through the host's existing skill-tag insertion. */
  onSkill?: (name: string) => void;
  /*
   * Where the conversation lives and what a project picker needed. The menu
   * offers no project (the persistent folder does), so it reads none of
   * these; its hosts still pass them.
   */
  selection: Selection;
  onChange: (next: Selection) => void;
  projects?: ProjectView[] | null;
  personalProject?: string;
  kinds?: ProjectKindSpec[];
  canCreateProject?: boolean;
  onProjectCreated?: (p: ProjectView) => void;
}) {
  const c = words.picker;
  const [agentQuery, setAgentQuery] = useState("");
  const [skillQuery, setSkillQuery] = useState("");
  const panel = useRef<HTMLDivElement>(null);
  const search = useRef<HTMLInputElement>(null);

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

  const matches = (choice: Choice, query: string) =>
    !query ||
    choice.name.toLowerCase().includes(query) ||
    (choice.description ?? "").toLowerCase().includes(query);
  const shownAgents = (agents ?? []).filter((agent) =>
    matches(agent, agentQuery.trim().toLowerCase()),
  );
  const shownSkills = (skills ?? []).filter((skill) =>
    matches(skill, skillQuery.trim().toLowerCase()),
  );

  // Enter picks, and in a search field never sends the message the menu sits in.
  const onKey = (event: KeyboardEvent<HTMLDivElement>, pane: string | null) => {
    if (event.key !== "Enter") return;
    const target = event.target as HTMLElement;
    const choice = target.dataset.paneItem === "choice";
    if (pane === "skills" && choice) {
      event.preventDefault();
      target.click();
    } else if (pane === "agents" && (target === search.current || choice)) {
      event.preventDefault();
      if (choice) target.click();
      else if (shownAgents.length === 1) onMention?.(shownAgents[0]!.name);
      onClose();
    } else if (target instanceof HTMLInputElement) event.preventDefault();
  };

  const rows: PaneMenuRow[] = [
    {
      id: "attach",
      label: words.attach,
      icon: <FileIcon size={16} className="text-aui-muted" />,
      onSelect: onAttach,
    },
    {
      id: "agents",
      label: words.agents,
      icon: <BotIcon size={16} className="text-aui-muted" />,
      value: mentioned.length > 0 ? c.selected(mentioned.length) : undefined,
      note: projectFixed,
      pane: {
        title: words.agents,
        body: (
          <>
            <div className="p-1">
              <SearchField
                label={words.findAgent}
                value={agentQuery}
                onChange={setAgentQuery}
                inputRef={search}
              />
            </div>
            {agents === null ? (
              <p role="status" className="px-2.5 py-3 text-aui-xs text-aui-muted">
                {words.agentsLoading}
              </p>
            ) : shownAgents.length === 0 ? (
              <p role="status" className="px-2.5 py-3 text-aui-xs text-aui-muted">
                {agents.length === 0 ? words.noAgents : words.noAgentMatch}
              </p>
            ) : (
              <div role="group" aria-label={words.agents} className="flex flex-col">
                {shownAgents.map((agent) => {
                  const checked = mentioned.includes(agent.name);
                  return (
                    <button
                      key={agent.name}
                      type="button"
                      role="checkbox"
                      aria-checked={checked}
                      data-pane-item="choice"
                      className={row}
                      onClick={() => onMention?.(agent.name)}
                    >
                      <AgentMark name={agent.name} size={16} />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate">{agent.name}</span>
                        {agent.description && (
                          <span className="block truncate text-aui-xs text-aui-muted">
                            {agent.description}
                          </span>
                        )}
                      </span>
                      <span
                        aria-hidden
                        className="inline-flex size-4 shrink-0 items-center justify-center"
                      >
                        {checked && <CheckIcon size={14} />}
                      </span>
                    </button>
                  );
                })}
              </div>
            )}
          </>
        ),
        rows:
          finder && !projectFixed
            ? [
                {
                  id: "finder",
                  label: defaultCopy.home.findHelp.title,
                  icon: <SearchIcon size={16} className="text-aui-muted" />,
                  pane: { ownKeys: true, body: <div data-plus-discovery>{finder}</div> },
                },
              ]
            : undefined,
      },
    },
  ];
  if (onSkill)
    rows.push({
      id: "skills",
      label: defaultCopy.navigation.skills,
      icon: <WrenchIcon size={16} className="text-aui-muted" />,
      pane: {
        title: defaultCopy.navigation.skills,
        body: (
          <>
            <div className="p-1">
              <SearchField
                label={c.findSkill}
                value={skillQuery}
                onChange={setSkillQuery}
                inputRef={search}
              />
            </div>
            {skillsFailed ? (
              <p role="alert" className="px-2.5 py-3 text-aui-xs text-aui-muted">
                {c.skillsFailed}
              </p>
            ) : skills == null ? (
              <p role="status" className="px-2.5 py-3 text-aui-xs text-aui-muted">
                {c.skillsLoading}
              </p>
            ) : shownSkills.length === 0 ? (
              <p role="status" className="px-2.5 py-3 text-aui-xs text-aui-muted">
                {skills.length === 0 ? c.noSkills : c.noSkillMatch}
              </p>
            ) : (
              <div
                role="group"
                aria-label={defaultCopy.navigation.skills}
                className="flex flex-col"
              >
                {shownSkills.map((skill) => (
                  <button
                    key={skill.name}
                    type="button"
                    data-pane-item="choice"
                    className={row}
                    onClick={() => {
                      onSkill(skill.name);
                      onClose();
                    }}
                  >
                    <WrenchIcon size={16} className="text-aui-muted" />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate">{skill.name}</span>
                      {skill.description && (
                        <span className="block truncate text-aui-xs text-aui-muted">
                          {skill.description}
                        </span>
                      )}
                    </span>
                  </button>
                ))}
              </div>
            )}
          </>
        ),
      },
    });

  return (
    <PaneMenu
      ref={panel}
      label={words.menu}
      rows={rows}
      onClose={onClose}
      onKey={onKey}
      data-layer="menu"
      data-plus-menu
      onBlur={(event) => {
        const to = event.relatedTarget as HTMLElement | null;
        if (to && !event.currentTarget.contains(to) && !to.closest("[data-plus-button]"))
          (onLeave ?? onClose)();
      }}
      style={{ maxHeight: place.max }}
      className={`motion-safe:animate-aui-pop absolute left-0 z-(--aui-z-popover) flex w-full min-w-0 max-w-sm flex-col overflow-x-hidden overflow-y-auto rounded-aui border border-aui-border bg-aui-surface p-1.5 shadow-lg ${place.up ? "bottom-full mb-1 origin-bottom-left" : "top-full mt-1 origin-top-left"}`}
    />
  );
}

const chip =
  "inline-flex min-h-7 max-w-full min-w-0 items-center gap-1.5 pr-1 text-aui-xs text-aui-muted";
const remove =
  "inline-flex size-6 shrink-0 items-center justify-center rounded-aui-sm text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent";

/**
 * Where the conversation is filed, as a text control in the composer's bar:
 * the project with its path (and channel when not general), or the control
 * a host passes to change it. The agents a message goes to are its
 * mentions, in its words, never here.
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
  personalProject?: string;
  workspace?: string;
  onChange: (next: Selection) => void;
  /** The project can't be removed, as when an agent has run in the conversation. */
  projectFixed?: boolean;
}) {
  const { project, channel } = selection;
  const p = project ? projects?.find((x) => x.name === project) : undefined;
  const where =
    project === null
      ? null
      : project === ""
        ? workspace || words.projectless
        : p
          ? titleOf(p)
          : project === personalProject
            ? defaultCopy.projects.personalTitle
            : project;
  const path = p ? above(p) : [];
  const label =
    where === null ? null : channel && channel !== "general" ? `${where} / #${channel}` : where;
  if (label === null && !projectControl) return null;
  return (
    <ul aria-label={words.selected} className="flex min-w-0 flex-wrap items-center gap-1">
      <li
        className={`humagen-chip-project ${projectControl ? "humagen-chip-picker min-w-0 max-w-full" : chip}`}
        title={path.length > 0 ? [...path, where].join(" › ") : undefined}
      >
        {projectControl ?? (
          <>
            <ProjectIcon size={13} className="shrink-0 text-aui-muted" />
            <span className="truncate">{label}</span>
            {!projectFixed && (
              <button
                type="button"
                className={remove}
                aria-label={words.removeProject(label ?? "Personal")}
                onClick={() => onChange({ ...selection, project: null, channel: "general" })}
              >
                <XIcon size={12} />
              </button>
            )}
          </>
        )}
      </li>
    </ul>
  );
}
