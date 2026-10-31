import type { ReactNode } from "react";
import { href, type Route } from "./route.ts";

export function WorkspaceTabs({
  label,
  items,
  current,
  onGo,
}: {
  label: string;
  items: { label: string; route: Route; active?: boolean; icon?: ReactNode }[];
  current: string;
  onGo: (route: Route) => void;
}) {
  return (
    <nav
      aria-label={label}
      className="humagen-beside-drawer flex shrink-0 flex-wrap gap-x-4 gap-y-1 border-aui-border border-b px-5 pt-2 md:px-8"
    >
      {items.map((item) => (
        <a
          key={item.label}
          href={href(item.route)}
          aria-current={
            (item.active ?? current.split("?")[0] === href(item.route).split("?")[0])
              ? "page"
              : undefined
          }
          onClick={(event) => {
            if (
              event.button === 0 &&
              !event.metaKey &&
              !event.ctrlKey &&
              !event.shiftKey &&
              !event.altKey
            ) {
              event.preventDefault();
              onGo(item.route);
            }
          }}
          className="inline-flex min-h-11 items-center gap-2 border-b-2 border-transparent px-1 text-sm text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-[current=page]:border-aui-primary aria-[current=page]:font-medium aria-[current=page]:text-aui-fg"
        >
          {item.icon}
          {item.label}
        </a>
      ))}
    </nav>
  );
}
