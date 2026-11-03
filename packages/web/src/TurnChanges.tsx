import { type Copy, defaultCopy } from "@humagen/components";
import { type FileChange, type Task, turnChanges } from "@humagen/core";
import { type MouseEvent, type ReactNode, useContext, useMemo, useState } from "react";
import { FileLinkContext } from "./WorkSteps";

type FileKind = "code" | "style" | "data" | "doc" | "file";

/** What a file is, from its extension, for its icon. */
export function fileKind(path: string): FileKind {
  const ext = /\.([a-z0-9]+)$/i.exec(path)?.[1]?.toLowerCase() ?? "";
  if (/^(tsx?|jsx?|mjs|cjs|go|py|rs|rb|java|kt|swift|c|h|cpp|cs|php|sh|sql)$/.test(ext))
    return "code";
  if (/^(css|scss|sass|less)$/.test(ext)) return "style";
  if (/^(json|ya?ml|toml|csv|tsv|xml|ini|env)$/.test(ext)) return "data";
  if (/^(md|mdx|txt|rst|docx?|pdf|pptx?|xlsx?)$/.test(ext)) return "doc";
  return "file";
}

const glyphs: Record<FileKind, ReactNode> = {
  code: <path d="m8 8-4 4 4 4M16 8l4 4-4 4M14 5l-4 14" />,
  style: (
    <path d="M8 4c-2 0-3 1-3 3v2c0 1.5-1 2.5-2 3 1 .5 2 1.5 2 3v2c0 2 1 3 3 3M16 4c2 0 3 1 3 3v2c0 1.5 1 2.5 2 3-1 .5-2 1.5-2 3v2c0 2-1 3-3 3" />
  ),
  data: (
    <>
      <ellipse cx="12" cy="6" rx="7" ry="3" />
      <path d="M5 6v12c0 1.7 3.1 3 7 3s7-1.3 7-3V6M5 12c0 1.7 3.1 3 7 3s7-1.3 7-3" />
    </>
  ),
  doc: (
    <>
      <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
      <path d="M14 3v5h5M9 13h6M9 17h6" />
    </>
  ),
  file: (
    <>
      <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
      <path d="M14 3v5h5" />
    </>
  ),
};

function FileGlyph({ path }: { path: string }) {
  const kind = fileKind(path);
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      width={15}
      height={15}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      className="shrink-0 text-aui-muted"
      data-kind={kind}
    >
      {glyphs[kind]}
    </svg>
  );
}

function Lines({ added, removed }: { added: number; removed: number }) {
  return (
    <span className="shrink-0 font-mono text-aui-xs tabular-nums">
      <span className="text-aui-ok">+{added}</span>
      <span className="ml-1 text-aui-bad">−{removed}</span>
    </span>
  );
}

const split = (path: string) => {
  const i = path.lastIndexOf("/");
  return i < 0 ? { name: path, dir: "" } : { name: path.slice(i + 1), dir: path.slice(0, i) };
};

/**
 * The files a finished turn changed, as one card under it: how many and
 * their lines, a Review that opens the changes in the Workspace, and a row
 * per file that opens its Changes tab. Nothing when the turn changed none.
 */
export function TurnChanges({
  tasks,
  copy = defaultCopy,
}: {
  tasks: readonly Task[];
  copy?: Copy;
}) {
  const links = useContext(FileLinkContext);
  const files = useMemo(() => turnChanges(tasks), [tasks]);
  const [open, setOpen] = useState(true);
  if (files.length === 0) return null;
  const w = copy.conversation.changes;
  const added = files.reduce((n, f) => n + f.added, 0);
  const removed = files.reduce((n, f) => n + f.removed, 0);
  const first = files[0] as FileChange;
  // Review opens the turn's first file at its changes: the Workspace reviews one file at a time.
  const reviewHref = links.href(first.path);
  const go = (path: string) => (e: MouseEvent<HTMLAnchorElement>) => {
    if (!links.open || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return;
    e.preventDefault();
    links.open(path);
  };
  return (
    <section
      aria-label={w.title(files.length)}
      data-testid="turn-changes"
      className="min-w-0 overflow-hidden rounded-aui border border-aui-border"
    >
      <div className="flex min-w-0 items-center gap-2 px-3 py-1.5">
        <button
          type="button"
          aria-expanded={open}
          aria-label={open ? w.hide : w.show}
          onClick={() => setOpen(!open)}
          className="flex min-h-8 pointer-coarse:min-h-11 min-w-0 flex-1 items-center gap-2 rounded-aui-sm text-left text-aui-sm text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
        >
          <span className="truncate text-aui-fg/90">{w.title(files.length)}</span>
          <Lines added={added} removed={removed} />
          <svg
            aria-hidden="true"
            focusable="false"
            width={12}
            height={12}
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth={2}
            className={`shrink-0 transition-transform motion-reduce:transition-none ${open ? "rotate-180" : ""}`}
          >
            <path d="m6 9 6 6 6-6" />
          </svg>
        </button>
        {reviewHref && (
          <a
            href={reviewHref}
            onClick={go(first.path)}
            aria-label={w.reviewLabel(files.length)}
            className="inline-flex min-h-8 pointer-coarse:min-h-11 shrink-0 items-center gap-1.5 rounded-aui-sm border border-aui-border px-2.5 text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            <svg
              aria-hidden="true"
              focusable="false"
              width={14}
              height={14}
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth={1.75}
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M9 3h6l4 4v10a2 2 0 0 1-2 2H9a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2zM5 7v12a2 2 0 0 0 2 2h8M11 10h4M11 14h4" />
            </svg>
            {w.review}
          </a>
        )}
      </div>
      {open && (
        <ul className="flex min-w-0 flex-col border-aui-border border-t py-1">
          {files.map((f) => {
            const { name, dir } = split(f.path);
            const href = links.href(f.path);
            const row = (
              <>
                <FileGlyph path={f.path} />
                <span className="shrink-0 truncate text-aui-fg/90">{name}</span>
                {/* The folder is cut from its start, so its nearest parts stay in view. */}
                <span className="min-w-0 flex-1 truncate text-left text-aui-xs text-aui-muted [direction:rtl]">
                  <bdi>{dir}</bdi>
                </span>
                <Lines added={f.added} removed={f.removed} />
              </>
            );
            const rowClass =
              "flex min-h-8 pointer-coarse:min-h-11 min-w-0 items-center gap-2 px-3 text-aui-sm hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent";
            return (
              <li key={f.path} className="min-w-0" data-testid="turn-change">
                {href ? (
                  <a href={href} onClick={go(f.path)} title={f.path} className={rowClass}>
                    {row}
                  </a>
                ) : (
                  <span title={f.path} className={rowClass}>
                    {row}
                  </span>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
