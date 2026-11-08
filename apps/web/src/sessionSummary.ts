/**
 * What a session did, for its Details panel, from its responses folded from
 * their streams: the current response's progress, the agents it brought in,
 * what it produced, where it ran, and what it used. Pure; no React.
 */
import {
  allTools,
  isAwaiting,
  type ResponseState,
  type ResponseTool,
  waiting,
  worked,
} from "@humagen/core";

export type OutputKind =
  | "spreadsheet"
  | "pdf"
  | "document"
  | "presentation"
  | "image"
  | "code"
  | "data"
  | "archive"
  | "html"
  | "pr"
  | "issue"
  | "slack";

/** Something the session produced: a saved file, or a pull request, issue or Slack post it made. */
export type Output = {
  key: string;
  kind: OutputKind;
  title: string;
  /** The short type a person reads: XLSX, PDF, Doc, PR, Issue, Slack post. */
  type: string;
  /** A saved file opens here. */
  artifactId?: string;
  /** A made thing opens where it lives. */
  url?: string;
};

export type UsedItem = { label: string; url?: string; detail?: string };
export type UsedKey = "uploads" | "connections" | "knowledge" | "context" | "web" | "memory";
export type Used = Record<UsedKey, UsedItem[]>;

export type Environment = { kind: "sandbox" | "browser"; label: string; detail?: string };

export type SessionSummary = {
  progress?: {
    status: ResponseState["status"];
    waiting: boolean;
    agents: number;
    tools: number;
    ms: number;
    /** The response the progress line opens. */
    response: string;
  };
  /** Calls running or waiting on a person, across the session: the header's count. */
  open: number;
  agents: { name: string; calls: number }[];
  outputs: Output[];
  environment: Environment[];
  used: Used;
};

/** A saved file as the panel lists it. */
export type SavedFile = { id: string; title: string; kind: string; mime: string };

// Connected systems by the first word of their tool names, or an MCP server's name.
const CONNECTORS: Record<string, string> = {
  github: "GitHub",
  gmail: "Gmail",
  slack: "Slack",
  jira: "Jira",
  linear: "Linear",
  notion: "Notion",
  drive: "Google Drive",
  gdrive: "Google Drive",
  calendar: "Google Calendar",
  gcal: "Google Calendar",
  snowflake: "Snowflake",
  bigquery: "BigQuery",
  confluence: "Confluence",
  sentry: "Sentry",
  datadog: "Datadog",
  s3: "S3",
  gcs: "Cloud Storage",
};

const KNOWLEDGE = new Set([
  "corpus_search",
  "corpus_query",
  "catalog_search",
  "catalog_query",
  "knowledge_search",
  "search_knowledge",
]);
const WEB = new Set(["web_search", "web_fetch", "fetch", "fetch_url", "browse_url", "read_url"]);
const MEMORY_READ = new Set([
  "memory_read",
  "read_memory",
  "recall",
  "memory_search",
  "search_memory",
]);
const MEMORY_WRITE = new Set([
  "memory_write",
  "write_memory",
  "remember",
  "save_memory",
  "memory_save",
]);
const SANDBOX = /^(sandbox_|bash$|shell$|run_command$|exec$|python$|code_execution$)/;
const BROWSER = /^(browser_|computer$)/;

const str = (v: unknown): string | undefined => (typeof v === "string" && v ? v : undefined);

/** The connected system a tool belongs to, by its name. */
export function connectorOf(name: string): string | undefined {
  const mcp = /^(?:mcp__)?([a-z0-9-]+)__/.exec(name);
  const head = mcp?.[1] ?? name.split(/[_.]/)[0] ?? "";
  return CONNECTORS[head];
}

const add = (list: UsedItem[], item: UsedItem) => {
  if (!list.some((x) => x.label === item.label)) list.push(item);
};

/** Pull requests, issues and Slack posts a result links to. */
function made(text: string): Output[] {
  const out: Output[] = [];
  for (const m of text.matchAll(
    /https:\/\/github\.com\/([^/\s]+\/[^/\s]+)\/(pull|issues)\/(\d+)/g,
  )) {
    const [url, repo, what, n] = m;
    out.push({
      key: url,
      kind: what === "pull" ? "pr" : "issue",
      type: what === "pull" ? "PR" : "Issue",
      title: `${repo} #${n}`,
      url,
    });
  }
  for (const m of text.matchAll(/https:\/\/[a-z0-9-]+\.slack\.com\/archives\/[A-Z0-9]+\/p\d+/g))
    out.push({ key: m[0], kind: "slack", type: "Slack post", title: "Slack post", url: m[0] });
  return out;
}

/** A saved file's kind and the type a person reads for it. */
export function fileType(f: SavedFile): { kind: OutputKind; type: string } {
  const ext = f.title.split(".").pop()?.toLowerCase() ?? "";
  const byMime: Record<string, [OutputKind, string]> = {
    "application/pdf": ["pdf", "PDF"],
    "text/csv": ["spreadsheet", "CSV"],
    "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": ["spreadsheet", "XLSX"],
    "application/vnd.openxmlformats-officedocument.wordprocessingml.document": ["document", "DOCX"],
    "application/vnd.openxmlformats-officedocument.presentationml.presentation": [
      "presentation",
      "PPTX",
    ],
    "text/markdown": ["document", "Doc"],
    "text/html": ["html", "HTML"],
    "application/json": ["data", "JSON"],
    "application/zip": ["archive", "ZIP"],
  };
  const hit = byMime[f.mime];
  if (hit) return { kind: hit[0], type: hit[1] };
  switch (f.kind) {
    case "spreadsheet":
      return { kind: "spreadsheet", type: ext ? ext.toUpperCase() : "Sheet" };
    case "pdf":
      return { kind: "pdf", type: "PDF" };
    case "presentation":
      return { kind: "presentation", type: "Slides" };
    case "image":
      return { kind: "image", type: ext ? ext.toUpperCase() : "Image" };
    case "code":
      return { kind: "code", type: ext ? ext.toUpperCase() : "Code" };
    case "data":
      return { kind: "data", type: ext ? ext.toUpperCase() : "Data" };
    case "archive":
      return { kind: "archive", type: "ZIP" };
    case "html":
      return { kind: "html", type: "HTML" };
    default:
      return { kind: "document", type: "Doc" };
  }
}

/**
 * The session's Details, from its responses (oldest first), the files it
 * saved, the files people uploaded, and whether it ran in a sandbox.
 */
export function summarize(
  responses: ResponseState[],
  extra: { files?: SavedFile[]; uploads?: string[]; sandbox?: string } = {},
): SessionSummary {
  const used: Used = {
    uploads: [],
    connections: [],
    knowledge: [],
    context: [],
    web: [],
    memory: [],
  };
  const agents = new Map<string, number>();
  const outputs: Output[] = [];
  const environment: Environment[] = [];
  let open = 0;
  for (const name of extra.uploads ?? []) add(used.uploads, { label: name });
  for (const f of extra.files ?? []) {
    const t = fileType(f);
    outputs.push({ key: f.id, ...t, title: f.title, artifactId: f.id });
  }
  if (extra.sandbox) environment.push({ kind: "sandbox", label: "Sandbox", detail: extra.sandbox });
  const tool = (t: ResponseTool) => {
    const name = t.name;
    if (t.kind === "agent") {
      const who = t.agent ?? name;
      agents.set(who, (agents.get(who) ?? 0) + 1);
      return;
    }
    if (t.kind === "context") {
      for (const s of t.result?.sources ?? [])
        add(used.context, { label: s.title || s.url || "", url: s.url });
      return;
    }
    if (KNOWLEDGE.has(name)) {
      const corpus = str(t.input.corpus) ?? str(t.input.source) ?? str(t.input.catalog);
      if (corpus) add(used.knowledge, { label: corpus });
      return;
    }
    if (WEB.has(name) || (t.kind === "provider" && /web/.test(name))) {
      const pages = t.result?.sources ?? [];
      if (pages.length)
        for (const s of pages) add(used.web, { label: s.title || s.url || "", url: s.url });
      else {
        const url = str(t.input.url);
        if (url) add(used.web, { label: url, url });
        else if (str(t.input.query))
          add(used.web, { label: str(t.input.query) ?? "", detail: "search" });
      }
      return;
    }
    if (MEMORY_READ.has(name) || MEMORY_WRITE.has(name)) {
      const what =
        str(t.input.name) ?? str(t.input.key) ?? str(t.input.path) ?? str(t.input.query) ?? name;
      add(used.memory, { label: what, detail: MEMORY_WRITE.has(name) ? "Wrote" : "Read" });
      return;
    }
    if (SANDBOX.test(name)) {
      if (!environment.some((e) => e.kind === "sandbox"))
        environment.push({
          kind: "sandbox",
          label: "Sandbox",
          detail: str(t.input.image) ?? str(t.input.runtime),
        });
    } else if (BROWSER.test(name)) {
      if (!environment.some((e) => e.kind === "browser"))
        environment.push({ kind: "browser", label: "Browser" });
    }
    const connector = connectorOf(name);
    if (connector) add(used.connections, { label: connector });
    for (const o of made(t.result?.text ?? ""))
      if (!outputs.some((x) => x.key === o.key))
        outputs.push({
          ...o,
          title: t.title && o.kind !== "slack" ? `${o.title} · ${t.title}` : o.title,
        });
  };
  for (const r of responses) {
    for (const t of allTools(r)) tool(t);
    for (const b of r.content)
      if (b?.type === "text")
        for (const c of b.citations)
          if (c.url && /^https?:/.test(c.url))
            add(used.web, { label: c.title || c.url, url: c.url });
    if (r.status === "running")
      open +=
        allTools(r).filter(
          (t) => t.status === "running" || t.status === "queued" || isAwaiting(t.status),
        ).length || 1;
  }
  const current = responses[responses.length - 1];
  // The progress line counts the whole session, so it agrees with the
  // Agents section; its state is the current response's.
  const totals = responses.map((r) => worked(r));
  return {
    progress: current
      ? {
          status: current.status,
          waiting: waiting(current).length > 0,
          agents: agents.size,
          tools: totals.reduce((n, w) => n + w.tools, 0),
          ms: totals.reduce((n, w) => n + w.ms, 0),
          response: current.rootRun ?? current.id ?? "",
        }
      : undefined,
    open,
    agents: [...agents].map(([name, calls]) => ({ name, calls })),
    outputs,
    environment,
    used,
  };
}

/** "GitHub, Gmail +2": the first few values of a row, then how many more. */
export function shortList(items: UsedItem[], max = 2): string {
  const shown = items.slice(0, max).map((i) => i.label);
  const more = items.length - shown.length;
  return more > 0 ? `${shown.join(", ")} +${more}` : shown.join(", ");
}
