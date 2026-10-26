import { defaultCopy } from "@humagen/components";
import { Inspector } from "@humagen/web";
import { useEffect, useRef, useState } from "react";
import { api, type ProjectView } from "./api.ts";
import { href, type Route } from "./route.ts";
import { field, Label, Problem } from "./ui.tsx";

const words = defaultCopy.projectSwitcher;

/** Search is capped at 20 server-filtered visible projects, never a local directory dump. */
export function QuickSwitcher({ onClose, onGo }: { onClose: () => void; onGo: (route: Route) => void }) {
  const [query, setQuery] = useState("");
  const [projects, setProjects] = useState<ProjectView[]>([]);
  const [loading, setLoading] = useState(true);
  const [problem, setProblem] = useState<string | null>(null);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => { input.current?.focus(); }, []);
  useEffect(() => {
    let current = true;
    setLoading(true); setProjects([]); setProblem(null);
    const timer = setTimeout(() => {
      api.findProjects(query.trim()).then((matches) => { if (current) setProjects(matches); })
        .catch(() => { if (current) setProblem(words.failed); }).finally(() => { if (current) setLoading(false); });
    }, 120);
    return () => { current = false; clearTimeout(timer); };
  }, [query]);
  return <Inspector open title={words.title} onClose={onClose}>
    <div className="flex min-w-0 flex-col gap-4"><Label text={words.search}><input ref={input} className={field} type="search" maxLength={200} value={query} onChange={(event) => setQuery(event.target.value)} /></Label>
      <Problem message={problem} />
      <p role="status" className="text-aui-sm text-aui-muted">{loading ? words.loading : projects.length === 0 && !problem ? words.empty : ""}</p>
      <ul className="divide-aui-border divide-y">{projects.map((project) => {
        const route: Route = { page: "project", name: project.name, tab: "inbox" };
        return <li key={project.name}><a href={href(route)} className="flex min-h-12 min-w-0 flex-col justify-center gap-1 rounded-aui px-2 py-3 hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent" onClick={(event) => {
          if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
          event.preventDefault(); onGo(route); onClose();
        }}><span className="break-words text-aui-sm font-medium">{project.title || project.name}</span><span className="break-words text-aui-xs text-aui-muted">{project.path.map((step) => step.title || step.name).filter(Boolean).join(" / ")}</span></a></li>;
      })}</ul>
    </div>
  </Inspector>;
}
