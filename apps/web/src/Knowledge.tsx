import { continuable, continueBody } from "@humagen/adapter/imported";
import { defaultCopy } from "@humagen/components";
import { Inspector } from "@humagen/web";
import { type FormEvent, useEffect, useRef, useState } from "react";
import {
  ApiError,
  api,
  type Doc,
  type Hit,
  type Revision,
  type Source,
  type SuiteView,
} from "./api.ts";
import { Evals } from "./Evals.tsx";
import { ConnectSource } from "./Integrations.tsx";
import type { SourceTab } from "./route.ts";
import { SourceCatalog, SourceCatalogLead, type SourceChoice } from "./SourceCatalog.tsx";
import {
  SourceAttachments,
  SourceLifecycle,
  SourceResources,
  SourceSyncSettings,
} from "./SourceLifecycle.tsx";
import { addTextFiles } from "./sourceUpload.ts";
import {
  ago,
  button,
  Chip,
  Confirm,
  card,
  Empty,
  field,
  Label,
  Menu,
  MoreMenu,
  Page,
  Problem,
  primary,
  rows,
  Tabs,
} from "./ui.tsx";

const continueWords = defaultCopy.continueImport;

/** Why a continuation was not started, as the reader is told, and whether reading the list again helps. */
export interface Refusal {
  message: string;
  reload?: boolean;
}

/**
 * What the reader is told when continuing a document is refused. The route
 * answers only a status and a sentence, with no stable reason: 409 and 400
 * each have several causes, so for them the server's sentence is shown as it
 * is, never a guess at which cause it was, and a 409 (something changed since
 * the page was read) offers to read the documents again. A status with one
 * meaning has the console's own words.
 */
export function continueRefusal(error: unknown): Refusal {
  if (!(error instanceof ApiError)) return { message: continueWords.failed };
  const said = error.message.trim() || continueWords.failed;
  switch (error.status) {
    case 409:
      return { message: said, reload: true };
    case 403:
      return { message: continueWords.forbidden };
    case 404:
      return { message: continueWords.gone, reload: true };
    case 413:
      return { message: continueWords.tooLarge };
    default:
      return { message: said };
  }
}

/**
 * The projects a source is filed in, as the reader sees them: the one it
 * belongs to, else those it is attached to, else the workspace itself (`~`).
 * Read only when wanted; null while it is read or when it cannot be.
 */
function useFiledIn(source: Source, wanted: boolean, reads: number): string[] | null {
  const scope = source.lifecycle?.project;
  const [attached, setAttached] = useState<string[] | null>(null);
  // biome-ignore lint/correctness/useExhaustiveDependencies: a source changed since, or a reload, is read again
  useEffect(() => {
    if (!wanted || scope) return;
    let live = true;
    setAttached(null);
    api
      .sourceAttachments(source.name)
      .then((list) => {
        if (!live) return;
        const names = [...new Set(list.map((a) => a.project || "~"))];
        setAttached(names.length === 0 ? ["~"] : names);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [source.name, source.updated_at, scope, wanted, reads]);
  return scope ? [scope] : attached;
}

/**
 * Every knowledge source, and one source at a time. A source is shared by the
 * agents that read it; its own page holds its documents, a search to try, its
 * variants and the evals that check what it retrieves.
 */
export function Knowledge({
  sources,
  agents,
  suites,
  selected,
  tab = "documents",
  onSelect,
  canWrite,
  canEval,
  canRun = false,
  onChanged,
  onSuitesChanged,
  onOpenAgent,
  onOpenThread,
  back,
}: {
  sources: Source[];
  agents: Revision[];
  suites: SuiteView[];
  /** The open source, by name. null lists them all. */
  selected: string | null;
  tab?: SourceTab;
  onSelect: (source: string | null, tab?: SourceTab) => void;
  canWrite: boolean;
  canEval: boolean;
  /** The reader may start runs, so may continue an imported conversation. */
  canRun?: boolean;
  onChanged: () => void;
  onSuitesChanged: () => void;
  onOpenAgent?: (name: string) => void;
  /** Opens a conversation, such as one just continued from a document. */
  onOpenThread?: (id: string) => void;
  /** Where a source's page leads back to: the corpus it was opened from, or Knowledge. */
  back?: { label: string; onClick: () => void };
}) {
  const setSelected = (name: string) => onSelect(name);
  const backTo = back ?? { label: defaultCopy.corpora.back, onClick: () => onSelect(null) };
  const [creating, setCreating] = useState(false);
  const [sourceKind, setSourceKind] = useState<SourceChoice>();
  const [draft, setDraft] = useState({ name: "", description: "" });
  const [problem, setProblem] = useState<string | null>(null);
  const current = sources.find((s) => s.name === selected);
  const readers = (name: string) =>
    agents
      .filter((a) => (a.spec.sources ?? []).some((x) => x.name === name))
      .map((a) => a.spec.name);

  if (selected) {
    return current ? (
      <SourceDetail
        key={current.name}
        source={current}
        tab={tab}
        onTab={(next) => onSelect(current.name, next)}
        back={backTo}
        usedBy={readers(current.name)}
        suites={suites}
        canWrite={canWrite}
        canEval={canEval}
        canRun={canRun && !!onOpenThread}
        onChanged={onChanged}
        onOpenThread={onOpenThread}
        onDeleted={() => {
          onChanged();
          onSelect(null);
        }}
        onSuitesChanged={onSuitesChanged}
        onOpenAgent={onOpenAgent}
      />
    ) : (
      <Page title={`No source named ${selected}`} lede="It may have been deleted." back={backTo}>
        {null}
      </Page>
    );
  }

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setProblem(null);
    try {
      await api.createSource(draft.name, draft.description);
      setSelected(draft.name);
      setDraft({ name: "", description: "" });
      setCreating(false);
      onChanged();
    } catch (err) {
      setProblem(err instanceof ApiError ? err.message : "The source could not be created.");
    }
  };

  return (
    <Page
      title="Sources"
      lede="What agents are grounded in: sources of documents they search and cite. Each source is shared: attach it to any agent from the agent's Context tab."
      actions={
        canWrite ? (
          <button
            type="button"
            className={primary}
            aria-haspopup="dialog"
            aria-expanded={creating}
            onClick={() => {
              setSourceKind(undefined);
              setCreating(true);
            }}
          >
            New Source
          </button>
        ) : undefined
      }
    >
      <p className="rounded-aui border border-aui-border px-3 py-2 text-aui-xs text-aui-muted">
        {defaultCopy.sourceLifecycle.grounding}
      </p>
      <Inspector
        open={creating}
        title={defaultCopy.sourceCatalog.title}
        lead={sourceKind ? undefined : <SourceCatalogLead />}
        onClose={() => setCreating(false)}
      >
        {!sourceKind ? (
          <SourceCatalog onSelect={setSourceKind} />
        ) : (
          <div className="space-y-4">
            <button type="button" className={button} onClick={() => setSourceKind(undefined)}>
              {defaultCopy.sourceCatalog.back}
            </button>
            {sourceKind === "manual" ? (
              <form onSubmit={create} className={`${card} flex flex-col gap-3`}>
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <Label text="Name" hint="Lowercase letters, digits and dashes.">
                    <input
                      className={field}
                      value={draft.name}
                      required
                      onChange={(e) => setDraft({ ...draft, name: e.target.value })}
                    />
                  </Label>
                  <Label text="Description">
                    <input
                      className={field}
                      value={draft.description}
                      onChange={(e) => setDraft({ ...draft, description: e.target.value })}
                    />
                  </Label>
                </div>
                <Problem message={problem} />
                <div className="flex justify-end gap-2">
                  <button type="button" className={button} onClick={() => setCreating(false)}>
                    Cancel
                  </button>
                  <button type="submit" className={primary}>
                    Create Source
                  </button>
                </div>
              </form>
            ) : (
              <ConnectSource
                key={sourceKind}
                initialProvider={sourceKind}
                onCancel={() => setCreating(false)}
                onCreated={(name) => {
                  setCreating(false);
                  onChanged();
                  setSelected(name);
                }}
              />
            )}
          </div>
        )}
      </Inspector>

      {sources.length === 0 ? (
        <div className={card}>
          <Empty title="No sources">
            {canWrite ? "Create one, then add documents to it." : "Ask an admin to add one."}
          </Empty>
        </div>
      ) : (
        <ul className={rows}>
          {sources.map((s) => {
            const by = readers(s.name);
            return (
              <li key={s.name} className="humagen-enter">
                <button
                  type="button"
                  onClick={() => setSelected(s.name)}
                  className="grid w-full grid-cols-1 items-center gap-x-6 gap-y-1 px-1 py-3 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent @3xl:grid-cols-[minmax(0,1fr)_14rem_12rem]"
                >
                  <span className="min-w-0">
                    <span className="block truncate text-aui-base font-medium text-aui-fg">
                      {s.name}
                    </span>
                    <span className="block truncate text-aui-sm text-aui-muted">
                      {s.description || "No description."}
                    </span>
                  </span>
                  <span className="text-aui-xs text-aui-muted tabular-nums">
                    {s.documents} {s.documents === 1 ? "document" : "documents"}, {s.passages}{" "}
                    passages
                  </span>
                  <span className="truncate text-aui-xs text-aui-muted">
                    {by.length === 0 ? "Read by no agent" : `Read by ${by.join(", ")}`}
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </Page>
  );
}

function SourceDetail({
  source,
  tab,
  onTab,
  back,
  usedBy,
  suites,
  canWrite,
  canEval,
  canRun,
  onChanged,
  onOpenThread,
  onDeleted,
  onSuitesChanged,
  onOpenAgent,
}: {
  source: Source;
  tab: SourceTab;
  onTab: (next: SourceTab) => void;
  back: { label: string; onClick: () => void };
  usedBy: string[];
  suites: SuiteView[];
  canWrite: boolean;
  canEval: boolean;
  canRun: boolean;
  onChanged: () => void;
  onOpenThread?: (id: string) => void;
  onDeleted: () => void;
  onSuitesChanged: () => void;
  onOpenAgent?: (name: string) => void;
}) {
  const canAddDocuments =
    canWrite &&
    (!source.lifecycle ||
      (source.lifecycle.provider === "manual" && source.lifecycle.mode === "indexed"));
  const upstream = Boolean(source.lifecycle && source.lifecycle.provider !== "manual");
  const frozen = upstream && source.lifecycle?.snapshot;
  const [deleting, setDeleting] = useState(false);
  const [docs, setDocs] = useState<Doc[]>([]);
  const [draft, setDraft] = useState({ title: "", uri: "", text: "" });
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<Hit[] | null>(null);
  const [warnings, setWarnings] = useState<{ source: string; cause: string }[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [variant, setVariant] = useState("");
  const [saveAs, setSaveAs] = useState("");
  const [pasting, setPasting] = useState(false);
  const picker = useRef<HTMLInputElement>(null);
  const [reads, setReads] = useState(0);

  // biome-ignore lint/correctness/useExhaustiveDependencies: reads asks for the list again
  useEffect(() => {
    if (upstream) {
      setDocs([]);
      return;
    }
    let live = true;
    api
      .documents(source.name)
      .then((d) => live && setDocs(d))
      .catch(() => live && setDocs([]));
    return () => {
      live = false;
    };
  }, [source.name, source.documents, source.excluded, upstream, reads]);

  // Continuing an imported conversation: which documents offer it, the
  // projects the source is filed in, and the one being started.
  const offers = canRun && docs.some(continuable);
  const places = useFiledIn(source, offers, reads);
  const [continuing, setContinuing] = useState<Doc | null>(null);
  const [refusal, setRefusal] = useState<Refusal | null>(null);
  const proceed = (doc: Doc, project: string) => {
    if (continuing || !onOpenThread) return;
    setContinuing(doc);
    setRefusal(null);
    api
      .continueImport(source.name, doc.id, continueBody(doc, project))
      .then((thread) => onOpenThread(thread.id))
      .catch((error: unknown) => setRefusal(continueRefusal(error)))
      .finally(() => setContinuing(null));
  };

  const fail = (fallback: string) => (e: unknown) =>
    setProblem(e instanceof ApiError ? e.message : fallback);

  const add = (e: FormEvent) => {
    e.preventDefault();
    setProblem(null);
    api
      .addDocument(source.name, draft)
      .then(() => {
        setDraft({ title: "", uri: "", text: "" });
        onChanged();
      })
      .catch(fail("The document could not be added."));
  };

  const search = (e: FormEvent) => {
    e.preventDefault();
    setProblem(null);
    setWarnings([]);
    api
      .searchKnowledgeDetailed(query, source.name, variant)
      .then((result) => {
        setHits(result.hits);
        setWarnings(result.warnings ?? []);
      })
      .catch(fail("The search failed."));
  };

  const upload = async (picked: FileList | null) => {
    if (!picked) return;
    setProblem(null);
    const skipped = await addTextFiles(source.name, Array.from(picked));
    onChanged();
    if (skipped.length > 0) setProblem(skipped.join(". "));
  };

  const mine = suites.filter((x) => x.target === "knowledge" && x.subject === source.name);
  return (
    <Page
      sections={{
        label: "Source sections",
        value: tab,
        onChange: (id) => onTab(id as typeof tab),
        items: [
          { id: "documents", label: "Documents", count: upstream ? undefined : docs.length },
          { id: "search", label: "Try a Question" },
          ...(!upstream
            ? [{ id: "variants", label: "Variants", count: source.variants.length }]
            : []),
          { id: "evals", label: "Evals", count: mine.length },
        ],
      }}
      back={back}
      title={source.name}
      lede={source.description || undefined}
      meta={
        <>
          <span>Updated {ago(source.updated_at)}</span>
          <span>
            {source.documents} {source.documents === 1 ? "document" : "documents"},{" "}
            {source.passages} passages
          </span>
          {usedBy.length === 0 ? (
            <Chip>Read by no agent</Chip>
          ) : (
            usedBy.map((a) =>
              onOpenAgent ? (
                <button
                  key={a}
                  type="button"
                  className="underline hover:text-aui-fg"
                  onClick={() => onOpenAgent(a)}
                >
                  Read by {a}
                </button>
              ) : (
                <Chip key={a}>Read by {a}</Chip>
              ),
            )
          )}
        </>
      }
      actions={
        canWrite ? (
          deleting ? (
            <Confirm
              question={`Delete ${source.name} and its indexed documents?`}
              action="Delete Source"
              onKeep={() => setDeleting(false)}
              onConfirm={() => {
                setDeleting(false);
                api
                  .deleteSource(source.name)
                  .then(onDeleted)
                  .catch(fail("The source could not be deleted."));
              }}
            />
          ) : (
            <button type="button" className={button} onClick={() => setDeleting(true)}>
              Delete Source
            </button>
          )
        ) : undefined
      }
    >
      <Problem message={problem} />
      <SourceLifecycle source={source} canWrite={canWrite} onChanged={onChanged} />
      <SourceAttachments
        key={`${source.name}:${source.updated_at}:attachments`}
        source={source}
        canWrite={canWrite}
        onChanged={onChanged}
      />
      {source.lifecycle && source.lifecycle.provider !== "manual" && (
        <>
          <SourceResources
            key={`${source.name}:${source.updated_at}:resources`}
            source={source}
            canWrite={canWrite}
            onChanged={onChanged}
          />
          <SourceSyncSettings
            key={`${source.name}:${source.updated_at}:sync`}
            source={source}
            canWrite={canWrite}
            onChanged={onChanged}
          />
        </>
      )}

      {tab === "search" && (
        <section className={`${card} flex flex-col gap-2`}>
          <p className="text-aui-sm text-aui-muted">
            What an agent asking this would get back, best match first.
          </p>
          {frozen ? (
            <p className="text-aui-sm leading-6 text-aui-muted">
              {defaultCopy.sourceLifecycle.frozenAccess}
            </p>
          ) : (
            <form onSubmit={search} className="flex gap-2">
              <input
                aria-label="Question"
                className={field}
                placeholder="What would an agent ask this source?"
                value={query}
                required
                onChange={(e) => setQuery(e.target.value)}
              />
              {source.variants.length > 0 && (
                <Menu
                  label="Search in"
                  className="w-44 shrink-0"
                  value={variant}
                  options={[
                    { value: "", label: "Current view" },
                    ...source.variants.map((v) => ({ value: v.name, label: v.name })),
                  ]}
                  onChange={(next) => {
                    setVariant(next);
                    setHits(null);
                  }}
                />
              )}
              <button type="submit" className={button}>
                Search
              </button>
            </form>
          )}
          {warnings.length > 0 && (
            <output className="text-aui-sm">{defaultCopy.sourceLifecycle.liveUnavailable}</output>
          )}
          {hits !== null &&
            (hits.length === 0 ? (
              <p className="text-aui-sm text-aui-muted">
                Nothing in this source matches those words.
              </p>
            ) : (
              <ol className="flex flex-col gap-2 pt-1">
                {hits.map((h, i) => (
                  <li
                    key={`${h.source}:${h.mode}:${h.document_id}:${h.index}`}
                    className="rounded-aui border border-aui-border p-3"
                  >
                    <div className="flex items-center justify-between gap-2 pb-1">
                      <span className="text-aui-xs font-medium text-aui-fg">
                        {i + 1}. {h.title}
                        {h.mode && <Chip>{defaultCopy.sourceLifecycle.modes[h.mode]}</Chip>}
                      </span>
                      <span className="text-aui-2xs tabular-nums text-aui-muted">
                        score {h.score.toFixed(2)}
                      </span>
                    </div>
                    <p className="whitespace-pre-wrap text-aui-sm text-aui-muted">{h.text}</p>
                  </li>
                ))}
              </ol>
            ))}
        </section>
      )}

      {tab === "documents" && upstream && (
        <p className={`${card} text-aui-sm leading-6 text-aui-muted`}>
          {frozen
            ? defaultCopy.sourceLifecycle.frozenAccess
            : defaultCopy.sourceLifecycle.retainedAccess}
        </p>
      )}
      {tab === "documents" && !upstream && (
        <section className={`${card} flex flex-col gap-2`}>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="text-aui-sm font-medium text-aui-fg">
              Documents ({docs.length}
              {source.excluded > 0 ? `, ${source.excluded} excluded` : ""})
            </h3>
            {canAddDocuments && (
              <div className="flex gap-2">
                <input
                  ref={picker}
                  type="file"
                  multiple
                  className="sr-only"
                  tabIndex={-1}
                  aria-hidden
                  onChange={(e) => {
                    void upload(e.target.files);
                    e.target.value = "";
                  }}
                />
                <button type="button" className={primary} onClick={() => picker.current?.click()}>
                  Upload Files
                </button>
                <button
                  type="button"
                  className={button}
                  aria-expanded={pasting}
                  onClick={() => setPasting(!pasting)}
                >
                  Paste Text
                </button>
              </div>
            )}
          </div>
          {canAddDocuments && pasting && (
            <form onSubmit={add} className="flex flex-col gap-3 border-aui-border border-b pb-3">
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <Label text="Title">
                  <input
                    className={field}
                    value={draft.title}
                    required
                    onChange={(e) => setDraft({ ...draft, title: e.target.value })}
                  />
                </Label>
                <Label text="Link">
                  <input
                    className={field}
                    value={draft.uri}
                    onChange={(e) => setDraft({ ...draft, uri: e.target.value })}
                  />
                </Label>
              </div>
              <Label text="Text">
                <textarea
                  className={`${field} min-h-28`}
                  value={draft.text}
                  required
                  onChange={(e) => setDraft({ ...draft, text: e.target.value })}
                />
              </Label>
              <div className="flex justify-end">
                <button type="submit" className={primary}>
                  Add Document
                </button>
              </div>
            </form>
          )}
          {continuing && (
            <output className="text-aui-sm text-aui-muted">
              {continueWords.starting(continuing.title)}
            </output>
          )}
          {refusal && (
            <div className="flex flex-col items-start gap-2">
              <Problem message={refusal.message} />
              {refusal.reload && (
                <button
                  type="button"
                  className={button}
                  onClick={() => {
                    setRefusal(null);
                    setReads((n) => n + 1);
                    onChanged();
                  }}
                >
                  {continueWords.reload}
                </button>
              )}
            </div>
          )}
          {docs.length === 0 ? (
            <p className="text-aui-sm text-aui-muted">Empty. Upload text files or paste text.</p>
          ) : (
            <ul>
              {docs.map((d) => (
                <li
                  key={d.id}
                  className="flex flex-wrap items-center gap-x-3 gap-y-1 border-aui-border border-t py-2 first:border-t-0"
                >
                  {/* A narrow row puts its controls under the title rather than cutting it short. */}
                  <div className={`min-w-0 flex-1 basis-48 ${d.excluded ? "opacity-55" : ""}`}>
                    <p className="truncate text-aui-sm text-aui-fg">{d.title}</p>
                    <p className="truncate text-aui-xs text-aui-muted">
                      {d.excluded ? "Excluded from search. " : ""}
                      {d.passages} passages, {Math.max(1, Math.round(d.bytes / 1024))} KB, added{" "}
                      {ago(d.added_at)}
                    </p>
                  </div>
                  <div className="ml-auto flex shrink-0 items-center gap-3">
                    {canWrite && (
                      <>
                        <label className="inline-flex min-h-8 shrink-0 items-center gap-1.5 text-aui-xs text-aui-muted">
                          Searchable
                          <input
                            type="checkbox"
                            className="humagen-switch"
                            checked={!d.excluded}
                            onChange={(e) =>
                              api
                                .excludeDocument(source.name, d.id, !e.target.checked)
                                .then(onChanged)
                                .catch(fail("The document could not be changed."))
                            }
                          />
                        </label>
                        <button
                          type="button"
                          className={button}
                          onClick={() =>
                            api
                              .removeDocument(source.name, d.id)
                              .then(onChanged)
                              .catch(fail("The document could not be removed."))
                          }
                        >
                          Remove
                        </button>
                      </>
                    )}
                    {canRun && continuable(d) && (
                      <MoreMenu
                        label={continueWords.menu(d.title)}
                        items={(places ?? [""]).map((project) => ({
                          label:
                            places && places.length > 1
                              ? continueWords.actionIn(
                                  project === "~" ? continueWords.workspace : project,
                                )
                              : continueWords.action,
                          // Not while the projects are read or another is starting; an
                          // excluded document is not found by the server.
                          disabled: !places || !!continuing || d.excluded,
                          onSelect: () => proceed(d, project),
                        }))}
                      />
                    )}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

      {tab === "variants" && upstream && (
        <p className={`${card} text-aui-sm leading-6 text-aui-muted`}>
          {defaultCopy.sourceLifecycle.retainedAccess}
        </p>
      )}
      {tab === "variants" && !upstream && (
        <section className={`${card} flex flex-col gap-2`}>
          <p className="text-aui-xs text-aui-muted">
            A variant saves which documents are searchable right now, so you can search two ways of
            curating this source side by side.
          </p>
          {source.variants.length > 0 && (
            <ul>
              {source.variants.map((v) => (
                <li
                  key={v.name}
                  className="flex items-center justify-between gap-3 border-aui-border border-t py-2 first:border-t-0"
                >
                  <span className="text-aui-sm text-aui-fg">
                    {v.name}
                    <span className="pl-2 text-aui-xs text-aui-muted">
                      {source.documents - v.excluded.length} of {source.documents} documents
                    </span>
                  </span>
                  {canWrite && (
                    <button
                      type="button"
                      className={button}
                      onClick={() => {
                        if (variant === v.name) setVariant("");
                        api
                          .deleteVariant(source.name, v.name)
                          .then(onChanged)
                          .catch(fail("The variant could not be deleted."));
                      }}
                    >
                      Delete
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
          {canWrite && (
            <form
              className="flex gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                setProblem(null);
                api
                  .saveVariant(source.name, saveAs)
                  .then(() => {
                    setSaveAs("");
                    onChanged();
                  })
                  .catch(fail("The variant could not be saved."));
              }}
            >
              <input
                aria-label="Variant name"
                className={field}
                placeholder="Name this view, such as no-drafts"
                value={saveAs}
                required
                onChange={(e) => setSaveAs(e.target.value)}
              />
              <button type="submit" className={button}>
                Save Variant
              </button>
            </form>
          )}
        </section>
      )}

      {tab === "evals" && (
        <Evals
          target="knowledge"
          subject={source.name}
          suites={suites}
          canWrite={canEval}
          onChanged={onSuitesChanged}
        />
      )}
    </Page>
  );
}
