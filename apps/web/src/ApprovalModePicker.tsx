import { defaultCopy } from "@humagen/components";
import { CheckIcon } from "@humagen/render-web";
import { useEffect, useState } from "react";
import { ApiError, coworking, type Thread } from "./api.ts";
import { Popover } from "./ui.tsx";

type Mode = "ask" | "auto";

/**
 * A conversation's approval mode, as a text control under the composer:
 * Default asks a person; Approve for Me approves gated actions as the person
 * who chose it, within policy. Who chose it is named when it is someone else.
 */
export function ApprovalModePicker({
  thread,
  me,
  nameOf = (s) => s,
}: {
  thread: Pick<Thread, "id" | "approval">;
  /** The reader's handle, to tell their own choice from someone else's. */
  me: string;
  nameOf?: (handle: string) => string;
}) {
  const words = defaultCopy.approvals;
  const [approval, setApproval] = useState(thread.approval);
  const [problem, setProblem] = useState("");
  useEffect(() => setApproval(thread.approval), [thread.approval]);
  const mode: Mode = approval?.mode === "auto" ? "auto" : "ask";
  const label =
    mode === "ask"
      ? words.ask
      : approval && approval.by !== me
        ? words.autoFor(nameOf(approval.by))
        : words.auto;
  const choose = (next: Mode, close: () => void) => {
    close();
    if (next === mode) return;
    const was = approval;
    setProblem("");
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
  return (
    <span className="inline-flex min-w-0 items-center gap-1">
      <Popover
        narrow
        up
        align="right"
        label={`${words.label}: ${label}`}
        className="inline-flex min-h-8 pointer-coarse:min-h-11 max-w-[11rem] items-center gap-1 rounded-aui px-1.5 text-aui-xs text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-expanded:text-aui-fg"
        button={
          <>
            <span className="truncate" data-approval-mode={mode}>
              {label}
            </span>
            <span aria-hidden>▾</span>
          </>
        }
      >
        {(close) => (
          <div role="menu" className="flex w-72 max-w-[80vw] flex-col">
            {(
              [
                ["ask", words.ask, words.askHint],
                ["auto", words.auto, words.autoHint],
              ] as const
            ).map(([value, name, hint]) => (
              <button
                key={value}
                type="button"
                role="menuitemradio"
                aria-checked={mode === value}
                onClick={() => choose(value, close)}
                className="flex items-start gap-2 rounded-aui px-2.5 py-2 text-left hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
              >
                <span className="mt-0.5 size-3.5 shrink-0">
                  {mode === value && <CheckIcon size={14} />}
                </span>
                <span className="flex min-w-0 flex-col gap-0.5">
                  <span className="text-aui-sm text-aui-fg">{name}</span>
                  <span className="text-aui-xs text-aui-muted">{hint}</span>
                </span>
              </button>
            ))}
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
