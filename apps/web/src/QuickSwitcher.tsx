import { defaultCopy } from "@humagen/components";
import { Inspector } from "@humagen/web";
import { useEffect, useRef, useState } from "react";
import { api, type DiscoveryItem } from "./api.ts";
import { href, type Route } from "./route.ts";
import { setAgentHues } from "./agentHues.ts";
import { AgentMark, field, Label, Problem } from "./ui.tsx";

const words = defaultCopy.projectSwitcher;

/** Search is capped at 20 server-filtered destinations, never a local directory dump. */
export function QuickSwitcher({ onClose, onGo }: { onClose: () => void; onGo: (route: Route) => void }) {
  const [query, setQuery] = useState("");
  const [items, setItems] = useState<DiscoveryItem[]>([]);
  const [kind, setKind] = useState("");
  const [loading, setLoading] = useState(true);
  const [problem, setProblem] = useState<string | null>(null);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => { input.current?.focus(); }, []);
  useEffect(() => {
    let current = true;
    setLoading(true); setItems([]); setProblem(null);
    const timer = setTimeout(() => {
      api.discover(query.trim(), { limit: 20, ...(kind ? { kinds: [kind] } : {}) }).then((matches) => { if (current) { setAgentHues(Object.fromEntries(matches.filter((item) => item.kind === "agent" && item.hue !== undefined).map((item) => [item.id, item.hue as number]))); setItems(matches); } })
        .catch(() => { if (current) setProblem(words.failed); }).finally(() => { if (current) setLoading(false); });
    }, 120);
    return () => { current = false; clearTimeout(timer); };
  }, [query, kind]);
  return <Inspector open title={words.title} onClose={onClose}>
    <div className="flex min-w-0 flex-col gap-4"><Label text={words.search}><input ref={input} className={field} type="search" maxLength={200} value={query} onChange={(event) => setQuery(event.target.value)} /></Label>
      <Problem message={problem} />
      <p role="status" className="text-aui-sm text-aui-muted">{loading ? words.loading : items.length === 0 && !problem ? words.empty : ""}</p>
      <Label text={words.kinds}><select aria-label={words.kinds} className={field} value={kind} onChange={(event) => setKind(event.target.value)}><option value="">{words.all}</option><option value="project">{words.projects}</option><option value="agent">{words.agents}</option><option value="conversation">{words.conversations}</option></select></Label>
      <ul className="divide-aui-border divide-y">{items.map((item) => {
        const route: Route = item.kind === "project" ? { page: "project", name: item.id === "~" ? "" : item.id, tab: "inbox" } : item.kind === "agent" ? { page: "agent", name: item.id, tab: "prompt" } : { page: "conversation", id: item.id };
        return <li key={`${item.kind}:${item.id}`}><a href={href(route)} className="flex min-h-12 min-w-0 flex-col justify-center gap-1 rounded-aui px-2 py-3 hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent" onClick={(event) => {
          if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
          event.preventDefault(); onGo(route); onClose();
        }}><span className="flex items-center gap-2 break-words text-aui-sm font-medium">{item.kind === "agent" && <AgentMark name={item.id} size={15} />}{item.title}</span><span className="break-words text-aui-xs text-aui-muted">{item.kind === "project" ? words.projects : item.kind === "agent" ? words.agents : words.conversations}{item.project ? ` / ${item.project}` : ""}{item.channel ? ` / #${item.channel}` : ""}</span></a></li>;
      })}</ul>
    </div>
  </Inspector>;
}
