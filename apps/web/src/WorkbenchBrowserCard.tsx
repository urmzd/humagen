import { defaultCopy } from "@humagen/components";
import type { Task } from "@humagen/core";
import { MonitorIcon } from "@humagen/render-web";
import { useMemo } from "react";
import { type BrowserShots, useBrowserShots } from "./browserShots.ts";
import { browserNow, browserSteps, callsOfTasks } from "./browserSteps.ts";
import { useOpenWorkbench } from "./workbench.ts";

const words = defaultCopy.workbenchBrowser;

/**
 * Under an answer that worked in a browser: one card for all of it. It says
 * where the browser was left, how much was done and where the browser runs,
 * and opens the Browser tab on the answer's last step. The picture is the
 * latest screenshot this console was given, when it has one; the platform
 * keeps none of the agent's own, so without one the card shows no picture.
 * Outside a session's page it is the same card, and opens nothing.
 */
export function BrowserCard({
  thread,
  tasks,
  shots,
}: {
  thread: string;
  /** The answer's steps, as the thread holds them. */
  tasks: Task[];
  shots?: BrowserShots;
}) {
  const open = useOpenWorkbench("browser");
  const steps = useMemo(() => browserSteps(callsOfTasks(tasks)), [tasks]);
  const taken = useBrowserShots(thread, shots);
  if (!steps.length) return null;
  const now = browserNow(steps);
  const latest = taken[taken.length - 1];
  const where = now.title ?? now.url ?? steps[steps.length - 1]?.label ?? "";
  const body = (
    <>
      <span className="flex h-14 w-20 shrink-0 items-center justify-center overflow-hidden rounded-aui border border-aui-border bg-aui-surface text-aui-muted">
        {latest ? (
          <img
            src={latest.src}
            alt={words.card.thumbnail}
            className="size-full object-cover object-top"
          />
        ) : (
          <MonitorIcon size={20} />
        )}
      </span>
      <span className="flex min-w-0 flex-1 flex-col text-left">
        <span className="font-medium text-aui-base text-aui-fg">{words.card.title}</span>
        <span className="truncate text-aui-base text-aui-muted" title={now.url ?? where}>
          {where}
        </span>
        <span className="truncate text-aui-base text-aui-muted">
          {[
            now.running ? words.card.working : "",
            words.card.steps(steps.length),
            now.provider ? words.card.on(now.provider) : "",
          ]
            .filter(Boolean)
            .join(" · ")}
        </span>
      </span>
    </>
  );
  const shape = "humagen-glass flex w-full max-w-md min-w-0 items-center gap-3 rounded-aui-lg p-3";
  return (
    <div className="pl-11" data-testid="browser-card">
      {open ? (
        <button
          type="button"
          aria-label={`${words.card.open}: ${where}`}
          onClick={() => open(steps[steps.length - 1]?.id)}
          className={`${shape} hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent`}
        >
          {body}
        </button>
      ) : (
        <div className={shape}>{body}</div>
      )}
    </div>
  );
}
