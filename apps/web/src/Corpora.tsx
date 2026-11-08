import { defaultCopy } from "@humagen/components";
import {
  ChevronRightIcon,
  DatabaseIcon,
  EllipsisIcon,
  EyeOffIcon,
  FileIcon,
} from "@humagen/render-web";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@humagen/render-web/ui/dropdown-menu";
import { NativeSelect, NativeSelectOption } from "@humagen/render-web/ui/native-select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@humagen/render-web/ui/sheet";
import { toast } from "@humagen/render-web/ui/sonner";
import {
  type FormEvent,
  Fragment,
  type MouseEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";
import {
  ApiError,
  api,
  type CatalogConnectorView,
  type Corpus,
  type CorpusMember,
  type CorpusMemberRef,
  type Person,
  type ProjectView,
  type Source,
} from "./api.ts";
import { BigQueryConnection } from "./BigQueryConnection.tsx";
import { ConnectionLogo } from "./ConnectionLogo.tsx";
import { Pill, Tag } from "./cards.tsx";
import { connectionApi } from "./connectionApi.ts";
import {
  holds,
  itemsOf,
  NAME,
  namedReaders,
  nameOf,
  providerOfSource,
  providersOf,
  type ReaderMode,
  readerMode,
  readersOf,
  unfiledSources,
} from "./corpora.ts";
import { ConnectSource } from "./Integrations.tsx";
import { type CorpusTab, href } from "./route.ts";
import { addTextFiles } from "./sourceUpload.ts";
import {
  button,
  Confirm,
  card,
  Empty,
  field,
  Label,
  Page,
  type Pickable,
  PickMany,
  Problem,
  primary,
  SkeletonRows,
  Tabs,
} from "./ui.tsx";

const words = defaultCopy.corpora;

/** A plain click goes there in place; a modified one is the browser's. */
const follow = (go: () => void) => (e: MouseEvent<HTMLAnchorElement>) => {
  if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  e.preventDefault();
  go();
};

const conflict = (e: unknown) => e instanceof ApiError && e.status === 409;
const message = (e: unknown) => (e instanceof ApiError ? e.message : words.failed);

const cardGrid = "grid min-w-0 grid-cols-[repeat(auto-fill,minmax(min(100%,16rem),1fr))] gap-3";
const moreButton =
  "-mr-1 inline-flex size-8 shrink-0 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:size-11";

export function providerLabel(provider: string): string {
  return words.providers[provider] ?? provider;
}

/** A provider's mark: its logo, or a neutral one where a brand has none. */
export function ProviderMark({ provider, small }: { provider?: string; small?: boolean }) {
  const size = small ? 14 : 20;
  if (!provider) return <EyeOffIcon size={size} className="text-aui-muted" />;
  if (provider === "manual") return <FileIcon size={size} className="text-aui-muted" />;
  if (provider === "s3" || provider === "gcs")
    return <DatabaseIcon size={size} className="text-aui-muted" />;
  return (
    <span
      aria-hidden="true"
      className={`inline-flex shrink-0 ${small ? "[&>*]:size-3.5 [&>span]:text-[0.5rem]" : "[&>*]:size-5"}`}
    >
      <ConnectionLogo id={provider} name={providerLabel(provider)} />
    </span>
  );
}

/** The ⋯ menu on a card. */
function More({
  label,
  items,
}: {
  label: string;
  items: { label: string; run: () => void; destructive?: boolean }[];
}) {
  // A choice runs once the menu has closed and given focus back to ⋯, so what
  // it opens (a confirm, a sheet) takes focus from there and returns it there.
  const pending = useRef<(() => void) | null>(null);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger aria-label={label} className={moreButton}>
        <EllipsisIcon size={16} />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        onCloseAutoFocus={() => {
          const run = pending.current;
          pending.current = null;
          run?.();
        }}
      >
        {items.map((item, i) => (
          <Fragment key={item.label}>
            {item.destructive && i > 0 && <DropdownMenuSeparator />}
            <DropdownMenuItem
              variant={item.destructive ? "destructive" : undefined}
              onSelect={() => {
                pending.current = item.run;
              }}
            >
              {item.label}
            </DropdownMenuItem>
          </Fragment>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** The people and projects a corpus can be shared with, and their names. */
function useDirectory() {
  const [people, setPeople] = useState<Person[]>([]);
  const [projects, setProjects] = useState<ProjectView[]>([]);
  useEffect(() => {
    let live = true;
    api.people({ limit: 50 }).then(
      (page) => live && setPeople(page.people),
      () => {},
    );
    api.projects().then(
      (all) => live && setProjects(all),
      () => {},
    );
    return () => {
      live = false;
    };
  }, []);
  const personName = (subject: string) => {
    const p = people.find((x) => x.subject === subject);
    return p ? p.name || `@${p.handle}` : subject;
  };
  const projectName = (name: string) => {
    const p = projects.find((x) => x.name === name);
    return p?.title || name;
  };
  const peopleOptions: Pickable[] = people
    .filter((p) => p.subject)
    .map((p) => ({ value: p.subject as string, label: p.name || p.handle, hint: `@${p.handle}` }));
  const projectOptions: Pickable[] = projects
    .filter((p) => p.name !== "")
    .map((p) => ({ value: p.name, label: p.title || p.name }));
  return { people, peopleOptions, projectOptions, personName, projectName };
}

interface ReadersDraft {
  mode: ReaderMode;
  people: string[];
  projects: string[];
}

const draftOf = (corpus?: Corpus): ReadersDraft => ({
  mode: readerMode(corpus?.readers ?? []),
  people: namedReaders(corpus?.readers ?? [], "person"),
  projects: namedReaders(corpus?.readers ?? [], "project"),
});

/** Who can read a corpus: its owner alone, the workspace, or people and projects. */
function ReadersField({
  value,
  onChange,
  directory,
}: {
  value: ReadersDraft;
  onChange: (next: ReadersDraft) => void;
  directory: ReturnType<typeof useDirectory>;
}) {
  const group = useId();
  const choices: [ReaderMode, string][] = [
    ["me", words.form.onlyMe],
    ["workspace", words.form.workspace],
    ["chosen", words.form.chosen],
  ];
  return (
    <fieldset className="flex min-w-0 flex-col gap-1">
      <legend className="pb-1 text-aui-xs font-medium text-aui-muted">{words.form.readers}</legend>
      {choices.map(([mode, label]) => (
        <label key={mode} className="flex min-h-8 items-center gap-2 text-aui-sm text-aui-fg">
          <input
            type="radio"
            name={group}
            checked={value.mode === mode}
            onChange={() => onChange({ ...value, mode })}
          />
          {label}
        </label>
      ))}
      {value.mode === "chosen" && (
        <div className="flex flex-col gap-3 pt-2 pl-6">
          <div className="flex flex-col gap-1">
            <span className="text-aui-xs font-medium text-aui-muted">{words.form.people}</span>
            <PickMany
              label={words.form.people}
              add={words.form.add}
              empty={words.form.none}
              options={directory.peopleOptions}
              value={value.people}
              onChange={(people) => onChange({ ...value, people })}
            />
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-aui-xs font-medium text-aui-muted">{words.form.projects}</span>
            <PickMany
              label={words.form.projects}
              add={words.form.add}
              empty={words.form.none}
              options={directory.projectOptions}
              value={value.projects}
              onChange={(projects) => onChange({ ...value, projects })}
            />
          </div>
        </div>
      )}
    </fieldset>
  );
}

/** A new corpus, or a change to one: its description and who can read it. */
function CorpusForm({
  corpus,
  onSaved,
  onCancel,
  onConflict,
}: {
  /** The corpus being changed; absent for a new one. */
  corpus?: Corpus;
  onSaved: (corpus: Corpus) => void;
  onCancel: () => void;
  onConflict: () => void;
}) {
  const directory = useDirectory();
  const [name, setName] = useState("");
  const [description, setDescription] = useState(corpus?.description ?? "");
  const [readers, setReaders] = useState(draftOf(corpus));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    const chosen = readersOf(readers.mode, readers.people, readers.projects);
    setBusy(true);
    setProblem(null);
    try {
      onSaved(
        corpus
          ? await api.updateCorpus(corpus.name, {
              description,
              readers: chosen,
              version: corpus.version,
            })
          : await api.createCorpus({ name, description, readers: chosen }),
      );
    } catch (err) {
      if (corpus && conflict(err)) onConflict();
      else setProblem(message(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form
      aria-label={corpus ? words.form.editTitle(corpus.name) : words.form.newTitle}
      className="flex min-w-0 flex-col gap-4"
      onSubmit={submit}
    >
      <Problem message={problem} />
      {!corpus && (
        <Label text={words.form.name} hint={words.form.nameHint}>
          <input
            className={field}
            required
            // biome-ignore lint/a11y/noAutofocus: the sheet opened to name a new corpus
            autoFocus
            pattern={NAME.source.slice(1, -1)}
            value={name}
            onChange={(e) => setName(nameOf(e.target.value))}
          />
        </Label>
      )}
      <Label text={words.form.description}>
        <input
          className={field}
          maxLength={500}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
      </Label>
      <ReadersField value={readers} onChange={setReaders} directory={directory} />
      <div className="flex flex-wrap gap-2">
        <button type="submit" className={primary} disabled={busy}>
          {corpus ? words.form.save : words.form.create}
        </button>
        <button type="button" className={button} onClick={onCancel}>
          {words.form.cancel}
        </button>
      </div>
    </form>
  );
}

/** The side sheet every corpus form and picker opens in. */
function SideSheet({
  open,
  title,
  description,
  wide,
  onClose,
  children,
}: {
  open: boolean;
  title: string;
  description: string;
  wide?: boolean;
  onClose: () => void;
  children: ReactNode;
}) {
  return (
    <Sheet open={open} onOpenChange={(next) => !next && onClose()}>
      <SheetContent className={wide ? "sm:max-w-xl" : undefined}>
        <SheetHeader>
          <SheetTitle>{title}</SheetTitle>
          <SheetDescription className="sr-only">{description}</SheetDescription>
        </SheetHeader>
        {open && children}
      </SheetContent>
    </Sheet>
  );
}

/**
 * Agents › Knowledge: one card per corpus the reader may read, a New Corpus
 * action, and the sources no corpus holds yet.
 */
export function KnowledgePage({
  sources,
  canWrite,
  onOpen,
}: {
  sources: Source[];
  canWrite: boolean;
  onOpen: (corpus: string) => void;
}) {
  const [corpora, setCorpora] = useState<Corpus[] | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [sheet, setSheet] = useState<
    { kind: "new" } | { kind: "edit"; name: string } | { kind: "file" } | null
  >(null);
  const [deleting, setDeleting] = useState<string | null>(null);

  const load = useCallback(() => {
    api.corpora().then(
      (list) => {
        setCorpora(list);
        setProblem(null);
      },
      () => {
        setCorpora((was) => was ?? []);
        setProblem(words.loadFailed);
      },
    );
  }, []);
  useEffect(() => load(), [load]);

  const reloadAfterConflict = () => {
    toast.error(words.changed);
    load();
  };
  const remove = async (c: Corpus) => {
    setDeleting(null);
    try {
      await api.deleteCorpus(c.name, c.version);
      toast.success(words.deleted(c.name));
    } catch (e) {
      if (conflict(e)) toast.error(words.changed);
      else toast.error(message(e));
    }
    load();
  };

  const list = corpora ?? [];
  const mine = list.filter((c) => c.mine);
  const unfiled = corpora ? unfiledSources(sources, list) : [];
  const editing = sheet?.kind === "edit" ? list.find((c) => c.name === sheet.name) : undefined;
  const newCorpus = canWrite ? (
    <button type="button" className={primary} onClick={() => setSheet({ kind: "new" })}>
      {words.newCorpus}
    </button>
  ) : null;

  return (
    <Page title={words.title} actions={list.length > 0 ? newCorpus : undefined}>
      <Problem message={problem} />
      {corpora === null ? (
        <SkeletonRows count={3} height="7rem" />
      ) : list.length === 0 ? (
        <div className="flex flex-col items-start gap-3">
          <Empty title={words.empty} />
          {newCorpus}
        </div>
      ) : (
        <ul aria-label={words.title} className={cardGrid}>
          {list.map((c) => {
            const owner = c.mine && canWrite;
            return (
              <li
                key={c.name}
                aria-label={c.name}
                className={`${card} flex h-full min-w-0 flex-col gap-2`}
              >
                <div className="flex min-w-0 items-center gap-2">
                  <h2 className="min-w-0 flex-1 truncate font-medium text-aui-fg">{c.name}</h2>
                  <Pill>{c.links === "shared" ? words.shared : words.isolated}</Pill>
                  {owner && (
                    <More
                      label={words.more(c.name)}
                      items={[
                        { label: words.edit, run: () => setSheet({ kind: "edit", name: c.name }) },
                        { label: words.delete, destructive: true, run: () => setDeleting(c.name) },
                      ]}
                    />
                  )}
                </div>
                {c.description && (
                  <p className="line-clamp-2 text-aui-sm text-aui-muted">{c.description}</p>
                )}
                <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                  {providersOf(c).map((p) => (
                    <Tag key={p} icon={<ProviderMark provider={p} small />}>
                      {providerLabel(p)}
                    </Tag>
                  ))}
                  <span className="text-aui-xs text-aui-muted tabular-nums">
                    {words.items(itemsOf(c))}
                  </span>
                </div>
                <div className="mt-auto pt-1">
                  {deleting === c.name ? (
                    <Confirm
                      question={words.deleteAsk(c.name)}
                      action={words.deleteAction}
                      onKeep={() => setDeleting(null)}
                      onConfirm={() => void remove(c)}
                    />
                  ) : (
                    <a
                      href={href({ page: "corpus", name: c.name, tab: "sources" })}
                      onClick={follow(() => onOpen(c.name))}
                      className={button}
                    >
                      {words.open}
                    </a>
                  )}
                </div>
              </li>
            );
          })}
        </ul>
      )}
      {canWrite && unfiled.length > 0 && (
        <button
          type="button"
          onClick={() => setSheet({ kind: "file" })}
          className="flex min-h-11 w-full items-center justify-between gap-3 rounded-aui px-3 text-left text-aui-sm text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
        >
          {words.unfiled(unfiled.length)}
          <ChevronRightIcon size={16} />
        </button>
      )}
      <SideSheet
        open={sheet?.kind === "new" || !!editing}
        title={editing ? words.form.editTitle(editing.name) : words.form.newTitle}
        description={words.title}
        onClose={() => setSheet(null)}
      >
        <CorpusForm
          key={editing ? `${editing.name}:${editing.version}` : "new"}
          corpus={editing}
          onCancel={() => setSheet(null)}
          onConflict={reloadAfterConflict}
          onSaved={(c) => {
            setSheet(null);
            if (editing) {
              toast.success(words.form.saved);
              load();
            } else {
              toast.success(words.form.created(c.name));
              onOpen(c.name);
            }
          }}
        />
      </SideSheet>
      <SideSheet
        open={sheet?.kind === "file"}
        title={words.file.title}
        description={words.unfiled(unfiled.length)}
        onClose={() => setSheet(null)}
      >
        <FileSources
          unfiled={unfiled}
          corpora={mine}
          onNew={() => setSheet({ kind: "new" })}
          onDone={() => {
            setSheet(null);
            load();
          }}
        />
      </SideSheet>
    </Page>
  );
}

/** Puts sources no corpus holds into one the person owns. */
function FileSources({
  unfiled,
  corpora,
  onNew,
  onDone,
}: {
  unfiled: Source[];
  corpora: Corpus[];
  onNew: () => void;
  onDone: () => void;
}) {
  const [target, setTarget] = useState(corpora[0]?.name ?? "");
  const [picked, setPicked] = useState<string[]>(unfiled.map((s) => s.name));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  if (corpora.length === 0)
    return (
      <div className="flex flex-col items-start gap-3">
        <p className="text-aui-sm text-aui-muted">{words.file.noCorpus}</p>
        <button type="button" className={primary} onClick={onNew}>
          {words.newCorpus}
        </button>
      </div>
    );
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy || !target || picked.length === 0) return;
    setBusy(true);
    setProblem(null);
    let added = 0;
    try {
      for (const name of picked) {
        await api.addCorpusMember(target, { kind: "source", name });
        added++;
      }
      toast.success(words.file.filed(added, target));
      onDone();
    } catch (err) {
      setProblem(message(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form aria-label={words.file.title} className="flex min-w-0 flex-col gap-4" onSubmit={submit}>
      <Problem message={problem} />
      <Label text={words.file.corpus}>
        <NativeSelect value={target} onChange={(e) => setTarget(e.target.value)}>
          {corpora.map((c) => (
            <NativeSelectOption key={c.name} value={c.name}>
              {c.name}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </Label>
      <fieldset className="flex min-w-0 flex-col gap-1">
        <legend className="pb-1 text-aui-xs font-medium text-aui-muted">
          {words.file.sources}
        </legend>
        {unfiled.map((s) => (
          <label key={s.name} className="flex min-h-8 items-center gap-2 text-aui-sm text-aui-fg">
            <input
              type="checkbox"
              checked={picked.includes(s.name)}
              onChange={(e) =>
                setPicked((was) =>
                  e.target.checked ? [...was, s.name] : was.filter((n) => n !== s.name),
                )
              }
            />
            <ProviderMark provider={providerOfSource(s)} small />
            <span className="min-w-0 truncate">{s.name}</span>
          </label>
        ))}
      </fieldset>
      <div>
        <button type="submit" className={primary} disabled={busy || !target || picked.length === 0}>
          {words.file.action}
        </button>
      </div>
    </form>
  );
}

/**
 * One corpus: the sources it holds and who may read it, each a tab with its
 * own address. Only its owner adds and removes sources, changes its readers
 * and hands it off; anyone else who may read it sees the same page read-only.
 */
export function CorpusPage({
  name,
  tab,
  sources,
  canWrite,
  onTab,
  onBack,
  onOpenSource,
  onMissing,
  onSourcesChanged,
}: {
  name: string;
  tab: CorpusTab;
  sources: Source[];
  canWrite: boolean;
  onTab: (tab: CorpusTab) => void;
  onBack: () => void;
  onOpenSource: (source: string) => void;
  /** An old `/knowledge/{source}` link named a source, not a corpus: open the source instead. */
  onMissing?: (source: string) => void;
  onSourcesChanged?: () => void;
}) {
  // undefined while loading, null when there is no such corpus for this reader.
  const [corpus, setCorpus] = useState<Corpus | null | undefined>(undefined);
  const [problem, setProblem] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);

  const load = useCallback(() => {
    api.corpus(name).then(
      (c) => {
        setCorpus(c);
        setProblem(null);
      },
      (e) => {
        if (e instanceof ApiError && e.status === 404) setCorpus(null);
        else {
          setCorpus((was) => was ?? null);
          setProblem(words.loadFailed);
        }
      },
    );
  }, [name]);
  useEffect(() => load(), [load]);
  useEffect(() => {
    if (corpus === null && onMissing && sources.some((s) => s.name === name)) onMissing(name);
  }, [corpus, sources, name, onMissing]);

  const reloadAfterConflict = () => {
    toast.error(words.changed);
    load();
  };

  if (corpus === undefined)
    return (
      <Page title={name} back={{ label: words.back, onClick: onBack }}>
        <SkeletonRows count={3} height="5rem" />
      </Page>
    );
  if (corpus === null)
    return (
      <Page title={words.notFound(name)} back={{ label: words.back, onClick: onBack }}>
        <Problem message={problem} />
      </Page>
    );

  const owner = corpus.mine && canWrite;
  const addSource = owner ? (
    <button type="button" className={primary} onClick={() => setAdding(true)}>
      {words.addSource}
    </button>
  ) : null;

  const remove = async (m: CorpusMember) => {
    const ref: CorpusMemberRef = { kind: m.kind, name: m.name };
    try {
      setCorpus(await api.removeCorpusMember(corpus.name, ref));
      toast.success(words.removed(m.name), {
        action: {
          label: words.undo,
          onClick: () =>
            void api
              .addCorpusMember(corpus.name, ref)
              .then(setCorpus, (e) => toast.error(message(e))),
        },
      });
    } catch (e) {
      toast.error(message(e));
    }
  };

  return (
    <Page
      title={corpus.name}
      back={{ label: words.back, onClick: onBack }}
      meta={
        <>
          <span className="font-medium text-aui-fg">{corpus.name}</span>
          {corpus.description && <span>{corpus.description}</span>}
        </>
      }
      actions={tab === "sources" && corpus.members.length > 0 ? addSource : undefined}
      tabs={
        <Tabs
          label={corpus.name}
          value={tab}
          onChange={onTab}
          tabs={[
            { id: "sources", label: words.tabs.sources, count: corpus.members.length },
            { id: "access", label: words.tabs.access },
          ]}
        />
      }
    >
      <Problem message={problem} />
      {tab === "sources" &&
        (corpus.members.length === 0 ? (
          <div className="flex flex-col items-start gap-3">
            <Empty title={words.noSources} />
            {addSource}
          </div>
        ) : (
          <ul aria-label={words.tabs.sources} className={cardGrid}>
            {corpus.members.map((m, i) => (
              <MemberCard
                // A hidden member has no name; its place in the list tells it apart.
                key={m.status === "hidden" ? `hidden:${i}` : `${m.kind}:${m.name}`}
                corpus={corpus.name}
                member={m}
                owner={owner}
                onOpen={() => onOpenSource(m.name)}
                onRemove={() => void remove(m)}
              />
            ))}
          </ul>
        ))}
      {tab === "access" && (
        <Access
          key={corpus.version}
          corpus={corpus}
          owner={owner}
          onChanged={setCorpus}
          onConflict={reloadAfterConflict}
        />
      )}
      {owner && (
        <AddSource
          open={adding}
          corpus={corpus}
          sources={sources}
          onClose={() => setAdding(false)}
          onAdded={(next, done) => {
            setCorpus(next);
            if (done) setAdding(false);
          }}
          onSourcesChanged={onSourcesChanged}
        />
      )}
    </Page>
  );
}

/** One source or warehouse a corpus holds, as the reader is allowed to see it. */
function MemberCard({
  corpus,
  member,
  owner,
  onOpen,
  onRemove,
}: {
  corpus: string;
  member: CorpusMember;
  owner: boolean;
  onOpen: () => void;
  onRemove: () => void;
}) {
  const hidden = member.status === "hidden";
  const title = hidden ? words.hidden : member.name;
  const facts = [
    member.holds ? words.holds[member.holds] : undefined,
    member.status === "missing" ? undefined : words.items(member.items),
  ].filter(Boolean);
  return (
    <li aria-label={title} className={`${card} flex h-full min-w-0 flex-col gap-2`}>
      <div className="flex min-w-0 items-center gap-2">
        <ProviderMark provider={hidden ? undefined : (member.provider ?? "manual")} />
        <h3
          className={`min-w-0 flex-1 truncate font-medium ${hidden ? "text-aui-muted" : "text-aui-fg"}`}
        >
          {title}
        </h3>
        {!hidden && (
          <Pill tone={member.status === "failed" ? "bad" : "neutral"}>
            {words.status[member.status]}
          </Pill>
        )}
        {owner && !hidden && (
          <More
            label={words.more(member.name)}
            items={[{ label: words.remove, destructive: true, run: onRemove }]}
          />
        )}
      </div>
      {!hidden && facts.length > 0 && (
        <p className="text-aui-xs text-aui-muted tabular-nums">{facts.join(" | ")}</p>
      )}
      {member.kind === "source" && !hidden && member.status !== "missing" && (
        <div className="mt-auto pt-1">
          <a
            href={href({ page: "context", source: member.name, corpus })}
            onClick={follow(onOpen)}
            className={button}
            aria-label={`${words.open} ${member.name}`}
          >
            {words.open}
          </a>
        </div>
      )}
    </li>
  );
}

type NewSource = "manual" | "drive" | "notion" | "s3" | "gcs" | "bigquery" | "snowflake";
const NEW_SOURCES: NewSource[] = [
  "manual",
  "drive",
  "notion",
  "s3",
  "gcs",
  "bigquery",
  "snowflake",
];
/** Its setup lands in a later backend layer. */
const SOON: NewSource[] = ["snowflake"];

/**
 * Add Source: a source or warehouse the owner can already see, or a new one
 * made here and added once it exists.
 */
function AddSource({
  open,
  corpus,
  sources,
  onClose,
  onAdded,
  onSourcesChanged,
}: {
  open: boolean;
  corpus: Corpus;
  sources: Source[];
  onClose: () => void;
  /** The corpus after an add; `done` closes the sheet. */
  onAdded: (corpus: Corpus, done: boolean) => void;
  onSourcesChanged?: () => void;
}) {
  const [step, setStep] = useState<NewSource | null>(null);
  const [connectors, setConnectors] = useState<CatalogConnectorView[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      setStep(null);
      setProblem(null);
      return;
    }
    let live = true;
    api.catalog().then(
      (page) => live && setConnectors(page.connectors ?? []),
      () => {},
    );
    return () => {
      live = false;
    };
  }, [open]);

  const add = async (ref: CorpusMemberRef, done: boolean) => {
    setBusy(`${ref.kind}:${ref.name}`);
    setProblem(null);
    try {
      onAdded(await api.addCorpusMember(corpus.name, ref), done);
      toast.success(words.added(ref.name));
    } catch (e) {
      setProblem(message(e));
    } finally {
      setBusy(null);
    }
  };
  const created = (ref: CorpusMemberRef, done = true) => {
    onSourcesChanged?.();
    void add(ref, done);
  };

  const existing = [
    ...sources.map((s) => ({
      kind: "source" as const,
      name: s.name,
      provider: providerOfSource(s),
    })),
    ...connectors.map((c) => ({ kind: "catalog" as const, name: c.name, provider: c.system })),
  ].filter((m) => !holds(corpus, m));

  const back = (
    <button
      type="button"
      className="inline-flex min-h-6 items-center self-start rounded-aui text-aui-xs text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
      onClick={() => {
        setStep(null);
        setProblem(null);
      }}
    >
      ‹ {words.add.back}
    </button>
  );

  return (
    <SideSheet
      open={open}
      wide
      title={step ? (step === "manual" ? words.add.upload : providerLabel(step)) : words.add.title}
      description={corpus.name}
      onClose={onClose}
    >
      <div className="flex min-w-0 flex-col gap-5">
        <Problem message={problem} />
        {step === null && (
          <>
            {existing.length > 0 && (
              <section aria-label={words.add.existing} className="flex flex-col gap-1">
                <h3 className="text-aui-xs font-medium text-aui-muted">{words.add.existing}</h3>
                <ul className="flex flex-col">
                  {existing.map((m) => (
                    <li
                      key={`${m.kind}:${m.name}`}
                      className="flex min-w-0 items-center gap-3 border-aui-border border-t py-2 first:border-t-0"
                    >
                      <ProviderMark provider={m.provider} />
                      <span className="min-w-0 flex-1 truncate text-aui-sm text-aui-fg">
                        {m.name}
                      </span>
                      <button
                        type="button"
                        className={button}
                        aria-label={words.add.addTo(m.name)}
                        disabled={busy !== null}
                        onClick={() => void add({ kind: m.kind, name: m.name }, false)}
                      >
                        {words.add.addOne}
                      </button>
                    </li>
                  ))}
                </ul>
              </section>
            )}
            <section aria-label={words.add.new} className="flex flex-col gap-1">
              <h3 className="text-aui-xs font-medium text-aui-muted">{words.add.new}</h3>
              <ul className="flex flex-col gap-2">
                {NEW_SOURCES.map((p) => {
                  const soon = SOON.includes(p);
                  return (
                    <li key={p}>
                      <button
                        type="button"
                        disabled={soon}
                        onClick={() => setStep(p)}
                        className="flex min-h-11 w-full items-center gap-3 rounded-aui border border-aui-border px-3 text-left text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent disabled:cursor-not-allowed disabled:text-aui-muted disabled:hover:bg-transparent"
                      >
                        <ProviderMark provider={p} />
                        <span className="min-w-0 flex-1 truncate">
                          {p === "manual" ? words.add.upload : providerLabel(p)}
                        </span>
                        {soon ? (
                          <Pill>{words.add.comingSoon}</Pill>
                        ) : (
                          <ChevronRightIcon size={16} className="text-aui-muted" />
                        )}
                      </button>
                    </li>
                  );
                })}
              </ul>
            </section>
          </>
        )}
        {step === "manual" && (
          <>
            {back}
            <UploadFiles
              corpus={corpus.name}
              onCreated={(source) => created({ kind: "source", name: source })}
            />
          </>
        )}
        {(step === "drive" || step === "notion" || step === "s3" || step === "gcs") && (
          <>
            {back}
            <ConnectSource
              key={step}
              initialProvider={step}
              onCancel={() => setStep(null)}
              onCreated={(source) => created({ kind: "source", name: source })}
            />
          </>
        )}
        {step === "bigquery" && (
          <BigQueryConnection
            canWrite
            onCancel={() => setStep(null)}
            // Added once saved; the sheet stays open to verify and publish it.
            onSaved={(connector) => created({ kind: "catalog", name: connector }, false)}
            onAuthorize={() =>
              void connectionApi
                .authorize("bigquery", { scope: "person" })
                .then((auth) => {
                  sessionStorage.setItem(
                    "humagen.connection.provider",
                    auth.provider ?? "bigquery",
                  );
                  window.location.assign(auth.url);
                })
                .catch((e) => setProblem(message(e)))
            }
          />
        )}
      </div>
    </SideSheet>
  );
}

/** A new source of uploaded text files, made inside the corpus. */
function UploadFiles({
  corpus,
  onCreated,
}: {
  corpus: string;
  onCreated: (source: string) => void;
}) {
  const [name, setName] = useState(nameOf(words.add.uploadName(corpus)));
  const [description, setDescription] = useState("");
  const [files, setFiles] = useState<File[]>([]);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy || files.length === 0) return;
    setBusy(true);
    setProblem(null);
    try {
      await api.createSource(name, description);
      const skipped = await addTextFiles(name, files);
      if (skipped.length > 0) toast.error(words.add.skipped(skipped.join(". ")));
      onCreated(name);
    } catch (err) {
      setProblem(message(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form aria-label={words.add.upload} className="flex min-w-0 flex-col gap-4" onSubmit={submit}>
      <Problem message={problem} />
      <Label text={words.form.name} hint={words.form.nameHint}>
        <input
          className={field}
          required
          pattern={NAME.source.slice(1, -1)}
          value={name}
          onChange={(e) => setName(nameOf(e.target.value))}
        />
      </Label>
      <Label text={words.form.description}>
        <input
          className={field}
          maxLength={500}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
      </Label>
      <Label text={words.add.files}>
        <input
          type="file"
          multiple
          required
          className="text-aui-sm"
          onChange={(e) => setFiles(Array.from(e.target.files ?? []))}
        />
      </Label>
      <div>
        <button type="submit" className={primary} disabled={busy || files.length === 0}>
          {words.add.uploadAction}
        </button>
      </div>
    </form>
  );
}

/** Who owns a corpus and who may read it; its owner changes both. */
function Access({
  corpus,
  owner,
  onChanged,
  onConflict,
}: {
  corpus: Corpus;
  owner: boolean;
  onChanged: (corpus: Corpus) => void;
  onConflict: () => void;
}) {
  const directory = useDirectory();
  const [readers, setReaders] = useState(draftOf(corpus));
  const [handTo, setHandTo] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const initial = JSON.stringify(draftOf(corpus));
  const dirty = JSON.stringify(readers) !== initial;

  const change = async (
    body: Parameters<typeof api.updateCorpus>[1],
    done: (next: Corpus) => void,
  ) => {
    setBusy(true);
    setProblem(null);
    try {
      const next = await api.updateCorpus(corpus.name, { ...body, version: corpus.version });
      onChanged(next);
      done(next);
    } catch (e) {
      if (conflict(e)) onConflict();
      else setProblem(message(e));
    } finally {
      setBusy(false);
    }
  };

  const ownerName = corpus.mine ? words.access.you : directory.personName(corpus.owner);
  const readerRows = corpus.readers.map((r) => ({
    key: `${r.kind}:${r.name ?? ""}`,
    label:
      r.kind === "workspace"
        ? words.form.workspace
        : r.kind === "project"
          ? directory.projectName(r.name ?? "")
          : directory.personName(r.name ?? ""),
  }));
  const others = directory.peopleOptions.filter((p) => p.value !== corpus.owner);

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <Problem message={problem} />
      <section aria-label={words.access.owner} className={`${card} flex flex-col gap-3`}>
        <h2 className="text-aui-xs font-medium text-aui-muted">{words.access.owner}</h2>
        <p className="text-aui-sm text-aui-fg">{ownerName}</p>
        {owner && others.length > 0 && (
          <form
            className="flex flex-wrap items-end gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (!handTo) return;
              void change({ owner: handTo }, () =>
                toast.success(words.access.handedOff(directory.personName(handTo))),
              );
            }}
          >
            <div className="min-w-48 max-w-xs flex-1">
              <Label text={words.access.handOffTo}>
                <NativeSelect value={handTo} onChange={(e) => setHandTo(e.target.value)}>
                  <NativeSelectOption value="" />
                  {others.map((p) => (
                    <NativeSelectOption key={p.value} value={p.value}>
                      {p.label}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
              </Label>
            </div>
            <button type="submit" className={button} disabled={busy || !handTo}>
              {words.access.handOff}
            </button>
          </form>
        )}
      </section>
      <section aria-label={words.access.readers} className={`${card} flex flex-col gap-3`}>
        {owner ? (
          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              void change(
                { readers: readersOf(readers.mode, readers.people, readers.projects) },
                () => toast.success(words.form.saved),
              );
            }}
          >
            <ReadersField value={readers} onChange={setReaders} directory={directory} />
            <div>
              <button type="submit" className={primary} disabled={busy || !dirty}>
                {words.form.save}
              </button>
            </div>
          </form>
        ) : (
          <>
            <h2 className="text-aui-xs font-medium text-aui-muted">{words.access.readers}</h2>
            <ul className="flex flex-col gap-1">
              {(readerRows.length > 0
                ? readerRows
                : [{ key: "owner", label: words.access.onlyOwner }]
              ).map((r) => (
                <li key={r.key} className="text-aui-sm text-aui-fg">
                  {r.label}
                </li>
              ))}
            </ul>
          </>
        )}
      </section>
    </div>
  );
}
