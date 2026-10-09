import { formatBytes } from "@agent-ui/core";
import { type FileProps, Markdown } from "@agent-ui/primitives";
import {
  ArtifactIcon,
  ArtifactOpenContext,
  CodeBlock,
  DownloadIcon,
  ExternalLinkIcon,
  phaseLabel,
  XIcon,
} from "@agent-ui/render-web";
import { type ReactNode, useEffect, useId, useRef, useState } from "react";

function Body({ file }: { file: FileProps }) {
  if (file.kind === "image" && (file.thumbnailUrl || file.url)) {
    return (
      <img
        src={file.thumbnailUrl ?? file.url}
        alt={file.description ?? file.title}
        className="w-full rounded-aui border border-aui-border object-contain"
      />
    );
  }
  if (file.previewText) {
    const markdown = file.mimeType === "text/markdown" || /\.mdx?$/.test(file.title);
    if (markdown) return <Markdown text={file.previewText} />;
    if (file.kind === "code" || file.kind === "data" || file.kind === "html") {
      return <CodeBlock text={file.previewText} title={file.title} />;
    }
    return (
      <pre className="whitespace-pre-wrap font-aui-mono text-aui-xs text-aui-fg">
        {file.previewText}
      </pre>
    );
  }
  return (
    <p className="text-aui-muted text-aui-sm">
      {file.description ?? "No preview for this file."}
      {file.url ? " Open it to see the whole thing." : ""}
    </p>
  );
}

/**
 * File preview: docked to the right on wide screens, a slide-over below
 * that. Escape or Close dismisses it and returns focus to where it came from.
 */
export function ArtifactPreview({
  file,
  onClose,
}: {
  file: FileProps | null;
  onClose: () => void;
}) {
  const titleId = useId();
  const close = useRef<HTMLButtonElement>(null);
  const opener = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!file) return;
    opener.current = document.activeElement as HTMLElement | null;
    close.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      opener.current?.focus?.();
    };
  }, [file, onClose]);

  if (!file) return null;
  const meta = [
    phaseLabel[file.phase],
    file.sizeBytes !== undefined ? formatBytes(file.sizeBytes) : null,
    file.version ? `v${file.version}` : null,
  ].filter(Boolean);
  const action =
    "inline-flex size-7 items-center justify-center rounded-aui-sm text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent";

  return (
    <>
      <button
        type="button"
        aria-label="Close preview"
        tabIndex={-1}
        onClick={onClose}
        className="aui-fade-up fixed inset-0 z-40 bg-aui-bg/60 lg:hidden"
      />
      <aside
        role="dialog"
        aria-modal="false"
        aria-labelledby={titleId}
        className="aui-fade-up aui-glass fixed inset-y-2 right-2 z-50 flex w-[min(28rem,calc(100vw-1rem))] flex-col overflow-hidden rounded-aui-lg lg:inset-y-4 lg:right-4"
      >
        <header className="flex items-center gap-2 border-aui-border border-b px-3 py-2">
          <ArtifactIcon kind={file.kind} phase={file.phase} size={18} className="text-aui-muted" />
          <span className="flex min-w-0 flex-1 flex-col">
            <h2 id={titleId} className="truncate font-medium text-aui-fg text-aui-sm">
              {file.title}
            </h2>
            <span className="text-aui-2xs text-aui-muted tabular-nums">{meta.join(" · ")}</span>
          </span>
          {file.url && (
            <a
              href={file.url}
              target="_blank"
              rel="noreferrer"
              className={action}
              aria-label={`Open ${file.title} in a new tab`}
            >
              <ExternalLinkIcon size={14} />
            </a>
          )}
          {file.downloadUrl && (
            <a
              href={file.downloadUrl}
              download={file.title}
              className={action}
              aria-label={`Download ${file.title}`}
            >
              <DownloadIcon size={14} />
            </a>
          )}
          <button
            ref={close}
            type="button"
            onClick={onClose}
            className={action}
            aria-label="Close preview"
          >
            <XIcon size={14} />
          </button>
        </header>
        <div className="aui-scroll flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-3">
          {file.description && file.kind !== "image" && (
            <p className="text-aui-muted text-aui-xs">{file.description}</p>
          )}
          <Body file={file} />
        </div>
      </aside>
    </>
  );
}

/** Makes File tiles' Open show the preview panel. Place inside a WebSurface. */
export function ArtifactPreviewProvider({ children }: { children: ReactNode }) {
  const [file, setFile] = useState<FileProps | null>(null);
  const [onClose] = useState(() => () => setFile(null));
  return (
    <ArtifactOpenContext.Provider value={setFile}>
      {children}
      <ArtifactPreview file={file} onClose={onClose} />
    </ArtifactOpenContext.Provider>
  );
}
