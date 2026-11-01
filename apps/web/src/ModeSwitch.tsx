import { href, type Route } from "./route.ts";

/**
 * On a phone, Home's top center switches between the two things the console
 * is for: talking to agents (Chat, the new conversation) and the work that
 * waits on the person (Work, their Notifications). It floats on glass beside
 * the drawer button. Wider, the sidebar holds both.
 */
export function ModeSwitch({
  current,
  waiting,
  onGo,
}: {
  current: "chat" | "work";
  /** How many things wait on the person, said on Work. */
  waiting: number;
  onGo: (next: Route) => void;
}) {
  const items: { key: "chat" | "work"; label: string; to: Route }[] = [
    { key: "chat", label: "Chat", to: { page: "new" } },
    { key: "work", label: "Work", to: { page: "inbox" } },
  ];
  return (
    <nav
      aria-label="Chat or work"
      className="humagen-drawer-button fixed top-[calc(0.5rem+env(safe-area-inset-top))] left-1/2 z-30 flex -translate-x-1/2 items-center gap-0.5 rounded-full border border-aui-border p-0.5 md:hidden"
    >
      {items.map((item) => (
        <a
          key={item.key}
          href={href(item.to)}
          aria-current={item.key === current ? "page" : undefined}
          onClick={(e) => {
            if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
            e.preventDefault();
            onGo(item.to);
          }}
          className="inline-flex min-h-10 items-center gap-1.5 rounded-full px-4 text-aui-sm text-aui-muted transition-colors hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-[current=page]:bg-aui-hover aria-[current=page]:font-medium aria-[current=page]:text-aui-fg"
        >
          {item.label}
          {item.key === "work" && waiting > 0 && (
            <span className="inline-flex h-4.5 min-w-4.5 items-center justify-center rounded-full bg-aui-accent px-1 text-aui-2xs font-medium tabular-nums text-aui-accent-fg">
              <span className="sr-only">, </span>
              {waiting}
              <span className="sr-only"> waiting</span>
            </span>
          )}
        </a>
      ))}
    </nav>
  );
}
