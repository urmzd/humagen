import { XIcon } from "@humagen/render-web";
import { type ReactNode, useEffect, useId, useRef } from "react";

/** A docked inspector on desktop and a native modal on smaller screens. */
export function Inspector({
  open,
  title,
  children,
  onClose,
  onEscape,
  wide = false,
  footer,
}: {
  open: boolean;
  title: string;
  children: ReactNode;
  onClose: () => void;
  /** A nested selection can step back before closing its inspector. */
  onEscape?: () => void;
  wide?: boolean;
  footer?: ReactNode;
}) {
  const root = useRef<HTMLDialogElement>(null);
  const close = useRef<HTMLButtonElement>(null);
  const label = useId();
  useEffect(() => {
    if (!open || !root.current) return;
    const dialog = root.current;
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const media = window.matchMedia("(max-width: 1023px)");
    const show = () => {
      if (dialog.open) dialog.close();
      if (media.matches) dialog.showModal();
      else dialog.show();
      close.current?.focus({ preventScroll: true });
    };
    show();
    media.addEventListener("change", show);
    return () => {
      media.removeEventListener("change", show);
      if (dialog.open) dialog.close();
      if (opener?.isConnected) opener.focus({ preventScroll: true });
    };
  }, [open]);
  return (
    <dialog
      ref={root}
      data-layer="inspector"
      aria-labelledby={label}
      onCancel={(event) => {
        if (event.target !== event.currentTarget) return;
        event.preventDefault();
        (onEscape ?? onClose)();
      }}
      onKeyDown={(event) => {
        // A nonmodal desktop dialog also closes with Escape.
        if (event.key === "Escape") {
          event.preventDefault();
          event.stopPropagation();
          (onEscape ?? onClose)();
        }
      }}
      className={`fixed inset-y-0 right-0 left-auto z-40 m-0 max-h-none h-dvh max-w-full border-0 border-aui-border bg-aui-bg p-0 text-aui-fg shadow-xl backdrop:bg-black/25 lg:inset-y-3 lg:right-3 lg:h-[calc(100dvh-1.5rem)] lg:rounded-aui-lg lg:border ${wide ? "w-[min(44rem,100vw)] lg:w-[min(44rem,55vw)]" : "w-[min(26rem,100vw)]"} ${open ? "open:flex" : ""} flex-col`}
    >
      <header className="flex shrink-0 items-center justify-between gap-3 border-aui-border border-b px-5 py-4">
        <h2 id={label} className="min-w-0 truncate font-medium text-aui-sm">
          {title}
        </h2>
        <button
          ref={close}
          type="button"
          aria-label={`Close ${title}`}
          onClick={onClose}
          className="inline-flex size-8 shrink-0 items-center justify-center rounded-aui hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
        >
          <XIcon size={16} />
        </button>
      </header>
      <div className="aui-scroll min-h-0 min-w-0 flex-1 overflow-y-auto p-5">{children}</div>
      {footer && (
        <footer className="flex shrink-0 flex-wrap items-center justify-end gap-2 border-aui-border border-t px-5 py-4">
          {footer}
        </footer>
      )}
    </dialog>
  );
}
