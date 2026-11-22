import { defaultCopy } from "@humagen/components";
import { Markdown } from "@humagen/primitives";
import { FileIcon } from "@humagen/render-web";
import { Modal } from "@humagen/web";
import { useState } from "react";
import type { MessageAttachmentText } from "../../../packages/client/src/generated/types.ts";
import { call } from "./api.ts";
import { Problem, SkeletonRows } from "./ui.tsx";

/** What the card reads of an attached file. */
type Attached = { name?: string; alt?: string; media_type: string };

const words = defaultCopy.attachedDocument;

/** Whether a file attached to a message is text a reader can open in the console. */
export const opensAsDocument = (file: Attached) => {
  const type = file.media_type.split(";")[0]?.trim().toLowerCase() ?? "";
  return (
    type.startsWith("text/") ||
    type.endsWith("+json") ||
    ["application/json", "application/yaml", "application/x-yaml", "application/toml"].includes(
      type,
    )
  );
};

/**
 * A text file attached to a message, as a card: its name, and pressed, the
 * file itself in a dialog. Markdown is shown formatted, other text as it is.
 */
export function AttachedDocument({
  session,
  message,
  index,
  file,
}: {
  session: string;
  message: string;
  index: number;
  file: Attached;
}) {
  const [open, setOpen] = useState(false);
  const [doc, setDoc] = useState<MessageAttachmentText>();
  const [problem, setProblem] = useState<string>();
  const name = file.name || file.alt || file.media_type;
  const show = () => {
    setOpen(true);
    if (doc) return;
    setProblem(undefined);
    call<MessageAttachmentText>(
      "GET",
      `/api/sessions/${encodeURIComponent(session)}/messages/${encodeURIComponent(message)}/attachments/${index}`,
    ).then(setDoc, (error) =>
      setProblem(error instanceof Error ? error.message : words.unavailable),
    );
  };
  const markdown = /markdown/.test(file.media_type) || /\.md$/i.test(name);
  return (
    <>
      <button
        type="button"
        onClick={show}
        aria-label={words.open(name)}
        className="flex w-72 max-w-full items-center gap-3 rounded-aui-lg border border-aui-border bg-aui-surface px-3 py-2 text-left text-aui-sm transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
      >
        <span className="flex size-8 shrink-0 items-center justify-center rounded-aui bg-aui-hover text-aui-muted">
          <FileIcon size={16} />
        </span>
        <span className="flex min-w-0 flex-col">
          <span className="truncate font-medium text-aui-fg">{name}</span>
          <span className="text-aui-2xs text-aui-muted">{words.kind(markdown)}</span>
        </span>
      </button>
      {open && (
        <Modal title={name} onClose={() => setOpen(false)}>
          <div className="flex max-h-[75vh] flex-col gap-3 overflow-y-auto p-5 text-aui-sm">
            <Problem message={problem ?? null} />
            {!doc && !problem && <SkeletonRows count={4} />}
            {doc &&
              (markdown ? (
                <Markdown text={doc.text} />
              ) : (
                <pre className="whitespace-pre-wrap break-words font-mono text-aui-xs">
                  {doc.text}
                </pre>
              ))}
            {doc?.truncated && <p className="text-aui-xs text-aui-muted">{words.truncated}</p>}
          </div>
        </Modal>
      )}
    </>
  );
}
