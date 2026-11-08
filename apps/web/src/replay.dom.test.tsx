import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { stubFetch } from "./dom.ts";
import { ReplayMenu } from "./ReplayMenu.tsx";

// DES-058: Replay With sends a finished turn's prompt to another agent.

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

const menu = (point: string, onOpen: (id: string) => void) => (
  <ReplayMenu
    agents={["lead", "writer"]}
    answered="lead"
    prompt="@lead draft the launch note"
    thread="th-1"
    point={point}
    onOpen={onOpen}
  />
);

test("Replay With forks at the selected finished turn and sends it to the chosen agent", async () => {
  net = stubFetch({
    "POST /api/threads/th-1/fork": () => ({ id: "th-fork" }),
    "POST /api/threads/th-fork/messages": () => ({ message: { id: "m" } }),
  });
  const opened: string[] = [];
  render(menu("r0", (id) => opened.push(id)));
  fireEvent.click(screen.getByRole("button", { name: "Replay With" }));
  const items = await screen.findAllByRole("menuitem");
  assert.deepEqual(
    items.map((i) => i.textContent),
    ["Replay with @writer", "Replay with @lead"],
    "the agent that answered is last",
  );
  fireEvent.click(items[0]!);
  assert.equal(net.seen.length, 0, "choosing an agent prepares the replay for review");
  fireEvent.click(await screen.findByRole("button", {name:"Start replay"}));
  await waitFor(() => assert.deepEqual(opened, ["th-fork"]));
  const posts = net.seen.filter((r) => r.method === "POST").map((r) => [r.path, r.body]);
  assert.deepEqual(posts, [
    ["/api/threads/th-1/fork", { through: "r0" }],
    ["/api/threads/th-fork/messages", { text: "@writer draft the launch note" }],
  ]);
});

test("a refused replay says so and opens nothing", async () => {
  net = stubFetch({});
  const opened: string[] = [];
  render(menu("r0", (id) => opened.push(id)));
  fireEvent.click(screen.getByRole("button", { name: "Replay With" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Replay with @writer" }));
  fireEvent.click(await screen.findByRole("button", {name:"Start replay"}));
  await screen.findByRole("alert");
  assert.deepEqual(opened, []);
});

test("a refused prompt post reports an error without navigating", async () => {
  net = stubFetch({
    "POST /api/threads/th-1/fork": () => ({ id: "th-fork" }),
  });
  const opened: string[] = [];
  render(menu("first-run", (id) => opened.push(id)));
  fireEvent.click(screen.getByRole("button", { name: "Replay With" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Replay with @writer" }));
  fireEvent.click(await screen.findByRole("button", {name:"Start replay"}));
  await screen.findByRole("alert");
  assert.deepEqual(opened, []);
  assert.deepEqual(net.seen.find((r) => r.path.endsWith("/fork"))?.body, { through: "first-run" });
});

test("the picker closes with Escape and returns focus to Replay With", async () => {
  net = stubFetch({});
  render(menu("r0", () => {}));
  const trigger = screen.getByRole("button", { name: "Replay With" });
  fireEvent.click(trigger);
  const choice = await screen.findByRole("menuitem", { name: "Replay with @writer" });
  fireEvent.keyDown(choice, { key: "Escape" });
  assert.equal(trigger.getAttribute("aria-expanded"), "false");
  assert.equal(document.activeElement, trigger);
  assert.equal(net.seen.length, 0);
});
