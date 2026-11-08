import { describe, expect, it } from "vitest";
import { responseStreams } from "./fixtures/responses";
import {
  allTools,
  answerOf,
  applyResponse,
  changes,
  emptyResponse,
  foldAll,
  foldResponse,
  type ResponseEvent,
  waiting,
  worked,
} from "./response";

const byName = {
  "single-agent": responseStreams.singleAgent,
  "parallel-agents": responseStreams.parallelAgents,
  "nested-approval": responseStreams.nestedApproval,
  stopped: responseStreams.stopped,
};
const fixture = (name: keyof typeof byName): ResponseEvent[] => structuredClone(byName[name]);

describe("one agent", () => {
  const state = foldAll(fixture("single-agent"));

  it("reads the message, the answer after the last tool, and how long it worked", () => {
    expect(state.id).toBe("msg_single");
    expect(state.agent).toBe("security-reviewer");
    expect(state.ref).toBe("slack://T1/C1/1700000000.000100");
    expect(state.status).toBe("done");
    expect(state.stopReason).toBe("end_turn");
    expect(state.costUsd).toBe(0.0048);
    expect(answerOf(state).text).toMatch(/^Not safe: any XSS can read localStorage\. Move/);
    expect(worked(state)).toEqual({ ms: 4215, agents: 0, tools: 2 });
  });

  it("assembles a streamed tool input and keeps a result's sources on its call", () => {
    const [context, search] = allTools(state);
    expect(context?.kind).toBe("context");
    expect(context?.input).toEqual({ request: "what did people say about session tokens" });
    expect(context?.status).toBe("done");
    expect(context?.elapsedMs).toBe(840);
    expect(context?.result?.sources).toEqual([
      {
        title: "#eng thread",
        url: "https://app.slack.com/client/T1/C1/thread/C1-1700000000.000100",
      },
    ]);
    expect(search?.result?.text).toMatch(/auth\.ts:12/);
  });
});

describe("two agents in parallel", () => {
  const state = foldAll(fixture("parallel-agents"));

  it("nests each agent's blocks under its call, in its own index space", () => {
    const calls = state.content.filter((b) => b.type === "tool");
    expect(calls.map((c) => c.type === "tool" && [c.agent, c.runId, c.status])).toEqual([
      ["security-reviewer", "r_sec", "done"],
      ["perf-reviewer", "r_perf", "done"],
    ]);
    const perf = state.tools.htu_r_entry_2;
    expect(
      perf?.content.filter((b) => b.type === "tool").map((b) => b.type === "tool" && b.name),
    ).toEqual(["read_diff", "run_benchmark"]);
    expect(perf?.progress).toBe("Profiling the query path");
    expect(state.tools.htu_r_perf_2?.depth).toBe(2);
    expect(state.tools.htu_r_entry_1?.result?.sources[0]?.title).toBe("PR #243 files");
  });

  it("counts agents and tools at every depth, and answers with the root's text", () => {
    expect(worked(state)).toEqual({ ms: 5815, agents: 2, tools: 3 });
    expect(answerOf(state).text).toMatch(/^## security-reviewer/);
  });
});

describe("delegation two deep, waiting on an approval", () => {
  const state = foldAll(fixture("nested-approval"));

  it("puts the question on the deepest call and on the depth-1 call, so it is never hidden", () => {
    expect(state.status).toBe("running");
    const deploy = state.tools.htu_r_deployer_1;
    expect(deploy?.depth).toBe(3);
    expect(deploy?.status).toBe("awaiting_approval");
    expect(deploy?.question?.prompt).toBe("Rotate the nightly deploy token?");
    expect(state.tools.htu_r_lead_1?.attention?.path).toEqual([
      "htu_r_lead_1",
      "htu_r_ops_2",
      "htu_r_deployer_1",
    ]);
    expect(state.tools.htu_r_ops_2?.attention?.status).toBe("awaiting_approval");
    expect(waiting(state).map((t) => t.id)).toEqual(["htu_r_deployer_1"]);
  });

  it("keeps a failed tool failed, with its error, beside the work that went on", () => {
    const logs = state.tools.htu_r_ops_1;
    expect(logs?.status).toBe("error");
    expect(logs?.result).toEqual({
      text: "403: the deploy token expired",
      sources: [],
      isError: true,
    });
    expect(state.tools.htu_r_ops_2?.progress).toBe("Waiting for approval to rotate the token");
  });

  it("has no answer yet, and says the agent is waiting when it starts to", () => {
    // Narration before a tool call is not the answer.
    expect(answerOf(state)).toEqual({ text: "" });
    const before = foldAll(fixture("nested-approval").slice(0, -1));
    expect(changes(before, state)).toEqual(["Rotate the deploy token is waiting for you"]);
  });
});

describe("a stopped run", () => {
  const state = foldAll(fixture("stopped"));

  it("ends stopped, and its open calls end stopped with an error result", () => {
    expect(state.status).toBe("stopped");
    expect(state.tools.htu_r_lead_1?.status).toBe("stopped");
    expect(state.tools.htu_r_lead_1?.result?.isError).toBe(true);
    expect(state.tools.htu_r_analyst_1?.status).toBe("stopped");
    expect(changes(emptyResponse(), state)).toContain("Stopped");
    // A stopped call's error result is not an answer.
    expect(answerOf(state)).toEqual({ text: "" });
  });
});

describe("folding", () => {
  it("event by event equals all at once, and never changes the state it was given", () => {
    const events = fixture("parallel-agents");
    let step = emptyResponse();
    const frozen = JSON.stringify(step);
    const first = foldResponse(step, events[0] as ResponseEvent);
    expect(JSON.stringify(step)).toBe(frozen);
    step = first;
    for (const e of events.slice(1)) step = foldResponse(step, e);
    expect(step).toEqual(foldAll(events));
  });

  it("a resent block start replaces its index and keeps what the call gathered", () => {
    const events = fixture("parallel-agents");
    const live = emptyResponse();
    for (const e of events.slice(0, 12)) applyResponse(live, e);
    // Resuming inside block 0 resends it from its start.
    applyResponse(live, events[1] as ResponseEvent);
    expect(live.content.filter((b) => b.type === "tool")).toHaveLength(2);
    expect(live.tools.htu_r_entry_1?.content.length).toBeGreaterThan(0);
  });

  it("stubs a block the reader may not read, so indices hold", () => {
    const state = foldAll([
      { type: "message_start", message: { id: "m" } },
      { type: "content_block_start", index: 0, content_block: { type: "redacted" } },
      { type: "content_block_start", index: 1, content_block: { type: "text", text: "Done." } },
      { type: "message_stop" },
    ]);
    expect(state.content.map((b) => b.type)).toEqual(["hidden", "text"]);
    expect(answerOf(state).text).toBe("Done.");
  });

  it("falls back to the last agent's result when the root ends with no text of its own", () => {
    const events = fixture("parallel-agents").filter(
      (e) =>
        !(
          e.type === "content_block_start" && (e.content_block as { type: string }).type === "text"
        ),
    );
    expect(answerOf(foldAll(events))).toEqual({
      text: "p95 went from 39ms to 41ms; within budget.",
      from: "perf-reviewer",
    });
  });
});
