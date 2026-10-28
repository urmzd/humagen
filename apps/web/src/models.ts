import type { ModelPolicy, ModelRef } from "./api.ts";

/** The provider an agent names to run on a workspace model policy. */
export const POLICY = "policy";

/** The most fallbacks a chain may hold. The platform refuses more. */
export const MAX_FALLBACKS = 4;

/** A model each provider starts on when picked. */
export const defaultModel: Record<string, string> = {
  offline: "offline",
  anthropic: "claude-sonnet-5-5",
  ollama: "llama3.2",
};

export const providers = [
  { value: "anthropic", label: "anthropic" },
  { value: "ollama", label: "ollama", hint: "A model running on this machine." },
  { value: "offline", label: "offline", hint: "Calls no model. For trying the platform out." },
];

/** When a call moves to the next model, in words. `any` is the default and stands alone. */
export const conditions: { value: string; label: string; hint: string }[] = [
  { value: "unavailable", label: "Outages", hint: "A 5xx, or an overloaded provider." },
  { value: "rate_limit", label: "Rate limits", hint: "A rate limit or an exhausted quota." },
  {
    value: "transient",
    label: "Timeouts and resets",
    hint: "Any error a retry could fix, outages and rate limits included.",
  },
  { value: "auth", label: "Credential errors", hint: "A refused or missing key." },
  {
    value: "context_length",
    label: "Prompt too long",
    hint: "The next model may have a larger window.",
  },
  {
    value: "content_filter",
    label: "Blocked by a safety filter",
    hint: "The next provider may have another policy.",
  },
];

const same = (a: ModelRef, b: ModelRef) =>
  a.provider.toLowerCase() === b.provider.toLowerCase() && a.model.trim() === b.model.trim();

/** What is wrong with a chain, before the server says so. */
export function chainProblem(models: ModelRef[], fallbackOn: string[] = []): string | null {
  if (models.length === 0) return "Name at least one model.";
  for (const [i, m] of models.entries()) {
    if (!m.model.trim())
      return i === 0 ? "The primary model needs a name." : `Fallback ${i} needs a model.`;
    if (m.provider === POLICY) return "A chain names models, not a policy.";
    if (models.slice(0, i).some((x) => same(x, m)))
      return `Fallback ${i} repeats a model above it.`;
  }
  if (models.length - 1 > MAX_FALLBACKS) return `At most ${MAX_FALLBACKS} fallbacks.`;
  if (fallbackOn.length > 0 && models.length < 2)
    return "Choose when to fall back only once there is a fallback.";
  return null;
}

/** A policy's chain in a few words: `sonnet, then llama3.2, on outages`. */
export function chainWords(models: ModelRef[], fallbackOn: string[] = []): string {
  const names = models.map((m) => m.model).join(", then ");
  if (models.length < 2) return names;
  const when =
    fallbackOn.length === 0
      ? "on any recoverable error"
      : `on ${fallbackOn.map((c) => conditions.find((x) => x.value === c)?.label.toLowerCase() ?? c).join(", ")}`;
  return `${names}, ${when}`;
}

/** A new, sensible policy to start from. */
export function starterPolicy(taken: string[]): ModelPolicy {
  let name = "standard";
  for (let n = 2; taken.includes(name); n++) name = `standard-${n}`;
  return {
    name,
    description: "",
    models: [
      { provider: "anthropic", model: "claude-sonnet-5-5" },
      { provider: "anthropic", model: "claude-haiku-5-5" },
    ],
    fallback_on: ["unavailable", "rate_limit"],
  };
}
