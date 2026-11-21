import assert from "node:assert/strict";
import { test } from "node:test";
import type { Permission } from "../../../packages/client/src/generated/types.ts";
import { doing, PERMISSIONS } from "./access.ts";

// The console names exactly the permissions the contract has: one the API
// gains and this list lacks, or the reverse, fails the typecheck.
type Listed = (typeof PERMISSIONS)[number];
export const contractIsListed: [Exclude<Permission, Listed>] extends [never] ? true : false = true;
export const listedIsContract: [Exclude<Listed, Permission>] extends [never] ? true : false = true;

test("every permission has words, the project ones included", () => {
  for (const p of PERMISSIONS) assert.ok(doing[p], `${p} has words`);
  assert.equal(doing["projects:read"], "see projects");
  assert.equal(doing["projects:write"], "make and change projects");
  assert.equal(doing["projects:govern"], "own every project");
  assert.equal(doing["people:read"], "see who is in the workspace");
  assert.equal(doing["speech:write"], "record voice notes");
  assert.deepEqual(
    PERMISSIONS.slice(-5),
    ["projects:read", "projects:write", "projects:govern", "people:read", "speech:write"],
    "in the order the platform lists them",
  );
});
