import { defaultCopy } from "@humagen/components/copy";
import { DiffIcon, FileIcon, MessageSquareIcon, XIcon } from "@humagen/render-web";
import { useEffect, useId, useMemo, useState } from "react";
import { compareText, parseUnifiedDiff, type ReviewLine } from "./code-diff";

export interface ReviewComment {
  path: string;
  line: number;
  side: "old" | "new";
  text: string;
}
export interface ChangeReviewProps {
  patch?: string;
  before?: string;
  after?: string;
  path?: string;
  onComment?: (comment: ReviewComment) => void;
}
const words = defaultCopy.editor;
const control =
  "inline-flex min-h-9 items-center justify-center gap-1.5 rounded-aui-sm px-2 text-aui-xs hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-50 pointer-coarse:min-h-11";

/** File and line review emits a draft comment; sending belongs to the host. */
export function ChangeReview({
  patch,
  before = "",
  after = "",
  path = "file",
  onComment,
}: ChangeReviewProps) {
  const diff = useMemo(
    () => (patch === undefined ? compareText(before, after, path) : parseUnifiedDiff(patch)),
    [patch, before, after, path],
  );
  const [selected, setSelected] = useState("");
  const [target, setTarget] = useState<Omit<ReviewComment, "text"> | null>(null);
  const [text, setText] = useState("");
  const [notice, setNotice] = useState("");
  useEffect(() => {
    // Comments belong to the precise version of the comparison they came from.
    void diff;
    setTarget(null);
    setText("");
    setNotice("");
  }, [diff]);
  const commentId = useId();
  const file = diff.files.find((item) => item.path === selected) ?? diff.files[0];
  const additions = diff.files.reduce((sum, item) => sum + item.additions, 0);
  const deletions = diff.files.reduce((sum, item) => sum + item.deletions, 0);
  const selectLine = (line: ReviewLine) => {
    if (!file || (line.newLine === undefined && line.oldLine === undefined)) return;
    setTarget({
      path: file.path,
      line: line.newLine ?? line.oldLine ?? 1,
      side: line.newLine === undefined ? "old" : "new",
    });
    setNotice("");
  };
  return (
    <section
      data-slot="change-review"
      aria-label={words.review}
      className="flex min-w-0 flex-col overflow-hidden rounded-aui border border-aui-border bg-aui-code text-aui-fg"
    >
      <header className="flex flex-wrap items-center gap-3 border-b border-aui-border px-3 py-2 text-aui-xs">
        <DiffIcon size={16} />
        <span>{words.review}</span>
        <span>{words.fileCount(diff.files.length)}</span>
        <span className="text-aui-good">
          <span aria-hidden>+{additions}</span>
          <span className="sr-only">{words.additions(additions)}</span>
        </span>
        <span className="text-aui-bad">
          <span aria-hidden>−{deletions}</span>
          <span className="sr-only">{words.deletions(deletions)}</span>
        </span>
      </header>
      {diff.error ? (
        <p role="alert" className="p-4 text-aui-sm">
          {diff.error}
        </p>
      ) : !file ? (
        <p className="p-4 text-aui-sm text-aui-muted">
          {patch === undefined ? words.noChanges : words.openDiff}
        </p>
      ) : (
        <>
          {diff.files.length > 1 && (
            <nav
              aria-label={words.changedFiles}
              className="flex max-h-44 min-w-0 flex-col overflow-y-auto border-b border-aui-border p-1"
            >
              {diff.files.map((item, index) => (
                <button
                  key={`${index}:${item.path}`}
                  type="button"
                  aria-current={file === item ? "true" : undefined}
                  onClick={() => {
                    setSelected(item.path);
                    setTarget(null);
                  }}
                  className={`${control} min-w-0 justify-start aria-current:bg-aui-hover`}
                >
                  <FileIcon size={14} />
                  <span className="min-w-0 flex-1 truncate text-left" title={item.path}>
                    {item.path}
                  </span>
                  <span className="text-aui-good">+{item.additions}</span>
                  <span className="text-aui-bad">−{item.deletions}</span>
                </button>
              ))}
            </nav>
          )}
          <div className="flex min-w-0 items-center gap-2 border-b border-aui-border px-3 py-2 text-aui-xs">
            <FileIcon size={14} />
            <span className="min-w-0 break-all font-aui-mono">{file.path}</span>
          </div>
          {file.binary ? (
            <p className="p-4 text-aui-sm text-aui-muted">{words.binary}</p>
          ) : (
            <section
              // biome-ignore lint/a11y/noNoninteractiveTabindex: keyboard users must be able to scroll the review
              tabIndex={0}
              aria-label={words.diffFor(file.path)}
              className="max-h-[55dvh] min-w-0 overflow-auto outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-aui-accent"
            >
              <table className="w-full border-collapse font-aui-mono text-aui-xs leading-6">
                <thead className="sr-only">
                  <tr>
                    <th>{words.oldLine}</th>
                    <th>{words.newLine}</th>
                    <th>{words.change}</th>
                    <th>{words.code}</th>
                    {onComment && <th>{words.reviewColumn}</th>}
                  </tr>
                </thead>
                <tbody>
                  {file.lines.map((line, index) => {
                    const commentable =
                      onComment && (line.oldLine !== undefined || line.newLine !== undefined);
                    const tone =
                      line.kind === "add"
                        ? "bg-aui-good/10"
                        : line.kind === "remove"
                          ? "bg-aui-bad/10"
                          : line.kind === "hunk"
                            ? "bg-aui-accent/10 text-aui-muted"
                            : "";
                    return (
                      <tr key={`${index}:${line.oldLine}:${line.newLine}`} className={tone}>
                        <td className="w-10 select-none px-2 text-right text-aui-muted">
                          {line.oldLine ?? ""}
                        </td>
                        <td className="w-10 select-none border-r border-aui-border px-2 text-right text-aui-muted">
                          {line.newLine ?? ""}
                        </td>
                        <td className="w-6 select-none px-2 text-aui-muted">
                          <span aria-hidden>
                            {line.kind === "add" ? "+" : line.kind === "remove" ? "−" : ""}
                          </span>
                          <span className="sr-only">
                            {line.kind === "add"
                              ? words.added
                              : line.kind === "remove"
                                ? words.removed
                                : ""}
                          </span>
                        </td>
                        <td className="whitespace-pre pr-3">{line.text || " "}</td>
                        {onComment && (
                          <td className="w-9 px-1">
                            {commentable && (
                              <button
                                type="button"
                                className={control}
                                aria-label={words.comment(
                                  file.path,
                                  line.newLine === undefined ? "old" : "new",
                                  line.newLine ?? line.oldLine ?? 1,
                                )}
                                onClick={() => selectLine(line)}
                              >
                                <MessageSquareIcon size={13} />
                              </button>
                            )}
                          </td>
                        )}
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </section>
          )}
        </>
      )}
      {diff.truncated && (
        <output className="border-t border-aui-border px-3 py-2 text-aui-xs text-aui-muted">
          {words.reviewTruncated}
        </output>
      )}
      {target && onComment && (
        <form
          className="flex flex-col gap-2 border-t border-aui-border p-3"
          onSubmit={(event) => {
            event.preventDefault();
            if (!text.trim()) return;
            onComment({ ...target, text: text.trim() });
            setTarget(null);
            setText("");
            setNotice(words.commentPrepared);
          }}
        >
          <div className="flex items-start justify-between gap-2">
            <label htmlFor={commentId} className="min-w-0 break-all text-aui-xs">
              {words.comment(target.path, target.side, target.line)}
            </label>
            <button
              type="button"
              className={control}
              aria-label={words.cancelComment}
              onClick={() => setTarget(null)}
            >
              <XIcon size={14} />
            </button>
          </div>
          <textarea
            id={commentId}
            value={text}
            onChange={(event) => setText(event.target.value)}
            maxLength={8000}
            rows={3}
            className="min-w-0 rounded-aui-sm border border-aui-border bg-aui-bg p-2 text-base outline-aui-accent sm:text-aui-xs"
          />
          <p className="text-aui-2xs text-aui-muted">{words.commentHint}</p>
          <button type="submit" className={`${control} self-start`} disabled={!text.trim()}>
            <MessageSquareIcon size={14} />
            {words.addToDraft}
          </button>
        </form>
      )}
      <output className={notice ? "px-3 py-2 text-aui-xs text-aui-muted" : "sr-only"}>
        {notice}
      </output>
    </section>
  );
}
