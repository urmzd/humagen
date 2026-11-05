import assert from "node:assert/strict";
import { test } from "node:test";
import type { Revision } from "./api.ts";
import { keptPick, modelName, namedIn, offersFor } from "./modelPick.ts";

// DES-067: the model and effort a message may pick.

const agent = (name: string, choices?: { models?: string[]; efforts?: string[] }): Revision => ({
  spec: { name, model: "m", provider: "p", system_prompt: "x", choices },
  rev: 1,
  hash: "h",
  created_at: "",
  created_by: "",
});

const agents = [
  agent("lead", { models: ["openai/gpt", "anthropic/opus"], efforts: ["high", "low"] }),
  agent("helper", { efforts: ["max", "medium"] }),
  agent("plain"),
];

test("offers what the named agents offer, together, efforts lightest first", () => {
  assert.deepEqual(offersFor(agents, ["lead", "helper"]), {
    models: ["anthropic/opus", "openai/gpt"],
    efforts: ["low", "medium", "high", "max"],
  });
  assert.deepEqual(offersFor(agents, ["plain"]), { models: [], efforts: [] });
  assert.deepEqual(offersFor(agents, []), { models: [], efforts: [] });
});

test("reads the agents a draft tags", () => {
  assert.deepEqual(namedIn("@lead and @Helper, not a@plain or @nobody", agents), [
    "lead",
    "helper",
  ]);
});

test("drops a pick no longer offered", () => {
  const offers = offersFor(agents, ["helper"]);
  assert.deepEqual(keptPick({ model: "openai/gpt", effort: "max" }, offers), { effort: "max" });
});

test("names a model without its provider", () => {
  assert.equal(modelName("anthropic/opus"), "opus");
  assert.equal(modelName("bare"), "bare");
});
