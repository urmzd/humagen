import assert from "node:assert/strict";
import { test } from "node:test";
import { checksReady, draftChecks } from "./evalCandidates.ts";

test("a slow answer drafts a time limit, any other reason words to contain", () => {
  assert.deepEqual(draftChecks("slow"), [{ kind: "within_ms", value: "" }]);
  assert.deepEqual(draftChecks("wrong"), [{ kind: "contains", value: "" }]);
  assert.deepEqual(draftChecks("something a person typed"), [{ kind: "contains", value: "" }]);
});

test("a drafted check is not ready until the reviewer writes it", () => {
  assert.equal(checksReady(draftChecks("wrong")), false);
  assert.equal(checksReady([]), false);
  assert.equal(checksReady([{ kind: "contains", value: "the new report" }]), true);
  assert.equal(checksReady([{ kind: "within_ms", value: "soon" }]), false);
  assert.equal(checksReady([{ kind: "within_ms", value: "5000" }]), true);
});
