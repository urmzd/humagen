import { defaultCopy } from "@humagen/components";
import {
  ArtifactOpenContext,
  InfoIcon,
  LayersIcon,
  ShareIcon,
  UsersIcon,
} from "@humagen/render-web";
import { Inspector } from "@humagen/web";
import { type ReactNode, type RefObject, useContext, useEffect, useState } from "react";
import type { Run } from "./adapter.ts";
import {
  api,
  coworking,
  type Doc,
  type ProjectKnowledge,
  type Source,
  type Thread,
} from "./api.ts";
import { ConversationAccess } from "./ConversationAccess.tsx";
import { ForkContext } from "./ForkConversation.tsx";
import { SessionReferences } from "./MessageRefs.tsx";
import { PersonMark } from "./PersonMark.tsx";
import { ProjectContextPreview } from "./ProjectContextPreview.tsx";
import { type ConversationPanel, type ConversationView, href } from "./route.ts";
import { DetailsNavigation, SessionRelations } from "./SessionDetails.tsx";
import type { Reference } from "./trace.ts";
import { AgentMark, Page, Problem, SkeletonRows, Tabs } from "./ui.tsx";

const words = defaultCopy.conversation;
const focus =
  "rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-aui-accent";
/** A row in the settings menu. */
const control = `inline-flex min-h-9 pointer-coarse:min-h-11 items-center gap-2 px-2.5 text-aui-xs ${focus}`;
/** Header actions are dense, like the console's other headers: icon-sized, one 4px gap, no side padding. */
const headerIcon = `inline-flex size-9 shrink-0 pointer-coarse:size-11 items-center justify-center ${focus}`;

/**
 * Who took part, at a glance: the first few agents by mark and name, then how
 * many more. Every name is in the label and on hover.
 */
export function Involved({ agents, shown = 3 }: { agents: string[]; shown?: number }) {
  const first = agents.slice(0, shown);
  const rest = agents.slice(shown);
  return (
    <span
      role="group"
      aria-label={words.agentsInvolved(agents)}
      title={agents.join(", ")}
      className="inline-flex min-w-0 items-center gap-x-2.5"
    >
      {first.map((name) => (
        <span key={name} className="inline-flex min-w-0 items-center gap-1" aria-hidden>
          <AgentMark name={name} size={12} />
          <span className="max-w-28 truncate">{name}</span>
        </span>
      ))}
      {rest.length > 0 && (
        <span aria-hidden className="tabular-nums">
          {words.agentsMore(rest.length)}
        </span>
      )}
    </span>
  );
}

/** The people in a conversation as overlapping initials, the first three and how many more. */
export function Faces({ people, shown = 3 }: { people: string[]; shown?: number }) {
  const unique = [...new Set(people)];
  if (unique.length === 0) return null;
  const rest = unique.length - shown;
  return (
    <span aria-hidden title={unique.join(", ")} className="inline-flex items-center">
      <span className="flex -space-x-1.5">
        {unique.slice(0, shown).map((name) => (
          <span key={name} className="rounded-full ring-2 ring-aui-bg">
            <PersonMark name={name} size={22} />
          </span>
        ))}
      </span>
      {rest > 0 && <span className="ml-1 tabular-nums">{words.agentsMore(rest)}</span>}
    </span>
  );
}

/** The conversation's own page header, used by a room and by older run-based chats. */
export function ConversationPage({
  title,
  project,
  private: personal,
  location,
  origin,
  onProject,
  projectRef,
  onPanel,
  panel: activePanel,
  onClosePanel,
  onArtifact,
  children,
  actions,
}: {
  actions?: ReactNode;
  title: string;
  project: string;
  private: boolean;
  agents: string[];
  /** The people in it, by name, drawn as overlapping faces on the Share or People control. */
  people?: string[];
  location?: ReactNode;
  /** A link back to where the session started, first among the header's icons. */
  origin?: ReactNode;
  onProject?: () => void;
  projectRef?: RefObject<HTMLButtonElement | null>;
  panel?: ConversationPanel;
  onPanel: (panel: ConversationPanel) => void;
  onClosePanel?: () => void;
  onArtifact?: (id: string) => void;
  children: ReactNode;
}) {
  return (
    <ArtifactOpenContext.Provider
      value={onArtifact ? (file) => onArtifact(file.artifactId ?? file.id) : null}
    >
      <Page
        layout="conversation"
        title={title}
        meta={
          <span className="inline-flex flex-wrap items-center gap-x-3">
            <span>{project}</span>
            {location}
          </span>
        }
        actions={
          <>
            {/* Icons only, in a fixed order: where it started, Details, People, then the ⋯ menu. */}
            {origin}
            <button
              type="button"
              data-session-details
              className={headerIcon}
              aria-label="Details"
              title="Details"
              aria-expanded={!!activePanel && activePanel !== "people"}
              onClick={() =>
                activePanel && activePanel !== "people" ? onClosePanel?.() : onPanel("workspace")
              }
            >
              <InfoIcon size={18} />
            </button>
            {/* One control for who is in it: Share where it can be shared, else People & Access. Both open the same panel. */}
            {personal && onProject ? (
              <button
                ref={projectRef}
                type="button"
                className={headerIcon}
                aria-label={defaultCopy.threads.share}
                title={defaultCopy.threads.share}
                onClick={onProject}
              >
                <ShareIcon size={18} />
              </button>
            ) : (
              <button
                ref={projectRef}
                type="button"
                className={headerIcon}
                aria-label={words.people}
                title={words.people}
                onClick={() => onPanel("people")}
              >
                <UsersIcon size={18} />
              </button>
            )}
            {actions}
          </>
        }
      >
        {children}
      </Page>
    </ArtifactOpenContext.Provider>
  );
}

function Sources({ project }: { project: string }) {
  const [data, setData] = useState<{ links: ProjectKnowledge; sources: Source[] }>();
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let current = true;
    setData(undefined);
    setFailed(false);
    Promise.all([coworking.knowledge(project), api.sources()])
      .then(([links, sources]) => {
        if (current) setData({ links, sources });
      })
      .catch(() => {
        if (current) setFailed(true);
      });
    return () => {
      current = false;
    };
  }, [project]);
  if (failed) return <Problem message={words.sourcesFailed} />;
  if (!data) return <SkeletonRows count={3} />;
  // Only render sources returned by both reader-confined responses.
  const sources = data.links.given.flatMap((link) => {
    const source = data.sources.find((item) => item.name === link.name);
    return source ? [source] : [];
  });
  return (
    <div className="flex flex-col gap-4">
      {sources.length > 0 ? (
        <p className="text-aui-xs text-aui-muted">{words.contextHint}</p>
      ) : (
        <div className="flex flex-col items-start gap-3 py-6">
          <LayersIcon size={24} className="text-aui-muted" />
          <p className="text-aui-sm text-aui-muted">{words.sourcesEmpty}</p>
          <a
            href={href({ page: "project", name: project })}
            className="inline-flex min-h-9 items-center rounded-aui text-aui-xs font-medium text-aui-fg underline underline-offset-4 focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            {words.artifactOpenContext}
          </a>
        </div>
      )}
      {sources.map((source) => (
        <SourceCard key={source.name} source={source} />
      ))}
    </div>
  );
}

function SourceCard({ source }: { source: Source }) {
  const name = source.name;
  const lifecycle = source.lifecycle;
  const labels = defaultCopy.sourceLifecycle;
  const mode = lifecycle?.mode ?? "indexed";
  const status = lifecycle?.status ?? (source.passages > 0 ? "available" : "connected");
  const lastGood =
    lifecycle?.last_good_at ?? (!lifecycle && source.passages > 0 ? source.updated_at : undefined);
  const [docs, setDocs] = useState<Doc[]>();
  const [visible, setVisible] = useState(20);
  const [failed, setFailed] = useState(false);
  return (
    <details
      className="min-w-0 rounded-aui border border-aui-border p-3"
      onToggle={(event) => {
        if (event.currentTarget.open && !docs && !failed)
          api
            .documents(name)
            .then(setDocs)
            .catch(() => setFailed(true));
      }}
    >
      <summary className="cursor-pointer text-aui-sm font-medium">
        {name}
        <span className="mt-1 flex flex-wrap gap-x-3 text-aui-xs font-normal text-aui-muted">
          <span>{labels.modes[mode]}</span>
          <span>{labels.status[status]}</span>
          <span>{words.sourceDocs(source.documents)}</span>
        </span>
      </summary>
      <dl className="mt-3 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-aui-xs">
        <dt className="text-aui-muted">{defaultCopy.memoryLibrary.scope}</dt>
        <dd className="break-words">{lifecycle?.project || defaultCopy.skillsLibrary.workspace}</dd>
        {lifecycle?.owner && (
          <>
            <dt className="text-aui-muted">{labels.owner}</dt>
            <dd className="break-words">{lifecycle.owner}</dd>
          </>
        )}
        {lifecycle?.origin && (
          <>
            <dt className="text-aui-muted">{words.sourceOrigin}</dt>
            <dd className="break-all">
              {lifecycle.provider}: {lifecycle.origin}
            </dd>
          </>
        )}
        {mode !== "live" && (
          <>
            <dt className="text-aui-muted">{labels.lastGood}</dt>
            <dd>
              {lastGood ? (
                <time dateTime={lastGood}>{new Date(lastGood).toLocaleString()}</time>
              ) : (
                labels.noCopy
              )}
            </dd>
          </>
        )}
      </dl>
      {lifecycle?.cause && (
        <p className="mt-2 text-aui-xs text-aui-muted">{labels.cause[lifecycle.cause]}</p>
      )}
      {source.description && (
        <p className="mt-3 text-aui-xs text-aui-muted">{source.description}</p>
      )}
      {failed ? (
        <Problem message={words.sourcesFailed} />
      ) : !docs ? (
        <SkeletonRows count={2} />
      ) : (
        <ul className="mt-3 flex flex-col divide-aui-border divide-y">
          {docs.slice(0, visible).map((doc) => (
            <li key={doc.id} className="min-w-0 py-3 text-aui-xs">
              <strong className="block break-words font-medium">{doc.title}</strong>
              {doc.uri && (
                <span className="mt-1 block break-all text-aui-muted">
                  {words.sourceOrigin}: {doc.uri}
                </span>
              )}
              <time className="mt-1 block text-aui-muted" dateTime={doc.added_at}>
                {words.sourceImported}: {new Date(doc.added_at).toLocaleString()}
              </time>
              <span className="flex flex-wrap gap-x-3 text-aui-muted">
                <span>{words.sourcePassages(doc.passages)}</span>
                <span>
                  {doc.excluded
                    ? words.sourceExcluded
                    : doc.passages > 0
                      ? words.sourceHealth
                      : words.sourceEmpty}
                </span>
              </span>
            </li>
          ))}
          {docs.length > visible && (
            <li>
              <button
                type="button"
                className={`${control} mt-2`}
                onClick={() => setVisible((count) => count + 20)}
              >
                {words.more}
              </button>
            </li>
          )}
        </ul>
      )}
    </details>
  );
}

export function ConversationInfo({
  canWrite = false,
  runs = [],
  origin,
  contextTab,
  onContextTab,
  panel,
  thread,
  onClose,
}: {
  runs?: Run[];
  /** Where the session started, the first reference of its root message. */
  origin?: Reference;
  view?: ConversationView;
  panel?: ConversationPanel;
  contextTab?: "sources" | "instructions";
  onContextTab?: (tab: "sources" | "instructions") => void;
  thread: Thread;
  canWrite?: boolean;
  project: string;
  onClose: () => void;
}) {
  const navigation = useContext(DetailsNavigation);
  const [ownTab, setOwnTab] = useState<"sources" | "instructions">("sources");
  const selected = contextTab ?? ownTab;
  const select = (next: "sources" | "instructions") =>
    onContextTab ? onContextTab(next) : setOwnTab(next);
  return (
    <Inspector
      open={panel === "overview" || panel === "context" || panel === "people"}
      title={panel === "people" ? "People" : "Details"}
      wide
      lead={panel === "people" ? undefined : navigation}
      onClose={onClose}
    >
      {panel === "overview" ? (
        <SessionRelations id={thread.id} canWrite={canWrite && !thread.read_only} />
      ) : panel === "context" ? (
        <div className="flex flex-col gap-6">
          <ForkContext thread={thread} />
          <Tabs
            label={words.contextSections}
            value={selected}
            tabs={[
              { id: "sources", label: words.sourcesTab },
              { id: "instructions", label: words.instructionsTab },
            ]}
            onChange={select}
          />
          {selected === "instructions" ? (
            <ProjectContextPreview
              key={`${thread.id}:${thread.version}`}
              project={thread.project}
              version={thread.version}
              initialAgent={
                thread.participants.find((participant) => participant.kind === "agent")?.name
              }
              initialChannel={thread.channel}
            />
          ) : (
            <>
              <SessionReferences runs={runs} origin={origin} />
              <Sources project={thread.project} />
            </>
          )}
        </div>
      ) : (
        <ConversationAccess thread={thread} />
      )}
    </Inspector>
  );
}
