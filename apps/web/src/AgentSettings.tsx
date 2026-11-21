import { defaultCopy } from "@humagen/components";
import { ChevronRightIcon, PencilIcon, SlackIcon } from "@humagen/render-web";
import { useEffect, useId, useRef, useState, useSyncExternalStore } from "react";
import { trimLook } from "./AgentLookEditor.tsx";
import { agentLook, setAgentLooks, subscribeHues } from "./agentHues.ts";
import { type AgentControl, type AgentLook, ApiError, api, type Revision } from "./api.ts";
import { agentShown } from "./branding.ts";
import { type AgentTab, href } from "./route.ts";
import { AgentMark, button, field, Problem, textAction } from "./ui.tsx";

const words = defaultCopy.agentSettings;

export type SettingsGroup = keyof typeof words.groups;
export type SettingsTab = Exclude<AgentTab, "home">;

/**
 * An agent's settings, grouped by what they decide: who it is (General),
 * who reaches it (Access), what it works with (Tools) and how it behaves
 * (Behavior). Each section keeps the address its tab always had.
 */
export const SETTINGS_GROUPS: readonly { id: SettingsGroup; tabs: readonly SettingsTab[] }[] = [
  { id: "general", tabs: ["general", "look", "prompt"] },
  { id: "access", tabs: ["access", "slack", "queue"] },
  { id: "tools", tabs: ["context", "model"] },
  { id: "behavior", tabs: ["memory", "evals", "alerts", "revisions"] },
];

/** The group a section is in. */
export const groupOf = (tab: SettingsTab): SettingsGroup =>
  SETTINGS_GROUPS.find((g) => g.tabs.includes(tab))?.id ?? "general";

/**
 * The settings' sections, grouped: a list beside them on a wide screen, and
 * one labelled picker above them on a phone. Only the sections the agent has
 * are offered (a new agent has no memory yet), and a count says what waits.
 */
export function AgentSettingsNav({
  name,
  tab,
  onTab,
  has,
  counts = {},
}: {
  /** The agent's handle, for each section's address. */
  name: string;
  tab: SettingsTab;
  onTab: (next: SettingsTab) => void;
  has: (tab: SettingsTab) => boolean;
  counts?: Partial<Record<SettingsTab, number>>;
}) {
  const id = useId();
  const groups = SETTINGS_GROUPS.map((g) => ({ ...g, tabs: g.tabs.filter(has) })).filter(
    (g) => g.tabs.length > 0,
  );
  return (
    <>
      <div className="flex flex-col gap-1 md:hidden">
        <label htmlFor={id} className="text-aui-xs font-medium text-aui-muted">
          {words.section}
        </label>
        <select
          id={id}
          className={field}
          value={tab}
          onChange={(e) => onTab(e.target.value as SettingsTab)}
        >
          {groups.map((g) => (
            <optgroup key={g.id} label={words.groups[g.id]}>
              {g.tabs.map((t) => (
                <option key={t} value={t}>
                  {words.sections[t]}
                  {counts[t] ? ` (${counts[t]})` : ""}
                </option>
              ))}
            </optgroup>
          ))}
        </select>
      </div>
      <nav
        aria-label={words.nav}
        className="sticky top-0 flex flex-col gap-4 self-start max-md:hidden"
      >
        {groups.map((g) => (
          <div key={g.id} className="flex flex-col gap-0.5">
            <h2 className="px-2.5 pb-1 text-aui-2xs font-medium uppercase tracking-wide text-aui-muted">
              {words.groups[g.id]}
            </h2>
            {g.tabs.map((t) => (
              <a
                key={t}
                href={href({ page: "agent", name, tab: t })}
                aria-current={tab === t ? "page" : undefined}
                data-section={t}
                onClick={(e) => {
                  e.preventDefault();
                  onTab(t);
                }}
                className={`flex min-h-8 items-center justify-between gap-2 rounded-aui px-2.5 text-aui-sm transition-colors focus-visible:outline-2 focus-visible:outline-aui-accent ${
                  tab === t
                    ? "bg-aui-hover font-medium text-aui-fg"
                    : "text-aui-muted hover:bg-aui-hover hover:text-aui-fg"
                }`}
              >
                {words.sections[t]}
                {!!counts[t] && (
                  <span className="rounded-full bg-aui-hover px-1.5 text-aui-2xs tabular-nums text-aui-muted">
                    {counts[t]}
                  </span>
                )}
              </a>
            ))}
          </div>
        ))}
      </nav>
    </>
  );
}

const row =
  "flex min-h-14 flex-wrap items-center justify-between gap-x-4 gap-y-2 rounded-aui border border-aui-border px-4 py-3";

/**
 * General: who the agent is and what it has done, how it answers in Slack,
 * its instructions at a glance, and pausing or retiring it. Pausing is
 * undone with Resume; retiring takes it out of the workspace and keeps its
 * sessions and revisions readable.
 */
export function AgentGeneral({
  revision,
  canWrite,
  builtin = false,
  onTab,
  onRetired,
  seed,
}: {
  revision: Revision;
  canWrite: boolean;
  /** One of the platform's own: paused, never retired. */
  builtin?: boolean;
  onTab: (tab: SettingsTab) => void;
  onRetired: () => void;
  /** What to show before it is read, for tests and the gallery. */
  seed?: { completed?: number; control?: AgentControl; slack?: string | null };
}) {
  const name = revision.spec.name;
  const look = useSyncExternalStore(
    subscribeHues,
    () => agentLook(name),
    () => agentLook(name),
  );
  const shown = agentShown(name);
  const [completed, setCompleted] = useState<number | null | undefined>(seed?.completed);
  const [control, setControl] = useState<AgentControl | undefined>(seed?.control);
  const [slack, setSlack] = useState<string | null | undefined>(seed?.slack);
  const [problem, setProblem] = useState("");
  const [busy, setBusy] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [draft, setDraft] = useState("");

  useEffect(() => {
    if (seed) return;
    let live = true;
    api
      .analytics("168h")
      .then((s) => {
        if (live) setCompleted(s.by_agent.find((g) => g.name === name)?.completed ?? 0);
      })
      .catch(() => live && setCompleted(null));
    api
      .agentSlackApp(name)
      .then((a) => live && setSlack(a.client ?? null))
      .catch(() => live && setSlack(null));
    return () => {
      live = false;
    };
  }, [name, seed]);

  // Whether it is paused is read on its own, so a failed read is said, with
  // Retry, beside the Pause it holds back.
  const [controlFailed, setControlFailed] = useState(false);
  const [controlRead, setControlRead] = useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: read again on Retry
  useEffect(() => {
    if (seed) return;
    let live = true;
    setControlFailed(false);
    api
      .agentControl(name)
      .then((c) => live && setControl(c))
      .catch(() => live && setControlFailed(true));
    return () => {
      live = false;
    };
  }, [name, seed, controlRead]);

  // The confirmation replaces the button that opened it, so focus follows:
  // to Keep It, the answer that changes nothing, and back to Retire.
  const keepIt = useRef<HTMLButtonElement>(null);
  const retireIt = useRef<HTMLButtonElement>(null);
  const asked = useRef(false);
  useEffect(() => {
    if (confirming) {
      asked.current = true;
      keepIt.current?.focus();
    } else if (asked.current) {
      asked.current = false;
      retireIt.current?.focus();
    }
  }, [confirming]);

  const paused = !!control?.paused;
  const setPaused = (next: boolean) => {
    if (!control) return;
    setBusy(true);
    setProblem("");
    api
      .saveAgentControl(name, {
        paused: next,
        manager: control.manager,
        stop_rules: control.stop_rules,
        version: control.version,
      })
      .then(setControl)
      .catch((e) => setProblem(e instanceof ApiError ? e.message : words.controlFailed))
      .finally(() => setBusy(false));
  };
  const retire = () => {
    setBusy(true);
    setProblem("");
    api
      .retireAgent(name, revision.rev)
      .then(onRetired)
      .catch((e) => {
        setProblem(e instanceof ApiError ? e.message : words.controlFailed);
        setBusy(false);
      });
  };
  const rename = () => {
    setBusy(true);
    setProblem("");
    const next = trimLook({ ...(look ?? {}), display_name: draft } as AgentLook);
    api
      .setAgentLook(name, next)
      .then((v) => {
        setAgentLooks(Object.keys(v.look).length ? { [name]: v.look } : {}, [name]);
        setRenaming(false);
      })
      .catch((e) =>
        setProblem(
          e instanceof ApiError && e.status === 409
            ? defaultCopy.agentLook.nameTaken
            : e instanceof ApiError
              ? e.message
              : words.renameFailed,
        ),
      )
      .finally(() => setBusy(false));
  };
  const prompt = revision.spec.system_prompt;

  return (
    <div className="flex flex-col gap-4" data-testid="agent-general">
      <section className="flex flex-col items-center gap-3 rounded-aui-lg border border-aui-border bg-aui-hover/60 px-6 py-8 text-center">
        <AgentMark name={name} size={88} />
        {renaming ? (
          <form
            className="flex w-full max-w-sm flex-wrap items-center justify-center gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              rename();
            }}
          >
            <input
              aria-label={words.renameLabel}
              className={`${field} min-w-0 flex-1`}
              value={draft}
              maxLength={40}
              placeholder={name}
              // biome-ignore lint/a11y/noAutofocus: the field opens because the person asked to rename
              autoFocus
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => e.key === "Escape" && setRenaming(false)}
            />
            <button type="submit" className={button} disabled={busy}>
              {words.save}
            </button>
            <button type="button" className={button} onClick={() => setRenaming(false)}>
              {words.cancel}
            </button>
          </form>
        ) : (
          <div className="flex min-w-0 items-center gap-1.5">
            <span
              className="truncate font-semibold text-aui-lg text-aui-fg"
              data-testid="general-name"
            >
              {shown}
            </span>
            {canWrite && (
              <button
                type="button"
                aria-label={words.rename}
                title={words.rename}
                className="inline-flex size-8 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
                onClick={() => {
                  setDraft(look?.display_name ?? "");
                  setRenaming(true);
                }}
              >
                <PencilIcon aria-hidden className="size-4" />
              </button>
            )}
          </div>
        )}
        <span className="text-aui-xs text-aui-muted">@{name}</span>
        {completed === null ? (
          <span className="text-aui-xs text-aui-muted">{words.statsFailed}</span>
        ) : completed === undefined ? (
          <span className="h-4" />
        ) : (
          <a
            className="inline-flex min-h-8 items-center gap-0.5 rounded-aui px-2 text-aui-sm text-aui-muted hover:bg-aui-hover hover:text-aui-fg"
            href={href({ page: "sessions", agent: name })}
            data-testid="general-outcome"
          >
            {completed > 0 ? words.completed(completed) : words.noneCompleted}
            <ChevronRightIcon aria-hidden className="size-4" />
          </a>
        )}
      </section>

      <div className={row}>
        <div className="flex min-w-0 items-center gap-3">
          <SlackIcon aria-hidden className="size-5 shrink-0" />
          <div className="flex min-w-0 flex-col">
            <span className="font-medium text-aui-sm text-aui-fg">{words.slack}</span>
            <span className="text-aui-xs text-aui-muted">
              {slack ? words.slackOwn(slack) : words.slackShared}
            </span>
          </div>
        </div>
        <button type="button" className={button} onClick={() => onTab("slack")}>
          {slack ? words.manage : words.setUp}
          <ChevronRightIcon aria-hidden className="size-4" />
        </button>
      </div>

      <div className={row}>
        <div className="flex min-w-0 flex-col">
          <span className="font-medium text-aui-sm text-aui-fg">{words.instructions}</span>
          <span className="line-clamp-2 text-aui-xs text-aui-muted">
            {revision.spec.description ||
              words.instructionsLine(prompt.split("\n").length, prompt.length)}
          </span>
        </div>
        <button type="button" className={button} onClick={() => onTab("prompt")}>
          {words.edit}
          <ChevronRightIcon aria-hidden className="size-4" />
        </button>
      </div>

      {canWrite ? (
        <section
          aria-label={words.stop}
          className="flex flex-col divide-y divide-aui-border rounded-aui border border-aui-border"
        >
          <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 py-3">
            <div className="flex min-w-0 flex-1 flex-col">
              <span className="font-medium text-aui-sm text-aui-fg">
                {paused ? words.resume : words.pause}
              </span>
              <span className="text-aui-xs text-aui-muted">
                {paused ? words.resumeWhat(shown) : words.pauseWhat(shown)}
              </span>
            </div>
            <button
              type="button"
              className={button}
              disabled={busy || !control}
              onClick={() => setPaused(!paused)}
            >
              {paused ? words.resume : words.pause}
            </button>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 py-3">
            <div className="flex min-w-0 flex-1 flex-col">
              <span className="font-medium text-aui-sm text-aui-fg">
                {confirming ? words.retireConfirm(shown) : words.retire}
              </span>
              <span className="text-aui-xs text-aui-muted">
                {builtin
                  ? words.kept
                  : confirming
                    ? words.retireConfirmWhat
                    : words.retireWhat(shown)}
              </span>
            </div>
            {!builtin &&
              (confirming ? (
                <div className="flex flex-wrap gap-2">
                  <button
                    ref={keepIt}
                    type="button"
                    className={button}
                    onClick={() => setConfirming(false)}
                  >
                    {words.keep}
                  </button>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={retire}
                    className={`${button} border-aui-bad/50 text-aui-bad hover:bg-aui-bad/10`}
                  >
                    {words.retire}
                  </button>
                </div>
              ) : (
                <button
                  ref={retireIt}
                  type="button"
                  className={`${button} text-aui-bad`}
                  onClick={() => setConfirming(true)}
                >
                  {words.retire}
                </button>
              ))}
          </div>
        </section>
      ) : (
        <p className="text-aui-xs text-aui-muted">{words.readOnly}</p>
      )}
      <Problem
        message={canWrite && controlFailed ? words.controlUnread(shown) : null}
        action={
          <button type="button" className={textAction} onClick={() => setControlRead((n) => n + 1)}>
            {defaultCopy.retry}
          </button>
        }
      />
      <Problem message={problem} />
    </div>
  );
}
