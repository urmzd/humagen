import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { useState } from "react";
import type { Me, ModelRef } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { ModelChain } from "./ModelChain.tsx";
import { SettingsNav } from "./Settings.tsx";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

const providers = () => ({
  providers: [
    { name: "anthropic", declared: true },
    { name: "offline", declared: true },
    { name: "openai", declared: false },
  ],
});

function Harness({ start, seen }: { start: ModelRef[]; seen: ModelRef[][] }) {
  const [models, setModels] = useState(start);
  return (
    <ModelChain
      models={models}
      fallbackOn={[]}
      editable
      onChange={(next) => {
        seen.push(next);
        setModels(next);
      }}
    />
  );
}

const chain: ModelRef[] = [
  { provider: "anthropic", model: "claude-sonnet-5-5" },
  { provider: "anthropic", model: "claude-haiku-5-5" },
  { provider: "openai", model: "gpt-5.1" },
];

test("a model moves with the arrow keys on its handle and keeps the keyboard there", async () => {
  net = stubFetch({ "GET /api/model-providers": providers });
  const seen: ModelRef[][] = [];
  render(<Harness start={chain} seen={seen} />);
  const grip = screen.getByRole("button", { name: /^Reorder claude-haiku-5-5, fallback 1/ });
  grip.focus();
  fireEvent.keyDown(grip, { key: "ArrowUp" });
  assert.deepEqual(
    seen.at(-1)?.map((m) => m.model),
    ["claude-haiku-5-5", "claude-sonnet-5-5", "gpt-5.1"],
  );
  assert.match(
    document.activeElement?.getAttribute("aria-label") ?? "",
    /^Reorder claude-haiku-5-5, primary/,
  );
  assert.ok(screen.getByText("claude-haiku-5-5 is now primary."));
  fireEvent.keyDown(document.activeElement as Element, { key: "End" });
  assert.deepEqual(
    seen.at(-1)?.map((m) => m.model),
    ["claude-sonnet-5-5", "gpt-5.1", "claude-haiku-5-5"],
  );
});

test("a model is removed by its icon, and a single model has no handle or remove", () => {
  net = stubFetch({ "GET /api/model-providers": providers });
  const seen: ModelRef[][] = [];
  render(<Harness start={chain.slice(0, 2)} seen={seen} />);
  fireEvent.click(screen.getByRole("button", { name: "Remove claude-sonnet-5-5" }));
  assert.deepEqual(seen.at(-1), [{ provider: "anthropic", model: "claude-haiku-5-5" }]);
  assert.equal(screen.queryByRole("button", { name: /^Reorder/ }), null);
  assert.equal(screen.queryByRole("button", { name: /^Remove/ }), null);
});

test("models are picked from provider groups, and a provider not set up says so", async () => {
  net = stubFetch({ "GET /api/model-providers": providers });
  const seen: ModelRef[][] = [];
  render(<Harness start={chain.slice(0, 1)} seen={seen} />);
  fireEvent.click(
    screen.getByRole("button", { name: "Model, primary: claude-sonnet-5-5 on Anthropic" }),
  );
  const openai = await screen.findByRole("group", { name: "OpenAI" });
  await within(openai).findByText("Not set up");
  const anthropic = screen.getByRole("group", { name: "Anthropic" });
  assert.equal(
    within(anthropic)
      .getByRole("button", { name: /claude-sonnet-5-5/ })
      .getAttribute("aria-pressed"),
    "true",
  );
  fireEvent.click(within(openai).getByRole("button", { name: "gpt-5.1" }));
  assert.deepEqual(seen.at(-1), [{ provider: "openai", model: "gpt-5.1" }]);
});

test("a model that is not listed is typed and named under the provider chosen", async () => {
  net = stubFetch({ "GET /api/model-providers": providers });
  const seen: ModelRef[][] = [];
  render(<Harness start={chain.slice(0, 1)} seen={seen} />);
  fireEvent.click(screen.getByRole("button", { name: /^Model, primary/ }));
  fireEvent.change(await screen.findByLabelText("Search models, or type a model's name"), {
    target: { value: "llama3.2" },
  });
  const openai = screen.getByRole("group", { name: "OpenAI" });
  assert.equal(screen.queryByRole("group", { name: "Offline" }), null, "offline takes no name");
  fireEvent.click(within(openai).getByRole("button", { name: "Use “llama3.2”" }));
  assert.deepEqual(seen.at(-1), [{ provider: "openai", model: "llama3.2" }]);
});

test("settings are split into your own and the workspace's, without models or schedules", () => {
  const me = {
    principal: { subject: "ada", tenant: "acme", kind: "user", name: "Ada", roles: ["admin"] },
    permissions: ["people:read", "projects:read", "schedules:read", "runs:read", "projects:govern"],
  } as Me;
  render(<SettingsNav me={me} section="profile" onSection={() => {}} onNavigate={() => {}} />);
  const names = (group: string) =>
    [...screen.getByRole("group", { name: group }).querySelectorAll("button, a")].map(
      (el) => el.textContent,
    );
  assert.deepEqual(names("You"), ["Profile", "Notifications", "Appearance"]);
  const admin = names("Admin");
  assert.deepEqual(admin.slice(0, 4), ["Workspace", "Agent routing", "Limits", "Access"]);
  assert.ok(admin.includes("People") && admin.includes("Audit"));
  assert.ok(!admin.includes("Models") && !admin.includes("Schedules"));
});
