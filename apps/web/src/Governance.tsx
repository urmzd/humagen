import { useCallback, useEffect, useState } from "react";
import {
  ApiError,
  api,
  type CallApproval,
  type CallCallers,
  type CallPolicy,
  type CallPolicyInput,
  callGovernance,
  type QueuedApproval,
  type Reach,
  type Revision,
} from "./api.ts";
import { AgentMark, ago, button, card, field, Label, PickMany, Problem, primary, rows, Section, Switch } from "./ui.tsx";

/** The roles a workspace ships with, offered first when choosing approvers. */
const ROLES = [
  { value: "role:admin", label: "Admins" },
  { value: "role:manager", label: "Managers" },
  { value: "role:operator", label: "Operators" },
];

const HOURS = [1, 4, 24, 72, 168];
const waitWords = (seconds: number) => (seconds % 86400 === 0 ? `${seconds / 86400} ${seconds === 86400 ? "day" : "days"}` : `${seconds / 3600} hours`);

/** A policy as the form edits it. */
function inputOf(p: CallPolicy): CallPolicyInput {
  return {
    callers: p.callers,
    allow: p.allow ?? [],
    approval: p.approval,
    trusted: p.trusted ?? [],
    approvers: p.approvers ?? [],
    self_approval: p.self_approval,
    expires_seconds: p.expires_seconds || 86400,
  };
}

/** Only the fields the chosen modes use, so a stale list never rides along. */
function bodyOf(p: CallPolicyInput): CallPolicyInput {
  return {
    callers: p.callers,
    ...(p.callers === "listed" ? { allow: p.allow ?? [] } : {}),
    approval: p.callers === "none" ? "never" : p.approval,
    ...(p.callers !== "none" && p.approval === "unlisted" ? { trusted: p.trusted ?? [] } : {}),
    ...(p.callers !== "none" && p.approval !== "never"
      ? { approvers: p.approvers ?? [], self_approval: p.self_approval ?? false, expires_seconds: p.expires_seconds ?? 86400 }
      : {}),
  };
}

/** What a policy means, in a sentence. */
export function policyWords(p: CallPolicyInput): string {
  const who = p.callers === "none" ? "No agent may call it." : p.callers === "listed" ? `Only ${(p.allow ?? []).join(", ") || "the agents listed"} may call it.` : "Any agent that lists it may call it.";
  if (p.callers === "none" || p.approval === "never") return `${who} Calls run without asking anyone.`;
  const approvers = (p.approvers ?? []).length === 0 ? "a manager or an admin" : (p.approvers ?? []).map((a) => ROLES.find((r) => r.value === a)?.label ?? a).join(", ");
  const which = p.approval === "always" ? "Every call" : `A call from anyone but ${(p.trusted ?? []).join(", ") || "its trusted callers"}`;
  return `${who} ${which} waits for ${approvers} to approve it, for up to ${waitWords(p.expires_seconds ?? 86400)}.`;
}

/**
 * Who may call an agent, and when a call waits for a person. The agent being
 * called decides; callers only see the agents that would take their call. It
 * applies to calls from the moment it is saved, not with the agent's next
 * revision.
 */
export function CallPolicyEditor({ agent, agents, canWrite }: { agent: string; agents: Revision[]; canWrite: boolean }) {
  const [saved, setSaved] = useState<CallPolicy | null>(null);
  const [draft, setDraft] = useState<CallPolicyInput | null>(null);
  const [people, setPeople] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let live = true;
    callGovernance
      .policy(agent)
      .then((p) => {
        if (!live) return;
        setSaved(p);
        setDraft(inputOf(p));
        setPeople((p.approvers ?? []).filter((a) => !ROLES.some((r) => r.value === a)).join("\n"));
      })
      .catch((e) => live && setProblem(e instanceof ApiError ? e.message : "The call policy could not be read."));
    return () => {
      live = false;
    };
  }, [agent]);

  if (!draft || !saved) return <Problem message={problem} />;
  const set = (patch: Partial<CallPolicyInput>) => setDraft((d) => (d ? { ...d, ...patch } : d));
  const roles = (draft.approvers ?? []).filter((a) => ROLES.some((r) => r.value === a));
  const named = people.split(/[\n,]+/).map((x) => x.trim()).filter(Boolean);
  const next = bodyOf({ ...draft, approvers: [...new Set([...roles, ...named])] });
  const dirty = JSON.stringify(next) !== JSON.stringify(bodyOf(inputOf(saved)));
  const others = agents.filter((a) => a.spec.name !== agent).map((a) => ({
    value: a.spec.name,
    label: a.spec.name,
    hint: a.spec.description || undefined,
    mark: <AgentMark name={a.spec.name} size={14} />,
  }));

  const save = () => {
    setSaving(true);
    setProblem(null);
    callGovernance
      .setPolicy(agent, next)
      .then((p) => {
        setSaved(p);
        setDraft(inputOf(p));
      })
      .catch((e) => setProblem(e instanceof ApiError ? e.message : "The call policy could not be saved."))
      .finally(() => setSaving(false));
  };

  const radio = <T extends string>(name: string, value: T, current: T, label: string, hint: string, onPick: (v: T) => void) => (
    <label className="flex items-start gap-2.5 text-aui-sm text-aui-fg">
      <input type="radio" name={name} className="mt-1" disabled={!canWrite} checked={current === value} onChange={() => onPick(value)} />
      <span>
        {label}
        <span className="block text-aui-xs text-aui-muted">{hint}</span>
      </span>
    </label>
  );

  return (
    <div className="flex flex-col gap-4">
      <p data-testid="call-policy-words" className="text-aui-sm text-aui-fg">
        {policyWords(next)}
      </p>
      <fieldset className={`${card} flex flex-col gap-3`}>
        <legend className="sr-only">Who may call {agent}</legend>
        <p className="text-aui-sm font-medium text-aui-fg">Who may call it</p>
        {radio<CallCallers>(`callers-${agent}`, "anyone", draft.callers, "Any agent", "Any agent whose own setup lists this one.", (callers) => set({ callers }))}
        {radio<CallCallers>(`callers-${agent}`, "listed", draft.callers, "Only these agents", "Others do not see it as something they can call.", (callers) => set({ callers }))}
        {draft.callers === "listed" && (
          <div className="pl-6">
            <PickMany label={`Agents that may call ${agent}`} add="Add Agent" disabled={!canWrite} empty="None yet, so nobody can call it." value={draft.allow ?? []} options={others} onChange={(allow) => set({ allow })} />
          </div>
        )}
        {radio<CallCallers>(`callers-${agent}`, "none", draft.callers, "No agent", "It only answers people and schedules.", (callers) => set({ callers }))}
      </fieldset>

      {draft.callers !== "none" && (
        <fieldset className={`${card} flex flex-col gap-3`}>
          <legend className="sr-only">When a call waits for a person</legend>
          <p className="text-aui-sm font-medium text-aui-fg">When a call waits for a person</p>
          {radio<CallApproval>(`approval-${agent}`, "never", draft.approval, "Never", "Calls run as soon as they are made.", (approval) => set({ approval }))}
          {radio<CallApproval>(`approval-${agent}`, "unlisted", draft.approval, "Unless the caller is trusted", "Trusted callers go straight through; every other call waits.", (approval) => set({ approval }))}
          {draft.approval === "unlisted" && (
            <div className="pl-6">
              <PickMany label={`Callers ${agent} trusts`} add="Add Agent" disabled={!canWrite} empty="None yet, so every call waits." value={draft.trusted ?? []} options={others} onChange={(trusted) => set({ trusted })} />
            </div>
          )}
          {radio<CallApproval>(`approval-${agent}`, "always", draft.approval, "Always", "Every call waits, whoever makes it.", (approval) => set({ approval }))}

          {draft.approval !== "never" && (
            <div className="flex flex-col gap-3 border-aui-border border-t pt-3">
              <div className="flex flex-col gap-1.5">
                <span className="text-aui-xs font-medium text-aui-muted">Who approves</span>
                <span className="flex flex-wrap gap-1.5">
                  {ROLES.map((r) => (
                    <button
                      key={r.value}
                      type="button"
                      aria-pressed={roles.includes(r.value)}
                      disabled={!canWrite}
                      className={`${button} h-7 px-2.5 text-aui-xs aria-pressed:border-aui-accent aria-pressed:bg-aui-accent-soft`}
                      onClick={() => set({ approvers: roles.includes(r.value) ? roles.filter((x) => x !== r.value) : [...roles, r.value] })}
                    >
                      {r.label}
                    </button>
                  ))}
                </span>
              </div>
              <Label text="And these people" hint="Sign-in subjects, one per line. With nobody chosen, managers and admins approve.">
                <textarea className={`${field} min-h-14 font-aui-mono text-aui-sm`} spellCheck={false} disabled={!canWrite} value={people} onChange={(e) => setPeople(e.target.value)} />
              </Label>
              <Switch
                label="The person who started the task may approve"
                hint="Off, an approval is always someone else's."
                checked={draft.self_approval ?? false}
                disabled={!canWrite}
                onChange={(self_approval) => set({ self_approval })}
              />
              <Label text="Deny if nobody answers within">
                <span className="flex flex-wrap gap-1.5">
                  {HOURS.map((h) => (
                    <button
                      key={h}
                      type="button"
                      aria-pressed={(draft.expires_seconds ?? 86400) === h * 3600}
                      disabled={!canWrite}
                      className={`${button} h-7 px-2.5 text-aui-xs aria-pressed:border-aui-accent aria-pressed:bg-aui-accent-soft`}
                      onClick={() => set({ expires_seconds: h * 3600 })}
                    >
                      {waitWords(h * 3600)}
                    </button>
                  ))}
                </span>
              </Label>
            </div>
          )}
        </fieldset>
      )}

      <Problem message={problem} />
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-aui-xs text-aui-muted">
          {saved.version === 0 ? "Not set yet: any agent that lists it may call it." : `Saved ${saved.updated_at ? ago(saved.updated_at) : ""}${saved.updated_by ? ` by ${saved.updated_by}` : ""}. Applies to calls from now on.`}
        </span>
        {canWrite && (
          <span className="flex gap-2">
            {dirty && (
              <button type="button" className={button} onClick={() => setDraft(inputOf(saved))}>
                Discard
              </button>
            )}
            <button type="button" className={primary} disabled={!dirty || saving} onClick={save}>
              Save Call Policy
            </button>
          </span>
        )}
      </div>
    </div>
  );
}

/** For an agent as a caller: which agents it may reach, so it is only offered those. */
export function useReach(agent: string | null, signal?: unknown): Reach[] | null {
  const [reach, setReach] = useState<Reach[] | null>(null);
  // biome-ignore lint/correctness/useExhaustiveDependencies: signal is the reason to look again
  useEffect(() => {
    if (!agent) return setReach(null);
    let live = true;
    callGovernance
      .reachable(agent)
      .then((r) => live && setReach(r.agents))
      .catch(() => live && setReach(null));
    return () => {
      live = false;
    };
  }, [agent, signal]);
  return reach;
}

/**
 * The calls waiting on a person: ones this agent wants to make, and ones that
 * would call it. Each is answered here, in its task, or in the inbox; an answer
 * in one place settles it everywhere. Only what the reader may answer is listed.
 */
export function ApprovalQueue({ agent, onOpenTask, onCount }: { agent: string; onOpenTask?: (thread: string) => void; onCount?: (n: number) => void }) {
  const [state, setState] = useState<"open" | "resolved">("open");
  const [items, setItems] = useState<QueuedApproval[] | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [notes, setNotes] = useState<Record<string, string>>({});

  const load = useCallback(() => {
    callGovernance
      .approvalQueue(agent, state)
      .then((r) => {
        setItems(r.messages);
        if (state === "open") onCount?.(r.messages.length);
      })
      .catch((e) => setProblem(e instanceof ApiError ? e.message : "The queue could not be read."));
  }, [agent, state, onCount]);
  useEffect(load, [load]);

  const decide = (m: QueuedApproval, action: "approve" | "deny") => {
    const q = m.parts.find((p) => p.question)?.question;
    if (!q) return;
    setBusy(m.id);
    setProblem(null);
    api
      .answer({ run: q.run, question: q.id, body: { action, note: notes[m.id]?.trim() || undefined } })
      .then(load)
      .catch((e) => setProblem(e instanceof ApiError ? e.message : "The answer was not taken."))
      .finally(() => setBusy(null));
  };

  return (
    <Section
      title="Approval queue"
      hint="Calls waiting for a person, ones this agent would make and ones that would call it. Answering here, in the task, or in the inbox settles it everywhere."
      actions={
        <span className="flex gap-1.5">
          {(["open", "resolved"] as const).map((s) => (
            <button key={s} type="button" aria-pressed={state === s} className={`${button} h-7 px-2.5 text-aui-xs aria-pressed:border-aui-accent`} onClick={() => setState(s)}>
              {s === "open" ? "Waiting" : "Answered"}
            </button>
          ))}
        </span>
      }
    >
      <Problem message={problem} />
      {items === null ? null : items.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">{state === "open" ? "Nothing is waiting on you here." : "Nothing answered yet."}</p>
      ) : (
        <ul className={rows} aria-label={`Approvals for ${agent}`}>
          {items.map((m) => {
            const q = m.parts.find((p) => p.question)?.question;
            const caller = m.source.agent;
            const callee = m.source.target;
            return (
              <li key={m.id} className="flex flex-col gap-2 py-3">
                <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-aui-sm text-aui-fg">
                  {caller && <AgentMark name={caller} size={12} />}
                  <span className="font-medium">{caller ?? "An agent"}</span>
                  {callee && (
                    <>
                      <span className="text-aui-muted">would call</span>
                      <AgentMark name={callee} size={12} />
                      <span className="font-medium">{callee}</span>
                    </>
                  )}
                  {m.source.project && <span className="text-aui-xs text-aui-muted">in {m.source.project}</span>}
                </div>
                <p className="whitespace-pre-wrap text-aui-sm text-aui-muted">{q?.prompt ?? m.subject}</p>
                <div className="flex flex-wrap items-center gap-2">
                  {m.source.thread && onOpenTask && (
                    <button type="button" className={button} onClick={() => onOpenTask(m.source.thread as string)}>
                      Open Task
                    </button>
                  )}
                  {state === "open" && q && (
                    <>
                      <input
                        aria-label="Note with the answer"
                        className={`${field} h-8 max-w-xs flex-1`}
                        placeholder="Note (optional)"
                        value={notes[m.id] ?? ""}
                        onChange={(e) => setNotes((n) => ({ ...n, [m.id]: e.target.value }))}
                      />
                      <button type="button" className={button} disabled={busy === m.id} onClick={() => decide(m, "deny")}>
                        Deny
                      </button>
                      <button type="button" className={primary} disabled={busy === m.id} onClick={() => decide(m, "approve")}>
                        Approve
                      </button>
                    </>
                  )}
                  {q?.expires_at && state === "open" && <span className="text-aui-xs text-aui-muted">Denied {ago(q.expires_at)} if nobody answers</span>}
                  {state === "resolved" && <span className="text-aui-xs text-aui-muted">{m.state}</span>}
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </Section>
  );
}
