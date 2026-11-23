import assert from "node:assert/strict";
import { test } from "node:test";
import type { UnknownEvent } from "@humagen/client/types";
import { defaultCopy } from "@humagen/components/copy";
import { type FailureCause, type FailureKind, isRetryable } from "@humagen/core";
import { adapter, failureCause, failureOf, type RunEvent } from "./run.ts";

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
  assert.deepEqual(out.slice(1), [{ type: "text.delta", text: "Done" }, { type: "turn.completed", durationMs: 0 }]);
});

test("completion says how long the run worked, from its acceptance", () => {
  const to = adapter();
  const done = { ...ev("run.completed.v1", { text: "Done" }, 2), time: "2026-10-09T00:00:32Z" };
  const out = [ev("run.accepted.v1"), done].flatMap(to);
  assert.deepEqual(out.at(-1), { type: "turn.completed", durationMs: 32_000 });
  // Without its acceptance, a completion cannot say.
  assert.deepEqual(adapter()(done).at(-1), { type: "turn.completed" });
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
    // When it started running, so a surface can count while it runs.
    assert.equal(done.task.startedAt, "2026-10-09T00:01:00.000Z");
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
  // A background agent call, and delegate_task on a run recorded before it.
  for (const name of ["agent", "delegate_task"]) {
    const to = adapter();
    const first = to(ev("run.delta.tool.call.v1", { id: "t1", name })).find((e) => e.type === "task.upserted");
    assert.equal(first?.type === "task.upserted" && first.task.subagent, undefined);
    const [named] = to(ev("run.action.taken.v1", { name, data: { run: "run-4", agent: "worker", tool_call_id: "t1", lifecycle: "delegated" } }));
    if (named?.type === "task.upserted") {
      assert.equal(named.task.title, "worker");
      assert.deepEqual(named.task.subagent, { name: "worker", runId: "run-4", summary: "Background work. Open Activity.", hue: undefined });
    } else {
      assert.fail("no task for the run that was started");
    }
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


test("an agent call is shown as the agent it called, as call_agent was", () => {
  const to = adapter();
  to(ev("run.delta.tool.call.v1", { id: "call-1", name: "agent", input: '{"agent":"researcher","title":"Look into it","brief":"Find the cause."}' }));
  to(ev("run.action.taken.v1", { name: "agent", data: { run: "helper-run", agent: "researcher", tool_call_id: "call-1", lifecycle: "attached" } }));
  const done = to(ev("run.delta.tool.done.v1", { id: "call-1", result: "The cause is a stale cache." })).find((e) => e.type === "task.upserted");
  assert.equal(done?.type === "task.upserted" && done.task.subagent?.runId, "helper-run");
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


test("provider credential failures preserve safe context without exposing a secret", () => {
  const failure = failureOf(
    "provider anthropic (model claude, status 401): API key sk-private is unauthorized",
    "r1",
  );
  assert.equal(failure.cause, "provider_credential");
  assert.equal(failure.retryable, false);
  assert.doesNotMatch(JSON.stringify(failure), /sk-private/);
  assert.equal(failure.detail, undefined);
  assert.equal(failure.message, defaultCopy.runFailure.because.provider_credential);
});

test("budget exhaustion is not retried blindly and a refused connection keeps its host out of every view", () => {
  const budget = failureOf("workspace spend limit reached", "r2");
  assert.equal(budget.cause, "budget");
  assert.equal(budget.retryable, false);
  const unavailable = failureOf("dial tcp private-host.internal:443: connection refused", "r3");
  assert.equal(unavailable.kind, "unavailable");
  assert.doesNotMatch(JSON.stringify(unavailable), /private-host/);
});

test("the engine's stop after failed steps is said in plain words, with Retry", () => {
  const failure = failureOf("agent run: too many consecutive failed tool turns: 2 turns", "r4");
  assert.equal(failure.kind, "tool_error");
  assert.equal(failure.cause, "failed_steps");
  assert.doesNotMatch(failure.message, /consecutive|tool turns/);
  assert.equal(failure.detail, undefined);
});

const NO_PROVIDER =
  'durable: max attempts exhausted: runtime: run r1: agent "github-reviewer" version 1.0.0: platform: model provider: @github-reviewer needs a model provider its models name (anthropic); an administrator enables one in Settings, under Model providers';

test("each runtime error is read as its cause, the specific cause before the retry wrapper", () => {
  const cases: [string, FailureCause][] = [
    [NO_PROVIDER, "no_model_provider"],
    ["durable: max attempts exhausted: agent: max iterations reached", "max_iterations"],
    ["durable: max attempts exhausted", "max_attempts"],
    ["durable: max attempts exhausted: weird thing", "max_attempts"],
    ["session: access denied", "permission_denied"],
    ['sandbox: access denied by policy: user "a", op read, key "k"', "permission_denied"],
    ["mcptools: denied by grant", "permission_denied"],
    ["budget exceeded: spent $1.00 of $1.00 after 3 requests", "budget"],
    ["session: budget exceeded or not granted", "budget"],
    ["llm call exceeded timeout 30s: context deadline exceeded", "timeout"],
    ["connector: undeliverable: channel_not_found", "undeliverable"],
    ["provider: 429 too many requests", "rate_limited"],
    ["agent: step 2\nprovider: 529 overloaded", "unknown"],
  ];
  for (const [error, cause] of cases) {
    assert.equal(failureCause(error), cause, error);
    const failure = failureOf(error, "r");
    assert.equal(failure.cause, cause, error);
    assert.equal(failure.message, defaultCopy.runFailure.because[cause], error);
  }
  assert.equal(failureCause(undefined), "unknown");
  assert.equal(failureOf(undefined, "r").detail, undefined);
});

test("a refusal by a tool or by the app the answer goes to is not said as the model provider's", () => {
  const cases: [string, FailureCause][] = [
    ["tool github_search: GET https://api.github.com/search: status 401: Bad credentials", "credential"],
    ["tool google_search: status 429: rate limit exceeded", "rate_limited"],
    ["connector: undeliverable: slack chat.postMessage: http 401 unauthorized", "undeliverable"],
    ["connector: undeliverable: slack: rate limited", "undeliverable"],
    ["status 401: unauthorized", "credential"],
    ["too many requests", "rate_limited"],
    // Named in passing, not in the runtime's form for a provider's error: said neutrally.
    ["Anthropic API key is unauthorized", "credential"],
    ["openai: status 429: rate limit exceeded", "rate_limited"],
    ["provider anthropic (model claude-sonnet, status 401): invalid x-api-key", "provider_credential"],
    ["provider openai (model gpt, status 429): rate limit exceeded", "provider_rate_limited"],
    ["provider gemini (model g): too many requests", "provider_rate_limited"],
    // The agent's handle and a tool's or server's name do not make it the model provider's.
    ['runtime: run r1: agent "openai-evals" version 1.0.0: mcp: linear: unauthorized', "credential"],
    ["mcp: google-drive: unauthorized", "credential"],
    ["mcp: google-calendar: status 429 too many requests", "rate_limited"],
    [
      'runtime: run r1: agent "connector-bot" version 1.0.0: provider anthropic: status 401: invalid x-api-key',
      "provider_credential",
    ],
    ["tool ask_model: provider openai: status 401: unauthorized", "credential"],
  ];
  for (const [error, cause] of cases) assert.equal(failureCause(error), cause, error);
  const words = defaultCopy.runFailure.because;
  for (const neutral of [words.credential, words.rate_limited])
    assert.doesNotMatch(neutral, /model provider/i);
});

test("a failure offers what it offered before it was said in plain words, but for a run with no model provider", () => {
  // Each error with the kind and retryability it had before causes were read.
  const before: [string, FailureKind, boolean][] = [
    ["sandbox: exec: egress: denied", "internal", true],
    ['sandbox: access denied by policy: user "a", op read, key "k"', "internal", true],
    ["session: access denied", "internal", true],
    ["tool deploy is denied by policy", "internal", true],
    ["bigquery: Access Denied: Table x", "internal", true],
    ["mcptools: denied by grant", "internal", true],
    ["agent: max iterations reached", "internal", true],
    ["durable: max attempts exhausted: agent: max iterations reached", "internal", true],
    ["connector: undeliverable: channel_not_found", "internal", true],
    ["connector: undeliverable: slack chat.postMessage: http 401 unauthorized", "forbidden", false],
    ["connector: undeliverable: slack: rate limited", "rate_limited", true],
    ["tool github_search: GET https://api.github.com/search: status 401: Bad credentials", "forbidden", false],
    ["Anthropic API key sk-private is unauthorized", "forbidden", false],
    ["anthropic: status 429: rate limit exceeded", "rate_limited", true],
    ["provider: 429 too many requests", "rate_limited", true],
    ["workspace spend limit reached", "forbidden", false],
    ["budget exceeded: spent $1.00 of $1.00 after 3 requests", "forbidden", false],
    ["session: budget exceeded or not granted", "forbidden", false],
    ["llm call exceeded timeout 30s: context deadline exceeded", "timeout", true],
    ["tool fetch: permission denied", "forbidden", false],
    ["github: status 403: forbidden", "forbidden", false],
    ["dial tcp private-host.internal:443: connection refused", "unavailable", true],
    ["upstream: status 503: service unavailable", "unavailable", true],
    ["agent run: too many consecutive failed tool turns: 2 turns", "tool_error", true],
    ["durable: max attempts exhausted", "internal", true],
    ["durable: max attempts exhausted: weird thing", "internal", true],
    ["agent: step 2\nprovider: 529 overloaded", "internal", true],
    ["", "internal", true],
  ];
  for (const [error, kind, retryable] of before) {
    const failure = failureOf(error, "r");
    assert.equal(failure.kind, kind, error);
    assert.equal(isRetryable(failure), retryable, error);
  }
  // The one change: nothing a person in the session does helps until an admin enables a provider.
  const none = failureOf(NO_PROVIDER, "r");
  assert.equal(none.kind, "forbidden");
  assert.equal(isRetryable(none), false);
});

test("the runtime's words are kept only for a failure no sentence explains", () => {
  assert.equal(
    failureOf("durable: max attempts exhausted: weird thing", "r").detail,
    "durable: max attempts exhausted: weird thing",
  );
  // Of an error on several lines, only the last: the cause, as it was shown before.
  assert.equal(failureOf("agent: step 2\nprovider: 529 overloaded", "r").detail, "provider: 529 overloaded");
  assert.equal(failureOf("boom: sk-abcd\nefgh1234567890 trailing", "r").detail, "efgh1234567890 trailing");
  const pem = [
    "sign: bad key:",
    "-----BEGIN PRIVATE KEY-----",
    "MIIEvQIBADANBgkqhkiG9w0BAQEFAASC",
    "-----END PRIVATE KEY-----",
    "stopped after 2 steps",
  ].join("\n");
  const shown = failureOf(pem, "r").detail ?? "";
  assert.equal(shown, "stopped after 2 steps");
  assert.doesNotMatch(shown, /PRIVATE KEY|MIIEvQ|bad key/);
  for (const error of [
    NO_PROVIDER,
    "session: access denied",
    "connector: undeliverable: channel_not_found",
    "dial tcp private-host.internal:443: connection refused",
    "llm call exceeded timeout 30s: context deadline exceeded",
    "workspace spend limit reached",
    "provider: 429 too many requests",
    "agent: max iterations reached",
    "Anthropic API key sk-private is unauthorized",
    "tool github_search: status 401: Bad credentials",
    // No sentence explains it, but it names where a credential is kept.
    "durable: max attempts exhausted: read secret reference op://vault/item",
  ])
    assert.equal(failureOf(error, "r").detail, undefined, error);
});

test("the runtime's words are shown without an address's sign-in or query, and without keys", () => {
  const shown = (error: string) => failureOf(error, "r").detail ?? "";
  assert.equal(
    shown("fetch https://dana:hunter2@files.example.test/a/b?sig=abc123&x=1 failed"),
    "fetch https://files.example.test/a/b failed",
  );
  assert.equal(
    shown("read https://files.example.test/data/part-0.parquet?X-Sig=abc: status 500"),
    "read https://files.example.test/data/part-0.parquet: status 500",
  );
  assert.equal(
    shown("postgres://svc:pw@db.example.test:5432/app: broken pipe"),
    "postgres://db.example.test:5432/app: broken pipe",
  );
  assert.equal(shown("header Authorization: Bearer abc.DEF-123_x sent"), "header Authorization: Bearer [hidden] sent");
  assert.equal(shown("sent Bearer abc.DEF-123_x and stopped"), "sent Bearer [hidden] and stopped");
  assert.equal(shown("request with token=s3cr3tvalue&x failed"), "request with token=[hidden]&x failed");
  assert.equal(shown('body {"password": "hunter2"} refused'), 'body {"password": [hidden]} refused');
  for (const key of ["sk-live1234567890", "xoxb-1234-abcdefgh", "ghp_abcdefghijklmnop", "eyJhbGciOiJIUzI1NiJ9.e30.abc"])
    assert.doesNotMatch(shown(`step failed with ${key} in the reply`), new RegExp(key.slice(0, 12)), key);
  assert.equal(
    shown("digest 0123456789abcdef0123456789abcdef0123456789abcdef did not match"),
    "digest [hidden] did not match",
  );
  // A run's id and ordinary words stay.
  assert.equal(
    shown("runtime: run r3d7fe6e5c2d3b2698248154ad1592982: weird thing"),
    "runtime: run r3d7fe6e5c2d3b2698248154ad1592982: weird thing",
  );
});

test("a publish that fails before it reports does not stay generating", () => {
  const to = adapter();
  to(ev("run.delta.tool.call.v1", { id: "t1", name: "publish_artifact" }));
  const closed = to(ev("run.delta.tool.done.v1", { id: "t1", name: "publish_artifact", error: "invalid arguments\nno such file: report.md" })).find((e) => e.type === "attachment.upserted");
  assert.ok(closed?.type === "attachment.upserted" && closed.attachment.type === "artifact");
  assert.equal(closed.attachment.id, "t1");
  assert.equal(closed.attachment.phase, "failed");
  assert.equal(closed.attachment.error, "no such file: report.md");
  // Nothing is left open for the run's end to close a second time.
  assert.deepEqual(to(ev("run.failed.v1", { terminal: true, error: "boom" })).map((e) => e.type), ["turn.failed"]);
});

test("a publish still open when the run fails is closed with it", () => {
  const to = adapter();
  to(ev("run.delta.tool.call.v1", { id: "t1", name: "publish_artifact" }));
  const [closed, failed] = to(ev("run.failed.v1", { terminal: true, error: "boom" }));
  assert.ok(closed?.type === "attachment.upserted" && closed.attachment.type === "artifact");
  assert.equal(closed.attachment.phase, "failed");
  assert.equal(failed?.type, "turn.failed");
});
