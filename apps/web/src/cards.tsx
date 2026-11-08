import {
  CalendarIcon,
  DatabaseIcon,
  FileIcon,
  GitHubIcon,
  MessageIcon,
  SlackIcon,
} from "@humagen/render-web";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@humagen/render-web/ui/select";
import type { MouseEvent, ReactNode } from "react";
import { ConnectionLogo } from "./ConnectionLogo.tsx";
import { AgentMark, MoreMenu, type PageAction } from "./ui.tsx";

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

/** The grid every entity's cards sit in: as many 20rem columns as fit, one on a phone. */
export const itemGrid =
  "grid min-w-0 grid-cols-[repeat(auto-fill,minmax(min(100%,20rem),1fr))] gap-3";

/** A plain click on an in-app link moves the address and tells the router, as Back would. */
function follow(event: MouseEvent<HTMLAnchorElement>, onOpen?: () => void) {
  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey)
    return;
  const to = event.currentTarget.getAttribute("href") ?? "";
  if (onOpen) {
    event.preventDefault();
    onOpen();
  } else if (to.startsWith("/")) {
    event.preventDefault();
    window.history.pushState(null, "", to);
    window.dispatchEvent(new PopStateEvent("popstate"));
  }
}

/**
 * The one card for every entity: sessions, schedules, agents, store packages,
 * projects, corpora and sources, Apps and MCPs. The whole card is its link
 * (a real `<a>` or button, with one focus ring); there is no Open button.
 *
 * Slots, top to bottom: a leading `mark` and the `title`, with one `status`
 * (a Pill or a dot, never empty) or the `time` at the top right and the ⋯
 * `menu`; a one-line `description`; a `facts` row of Reference chips and
 * Pills; then anything else and the `actions` footer, which sits at the
 * bottom so cards in a row line up. Controls inside a card stay clickable
 * above the card's link.
 */
export function ItemCard({
  title,
  mark,
  status,
  description,
  facts,
  time,
  menu,
  actions,
  onOpen,
  href,
  children,
}: {
  title: ReactNode;
  /** A provider's mark or an icon, before the title. */
  mark?: ReactNode;
  /** One state at the top right, as a Pill or a dot. Leave it out rather than pass an empty one. */
  status?: ReactNode;
  /** One line on what it is. */
  description?: ReactNode;
  facts?: ReactNode;
  time?: ReactNode;
  /** Its ⋯ menu, the shared More menu. */
  menu?: PageAction[];
  actions?: ReactNode;
  onOpen?: () => void;
  href?: string;
  children?: ReactNode;
}) {
  const titleClass =
    "line-clamp-2 min-h-6 min-w-0 flex-1 text-left font-medium text-aui-sm text-aui-fg outline-none after:absolute after:inset-0 after:rounded-aui-lg focus-visible:after:outline-2 focus-visible:after:outline-aui-accent pointer-coarse:min-h-11";
  const linked = !!href || !!onOpen;
  return (
    <article
      className={`humagen-glass relative flex h-full min-w-0 flex-col gap-3 rounded-aui-lg p-4 ${linked ? "hover:bg-aui-hover" : ""}`}
    >
      <header className="flex min-w-0 items-start gap-3">
        {mark && <span className="flex size-6 shrink-0 items-center justify-center">{mark}</span>}
        {href ? (
          <a href={href} onClick={(event) => follow(event, onOpen)} className={titleClass}>
            {title}
          </a>
        ) : onOpen ? (
          <button type="button" onClick={onOpen} className={titleClass}>
            {title}
          </button>
        ) : (
          <h3 className="line-clamp-2 min-w-0 flex-1 font-medium text-aui-sm text-aui-fg">
            {title}
          </h3>
        )}
        {status && <span className="relative z-10 shrink-0">{status}</span>}
        {time && (
          <span className="relative z-10 shrink-0 text-aui-2xs text-aui-muted tabular-nums">
            {time}
          </span>
        )}
        {menu && menu.length > 0 && (
          <span className="relative z-10 -my-1.5 -mr-1.5 shrink-0">
            <MoreMenu items={menu} />
          </span>
        )}
      </header>
      {description && (
        <p className="line-clamp-1 min-w-0 text-aui-muted text-aui-xs">{description}</p>
      )}
      {facts && (
        <div className="relative z-10 flex min-w-0 flex-wrap items-center gap-1.5 self-start">
          {facts}
        </div>
      )}
      {children && <div className="relative z-10 min-w-0">{children}</div>}
      {actions && (
        <footer className="relative z-10 mt-auto flex flex-wrap items-center gap-2 pt-1">
          {actions}
        </footer>
      )}
    </article>
  );
}

/** What a reference points at, which picks its mark. */
export type ReferenceKind =
  | "slack"
  | "github"
  | "schedule"
  | "file"
  | "corpus"
  | "agent"
  | "session"
  | "url"
  /** A connected provider (Snowflake, BigQuery, Drive, S3…): its own logo, from `provider`. */
  | "provider";

function referenceMark(
  kind: ReferenceKind | undefined,
  label: string,
  provider?: string,
): ReactNode {
  switch (kind) {
    case "provider":
      // Any provider ConnectionLogo knows draws its logo; one it does not, its initial.
      return provider ? (
        <span
          aria-hidden="true"
          className="inline-flex shrink-0 [&>*]:size-3 [&>span]:rounded-sm [&>span]:text-[0.5rem]"
        >
          <ConnectionLogo id={provider} name={provider} />
        </span>
      ) : (
        <DatabaseIcon size={12} aria-hidden />
      );
    case "slack":
      return <SlackIcon size={12} aria-hidden />;
    case "github":
      return <GitHubIcon size={12} aria-hidden />;
    case "schedule":
      return <CalendarIcon size={12} aria-hidden />;
    case "file":
      return <FileIcon size={12} aria-hidden />;
    case "corpus":
      return <DatabaseIcon size={12} aria-hidden />;
    case "agent":
      return <AgentMark name={label.replace(/^@/, "")} size={12} />;
    case "session":
      return <MessageIcon size={12} aria-hidden />;
    default:
      return null;
  }
}

/**
 * The one chip for every reference in the console: an origin (the Slack
 * thread or PR a session started in), what an agent pulled in, an item or a
 * backlink, a knowledge source ("Uploads · 1 item"), a search hit, a child
 * session. A leading mark for what it points at, a label, an optional count,
 * and a link when it has a target: a web address opens in a new tab, a
 * console path opens here. Pill stays for status; Tag for plain facts.
 */
export function Reference({
  kind,
  provider,
  label,
  count,
  href,
  onOpen,
  title,
}: {
  kind?: ReferenceKind;
  /** With `kind: "provider"`, the provider's id, such as `snowflake`, `bigquery`, `drive` or `s3`. */
  provider?: string;
  label: string;
  count?: number;
  href?: string;
  onOpen?: () => void;
  /** A longer line on hover. */
  title?: string;
}) {
  const body = (
    <>
      {referenceMark(kind, label, provider)}
      <span className="min-w-0 truncate">{label}</span>
      {count !== undefined && <span className="shrink-0 tabular-nums">· {count}</span>}
    </>
  );
  const shape =
    "inline-flex h-5 min-w-0 max-w-full shrink items-center gap-1 rounded-full border border-aui-border px-2 text-aui-2xs text-aui-muted";
  const hint = title ?? label;
  if (href) {
    const external = /^https?:\/\//.test(href);
    return (
      <a
        href={href}
        title={hint}
        data-reference={kind ?? "url"}
        onClick={external ? undefined : (event) => follow(event, onOpen)}
        {...(external ? { target: "_blank", rel: "noreferrer" } : {})}
        className={`${shape} hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent`}
      >
        {body}
      </a>
    );
  }
  if (onOpen)
    return (
      <button
        type="button"
        title={hint}
        data-reference={kind ?? "url"}
        onClick={onOpen}
        className={`${shape} hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent`}
      >
        {body}
      </button>
    );
  return (
    <span title={hint} data-reference={kind ?? "url"} className={shape}>
      {body}
    </span>
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
