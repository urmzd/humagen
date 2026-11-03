import { defaultCopy } from "@humagen/components";
import { BrainIcon, ChevronRightIcon } from "@humagen/render-web";
import { Button } from "@humagen/render-web/ui/button";
import { type ReactNode, useCallback, useEffect, useState } from "react";
import type {
  MemoryTreeView,
  RunMemory,
  RunMemoryTree,
  SessionMemoryTree,
} from "../../../packages/client/src/generated/types.ts";
import { href } from "./route.ts";
import { AgentMark, ago, SkeletonRows } from "./ui.tsx";
import { type ConversationMemory, workspaceFiles } from "./workspace-files.ts";

const words = defaultCopy.workspace.memories;

/** Where a tree is changed: the Library's Memory, or an agent's Memory tab. */
export function memoryEditHref(tree: MemoryTreeView): string | undefined {
  if (tree.kind === "person")
    return tree.self ? href({ page: "library", section: "memory", scope: "personal" }) : undefined;
  if (tree.kind === "project")
    return tree.project
      ? href({ page: "library", section: "memory", scope: "project", owner: tree.project })
      : href({ page: "library", section: "memory", scope: "workspace" });
  return tree.agent ? href({ page: "agent", name: tree.agent, tab: "memory" }) : undefined;
}

function TreeName({ tree }: { tree: MemoryTreeView }) {
  const name =
    tree.kind === "person"
      ? ""
      : tree.kind === "agent"
        ? (tree.agent ?? "")
        : tree.project || words.workspace;
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5">
      {tree.kind === "agent" && tree.agent ? (
        <AgentMark name={tree.agent} size={14} />
      ) : (
        <BrainIcon size={14} />
      )}
      <span className="truncate">
        {words.kind[tree.kind] ?? tree.kind}
        {name ? `: ${name}` : ""}
      </span>
    </span>
  );
}

function Tree({
  tree,
  children,
}: {
  tree: MemoryTreeView | RunMemoryTree | SessionMemoryTree;
  children?: ReactNode;
}) {
  const edit = memoryEditHref(tree);
  return (
    <li className="flex min-w-0 flex-col gap-1 rounded-aui border border-aui-border p-2.5">
      <div className="flex flex-wrap items-center justify-between gap-2 text-aui-sm">
        <TreeName tree={tree} />
        {edit && (
          <a
            href={edit}
            className="inline-flex min-h-9 items-center rounded-aui px-1 text-aui-xs font-medium underline underline-offset-4 focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:min-h-11"
          >
            {words.edit}
          </a>
        )}
      </div>
      <p className="flex flex-wrap gap-x-3 text-aui-xs text-aui-muted">
        <span className="font-mono">{tree.dir}</span>
        <span>{words.head(tree.head, tree.files)}</span>
        {tree.read_only && <span>{words.readOnly}</span>}
      </p>
      {children}
    </li>
  );
}

function Facts({ facts }: { facts: (string | false | undefined)[] }) {
  const shown = facts.filter(Boolean) as string[];
  if (!shown.length) return null;
  return (
    <p className="flex flex-wrap gap-x-3 text-aui-xs">
      {shown.map((f) => (
        <span key={f}>{f}</span>
      ))}
    </p>
  );
}

function RunDetail({ run, onBack }: { run: RunMemory; onBack: () => void }) {
  return (
    <div className="flex min-w-0 flex-col gap-3" data-slot="workspace-run-memory">
      <Button variant="ghost" size="sm" className="self-start" onClick={onBack}>
        {words.back}
      </Button>
      <h3 className="inline-flex min-w-0 items-center gap-2 text-aui-sm font-medium">
        <AgentMark name={run.agent} size={16} />
        <span className="truncate">{run.agent}</span>
        <span className="text-aui-xs font-normal text-aui-muted">{ago(run.created_at)}</span>
      </h3>
      {run.trees.length === 0 ? (
        <p className="text-aui-xs text-aui-muted">{words.none}</p>
      ) : (
        <ul className="flex min-w-0 flex-col gap-2">
          {run.trees.map((t) => (
            <Tree key={`${t.kind}:${t.project}:${t.agent}`} tree={t}>
              <Facts
                facts={[
                  t.installed ? words.installed(t.version ?? 0) : words.inContext,
                  !!t.committed && words.committed(t.committed),
                  !!t.proposals && words.proposals(t.proposals),
                  !!t.dropped && words.dropped(t.dropped),
                ]}
              />
            </Tree>
          ))}
        </ul>
      )}
      {run.notes.length > 0 && (
        <section aria-label={words.notesTitle} className="flex flex-col gap-1">
          <h4 className="text-aui-xs font-medium text-aui-muted">{words.notesTitle}</h4>
          <ul className="flex flex-col gap-1">
            {run.notes.map((n) => (
              <li key={n.id} className="rounded-aui border border-aui-border p-2 text-aui-xs">
                <p className="break-words">{n.text}</p>
                <p className="mt-1 text-aui-muted">
                  {words.kind[n.kind] ?? n.kind}
                  {n.owner ? `: ${n.owner}` : ""} {n.status}
                </p>
              </li>
            ))}
          </ul>
        </section>
      )}
      {run.hidden > 0 && <p className="text-aui-xs text-aui-muted">{words.hidden(run.hidden)}</p>}
    </div>
  );
}

/** Read only: what memory the conversation, its session and each message used and wrote. */
export function Memories({
  thread,
  open,
  selected,
  onSelect,
}: {
  thread: string;
  open: boolean;
  selected?: string;
  onSelect: (run?: string) => void;
}) {
  const [value, setValue] = useState<ConversationMemory>();
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const read = useCallback(() => workspaceFiles.memory(thread), [thread]);
  useEffect(() => {
    void attempt;
    if (!open) return;
    let live = true;
    setFailed(false);
    read().then(
      (next) => {
        if (live) setValue(next);
      },
      () => {
        if (live) setFailed(true);
      },
    );
    return () => {
      live = false;
    };
  }, [open, read, attempt]);
  if (failed)
    return (
      <div role="alert" className="flex flex-wrap items-center gap-2 text-aui-xs text-aui-bad">
        {words.failed}
        <Button variant="outline" size="sm" onClick={() => setAttempt((n) => n + 1)}>
          {words.retry}
        </Button>
      </div>
    );
  if (!value) return <SkeletonRows count={3} />;
  const run = selected ? value.runs.find((r) => r.run === selected) : undefined;
  if (run) return <RunDetail run={run} onBack={() => onSelect()} />;
  return (
    <div className="flex min-w-0 flex-col gap-4" data-slot="workspace-memories">
      <p className="text-aui-xs text-aui-muted">{words.hint(value.isolated)}</p>
      <section aria-label={words.conversation} className="flex flex-col gap-2">
        <h3 className="text-aui-xs font-medium text-aui-muted">{words.conversation}</h3>
        {value.conversation.length === 0 ? (
          <p className="text-aui-xs text-aui-muted">{words.none}</p>
        ) : (
          <ul className="flex min-w-0 flex-col gap-2">
            {value.conversation.map((t) => (
              <Tree key={`${t.kind}:${t.project}:${t.agent}`} tree={t} />
            ))}
          </ul>
        )}
        {value.hidden > 0 && (
          <p className="text-aui-xs text-aui-muted">{words.hidden(value.hidden)}</p>
        )}
      </section>
      <section aria-label={words.session} className="flex flex-col gap-2">
        <h3 className="text-aui-xs font-medium text-aui-muted">{words.session}</h3>
        {value.session.length === 0 ? (
          <p className="text-aui-xs text-aui-muted">{words.noRuns}</p>
        ) : (
          <ul className="flex min-w-0 flex-col gap-2">
            {value.session.map((t) => (
              <Tree key={`${t.kind}:${t.project}:${t.agent}`} tree={t}>
                <Facts
                  facts={[
                    words.runsUsing(t.runs),
                    words.versionsUsed(t.versions),
                    t.commits.length > 0 && words.commits(t.commits),
                    t.proposals > 0 && words.proposals(t.proposals),
                    t.notes > 0 && words.notes(t.notes),
                  ]}
                />
              </Tree>
            ))}
          </ul>
        )}
      </section>
      <section aria-label={words.runs} className="flex min-w-0 flex-col gap-1">
        <h3 className="text-aui-xs font-medium text-aui-muted">{words.runs}</h3>
        {value.runs.length === 0 ? (
          <p className="text-aui-xs text-aui-muted">{words.noRuns}</p>
        ) : (
          <ul className="flex min-w-0 flex-col">
            {value.runs.map((r) => (
              <li key={r.run} style={{ paddingInlineStart: `${Math.min(r.depth, 4) * 16}px` }}>
                <button
                  type="button"
                  className="flex min-h-11 w-full min-w-0 items-center gap-2 rounded-aui px-2 py-2 text-left text-aui-sm hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                  onClick={() => onSelect(r.run)}
                >
                  <AgentMark name={r.agent} size={14} />
                  <span className="min-w-0 flex-1 truncate">{r.agent}</span>
                  <span className="shrink-0 text-aui-xs text-aui-muted">
                    {r.trees.some((t) => t.committed || t.proposals) || r.notes.length
                      ? words.wrote
                      : words.versionsUsed(
                          r.trees.flatMap((t) => (t.installed ? [t.version ?? 0] : [])),
                        )}
                  </span>
                  <span className="shrink-0 text-aui-xs text-aui-muted">{ago(r.created_at)}</span>
                  <ChevronRightIcon size={14} />
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
