import assert from "node:assert/strict";
import { test } from "node:test";
import type { AgentSession } from "../../packages/client/src/generated/types.ts";
import { readReview } from "../../packages/core/src/review.ts";
import { fixtureResponse } from "./native.ts";
import {
  assertProjectedReview,
  FINDING_TITLE,
  REVIEW_ANSWER,
  REVIEW_ASK,
  REVIEW_CONTEXT,
  REVIEW_STEP,
} from "./session-fixtures.ts";

test("the model fixture reads the pinned context before returning review v2", () => {
  const request = { messages: [{ role: "user", content: REVIEW_ASK }] };
  const read = fixtureResponse(request);
  assert.equal(read.finish, "tool_calls");
  assert.ok("tool_calls" in read.delta);
  const tool = read.delta.tool_calls?.[0];
  assert.equal(tool?.id, REVIEW_STEP);
  assert.equal(tool?.function.name, "github_read_context");
  assert.deepEqual(JSON.parse(tool?.function.arguments ?? "{}"), {
    address: REVIEW_CONTEXT.context.subject.html_url,
  });
  const review = fixtureResponse({
    messages: [
      ...request.messages,
      { role: "tool", content: JSON.stringify(REVIEW_CONTEXT.context) },
    ],
  });
  assert.equal(review.finish, "stop");
  const parsed = readReview("content" in review.delta ? (review.delta.content ?? "") : "");
  assert.equal(parsed.kind, "review");
  if (parsed.kind !== "review") return;
  assert.equal(parsed.report.format, "humagen.review.v2");
  assert.equal(parsed.report.findings[0]?.id, "briefing-owner");
  assert.equal(parsed.report.findings[0]?.title, FINDING_TITLE);
  assert.equal(parsed.report.findings[0]?.change, "- Developer owns the briefing");
  assert.equal(parsed.report.findings[0]?.evidence[0]?.step, REVIEW_STEP);
  const wrong = structuredClone(REVIEW_CONTEXT.context);
  wrong.subject.head.sha = "0".repeat(40);
  assert.throws(() =>
    fixtureResponse({
      messages: [...request.messages, { role: "tool", content: JSON.stringify(wrong) }],
    }),
  );
});

test("seed validation refuses a completed answer rejected by host provenance", () => {
  const session = (answer: string) =>
    ({
      messages: [{ run: { agent: "screens-reviewer", answer } }],
    }) as AgentSession;
  assert.doesNotThrow(() => assertProjectedReview(session(REVIEW_ANSWER)));
  assert.throws(
    () =>
      assertProjectedReview(
        session("Review needs correction: its scope does not match the recorded review context."),
      ),
    /host rejected/,
  );
  const unverified = JSON.parse(REVIEW_ANSWER);
  delete unverified.findings[0].evidence[0].step;
  assert.throws(() => assertProjectedReview(session(JSON.stringify(unverified))));
});

test("the approval fixture emits a real file-write call", () => {
  const approval = fixtureResponse({
    messages: [{ role: "user", content: "Write a multi-step plan after approval." }],
  });
  assert.equal(approval.finish, "tool_calls");
  assert.ok("tool_calls" in approval.delta);
  const call = approval.delta.tool_calls?.[0];
  assert.ok(call);
  assert.equal(call.function.name, "file_write");
  assert.equal(JSON.parse(call.function.arguments).path, "plan.md");
});
