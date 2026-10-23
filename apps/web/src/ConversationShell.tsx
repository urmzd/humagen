import { defaultCopy } from "@humagen/components";
import {
  ArtifactOpenContext,
  FolderIcon,
  LayersIcon,
  SettingsIcon,
  UsersIcon,
} from "@humagen/render-web";
import { Inspector } from "@humagen/web";
import { type ReactNode, type RefObject, useEffect, useState } from "react";
import {
  api,
  coworking,
  type Doc,
  type ProjectKnowledge,
  type Source,
  type Thread,
} from "./api.ts";
import type { ConversationPanel } from "./route.ts";
import { AgentMark, Page, Popover, Problem, SkeletonRows } from "./ui.tsx";

const words = defaultCopy.conversation;
const control =
  "inline-flex min-h-8 min-w-8 items-center justify-center gap-1.5 rounded-aui px-2 text-aui-xs text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent";

/** The conversation's own page header, used by a room and by older run-based chats. */
export function ConversationPage({
  title,
  project,
  private: personal,
  agents,
  location,
  onProject,
  projectRef,
  onPanel,
  onArtifact,
  children,
}: {
  title: string;
  project: string;
  private: boolean;
  agents: string[];
  location?: ReactNode;
  onProject?: () => void;
  projectRef?: RefObject<HTMLButtonElement | null>;
  onPanel: (panel: ConversationPanel) => void;
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
        start={
          <button
            ref={projectRef}
            type="button"
            className={`${control} max-w-full justify-start`}
            title={words.projectAction}
            aria-label={personal && onProject ? defaultCopy.threads.share : project}
            onClick={onProject}
            disabled={!onProject}
          >
            <FolderIcon size={15} />
            <span className="truncate">{project}</span>
          </button>
        }
        meta={
          <>
            <span>{location ?? (personal ? words.private : words.shared)}</span>
            {agents.length > 0 && <span> · {words.agents(new Set(agents).size)}</span>}
          </>
        }
        actions={
          <>
            <button
              type="button"
              className={control}
              aria-label={words.context}
              onClick={() => onPanel("context")}
            >
              <LayersIcon size={16} />
              <span className="hidden xl:inline">{words.context}</span>
            </button>
            <button
              type="button"
              className={control}
              aria-label={words.people}
              onClick={() => onPanel("people")}
            >
              <UsersIcon size={16} />
            </button>
            <Popover
              label={words.settings}
              button={<SettingsIcon size={16} />}
              className={control}
              align="right"
              narrow
            >
              {(close) => (
                <div className="flex flex-col p-1">
                  {(["activity", "deliverables"] as const).map((panel) => (
                    <button
                      key={panel}
                      type="button"
                      className={`${control} justify-start`}
                      onClick={() => {
                        close();
                        onPanel(panel);
                      }}
                    >
                      {words[panel]}
                    </button>
                  ))}
                </div>
              )}
            </Popover>
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
  return (
    <div className="flex flex-col gap-4">
      <p className="text-aui-xs text-aui-muted">{words.contextHint}</p>
      {data.links.given.length === 0 && (
        <p className="text-aui-sm text-aui-muted">{words.sourcesEmpty}</p>
      )}
      {data.links.given.map((link) => (
        <SourceCard
          key={link.name}
          name={link.name}
          source={data.sources.find((source) => source.name === link.name)}
        />
      ))}
    </div>
  );
}

function SourceCard({ name, source }: { name: string; source?: Source }) {
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
        <span className="mt-1 block text-aui-xs font-normal text-aui-muted">
          {words.sourceMode} · {words.sourceUpdates}
          {source ? ` · ${words.sourceDocs(source.documents)}` : ""}
        </span>
      </summary>
      {source?.description && (
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
              <span className="text-aui-muted">
                {words.sourcePassages(doc.passages)} ·{" "}
                {doc.excluded
                  ? words.sourceExcluded
                  : doc.passages > 0
                    ? words.sourceHealth
                    : words.sourceEmpty}
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
  panel,
  thread,
  project,
  onClose,
}: {
  panel?: ConversationPanel;
  thread: Thread;
  project: string;
  onClose: () => void;
}) {
  if (panel !== "context" && panel !== "people") return null;
  return (
    <Inspector open title={words[panel]} onClose={onClose}>
      {panel === "context" ? (
        <Sources project={thread.project} />
      ) : (
        <div className="flex flex-col gap-4">
          <p className="text-aui-xs text-aui-muted">
            {thread.visibility === "private" ? words.private : `${words.shared} · ${project}`}
          </p>
          <ul className="flex flex-col divide-aui-border divide-y">
            {thread.participants.map((person) => (
              <li
                key={`${person.kind}:${person.name}`}
                className="flex min-w-0 items-center gap-2 py-3 text-aui-sm"
              >
                {person.kind === "agent" && <AgentMark name={person.name} size={15} />}
                <span className="min-w-0 flex-1 truncate">{person.name}</span>
                <span className="text-aui-2xs text-aui-muted">
                  {person.kind}
                  {person.owner ? " · owner" : ""}
                </span>
              </li>
            ))}
          </ul>
          {thread.participants.length === 0 && (
            <p className="text-aui-sm text-aui-muted">{words.peopleEmpty}</p>
          )}
        </div>
      )}
    </Inspector>
  );
}
