import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import {
  foldHumanActivity,
  humanActivity,
  MAX_HUMAN_DETAIL,
  MAX_HUMAN_SUMMARY,
  type ResponseBlock,
  type ResponseState,
  type ResponseTool,
} from "@humagen/core";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type {
  ConversationSandboxes,
  SandboxCommandResult,
} from "../../../packages/client/src/generated/types.ts";
import type { Run } from "./adapter.ts";
import { ApiError } from "./api.ts";
import { useHumanActivity } from "./humanActivity.tsx";
import { type TerminalApi, WorkbenchTerminal } from "./WorkbenchTerminal.tsx";
import { WORKBENCH_TABS, type WorkbenchSession, workbenchTabs } from "./workbench.ts";
import {
  activityOf,
  agentCommands,
  commandLog,
  fromResult,
  outputTail,
  personCommands,
} from "./workbenchTerminal.ts";

const words = defaultCopy.workbenchTerminal;
const thread = "s_terminal";

afterEach(() => {
  cleanup();
  humanActivity.clear(thread);
  personCommands.clear(thread);
});

let minted = 0;
function tool(name: string, input: Record<string, unknown>, more: Partial<ResponseTool> = {}) {
  return {
    type: "tool",
    id: `tool-${++minted}`,
    name,
    kind: "tool",
    input,
    depth: 0,
    status: "done",
    content: [],
    ...more,
  } as ResponseTool;
}
const result = (text: string, isError = false) => ({ text, sources: [], isError });
function response(content: ResponseBlock[], more: Partial<ResponseState> = {}): ResponseState {
  return { status: "done", content, tools: {}, partial: {}, agent: "lead", ...more };
}
const run = (id: string, created_at: string, status: Run["status"] = "completed") =>
  ({ id, created_at, status, thread }) as Run;

function boxes(session: Partial<ConversationSandboxes["session"]> = {}): ConversationSandboxes {
  return {
    session: {
      isolated: true,
      files: 0,
      bytes: 0,
      retention_days: 7,
      writable: true,
      commands: true,
      ...session,
    },
    runs: [],
    more: false,
  };
}
const ended = (more: Partial<SandboxCommandResult> = {}): SandboxCommandResult => ({
  command: "pnpm test",
  exit_code: 1,
  timed_out: false,
  output: "FAIL src/route.test.ts\n1 failed\n",
  truncated: false,
  duration_ms: 1200,
  files: [],
  conflicts: [],
  ...more,
});

/** The tab as the pane mounts it, with the list above the composer beside it. */
function Bench({
  api,
  responses,
  runs = [],
  canPost = true,
  selection,
  onOpen,
}: {
  api: TerminalApi;
  responses: ResponseState[] | undefined;
  runs?: Run[];
  canPost?: boolean;
  selection?: string;
  onOpen?: (id: string) => void;
}) {
  const { record, records } = useHumanActivity(thread);
  return (
    <>
      <WorkbenchTerminal
        thread={thread}
        runs={runs}
        responses={responses}
        summary={undefined}
        canPost={canPost}
        selection={selection}
        onSelection={() => {}}
        openTab={(id) => onOpen?.(id)}
        record={record}
        expanded={false}
        api={api}
      />
      <ul data-testid="by-hand">
        {records.map((r) => (
          <li key={r.id}>{r.summary}</li>
        ))}
      </ul>
    </>
  );
}

function standIn(session?: Partial<ConversationSandboxes["session"]>) {
  const ran: string[] = [];
  let answer: (command: string) => Promise<SandboxCommandResult> = async (command) =>
    ended({ command });
  const api: TerminalApi = {
    sandboxes: async () => boxes(session),
    run: (_thread, command) => {
      ran.push(command);
      return answer(command);
    },
  };
  return {
    api,
    ran,
    answers: (next: typeof answer) => {
      answer = next;
    },
  };
}

test("the agents' commands are read from their answers, at every depth", () => {
  const nested = tool("shell", { command: "ls" }, { result: result("exit 0\na.txt\n"), depth: 1 });
  const commands = agentCommands([
    response([
      { type: "text", text: "Running the tests.", citations: [] },
      tool("file_read", { path: "a.txt" }, { result: result("hello") }),
      tool(
        "shell",
        { command: "pnpm test" },
        { result: result("exit 1\n[earlier output cut]\nFAIL a.test.ts\n"), elapsedMs: 900 },
      ),
      tool("call_agent", {}, { kind: "agent", agent: "reviewer", content: [nested] }),
      tool("shell", { command: "sleep 5" }, { status: "running" }),
      tool("shell", { command: "rm -rf x" }, { status: "denied" }),
      tool("shell", { command: "boom" }, { result: result("the command did not run", true) }),
      // An imported session's shell: an argv, and output with no exit line.
      tool("exec_command", { cmd: ["bash", "-lc", "git status"] }, { result: result("clean") }),
    ]),
  ]);
  assert.deepEqual(
    commands.map((c) => [c.agent, c.command, c.state, c.exit, c.output, !!c.cut]),
    [
      ["lead", "pnpm test", "exited", 1, "FAIL a.test.ts\n", true],
      ["reviewer", "ls", "exited", 0, "a.txt\n", false],
      ["lead", "sleep 5", "running", undefined, "", false],
      ["lead", "rm -rf x", "denied", undefined, "", false],
      ["lead", "boom", "failed", undefined, "the command did not run", false],
      ["lead", "bash -lc git status", "exited", undefined, "clean", false],
    ],
  );
  assert.equal(commands[0]?.elapsedMs, 900);
});

test("a reader's commands sit among the runs by when they ran", () => {
  const first = response([tool("shell", { command: "one" }, { result: result("exit 0\n") })], {
    rootRun: "r1",
  });
  const second = response([tool("shell", { command: "three" }, { result: result("exit 0\n") })], {
    rootRun: "r2",
  });
  const mine = fromResult("me", "2026-11-19T10:05:00.000Z", ended({ command: "two" }));
  const answers = [
    { id: "r1", created_at: "2026-11-19T10:00:00.000Z" },
    // A time with another zone's offset still sorts as the moment it is.
    { id: "r2", created_at: "2026-11-19T05:10:00.000-05:00" },
  ];
  const order = (log: { command: string }[]) => log.map((c) => c.command);
  assert.deepEqual(order(commandLog([first, second], [mine], answers)), ["one", "two", "three"]);
  // A response that does not say its run is placed by its turn, newest last.
  const unnamed = [first, second].map((r) => ({ ...r, rootRun: undefined }));
  assert.deepEqual(order(commandLog(unnamed, [mine], answers)), ["one", "two", "three"]);
  assert.deepEqual(order(commandLog([unnamed[1] as ResponseState], [mine], answers)), [
    "two",
    "three",
  ]);
});

test("what goes with the next message keeps the outcome and the end of the output, inside the limits", () => {
  const long = Array.from({ length: 400 }, (_, i) => `line ${i}`).join("\n");
  const done = fromResult(
    "me",
    "2026-11-19T10:05:00.000Z",
    ended({
      command: `pnpm test ${"x".repeat(400)}`,
      output: `${long}\nFAIL at the end\n`,
      files: [
        { path: "out/report.txt", size: 3 },
        { path: "old.txt", size: 0, removed: true },
      ],
      conflicts: ["a.txt changed"],
    }),
  );
  const activity = activityOf(done, words);
  assert.ok(activity.summary.length <= MAX_HUMAN_SUMMARY, `${activity.summary.length}`);
  assert.ok(activity.summary.endsWith(": exit 1"), activity.summary);
  assert.ok(activity.detail.length <= MAX_HUMAN_DETAIL, `${activity.detail.length}`);
  assert.ok(activity.detail.startsWith("Changed: out/report.txt, old.txt (removed)\n"));
  assert.ok(activity.detail.includes(words.notKept(1)));
  assert.ok(activity.detail.endsWith("FAIL at the end"), activity.detail.slice(-40));
  assert.equal(outputTail("short\n", 100), "short");
  const stopped = activityOf(
    fromResult(
      "t",
      "2026-11-19T10:06:00.000Z",
      ended({ timed_out: true, exit_code: -1, output: "" }),
    ),
    words,
  );
  assert.equal(stopped.summary, `Ran pnpm test: ${words.timedOut}`);
  assert.equal(stopped.detail, words.noOutput);
});

test("the tab is in the registry, and shows once a session ran a command or has a sandbox", () => {
  const entry = WORKBENCH_TABS.find((t) => t.id === "terminal");
  assert.ok(entry?.fill, "the terminal lays out its own height");
  const session: WorkbenchSession = {
    thread,
    runs: [],
    responses: undefined,
    summary: undefined,
    canPost: true,
  };
  const ids = (s: WorkbenchSession) => workbenchTabs(s).map((t) => t.id);
  assert.ok(!ids(session).includes("terminal"), "unknown is no");
  assert.ok(!ids({ ...session, responses: [response([])] }).includes("terminal"));
  assert.ok(
    ids({
      ...session,
      responses: [response([tool("shell", { command: "ls" })])],
    }).includes("terminal"),
  );
});

test("the log shows what the agents ran, is reachable from the keyboard, and offers no command line where the sandbox has none", async () => {
  const { api, ran } = standIn({ commands: false });
  const many = Array.from({ length: 60 }, (_, i) => `row ${i}`).join("\n");
  const opened: string[] = [];
  const view = render(
    <Bench
      api={api}
      onOpen={(id) => opened.push(id)}
      responses={[
        response([
          tool("shell", { command: "pnpm test" }, { result: result(`exit 1\n${many}\n`) }),
          tool("shell", { command: "true" }, { result: result("exit 0\n"), hidden: false }),
          tool("shell", {}, { hidden: true }),
        ]),
      ]}
    />,
  );
  const log = screen.getByRole("log", { name: words.log });
  // A region that scrolls takes focus, and holds none: nothing in it stops Tab.
  assert.equal(log.getAttribute("tabindex"), "0");
  assert.equal(log.getAttribute("aria-live"), "off");
  const entries = within(log).getAllByRole("listitem");
  assert.equal(entries.length, 3);
  assert.equal(
    entries[0]?.getAttribute("aria-label"),
    words.entry("Lead", "pnpm test", words.state.exited(1)),
  );
  assert.ok(within(entries[0] as HTMLElement).getByText(words.state.exited(1)));
  // Long output is folded to its end, and opens.
  const output = within(entries[0] as HTMLElement).getByTestId("terminal-output");
  assert.ok(!output.textContent?.includes("row 0\n"));
  assert.ok(output.textContent?.endsWith("row 59"));
  const more = within(entries[0] as HTMLElement).getByRole("button", { name: words.showAll(60) });
  assert.equal(more.getAttribute("aria-expanded"), "false");
  fireEvent.click(more);
  assert.ok(
    within(entries[0] as HTMLElement)
      .getByTestId("terminal-output")
      .textContent?.startsWith("row 0\n"),
  );
  assert.ok(within(entries[1] as HTMLElement).getByText(words.noOutput));
  assert.equal(entries[2]?.getAttribute("aria-label"), words.hidden);
  await waitFor(() => assert.equal(view.container.querySelector("form"), null));
  assert.equal(screen.queryByRole("textbox", { name: words.input }), null);
  assert.deepEqual(ran, []);
});

test("with nothing run and no command line, the tab is a title and one action", async () => {
  const { api } = standIn({ commands: false });
  const opened: string[] = [];
  render(<Bench api={api} responses={[]} onOpen={(id) => opened.push(id)} />);
  assert.ok(screen.getByRole("heading", { name: words.empty }));
  fireEvent.click(screen.getByRole("button", { name: words.toDetails }));
  assert.deepEqual(opened, ["details"]);
  cleanup();
  // While the answers are read, the tab shows rows, not a claim of nothing.
  render(<Bench api={api} responses={undefined} />);
  assert.equal(screen.queryByRole("heading", { name: words.empty }), null);
  assert.ok(screen.getByTestId("skeleton"));
});

test("a command the reader runs is shown, recorded for their next message, and sent to no agent", async () => {
  const { api, ran } = standIn();
  render(<Bench api={api} responses={[]} />);
  const input = (await screen.findByRole("textbox", { name: words.input })) as HTMLInputElement;
  assert.ok(input.getAttribute("aria-describedby"));
  assert.ok(screen.getByText(words.fresh));
  input.focus();
  fireEvent.change(input, { target: { value: "  pnpm test  " } });
  fireEvent.submit(input.closest("form") as HTMLFormElement);
  const log = screen.getByRole("log", { name: words.log });
  const entry = await within(log).findByText(words.state.exited(1));
  const item = entry.closest("li") as HTMLElement;
  assert.equal(item.dataset.by, "person");
  assert.ok(within(item).getByText(words.you));
  assert.ok(within(item).getByTestId("terminal-output").textContent?.includes("1 failed"));
  assert.deepEqual(ran, ["pnpm test"]);
  // One record, with the outcome, above the composer; the fold carries it.
  const listed = within(screen.getByTestId("by-hand")).getAllByRole("listitem");
  assert.deepEqual(
    listed.map((l) => l.textContent),
    ["Ran pnpm test: exit 1"],
  );
  const records = humanActivity.list(thread);
  assert.equal(records[0]?.source, "terminal");
  assert.ok(records[0]?.detail?.includes("FAIL src/route.test.ts"));
  const folded = foldHumanActivity("Why did it fail?", records);
  assert.ok(folded.text.startsWith("Why did it fail?\n\n[Done by hand since my last message: 1]"));
  assert.ok(folded.text.includes("Ran pnpm test: exit 1"));
  // The outcome is said once, for a reader who cannot see the log.
  assert.equal(screen.getByRole("status").textContent, words.ended(words.state.exited(1), 2));
  // The line is ready for the next command, and Up brings the last one back.
  assert.equal(input.value, "");
  assert.equal(document.activeElement, input);
  fireEvent.keyDown(input, { key: "ArrowUp" });
  assert.equal(input.value, "pnpm test");
  fireEvent.keyDown(input, { key: "ArrowDown" });
  assert.equal(input.value, "");
});

test("a command that did not run is said so and is not recorded", async () => {
  const stand = standIn();
  stand.answers(async () => {
    throw new ApiError(409, "a command is already running in this conversation");
  });
  render(<Bench api={stand.api} responses={[]} />);
  const input = await screen.findByRole("textbox", { name: words.input });
  fireEvent.change(input, { target: { value: "ls" } });
  fireEvent.submit(input.closest("form") as HTMLFormElement);
  const alert = await screen.findByRole("alert");
  assert.equal(alert.textContent, "a command is already running in this conversation");
  assert.ok(screen.getByText(words.state.failed));
  assert.equal(humanActivity.list(thread).length, 0);
});

test("the command line waits while the agents work, and a reader who may not post has none", async () => {
  const busy = standIn({ writable: false, reason: "workspace files: conflict: wait" });
  const view = render(
    <Bench
      api={busy.api}
      responses={[]}
      runs={[run("r1", "2026-11-19T10:00:00.000Z", "running")]}
    />,
  );
  const input = (await screen.findByRole("textbox", { name: words.input })) as HTMLInputElement;
  assert.equal(input.disabled, true);
  assert.ok(screen.getByText(words.busy));
  assert.equal(screen.getByRole("log").getAttribute("aria-busy"), "true");
  fireEvent.submit(input.closest("form") as HTMLFormElement);
  assert.deepEqual(busy.ran, []);
  view.unmount();
  const reader = standIn();
  let asked = 0;
  const api: TerminalApi = {
    ...reader.api,
    sandboxes: () => {
      asked++;
      return reader.api.sandboxes(thread);
    },
  };
  render(<Bench api={api} responses={[]} canPost={false} />);
  await new Promise((r) => setTimeout(r, 0));
  assert.equal(screen.queryByRole("textbox", { name: words.input }), null);
  assert.equal(asked, 0);
});

test("the address's selection marks its command", () => {
  const { api } = standIn({ commands: false });
  const picked = tool("shell", { command: "second" }, { result: result("exit 0\n") });
  render(
    <Bench
      api={api}
      selection={picked.id}
      responses={[
        response([tool("shell", { command: "first" }, { result: result("exit 0\n") }), picked]),
      ]}
    />,
  );
  const entries = within(screen.getByRole("log")).getAllByRole("listitem");
  assert.equal(entries[0]?.getAttribute("aria-current"), null);
  assert.equal(entries[1]?.getAttribute("aria-current"), "true");
});
