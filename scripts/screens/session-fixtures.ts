import assert from "node:assert/strict";
import type {
  AgentSession,
  AgentSpec,
  CreateSessionAlertBody,
  SessionInput,
} from "../../packages/client/src/generated/types.ts";

export type Call = <T>(method: string, path: string, body?: unknown) => Promise<T>;
export const REVIEW_ASK = "Review the canonical session fixture.";
export const REVIEW_TITLE = "Launch readiness review";
export const FINDING_TITLE = "Confirm the owner before publication";
export const ALERT_TITLE = "Confirm the launch briefing owner";
export const REVIEW_ANSWER = JSON.stringify({
  format: "humagen.review.v2",
  title: REVIEW_TITLE,
  verdict: "request_changes",
  scope: { commit: "316c56f81540c3eaca02184173df322a48aebdd9" },
  summary:
    "The launch date is unchanged. One owner decision is required before sharing the briefing.",
  comments: ["This fixture uses a local model and does not publish to GitHub."],
  findings: [
    {
      id: "briefing-owner",
      severity: "warning",
      title: FINDING_TITLE,
      suggestion: "Name the briefing owner and keep publication subject to human approval.",
      location: { path: "plan.md", line: 4, side: "RIGHT" },
      change: "- Developer owns the briefing",
      evidence: [
        { observation: "The draft does not identify the briefing owner.", source: "plan.md:4" },
      ],
      prompt: "What evidence should we confirm before naming the briefing owner?",
    },
  ],
  related: [],
});

export interface SessionFixtures {
  work: AgentSession;
  review: AgentSession;
  approval: AgentSession;
  child: AgentSession;
}

/** Real session, run, approval, alert and file APIs, backed by the existing local model. */
export async function seedSessionFixtures(call: Call): Promise<SessionFixtures> {
  for (const spec of [
    { name: "screens-writer", tools: ["file_write", "file_read", "file_list"] },
    { name: "screens-reviewer", tools: [] },
    {
      name: "screens-approval",
      tools: ["file_write", "file_read", "file_list"],
      approvals: ["file_write"],
      self_approval: true,
    },
  ]) {
    await call("POST", "/api/agents", {
      description: "Local fixture for console visual review.",
      provider: "screens-native",
      model: "screens-native",
      system_prompt: "Follow the deterministic local fixture response.",
      ...spec,
    } satisfies AgentSpec);
  }
  const start = async (input: SessionInput, status: AgentSession["status"] = "done") => {
    const first = await call<AgentSession>("POST", "/api/sessions", input);
    assert.match(first.id, /^s_/);
    for (let i = 0; i < 300; i++) {
      const session = await call<AgentSession>("GET", `/api/sessions/${first.id}`);
      if (session.status === status) return session;
      assert.ok(
        !["failed", "stopped"].includes(session.status),
        `Fixture ${input.agent}: ${session.status}`,
      );
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    throw new Error(`Fixture ${first.id} did not reach ${status}`);
  };
  const work = await start({
    agent: "screens-writer",
    text: "Write a multi-step plan for launch.",
    project: "Launch",
  });
  const review = await start({ agent: "screens-reviewer", text: REVIEW_ASK, project: "Launch" });
  const child = await start({
    agent: "screens-reviewer",
    parent: work.id,
    label: "review",
    text: REVIEW_ASK,
  });
  const approval = await start(
    {
      agent: "screens-approval",
      text: "Write a multi-step plan after approval.",
      project: "Launch",
    },
    "waiting",
  );
  assert.ok(
    approval.messages.some((message) => message.ask),
    "The approval fixture must be durably parked",
  );
  const anchor = work.messages.find((message) => message.run)?.id;
  assert.ok(anchor, "The alert must anchor a real session message");
  await call("POST", `/api/sessions/${work.id}/alerts`, {
    title: ALERT_TITLE,
    body: "Review the draft and confirm its owner before publication.",
    priority: "normal",
    message: anchor,
  } satisfies CreateSessionAlertBody);
  return { work, review, child, approval };
}
