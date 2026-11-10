import { defaultCopy } from "@humagen/components";
import { FastForwardIcon, HandIcon } from "@humagen/render-web";
import { useEffect, useState } from "react";
import { ApiError, api, coworking, type Revision, type Thread } from "./api.ts";
import { useConfiguredProviders } from "./ModelChain.tsx";
import { POLICY } from "./models.ts";

/** How a session's gated actions are approved: ✋ a person, or ⏩ the approval agent. */
export type ApprovalMode = "ask" | "auto";

/** The built-in that decides gated actions in a session set to Run. */
const APPROVER = "approver";

let approverRead: Promise<Revision | null> | null = null;

/** Forget the approval agent read, so the next toggle reads it again. */
export function forgetApprover() {
  approverRead = null;
}

/**
 * Whether the approval agent can run: one model in its chain is on a provider
 * an administrator enabled, or it runs on a workspace model policy. Null while
 * it is read. An agent that cannot be read counts as one that cannot run.
 */
export function useApproverReady(): boolean | null {
  const configured = useConfiguredProviders();
  const [approver, setApprover] = useState<Revision | null | undefined>(undefined);
  useEffect(() => {
    let live = true;
    approverRead ??= api
      .agents()
      .then((all) => all.find((a) => a.spec.name === APPROVER) ?? null)
      .catch(() => {
        approverRead = null;
        return null;
      });
    approverRead.then((a) => live && setApprover(a));
    return () => {
      live = false;
    };
  }, []);
  if (configured === null || approver === undefined) return null;
  if (approver === null) return false;
  const chain = [
    { provider: approver.spec.provider, model: approver.spec.model },
    ...(approver.spec.fallbacks ?? []),
  ];
  return chain.some((m) => m.provider === POLICY || configured.some((c) => c.name === m.provider));
}

/**
 * A session's approval mode under the composer, always shown: a two-state
 * icon button, ✋ Ask Me (every gated action waits for a person) or ⏩ Run
 * (the approval agent decides each, handing a person anything it is unsure
 * of). Its tooltip says what the mode does, and when the approval agent has
 * no model to run on, that every action waits for a person. With no session
 * yet, as on Home, the choice is held and applied once the session starts.
 */
export function ApprovalToggle({
  thread,
  me,
  nameOf = (s) => s,
  pending = "ask",
  onPending,
}: {
  /** The session, once there is one. */
  thread?: Pick<Thread, "id" | "approval">;
  /** The reader's handle, to tell their own choice from someone else's. */
  me: string;
  nameOf?: (handle: string) => string;
  /** The mode held for a session not started yet. */
  pending?: ApprovalMode;
  onPending?: (mode: ApprovalMode) => void;
}) {
  const words = defaultCopy.approvals;
  const ready = useApproverReady();
  const [approval, setApproval] = useState(thread?.approval);
  const [problem, setProblem] = useState("");
  useEffect(() => setApproval(thread?.approval), [thread?.approval]);
  const mode: ApprovalMode = thread ? (approval?.mode === "auto" ? "auto" : "ask") : pending;
  const label =
    mode === "ask"
      ? words.ask
      : approval && thread && approval.by !== me
        ? words.autoFor(nameOf(approval.by))
        : words.auto;
  const hint = mode === "ask" ? words.askHint : ready === false ? words.waiting : words.autoHint;
  const toggle = () => {
    const next: ApprovalMode = mode === "ask" ? "auto" : "ask";
    setProblem("");
    if (!thread) {
      onPending?.(next);
      return;
    }
    const was = approval;
    setApproval(
      next === "auto" ? { mode: "auto", by: me, at: new Date().toISOString() } : undefined,
    );
    coworking.setApprovalMode(thread.id, next).then(
      (t) => setApproval(t.approval),
      (e) => {
        setApproval(was);
        setProblem(e instanceof ApiError && e.status === 403 ? e.message : words.failed);
      },
    );
  };
  const Icon = mode === "ask" ? HandIcon : FastForwardIcon;
  return (
    <span className="inline-flex min-w-0 items-center gap-1">
      <button
        type="button"
        onClick={toggle}
        aria-label={`${words.label}: ${label}`}
        title={`${label}. ${hint}`}
        data-approval-mode={mode}
        className="inline-flex size-8 pointer-coarse:size-11 shrink-0 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
      >
        <Icon size={16} />
      </button>
      {problem && (
        <span role="alert" className="text-aui-2xs text-aui-bad">
          {problem}
        </span>
      )}
    </span>
  );
}
