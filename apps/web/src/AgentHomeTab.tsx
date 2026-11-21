import { defaultCopy } from "@humagen/components";
import { useSyncExternalStore } from "react";
import { agentLook, subscribeHues } from "./agentHues.ts";
import type { Group, Revision } from "./api.ts";
import { agentShown } from "./branding.ts";
import { href } from "./route.ts";
import { AgentMark, button, primary } from "./ui.tsx";

const words = defaultCopy.agentHome;

/**
 * An agent's home: who it is and how to ask it. Its bot and the name people
 * call it, its greeting in its own voice, Ask, and the starters it offers,
 * each a new conversation with it and the message written (RFC 0012). The
 * other tabs are for whoever looks after it.
 */
export function AgentHomeTab({
  revision,
  stats,
  canWrite,
  onLook,
}: {
  revision: Revision;
  /** Its runs over the last day, when they were read. */
  stats?: Group;
  canWrite: boolean;
  /** Open its Look tab. */
  onLook?: () => void;
}) {
  const name = revision.spec.name;
  const look = useSyncExternalStore(
    subscribeHues,
    () => agentLook(name),
    () => agentLook(name),
  );
  const shown = agentShown(name);
  const greeting = look?.greeting || revision.spec.description || words.noGreeting;
  const starters = look?.starters ?? [];
  return (
    <div className="flex flex-col gap-6" data-testid="agent-home">
      <div className="flex flex-col items-center gap-3 rounded-aui border border-aui-border bg-aui-surface p-6 text-center sm:flex-row sm:items-start sm:text-left">
        <AgentMark name={name} size={72} />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="font-semibold text-aui-lg">{shown}</span>
          <span className="text-aui-xs text-aui-muted">@{name}</span>
          <p className="max-w-prose text-aui-sm" data-testid="agent-home-greeting">
            {greeting}
          </p>
          {stats && stats.runs > 0 && (
            <span className="text-aui-xs text-aui-muted">
              {words.lastDay(stats.runs, stats.failed)}
            </span>
          )}
        </div>
        <div className="flex shrink-0 flex-wrap gap-2">
          <a className={primary} href={href({ page: "new", agent: name })}>
            {words.ask(shown)}
          </a>
          {canWrite && onLook && (
            <button type="button" className={button} onClick={onLook}>
              {words.editLook}
            </button>
          )}
        </div>
      </div>
      <section className="flex flex-col gap-2">
        <h2 className="font-medium text-aui-sm">{words.putToWork(shown)}</h2>
        {starters.length === 0 ? (
          <p className="text-aui-xs text-aui-muted">
            {canWrite ? words.noStartersOwner : words.noStarters}
          </p>
        ) : (
          <ul className="flex flex-col divide-y divide-aui-border rounded-aui border border-aui-border">
            {starters.map((st) => (
              <li key={st.title}>
                <a
                  className="flex min-w-0 flex-col gap-0.5 px-4 py-3 hover:bg-aui-hover"
                  href={href({ page: "new", agent: name, ask: st.prompt })}
                >
                  <span className="truncate font-medium text-aui-sm">{st.title}</span>
                  <span className="truncate text-aui-xs text-aui-muted">{st.prompt}</span>
                </a>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
