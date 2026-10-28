import assert from "node:assert/strict";
import { test } from "node:test";
import type { Permission } from "../../../packages/client/src/generated/types.ts";
import {
  accessOf,
  asked,
  doing,
  explained,
  explainedPermission,
  explainUrl,
  listed,
  NOTABLE,
  PERMISSIONS,
} from "./access.ts";
import type { AccessDecision, Me } from "./api.ts";

const reads = PERMISSIONS.filter((p) => p.endsWith(":read"));
const operatorWrites = [
  "runs:write",
  "schedules:write",
  "alerts:write",
  "evals:write",
  "artifacts:write",
  "projects:write",
];

// The console names exactly the permissions the contract has: one the API
// gains and this list lacks, or the reverse, fails the typecheck.
type Listed = (typeof PERMISSIONS)[number];
export const contractIsListed: [Exclude<Permission, Listed>] extends [never] ? true : false = true;
export const listedIsContract: [Exclude<Listed, Permission>] extends [never] ? true : false = true;

function me(roles: string[], permissions: string[], subject = "00u-ada"): Me {
  return {
    principal: { subject, tenant: "acme", kind: "user", name: "Ada Lovelace", roles },
    permissions,
  };
}
const operator = me(["viewer", "operator"], [...reads, ...operatorWrites]);
const viewer = me(["viewer"], [...reads]);
const nobody = me([], []);
const admin = me(["admin"], [...PERMISSIONS]);

const decision = (d: Partial<AccessDecision>): AccessDecision => ({
  permission: "runs:write",
  allowed: true,
  outcome: "allowed",
  held: [],
  ...d,
});

test("every permission has words, the project ones included", () => {
  for (const p of PERMISSIONS) assert.ok(doing[p], `${p} has words`);
  assert.equal(doing["projects:read"], "see projects");
  assert.equal(doing["projects:write"], "make and change projects");
  assert.equal(doing["projects:govern"], "own every project");
  assert.equal(doing["people:read"], "see who is in the workspace");
  assert.deepEqual(
    PERMISSIONS.slice(-4),
    ["projects:read", "projects:write", "projects:govern", "people:read"],
    "in the order the platform lists them",
  );
});

test("owning every project is the last thing named, so only someone who may do everything else is told they cannot", () => {
  assert.equal(NOTABLE[NOTABLE.length - 1], "projects:govern");
  const almost = me(
    ["manager", "billing", "clients"],
    PERMISSIONS.filter((p) => p !== "projects:govern"),
  );
  const a = accessOf(almost, decision({ role: "manager", held: [{ role: "manager" }] }));
  assert.equal(a.denies, "own every project");
  assert.notEqual(a.allows, "do everything in this workspace");
  // An operator makes projects, and is still told first that it cannot change an agent.
  assert.equal(
    accessOf(operator, decision({ role: "operator", held: [{ role: "operator" }] })).denies,
    "change what an agent is",
  );
  assert.equal(
    explained(
      decision({
        permission: "projects:govern",
        allowed: false,
        outcome: "no match",
        held: [{ role: "operator" }],
      }),
      "Ada",
    ).question,
    "Can Ada own every project?",
  );
});

test("listed: joins with commas and a final and", () => {
  assert.equal(listed([]), "");
  assert.equal(listed(["a"]), "a");
  assert.equal(listed(["a", "b", "c"]), "a, b and c");
});

test("explainedPermission: something the person may change, else read", () => {
  assert.equal(explainedPermission(operator), "runs:write");
  assert.equal(explainedPermission(viewer), "agents:read");
  assert.equal(explainedPermission(nobody), "runs:read");
});

test("accessOf: an operator is told the role, the group that earned it, and what it does not allow", () => {
  const d = decision({
    role: "operator",
    held: [
      { role: "viewer", grant: "grant viewer to anyone" },
      { role: "operator", grant: 'grant operator to group "eng-oncall"', group: "eng-oncall" },
    ],
  });
  const a = accessOf(operator, d, "https://example.test/policy");
  assert.equal(a.role, "operator");
  assert.equal(a.group, "eng-oncall");
  assert.equal(a.via, "your company's identity provider");
  assert.equal(a.allows, "run agents and manage schedules");
  assert.equal(a.denies, "change what an agent is");
  assert.equal(a.explainUrl, explainUrl("runs:write"));
  assert.equal(a.policyUrl, "https://example.test/policy");
});

test("accessOf: a viewer sees everything and cannot run agents", () => {
  const a = accessOf(
    viewer,
    decision({
      permission: "agents:read",
      role: "viewer",
      held: [{ role: "viewer", group: "readers" }],
    }),
  );
  assert.equal(a.role, "viewer");
  assert.equal(a.group, "readers");
  assert.equal(a.allows, "see everything in this workspace");
  assert.equal(a.denies, "run agents");
});

test("accessOf: an admin does everything and is denied nothing", () => {
  const a = accessOf(admin, null);
  assert.equal(a.role, "admin");
  assert.equal(a.allows, "do everything in this workspace");
  assert.equal(a.denies, undefined);
});

test("accessOf: no role names no role, so the page says no rule grants one", () => {
  const a = accessOf(
    nobody,
    decision({ permission: "runs:read", allowed: false, outcome: "no match" }),
  );
  assert.equal(a.role, undefined);
  assert.equal(a.allows, undefined);
  assert.equal(a.denies, undefined);
  assert.equal(a.explainUrl, explainUrl("runs:read"));
});

test("accessOf: dev mode is not through an identity provider", () => {
  assert.equal(accessOf(me(["admin"], [...PERMISSIONS], "dev"), null).via, undefined);
});

test("explained: no rule covers it, as the Access board shows", () => {
  const e = explained(
    decision({
      permission: "agents:write",
      allowed: false,
      outcome: "no match",
      held: [
        { role: "operator", grant: 'grant operator to group "eng-oncall"', group: "eng-oncall" },
      ],
    }),
    "Ada Lovelace",
  );
  assert.equal(e.question, "Can Ada change an agent?");
  assert.equal(e.answer, "No. Nothing in the policy allows it.");
  assert.deepEqual(
    e.steps.map((s) => s.label),
    ["Signed in", "Granted", "Asked for"],
  );
  assert.equal(e.steps[1]?.text, 'grant operator to group "eng-oncall"');
  assert.match(e.steps[2]?.text ?? "", /^write on agents, which no rule in these roles covers$/);
  assert.ok(e.fix);
});

test("explained: allowed names the grant, the inheritance and the rule", () => {
  const e = explained(
    decision({
      permission: "runs:read",
      role: "operator",
      path: ["operator", "viewer"],
      rule: "allow read on *",
      grant: 'grant operator to group "eng-oncall"',
      held: [{ role: "operator", grant: 'grant operator to group "eng-oncall"' }],
    }),
    "Ada",
  );
  assert.equal(e.answer, "Yes. You are an operator, and the operator role allows it.");
  assert.deepEqual(
    e.steps.map((s) => [s.label, s.text]),
    [
      ["Signed in", "Ada, holding operator"],
      ["Granted", 'grant operator to group "eng-oncall"'],
      ["Through", "operator extends viewer"],
      ["Allows", "allow read on *"],
      ["Asked for", "read on runs, which the rule above allows"],
    ],
  );
  assert.equal(e.fix, undefined);
});

test("explained: a deny says it beats every allow", () => {
  const e = explained(
    decision({
      permission: "billing:read",
      allowed: false,
      outcome: "denied",
      role: "auditor",
      rule: "deny read on billing",
      held: [{ role: "auditor" }],
    }),
    "Ada",
  );
  assert.match(e.answer, /deny beats every allow/);
  assert.equal(e.steps.find((s) => s.label === "Denies")?.text, "deny read on billing");
});

test("explained: nobody with no role is told no grant gives one", () => {
  const e = explained(
    decision({ permission: "runs:read", allowed: false, outcome: "no match" }),
    "Ada",
  );
  assert.equal(e.answer, "No. No grant gives you a role yet.");
  assert.equal(e.steps[0]?.text, "Ada, holding no role");
});

test("explained: a machine is decided by its scope", () => {
  assert.match(explained(decision({ outcome: "scope" }), "ci").answer, /scope/);
  assert.equal(asked("agents:write"), "write on agents");
});
