/**
 * The structured review an agent answers with (`humagen.github.review.v1`),
 * read from its answer text. The contract and its checks mirror
 * internal/presentation/github/review.go, which turns the same answer into a
 * GitHub comment: a report the formatter would reject is shown as text here.
 */

/** The format id, the first key of a review report. */
export const REVIEW_FORMAT = "humagen.github.review.v1";

export type ReviewSeverity = "error" | "warning" | "info";

/** Severities in the order a review is read: what blocks first. */
export const REVIEW_SEVERITIES: readonly ReviewSeverity[] = ["error", "warning", "info"];

export interface ReviewEvidence {
  observation: string;
  /** A commit-pinned permalink, a `file:line`, or an exact command. */
  source: string;
}

export interface ReviewFinding {
  severity: ReviewSeverity;
  title: string;
  suggestion: string;
  evidence: ReviewEvidence[];
  /** What the reader can ask the agent next about this finding. */
  prompt: string;
}

export interface ReviewLink {
  title: string;
  url: string;
}

export interface ReviewReport {
  format: string;
  summary: string;
  comments: string[];
  findings: ReviewFinding[];
  related: ReviewLink[];
}

/**
 * What an answer is. `text` is drawn as Markdown; `partial` is a review still
 * arriving, with its summary once that has arrived whole; `review` is a
 * complete, valid report.
 */
export type ReviewRead =
  | { kind: "text" }
  | { kind: "partial"; summary?: string }
  | { kind: "review"; report: ReviewReport };

/**
 * The answer with surrounding space and one Markdown code fence removed, as
 * models often wrap the report in ```json ... ```. A fence still streaming has
 * no closing line yet. The same rule as ReviewJSON in review.go.
 */
export function reviewJSON(text: string): string {
  const trimmed = text.trim();
  if (!trimmed.startsWith("```")) return trimmed;
  const nl = trimmed.indexOf("\n");
  if (nl === -1 || trimmed.slice(3, nl).includes("`")) return trimmed;
  let body = trimmed.slice(nl + 1).trim();
  if (body.endsWith("```")) body = body.slice(0, -3);
  return body.trim();
}

const HEADER = `{"format":"${REVIEW_FORMAT}"`;
const SUMMARY = /"summary"\s*:\s*"((?:[^"\\]|\\.)*)"/;

const filled = (v: unknown): v is string => typeof v === "string" && v.trim() !== "";

/** The report if `value` is one the GitHub formatter would accept, else null. */
function asReport(value: unknown): ReviewReport | null {
  if (typeof value !== "object" || value === null) return null;
  const r = value as Record<string, unknown>;
  if (r.format !== REVIEW_FORMAT || !filled(r.summary)) return null;
  if (r.findings !== undefined && !Array.isArray(r.findings)) return null;
  const findings: ReviewFinding[] = [];
  for (const f of (r.findings ?? []) as Record<string, unknown>[]) {
    if (typeof f !== "object" || f === null) return null;
    if (!REVIEW_SEVERITIES.includes(f.severity as ReviewSeverity)) return null;
    if (!filled(f.title) || !filled(f.suggestion) || !filled(f.prompt)) return null;
    if (!Array.isArray(f.evidence) || f.evidence.length === 0) return null;
    const evidence: ReviewEvidence[] = [];
    for (const e of f.evidence as Record<string, unknown>[]) {
      if (typeof e !== "object" || e === null) return null;
      if (!filled(e.observation) || !filled(e.source)) return null;
      evidence.push({ observation: e.observation, source: e.source });
    }
    findings.push({
      severity: f.severity as ReviewSeverity,
      title: f.title,
      suggestion: f.suggestion,
      evidence,
      prompt: f.prompt,
    });
  }
  const comments = Array.isArray(r.comments) ? r.comments.filter(filled) : [];
  const related = Array.isArray(r.related)
    ? (r.related as Record<string, unknown>[]).flatMap((l) =>
        l && filled(l.title) && filled(l.url) ? [{ title: l.title, url: l.url }] : [],
      )
    : [];
  return { format: REVIEW_FORMAT, summary: r.summary, comments, findings, related };
}

/**
 * Reads an answer as a review. While it streams, an answer that has named the
 * format, or is still on its way to naming it, is `partial`. Once it ends, an
 * answer that does not parse as a valid report is `text`, so nothing an agent
 * wrote is hidden behind a card that cannot show it.
 */
export function readReview(text: string, streaming = false): ReviewRead {
  const body = reviewJSON(text);
  if (!body.startsWith("{")) return { kind: "text" };
  try {
    const report = asReport(JSON.parse(body));
    if (report) return { kind: "review", report };
    if (!streaming) return { kind: "text" };
  } catch {
    if (!streaming) return { kind: "text" };
  }
  const compact = body.replace(/\s+/g, "");
  const named = body.includes(REVIEW_FORMAT);
  // Before the format id arrives, only a beginning of the header is held back.
  if (!named && !HEADER.startsWith(compact)) return { kind: "text" };
  const m = SUMMARY.exec(body);
  if (!m) return { kind: "partial" };
  try {
    return { kind: "partial", summary: JSON.parse(`"${m[1]}"`) as string };
  } catch {
    return { kind: "partial" };
  }
}

/** Findings per severity, counted here, never taken from the model. */
export function reviewCounts(report: ReviewReport): Record<ReviewSeverity, number> {
  const counts: Record<ReviewSeverity, number> = { error: 0, warning: 0, info: 0 };
  for (const f of report.findings) counts[f.severity]++;
  return counts;
}

/** Lines of a file at a full commit SHA: what a pinned permalink's code is read by. */
export interface ReviewCodeRef {
  /** owner/name */
  repo: string;
  sha: string;
  path: string;
  start: number;
  end: number;
}

/** Where a piece of evidence points: a link with a short label, or text such as a command. */
export interface ReviewSource {
  label: string;
  url?: string;
  /** Pinned to a commit, so it shows the code as it was reviewed. */
  pinned?: boolean;
  /** Set for a permalink to lines at a full commit SHA, whose code can be shown in place. */
  code?: ReviewCodeRef;
}

const BLOB = /^\/([^/]+)\/([^/]+)\/blob\/([^/]+)\/(.+)$/;
const SHA = /^[0-9a-f]{7,40}$/i;
const LINES = /^L(\d+)(?:C\d+)?(?:-L(\d+)(?:C\d+)?)?$/;

function safeUrl(source: string): URL | null {
  try {
    const u = new URL(source.trim());
    if ((u.protocol !== "https:" && u.protocol !== "http:") || !u.host) return null;
    if (u.username || u.password) return null;
    return u;
  } catch {
    return null;
  }
}

/**
 * The label and link for an evidence source. A GitHub file permalink reads as
 * `path:line` (or `path:10-14`); another link as its host and path; anything
 * else, such as a test command, as the text it is, with no link.
 */
export function reviewSource(source: string): ReviewSource {
  const u = safeUrl(source);
  if (!u) return { label: source.trim() };
  const blob = u.hostname === "github.com" ? BLOB.exec(u.pathname) : null;
  if (blob) {
    const path = decodeURIComponent(blob[4] as string);
    const lines = LINES.exec(u.hash.slice(1));
    const at = lines
      ? `:${lines[1]}${lines[2] && lines[2] !== lines[1] ? `-${lines[2]}` : ""}`
      : "";
    const sha = blob[3] as string;
    const code =
      lines && /^[0-9a-f]{40}$/i.test(sha)
        ? {
            repo: `${blob[1]}/${blob[2]}`,
            sha: sha.toLowerCase(),
            path,
            start: Number(lines[1]),
            end: Math.max(Number(lines[1]), Number(lines[2] ?? lines[1])),
          }
        : undefined;
    return {
      label: `${path}${at}`,
      url: u.href,
      pinned: SHA.test(sha),
      ...(code ? { code } : {}),
    };
  }
  const path = u.pathname === "/" ? "" : u.pathname;
  return { label: `${u.host}${path}`, url: u.href };
}

/** The commit a review read, from its pinned evidence or a full SHA in its words. */
export function reviewCommit(report: ReviewReport): { sha: string; url?: string } | undefined {
  for (const f of report.findings) {
    for (const e of f.evidence) {
      const u = safeUrl(e.source);
      const blob = u?.hostname === "github.com" ? BLOB.exec(u.pathname) : null;
      if (blob && SHA.test(blob[3] as string)) {
        const sha = (blob[3] as string).toLowerCase();
        return { sha, url: `https://github.com/${blob[1]}/${blob[2]}/commit/${sha}` };
      }
    }
  }
  const words = [report.summary, ...report.comments].join("\n");
  const sha = /\b[0-9a-f]{40}\b/i.exec(words)?.[0];
  return sha ? { sha: sha.toLowerCase() } : undefined;
}

const PULL = /^\/([^/]+)\/([^/]+)\/pull\/(\d+)/;

/**
 * The pull request a review is about: the run's own source when it names one,
 * else the first pull request link in the report.
 */
export function reviewPullRequest(
  report: ReviewReport,
  hint?: string,
): { label: string; url: string } | undefined {
  const words = [report.summary, ...report.comments, ...report.related.map((l) => l.url)];
  const candidates = [hint ?? "", ...words.flatMap((w) => w.match(/https?:\/\/\S+/g) ?? [])];
  for (const c of candidates) {
    const u = safeUrl(c.replace(/[).,;]+$/, ""));
    const m = u?.hostname === "github.com" ? PULL.exec(u.pathname) : null;
    if (m) {
      return {
        label: `${m[1]}/${m[2]}#${m[3]}`,
        url: `https://github.com/${m[1]}/${m[2]}/pull/${m[3]}`,
      };
    }
  }
  return undefined;
}

/** The words the composer gets for a finding's follow-up: the agent tagged, then the prompt. */
export function reviewFollowUp(agent: string, prompt: string): string {
  return `@${agent} ${prompt.trim()}`;
}

/** The words `reviewMarkdown` writes: the console's copy, `copy.reviewReport`. */
export interface ReviewMarkdownWords {
  count: Record<ReviewSeverity, (n: number) => string>;
  group: Record<ReviewSeverity, string>;
  summary: string;
  comments: string;
  findings: string;
  suggestion: string;
  evidence: string;
  related: string;
  noFindings: string;
}

const ICON: Record<ReviewSeverity, string> = { error: "🔴", warning: "🟡", info: "🔵" };

/** Model text kept inside its table cell or list item: one line, no stray pipes. */
const cell = (s: string) => s.split(/\s+/).filter(Boolean).join(" ").replace(/\|/g, "\\|");

/**
 * The review as GitHub Markdown, in the layout the GitHub sink posts
 * (internal/presentation/github FormatReview): counts, summary, notes, a table
 * of findings and related links. The API does not expose that formatter's
 * output, so this is its client-side equivalent, for copying.
 */
export function reviewMarkdown(report: ReviewReport, w: ReviewMarkdownWords): string {
  const counts = reviewCounts(report);
  const lines = [
    REVIEW_SEVERITIES.map((s) => `${ICON[s]} **${w.count[s](counts[s])}**`).join(" "),
    "",
    `## ${w.summary}`,
    "",
    cell(report.summary),
  ];
  if (report.comments.length > 0) {
    lines.push("", `## ${w.comments}`, "", ...report.comments.map((c) => `- ${cell(c)}`));
  }
  if (report.findings.length > 0) {
    lines.push(
      "",
      `## ${w.findings}`,
      "",
      `| Level | Finding | ${w.suggestion} | ${w.evidence} | Follow-up prompt |`,
      "| :---: | :--- | :--- | :--- | :--- |",
    );
    for (const f of report.findings) {
      const evidence = f.evidence
        .map((e) => {
          const src = reviewSource(e.source);
          const at = src.url ? `[${cell(src.label)}](${src.url})` : `\`${cell(src.label)}\``;
          return `${cell(e.observation)} (${at})`;
        })
        .join("<br><br>");
      lines.push(
        `| ${ICON[f.severity]} ${w.group[f.severity]} | ${cell(f.title)} | ${cell(f.suggestion)} | ${evidence} | ${cell(f.prompt)} |`,
      );
    }
  } else {
    lines.push("", w.noFindings);
  }
  if (report.related.length > 0) {
    lines.push(
      "",
      `## ${w.related}`,
      "",
      ...report.related.map((l) => `- ${cell(l.title)}: ${l.url}`),
    );
  }
  return `${lines.join("\n")}\n`;
}
