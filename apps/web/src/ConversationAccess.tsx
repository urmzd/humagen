import { defaultCopy } from "@humagen/components";
import { type FormEvent, useEffect, useState } from "react";
import { api, coworking, type Thread } from "./api.ts";
import { AgentMark, button, Confirm, field, Label, primary, Problem } from "./ui.tsx";

const words = defaultCopy.conversationAccess;

/** Conversation participation is narrower than project roles. The server owns both checks. */
export function ConversationAccess({ thread, onChanged }: { thread: Thread; onChanged?: (thread: Thread) => void }) {
  const [current, setCurrent] = useState(thread);
  const [handle, setHandle] = useState("");
  const [kind, setKind] = useState<"person" | "agent">("person");
  const [myHandle, setMyHandle] = useState("");
  const [write, setWrite] = useState(false);
  const [manage, setManage] = useState(false);
  const [saving, setSaving] = useState(false);
  const [remove, setRemove] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => setCurrent(thread), [thread]);
  useEffect(() => {
    let active = true;
    Promise.all([api.me(), api.project(thread.project)]).then(([me, project]) => {
      if (!active) return;
      const mine = me.handle || me.principal.subject;
      setMyHandle(mine);
      const mayWrite = me.permissions.includes("runs:write") && (project.project.access.role === "owner" || project.project.access.role === "member") && (thread.visibility === "shared" || thread.participants.some((participant) => participant.kind === "person" && participant.name === mine));
      setWrite(mayWrite);
      setManage(mayWrite && (thread.created_by === mine || project.project.access.role === "owner"));
    }).catch(() => { if (active) { setWrite(false); setManage(false); } });
    return () => { active = false; };
  }, [thread]);
  const keep = (next: Thread) => { setCurrent(next); onChanged?.(next); setProblem(null); };
  const change = (operation: Promise<Thread>) => {
    setSaving(true); setProblem(null);
    operation.then(keep).catch((error: unknown) => setProblem(error instanceof Error ? error.message : words.failed)).finally(() => setSaving(false));
  };
  const add = (event: FormEvent) => {
    event.preventDefault();
    change(coworking.participants(current.id, { version: current.version, add: [{ kind, name: handle.trim().replace(/^@/, "") }] }).then((next) => { setHandle(""); return next; }));
  };
  return <div className="flex min-w-0 flex-col gap-5">
    <p className="text-aui-sm text-aui-muted">{current.visibility === "private" ? words.privateHint : words.sharedHint}</p>
    {current.fork && <p className="text-aui-xs text-aui-muted">{words.forkHint}</p>}
    {manage && <button type="button" className={`${button} self-start`} disabled={saving} onClick={() => change(coworking.update(current.id, { version: current.version, title: current.title || "", visibility: current.visibility === "private" ? "shared" : "private", default: current.default || "", agents: current.agents, mode: current.mode }))}>{current.visibility === "private" ? words.share : words.makePrivate}</button>}
    <ul className="divide-aui-border divide-y">{current.participants.map((person) => <li key={`${person.kind}:${person.name}`} className="flex min-w-0 flex-wrap items-center gap-2 py-3 text-aui-sm">
      {person.kind === "agent" && <AgentMark name={person.name} size={15} />}<span className="min-w-0 flex-1 break-words">{person.name}</span><span className="text-aui-xs text-aui-muted">{person.owner ? words.owner : person.kind === "person" ? words.person : words.agent}</span>
      {write && !person.owner && (manage || person.kind === "person" && person.name === myHandle) && (remove === person.name ? <Confirm question={`${words.remove} ${person.name}?`} action={words.remove} onKeep={() => setRemove(null)} onConfirm={() => { setRemove(null); change(coworking.participants(current.id, { version: current.version, remove: [person.name] })); }} /> : <button type="button" className={button} disabled={saving} onClick={() => setRemove(person.name)}>{words.remove}</button>)}
    </li>)}</ul>
    {write && <form className="flex flex-col gap-3" onSubmit={add}><Label text={words.kind}><select aria-label={words.kind} className={field} value={kind} onChange={(event) => setKind(event.target.value as "person" | "agent")}><option value="person">{words.person}</option><option value="agent">{words.agent}</option></select></Label><Label text={words.handle}><input className={field} value={handle} onChange={(event) => setHandle(event.target.value)} maxLength={200} required /></Label><button type="submit" className={`${primary} self-start`} disabled={saving || !handle.trim()}>{words.add}</button></form>}
    <Problem message={problem} />
  </div>;
}
