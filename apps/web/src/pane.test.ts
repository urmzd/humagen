import assert from "node:assert/strict";
import { test } from "node:test";
import {
  activeKey,
  closeTab,
  MAX_PANE_TABS,
  openTab,
  paneTabs,
  parseTab,
  replaceTab,
  tabKey,
} from "./pane.ts";
import { href, parse, type Route } from "./route.ts";

const at = (url: string) => {
  const u = new URL(url, "http://x");
  return parse(u.pathname, u.search);
};

test("a tab is a list kind, or an item kind with a reference", () => {
  assert.deepEqual(parseTab("files"), { kind: "files" });
  assert.deepEqual(parseTab("file:src/a:b.ts"), { kind: "file", ref: "src/a:b.ts" });
  assert.deepEqual(parseTab("pull:urmzd/humagen#12"), { kind: "pull", ref: "urmzd/humagen#12" });
  for (const bad of ["file", "files:x", "nope", "file:", "artifact"])
    assert.equal(parseTab(bad), undefined, bad);
  assert.equal(tabKey({ kind: "scratchpad", ref: "sp-1" }), "scratchpad:sp-1");
});

test("an address from before tabs opens one tab for its section", () => {
  assert.deepEqual(paneTabs({ panel: "workspace" }), [{ kind: "files" }]);
  assert.deepEqual(paneTabs({ panel: "workspace", file: "a.md" }), [{ kind: "file", ref: "a.md" }]);
  assert.deepEqual(paneTabs({ panel: "workspace", workspaceSection: "sandboxes", sandbox: "r1" }), [
    { kind: "sandbox", ref: "r1" },
  ]);
});

test("opening, replacing and closing tabs keep one active and a bounded set", () => {
  let view = openTab({ panel: "workspace" }, { kind: "scratchpads" });
  assert.deepEqual(view.tabs, ["files", "scratchpads"]);
  view = replaceTab(view, "scratchpads", { kind: "scratchpad", ref: "sp-1" });
  assert.deepEqual(view.tabs, ["files", "scratchpad:sp-1"]);
  assert.equal(activeKey(view), "scratchpad:sp-1");
  // Opening an open tab shows it.
  view = openTab(view, { kind: "files" });
  assert.deepEqual(view.tabs, ["files", "scratchpad:sp-1"]);
  assert.equal(view.active, "files");
  view = closeTab(view, "files") ?? view;
  assert.deepEqual(view.tabs, ["scratchpad:sp-1"]);
  assert.equal(view.active, "scratchpad:sp-1");
  assert.equal(closeTab(view, "scratchpad:sp-1"), undefined);
  for (let i = 0; i < MAX_PANE_TABS + 3; i++) view = openTab(view, { kind: "file", ref: `f${i}` });
  assert.equal(view.tabs?.length, MAX_PANE_TABS);
  assert.equal(view.active, `file:f${MAX_PANE_TABS + 2}`);
});

test("the pane's tabs, active tab, full and split survive a copied address", () => {
  const view = {
    page: "conversation" as const,
    id: "t-1",
    panel: "workspace" as const,
    tabs: ["files", "file:src/a,b.ts", "artifact:art-1", "pull:urmzd/humagen#12"],
    active: "artifact:art-1",
    expanded: true,
    split: true,
  } satisfies Route;
  const address = href(view);
  assert.match(address, /^\/sessions\/t-1\?panel=files&/);
  assert.deepEqual(at(address), JSON.parse(JSON.stringify(view)));
});
