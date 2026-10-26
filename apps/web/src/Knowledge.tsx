import { defaultCopy } from "@humagen/components";
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
import type { SourceTab } from "./route.ts";
import { SourceLifecycle } from "./SourceLifecycle.tsx";
import {
  ago,
  button,
  Chip,
  card,
  Empty,
  field,
  Label,
  Menu,
  Page,
  Problem,
  primary,
  rows,
  Tabs,
} from "./ui.tsx";

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
  onChanged,
  onSuitesChanged,
  onOpenAgent,
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
  onChanged: () => void;
  onSuitesChanged: () => void;
  onOpenAgent?: (name: string) => void;
}) {
  const setSelected = (name: string) => onSelect(name);
  const [creating, setCreating] = useState(false);
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
        onBack={() => onSelect(null)}
        usedBy={readers(current.name)}
        suites={suites}
        canWrite={canWrite}
        canEval={canEval}
        onChanged={onChanged}
        onDeleted={() => {
          onChanged();
          onSelect(null);
        }}
        onSuitesChanged={onSuitesChanged}
        onOpenAgent={onOpenAgent}
      />
    ) : (
      <Page
        title={`No source named ${selected}`}
        lede="It may have been deleted."
        back={{ label: "All sources", onClick: () => onSelect(null) }}
      >
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
      title="Context"
      lede="What agents are grounded in: sources of documents they search and cite. Each source is shared: attach it to any agent from the agent's Context tab."
      actions={
        canWrite && !creating ? (
          <button type="button" className={primary} onClick={() => setCreating(true)}>
            New Source
          </button>
        ) : undefined
      }
    >
      <p className="rounded-aui border border-aui-border px-3 py-2 text-aui-xs text-aui-muted">
        {defaultCopy.sourceLifecycle.grounding}
      </p>
      {creating && (
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
      )}

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
  onBack,
  usedBy,
  suites,
  canWrite,
  canEval,
  onChanged,
  onDeleted,
  onSuitesChanged,
  onOpenAgent,
}: {
  source: Source;
  tab: SourceTab;
  onTab: (next: SourceTab) => void;
  onBack: () => void;
  usedBy: string[];
  suites: SuiteView[];
  canWrite: boolean;
  canEval: boolean;
  onChanged: () => void;
  onDeleted: () => void;
  onSuitesChanged: () => void;
  onOpenAgent?: (name: string) => void;
}) {
  const canAddDocuments =
    canWrite &&
    (!source.lifecycle ||
      (source.lifecycle.provider === "manual" && source.lifecycle.mode === "indexed"));
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

  useEffect(() => {
    let live = true;
    api
      .documents(source.name)
      .then((d) => live && setDocs(d))
      .catch(() => live && setDocs([]));
    return () => {
      live = false;
    };
  }, [source.name, source.documents, source.excluded]);

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

  // Files are read here and sent as text, so only text files can be added.
  const upload = async (picked: FileList | null) => {
    if (!picked) return;
    setProblem(null);
    const skipped: string[] = [];
    for (const file of Array.from(picked)) {
      const text = await file.text();
      if (file.size > 900 * 1024) skipped.push(`${file.name} is larger than 900 KB`);
      else if (text.includes("\u0000")) skipped.push(`${file.name} is not a text file`);
      else {
        try {
          await api.addDocument(source.name, {
            title: file.name.replace(/\.[^.]+$/, ""),
            uri: "",
            text,
          });
        } catch (e) {
          skipped.push(`${file.name}: ${e instanceof ApiError ? e.message : "could not be added"}`);
        }
      }
    }
    onChanged();
    if (skipped.length > 0) setProblem(skipped.join(". "));
  };

  const mine = suites.filter((x) => x.target === "knowledge" && x.subject === source.name);
  return (
    <Page
      back={{ label: "All sources", onClick: onBack }}
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
          <button
            type="button"
            className={button}
            onClick={() =>
              api
                .deleteSource(source.name)
                .then(onDeleted)
                .catch(fail("The source could not be deleted."))
            }
          >
            Delete Source
          </button>
        ) : undefined
      }
      tabs={
        <Tabs
          label="Source sections"
          value={tab}
          onChange={onTab}
          tabs={[
            { id: "documents", label: "Documents", count: docs.length },
            { id: "search", label: "Try a Question" },
            { id: "variants", label: "Variants", count: source.variants.length },
            { id: "evals", label: "Evals", count: mine.length },
          ]}
        />
      }
    >
      <Problem message={problem} />
      <SourceLifecycle source={source} canWrite={canWrite} onChanged={onChanged} />

      {tab === "search" && (
        <section className={`${card} flex flex-col gap-2`}>
          <p className="text-aui-sm text-aui-muted">
            What an agent asking this would get back, best match first.
          </p>
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

      {tab === "documents" && (
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
          {docs.length === 0 ? (
            <p className="text-aui-sm text-aui-muted">Empty. Upload text files or paste text.</p>
          ) : (
            <ul>
              {docs.map((d) => (
                <li
                  key={d.id}
                  className="flex items-center gap-3 border-aui-border border-t py-2 first:border-t-0"
                >
                  <div className={`min-w-0 flex-1 ${d.excluded ? "opacity-55" : ""}`}>
                    <p className="truncate text-aui-sm text-aui-fg">{d.title}</p>
                    <p className="truncate text-aui-xs text-aui-muted">
                      {d.excluded ? "Excluded from search. " : ""}
                      {d.passages} passages, {Math.max(1, Math.round(d.bytes / 1024))} KB, added{" "}
                      {ago(d.added_at)}
                    </p>
                  </div>
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
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

      {tab === "variants" && (
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
