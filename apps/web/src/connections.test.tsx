import assert from "node:assert/strict";
import { test } from "node:test";
import type { ReactElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import type { ToolCredential } from "./api.ts";
import { ConnectForm, ConnectionCard } from "./Connections.tsx";
import {
  cannotConnect,
  destination,
  standing,
  startsAllowed,
  ways,
  whereReferences,
} from "./connections.ts";

const html = (el: ReactElement) => renderToStaticMarkup(el);
const text = (s: string) => s.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ");
const noop = () => {};

const issues: ToolCredential = {
  provider: "issues",
  transport: "http",
  endpoint: "https://issues.example.com/mcp",
  tools: 2,
  connected: false,
};
const byRef: ToolCredential = {
  ...issues,
  connected: true,
  kind: "reference",
  ref: "op://People/ana/issues",
  updated_at: "2026-10-21T09:00:00Z",
};
const stored: ToolCredential = {
  ...issues,
  connected: true,
  kind: "value",
  updated_at: "2026-10-21T09:00:00Z",
};

const card = (
  c: ToolCredential,
  w = ways({ values: true, references: ["op://People/"] }),
  open = false,
) =>
  html(
    <ConnectionCard
      connection={c}
      ways={w}
      references={["op://People/"]}
      open={open}
      onOpen={noop}
      onCancel={noop}
      onSave={noop}
      onRemove={noop}
    />,
  );

test("a tool that acts as you says whether you connected it, and how", () => {
  const none = text(card(issues));
  assert.match(none, /issues/);
  assert.match(none, /Not connected/);
  assert.match(none, /cannot use these tools for you/);
  assert.match(none, /Connect/);
  assert.doesNotMatch(none, /Remove/);

  const ref = text(card(byRef));
  assert.match(ref, /op:\/\/People\/ana\/issues/);
  assert.match(ref, /stores only the reference/);
  assert.match(ref, /Replace/);
  assert.match(ref, /Remove/);

  // A stored value is never part of what the page has, so it says where it is
  // and nothing more.
  const value = text(card(stored));
  assert.match(value, /stored with Humagen/);
  assert.doesNotMatch(value, /op:\/\//);
});

test("it says where your credential goes", () => {
  assert.equal(destination(issues), "Your credential is sent to issues.example.com.");
  assert.match(
    destination({ ...issues, transport: "stdio", endpoint: undefined }),
    /runs on the server/,
  );
  assert.match(text(card(issues)), /sent to issues\.example\.com/);
});

test("you are offered only the ways this workspace allows", () => {
  assert.deepEqual(ways({ values: false, references: ["op://People/"] }), ["reference"]);
  assert.deepEqual(ways({ values: true, references: [] }), ["value"]);
  assert.deepEqual(ways({ values: false, references: [] }), []);

  const both = html(
    <ConnectForm
      ways={["reference", "value"]}
      references={["op://People/"]}
      replacing={false}
      onSave={noop}
      onCancel={noop}
    />,
  );
  assert.match(both, /Reference/);
  assert.match(both, /Value/);

  const refOnly = html(
    <ConnectForm
      ways={["reference"]}
      references={["op://People/", "keychain:humagen-people/"]}
      replacing={false}
      onSave={noop}
      onCancel={noop}
    />,
  );
  assert.doesNotMatch(refOnly, /type="password"|>Value</);
  assert.match(text(refOnly), /starts with op:\/\/People\/, keychain:humagen-people\//);

  const valueOnly = html(
    <ConnectForm ways={["value"]} references={[]} replacing onSave={noop} onCancel={noop} />,
  );
  assert.match(valueOnly, /type="password"/);
  assert.match(text(valueOnly), /never shown again/);
  assert.match(text(valueOnly), /Replace/);

  // With no way at all, nothing can be connected and the card says why.
  const nothing = text(card(issues, []));
  assert.doesNotMatch(nothing, /Connect\b(?! a credential)/);
  assert.match(nothing, /lets nobody connect a credential/);
});

test("a reference from outside the workspace's starts is caught before it is sent", () => {
  assert.ok(startsAllowed("op://People/ana/issues", ["op://People/"]));
  assert.ok(startsAllowed("  op://People/ana/issues ", ["op://People/"]));
  assert.ok(!startsAllowed("op://Infra/db/password", ["op://People/"]));
  assert.ok(!startsAllowed("op://PeopleX/a/b", ["op://People/"]));
  assert.ok(!startsAllowed("", ["op://People/"]));
  assert.ok(!startsAllowed("op://People/a/b", []));
  assert.match(whereReferences([]), /lets nobody reference/);
});

test("what you stand with is said in words", () => {
  assert.match(standing(issues), /^Not connected/);
  assert.match(standing(byRef), /reads op:\/\/People\/ana\/issues each time a run uses it/);
  assert.match(standing(stored), /stored with Humagen/);
});

test("someone who may not connect is shown where they stand with each tool, and nothing to press", () => {
  for (const c of [issues, byRef, stored]) {
    const out = text(
      html(
        <ConnectionCard
          connection={c}
          ways={["reference", "value"]}
          references={["op://People/"]}
          canConnect={false}
          open
          onOpen={noop}
          onCancel={noop}
          onSave={noop}
          onRemove={noop}
        />,
      ),
    );
    assert.match(out, /connected/i);
    assert.doesNotMatch(out, /\bConnect\b|Replace|Remove|Cancel|type="password"/);
    assert.doesNotMatch(
      html(
        <ConnectionCard
          connection={c}
          ways={["reference"]}
          references={["op://People/"]}
          canConnect={false}
          open
          onOpen={noop}
          onCancel={noop}
          onSave={noop}
          onRemove={noop}
        />,
      ),
      /<input|<button/,
    );
  }
  assert.equal(
    cannotConnect,
    "Connecting a credential takes permission to run agents, which you don't have. An admin can give it to you.",
  );
});
