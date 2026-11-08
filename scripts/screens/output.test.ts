import assert from "node:assert/strict";
import { test } from "node:test";
import { screenOutput } from "./output.ts";

test("CI theme output stays in the root artifact directory from a package working directory", () => {
  const repo = "/home/runner/work/humagen/humagen";
  for (const theme of ["light", "dark"])
    assert.equal(screenOutput(repo, `.screens/${theme}`), `${repo}/.screens/${theme}`);
  assert.equal(screenOutput(repo), `${repo}/.screens`);
  assert.equal(screenOutput(repo, "/tmp/screens"), "/tmp/screens");
});
