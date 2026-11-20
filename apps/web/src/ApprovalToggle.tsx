import { defaultCopy } from "@humagen/components";
import { CheckIcon, ChevronDownIcon, FastForwardIcon, HandIcon } from "@humagen/render-web";
import { type KeyboardEvent, useEffect, useState } from "react";
import { ApiError, api, coworking, type Revision, type Thread } from "./api.ts";
import { useConfiguredProviders } from "./ModelChain.tsx";
import { POLICY } from "./models.ts";
import { Popover } from "./ui.tsx";

/**
 * How a session's gated actions are approved: ✋ a person, or ⏩ the approval agent,
 * which in "decide" also answers questions the session already settles (DES-079).
 */
export type ApprovalMode = "ask" | "auto" | "decide";

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
 * A session's approval mode under the composer, always shown: one control
 * that names the mode, ✋ Ask Me or ⏩ Run or ⏩ Run and Answer, and opens a
 * menu of the three. Each row says in one line what the approval agent does
 * in that mode (DES-057, DES-079), so the choice is read before it is made
 * and nothing about it sits in a tooltip. The menu also says when the
 * approval agent has no model to run on, so every action waits for a person,
 * and who chose the mode when it was someone else. With no session yet, as
 * on Home, the choice is held and applied once the session starts.
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
  const mode: ApprovalMode = thread
    ? approval?.mode === "auto" || approval?.mode === "decide"
      ? approval.mode
      : "ask"
    : pending;
  const someoneElse = approval && thread && approval.by !== me ? nameOf(approval.by) : "";
  const label =
    mode === "ask"
      ? words.ask
      : mode === "decide"
        ? someoneElse
          ? words.decideFor(someoneElse)
          : words.decide
        : someoneElse
          ? words.autoFor(someoneElse)
          : words.auto;
  const choose = (next: ApprovalMode) => {
    setProblem("");
    if (next === mode) return;
    if (!thread) {
      onPending?.(next);
      return;
    }
    const was = approval;
    setApproval(next === "ask" ? undefined : { mode: next, by: me, at: new Date().toISOString() });
    coworking.setApprovalMode(thread.id, next).then(
      (t) => setApproval(t.approval),
      (e) => {
        setApproval(was);
        setProblem(e instanceof ApiError && e.status === 403 ? e.message : words.failed);
      },
    );
  };
  const options: { mode: ApprovalMode; name: string; hint: string }[] = [
    { mode: "ask", name: words.ask, hint: words.askHint },
    { mode: "auto", name: words.auto, hint: words.autoHint },
    { mode: "decide", name: words.decide, hint: words.decideHint },
  ];
  // Arrow keys, Home and End move through the rows, as in every menu here.
  const move = (e: KeyboardEvent<HTMLDivElement>) => {
    const rows = [...e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="menuitemradio"]')];
    const at = rows.indexOf(document.activeElement as HTMLButtonElement);
    const to =
      e.key === "ArrowDown"
        ? (at + 1) % rows.length
        : e.key === "ArrowUp"
          ? (at - 1 + rows.length) % rows.length
          : e.key === "Home"
            ? 0
            : e.key === "End"
              ? rows.length - 1
              : -1;
    if (to < 0) return;
    e.preventDefault();
    rows[to]?.focus();
  };
  const Icon = mode === "ask" ? HandIcon : FastForwardIcon;
  return (
    <span className="inline-flex min-w-0 items-center gap-1" data-approval-mode={mode}>
      <Popover
        up
        align="right"
        popup="menu"
        label={`${words.label}: ${label}`}
        className="inline-flex h-8 min-w-8 pointer-coarse:h-11 pointer-coarse:min-w-11 shrink-0 items-center justify-center gap-1.5 rounded-aui px-2 text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-expanded:bg-aui-hover aria-expanded:text-aui-fg"
        button={
          <>
            <Icon size={16} />
            {/* The mode in words where the bar has room; the icon alone in a narrow one. */}
            <span aria-hidden className="humagen-approval-label inline-flex items-center gap-1">
              <span className="text-aui-xs">
                {mode === "ask" ? words.ask : mode === "decide" ? words.decide : words.auto}
              </span>
              <ChevronDownIcon size={12} />
            </span>
          </>
        }
      >
        {(close) => (
          <div role="menu" aria-label={words.label} onKeyDown={move} className="flex flex-col">
            {options.map((o) => {
              const RowIcon = o.mode === "ask" ? HandIcon : FastForwardIcon;
              const on = o.mode === mode;
              return (
                <button
                  key={o.mode}
                  type="button"
                  role="menuitemradio"
                  aria-checked={on}
                  // biome-ignore lint/a11y/noAutofocus: an opened menu takes focus on its current row.
                  autoFocus={on}
                  data-approval-option={o.mode}
                  onClick={() => {
                    close();
                    choose(o.mode);
                  }}
                  className="flex min-h-11 w-full items-start gap-2.5 rounded-aui px-2.5 py-2 text-left outline-none hover:bg-aui-hover focus-visible:bg-aui-hover"
                >
                  <RowIcon size={16} className="mt-0.5 shrink-0 text-aui-muted" />
                  <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span className="text-aui-sm text-aui-fg">{o.name}</span>
                    <span className="text-aui-xs text-aui-muted">{o.hint}</span>
                  </span>
                  <CheckIcon
                    size={16}
                    aria-hidden
                    className={`mt-0.5 shrink-0 text-aui-fg ${on ? "" : "invisible"}`}
                  />
                </button>
              );
            })}
            {(ready === false || someoneElse) && (
              <p className="mt-1 border-[var(--humagen-rule)] border-t px-2.5 pt-2 pb-1 text-aui-xs text-aui-muted">
                {[someoneElse && words.chosenBy(someoneElse), ready === false && words.waiting]
                  .filter(Boolean)
                  .join(" ")}
              </p>
            )}
          </div>
        )}
      </Popover>
      {problem && (
        <span role="alert" className="text-aui-2xs text-aui-bad">
          {problem}
        </span>
      )}
    </span>
  );
}
