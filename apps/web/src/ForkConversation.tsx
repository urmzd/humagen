import { defaultCopy } from "@humagen/components";
import { Inspector } from "@humagen/web";
import { type FormEvent, useEffect, useState } from "react";
import { api, coworking, type Thread } from "./api.ts";
import { href } from "./route.ts";
import { AgentMark, button, field, Label, primary, Problem } from "./ui.tsx";

const words = defaultCopy.forking;

/** Saved branch creation. The host owns navigation to the resulting thread. */
export function ForkConversation({ thread, through, onClose, onForked }: {
  thread: Thread; through: string; onClose: () => void; onForked: (thread: Thread) => void;
}) {
  const [title, setTitle] = useState(thread.title ? `${thread.title} (fork)` : "");
  const [available, setAvailable] = useState<string[] | null>(null);
  const [agents, setAgents] = useState<string[]>([]);
  const [mode, setMode] = useState<"best_fit" | "all">("best_fit");
  const [sources, setSources] = useState<string[]>([]);
  const [knownSources, setKnownSources] = useState<string[]>([]);
  const [sourceMode, setSourceMode] = useState("inherit");
  const [problem, setProblem] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  useEffect(() => {
    let active = true;
    Promise.all([coworking.available(thread.project), api.agentList()]).then(([result, visible]) => {
      if (active) setAvailable(visible.agents.map((agent) => agent.spec.name).filter((name) => result.available.names.includes(name) || result.available.all && (!visible.homes?.[name] || visible.homes[name] === "~")));
    }).catch(() => { if (active) setAvailable([]); });
    api.sources().then((result) => { if (active) setKnownSources(result.map((source) => source.name)); }).catch(() => {});
    return () => { active = false; };
  }, [thread.project]);
  const submit = (event: FormEvent) => {
    event.preventDefault();
    setSaving(true); setProblem(null);
    coworking.fork(thread.id, { through, title, agents, ...(agents.length > 1 ? { mode } : {}), ...(sourceMode === "inherit" ? {} : { sources: sourceMode === "none" ? [] : sources }) })
      .then(onForked).catch((error: unknown) => setProblem(error instanceof Error ? error.message : words.failed)).finally(() => setSaving(false));
  };
  return <Inspector open title={words.title} onClose={onClose}>
    <form onSubmit={submit} className="flex min-w-0 flex-col gap-5">
      <p className="text-aui-sm text-aui-muted">{words.hint}</p>
      <p className="text-aui-xs text-aui-muted">{words.through}: {through}</p>
      <Label text={words.name}><input className={field} value={title} maxLength={200} onChange={(event) => setTitle(event.target.value)} /></Label>
      <fieldset className="flex flex-col gap-2"><legend className="mb-2 text-aui-sm font-medium">{words.agents}</legend>
        {available === null ? <p className="text-aui-sm text-aui-muted">{words.loading}</p> : available.map((name) => <label key={name} className="flex min-h-10 items-center gap-2 text-aui-sm"><input type="checkbox" checked={agents.includes(name)} disabled={!agents.includes(name) && agents.length >= 5} onChange={(event) => setAgents(event.target.checked ? [...agents, name] : agents.filter((agent) => agent !== name))} /><AgentMark name={name} size={16} />{name}</label>)}
        {agents.length === 0 && <p className="text-aui-xs text-aui-muted">{words.empty}</p>}
      </fieldset>
      {agents.length > 1 && <Label text={words.mode}><select aria-label={words.mode} className={field} value={mode} onChange={(event) => setMode(event.target.value as "best_fit" | "all")}><option value="best_fit">{words.bestFit}</option><option value="all" disabled={agents.length > 3}>{words.all}</option></select></Label>}
      <Label text={words.sources}><select aria-label={words.sources} className={field} value={sourceMode} onChange={(event) => setSourceMode(event.target.value)}><option value="inherit">{words.inherit}</option><option value="none">{words.none}</option>{knownSources.length > 0 && <option value="selected">{words.selected}</option>}</select></Label>
      {sourceMode === "selected" && <fieldset className="flex flex-col gap-2"><legend className="text-aui-xs text-aui-muted">{words.sourceHint}</legend>{knownSources.map((name) => <label key={name} className="flex min-h-10 items-center gap-2 text-aui-sm"><input type="checkbox" checked={sources.includes(name)} disabled={!sources.includes(name) && sources.length >= 16} onChange={(event) => setSources(event.target.checked ? [...sources, name] : sources.filter((source) => source !== name))} />{name}</label>)}</fieldset>}
      <Problem message={problem} />
      <div className="flex flex-wrap justify-end gap-2"><button className={button} type="button" onClick={onClose}>{words.cancel}</button><button className={primary} type="submit" disabled={saving || available === null}>{words.create}</button></div>
    </form>
  </Inspector>;
}

export function ForkContext({ thread }: { thread: Thread }) {
  if (!thread.fork) return null;
  return <section className="flex min-w-0 flex-col gap-3">
    <h3 className="text-aui-sm font-medium">{words.context}</h3>
    <a className="text-aui-sm text-aui-accent underline" href={href({ page: "conversation", id: thread.fork.source_thread })}>{words.parent}</a>
    <p className="text-aui-xs text-aui-muted">{words.through}: {thread.fork.through}</p>
    <ol className="divide-aui-border divide-y">{thread.fork.context.map((turn, index) => <li key={`${index}:${turn.at}`} className="min-w-0 py-3"><div className="flex flex-wrap items-center gap-2 text-aui-xs text-aui-muted">{turn.kind === "agent" && <AgentMark name={turn.by} size={14} />}<span>{turn.name || turn.by}</span><time dateTime={turn.at}>{new Date(turn.at).toLocaleString()}</time></div><p className="mt-2 whitespace-pre-wrap break-words text-aui-sm">{turn.text}</p></li>)}</ol>
  </section>;
}

export function BringBackResult({ thread, run, text: initial, onClose, onPosted }: {
  thread: Thread; run: string; text: string; onClose: () => void; onPosted: () => void;
}) {
  const [text, setText] = useState(initial);
  const [saving, setSaving] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  return <Inspector open title={words.bringBack} onClose={onClose}><form className="flex min-w-0 flex-col gap-4" onSubmit={(event) => {
    event.preventDefault(); setSaving(true); setProblem(null);
    coworking.bringBack(thread.id, { run, text }).then(onPosted).catch((error: unknown) => setProblem(error instanceof Error ? error.message : words.bringFailed)).finally(() => setSaving(false));
  }}><p className="text-aui-sm text-aui-muted">{words.bringHint}</p><Label text={words.result}><textarea className={`${field} min-h-64`} value={text} maxLength={7600} onChange={(event) => setText(event.target.value)} /></Label><Problem message={problem} /><button type="submit" className={primary} disabled={saving || !text.trim()}>{words.bringBack}</button></form></Inspector>;
}
