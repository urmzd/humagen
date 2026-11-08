import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@humagen/render-web/ui/select";
import type { ReactNode } from "react";
import { AgentMark } from "./ui.tsx";

/**
 * How an item's facts are shown: a state is a filled pill, a place or owner is
 * an outlined tag, and an item is a card. Pipes and dashes are only for page
 * level metadata, never inside an item.
 */
export type Tone = "bad" | "warn" | "ok" | "accent" | "neutral";

const toneClass: Record<Tone, string> = {
  bad: "text-aui-bad bg-[color-mix(in_oklab,var(--aui-bad)_14%,transparent)]",
  warn: "text-aui-warn bg-[color-mix(in_oklab,var(--aui-warn)_14%,transparent)]",
  ok: "text-aui-ok bg-[color-mix(in_oklab,var(--aui-ok)_14%,transparent)]",
  accent: "text-aui-accent bg-[color-mix(in_oklab,var(--aui-accent)_14%,transparent)]",
  neutral: "text-aui-muted bg-aui-hover",
};

/** A state: priority, status, health. Filled, so it reads before the text around it. */
export function Pill({ tone = "neutral", children }: { tone?: Tone; children: ReactNode }) {
  return (
    <span
      className={`inline-flex h-5 shrink-0 items-center rounded-full px-2 font-medium text-aui-2xs ${toneClass[tone]}`}
    >
      {children}
    </span>
  );
}

/** A place or an owner: a project, an agent, a source. Outlined, so it stays quiet. */
export function Tag({ icon, children }: { icon?: ReactNode; children: ReactNode }) {
  return (
    <span className="inline-flex h-5 min-w-0 max-w-full shrink items-center gap-1 rounded-full border border-aui-border px-2 text-aui-2xs text-aui-muted">
      {icon}
      <span className="min-w-0 truncate">{children}</span>
    </span>
  );
}

export function AgentTag({ name }: { name: string }) {
  return <Tag icon={<AgentMark name={name} size={12} />}>@{name}</Tag>;
}

const statusTone: Record<string, Tone> = {
  working: "accent",
  waiting: "warn",
  failed: "bad",
  done: "ok",
  stopped: "neutral",
};

export function StatusPill({ status }: { status: string }) {
  return (
    <Pill tone={statusTone[status] ?? "neutral"}>
      {status[0]?.toUpperCase()}
      {status.slice(1)}
    </Pill>
  );
}

export function PriorityPill({ priority }: { priority: "urgent" | "normal" | "low" }) {
  return (
    <Pill tone={priority === "urgent" ? "bad" : priority === "normal" ? "warn" : "neutral"}>
      {priority === "urgent" ? "Urgent" : priority === "normal" ? "Normal" : "Low"}
    </Pill>
  );
}

/**
 * One item as a card: a title, its pills and tags in one row, the time at the
 * top right, and its actions on a footer that always sits at the bottom, so
 * cards in a row line their buttons up.
 */
export function ItemCard({
  title,
  facts,
  time,
  actions,
  onOpen,
  href,
  children,
}: {
  title: ReactNode;
  facts?: ReactNode;
  time?: ReactNode;
  actions?: ReactNode;
  onOpen?: () => void;
  href?: string;
  children?: ReactNode;
}) {
  return (
    <article className="humagen-glass flex h-full min-w-0 flex-col gap-3 rounded-aui-lg p-4">
      <header className="flex min-w-0 items-start gap-3">
        {href ? (
          <a
            href={href}
            onClick={(event) => {
              if (
                onOpen &&
                event.button === 0 &&
                !event.metaKey &&
                !event.ctrlKey &&
                !event.shiftKey &&
                !event.altKey
              ) {
                event.preventDefault();
                onOpen();
              }
            }}
            className="line-clamp-2 min-h-6 min-w-0 flex-1 text-left font-medium text-aui-sm text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:min-h-11"
          >
            {title}
          </a>
        ) : onOpen ? (
          <button
            type="button"
            onClick={onOpen}
            className="line-clamp-2 min-h-6 min-w-0 flex-1 text-left font-medium text-aui-sm text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:min-h-11"
          >
            {title}
          </button>
        ) : (
          <h3 className="line-clamp-2 min-w-0 flex-1 font-medium text-aui-sm text-aui-fg">
            {title}
          </h3>
        )}
        {time && <span className="shrink-0 text-aui-2xs text-aui-muted tabular-nums">{time}</span>}
      </header>
      {facts && <div className="flex min-w-0 flex-wrap items-center gap-1.5">{facts}</div>}
      {children}
      {actions && (
        <footer className="mt-auto flex flex-wrap items-center gap-2 pt-1">{actions}</footer>
      )}
    </article>
  );
}

const ALL = "__all";

/**
 * A filter beside a search box: shadcn Select, sized to its value, the same
 * height as the search and segmented controls. An empty value means "all".
 */
export function FilterSelect<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T | "";
  options: { value: T | ""; label: string }[];
  onChange: (next: T | "") => void;
}) {
  return (
    <Select
      value={value === "" ? ALL : value}
      onValueChange={(v) => onChange(v === ALL ? "" : (v as T))}
    >
      <SelectTrigger aria-label={label} className="h-9 min-w-32 border-aui-border">
        <SelectValue />
      </SelectTrigger>
      <SelectContent align="end">
        {options.map((o) => (
          <SelectItem key={o.value || ALL} value={o.value === "" ? ALL : o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
