import type { ItemActions, TurnItem } from "@humagen/render-web";
import type { Run } from "./adapter.ts";
import { CopyAs } from "./CopyAs.tsx";
import type { Item } from "./copyas.ts";

/**
 * An item inside a turn as something "Copy as" understands. A tool call keeps
 * its name and whatever came back. A call to another agent made inside a turn
 * has no run of its own on the platform, so it is copied as the call it was:
 * its name and its answer.
 */
export function itemOf(run: Run, item: TurnItem): Item | null {
  if (item.kind === "answer")
    return { kind: "answer", run: { ...run, answer: item.turn.text || run.answer } };
  const task = item.task;
  if (!task) return null;
  const failed = task.status === "error";
  const ended = task.status === "complete" || failed;
  return {
    kind: "tool",
    tool: {
      name: task.tool ?? task.title,
      run: run.id,
      agent: run.agent,
      ...(failed
        ? { error: task.output || "The call failed without saying why." }
        : ended
          ? { result: task.output ?? "" }
          : {}),
    },
  };
}

/**
 * "Copy as" on the items humagen draws in a turn: each tool call and each call
 * to another agent. Pass the result to `AgentMessage` as `itemActions`.
 *
 * The answer gets no menu: null leaves humagen's own copy button, which puts
 * the answer's Markdown on the clipboard in one click, as a person's message
 * does. Return null itself, never an element that renders nothing, or the
 * answer is left with no way to copy it.
 */
export function itemActions(run: Run): ItemActions {
  return (item) => {
    if (item.kind === "answer") return null;
    const found = itemOf(run, item);
    if (!found) return null;
    return <CopyAs compact align="right" label="Copy as" testId="item-copy" item={found} />;
  };
}
