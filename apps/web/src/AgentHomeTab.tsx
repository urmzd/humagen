import { defaultCopy } from "@humagen/components";
import { ChevronRightIcon, SparkleIcon } from "@humagen/render-web";
import { useQueue } from "@humagen/web";
import { useEffect, useState, useSyncExternalStore } from "react";
import type { AgentSession } from "../../../packages/client/src/generated/types.ts";
import { agentLook, subscribeHues } from "./agentHues.ts";
import { type AgentControl, api, call, type Group, type Revision } from "./api.ts";
import { agentShown } from "./branding.ts";
import { Composer } from "./Composer.tsx";
import { href } from "./route.ts";
import { AgentMark, button, Problem } from "./ui.tsx";

const words = defaultCopy.agentHome;

/** One job the home offers: what it is called, the message it sends, and a line on what it gives. */
export interface Starter {
  title: string;
  prompt: string;
  line: string;
}

/**
 * The jobs an agent's home offers: the starters its look keeps, or else one
 * per skill it uses by default, run as that skill's slash command (DES-047).
 */
export function startersOf(
  kept: readonly { title: string; prompt: string }[] | undefined,
  skills: readonly { name: string; description: string }[],
): Starter[] {
  if (kept?.length)
    return kept.map((st) => ({ title: st.title || st.prompt, prompt: st.prompt, line: st.prompt }));
  return skills.slice(0, 3).map((sk) => ({
    title: sk.name,
    prompt: `/${sk.name} `,
    line: sk.description.split(/(?<=\.)\s/)[0] ?? sk.description,
  }));
}

/** The skills an agent uses by default, by name and description. Nothing when they cannot be read. */
async function defaultSkills(name: string): Promise<{ name: string; description: string }[]> {
  const bindings = await api.agentSkillDefaults(name);
  const read = await Promise.all(
    bindings.skills.slice(0, 3).map(async (ref) => {
      const revisions = await api.librarySkill(ref.id);
      return revisions.find((r) => r.version === ref.version) ?? revisions[0];
    }),
  );
  return read.flatMap((r) => (r ? [{ name: r.name, description: r.description }] : []));
}

/**
 * An agent's home: its bot, who it says it is in its own voice, the jobs it
 * offers ("Put {name} to work"), each a session with it and the message
 * written, and a composer addressed to it that starts one. The settings are
 * for whoever looks after it.
 */
export function AgentHomeTab({
  revision,
  stats,
  canWrite,
  onLook,
  onOpenThread,
  seed,
}: {
  revision: Revision;
  /** Its runs over the last day, when they were read. */
  stats?: Group;
  canWrite: boolean;
  /** Open its Look settings. */
  onLook?: () => void;
  /** Open the session the composer started. */
  onOpenThread?: (id: string) => void;
  /** What to show before it is read, for tests and the gallery. */
  seed?: { skills?: { name: string; description: string }[]; control?: AgentControl };
}) {
  const name = revision.spec.name;
  const look = useSyncExternalStore(
    subscribeHues,
    () => agentLook(name),
    () => agentLook(name),
  );
  const shown = agentShown(name);
  const greeting = look?.greeting || revision.spec.description || words.noGreeting;
  const [skills, setSkills] = useState(seed?.skills ?? []);
  const [control, setControl] = useState<AgentControl | undefined>(seed?.control);
  const [problem, setProblem] = useState("");
  const kept = look?.starters;
  useEffect(() => {
    if (seed) return;
    let live = true;
    api
      .agentControl(name)
      .then((c) => live && setControl(c))
      .catch(() => {});
    if (!kept?.length)
      defaultSkills(name)
        .then((s) => live && setSkills(s))
        .catch(() => {});
    return () => {
      live = false;
    };
  }, [name, seed, kept?.length]);
  const starters = startersOf(kept, skills);

  const queue = useQueue({
    onStart: (message) => {
      setProblem("");
      call<AgentSession>("POST", "/api/sessions", { agent: name, text: message.text })
        .then((session) => {
          queue.turnEnded("done");
          if (onOpenThread) onOpenThread(session.id);
          else window.location.assign(href({ page: "conversation", id: session.id }));
        })
        .catch(() => {
          queue.reject();
          setProblem(words.startFailed);
        });
    },
    onInterrupt: () => queue.turnEnded("stopped"),
  });

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-6" data-testid="agent-home">
      <div className="flex flex-col items-center gap-2 pt-2">
        <AgentMark name={name} size={96} />
        {control?.paused && (
          <output className="text-center text-aui-sm text-aui-warn" data-testid="agent-home-paused">
            {words.paused(shown)}
          </output>
        )}
      </div>

      <div className="flex flex-col gap-2">
        <div className="flex items-end gap-2">
          <AgentMark name={name} size={24} />
          <div className="flex min-w-0 flex-col gap-1.5">
            <p className="self-start rounded-aui-lg rounded-bl-sm bg-aui-hover px-3.5 py-2 text-aui-sm text-aui-fg">
              {words.hello(shown)}
            </p>
            <p
              className="self-start rounded-aui-lg rounded-bl-sm bg-aui-hover px-3.5 py-2 text-aui-sm text-aui-fg"
              data-testid="agent-home-greeting"
            >
              {greeting}
            </p>
          </div>
        </div>
        {stats && stats.runs > 0 && (
          <span className="pl-8 text-aui-xs text-aui-muted">
            {words.lastDay(stats.runs, stats.failed)}
          </span>
        )}
      </div>

      {(starters.length > 0 || canWrite) && (
        <section className="flex flex-col gap-2 rounded-aui-lg border border-aui-border p-2">
          <h2 className="px-2 pt-1 font-medium text-aui-sm text-aui-fg">
            {words.putToWork(shown)}
          </h2>
          {starters.length === 0 ? (
            <div className="flex flex-wrap items-center justify-between gap-2 px-2 pb-1">
              <p className="text-aui-xs text-aui-muted">{words.noStarters}</p>
              {onLook && (
                <button type="button" className={button} onClick={onLook}>
                  {words.addStarters}
                </button>
              )}
            </div>
          ) : (
            <ul className="flex flex-col">
              {starters.map((st) => (
                <li key={st.title}>
                  <a
                    className="flex min-h-12 min-w-0 items-center gap-3 rounded-aui px-2 py-2 hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                    href={href({ page: "new", agent: name, ask: st.prompt })}
                  >
                    <span
                      aria-hidden
                      className="flex size-8 shrink-0 items-center justify-center rounded-aui border border-aui-border text-aui-muted"
                    >
                      <SparkleIcon className="size-4" />
                    </span>
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span className="truncate font-medium text-aui-sm text-aui-fg">
                        {st.title}
                      </span>
                      <span className="truncate text-aui-xs text-aui-muted">{st.line}</span>
                    </span>
                    <ChevronRightIcon aria-hidden className="size-4 shrink-0 text-aui-muted" />
                  </a>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

      <Problem message={problem} />
      <Composer placeholder={words.askPlaceholder(shown)} queue={queue} />
    </div>
  );
}
