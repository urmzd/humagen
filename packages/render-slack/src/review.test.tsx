import { defaultCopy, TurnView } from "@humagen/components";
import { emptyTurn, type Turn } from "@humagen/core";
import * as fx from "@humagen/core/fixtures";
import { describe, expect, it } from "vitest";
import { renderToSlack } from "./index";
import { streamUpdate } from "./stream";

const SHA = "316c56f81540c3eaca02184173df322a48aebdd9";
const words = defaultCopy.reviewReport;

describe("a review on Slack", () => {
  const out = renderToSlack(<TurnView turn={fx.reviewTurn} />);
  const json = JSON.stringify(out.blocks);

  it("is a card, never the JSON it came as", () => {
    expect(json).not.toContain('\\"format\\"');
    expect(out.blocks[0]).toMatchObject({ type: "header", text: { text: words.title } });
    expect(out.warnings).toEqual([]);
  });

  it("says what was reviewed and how many findings of each severity, counted here", () => {
    expect(json).toContain("<https://github.com/urmzd/humagen/pull/212|urmzd/humagen#212>");
    expect(json).toContain(`<https://github.com/urmzd/humagen/commit/${SHA}|Commit 316c56f>`);
    expect(json).toContain("1 error");
    expect(json).toContain("1 warning");
    expect(json).toContain("1 info");
    expect(out.text).toMatch(/^Review: 1 error, 1 warning, 1 info\. Adds horizontal scaling/);
  });

  it("shows findings in severity order, with evidence as path:line links and commands as code", () => {
    const at = (s: string) => json.indexOf(s);
    expect(at("Lease is released")).toBeLessThan(at("Refresh gap"));
    expect(at("Refresh gap")).toBeLessThan(at("Generation tracking"));
    expect(json).toContain(
      `<https://github.com/urmzd/humagen/blob/${SHA}/internal/app/slack.go#L298-L304|internal/app/slack.go:298-304>`,
    );
    expect(json).toContain("`go test -race ./internal/platform -run TestClientSync`");
  });

  it("offers each follow-up as words to ask with, never a button its composer cannot take", () => {
    expect(json).toContain(
      `${words.followUpPrompt}: \`@github-agent Show the order of unlock and close in the lease loop and propose the fix.\``,
    );
    expect(json).not.toContain(words.followUp);
    expect(json).not.toContain(words.copyMarkdown);
    expect(json).not.toContain("aui.review.follow_up");
  });

  it("ends with notes and related links", () => {
    expect(json).toContain(words.comments);
    expect(json).toContain("PostgreSQL advisory locks");
  });

  it("shows the summary on a still card while the rest arrives", () => {
    const live = JSON.stringify(renderToSlack(<TurnView turn={fx.reviewStreamingTurn} />).blocks);
    expect(live).toContain(words.preparing);
    expect(live).toContain("Adds horizontal scaling");
    expect(live).not.toContain('\\"summary\\"');
  });
});

describe("streaming a review", () => {
  const start: Turn = { ...emptyTurn("t", { name: "github-agent" }), status: "working" };
  const markdown = (prev: Turn | null, next: Turn) =>
    streamUpdate(prev, next)
      .chunks.filter((c) => c.type === "markdown_text")
      .map((c) => (c as { text: string }).text)
      .join("");

  it("holds the review back, and the final blocks draw it as a card", () => {
    const opening = { ...start, text: "```json\n" };
    const partial = { ...start, text: fx.reviewStreamingTurn.text };
    expect(markdown(start, opening)).toBe("");
    expect(markdown(opening, partial)).toBe("");
    expect(markdown(partial, { ...fx.reviewTurn, status: "working" })).toBe("");
    const final = JSON.stringify(renderToSlack(<TurnView turn={fx.reviewTurn} streamed />).blocks);
    expect(final).toContain("Lease is released before the socket closes");
  });

  it("streams held words once they turn out not to be a review", () => {
    const brace = { ...start, text: "{" };
    const other = { ...start, text: '{"answer": 42}' };
    expect(markdown(start, brace)).toBe("");
    expect(markdown(brace, other)).toBe('{"answer": 42}');
  });

  it("draws an answer that named the format but did not parse as its text", () => {
    const bad = { ...fx.reviewTurn, text: '{"format":"humagen.github.review.v1","summary":' };
    const final = JSON.stringify(renderToSlack(<TurnView turn={bad} streamed />).blocks);
    expect(final).toContain("humagen.github.review.v1");
  });
});
