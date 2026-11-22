import { type Copy, defaultCopy } from "@humagen/components";
import { CopyButton, ReplyIcon } from "@humagen/render-web";
import type { ReactNode } from "react";

export const messageActionButton =
  "inline-flex size-8 pointer-coarse:size-11 shrink-0 items-center justify-center rounded-aui-sm text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:text-aui-accent";

/**
 * The same action row for a person's message and an agent's answer. A row is
 * shown in full or not at all, never greyed: the latest message's row is
 * always shown, and an earlier one's shows while its message (`group/message`)
 * is hovered, focused or tapped (theme.css). Its controls are small, so a row
 * that is always shown stays quiet. It only fades, so the thread never
 * shifts, and a pressed control keeps the row shown.
 */
export function MessageActionRow({
  text,
  copy = defaultCopy,
  copyLabel,
  copyControl,
  onReply,
  children,
}: {
  text: string;
  copy?: Copy;
  copyLabel: string;
  copyControl?: ReactNode;
  onReply?: () => void;
  children?: ReactNode;
}) {
  return (
    <div
      role="toolbar"
      aria-label={copy.conversation.messageActions}
      className="aui-message-actions mt-1 md:-ml-2 flex min-w-0 flex-wrap items-center gap-0.5 transition-opacity motion-reduce:transition-none"
    >
      {text && (copyControl ?? <CopyButton text={text} label={copyLabel} note="above" />)}
      {onReply && (
        <button
          type="button"
          className={messageActionButton}
          aria-label={copy.conversation.reply}
          title={copy.conversation.reply}
          onClick={onReply}
        >
          <ReplyIcon size={16} />
        </button>
      )}
      {children}
    </div>
  );
}
