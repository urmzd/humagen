import type { Turn } from "@humagen/core";
import type { Binding, ClientRecord } from "./humagen.ts";

export function bridgePolicy(client: ClientRecord | null | undefined, binding?: Binding) {
  const enabled = client !== null && (client === undefined || client.enabled);
  const direction = binding?.direction ?? "two_way";
  const known = direction === "two_way" || direction === "inbound" || direction === "outcomes";
  return {
    bound: !!binding,
    inbound: enabled && known && direction !== "outcomes",
    passive: !!binding && direction === "inbound",
    outbound:
      enabled &&
      known &&
      direction !== "inbound" &&
      client?.capabilities?.deliver_outcomes !== false,
  };
}
export function subscribed(client: ClientRecord | null | undefined, event: string) {
  return !client?.capabilities || client.capabilities.events.includes(event);
}

// A bridge carries outcomes and decisions, never Activity or retained source
// metadata. The server independently rejects private-context delivery.
export function bridgeOutcome(turn: Turn): Turn {
  return {
    ...turn,
    tasks: [],
    citations: [],
    notices: [],
    reasoning: undefined,
    activity: undefined,
    attachments: undefined,
    source: undefined,
    usage: undefined,
    handoff: undefined,
    failure: turn.failure
      ? { kind: turn.failure.kind, message: turn.failure.message, retryable: false }
      : undefined,
  };
}
