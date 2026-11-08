import assert from "node:assert/strict";
import { test } from "node:test";
import type { Locator, Page } from "playwright-core";
import { fixtureResponse } from "./native.ts";
import type { SessionFixtures } from "./session-fixtures.ts";
import { sessionScreens } from "./sessions.ts";
import { viewports } from "./viewports.ts";

const ids = {
  work: { id: "s_work" },
  review: { id: "s_review" },
  approval: { id: "s_approval" },
  child: { id: "s_child" },
} as SessionFixtures;

/** Runs selector actions without starting Playwright or claiming browser validation. */
function recorder({ pickerVisible = true, fileContent = "" } = {}) {
  const calls: { action: string; selector: string }[] = [];
  const role = (name: string, options?: { name?: string | RegExp }) =>
    `${name}:${String(options?.name ?? "")}`;
  const locate = (selector: string): Locator =>
    ({
      getByRole: (name: string, options?: { name?: string | RegExp }) =>
        locate(`${selector}>${role(name, options)}`),
      getByText: (text: string) => locate(`${selector}>text:${text}`),
      locator: (next: string) => locate(`${selector}>${next}`),
      isVisible: async () => (selector.endsWith("button:Files") ? pickerVisible : true),
      allTextContents: async () => ["Files", "Sources", "Steps", "Sessions"],
      evaluate: async () => true,
      boundingBox: async () => ({ x: 0, width: 390 }),
      inputValue: async () => fileContent,
      scrollIntoViewIfNeeded: async () => calls.push({ action: "scroll", selector }),
      first: () => locate(selector),
      or: (other: Locator) => locate(`${selector}|${String(other)}`),
      toString: () => selector,
      waitFor: async () => calls.push({ action: "wait", selector }),
      click: async () => calls.push({ action: "click", selector }),
    }) as unknown as Locator;
  const page = {
    getByRole: (name: string, options?: { name?: string | RegExp }) => locate(role(name, options)),
    locator: locate,
    viewportSize: () => ({ width: 390, height: 844 }),
  } as unknown as Page;
  return { page, calls };
}

test("the canonical session suite keeps 20 unique states at all five widths", () => {
  const screens = sessionScreens(ids);
  assert.equal(screens.length, 20);
  assert.equal(new Set(screens.map((screen) => screen.name)).size, 20);
  assert.equal(screens.length * viewports().length, 100);
  for (const [name, path] of Object.entries({
    new: "/",
    sessions: "/sessions",
    alerts: "/alerts",
    agents: "/agents",
    connections: "/connections",
    artifacts: "/artifacts",
    settings: "/settings/appearance",
  })) {
    assert.equal(screens.find((screen) => screen.name === name)?.path, path);
  }
  assert.ok(!screens.some((screen) => screen.name === "library"));
});

test("Files opens the actual native multi-step draft through either responsive picker", async () => {
  const response = fixtureResponse({
    messages: [
      { role: "user", content: "Write a multi-step plan for launch." },
      { role: "assistant", content: "Step one" },
      { role: "assistant", content: "Step two" },
    ],
  });
  assert.ok("tool_calls" in response.delta);
  const write = response.delta.tool_calls?.[0];
  assert.equal(write?.function.name, "file_write");
  const file = JSON.parse(write?.function.arguments ?? "{}");
  assert.equal(file.path, "plan.md");
  const scenario = sessionScreens(ids).find((screen) => screen.name === "session-details-files");
  assert.equal(scenario?.path, "/sessions/s_work?panel=files");
  assert.ok(scenario?.act);
  for (const pickerVisible of [true, false]) {
    const { page, calls } = recorder({ pickerVisible, fileContent: file.content });
    await scenario.act(page);
    assert.ok(
      calls.some(
        (call) =>
          call.action === "click" &&
          call.selector ===
            (pickerVisible
              ? "dialog:Files>button:plan.md"
              : 'dialog:Details>[data-slot="file-browser"]>button:plan.md'),
      ),
    );
    assert.ok(
      calls.some(
        (call) => call.action === "click" && call.selector === "dialog:Details>button:Preview",
      ),
    );
    assert.deepEqual(calls.at(-1), {
      action: "scroll",
      selector: "dialog:Details>text:Developer owns the briefing",
    });
  }
  await assert.rejects(scenario.act(recorder({ fileContent: "A stale or missing draft" }).page), {
    message: /native work-session file must contain/,
  });
});

test("navigation checks every current destination as a real link", async () => {
  const { page, calls } = recorder();
  await sessionScreens(ids)
    .find((screen) => screen.name === "navigation")
    ?.act?.(page);
  assert.deepEqual(
    calls.filter((call) => call.action === "wait").map((call) => call.selector),
    ["New", "Sessions", "Alerts", "Agents", "Connections", "Artifacts", "Settings"].map(
      (name) => `navigation:Sections>link:/^${name}(?:,|$)/`,
    ),
  );
});

test("artifacts waits for loaded content and Data sources keeps its stable capture name", async () => {
  const { page, calls } = recorder();
  const screens = sessionScreens(ids);
  await screens.find((screen) => screen.name === "artifacts")?.act?.(page);
  await screens.find((screen) => screen.name === "connections-knowledge")?.act?.(page);
  assert.deepEqual(calls, [
    { action: "wait", selector: "heading:Deliverables" },
    { action: "wait", selector: "searchbox:Search artifacts" },
    { action: "wait", selector: "list:Artifacts|heading:No artifacts yet" },
    { action: "click", selector: "tab:Data sources" },
    { action: "wait", selector: "heading:BigQuery" },
  ]);
});

test("session-list readiness requires a native link to the seeded session", async () => {
  const { page, calls } = recorder();
  await sessionScreens(ids)
    .find((screen) => screen.name === "sessions")
    ?.act?.(page);
  assert.ok(
    calls.some(
      (call) => call.action === "wait" && call.selector === 'main a[href="/sessions/s_work"]',
    ),
  );
});
