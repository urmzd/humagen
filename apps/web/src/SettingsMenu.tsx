import { defaultCopy } from "@humagen/components";
import { CheckIcon, SettingsIcon } from "@humagen/render-web";
import {
  type KeyboardEvent,
  type ReactNode,
  type RefObject,
  useLayoutEffect,
  useState,
} from "react";
import { AUTO } from "./availability.ts";
import { FindHelp } from "./FindHelp.tsx";
import { PaneMenu, type PaneMenuRow, paneMenuRow } from "./PaneMenu.tsx";
import { ProjectChoices, projectLabel } from "./ProjectPicker.tsx";
import { href } from "./route.ts";
import { agentsValue, type SessionSettingsProps } from "./SessionPickers.tsx";
import type { SessionImage } from "./sessionOptions.ts";
import { AgentMark, SearchField } from "./ui.tsx";

/*
 * The menu of a new session's settings (SessionPickers.tsx draws its control
 * beside +): a row for each setting (Project, Agents, Repositories, Image)
 * with its value, each opening its own picker in the same menu. Every row is
 * always there: one that cannot change says why. Read as soon as the control
 * is drawn (pages.ts), so the menu opens with nothing to wait for.
 */

const words = defaultCopy.sessionSetup;
const setup = defaultCopy.chat.setup;

/** A choice in a picker: its label, a line of detail, and whether it is picked. */
export interface SetupChoice {
  id: string;
  label: string;
  hint?: string;
  /** A mark ahead of the label, such as an agent's. */
  mark?: ReactNode;
  checked: boolean;
  /** It is listed but cannot be picked now. */
  disabled?: boolean;
}

const quiet = "px-2.5 py-3 text-aui-xs text-aui-muted";
const link =
  "inline-flex min-h-9 items-center rounded-aui text-aui-sm text-aui-fg underline underline-offset-4 focus-visible:outline-2 focus-visible:outline-aui-accent";

/** The label with what was searched for marked in it. */
function Highlight({ text, query }: { text: string; query: string }) {
  const at = query ? text.toLowerCase().indexOf(query) : -1;
  if (at < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, at)}
      <mark className="rounded-sm bg-aui-accent-soft text-aui-accent">
        {text.slice(at, at + query.length)}
      </mark>
      {text.slice(at + query.length)}
    </>
  );
}

/**
 * One setting's picker, inside the settings menu: a search field that has
 * focus when it opens (for a list that can be long), then the choices, with
 * what was searched for marked. Enter in the field picks the first match.
 * The menu's own keys move between them and step back.
 */
export function SetupChoices({
  name,
  choices,
  multi = false,
  search,
  loading,
  empty,
  note,
  onPick,
}: {
  name: string;
  /** Null while they load. */
  choices: SetupChoice[] | null;
  multi?: boolean;
  search?: { label: string; none: string };
  loading?: string;
  /** What the picker says when there is nothing to choose. */
  empty?: ReactNode;
  /** A line over the choices, such as who answers with no mention. */
  note?: ReactNode;
  onPick: (id: string) => void;
}) {
  const [query, setQuery] = useState("");
  const q = query.trim().toLowerCase();
  const shown = (choices ?? []).filter(
    (c) => !q || c.label.toLowerCase().includes(q) || (c.hint ?? "").toLowerCase().includes(q),
  );
  const first = shown.find((c) => !c.disabled);
  return (
    <div className="flex min-w-0 flex-col" data-setup-picker={name}>
      {search && choices !== null && choices.length > 0 && (
        // biome-ignore lint/a11y/noNoninteractiveElementInteractions: Enter in the field inside picks
        <div
          className="p-1"
          onKeyDown={(e: KeyboardEvent<HTMLDivElement>) => {
            // Enter picks the first match and never sends the message the menu sits in.
            if (e.key !== "Enter" || e.nativeEvent.isComposing) return;
            e.preventDefault();
            if (first) onPick(first.id);
          }}
        >
          <SearchField label={search.label} value={query} onChange={setQuery} />
        </div>
      )}
      {note && <p className="px-2.5 pt-1 pb-1.5 text-aui-xs text-aui-muted">{note}</p>}
      {choices === null ? (
        <p role="status" className={quiet}>
          {loading}
        </p>
      ) : choices.length === 0 ? (
        <div className={quiet}>{empty}</div>
      ) : shown.length === 0 ? (
        <p role="status" className={quiet}>
          {search?.none}
        </p>
      ) : (
        <div role={multi ? "group" : "radiogroup"} aria-label={name} className="flex flex-col">
          {shown.map((c) => (
            <button
              key={c.id}
              type="button"
              role={multi ? "checkbox" : "radio"}
              aria-checked={c.checked}
              aria-disabled={c.disabled || undefined}
              data-pane-item="choice"
              data-setup-choice={c.id}
              className={`${paneMenuRow} aria-disabled:cursor-not-allowed aria-disabled:opacity-60 aria-disabled:hover:bg-transparent`}
              onClick={() => {
                if (!c.disabled) onPick(c.id);
              }}
            >
              {c.mark}
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="truncate">
                  <Highlight text={c.label} query={q} />
                </span>
                {c.hint && <span className="truncate text-aui-xs text-aui-muted">{c.hint}</span>}
              </span>
              <CheckIcon
                size={16}
                aria-hidden
                className={`shrink-0 text-aui-fg ${c.checked ? "" : "invisible"}`}
              />
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

/** An image's name as a person reads it. */
export function imageName(img: SessionImage): string {
  return img.default ? words.defaultImage : img.name;
}

/** What an image offers now, in a few words. */
export function imageHint(img: SessionImage): string {
  if (img.default) return words.defaultHint(img.ref);
  if (img.status === "checking") return words.checking;
  if (img.status === "failed")
    return img.problem ? `${words.failed}: ${img.problem}` : words.failed;
  return `${img.ref} · ${img.warm ? words.warm : words.cold}`;
}

export function SettingsMenu({
  project,
  agents,
  options,
  setup: chosen,
  onSetup,
  fixed,
  connectHref,
  maxRepos = 10,
  panel,
  onClose,
}: SessionSettingsProps & {
  panel: RefObject<HTMLDivElement | null>;
  onClose: () => void;
}) {
  const c = defaultCopy.chat;
  // The menu opens on the side of the composer with more room.
  const [place, setPlace] = useState({ up: false, max: 448 });
  useLayoutEffect(() => {
    const bar = panel.current?.closest("form") ?? panel.current?.parentElement;
    if (!bar) return;
    const rect = bar.getBoundingClientRect();
    const above = rect.top - 8;
    const below = window.innerHeight - rect.bottom - 8;
    const up = above > below;
    setPlace({ up, max: Math.max(200, Math.min(448, up ? above : below)) });
  }, [panel]);

  const images = options?.images ?? [];
  const image = images.find((i) => i.id === chosen.image) ?? images.find((i) => i.default);
  const repos = options?.repos ?? null;
  // A setting that cannot change keeps its row, which says why.
  const reposFixed = fixed ?? (options === null ? setup.unreadable : undefined);
  const imageFixed =
    fixed ??
    (options === null
      ? setup.unreadable
      : options && !options.isolated
        ? setup.notSandboxed
        : undefined);

  const rows: PaneMenuRow[] = [
    {
      id: "project",
      label: c.project,
      value: projectLabel(
        project.value,
        project.projects,
        project.personalProject,
        project.channel,
      ),
      pane: {
        title: c.project,
        ownKeys: true,
        body: (
          <ProjectChoices
            projects={project.projects}
            value={project.value}
            personalProject={project.personalProject}
            onChange={project.onChange}
            onCreated={project.onCreated}
            close={onClose}
          />
        ),
      },
    },
    {
      id: "agents",
      label: c.agents,
      value: agentsValue(agents.mentioned, agents.defaultAgent),
      pane: {
        title: c.agents,
        body: (
          <SetupChoices
            name={c.agents}
            multi
            search={{ label: c.findAgent, none: c.noAgentMatch }}
            loading={c.agentsLoading}
            empty={c.noAgents}
            note={
              agents.mentioned.length === 0
                ? agents.defaultAgent && agents.defaultAgent !== AUTO
                  ? setup.defaultAnswers(agents.defaultAgent)
                  : setup.bestAnswers
                : undefined
            }
            choices={
              agents.available?.map((a) => ({
                id: a.name,
                label: a.name,
                hint: a.description,
                mark: <AgentMark name={a.name} size={16} />,
                checked: agents.mentioned.includes(a.name),
              })) ?? null
            }
            onPick={agents.onMention}
          />
        ),
        rows: agents.finder
          ? [
              {
                id: "finder",
                label: defaultCopy.home.findHelp.title,
                pane: {
                  ownKeys: true,
                  body: (
                    <FindHelp
                      initialDescription={agents.finder.draft()}
                      onSearch={agents.finder.onSearch}
                      onChoose={(match, description) => {
                        agents.finder?.onChoose(match, description);
                        onClose();
                      }}
                    />
                  ),
                },
              },
            ]
          : undefined,
      },
    },
    {
      id: "repos",
      label: c.picker.repos,
      value: chosen.repos.length === 0 ? setup.noRepos : setup.repos(chosen.repos.length),
      ...(reposFixed
        ? { note: reposFixed, chevron: false }
        : {
            pane: {
              title: c.picker.repos,
              body: (
                <SetupChoices
                  name={c.picker.repos}
                  multi
                  search={{ label: setup.findRepo, none: setup.noRepoMatch }}
                  loading={c.picker.reposLoading}
                  note={repos && repos.length > maxRepos ? setup.reposMax(maxRepos) : undefined}
                  empty={
                    <div className="flex flex-col items-start gap-2">
                      <p>{c.picker.noRepos}</p>
                      {connectHref && (
                        <a href={connectHref} data-pane-item="action" className={link}>
                          {c.picker.connectGitHub}
                        </a>
                      )}
                    </div>
                  }
                  choices={
                    repos === null
                      ? null
                      : repos.map((r) => {
                          const on = chosen.repos.includes(r);
                          return {
                            id: r,
                            label: r,
                            checked: on,
                            disabled: !on && chosen.repos.length >= maxRepos,
                          };
                        })
                  }
                  onPick={(r) =>
                    onSetup({
                      ...chosen,
                      repos: chosen.repos.includes(r)
                        ? chosen.repos.filter((x) => x !== r)
                        : [...chosen.repos, r],
                    })
                  }
                />
              ),
            },
          }),
    },
    {
      id: "image",
      label: words.image,
      value: !image || image.default ? words.defaultImage : image.name,
      ...(imageFixed
        ? { note: imageFixed, chevron: false }
        : {
            pane: {
              title: words.image,
              body: (
                <>
                  <SetupChoices
                    name={words.image}
                    search={
                      images.length > 7
                        ? { label: setup.findImage, none: setup.noImageMatch }
                        : undefined
                    }
                    loading={setup.imagesLoading}
                    choices={
                      options === undefined
                        ? null
                        : images.map((img) => ({
                            id: img.id,
                            label: imageName(img),
                            hint: imageHint(img),
                            checked: img.id === (image?.id ?? "default"),
                            disabled: img.status !== "ready",
                          }))
                    }
                    onPick={(id) => {
                      onSetup({ ...chosen, image: id });
                      onClose();
                    }}
                  />
                  {(options?.imageRegistries ?? []).length > 0 && (
                    <a
                      href={href({ page: "settings", section: "images" })}
                      data-pane-item="action"
                      className={`${paneMenuRow} mt-1 border-aui-border border-t text-aui-muted`}
                    >
                      <SettingsIcon size={16} className="shrink-0" />
                      {words.manage}
                    </a>
                  )}
                </>
              ),
            },
          }),
    },
  ];

  return (
    <PaneMenu
      ref={panel}
      label={setup.label}
      rows={rows}
      onClose={onClose}
      data-layer="menu"
      data-session-settings-menu
      style={{ maxHeight: place.max }}
      className={`motion-safe:animate-aui-pop humagen-glass-pop absolute left-0 z-(--aui-z-popover) flex w-full max-w-sm min-w-0 flex-col overflow-x-hidden overflow-y-auto rounded-aui-lg p-1.5 ${place.up ? "bottom-full mb-1 origin-bottom-left" : "top-full mt-1 origin-top-left"}`}
    />
  );
}
