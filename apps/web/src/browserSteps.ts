/**
 * What an agent did in a browser, read from its tool calls: one step per
 * call, in the order they were made, with the page each left it on. Pure; no
 * React. The thread's card and the Browser tab both read these.
 *
 * A browser is an MCP tool provider (docs/guides/browser.md), so its tools
 * arrive named `<provider>__browser_navigate`, or `mcp__<provider>__...` from
 * a program that speaks MCP itself. The provider is where the browser runs.
 * A screenshot's pixels are not in a run's events: a tool's result reaches
 * the console as text only, so a step carries the page's address and title
 * and no image.
 */

import { defaultCopy } from "@humagen/components";
import { allTools, type ResponseState, type Task } from "@humagen/core";

const words = defaultCopy.workbenchBrowser.step;

/** A tool call, from either of the two shapes the console holds one in. */
export interface BrowserCall {
  id: string;
  name: string;
  input: Record<string, unknown>;
  /** The result's text, or the failure's. */
  output?: string;
  failed: boolean;
  running: boolean;
}

export type BrowserStepKind =
  | "navigate"
  | "back"
  | "click"
  | "type"
  | "fill"
  | "press"
  | "screenshot"
  | "snapshot"
  | "tabs"
  | "wait"
  | "resize"
  | "dialog"
  | "close"
  | "computer"
  | "other";

export interface BrowserStep {
  id: string;
  kind: BrowserStepKind;
  /** What the agent did, in words. */
  label: string;
  /** The tool provider the browser is reached through, when the name says. */
  provider?: string;
  /** The page it left the browser on, when the result says. */
  url?: string;
  title?: string;
  failed: boolean;
  running: boolean;
  /** The first line of why it failed. */
  error?: string;
}

const KINDS: Record<string, BrowserStepKind> = {
  browser_navigate: "navigate",
  browser_navigate_back: "back",
  browser_click: "click",
  browser_type: "type",
  browser_fill_form: "fill",
  browser_press_key: "press",
  browser_take_screenshot: "screenshot",
  browser_snapshot: "snapshot",
  browser_tabs: "tabs",
  browser_wait_for: "wait",
  browser_resize: "resize",
  browser_handle_dialog: "dialog",
  browser_close: "close",
};

/** A browser tool's provider and bare name, or nothing for any other tool. */
export function browserTool(name: string): { provider?: string; tool: string } | undefined {
  const m = /^(?:mcp__)?([a-z][a-z0-9-]*)__(.+)$/.exec(name);
  const tool = m?.[2] ?? name;
  if (tool === "computer" || tool.startsWith("browser_")) return { provider: m?.[1], tool };
  return undefined;
}

const text = (v: unknown): string | undefined =>
  typeof v === "string" && v.trim() ? v.trim() : undefined;

/** The page a Playwright MCP result says the browser is on. */
export function pageOf(output: string | undefined): { url?: string; title?: string } {
  if (!output) return {};
  return {
    url: /^- Page URL: (.+)$/m.exec(output)?.[1]?.trim(),
    title: /^- Page Title: (.+)$/m.exec(output)?.[1]?.trim(),
  };
}

function label(kind: BrowserStepKind, tool: string, input: Record<string, unknown>): string {
  const what = text(input.element) ?? words.thePage;
  switch (kind) {
    case "navigate":
      return words.navigate(text(input.url) ?? words.thePage);
    case "back":
      return words.back;
    case "click":
      return words.click(what);
    case "type":
      return words.type(what);
    case "fill":
      return words.fill;
    case "press":
      return words.press(text(input.key) ?? "");
    case "screenshot":
      return words.screenshot;
    case "snapshot":
      return words.snapshot;
    case "tabs":
      return words.tabs;
    case "wait":
      return words.wait;
    case "resize":
      return words.resize;
    case "dialog":
      return words.dialog;
    case "close":
      return words.close;
    case "computer":
      return words.computer(text(input.action) ?? tool);
    default:
      return words.other(tool.replace(/^browser_/, "").replaceAll("_", " "));
  }
}

/**
 * The steps among some tool calls, oldest first. A step that does not say
 * where it left the browser is on the page the step before it left.
 */
export function browserSteps(calls: BrowserCall[]): BrowserStep[] {
  const out: BrowserStep[] = [];
  let url: string | undefined;
  let title: string | undefined;
  for (const call of calls) {
    const named = browserTool(call.name);
    if (!named) continue;
    const kind: BrowserStepKind =
      named.tool === "computer" ? "computer" : (KINDS[named.tool] ?? "other");
    const page = call.failed ? {} : pageOf(call.output);
    if (page.url && page.url !== url) title = undefined;
    url =
      page.url ?? (kind === "navigate" && !call.failed ? text(call.input.url) : undefined) ?? url;
    title = page.title ?? title;
    out.push({
      id: call.id,
      kind,
      label: label(kind, named.tool, call.input),
      provider: named.provider,
      url,
      title,
      failed: call.failed,
      running: call.running,
      error: call.failed ? call.output?.trim().split("\n")[0]?.slice(0, 300) : undefined,
    });
  }
  return out;
}

/** Every tool call of a session's answers, oldest first. */
export function callsOfResponses(responses: ResponseState[]): BrowserCall[] {
  return responses.flatMap((response) =>
    allTools(response).map((t) => ({
      id: t.id,
      name: t.name,
      input: t.input ?? {},
      output: t.result?.text,
      failed: t.status === "error" || !!t.result?.isError,
      running: t.status === "running" || t.status === "queued",
    })),
  );
}

function parsed(input: string | undefined): Record<string, unknown> {
  try {
    const v: unknown = JSON.parse(input ?? "");
    return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

/** The tool calls of one answer in the thread, from its steps. */
export function callsOfTasks(tasks: Task[]): BrowserCall[] {
  return tasks.flatMap((t) =>
    t.tool
      ? [
          {
            id: t.id,
            name: t.tool,
            input: parsed(t.input),
            output: t.output,
            failed: t.status === "error",
            running: t.status === "in_progress",
          },
        ]
      : [],
  );
}

/** Where the steps left the browser, and through which provider. */
export function browserNow(steps: BrowserStep[]): {
  url?: string;
  title?: string;
  provider?: string;
  running: boolean;
} {
  const last = steps[steps.length - 1];
  return {
    url: last?.url,
    title: last?.title,
    provider: steps.findLast((s) => s.provider)?.provider,
    running: steps.some((s) => s.running),
  };
}
