import { defaultCopy } from "@humagen/components";
import {
  BotIcon,
  ExternalLinkIcon,
  FileIcon,
  GitHubIcon,
  MonitorIcon,
  PaperclipIcon,
  SearchIcon,
  TerminalIcon,
  WrenchIcon,
  XIcon,
} from "@humagen/render-web";
import {
  type ComponentType,
  type KeyboardEvent,
  type ReactNode,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { call } from "./api.ts";
import { follow } from "./cards.tsx";
import { type Hit, segments } from "./commands.ts";
import { href, type WorkbenchTabId } from "./route.ts";
import { AgentMark, button, SkeletonRows } from "./ui.tsx";
import { WORKBENCH_TABS, type WorkbenchTabProps } from "./workbench.ts";
import {
  resolveTarget,
  type SearchGroup,
  type SearchHit,
  type SearchItem,
  type SearchTarget,
  type SessionRead,
  searchItems,
  searchSession,
} from "./workbenchSearch.ts";

const words = defaultCopy.workbenchSearch;

/** How many of a group are listed before its Show All. */
export const GROUP_SHOWN = 6;

const groupIcon: Record<SearchGroup, ComponentType<{ size?: number }>> = {
  files: FileIcon,
  pulls: GitHubIcon,
  links: ExternalLinkIcon,
  commands: TerminalIcon,
  browser: MonitorIcon,
  tools: WrenchIcon,
  agents: BotIcon,
  attachments: PaperclipIcon,
};

const isBuilt = (tab: WorkbenchTabId) => WORKBENCH_TABS.some((t) => t.id === tab);

/** Whether the keys are a Mac's, for the shortcut the field shows. */
const onMac = () =>
  typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform ?? "");

/** Whether a key press is the tab's shortcut: Command or Control, Shift and F. */
export function isSearchShortcut(event: {
  key: string;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
  isComposing?: boolean;
}): boolean {
  return (
    (event.metaKey || event.ctrlKey) &&
    event.shiftKey &&
    !event.altKey &&
    !event.isComposing &&
    event.key.toLowerCase() === "f"
  );
}

/** Where a result opens, in a word. */
function opensIn(target: SearchTarget | undefined, thread: string): string {
  if (!target) return defaultCopy.workbench.tab.details;
  if (target.kind === "file") return words.opens.workspace;
  if (target.kind === "url") return words.opens.external;
  if (target.kind === "tab") return defaultCopy.workbench.tab[target.tab];
  const route = target.route;
  if (route.page !== "conversation") return words.opens.page;
  if (route.id !== thread) return words.opens.session;
  return route.message ? words.opens.message : words.opens.activity;
}

/** Text with the letters a query matched marked, as the composer's menu marks them. */
function Marked({ text, hits }: { text: string; hits: Hit[] }) {
  return (
    <>
      {segments(text, hits).map((seg, i) =>
        seg.hit ? (
          // biome-ignore lint/suspicious/noArrayIndexKey: segments are positional
          <mark key={i} className="rounded-sm bg-aui-accent-soft text-aui-fg">
            {seg.text}
          </mark>
        ) : (
          seg.text
        ),
      )}
    </>
  );
}

const rowLook =
  "flex min-h-12 w-full min-w-0 items-center gap-3 rounded-aui px-2 py-2 text-left hover:bg-aui-hover focus-visible:bg-aui-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-accent";

/**
 * The workbench's Search tab: one field that finds what belongs to this
 * session (its files, the pull requests and issues it made, its links, the
 * commands and browser steps it ran, the tools and agents it used, and what
 * people attached) and opens each where it lives. It searches what the page
 * already holds and one read of the session; it asks the server to search
 * nothing. The query is the tab's selection, so a search has an address.
 */
export function WorkbenchSearch({
  thread,
  runs,
  responses,
  summary,
  origin,
  selection,
  onSelection,
  openTab,
  openFile,
  built = isBuilt,
}: WorkbenchTabProps & {
  /** Whether a workbench tab is built, for a test to stand in for. */
  built?: (tab: WorkbenchTabId) => boolean;
}) {
  const [query, setQuery] = useState(selection ?? "");
  const [session, setSession] = useState<SessionRead>();
  const [partial, setPartial] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [all, setAll] = useState<SearchGroup[]>([]);
  const field = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLDivElement>(null);

  // The address moved under the tab (Back, a link): show its query.
  useEffect(() => setQuery(selection ?? ""), [selection]);

  // The session's own record holds its linked sessions and what people
  // attached. One bounded read; a thread that is not a session has neither.
  const answers = runs.length;
  // biome-ignore lint/correctness/useExhaustiveDependencies: attempt and answers are the signals to read again
  useEffect(() => {
    let live = true;
    setPartial(false);
    if (!thread.startsWith("s_")) {
      setSession({});
      return;
    }
    call<SessionRead>("GET", `/api/sessions/${encodeURIComponent(thread)}`).then(
      (next) => live && setSession(next),
      () => {
        if (!live) return;
        setSession((had) => had ?? {});
        setPartial(true);
      },
    );
    return () => {
      live = false;
    };
  }, [thread, attempt, answers]);

  useEffect(() => {
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (!isSearchShortcut(event)) return;
      event.preventDefault();
      field.current?.focus();
      field.current?.select();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const items = useMemo(
    () =>
      searchItems({
        thread,
        runs: runs.map((r) => r.id),
        responses,
        summary,
        origin,
        session,
      }),
    [thread, runs, responses, summary, origin, session],
  );
  const results = useMemo(() => searchSession(items, query), [items, query]);
  const total = results.reduce((n, g) => n + g.hits.length, 0);
  const loading = !responses || !summary || !session;
  const asked = query.trim();

  // Opening a result leaves the tab, and the field loses the cursor as the
  // pane goes: the query is not written to the address then, which would
  // bring the tab back over what was just opened.
  const opened = useRef(false);
  const settle = () => {
    if (!opened.current && asked !== (selection ?? "")) onSelection(asked || undefined);
  };
  const rows = () => [...(list.current?.querySelectorAll<HTMLElement>("[data-result]") ?? [])];
  const move = (event: KeyboardEvent, fromField = false) => {
    if (event.nativeEvent.isComposing) return;
    const all = rows();
    if (event.key === "Escape" && !fromField) {
      event.preventDefault();
      event.stopPropagation();
      field.current?.focus();
      return;
    }
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key) || all.length === 0) return;
    // In the field, Home and End move the cursor through the words.
    if (fromField && (event.key === "Home" || event.key === "End")) return;
    event.preventDefault();
    const at = all.indexOf(document.activeElement as HTMLElement);
    if (event.key === "Home") all[0]?.focus();
    else if (event.key === "End") all.at(-1)?.focus();
    else if (fromField) (event.key === "ArrowDown" ? all[0] : all.at(-1))?.focus();
    else if (at === 0 && event.key === "ArrowUp") field.current?.focus();
    else if (at === all.length - 1 && event.key === "ArrowDown") field.current?.focus();
    else all[at + (event.key === "ArrowDown" ? 1 : -1)]?.focus();
  };

  const row = ({ item, hits }: SearchHit): ReactNode => {
    const target = resolveTarget(item.target, built);
    const Icon = groupIcon[item.group];
    const second = [item.detail, item.count && item.count > 1 ? words.calls(item.count) : ""]
      .filter(Boolean)
      .join(" · ");
    const body = (
      <>
        <span
          aria-hidden
          className="inline-flex size-8 shrink-0 items-center justify-center rounded-aui border border-aui-border text-aui-muted"
        >
          {item.agent ? <AgentMark name={item.agent} size={18} /> : <Icon size={16} />}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate font-medium text-aui-base text-aui-fg">
            <Marked text={item.title} hits={hits} />
          </span>
          {second && <span className="block truncate text-aui-base text-aui-muted">{second}</span>}
        </span>
        <span className="inline-flex shrink-0 items-center gap-1 text-aui-base text-aui-muted">
          {opensIn(target, thread)}
          {target?.kind === "url" && <ExternalLinkIcon size={14} />}
        </span>
      </>
    );
    const mark = { "data-result": item.key, "data-opens": target?.kind ?? "details" };
    if (target?.kind === "url")
      return (
        <a href={target.url} target="_blank" rel="noreferrer" className={rowLook} {...mark}>
          {body}
        </a>
      );
    if (target?.kind === "route" || (target?.kind === "file" && !openFile))
      return (
        <a
          href={href(
            target.kind === "route"
              ? target.route
              : { page: "artifacts", artifact: target.artifact },
          )}
          onClick={(event) => follow(event)}
          className={rowLook}
          {...mark}
        >
          {body}
        </a>
      );
    return (
      <button
        type="button"
        className={rowLook}
        onClick={() => {
          if (target?.kind === "file") openFile?.(target.artifact);
          else if (target?.kind === "tab") openTab(target.tab, target.ref);
          // A call whose answer the page has not read has no view of its own.
          else openTab("details");
        }}
        {...mark}
      >
        {body}
      </button>
    );
  };

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col" data-testid="workbench-search">
      <div className="shrink-0 px-5 pt-4 pb-3">
        <div className="flex h-11 items-center gap-2 rounded-aui border border-[var(--humagen-rule)] bg-aui-bg px-3 text-aui-muted transition-colors focus-within:border-aui-accent focus-within:text-aui-fg motion-reduce:transition-none">
          <SearchIcon size={16} />
          <input
            ref={field}
            type="search"
            aria-label={words.field}
            aria-controls="workbench-search-results"
            aria-keyshortcuts="Meta+Shift+F Control+Shift+F"
            placeholder={words.field}
            maxLength={200}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onFocus={() => {
              opened.current = false;
            }}
            onBlur={settle}
            onKeyDown={(event) => {
              if (event.key === "Escape" && query) {
                event.stopPropagation();
                event.preventDefault();
                setQuery("");
              } else if (event.key === "Enter" && !event.nativeEvent.isComposing) {
                event.preventDefault();
                // Enter opens the closest match; the query stays at the address.
                const first = rows()[0];
                settle();
                if (asked && first) first.click();
              } else move(event, true);
            }}
            className="min-w-0 flex-1 self-stretch bg-transparent text-aui-base text-aui-fg outline-none placeholder:text-aui-muted [&::-webkit-search-cancel-button]:hidden"
          />
          {query ? (
            <button
              type="button"
              aria-label={words.clear}
              title={words.clear}
              onClick={() => {
                setQuery("");
                field.current?.focus();
              }}
              className="-mr-1.5 inline-flex size-8 pointer-coarse:size-11 shrink-0 items-center justify-center rounded-aui-sm text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
            >
              <XIcon size={14} />
            </button>
          ) : (
            <kbd
              data-testid="workbench-search-shortcut"
              className="shrink-0 font-aui-sans text-aui-base text-aui-muted pointer-coarse:hidden"
            >
              {onMac() ? words.shortcut.mac : words.shortcut.other}
            </kbd>
          )}
        </div>
      </div>
      <div
        ref={list}
        id="workbench-search-results"
        role="group"
        aria-label={words.results}
        aria-busy={loading}
        onKeyDown={(event) => move(event)}
        onClickCapture={(event) => {
          const hit = (event.target as HTMLElement).closest("[data-result]");
          if (hit && !hit.getAttribute("data-result")?.startsWith("more:")) opened.current = true;
        }}
        className="aui-scroll min-h-0 min-w-0 flex-1 overflow-y-auto px-3 pb-5"
      >
        <p role="status" aria-live="polite" className="sr-only">
          {loading ? "" : words.count(total)}
        </p>
        {partial && (
          <p
            role="alert"
            className="mx-2 mb-3 flex flex-wrap items-center justify-between gap-2 rounded-aui border border-aui-border px-3 py-2 text-aui-base text-aui-fg"
          >
            {words.partial}
            <button
              type="button"
              className={`${button} text-aui-base`}
              onClick={() => setAttempt((n) => n + 1)}
            >
              {words.retry}
            </button>
          </p>
        )}
        {loading ? (
          <div className="px-2">
            <SkeletonRows count={5} height="3rem" />
          </div>
        ) : total === 0 ? (
          <div className="flex flex-col items-start gap-3 px-2 py-8">
            <p className="humagen-display break-words font-medium text-aui-fg text-aui-lg">
              {asked ? words.none(asked) : words.empty}
            </p>
            <button
              type="button"
              className={`${button} text-aui-base`}
              onClick={() => {
                if (!asked) return openTab("details");
                setQuery("");
                onSelection(undefined);
                field.current?.focus();
              }}
            >
              {asked ? words.clear : words.emptyAction}
            </button>
          </div>
        ) : (
          <>
            {results.map(({ group, hits }) => {
              const open = all.includes(group) || hits.length <= GROUP_SHOWN;
              const shown = open ? hits : hits.slice(0, GROUP_SHOWN);
              return (
                <section key={group} aria-labelledby={`workbench-search-${group}`} className="mb-4">
                  <h3
                    id={`workbench-search-${group}`}
                    className="flex items-baseline gap-2 px-2 pb-1 font-medium text-aui-base text-aui-muted"
                  >
                    {words.group[group]}
                    <span className="font-normal tabular-nums">{hits.length}</span>
                  </h3>
                  <ul className="flex min-w-0 flex-col">
                    {shown.map((hit: SearchHit) => (
                      <li key={hit.item.key}>{row(hit)}</li>
                    ))}
                  </ul>
                  {!open && (
                    <button
                      type="button"
                      data-result={`more:${group}`}
                      className="ml-2 mt-1 inline-flex min-h-9 pointer-coarse:min-h-11 items-center rounded-aui px-2 font-medium text-aui-base text-aui-fg underline underline-offset-4 hover:no-underline focus-visible:outline-2 focus-visible:outline-aui-accent"
                      onClick={() => setAll((had) => [...had, group])}
                    >
                      {words.showAll(hits.length)}
                    </button>
                  )}
                </section>
              );
            })}
            <p className="px-2 text-aui-base text-aui-muted pointer-coarse:hidden">{words.keys}</p>
          </>
        )}
      </div>
    </div>
  );
}

export type { SearchItem };
