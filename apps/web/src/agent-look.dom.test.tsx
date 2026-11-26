import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AgentLookEditor } from "./AgentLookEditor.tsx";
import { agentLook, configureAgentHues } from "./agentHues.ts";
import type { AgentLook, AgentLookView } from "./api.ts";
import { agentShown } from "./branding.ts";
import { stubFetch } from "./dom.ts";

// The server still lists the old bot choices; the editor no longer offers them.
const choices = { shapes: [], faces: [], finishes: [], hats: [], glasses: [] };
const view = (look: AgentLook, hue = 60): AgentLookView => ({
  agent: "barbara",
  look,
  hue,
  taken: [0, 120],
  choices,
});

const copy = defaultCopy.agentLook;
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
  configureAgentHues(undefined);
});

test("an appearance is chosen, previewed and saved, and every mark takes the new name", async () => {
  const saved: { look: AgentLook; hue?: number }[] = [];
  net = stubFetch({
    "GET /api/agents/barbara/look": () => view({}),
    "PUT /api/agents/barbara/look": (body) => {
      const b = body as { look: AgentLook; hue?: number };
      saved.push(b);
      return view(b.look, b.hue ?? 60);
    },
  });
  render(<AgentLookEditor name="barbara" canWrite />);
  await screen.findByText("Default");
  const save = screen.getByText(copy.save) as HTMLButtonElement;
  assert.ok(save.disabled, "nothing to save before a change");

  fireEvent.change(screen.getByPlaceholderText("barbara"), { target: { value: "  Barbara " } });
  assert.equal(screen.getByTestId("look-name").textContent, "Barbara");
  fireEvent.click(screen.getByRole("button", { name: copy.characters.detective }));
  const taken = screen.getByLabelText("Hue 120") as HTMLButtonElement;
  assert.ok(taken.disabled, "another agent's color cannot be picked");
  fireEvent.click(screen.getByLabelText("Hue 270"));
  assert.ok(!save.disabled);
  fireEvent.click(save);

  await screen.findByText(copy.saved);
  assert.deepEqual(saved, [
    { look: { display_name: "Barbara", character: "detective" }, hue: 270 },
  ]);
  assert.equal(agentLook("barbara")?.display_name, "Barbara");
  assert.equal(agentShown("barbara"), "Barbara");
});

test("a taken name is explained, and Use Default Appearance clears every choice", async () => {
  net = stubFetch({
    "GET /api/agents/barbara/look": () => view({ display_name: "Barbara", character: "writer" }),
    "PUT /api/agents/barbara/look": (body) => {
      const b = body as { look: AgentLook };
      if (b.look.display_name === "Rex")
        return new Response(JSON.stringify({ error: "another agent is called that" }), {
          status: 409,
        });
      return view(b.look);
    },
  });
  render(<AgentLookEditor name="barbara" canWrite />);
  const input = (await screen.findByDisplayValue("Barbara")) as HTMLInputElement;
  fireEvent.change(input, { target: { value: "Rex" } });
  fireEvent.click(screen.getByText(copy.save));
  await screen.findByText("Another agent is called that. Pick another name.");

  fireEvent.click(screen.getByText(copy.reset));
  assert.equal(input.value, "");
  assert.ok(screen.getByText("Default"));
});

test("someone who may not change the agent sees its appearance, read only", async () => {
  net = stubFetch({ "GET /api/agents/barbara/look": () => view({ character: "writer" }) });
  render(<AgentLookEditor name="barbara" canWrite={false} />);
  await screen.findByText("Only the agent's owners can change how it looks.");
  assert.equal(screen.queryByText(copy.save), null);
  await waitFor(() =>
    assert.ok(
      (
        screen.getByRole("button", { name: copy.characters.detective }) as HTMLButtonElement
      ).closest("fieldset[disabled]"),
    ),
  );
});

test("a greeting and starters are kept with the appearance, blank starters left out", async () => {
  const saved: { look: AgentLook }[] = [];
  net = stubFetch({
    "GET /api/agents/barbara/look": () => view({}),
    "PUT /api/agents/barbara/look": (body) => {
      saved.push(body as { look: AgentLook });
      return view((body as { look: AgentLook }).look);
    },
  });
  render(<AgentLookEditor name="barbara" canWrite />);
  await screen.findByText("Default");
  fireEvent.change(screen.getByLabelText(/^Greeting/), {
    target: { value: " I keep customer performance reports current. " },
  });
  assert.equal(
    screen.getByTestId("look-greeting").textContent,
    "I keep customer performance reports current.",
  );
  fireEvent.change(screen.getByLabelText("Starter 1 title"), {
    target: { value: "Summarize the latest reports" },
  });
  fireEvent.change(screen.getByLabelText("Starter 1 message"), {
    target: { value: "Summarize the latest customer reports and flag gaps." },
  });
  fireEvent.click(screen.getByText(copy.save));
  await screen.findByText(copy.saved);
  assert.deepEqual(saved[0]?.look, {
    greeting: "I keep customer performance reports current.",
    starters: [
      {
        title: "Summarize the latest reports",
        prompt: "Summarize the latest customer reports and flag gaps.",
      },
    ],
  });
});
