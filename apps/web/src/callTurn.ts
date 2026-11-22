import {
  isAwaiting,
  type ResponseBlock,
  type ResponseTool,
  type Task,
  type Thought,
  type Turn,
} from "@humagen/core";
import { brandShown } from "./branding.ts";

const taskStatus = (tool: ResponseTool): Task["status"] =>
  tool.status === "done"
    ? "complete"
    : tool.status === "error"
      ? "error"
      : tool.status === "denied"
        ? "denied"
        : tool.status === "running"
          ? "in_progress"
          : isAwaiting(tool.status)
            ? "waiting"
            : "pending";

/**
 * How the stream is drawn. Whether the call failed, stopped or was denied is
 * said once, on its line; the stream under it shows what happened and does
 * not say it again with a failure of its own.
 */
const turnStatus = (tool: ResponseTool): Turn["status"] =>
  tool.status === "running"
    ? "working"
    : isAwaiting(tool.status)
      ? "waiting"
      : tool.status === "queued"
        ? "queued"
        : "done";

const input = (value: Record<string, unknown>) =>
  Object.keys(value).length ? JSON.stringify(value) : undefined;

/**
 * A called agent's own record as a turn, so an answer can draw it with the
 * same parts as its own stream: the agent's thinking, its words, each step
 * with what it was given and returned, and the agents it called in turn, all
 * in the order they came. Blocks the reader may not see are left out; the
 * caller says how many.
 *
 * An agent that wrote no words of its own has its call's result as its
 * words, so what it came back with is never missing.
 *
 * `omit` leaves a step out: one the caller draws itself, as a question that
 * waits on a person is drawn once, with its controls.
 */
export function turnOfCall(call: ResponseTool, omit?: (step: ResponseTool) => boolean): Turn {
  let text = "";
  const tasks: Task[] = [];
  const thoughts: Thought[] = [];
  const blocks: ResponseBlock[] = call.content;
  for (const b of blocks) {
    if (b.type === "text") {
      if (b.text.trim()) text += (text ? "\n\n" : "") + b.text.trim();
      continue;
    }
    if (b.type === "thinking") {
      if (b.text.trim()) thoughts.push({ at: text.length, after: tasks.length, text: b.text });
      continue;
    }
    if (b.type !== "tool" || b.hidden || omit?.(b)) continue;
    if (b.kind === "agent") {
      const name = b.agent ?? b.name;
      tasks.push({
        id: b.id,
        title: name,
        tool: "call_agent",
        status: taskStatus(b),
        input: input({ agent: name, ...(b.title ? { title: b.title } : {}) }),
        output: b.result?.text,
        durationMs: b.elapsedMs,
        subagent: { name, runId: b.runId, summary: b.title, durationMs: b.elapsedMs },
        at: text.length,
      });
      continue;
    }
    tasks.push({
      id: b.id,
      title: b.title || b.name,
      tool: b.name,
      status: taskStatus(b),
      input: input(b.input),
      output: b.result?.text,
      durationMs: b.elapsedMs,
      at: text.length,
    });
  }
  const said = blocks.some((b) => b.type === "text" && b.text.trim());
  if (!said && call.result && call.status !== "stopped") text = call.result.text.trim();
  return {
    id: call.runId ?? call.id,
    // The name its mark is drawn from; the message shows it by its shown name (AgentNameContext).
    agent: { name: brandShown(call.agent ?? call.name) },
    status: turnStatus(call),
    text,
    thoughts,
    tasks,
    citations: [],
    interrupts: [],
    notices: [],
    durationMs: call.elapsedMs,
  };
}
