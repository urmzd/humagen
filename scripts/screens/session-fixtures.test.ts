import assert from "node:assert/strict";
import { test } from "node:test";
import { readReview } from "../../packages/core/src/review.ts";
import { fixtureResponse } from "./native.ts";
import { FINDING_TITLE, REVIEW_ASK } from "./session-fixtures.ts";

test("the model fixture emits review v2 and a real file-write call for approval", () => {
  const review = fixtureResponse({ messages: [{ role: "user", content: REVIEW_ASK }] });
  assert.equal(review.finish, "stop");
  const parsed = readReview("content" in review.delta ? (review.delta.content ?? "") : "");
  assert.equal(parsed.kind, "review");
  if (parsed.kind !== "review") return;
  assert.equal(parsed.report.format, "humagen.review.v2");
  assert.equal(parsed.report.findings[0]?.id, "briefing-owner");
  assert.equal(parsed.report.findings[0]?.title, FINDING_TITLE);
  assert.equal(parsed.report.findings[0]?.change, "- Developer owns the briefing");
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
