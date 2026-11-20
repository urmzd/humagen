import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AgentLookEditor } from "./AgentLookEditor.tsx";
import { agentLook, configureAgentHues } from "./agentHues.ts";
import type { AgentLook, AgentLookView } from "./api.ts";
import { agentShown } from "./branding.ts";
import { stubFetch } from "./dom.ts";

const choices = {
  shapes: ["clover", "ghost", "star"],
  faces: ["eyes", "mouth"],
  finishes: ["fabric", "plastic", "smooth", "flat"],
  hats: ["none", "beret", "beanie", "party", "crown"],
  glasses: ["none", "round", "square", "shades"],
};
const view = (look: AgentLook, hue = 60): AgentLookView => ({
  agent: "barbara",
  look,
  hue,
  taken: [0, 120],
  choices,
});

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
  configureAgentHues(undefined);
});

test("a look is chosen, previewed and saved, and every mark takes the new name", async () => {
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
  const save = screen.getByText("Save Look") as HTMLButtonElement;
  assert.ok(save.disabled, "nothing to save before a change");

  fireEvent.change(screen.getByPlaceholderText("barbara"), { target: { value: "  Barbara " } });
  assert.equal(screen.getByTestId("look-name").textContent, "Barbara");
  fireEvent.click(screen.getByLabelText("ghost"));
  fireEvent.click(screen.getByText("Crown"));
  const taken = screen.getByLabelText("Hue 120") as HTMLButtonElement;
  assert.ok(taken.disabled, "another agent's color cannot be picked");
  fireEvent.click(screen.getByLabelText("Hue 270"));
  assert.ok(!save.disabled);
  fireEvent.click(save);

  await screen.findByText("Look saved");
  assert.deepEqual(saved, [
    { look: { display_name: "Barbara", shape: "ghost", hat: "crown" }, hue: 270 },
  ]);
  assert.equal(agentLook("barbara")?.display_name, "Barbara");
  assert.equal(agentShown("barbara"), "Barbara");
});

test("a taken name is explained, and Use Default Look clears every choice", async () => {
  net = stubFetch({
    "GET /api/agents/barbara/look": () => view({ display_name: "Barbara", hat: "beret" }),
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
  fireEvent.click(screen.getByText("Save Look"));
  await screen.findByText("Another agent is called that. Pick another name.");

  fireEvent.click(screen.getByText("Use Default Look"));
  assert.equal(input.value, "");
  assert.ok(screen.getByText("Default"));
});

test("someone who may not change the agent sees its look, read only", async () => {
  net = stubFetch({ "GET /api/agents/barbara/look": () => view({ hat: "crown" }) });
  render(<AgentLookEditor name="barbara" canWrite={false} />);
  await screen.findByText("Only the agent's owners can change how it looks.");
  assert.equal(screen.queryByText("Save Look"), null);
  await waitFor(() =>
    assert.ok((screen.getByLabelText("ghost") as HTMLButtonElement).closest("fieldset[disabled]")),
  );
});

test("a greeting and starters are kept with the look, blank starters left out", async () => {
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
  fireEvent.click(screen.getByText("Save Look"));
  await screen.findByText("Look saved");
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
