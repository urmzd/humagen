import assert from "node:assert/strict";
import { test } from "node:test";
import { DESKTOP, LANDSCAPE, PHONE } from "./checks.ts";
import { viewports } from "./viewports.ts";

test("responsive acceptance includes each required breakpoint once", () => {
  const sizes = viewports();
  assert.deepEqual(
    sizes.map((size) => size.width),
    [390, 480, 768, 1024, 1280],
  );
  assert.equal(sizes[0]?.rules, PHONE);
  assert.equal(sizes[1]?.rules, PHONE);
  assert.equal(sizes[2]?.rules, DESKTOP);
  assert.equal(viewports("844x390")[0]?.rules, LANDSCAPE);
});

test("invalid or repeated viewport input cannot silently omit acceptance coverage", () => {
  for (const value of ["", "390", "390x", "390x100", "390x844,", "390x844,390x844", "3000x800"])
    assert.throws(() => viewports(value));
});
