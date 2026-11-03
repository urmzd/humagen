import { defaultCopy } from "@humagen/components/copy";
import type { ReviewComment } from "../../../packages/web/src/ChangeReview.tsx";
import { compareText, parseUnifiedDiff } from "../../../packages/web/src/code-diff.ts";

/** Keep the reviewed bytes beside feedback even when the working file is unsaved. */
export function workspaceReviewPrompt(
  source: { path: string; content: string; revision: string; base: string },
  comment: ReviewComment,
): string {
  const words = defaultCopy.workspaceFiles;
  const diff = /\.(diff|patch)$/i.test(source.path)
    ? parseUnifiedDiff(source.content)
    : compareText(source.base, source.content, source.path);
  const file = diff.files.find((item) => item.path === comment.path);
  const at =
    file?.lines.findIndex((line) =>
      comment.side === "old" ? line.oldLine === comment.line : line.newLine === comment.line,
    ) ?? -1;
  const excerpt =
    at < 0
      ? []
      : file?.lines.slice(Math.max(0, at - 2), at + 3).map((line) => ({
          kind: line.kind,
          oldLine: line.oldLine,
          newLine: line.newLine,
          text: line.text.slice(0, 1200),
          ...(line.text.length > 1200 ? { truncated: true } : {}),
        }));
  return [
    words.reviewComment(comment.path, comment.line, comment.side, comment.text),
    words.reviewSource(
      source.path,
      source.revision,
      !source.revision || source.content !== source.base,
    ),
    JSON.stringify(excerpt, null, 2),
  ].join("\n\n");
}
