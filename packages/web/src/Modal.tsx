import { type ReactNode, useEffect, useId, useRef } from "react";

/** A native modal: contained keyboard focus, Escape dismissal and focus return. */
export function Modal({
  title,
  onClose,
  children,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
}) {
  const root = useRef<HTMLDialogElement>(null);
  const label = useId();
  useEffect(() => {
    const dialog = root.current;
    if (!dialog) return;
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    dialog.showModal();
    dialog.querySelector<HTMLInputElement>('input[type="search"]')?.focus();
    return () => {
      dialog.close();
      if (opener?.isConnected) opener.focus({ preventScroll: true });
    };
  }, []);
  return (
    <dialog
      ref={root}
      aria-labelledby={label}
      aria-modal="true"
      data-layer="search"
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.preventDefault();
          event.stopPropagation();
          onClose();
        }
      }}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onClick={(event) => {
        if (event.target !== event.currentTarget) return;
        const box = event.currentTarget.getBoundingClientRect();
        if (
          event.clientX < box.left ||
          event.clientX > box.right ||
          event.clientY < box.top ||
          event.clientY > box.bottom
        )
          onClose();
      }}
      className="humagen-search-modal m-auto w-[min(44rem,calc(100vw-2rem))] max-w-none overflow-hidden rounded-aui-lg border border-aui-border bg-aui-bg p-0 text-aui-fg shadow-2xl backdrop:bg-black/40 open:flex open:flex-col"
    >
      <h2 id={label} className="sr-only">
        {title}
      </h2>
      {children}
    </dialog>
  );
}
