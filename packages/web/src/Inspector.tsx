import { XIcon } from "@humagen/render-web";
import { type ReactNode, useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { usePresence } from "./Presence.tsx";

/** A docked inspector on desktop and a native modal on smaller screens. */
export function Inspector({
  open,
  title,
  children,
  onClose,
  onEscape,
  wide = false,
  footer,
  actions,
}: {
  /** Controls in the header, before Close. They stay on its one line. */
  actions?: ReactNode;
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
  const heading = useRef<HTMLHeadingElement>(null);
  const label = useId();
  const [dock, setDock] = useState<HTMLElement | null>(null);
  const { mounted } = usePresence(open, 220);
  const snapshot = useRef({ children, title, footer });
  if (open) snapshot.current = { children, title, footer };
  const shown = open ? { children, title, footer } : snapshot.current;
  useLayoutEffect(() => {
    setDock(document.getElementById("humagen-inspector-dock"));
  }, []);
  useEffect(() => {
    if (!open || !root.current) return;
    const dialog = root.current;
    if (dock && dialog.parentElement !== dock) return;
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const media = window.matchMedia("(max-width: 1023px)");
    const show = () => {
      if (dialog.open) dialog.close();
      if (media.matches) dialog.showModal();
      else dialog.show();
      heading.current?.focus({ preventScroll: true });
    };
    show();
    media.addEventListener("change", show);
    return () => {
      media.removeEventListener("change", show);
      if (dialog.open) dialog.close();
      if (opener?.isConnected) opener.focus({ preventScroll: true });
    };
  }, [open, dock]);
  if (!mounted) return null;
  const content = (
    <dialog
      ref={root}
      data-layer="inspector"
      data-wide={wide || undefined}
      inert={!open}
      aria-hidden={!open || undefined}
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
      className={`humagen-inspector fixed inset-y-0 right-0 left-auto z-40 m-0 h-dvh max-h-none max-w-full flex-col border-0 border-aui-border border-l bg-aui-bg p-0 text-aui-fg open:flex pointer-events-none open:pointer-events-auto translate-x-6 opacity-0 open:translate-none open:opacity-100 starting:open:translate-x-6 starting:open:opacity-0 transition-[opacity,translate,display,overlay] transition-discrete duration-200 ease-(--aui-ease-arrive) motion-reduce:transition-none backdrop:pointer-events-none open:backdrop:pointer-events-auto backdrop:bg-black/25 backdrop:opacity-0 open:backdrop:opacity-100 starting:open:backdrop:opacity-0 backdrop:transition-opacity backdrop:duration-200 motion-reduce:backdrop:transition-none ${wide ? "w-[min(44rem,100vw)] lg:w-[min(44rem,45vw)]" : "w-[min(24rem,100vw)]"}`}
    >
      <header className="flex min-h-16 shrink-0 items-center justify-between gap-3 border-aui-border border-b px-5 py-3 lg:h-[var(--humagen-header-h,4.5rem)] lg:min-h-0">
        <h2
          ref={heading}
          tabIndex={-1}
          id={label}
          className="min-w-0 truncate font-medium text-aui-sm outline-none"
        >
          {shown.title}
        </h2>
        <span className="ml-auto flex shrink-0 items-center gap-1">{actions}</span>
        <button
          type="button"
          aria-label={`Close ${shown.title}`}
          onClick={onClose}
          className="inline-flex size-9 shrink-0 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-aui-accent"
        >
          <XIcon size={16} />
        </button>
      </header>
      <div className="aui-scroll min-h-0 min-w-0 flex-1 overflow-y-auto p-5">{shown.children}</div>
      {shown.footer && (
        <footer className="flex shrink-0 flex-wrap items-center justify-end gap-2 border-aui-border border-t px-5 py-4">
          {shown.footer}
        </footer>
      )}
    </dialog>
  );
  return dock ? createPortal(content, dock) : content;
}
