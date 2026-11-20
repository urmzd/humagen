import { defaultCopy } from "@humagen/components";
import { useCallback, useEffect, useState } from "react";
import { api, type BackgroundTask } from "./api.ts";
import { AgentTag, ItemCard, itemGrid, Pill, Tag, type Tone } from "./cards.tsx";
import { age, ago } from "./format.ts";
import { Empty, Problem, SkeletonRows } from "./ui.tsx";

const words = defaultCopy.background;

const stateTone: Record<BackgroundTask["state"], Tone> = {
  active: "ok",
  done: "ok",
  expired: "neutral",
  stopped: "neutral",
  failed: "bad",
};

function firstLine(text?: string): string | undefined {
  const line = text?.split("\n").find((l) => l.trim() !== "");
  return line?.trim() || undefined;
}

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
 * Your background work: the watches, goals and tasks agents keep at for you,
 * what is still running first. A card opens the conversation its runs are in;
 * its menu stops one still running.
 */
export function Background({
  onConversation,
  tick = 0,
}: {
  onConversation: (id: string) => void;
  /** Bumped whenever a run starts or ends: the list is read again, as its runs are what change it. */
  tick?: number;
}) {
  const [items, setItems] = useState<BackgroundTask[] | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [stopping, setStopping] = useState<string | null>(null);

  const load = useCallback(() => {
    setProblem(null);
    api.background().then(setItems, () => setProblem(words.couldNotLoad));
  }, []);
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick is the signal to read again.
  useEffect(load, [load, tick]);

  const stop = (id: string) => {
    setStopping(id);
    setProblem(null);
    api
      .stopBackground(id)
      .then(
        (next) => setItems((now) => (now ?? []).map((t) => (t.id === id ? next : t))),
        () => setProblem(words.couldNotStop),
      )
      .finally(() => setStopping(null));
  };

  if (items === null) {
    return problem ? (
      <div className="flex flex-col items-start gap-3">
        <Problem message={problem} />
        <button
          type="button"
          className="text-aui-sm text-aui-accent underline-offset-2 hover:underline"
          onClick={load}
        >
          {words.retry}
        </button>
      </div>
    ) : (
      <SkeletonRows count={4} height="5.5rem" />
    );
  }
  if (items.length === 0) return <Empty title={words.empty} />;
  return (
    <div className="flex flex-col gap-3">
      <Problem message={problem} />
      <ul className={itemGrid} aria-label={words.title}>
        {items.map((t) => {
          const running = t.state === "active";
          const where = t.thread || t.session;
          return (
            <li key={t.id} className="min-w-0">
              <ItemCard
                title={t.text}
                onOpen={where ? () => onConversation(where) : undefined}
                status={
                  running && t.working ? (
                    <Pill tone="accent">{words.working}</Pill>
                  ) : (
                    <Pill tone={stateTone[t.state] ?? "neutral"}>
                      {words.state[t.state] ?? t.state}
                    </Pill>
                  )
                }
                description={firstLine(t.result) ?? firstLine(t.how)}
                facts={
                  <>
                    <Tag>{words.kind[t.kind] ?? t.kind}</Tag>
                    <AgentTag name={t.agent} />
                    {progress(t) && <Tag>{progress(t)}</Tag>}
                    {running && !t.working && t.next && <Tag>{nextIn(t.next)}</Tag>}
                    {running && <Tag>{words.stopsIn(ago(t.until))}</Tag>}
                  </>
                }
                time={age(running ? t.created_at : (t.ended_at ?? t.created_at))}
                menuLabel={`More for ${t.text}`}
                menu={
                  running
                    ? [
                        {
                          label: words.stop,
                          destructive: true,
                          disabled: stopping === t.id,
                          onSelect: () => stop(t.id),
                        },
                      ]
                    : undefined
                }
              />
            </li>
          );
        })}
      </ul>
    </div>
  );
}
