import assert from "node:assert/strict";
import { test } from "node:test";
import type { UnknownEvent } from "@humagen/client/types";
import { adapter, type RunEvent } from "./run.ts";

function ev(type: string, data: unknown = {}, seq = 1): RunEvent {
  return {
    id: `e${seq}`,
    source: "humagen:acme",
    type,
    channel: "humagen://acme",
    time: "2026-10-09T00:00:00Z",
    hops: 0,
    tenant: "acme",
    run: "r1",
    seq,
    agent: "lead",
    version: "1.0.0",
    data,
  } as RunEvent;
}

test("an event of a type this build does not know says nothing to the turn", () => {
  const to = adapter();
  const unknown: UnknownEvent = { ...ev("run.accepted.v1"), type: "run.delta.audio.v1", data: { bytes: 3 } };
  assert.deepEqual(to(unknown as RunEvent), []);
  // Opt-in families are known and say nothing either.
  assert.deepEqual(to(ev("run.usage.v1", { accounting_id: "a1" })), []);
  // Nor does a known name at a version this build does not know.
  assert.deepEqual(to(ev("run.completed.v2", { text: "Done" })), []);
});

test("a streamed answer is not repeated on completion", () => {
  const to = adapter();
  const out = [
    ev("run.accepted.v1"),
    ev("run.delta.text.v1", { text: "Hello" }),
    ev("run.completed.v1", { text: "Hello" }),
  ].flatMap(to);
  assert.deepEqual(
    out.map((e) => e.type),
    ["turn.started", "activity", "text.delta", "turn.completed"],
  );
});

test("an answer that never streamed arrives with completion", () => {
  const to = adapter();
  const out = [ev("run.accepted.v1"), ev("run.completed.v1", { text: "Done" })].flatMap(to);
  assert.deepEqual(out.slice(1), [{ type: "text.delta", text: "Done" }, { type: "turn.completed" }]);
});

test("a stopped run ends the turn as stopped, keeping its partial answer", () => {
  const to = adapter();
  const out = [ev("run.accepted.v1"), ev("run.stopped.v1", { text: "Half", reason: "Stopped by ada", by: "ada" })].flatMap(to);
  assert.deepEqual(out.slice(1), [{ type: "text.delta", text: "Half" }, { type: "turn.stopped" }, { type: "stop.updated", stop: "confirmed" }]);
  const streamed = adapter();
  const again = [ev("run.delta.text.v1", { text: "Half" }), ev("run.stopped.v1", { text: "Half" })].flatMap(streamed);
  assert.deepEqual(again.map((e) => e.type).slice(-2), ["turn.stopped", "stop.updated"]);
  assert.equal(again.filter((e) => e.type === "text.delta").length, 1);
});

test("a retried attempt is not a failed turn", () => {
  const to = adapter();
  assert.deepEqual(to(ev("run.failed.v1", { error: "timeout", terminal: false })), []);
  const [last] = to(ev("run.failed.v1", { error: "timeout", terminal: true }));
  assert.equal(last?.type, "turn.failed");
});

test("a tool call keeps its name when the done event omits it", () => {
  const to = adapter();
  to(ev("run.delta.tool.call.v1", { id: "t1", name: "github.comment", phase: "requested" }));
  const done = to(ev("run.delta.tool.done.v1", { id: "t1", result: "ok" })).find((e) => e.type === "task.upserted");
  assert.equal(done?.type, "task.upserted");
  if (done?.type === "task.upserted") {
    assert.equal(done.task.title, "Github comment");
    assert.equal(done.task.status, "complete");
  }
});

test("tool detail keeps bounded arguments and measures execution rather than approval time", () => {
  const to = adapter();
  to(ev("run.delta.tool.call.v1", { id: "t1", name: "search", phase: "requested" }));
  to({ ...ev("run.delta.tool.call.v1", { id: "t1", name: "search", phase: "executing", arguments: { q: "launch" } }), time: "2026-10-09T00:01:00Z" });
  const done = to({ ...ev("run.delta.tool.done.v1", { id: "t1", result: "No conflicts found" }), time: "2026-10-09T00:01:01.500Z" }).find((e) => e.type === "task.upserted");
  assert.equal(done?.type, "task.upserted");
  if (done?.type === "task.upserted") {
    assert.equal(done.task.input, '{\n  "q": "launch"\n}');
    assert.equal(done.task.durationMs, 1500);
  }
  const big = to(ev("run.delta.tool.call.v1", { id: "t2", name: "read", arguments: { text: "a".repeat(50_000) } })).find((e) => e.type === "task.upserted");
  if (big?.type === "task.upserted") assert.equal(big.task.input?.length, 32_000);
});

test("parked means waiting, which must not animate", () => {
  assert.deepEqual(adapter()(ev("run.parked.v1", { reason: "approval" })), [
    { type: "activity", activity: "waiting" },
  ]);
});

test("the orb is told what kind of work a tool call is", () => {
  const activity = (name: string) => {
    const [first] = adapter()(ev("run.delta.tool.call.v1", { id: "t", name, phase: "requested" }));
    return first?.type === "activity" ? first.activity : undefined;
  };
  assert.equal(activity("knowledge.search"), "searching");
  assert.equal(activity("delegate_helper"), "delegating");
  assert.equal(activity("github.comment"), "calling");
});

test("a call to another agent is that agent, not a tool", () => {
  const to = adapter();
  const [doing, started] = to(ev("run.delta.tool.call.v1", { id: "t1", name: "tag_helper", phase: "requested" }));
  assert.deepEqual(doing, { type: "activity", activity: "delegating" });
  assert.equal(started?.type, "task.upserted");
  if (started?.type === "task.upserted") {
    assert.equal(started.task.title, "helper");
    assert.equal(started.task.subagent?.name, "helper");
  }
  const done = to(ev("run.delta.tool.done.v1", { id: "t1", result: "Three tests fail." })).find((e) => e.type === "task.upserted");
  if (done?.type === "task.upserted") {
    assert.equal(done.task.status, "complete");
    assert.deepEqual(done.task.subagent, { name: "helper", runId: undefined, summary: "Result returned to caller", hue: undefined });
  }
  const [, tinted] = adapter({ hue: () => 210 })(ev("run.delta.tool.call.v1", { id: "t3", name: "tag_helper" }));
  if (tinted?.type === "task.upserted") {
    assert.equal(tinted.task.subagent?.hue, 210);
  }
  // An ordinary tool is still a tool.
  const plain = to(ev("run.delta.tool.call.v1", { id: "t2", name: "github.comment" })).find((e) => e.type === "task.upserted");
  assert.equal(plain?.type === "task.upserted" && plain.task.subagent, undefined);
});

test("a published file is one card that goes from generating to ready", () => {
  const to = adapter();
  const opened = to(ev("run.delta.tool.call.v1", { id: "t1", name: "publish_artifact" })).find((e) => e.type === "attachment.upserted");
  assert.equal(opened?.type === "attachment.upserted" && opened.attachment.type === "artifact" && opened.attachment.phase, "generating");
  const [ready] = to(
    ev("run.action.taken.v1", {
      name: "publish_artifact",
      data: { id: "art-7", title: "report.md", kind: "document", mime: "text/markdown", size: 19, version: 2, preview: "# Q3" },
    }),
  );
  assert.deepEqual(ready, {
    type: "attachment.upserted",
    attachment: {
      type: "artifact",
      id: "t1",
      title: "report.md",
      artifactId: "art-7",
      kind: "document",
      phase: "ready",
      mimeType: "text/markdown",
      sizeBytes: 19,
      version: 2,
      url: "/api/artifacts/art-7/content?inline=1",
      downloadUrl: "/api/artifacts/art-7/content",
      description: undefined,
      previewText: "# Q3",
    },
  });
});

test("a publish that is refused shows a failed file with the reason", () => {
  const to = adapter();
  to(ev("run.delta.tool.call.v1", { id: "t1", name: "publish_artifact" }));
  const [failed] = to(ev("run.action.taken.v1", { name: "publish_artifact", summary: "report.md", error: "platform: storage limit reached: the workspace is full" }));
  assert.equal(failed?.type, "attachment.upserted");
  if (failed?.type === "attachment.upserted" && failed.attachment.type === "artifact") {
    assert.equal(failed.attachment.id, "t1");
    assert.equal(failed.attachment.phase, "failed");
    assert.match(failed.attachment.error ?? "", /storage limit/);
  }
  // Another connector's action is not a file.
  assert.deepEqual(to(ev("run.action.taken.v1", { name: "jira_transition" })), []);
});

test("a call to another agent learns which run it started", () => {
  const to = adapter();
  to(ev("run.delta.tool.call.v1", { id: "t1", name: "tag_helper" }));
  const [named] = to(ev("run.action.taken.v1", { name: "tag_helper", data: { run: "run-9", agent: "helper", lifecycle: "attached" } }));
  assert.equal(named?.type, "task.upserted");
  if (named?.type === "task.upserted") {
    assert.equal(named.task.id, "t1");
    assert.equal(named.task.subagent?.runId, "run-9");
    assert.equal(named.task.status, "in_progress");
  }
  // The run is still known when the answer comes back.
  const done = to(ev("run.delta.tool.done.v1", { id: "t1", result: "Three tests fail." })).find((e) => e.type === "task.upserted");
  if (done?.type === "task.upserted") {
    assert.equal(done.task.status, "complete");
    assert.equal(done.task.subagent?.runId, "run-9");
    assert.equal(done.task.subagent?.summary, "Result returned to caller");
  }
});

test("work handed on without waiting is drawn as the agent it went to", () => {
  const to = adapter();
  const first = to(ev("run.delta.tool.call.v1", { id: "t1", name: "delegate_task" })).find((e) => e.type === "task.upserted");
  assert.equal(first?.type === "task.upserted" && first.task.subagent, undefined);
  const [named] = to(ev("run.action.taken.v1", { name: "delegate_task", data: { run: "run-4", agent: "worker", lifecycle: "delegated" } }));
  if (named?.type === "task.upserted") {
    assert.equal(named.task.title, "worker");
    assert.deepEqual(named.task.subagent, { name: "worker", runId: "run-4", summary: "Background work. Open Activity.", hue: undefined });
  } else {
    assert.fail("no task for the run that was started");
  }
});


test("a helper result is kept for inspection without repeating the parent answer", () => {
  const to = adapter();
  to(ev("run.delta.tool.call.v1", { id: "call-1", name: "call_agent" }));
  to(ev("run.action.taken.v1", { name: "call_agent", data: { run: "helper-run", agent: "researcher", lifecycle: "attached" } }));
  const result = "The complete helper result that the parent may incorporate.";
  const done = to(ev("run.delta.tool.done.v1", { id: "call-1", result })).find((e) => e.type === "task.upserted");
  assert.equal(done?.type, "task.upserted");
  if (done?.type === "task.upserted") {
    assert.equal(done.task.output, result);
    assert.equal(done.task.subagent?.runId, "helper-run");
    assert.equal(done.task.subagent?.summary, "Result returned to caller");
    assert.notEqual(done.task.subagent?.summary, result);
  }
});


test("parallel helpers correlate by call ID even when they start out of order", () => {
  const to = adapter();
  for (const id of ["call-a", "call-b"]) {
    to(ev("run.delta.tool.call.v1", { id, name: "call_agent", phase: "requested" }));
    to(ev("run.delta.tool.call.v1", { id, name: "call_agent", phase: "executing" }));
  }
  const [b] = to(ev("run.action.taken.v1", { name: "call_agent", data: { run: "run-b", agent: "second", tool_call_id: "call-b", lifecycle: "attached" } }));
  const [a] = to(ev("run.action.taken.v1", { name: "call_agent", data: { run: "run-a", agent: "first", tool_call_id: "call-a", lifecycle: "attached" } }));
  assert.equal(b?.type === "task.upserted" && b.task.id, "call-b");
  assert.equal(b?.type === "task.upserted" && b.task.subagent?.runId, "run-b");
  assert.equal(a?.type === "task.upserted" && a.task.id, "call-a");
  assert.equal(a?.type === "task.upserted" && a.task.subagent?.runId, "run-a");
});

test("a surface that is not the platform's own origin places file links itself", () => {
  const published = ev("run.action.taken.v1", { name: "publish_artifact", data: { id: "art-7", title: "report.md", kind: "document", mime: "text/markdown", size: 19, version: 1 } });
  const [abs] = adapter({ href: (path) => `https://humagen.example.com${path}` })(published);
  assert.equal(abs?.type === "attachment.upserted" && abs.attachment.type === "artifact" && abs.attachment.downloadUrl, "https://humagen.example.com/api/artifacts/art-7/content");
  const [none] = adapter({ href: () => undefined })(published);
  assert.equal(none?.type === "attachment.upserted" && none.attachment.type === "artifact" && none.attachment.url, undefined);
  assert.equal(none?.type === "attachment.upserted" && none.attachment.type === "artifact" && none.attachment.phase, "ready");
});

test("a stopped event ends the turn as stopped, confirmed", () => {
  const to = adapter();
  assert.deepEqual(to(ev("run.stopped.v1", { cause: "Stopped by dana" })), [
    { type: "turn.stopped" },
    { type: "stop.updated", stop: "confirmed" },
  ]);
});

test("trusted citation mode and retained-copy freshness survive the UI adapter", () => {
  const [event] = adapter()(
    ev("run.delta.citation.v1", {
      label: "[1]",
      index: 1,
      text: "Recorded passage",
      source: { document_title: "Runbook", source_uri: "https://example.com/runbook" },
      mode: "indexed",
      copy_at: "2026-10-20T12:00:00Z",
      refresh_failed: true,
    }),
  );
  assert.equal(event?.type, "citation.added");
  if (event?.type !== "citation.added") return;
  assert.deepEqual(
    {
      mode: event.citation.mode,
      copyAt: event.citation.copyAt,
      refreshFailed: event.citation.refreshFailed,
    },
    { mode: "indexed", copyAt: "2026-10-20T12:00:00Z", refreshFailed: true },
  );
});

test("legacy and malformed citation facts remain explicitly unproven", () => {
  for (const fields of [
    {},
    { mode: "hybrid", copy_at: "2026-10-20T12:00:00Z", refresh_failed: true },
    {
      source: {
        document_title: "Claims a live source",
        mode: "live",
        copy_at: "2026-10-20T12:00:00Z",
      },
    },
  ]) {
    const [event] = adapter()(
      ev("run.delta.citation.v1", {
        label: "[1]",
        index: 1,
        text: "Passage",
        source: { document_title: "Source" },
        ...fields,
      }),
    );
    assert.equal(event?.type, "citation.added");
    if (event?.type !== "citation.added") continue;
    assert.equal(event.citation.mode, undefined);
    assert.equal(event.citation.copyAt, undefined);
    assert.equal(event.citation.refreshFailed, undefined);
  }
  const [event] = adapter()(
    ev("run.delta.citation.v1", {
      label: "[1]",
      index: 1,
      text: "Passage",
      source: { document_title: "Source" },
      mode: "live",
      copy_at: "today",
      refresh_failed: "true",
    }),
  );
  if (event?.type !== "citation.added") return;
  assert.equal(event.citation.mode, "live");
  assert.equal(event.citation.copyAt, undefined);
  assert.equal(event.citation.refreshFailed, undefined);
});
