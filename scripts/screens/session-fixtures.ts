import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import type {
  AgentSession,
  AgentSpec,
  ClientInput,
  CreateSessionAlertBody,
  SessionInput,
} from "../../packages/client/src/generated/types.ts";
import { readReview } from "../../packages/core/src/review.ts";

export type Call = <T>(method: string, path: string, body?: unknown) => Promise<T>;
export const REVIEW_ASK = "Review the canonical session fixture.";
export const REVIEW_TITLE = "Launch readiness review";
export const FINDING_TITLE = "Confirm the owner before publication";
export const ALERT_TITLE = "Confirm the launch briefing owner";
export const REVIEW_CONTEXT = JSON.parse(
  readFileSync(new URL("./fixture-host/review-context.json", import.meta.url), "utf8"),
) as {
  owner: string;
  repo: string;
  number: number;
  context: { subject: { head: { sha: string }; html_url: string }; files: { filename: string }[] };
};
export const REVIEW_STEP = "screens-review-context";
export const REVIEW_ANSWER = JSON.stringify({
  format: "humagen.review.v2",
  title: REVIEW_TITLE,
  verdict: "request_changes",
  scope: { commit: REVIEW_CONTEXT.context.subject.head.sha },
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
        {
          observation: "The draft does not identify the briefing owner.",
          source: "plan.md",
          step: REVIEW_STEP,
        },
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
    { name: "screens-reviewer", tools: ["github_read_context"] },
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
  // The screenshot host supplies only the in-memory GitHub API. The normal
  // client and action grants still authorize this recorded evidence read.
  await call("POST", "/api/clients", {
    id: "screens-github",
    name: "Local review evidence",
    surface: "github",
    enabled: true,
    identity: { owners: [REVIEW_CONTEXT.owner] },
    agents: ["screens-reviewer"],
    github: { read_repositories: [`${REVIEW_CONTEXT.owner}/${REVIEW_CONTEXT.repo}`] },
    credentials: {
      token: { ref: "env:HUMAGEN_SCREENS_GITHUB_TOKEN" },
      signing_secret: { ref: "env:HUMAGEN_SCREENS_GITHUB_SECRET" },
    },
  } satisfies ClientInput);
  const capabilities = await call<{ version: number }>(
    "GET",
    "/api/clients/screens-github/capabilities",
  );
  await call("PUT", "/api/clients/screens-github/capabilities", {
    version: capabilities.version,
    events: [],
    actions: ["github_read_context"],
    deliver_outcomes: false,
  });
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
  assertProjectedReview(review);
  const child = await start({
    agent: "screens-reviewer",
    parent: work.id,
    label: "review",
    text: REVIEW_ASK,
  });
  assertProjectedReview(child);
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

/** A completed run alone is insufficient: the host must retain the v2 card and receipt. */
export function assertProjectedReview(session: AgentSession): void {
  const answer = session.messages.findLast((message) => message.run?.agent === "screens-reviewer")
    ?.run?.answer;
  const parsed = readReview(answer ?? "");
  assert.equal(parsed.kind, "review", "The host rejected the review fixture's provenance");
  if (parsed.kind !== "review") return;
  assert.equal(parsed.report.format, "humagen.review.v2");
  assert.equal(parsed.report.scope?.commit, REVIEW_CONTEXT.context.subject.head.sha);
  assert.equal(parsed.report.title, REVIEW_TITLE);
  assert.equal(parsed.report.findings[0]?.evidence[0]?.step, REVIEW_STEP);
}
