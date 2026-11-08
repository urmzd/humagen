import {
  ActionId,
  reviewMarkdown as coreReviewMarkdown,
  REVIEW_FORMAT_V2,
  REVIEW_SEVERITIES,
  type ReviewReport,
  type ReviewSeverity,
  readReview,
  reviewCommit,
  reviewCounts,
  reviewFollowUp,
  reviewPullRequest,
  reviewSource,
  type Tone,
} from "@humagen/core";
import { Markdown, Review, type ReviewProps, Summary } from "@humagen/primitives";
import { type Copy, defaultCopy } from "./copy";

const TONE: Record<ReviewSeverity, Tone> = { error: "bad", warning: "warn", info: "info" };

/** One line of the counts, in severity order: "1 error, 2 warnings, 0 info". */
export function reviewCountLine(report: ReviewReport, copy: Copy = defaultCopy): string {
  const counts = reviewCounts(report);
  return REVIEW_SEVERITIES.map((s) => copy.reviewReport.count[s](counts[s])).join(", ");
}

/** The review as GitHub Markdown in the console's words (see core `reviewMarkdown`). */
export function reviewMarkdown(report: ReviewReport, copy: Copy = defaultCopy): string {
  return coreReviewMarkdown(report, copy.reviewReport);
}

/**
 * A structured review as a card. `agent` is who wrote it, for follow-ups that
 * tag it; leave `followUp` off where the host cannot put words in a composer,
 * and each finding shows its prompt as words to copy instead.
 */
export function ReviewCard({
  turnId,
  report,
  agent,
  pullRequestHint,
  followUp = true,
  copy = defaultCopy,
}: {
  turnId: string;
  report: ReviewReport;
  agent: string;
  /** The run's source link, which names the pull request when it came from one. */
  pullRequestHint?: string;
  followUp?: boolean;
  copy?: Copy;
}) {
  const w = copy.reviewReport;
  const counts = reviewCounts(report);
  const commit = reviewCommit(report);
  const pr = reviewPullRequest(report, pullRequestHint);
  let index = 0;
  const groups: ReviewProps["groups"] = REVIEW_SEVERITIES.map((severity) => ({
    id: severity,
    label: w.group[severity],
    tone: TONE[severity],
    findings: [] as ReviewProps["groups"][number]["findings"],
  }));
  for (const f of report.findings) {
    const i = index++;
    const text = reviewFollowUp(agent, f.prompt);
    groups[REVIEW_SEVERITIES.indexOf(f.severity)]?.findings.push({
      title: f.title,
      ...(f.found_by ? { foundBy: w.foundBy(f.found_by) } : {}),
      ...(f.location
        ? {
            location: {
              label: `${f.location.path}:${f.location.line}`,
              ...(report.scope && pr
                ? {
                    url: `${pr.url.split("/pull/")[0]}/blob/${report.scope.commit}/${f.location.path.split("/").map(encodeURIComponent).join("/")}#L${f.location.start_line ?? f.location.line}-L${f.location.line}`,
                  }
                : {}),
            },
          }
        : {}),
      ...(f.change !== undefined ? { change: f.change } : {}),
      ...(f.id
        ? {
            feedback: {
              up: { id: ActionId.feedback, payload: { turnId, target: "up", findingId: f.id } },
              down: { id: ActionId.feedback, payload: { turnId, target: "down", findingId: f.id } },
            },
          }
        : {}),
      suggestion: f.suggestion,
      evidence: f.evidence.map((e) => ({
        observation:
          report.format === REVIEW_FORMAT_V2 && !e.step && !e.session
            ? `${e.observation} (${w.unverified})`
            : e.observation,
        source: (({
          label,
          url,
          code,
          action,
        }: ReviewProps["groups"][number]["findings"][number]["evidence"][number]["source"]) => ({
          label,
          ...(url ? { url } : {}),
          ...(code ? { code } : {}),
          ...(action ? { action } : {}),
        }))(
          e.session
            ? {
                label: e.step ? w.openStep : w.openSession,
                url: `/sessions/${encodeURIComponent(e.session)}${e.step ? `?panel=steps&step=${encodeURIComponent(e.step)}` : ""}`,
              }
            : e.step
              ? {
                  label: w.openStep,
                  action: {
                    id: ActionId.openRun,
                    payload: { turnId, target: turnId, option: e.step },
                  },
                }
              : { ...reviewSource(e.source), action: undefined },
        ),
      })),
      prompt: text,
      ...(followUp
        ? {
            followUp: {
              label: w.followUp,
              accessibilityLabel: w.followUpLabel(f.title),
              action: {
                id: ActionId.reviewFollowUp,
                payload: { turnId, target: String(i), text },
              },
            },
          }
        : {}),
    });
  }
  return (
    <>
      <Summary text={w.point(reviewCountLine(report, copy), report.summary)} />
      <Review
        title={report.title ?? w.title}
        {...(commit
          ? {
              commit: {
                label: w.commit(commit.sha.slice(0, 7)),
                ...(commit.url ? { url: commit.url } : {}),
              },
            }
          : {})}
        {...(pr
          ? {
              pullRequest: pr,
              openReview: {
                label: copy.githubTabs.openReview,
                action: { id: ActionId.openReview, payload: { turnId, target: pr.label } },
              },
            }
          : {})}
        summary={report.summary}
        counts={REVIEW_SEVERITIES.map((s) => ({
          id: s,
          label: w.count[s](counts[s]),
          count: counts[s],
          tone: TONE[s],
        }))}
        groups={groups.filter((g) => g.findings.length > 0)}
        labels={{
          severity: w.severity,
          suggestion: w.suggestion,
          evidence: w.evidence,
          followUpPrompt: w.followUpPrompt,
          noFindings: w.noFindings,
          source: w.source,
        }}
        {...(report.comments.length
          ? { comments: { label: w.comments, items: report.comments } }
          : {})}
        {...(report.related.length ? { related: { label: w.related, items: report.related } } : {})}
        copyMarkdown={{ label: w.copyMarkdown, done: w.copied, text: reviewMarkdown(report, copy) }}
      />
    </>
  );
}

/**
 * An agent's answer text: a review card when it is a structured review, the
 * summary on a still card while one arrives, and Markdown otherwise.
 */
export function AnswerText({
  turnId,
  agent,
  text,
  markdown = text,
  streaming = false,
  pullRequestHint,
  followUp,
  copy = defaultCopy,
}: {
  turnId: string;
  agent: string;
  /** The answer as the agent wrote it, read for a review. */
  text: string;
  /** What to draw when it is not a review, such as the text with citations linked. */
  markdown?: string;
  streaming?: boolean;
  pullRequestHint?: string;
  followUp?: boolean;
  copy?: Copy;
}) {
  const read = readReview(text, streaming);
  if (read.kind === "review") {
    return (
      <ReviewCard
        turnId={turnId}
        report={read.report}
        agent={agent}
        pullRequestHint={pullRequestHint}
        followUp={followUp}
        copy={copy}
      />
    );
  }
  if (read.kind === "partial") {
    // Still: the review is the agent's work in flight, and the status line says so.
    return (
      <Review
        title={copy.reviewReport.title}
        pending
        pendingLabel={copy.reviewReport.preparing}
        {...(read.summary ? { summary: read.summary } : {})}
        counts={[]}
        groups={[]}
        labels={{
          severity: copy.reviewReport.severity,
          suggestion: copy.reviewReport.suggestion,
          evidence: copy.reviewReport.evidence,
          followUpPrompt: copy.reviewReport.followUpPrompt,
          noFindings: copy.reviewReport.noFindings,
        }}
      />
    );
  }
  return <Markdown text={markdown} streaming={streaming} />;
}
