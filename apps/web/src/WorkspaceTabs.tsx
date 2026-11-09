import type { ReactNode } from "react";
import { href, type Route } from "./route.ts";
import { pageColumn, tabItem, tabRule, tabStrip } from "./ui.tsx";

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
  // The same strip a page's own tabs draw (Tabs), in the page's column, so
  // tabs look and sit the same whichever kind heads a page.
  return (
    <nav aria-label={label} className={`humagen-beside-drawer shrink-0 pt-8 ${pageColumn}`}>
      <div className="relative">
        <span aria-hidden className={tabRule} />
        <div className={tabStrip}>
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
              className={tabItem}
            >
              {item.icon}
              {item.label}
            </a>
          ))}
        </div>
      </div>
    </nav>
  );
}
