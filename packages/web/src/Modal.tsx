import { type ReactNode, useEffect, useId, useRef } from "react";

/** A native modal: contained keyboard focus, Escape dismissal and focus return. */
export function Modal({
  title,
  onClose,
  children,
  open = true,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  open?: boolean;
}) {
  const root = useRef<HTMLDialogElement>(null);
  const label = useId();
  useEffect(() => {
    const dialog = root.current;
    if (!dialog || !open) return;
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    dialog.showModal();
    dialog.querySelector<HTMLInputElement>('input[type="search"]')?.focus();
    return () => {
      dialog.close();
      if (opener?.isConnected) opener.focus({ preventScroll: true });
    };
  }, [open]);
  return (
    <dialog
      ref={root}
      aria-labelledby={label}
      aria-modal="true"
      data-layer="search"
      inert={!open}
      aria-hidden={!open || undefined}
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
      className="humagen-search-modal m-auto w-[min(44rem,calc(100vw-2rem))] max-w-none aui-glass overflow-hidden rounded-aui-lg p-0 text-aui-fg open:flex pointer-events-none open:pointer-events-auto open:flex-col scale-[0.98] opacity-0 open:scale-none open:opacity-100 starting:open:scale-[0.98] starting:open:opacity-0 transition-[opacity,scale,display,overlay] transition-discrete duration-150 ease-(--aui-ease-arrive) motion-reduce:transition-none backdrop:pointer-events-none open:backdrop:pointer-events-auto backdrop:bg-black/40 backdrop:opacity-0 open:backdrop:opacity-100 starting:open:backdrop:opacity-0 backdrop:transition-opacity backdrop:duration-150 motion-reduce:backdrop:transition-none"
    >
      <h2 id={label} className="sr-only">
        {title}
      </h2>
      {children}
    </dialog>
  );
}
