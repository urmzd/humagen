import { importDrift, importFacts } from "@humagen/adapter/imported";
import { defaultCopy } from "@humagen/components";
import { Inspector } from "@humagen/web";
import { type FormEvent, type ReactNode, useEffect, useId, useRef, useState } from "react";
import { api, coworking, type Thread } from "./api.ts";
import { Reference } from "./cards.tsx";
import { href } from "./route.ts";
import { AgentMark, button, field, Label, Problem, primary } from "./ui.tsx";

const words = defaultCopy.forking;
const imported = defaultCopy.continueImport;

/** Saved branch creation. The host owns navigation to the resulting thread. */
export function ForkConversation({
  thread,
  through,
  onClose,
  onForked,
  onBringBack,
  open = true,
}: {
  open?: boolean;
  thread: Thread;
  through: string;
  onClose: () => void;
  onForked: (thread: Thread) => void;
  onBringBack?: () => void;
}) {
  const [title, setTitle] = useState(thread.title ? `${thread.title} (fork)` : "");
  const [available, setAvailable] = useState<string[] | null>(null);
  const [agents, setAgents] = useState<string[]>([]);
  const [mode, setMode] = useState<"join" | "all">("join");
  const [sources, setSources] = useState<string[]>([]);
  const [knownSources, setKnownSources] = useState<string[]>([]);
  const [sourceMode, setSourceMode] = useState("inherit");
  const [problem, setProblem] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  useEffect(() => {
    let active = true;
    Promise.all([coworking.available(thread.project), api.agentList()])
      .then(([result, visible]) => {
        if (active)
          setAvailable(
            visible.agents
              .map((agent) => agent.spec.name)
              .filter(
                (name) =>
                  result.available.names.includes(name) ||
                  (result.available.all && (!visible.homes?.[name] || visible.homes[name] === "~")),
              ),
          );
      })
      .catch(() => {
        if (active) setAvailable([]);
      });
    api
      .sources()
      .then((result) => {
        if (active) setKnownSources(result.map((source) => source.name));
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, [thread.project]);
  const submit = (event: FormEvent) => {
    event.preventDefault();
    setSaving(true);
    setProblem(null);
    coworking
      .fork(thread.id, {
        through,
        title,
        agents,
        ...(agents.length > 1 ? { mode } : {}),
        ...(sourceMode === "inherit" ? {} : { sources: sourceMode === "none" ? [] : sources }),
      })
      .then(onForked)
      .catch((error: unknown) => setProblem(error instanceof Error ? error.message : words.failed))
      .finally(() => setSaving(false));
  };
  return (
    <Inspector open={open} title={words.title} onClose={onClose}>
      <form onSubmit={submit} className="flex min-w-0 flex-col gap-5">
        {onBringBack && (
          <div className="flex flex-col gap-2 border-b border-aui-border pb-4">
            <p className="text-aui-sm text-aui-muted">
              This answer belongs to a fork. You can review bringing it back to the parent session.
            </p>
            <button type="button" className={button} onClick={onBringBack}>
              Bring result back to parent
            </button>
          </div>
        )}
        <p className="text-aui-sm text-aui-muted">{words.hint}</p>
        <p className="text-aui-xs text-aui-muted">Fork starts after the selected message.</p>
        <Label text={words.name}>
          <input
            className={field}
            value={title}
            maxLength={200}
            onChange={(event) => setTitle(event.target.value)}
          />
        </Label>
        <fieldset className="flex flex-col gap-2">
          <legend className="mb-2 text-aui-sm font-medium">{words.agents}</legend>
          {available === null ? (
            <p className="text-aui-sm text-aui-muted">{words.loading}</p>
          ) : (
            available.map((name) => (
              <label key={name} className="flex min-h-10 items-center gap-2 text-aui-sm">
                <input
                  type="checkbox"
                  checked={agents.includes(name)}
                  disabled={!agents.includes(name) && agents.length >= 5}
                  onChange={(event) =>
                    setAgents(
                      event.target.checked
                        ? [...agents, name]
                        : agents.filter((agent) => agent !== name),
                    )
                  }
                />
                <AgentMark name={name} size={16} />
                {name}
              </label>
            ))
          )}
          {agents.length === 0 && <p className="text-aui-xs text-aui-muted">{words.empty}</p>}
        </fieldset>
        {agents.length > 1 && (
          <Label text={words.mode}>
            <select
              aria-label={words.mode}
              className={field}
              value={mode}
              onChange={(event) => setMode(event.target.value as "join" | "all")}
            >
              <option value="join">{words.join}</option>
              <option value="all" disabled={agents.length > 3}>
                {words.all}
              </option>
            </select>
          </Label>
        )}
        <Label text={words.sources}>
          <select
            aria-label={words.sources}
            className={field}
            value={sourceMode}
            onChange={(event) => setSourceMode(event.target.value)}
          >
            <option value="inherit">{words.inherit}</option>
            <option value="none">{words.none}</option>
            {knownSources.length > 0 && <option value="selected">{words.selected}</option>}
          </select>
        </Label>
        {sourceMode === "selected" && (
          <fieldset className="flex flex-col gap-2">
            <legend className="text-aui-xs text-aui-muted">{words.sourceHint}</legend>
            {knownSources.map((name) => (
              <label key={name} className="flex min-h-10 items-center gap-2 text-aui-sm">
                <input
                  type="checkbox"
                  checked={sources.includes(name)}
                  disabled={!sources.includes(name) && sources.length >= 16}
                  onChange={(event) =>
                    setSources(
                      event.target.checked
                        ? [...sources, name]
                        : sources.filter((source) => source !== name),
                    )
                  }
                />
                {name}
              </label>
            ))}
          </fieldset>
        )}
        <Problem message={problem} />
        <div className="flex flex-wrap justify-end gap-2">
          <button className={button} type="button" onClick={onClose}>
            {words.cancel}
          </button>
          <button className={primary} type="submit" disabled={saving || available === null}>
            {words.create}
          </button>
        </div>
      </form>
    </Inspector>
  );
}

/** One passive turn of a conversation's carried-over history, as a fork and a continuation show it. */
interface ContextTurn {
  key: string;
  /** A person's turn sits on the right, as the reader's own messages do. */
  person: boolean;
  mark?: ReactNode;
  label: string;
  at?: string;
  text: string;
  cut?: boolean;
}

/** The read-only turns a conversation carries from where it came from. */
function ContextTurns({ turns }: { turns: ContextTurn[] }) {
  return (
    <ol className="divide-aui-border divide-y">
      {turns.map((turn) => (
        <li
          key={turn.key}
          className={`min-w-0 py-3 ${turn.person ? "ml-auto w-fit max-w-[85%] rounded-aui-lg bg-aui-hover px-3" : ""}`}
        >
          <div className="flex flex-wrap items-center gap-2 text-aui-xs text-aui-muted">
            {turn.mark}
            <span>{turn.label}</span>
            {turn.at && <time dateTime={turn.at}>{new Date(turn.at).toLocaleString()}</time>}
            {turn.cut && <span>{imported.turnCut}</span>}
          </div>
          <p className="mt-2 whitespace-pre-wrap break-words text-aui-sm">{turn.text}</p>
        </li>
      ))}
    </ol>
  );
}

export function ForkContext({ thread }: { thread: Thread }) {
  if (!thread.fork) return null;
  return (
    <section className="flex min-w-0 flex-col gap-3">
      <h3 className="text-aui-sm font-medium">
        {thread.read_only ? words.archive : words.context}
      </h3>
      <a
        className="text-aui-sm text-aui-accent underline"
        href={href({ page: "conversation", id: thread.fork.source_thread })}
      >
        {words.parent}
      </a>
      <p className="text-aui-xs text-aui-muted">
        Saved context through the selected message. Replies continue here.
      </p>
      <ContextTurns
        turns={thread.fork.context.map((turn, index) => ({
          key: `${index}:${turn.at}`,
          person: turn.kind === "person",
          mark: turn.kind === "agent" ? <AgentMark name={turn.by} size={14} /> : undefined,
          label: turn.name || turn.by,
          at: turn.at,
          text: turn.text,
        }))}
      />
    </section>
  );
}

/** An instant as a reader's short local date and time. */
const when = (iso: string) =>
  new Date(iso).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });

/**
 * Where a conversation continued from an imported one came from: the source
 * and the document, what was carried over of it and what was not, when it
 * ran in its tool, and whether the document has changed since. Its turns are
 * the fork's read-only rendering, each named as imported from the tool.
 */
export function ImportContext({ thread, autoFocus }: { thread: Thread; autoFocus?: boolean }) {
  const origin = thread.import;
  const heading = useRef<HTMLHeadingElement>(null);
  const headingId = useId();
  const [drift, setDrift] = useState<"same" | "changed" | "gone" | null>(null);
  const source = origin?.source;
  const document = origin?.document;
  const digest = origin?.document_digest;
  useEffect(() => {
    if (!source || !document || !digest) return;
    let live = true;
    setDrift(null);
    api
      .documents(source)
      .then((listed) => {
        if (live) setDrift(importDrift({ document, document_digest: digest }, listed));
      })
      // A listing that cannot be read says nothing either way.
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [source, document, digest]);
  useEffect(() => {
    if (autoFocus && origin) heading.current?.focus();
  }, [autoFocus, origin]);
  if (!origin) return null;
  const tool = origin.tool ? imported.tools[origin.tool] : undefined;
  const facts = importFacts(origin);
  const sourcePage = href({ page: "context", source: origin.source });
  const fact = "min-w-0";
  return (
    <section aria-labelledby={headingId} className="flex min-w-0 flex-col gap-3">
      <h3
        id={headingId}
        ref={heading}
        tabIndex={-1}
        className="text-aui-sm font-medium outline-none focus-visible:outline-2 focus-visible:outline-aui-accent"
      >
        {tool ? imported.heading(tool) : imported.headingUnknown}
      </h3>
      <div className="flex min-w-0 flex-wrap items-center gap-1.5">
        <Reference kind="corpus" label={origin.source} href={sourcePage} />
        <Reference kind="file" label={origin.title} href={sourcePage} title={origin.uri} />
      </div>
      <ul
        aria-label={imported.facts}
        className="flex flex-wrap gap-x-4 gap-y-1 text-aui-xs text-aui-muted tabular-nums"
      >
        <li className={fact}>{imported.sent(facts.sent, facts.kept)}</li>
        {facts.known ? (
          <li className={fact}>{imported.mainPath}</li>
        ) : (
          <>
            <li className={fact}>{imported.oneBlock}</li>
            <li className={fact}>{imported.speakersUnknown}</li>
          </>
        )}
        {facts.shortened > 0 && <li className={fact}>{imported.shortened(facts.shortened)}</li>}
        {facts.branches !== undefined && facts.branches > 0 && (
          <li className={fact}>{imported.branches(facts.branches)}</li>
        )}
        {facts.files !== undefined && facts.files > 0 && (
          <li className={fact}>{imported.files(facts.files)}</li>
        )}
        {facts.importLeftOut === undefined ? (
          <li className={fact}>{imported.importUnknown}</li>
        ) : (
          facts.importLeftOut > 0 && (
            <li className={fact}>{imported.importLeftOut(facts.importLeftOut)}</li>
          )
        )}
        {facts.importShortened !== undefined && facts.importShortened > 0 && (
          <li className={fact}>{imported.importShortened(facts.importShortened)}</li>
        )}
      </ul>
      <p className="flex flex-wrap gap-x-4 gap-y-1 text-aui-xs text-aui-muted">
        {origin.started_at && (
          <span>
            {imported.started} <time dateTime={origin.started_at}>{when(origin.started_at)}</time>
          </span>
        )}
        {origin.ended_at && (
          <span>
            {imported.ended} <time dateTime={origin.ended_at}>{when(origin.ended_at)}</time>
          </span>
        )}
        <span title={origin.document_digest}>
          {imported.revision(origin.document_digest.slice(0, 12))}
        </span>
      </p>
      {drift === "changed" && <p className="text-aui-sm text-aui-fg">{imported.changedSince}</p>}
      {drift === "gone" && <p className="text-aui-sm text-aui-fg">{imported.goneSince}</p>}
      <ContextTurns
        turns={origin.context.map((turn, index) => ({
          key: `${index}:${turn.at ?? ""}`,
          person: turn.speaker === "user",
          label: tool
            ? imported.speaker(imported.speakers[turn.speaker], tool)
            : imported.speakers[turn.speaker],
          at: turn.at,
          text: turn.text,
          cut: turn.cut,
        }))}
      />
    </section>
  );
}

export function BringBackResult({
  thread,
  run,
  text: initial,
  onClose,
  onPosted,
  open = true,
}: {
  open?: boolean;
  thread: Thread;
  run: string;
  text: string;
  onClose: () => void;
  onPosted: () => void;
}) {
  const [text, setText] = useState(initial);
  const [saving, setSaving] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  return (
    <Inspector open={open} title={words.bringBack} onClose={onClose}>
      <form
        className="flex min-w-0 flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          setSaving(true);
          setProblem(null);
          coworking
            .bringBack(thread.id, { run, text })
            .then(onPosted)
            .catch((error: unknown) =>
              setProblem(error instanceof Error ? error.message : words.bringFailed),
            )
            .finally(() => setSaving(false));
        }}
      >
        <p className="text-aui-sm text-aui-muted">{words.bringHint}</p>
        <Label text={words.result}>
          <textarea
            className={`${field} min-h-64`}
            value={text}
            maxLength={7600}
            onChange={(event) => setText(event.target.value)}
          />
        </Label>
        <Problem message={problem} />
        <button type="submit" className={primary} disabled={saving || !text.trim()}>
          {words.bringBack}
        </button>
      </form>
    </Inspector>
  );
}
