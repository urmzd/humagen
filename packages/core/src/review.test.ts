import { describe, expect, it } from "vitest";
import { reviewReportJSON } from "./fixtures";
import {
  REVIEW_FORMAT,
  readReview,
  reviewCommit,
  reviewCounts,
  reviewFollowUp,
  reviewJSON,
  reviewPullRequest,
  reviewSource,
} from "./review";

const SHA = "316c56f81540c3eaca02184173df322a48aebdd9";

describe("reviewJSON", () => {
  it.each([
    { name: "plain", text: '  {"a":1}  ', want: '{"a":1}' },
    { name: "json fence", text: '```json\n{"a":1}\n```', want: '{"a":1}' },
    { name: "bare fence", text: '```\n{"a":1}\n```\n', want: '{"a":1}' },
    { name: "fence still streaming", text: '```json\n{"a":', want: '{"a":' },
    { name: "inline backticks stay", text: "```a`b\n{}\n```", want: "```a`b\n{}\n```" },
  ])("$name", ({ text, want }) => {
    expect(reviewJSON(text)).toBe(want);
  });
});

describe("readReview", () => {
  it("reads an unfenced report", () => {
    const read = readReview(reviewReportJSON);
    expect(read.kind).toBe("review");
    if (read.kind !== "review") return;
    expect(read.report.findings.map((f) => f.severity)).toEqual(["error", "warning", "info"]);
    expect(read.report.related).toHaveLength(1);
  });

  it("reads a fenced report", () => {
    expect(readReview(`\`\`\`json\n${reviewReportJSON}\n\`\`\``).kind).toBe("review");
  });

  it("reads a report with no findings, comments or related links", () => {
    const read = readReview(JSON.stringify({ format: REVIEW_FORMAT, summary: "Docs only." }));
    expect(read).toEqual({
      kind: "review",
      report: {
        format: REVIEW_FORMAT,
        summary: "Docs only.",
        comments: [],
        findings: [],
        related: [],
      },
    });
  });

  it.each([
    { name: "prose", text: "The PR looks good." },
    { name: "other JSON", text: '{"answer": 42}' },
    { name: "wrong format", text: JSON.stringify({ format: "other.v1", summary: "x" }) },
    { name: "no summary", text: JSON.stringify({ format: REVIEW_FORMAT, summary: " " }) },
    {
      name: "invalid severity",
      text: reviewReportJSON.replace('"severity": "error"', '"severity": "critical"'),
    },
    {
      name: "a finding without evidence",
      text: JSON.stringify({
        format: REVIEW_FORMAT,
        summary: "x",
        findings: [{ severity: "info", title: "t", suggestion: "s", prompt: "p", evidence: [] }],
      }),
    },
    { name: "cut off", text: reviewReportJSON.slice(0, 200) },
  ])("is text once ended: $name", ({ text }) => {
    expect(readReview(text).kind).toBe("text");
  });

  it("holds a review back while it streams, with its summary once whole", () => {
    expect(readReview("{", true)).toEqual({ kind: "partial" });
    expect(readReview('{"form', true)).toEqual({ kind: "partial" });
    expect(readReview('```json\n{\n  "format": "humagen.gi', true)).toEqual({ kind: "partial" });
    const head = `{"format":"${REVIEW_FORMAT}","summary":"Adds a \\"lease\\" loop`;
    expect(readReview(head, true)).toEqual({ kind: "partial" });
    expect(readReview(`${head}.", "comm`, true)).toEqual({
      kind: "partial",
      summary: 'Adds a "lease" loop.',
    });
  });

  it("lets other answers stream as text", () => {
    expect(readReview('{"answer": 4', true).kind).toBe("text");
    expect(readReview("Looking at the diff", true).kind).toBe("text");
    expect(readReview("```go\nfunc main() {}", true).kind).toBe("text");
  });

  it("holds an invalid report that names the format until it ends, then shows it as text", () => {
    const invalid = reviewReportJSON.replace('"severity": "info"', '"severity": "nit"');
    expect(readReview(invalid, true).kind).toBe("partial");
    expect(readReview(invalid, false).kind).toBe("text");
  });
});

describe("derived from the report", () => {
  const read = readReview(reviewReportJSON);
  if (read.kind !== "review") throw new Error("fixture is not a review");
  const report = read.report;

  it("counts findings by severity itself", () => {
    expect(reviewCounts(report)).toEqual({ error: 1, warning: 1, info: 1 });
  });

  it("finds the reviewed commit in pinned evidence", () => {
    expect(reviewCommit(report)).toEqual({
      sha: SHA,
      url: `https://github.com/urmzd/humagen/commit/${SHA}`,
    });
  });

  it("falls back to a full SHA in the summary", () => {
    expect(reviewCommit({ ...report, findings: [] })).toEqual({ sha: SHA });
    expect(reviewCommit({ ...report, findings: [], summary: "no sha" })).toBeUndefined();
  });

  it("finds the pull request from the run's source, then the report", () => {
    expect(reviewPullRequest(report)).toBeUndefined();
    expect(reviewPullRequest(report, "https://github.com/urmzd/humagen/pull/212/files")).toEqual({
      label: "urmzd/humagen#212",
      url: "https://github.com/urmzd/humagen/pull/212",
    });
    const mentioned = { ...report, comments: ["See https://github.com/a/b/pull/7."] };
    expect(reviewPullRequest(mentioned)?.label).toBe("a/b#7");
  });

  it("tags the agent ahead of the prompt", () => {
    expect(reviewFollowUp("github-agent", " Why? ")).toBe("@github-agent Why?");
  });
});

describe("reviewSource", () => {
  it.each([
    {
      name: "pinned permalink to a line",
      source: `https://github.com/o/r/blob/${SHA}/internal/app/slack.go#L298`,
      want: {
        label: "internal/app/slack.go:298",
        pinned: true,
        code: { repo: "o/r", sha: SHA, path: "internal/app/slack.go", start: 298, end: 298 },
      },
    },
    {
      name: "pinned permalink to a range",
      source: `https://github.com/o/r/blob/${SHA}/a/b.ts#L10-L14`,
      want: {
        label: "a/b.ts:10-14",
        pinned: true,
        code: { repo: "o/r", sha: SHA, path: "a/b.ts", start: 10, end: 14 },
      },
    },
    {
      name: "short sha: pinned, but its code is not read",
      source: "https://github.com/o/r/blob/316c56f/a.go#L3",
      want: { label: "a.go:3", pinned: true },
    },
    {
      name: "a whole file: no lines to show",
      source: `https://github.com/o/r/blob/${SHA}/a.go`,
      want: { label: "a.go", pinned: true },
    },
    {
      name: "branch link",
      source: "https://github.com/o/r/blob/main/README.md",
      want: { label: "README.md", pinned: false },
    },
    {
      name: "other link",
      source: "https://www.postgresql.org/docs/current/",
      want: { label: "www.postgresql.org/docs/current/" },
    },
  ])("$name", ({ source, want }) => {
    expect(reviewSource(source)).toEqual({ ...want, url: new URL(source).href });
  });

  it.each([
    { name: "a command", source: "go test -race ./...", label: "go test -race ./..." },
    { name: "file:line", source: "pkg/a.go:12", label: "pkg/a.go:12" },
    {
      name: "credentials",
      source: "https://u:p@github.com/o/r",
      label: "https://u:p@github.com/o/r",
    },
    { name: "a script link", source: "javascript:alert(1)", label: "javascript:alert(1)" },
  ])("is text with no link: $name", ({ source, label }) => {
    expect(reviewSource(source)).toEqual({ label });
  });
});
