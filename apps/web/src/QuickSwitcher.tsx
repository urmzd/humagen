import { defaultCopy } from "@humagen/components";
import {
  FileIcon,
  LayersIcon,
  MessageIcon,
  ProjectIcon,
  SearchIcon,
  XIcon,
} from "@humagen/render-web";
import { Modal } from "@humagen/web";
import { type KeyboardEvent, useEffect, useRef, useState } from "react";
import { setAgentHues } from "./agentHues.ts";
import { api, type DiscoveryItem } from "./api.ts";
import { href, type Route } from "./route.ts";
import { AgentMark, Problem } from "./ui.tsx";

const words = defaultCopy.projectSwitcher;
const kinds = ["", "conversation", "message", "project", "agent", "file", "source"] as const;
const kindLabel = (kind: string) =>
  ({
    project: words.projects,
    agent: words.agents,
    conversation: words.conversations,
    message: words.messages,
    file: words.files,
    source: words.sources,
  })[kind] ?? words.all;

function destination(item: DiscoveryItem): Route {
  if (item.kind === "project")
    return { page: "project", name: item.id === "~" ? "" : item.id, tab: "inbox" };
  if (item.kind === "agent") return { page: "agent", name: item.id, tab: "prompt" };
  if (item.kind === "source")
    return { page: "context", source: item.source ?? item.id, tab: "documents" };
  if (item.kind === "file") return { page: "artifacts", artifact: item.id };
  return {
    page: "conversation",
    id: item.thread ?? item.id,
    ...(item.message ? { message: item.message } : {}),
  };
}

/** A bounded server search. Every result retains its native address. */
export function QuickSwitcher({
  onClose,
  onGo,
  open = true,
}: {
  onClose: () => void;
  onGo: (route: Route) => void;
  open?: boolean;
}) {
  const [query, setQuery] = useState("");
  const [items, setItems] = useState<DiscoveryItem[]>([]);
  const [kind, setKind] = useState("");
  const [loading, setLoading] = useState(true);
  const [problem, setProblem] = useState<string | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const results = useRef<HTMLUListElement>(null);
  useEffect(() => {
    let current = true;
    setLoading(true);
    setItems([]);
    setProblem(null);
    const timer = setTimeout(() => {
      api
        .discover(query.trim(), { limit: 20, content: true, ...(kind ? { kinds: [kind] } : {}) })
        .then((matches) => {
          if (!current) return;
          setAgentHues(
            Object.fromEntries(
              matches
                .filter((item) => item.kind === "agent" && item.hue !== undefined)
                .map((item) => [item.id, item.hue as number]),
            ),
          );
          setItems(matches);
        })
        .catch(() => {
          if (current) setProblem(words.failed);
        })
        .finally(() => {
          if (current) setLoading(false);
        });
    }, 160);
    return () => {
      current = false;
      clearTimeout(timer);
    };
  }, [query, kind]);
  const move = (event: KeyboardEvent, fromInput = false) => {
    if (event.nativeEvent.isComposing || !["ArrowDown", "ArrowUp"].includes(event.key)) return;
    const links = [...(results.current?.querySelectorAll<HTMLAnchorElement>("a") ?? [])];
    if (links.length === 0) return;
    event.preventDefault();
    const at = links.indexOf(document.activeElement as HTMLAnchorElement);
    if (fromInput) (event.key === "ArrowDown" ? links[0] : links.at(-1))?.focus();
    else if (at === 0 && event.key === "ArrowUp") input.current?.focus();
    else links[(at + (event.key === "ArrowDown" ? 1 : links.length - 1)) % links.length]?.focus();
  };
  return (
    <Modal open={open} title={words.title} onClose={onClose}>
      <div className="flex min-h-18 items-center gap-3 border-aui-border border-b px-5 py-4">
        <SearchIcon size={20} className="text-aui-muted" />
        <input
          ref={input}
          aria-label={words.search}
          aria-describedby="workspace-search-hint"
          aria-controls="workspace-search-results"
          className="min-w-0 flex-1 bg-transparent py-1 text-aui-base text-aui-fg outline-none placeholder:text-aui-muted"
          type="search"
          placeholder={words.search}
          maxLength={200}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          onKeyDown={(event) => move(event, true)}
        />
        <button
          type="button"
          aria-label={words.close}
          onClick={onClose}
          className="inline-flex size-9 shrink-0 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
        >
          <XIcon size={18} />
        </button>
      </div>
      <fieldset className="flex shrink-0 flex-wrap gap-1 border-aui-border border-b px-4 py-3">
        <legend className="sr-only">{words.kinds}</legend>
        {kinds.map((value) => (
          <button
            key={value}
            type="button"
            aria-pressed={kind === value}
            className="inline-flex min-h-8 items-center rounded-aui px-2.5 text-aui-xs text-aui-muted hover:bg-aui-hover aria-pressed:bg-aui-hover aria-pressed:font-medium aria-pressed:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
            onClick={() => setKind(value)}
          >
            {kindLabel(value)}
          </button>
        ))}
      </fieldset>
      <div className="aui-scroll min-h-0 flex-1 overflow-y-auto px-3 py-3" aria-busy={loading}>
        <div className="px-2 pb-2 text-aui-2xs text-aui-muted">
          {query.trim() ? words.results : words.recent}
        </div>
        <Problem message={problem} />
        <p
          role="status"
          aria-live="polite"
          className={
            loading || (!items.length && !problem)
              ? "px-2 py-8 text-center text-aui-sm text-aui-muted"
              : "sr-only"
          }
        >
          {loading
            ? words.loading
            : !items.length && !problem
              ? words.empty
              : !problem
                ? words.resultCount(items.length)
                : ""}
        </p>
        <ul
          ref={results}
          id="workspace-search-results"
          aria-label={words.results}
          onKeyDown={move}
          className="flex min-w-0 flex-col gap-1"
        >
          {items.map((item) => {
            const route = destination(item);
            const Icon =
              item.kind === "project"
                ? ProjectIcon
                : item.kind === "source"
                  ? LayersIcon
                  : item.kind === "file"
                    ? FileIcon
                    : MessageIcon;
            return (
              <li key={`${item.kind}:${item.id}`}>
                <a
                  href={href(route)}
                  className="group flex min-h-16 min-w-0 items-start gap-3 rounded-aui px-3 py-3 hover:bg-aui-hover focus-visible:bg-aui-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-accent"
                  onClick={(event) => {
                    if (
                      event.button !== 0 ||
                      event.metaKey ||
                      event.ctrlKey ||
                      event.shiftKey ||
                      event.altKey
                    )
                      return;
                    event.preventDefault();
                    onGo(route);
                    onClose();
                  }}
                >
                  <span className="mt-0.5 inline-flex size-8 shrink-0 items-center justify-center rounded-aui border border-aui-border text-aui-muted">
                    {item.kind === "agent" ? (
                      <AgentMark name={item.id} size={18} />
                    ) : (
                      <Icon size={16} />
                    )}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-aui-sm font-medium">{item.title}</span>
                    <span className="mt-0.5 flex flex-wrap gap-x-2 text-aui-xs text-aui-muted">
                      <span>{kindLabel(item.kind)}</span>
                      {item.project && <span>{item.project}</span>}
                      {item.channel && <span>#{item.channel}</span>}
                      {item.source && <span>{item.source}</span>}
                    </span>
                    {item.snippet && (
                      <span className="mt-1 block line-clamp-2 break-words text-aui-xs text-aui-muted">
                        {item.snippet}
                      </span>
                    )}
                  </span>
                </a>
              </li>
            );
          })}
        </ul>
      </div>
      <footer className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-aui-border border-t px-5 py-3 text-aui-2xs text-aui-muted">
        <span id="workspace-search-hint">
          {words.hint}
          <span className="mt-1 block">{words.bounds}</span>
        </span>
        <span className="hidden sm:inline">↑ ↓ {words.keyboard}</span>
      </footer>
    </Modal>
  );
}
