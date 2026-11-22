import assert from "node:assert/strict";
import { test } from "node:test";
import { sessionSourceLink } from "./sourceLink.ts";

test("a GitHub session links to the issue or pull request it was asked on", () => {
  assert.deepEqual(sessionSourceLink("github://urmzd/humagen/pull/512"), {
    kind: "github",
    label: "urmzd/humagen PR #512",
    url: "https://github.com/urmzd/humagen/pull/512",
  });
  assert.equal(
    sessionSourceLink("github://urmzd/humagen/issues/7")?.url,
    "https://github.com/urmzd/humagen/issues/7",
  );
  assert.equal(sessionSourceLink("slack://T1/C1/1699.1"), undefined);
  assert.equal(sessionSourceLink("console:ana"), undefined);
  assert.equal(sessionSourceLink("s_parent"), undefined);
});
