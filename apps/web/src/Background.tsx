import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { api, type BackgroundTask } from "./api.ts";
import type { Tone } from "./cards.tsx";
import { ago } from "./format.ts";
import { Section } from "./SessionSections.tsx";

const words = defaultCopy.background;

const stateTone: Record<BackgroundTask["state"], Tone> = {
  active: "ok",
  done: "ok",
  expired: "neutral",
  stopped: "neutral",
  failed: "bad",
};

/** A state's word takes its tone: a failure reads as one, the rest stay quiet. */
const stateWords: Record<Tone, string> = {
  ok: "text-aui-muted",
  neutral: "text-aui-muted",
  bad: "text-aui-bad",
  warn: "text-aui-warn",
  accent: "text-aui-muted",
};

/** When the next run starts, in words, or soon when it is due. */
function nextIn(iso: string): string {
  const when = ago(iso);
  return when === "just now" ? words.nextSoon : words.next(when);
}

/** A Go duration as a person reads it: "15m0s" is 15m, "1h30m0s" is 1h30m. */
export function span(d: string): string {
  return d.replace(/([hm])0s$/, "$1").replace(/h0m$/, "h");
}

/** What a card says about how far along a task is. */
function progress(t: BackgroundTask): string | undefined {
  if (t.kind === "goal") return words.turns(t.runs, t.limit);
  if (t.kind === "watch")
    return t.state === "active" && t.every ? words.every(span(t.every)) : words.checks(t.runs);
  return undefined;
}

/**
 * A session's background work, as a section of its Details: the watches,
 * goals and tasks its agents keep at for it, what is still running first.
 * Each session carries its own, so it is listed here and nowhere else. A
 * running one has Stop on its row. With none, the section is left out.
 */
export function SessionBackground({
  session,
  version,
}: {
  session: string;
  /** Changes as the session's runs start and end: the list is read again. */
  version?: unknown;
}) {
  const [items, setItems] = useState<BackgroundTask[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [stopping, setStopping] = useState<string | null>(null);

  // biome-ignore lint/correctness/useExhaustiveDependencies: version is the signal to read again.
  useEffect(() => {
    let live = true;
    api.background().then(
      (all) => {
        if (live) setItems(all.filter((t) => t.session === session || t.thread === session));
      },
      // Background work is extra: Details reads fine without it.
      () => {},
    );
    return () => {
      live = false;
    };
  }, [session, version]);

  const stop = (id: string) => {
    setStopping(id);
    setProblem(null);
    api
      .stopBackground(id)
      .then(
        (next) => setItems((now) => now.map((t) => (t.id === id ? next : t))),
        () => setProblem(words.couldNotStop),
      )
      .finally(() => setStopping(null));
  };

  if (items.length === 0) return null;
  const order = [...items].sort(
    (a, b) => Number(b.state === "active") - Number(a.state === "active"),
  );
  return (
    <Section label={words.title} testId="background">
      {order.map((t) => {
        const running = t.state === "active";
        const facts = [
          words.kind[t.kind] ?? t.kind,
          t.agent,
          progress(t),
          running && !t.working && t.next ? nextIn(t.next) : undefined,
          running ? words.stopsIn(ago(t.until)) : undefined,
        ].filter(Boolean);
        return (
          <li
            key={t.id}
            data-testid="details-background"
            data-state={t.state}
            className="flex min-h-9 items-center justify-between gap-2 px-2.5 py-1.5 text-aui-sm"
          >
            <span className="flex min-w-0 flex-col">
              <span className="min-w-0 truncate text-aui-fg" title={t.text}>
                {t.text}
              </span>
              <span className="min-w-0 truncate text-aui-muted text-aui-xs">
                {facts.join(" · ")}
              </span>
            </span>
            <span className="flex shrink-0 items-center gap-1 text-aui-xs">
              <span className={stateWords[stateTone[t.state] ?? "neutral"]}>
                {running && t.working ? words.working : (words.state[t.state] ?? t.state)}
              </span>
              {running && (
                <button
                  type="button"
                  aria-label={words.stopLabel(t.text)}
                  disabled={stopping === t.id}
                  onClick={() => stop(t.id)}
                  className="inline-flex min-h-7 items-center rounded-aui px-1.5 text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-60 pointer-coarse:min-h-11"
                >
                  {words.stop}
                </button>
              )}
            </span>
          </li>
        );
      })}
      {problem && (
        <li role="alert" className="px-2.5 text-aui-bad text-aui-xs">
          {problem}
        </li>
      )}
    </Section>
  );
}
