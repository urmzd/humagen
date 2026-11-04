import type { ReviewPin } from "../../../packages/web/src/code-diff.ts";

/** The format id of a structured review (humagen.github.review.v1). */
export const REVIEW_FORMAT = "humagen.github.review.v1";

interface Finding {
  severity?: string;
  title?: string;
  evidence?: { observation?: string; source?: string }[];
}

/** Whether text is a structured review, as a saved review artifact holds it. */
export function isReviewText(text: string): boolean {
  try {
    return (JSON.parse(text) as { format?: string }).format === REVIEW_FORMAT;
  } catch {
    return false;
  }
}

/**
 * The lines a structured review's findings cite, as pins for a diff: each
 * evidence source of the form `path:line` or a permalink ending `#L<line>`.
 * The review card that renders these reports lives elsewhere; this reads
 * only what a diff needs.
 */
export function reviewPins(text: string): ReviewPin[] {
  let report: { format?: string; findings?: Finding[] };
  try {
    report = JSON.parse(
      text
        .trim()
        .replace(/^```(?:json)?\s*\n?/, "")
        .replace(/\n?```$/, ""),
    );
  } catch {
    return [];
  }
  if (report.format !== REVIEW_FORMAT || !Array.isArray(report.findings)) return [];
  const pins: ReviewPin[] = [];
  for (const finding of report.findings.slice(0, 200)) {
    const severity =
      finding.severity === "error" || finding.severity === "warning" || finding.severity === "info"
        ? finding.severity
        : undefined;
    for (const evidence of finding.evidence ?? []) {
      const source = evidence.source ?? "";
      const cited =
        /^([^\s:#]+):(\d+)(?:[-:]\d+)?$/.exec(source) ??
        /\/blob\/[^/]+\/([^#\s]+)#L(\d+)/.exec(source);
      if (!cited) continue;
      pins.push({
        path: cited[1] as string,
        line: Number(cited[2]),
        severity,
        text: [finding.title, evidence.observation].filter(Boolean).join(": "),
      });
    }
  }
  return pins;
}
