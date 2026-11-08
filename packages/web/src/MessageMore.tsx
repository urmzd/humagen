import { EllipsisIcon } from "@humagen/render-web";
import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { messageActionButton } from "./MessageActionRow";

/** Less frequent answer actions share one viewport-contained menu. */
export function MessageMore({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  const close = useCallback(() => {
    setOpen(false);
    trigger.current?.focus();
  }, []);
  useEffect(() => {
    if (!open || !dialog.current) return;
    const panel = dialog.current;
    panel.show();
    const away = (event: PointerEvent) => {
      if (
        !panel.contains(event.target as Node) &&
        !trigger.current?.contains(event.target as Node) &&
        !(event.target as Element).closest?.("[data-floating-popover]")
      )
        close();
    };
    document.addEventListener("pointerdown", away);
    const place = () => {
      const anchor = trigger.current?.getBoundingClientRect();
      if (!anchor) return;
      const rect = panel.getBoundingClientRect();
      panel.style.left = `${Math.max(12, Math.min(anchor.left, window.innerWidth - rect.width - 12))}px`;
      panel.style.top = `${Math.max(12, anchor.top >= rect.height + 16 ? anchor.top - rect.height - 6 : Math.min(anchor.bottom + 6, window.innerHeight - rect.height - 12))}px`;
    };
    place();
    window.addEventListener("resize", place);
    return () => {
      window.removeEventListener("resize", place);
      document.removeEventListener("pointerdown", away);
      panel.close();
    };
  }, [open, close]);
  return (
    <>
      <button
        ref={trigger}
        type="button"
        aria-label="More actions"
        title="More actions"
        aria-haspopup="dialog"
        aria-expanded={open}
        className={messageActionButton}
        onClick={() => setOpen(true)}
      >
        <EllipsisIcon size={16} />
      </button>
      {open &&
        createPortal(
          <dialog
            ref={dialog}
            aria-label="More actions"
            className="fixed m-0 max-h-[calc(100dvh-1.5rem)] max-w-[calc(100vw-1.5rem)] overflow-auto rounded-aui border border-aui-border bg-aui-bg p-2 text-aui-fg shadow-lg backdrop:bg-transparent"
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                event.preventDefault();
                event.stopPropagation();
                close();
              }
            }}
            onCancel={(event) => {
              event.preventDefault();
              close();
            }}
            onClick={(event) => {
              if (event.target === event.currentTarget) close();
            }}
          >
            <div className="flex flex-wrap items-center gap-2">{children}</div>
            <button
              type="button"
              onClick={close}
              className="mt-2 min-h-9 w-full rounded-aui px-3 text-aui-xs hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
            >
              Close
            </button>
          </dialog>,
          document.body,
        )}
    </>
  );
}
