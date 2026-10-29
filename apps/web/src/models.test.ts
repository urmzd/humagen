import assert from "node:assert/strict";
import { test } from "node:test";
import { chainProblem, chainWords, starterPolicy } from "./models.ts";

const m = (provider: string, model: string) => ({ provider, model });

test("a chain is refused before saving when it cannot run", () => {
  assert.equal(chainProblem([m("anthropic", "claude-sonnet-5-5"), m("openai", "gpt-5.1")]), null);
  assert.match(chainProblem([]) ?? "", /at least one/);
  assert.match(chainProblem([m("anthropic", "")]) ?? "", /primary model/);
  assert.match(chainProblem([m("anthropic", "x"), m("anthropic", "x")]) ?? "", /repeats/);
  assert.match(
    chainProblem([m("anthropic", "x")], ["rate_limit"]) ?? "",
    /once there is a fallback/,
  );
  assert.match(chainProblem([m("policy", "standard")]) ?? "", /not a policy/);
  const five = ["a", "b", "c", "d", "e", "f"].map((x) => m("anthropic", x));
  assert.match(chainProblem(five) ?? "", /At most 4/);
});

test("a chain reads as a sentence", () => {
  assert.equal(chainWords([m("anthropic", "sonnet")]), "sonnet");
  assert.equal(
    chainWords([m("anthropic", "sonnet"), m("openai", "gpt")]),
    "sonnet, then gpt, on any recoverable error",
  );
  assert.equal(
    chainWords([m("anthropic", "sonnet"), m("openai", "gpt")], ["unavailable", "rate_limit"]),
    "sonnet, then gpt, on outages, rate limits",
  );
});

test("a starter policy takes a free name and is valid", () => {
  const p = starterPolicy(["standard"]);
  assert.equal(p.name, "standard-2");
  assert.equal(chainProblem(p.models, p.fallback_on), null);
});
