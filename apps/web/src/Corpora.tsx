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
  type CorpusHit,
  type CorpusMember,
  type CorpusMemberRef,
  type CorpusSearch,
  type GraphNodeView,
  type GraphView,
  type OwnerAction,
  type Person,
  type ProjectView,
  type Source,
} from "./api.ts";
import { BigQueryConnection } from "./BigQueryConnection.tsx";
import { ConnectionLogo } from "./ConnectionLogo.tsx";
import { Pill, Tag } from "./cards.tsx";
import { connectionApi } from "./connectionApi.ts";
import {
  type CorpusLabel,
  holds,
  isDocumentNode,
  itemsOf,
  LABELS,
  memberOfNode,
  namedReaders,
  nameFromTitle,
  nameOf,
  type OntologyType,
  ontologyTypes,
  ownerChoices,
  providerOfSource,
  providersOf,
  type ReaderMode,
  readerMode,
  readersOf,
  safeLink,
  titleOf,
  unfiledSources,
} from "./corpora.ts";
import { ConnectSource } from "./Integrations.tsx";
import { type CorpusTab, href } from "./route.ts";
import { addTextFiles } from "./sourceUpload.ts";
import {
  AgentMark,
  ago,
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
  rows,
  SkeletonRows,
  Switch,
  Tabs,
} from "./ui.tsx";

const words = defaultCopy.corpora;
/** A corpus or source name (corpora.ts NAME) as an input's pattern, which is read with the `v` flag: a literal dash is escaped. */
const NAME_PATTERN = "[a-z0-9][a-z0-9\\-]{0,62}";

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
            className="accent-aui-fg"
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
  const [title, setTitle] = useState(corpus ? titleOf(corpus) : "");
  // A title with no letter or digit a name can hold still makes a corpus.
  const [spare] = useState(() => `corpus-${Math.random().toString(36).slice(2, 8)}`);
  const name = corpus?.name ?? (nameFromTitle(title) || spare);
  const [description, setDescription] = useState(corpus?.description ?? "");
  const [readers, setReaders] = useState(draftOf(corpus));
  const [label, setLabel] = useState<CorpusLabel>(corpus?.label ?? "internal");
  const [purpose, setPurpose] = useState(corpus?.purpose ?? "");
  const [graphSearch, setGraphSearch] = useState(corpus?.graph_search ?? false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    const chosen = readersOf(readers.mode, readers.people, readers.projects);
    const sensitivity = {
      label,
      ...(label === "restricted" ? { purpose: purpose.trim() } : {}),
    };
    setBusy(true);
    setProblem(null);
    try {
      onSaved(
        corpus
          ? await api.updateCorpus(corpus.name, {
              title: title.trim(),
              description,
              readers: chosen,
              ...sensitivity,
              graph_search: graphSearch,
              version: corpus.version,
            })
          : await api.createCorpus({
              name,
              title: title.trim(),
              description,
              readers: chosen,
              ...sensitivity,
            }),
      );
    } catch (err) {
      if (corpus && conflict(err)) onConflict();
      else setProblem(refusal(err, label));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form
      aria-label={corpus ? words.form.editTitle(titleOf(corpus)) : words.form.newTitle}
      className="flex min-w-0 flex-col gap-4"
      onSubmit={submit}
    >
      <Problem message={problem} />
      {/* Names never change: a new corpus takes its name from its title. */}
      <Label
        text={words.form.title}
        hint={corpus || !title.trim() ? undefined : words.form.nameIs(name)}
      >
        <input
          className={field}
          required
          // biome-ignore lint/a11y/noAutofocus: the sheet opened to name the corpus
          autoFocus
          maxLength={120}
          value={title}
          onChange={(e) => setTitle(e.target.value)}
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
      <ReadersField value={readers} onChange={setReaders} directory={directory} />
      <LabelField label={label} purpose={purpose} onLabel={setLabel} onPurpose={setPurpose} />
      {corpus && (
        <Switch
          label={words.labels.graphSearch}
          hint={words.labels.graphSearchHint}
          checked={graphSearch}
          onChange={setGraphSearch}
        />
      )}
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

/** A refusal in words: restricting needs a workspace admin, which the server says as a bare 403. */
function refusal(e: unknown, label?: string): string {
  if (e instanceof ApiError && e.status === 403 && label === "restricted")
    return words.labels.adminNeeded;
  return message(e);
}

/** How sensitive a corpus is, with one line on what that means, and why when it is restricted. */
function LabelField({
  label,
  purpose,
  onLabel,
  onPurpose,
}: {
  label: CorpusLabel;
  purpose: string;
  onLabel: (label: CorpusLabel) => void;
  onPurpose: (purpose: string) => void;
}) {
  return (
    <>
      <Label text={words.labels.title} hint={words.labels.meaning}>
        <NativeSelect value={label} onChange={(e) => onLabel(e.target.value as CorpusLabel)}>
          {LABELS.map((l) => (
            <NativeSelectOption key={l} value={l}>
              {words.labels[l]}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </Label>
      {label === "restricted" && (
        <Label text={words.labels.purpose} hint={words.labels.purposeHint}>
          <input
            className={field}
            required
            maxLength={500}
            value={purpose}
            onChange={(e) => onPurpose(e.target.value)}
          />
        </Label>
      )}
    </>
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
      toast.success(words.deleted(titleOf(c)));
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
            const title = titleOf(c);
            return (
              <li
                key={c.name}
                aria-label={title}
                className={`${card} flex h-full min-w-0 flex-col gap-2`}
              >
                <div className="flex min-w-0 items-center gap-2">
                  <h2 className="min-w-0 flex-1 truncate font-medium text-aui-fg">{title}</h2>
                  <Pill>{words.labels[c.label ?? "internal"]}</Pill>
                  {owner && (
                    <More
                      label={words.more(title)}
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
                      question={words.deleteAsk(title)}
                      action={words.deleteAction}
                      onKeep={() => setDeleting(null)}
                      onConfirm={() => void remove(c)}
                    />
                  ) : (
                    <a
                      href={href({ page: "corpus", name: c.name, tab: "sources" })}
                      onClick={follow(() => onOpen(c.name))}
                      className={button}
                      aria-label={`${words.open} ${title}`}
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
        title={editing ? words.form.editTitle(titleOf(editing)) : words.form.newTitle}
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
              toast.success(words.form.created(titleOf(c)));
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
      const into = corpora.find((c) => c.name === target);
      toast.success(words.file.filed(added, into ? titleOf(into) : target));
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
              {titleOf(c)}
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
              className="accent-aui-fg"
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

/** Starts the person's own BigQuery connection: Google sign-in, then back to the console. */
function startBigQueryLogin(onError: (message: string) => void) {
  connectionApi
    .authorize("bigquery", { scope: "person" })
    .then((auth) => {
      sessionStorage.setItem("humagen.connection.provider", auth.provider ?? "bigquery");
      window.location.assign(auth.url);
    })
    .catch((e) => onError(message(e)));
}

/** Each warehouse the reader's own login reaches, by connector name, from GET /api/catalog. */
function useConnectors(names: string[]) {
  const [connectors, setConnectors] = useState<Record<string, CatalogConnectorView>>({});
  const key = names.join("\n");
  const load = useCallback(() => {
    if (!key) return;
    api.catalog().then(
      (page) => setConnectors(Object.fromEntries((page.connectors ?? []).map((c) => [c.name, c]))),
      () => {},
    );
  }, [key]);
  useEffect(() => load(), [load]);
  return { connectors, reload: load };
}

/**
 * One corpus: the sources it holds, a search to try and who may read it,
 * each a tab with its own address. Only its owner adds and removes sources,
 * changes its readers and agents and hands it off; anyone else who may read
 * it sees the same page read-only, with their own warehouse logins.
 */
export function CorpusPage({
  name,
  tab,
  q,
  sources,
  agents = [],
  viewer = "",
  canWrite,
  onTab,
  onSearch,
  onBack,
  onOpenSource,
  onMissing,
  onSourcesChanged,
}: {
  name: string;
  tab: CorpusTab;
  /** The words of the search on the Search tab. */
  q?: string;
  sources: Source[];
  /** The agents the reader can see, to grant it to. */
  agents?: string[];
  /** The reader's subject, to tell the documents they own. */
  viewer?: string;
  canWrite: boolean;
  onTab: (tab: CorpusTab) => void;
  onSearch?: (q: string) => void;
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
  const [loginFor, setLoginFor] = useState<string | null>(null);
  const directory = useDirectory();
  const warehouses = (corpus?.members ?? [])
    .filter((m) => m.kind === "catalog" && m.name && m.status !== "hidden")
    .map((m) => m.name);
  const { connectors, reload: reloadConnectors } = useConnectors(warehouses);

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
  const title = titleOf(corpus);
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
  const forget = async (connector: string) => {
    try {
      await api.deleteCatalogLogin(connector);
      toast.success(words.login.forgotten);
      reloadConnectors();
    } catch (e) {
      toast.error(message(e));
    }
  };

  return (
    <Page
      title={title}
      back={{ label: words.back, onClick: onBack }}
      meta={
        <>
          <span className="font-medium text-aui-fg">{title}</span>
          {corpus.description && <span>{corpus.description}</span>}
        </>
      }
      actions={tab === "sources" && corpus.members.length > 0 ? addSource : undefined}
      tabs={
        <Tabs
          label={title}
          value={tab}
          onChange={onTab}
          tabs={[
            { id: "sources", label: words.tabs.sources, count: corpus.members.length },
            { id: "search", label: words.tabs.search },
            { id: "graph", label: words.tabs.graph },
            { id: "types", label: words.tabs.types },
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
                // A member hidden from a reader has no name; its place in the list tells it apart.
                key={m.name ? `${m.kind}:${m.name}` : `hidden:${i}`}
                corpus={corpus.name}
                member={m}
                mine={corpus.mine}
                owner={owner}
                connector={m.kind === "catalog" ? connectors[m.name] : undefined}
                onOpen={() => onOpenSource(m.name)}
                onRemove={() => void remove(m)}
                onConnect={() =>
                  (connectors[m.name]?.system ?? m.provider) === "bigquery"
                    ? startBigQueryLogin((why) => toast.error(why))
                    : setLoginFor(m.name)
                }
                onForget={() => void forget(m.name)}
              />
            ))}
          </ul>
        ))}
      {tab === "search" && (
        <SearchTab key={corpus.name} corpus={corpus} q={q} onSearch={onSearch} />
      )}
      {tab === "graph" && (
        <GraphTab
          key={corpus.name}
          corpus={corpus}
          owner={owner}
          viewer={viewer}
          directory={directory}
        />
      )}
      {tab === "types" && (
        <TypesTab
          key={corpus.version}
          corpus={corpus}
          owner={owner}
          onChanged={setCorpus}
          onConflict={reloadAfterConflict}
        />
      )}
      {tab === "access" && (
        <Access
          key={corpus.version}
          corpus={corpus}
          owner={owner}
          agents={agents}
          directory={directory}
          onChanged={setCorpus}
          onConflict={reloadAfterConflict}
        />
      )}
      {owner && (
        <AddSource
          open={adding}
          corpus={corpus}
          sources={sources}
          onClose={() => {
            setAdding(false);
            reloadConnectors();
          }}
          onAdded={(next, done) => {
            setCorpus(next);
            if (done) setAdding(false);
          }}
          onSourcesChanged={onSourcesChanged}
        />
      )}
      <SideSheet
        open={loginFor !== null}
        title={words.login.title(loginFor ?? "")}
        description={title}
        onClose={() => setLoginFor(null)}
      >
        {loginFor && (
          <LoginForm
            connector={loginFor}
            onSaved={() => {
              setLoginFor(null);
              reloadConnectors();
            }}
          />
        )}
      </SideSheet>
    </Page>
  );
}

/** One source or warehouse a corpus holds, as the reader is allowed to see it. */
function MemberCard({
  corpus,
  member,
  mine,
  owner,
  connector,
  onOpen,
  onRemove,
  onConnect,
  onForget,
}: {
  corpus: string;
  member: CorpusMember;
  /** The reader owns the corpus, so a member hidden from them still shows its name. */
  mine: boolean;
  /** The reader owns the corpus and may change it. */
  owner: boolean;
  /** The warehouse a catalog member is, with the reader's own login to it. */
  connector?: CatalogConnectorView;
  onOpen: () => void;
  onRemove: () => void;
  onConnect: () => void;
  onForget: () => void;
}) {
  const hidden = member.status === "hidden";
  // Only the owner is told the name of a member hidden from them, so they can remove it.
  const named = !hidden || (mine && !!member.name);
  const title = named ? member.name : words.hidden;
  const login = !hidden && member.kind === "catalog" ? connector?.login : undefined;
  const system = connector?.system ?? member.provider;
  const menu = [
    ...(login === "connected" && system !== "bigquery"
      ? [{ label: words.login.forget, run: onForget }]
      : []),
    ...(owner && named ? [{ label: words.remove, destructive: true, run: onRemove }] : []),
  ];
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
        {menu.length > 0 && <More label={words.more(title)} items={menu} />}
      </div>
      {hidden && named && <p className="text-aui-xs text-aui-muted">{words.hiddenToOwner}</p>}
      {!hidden && facts.length > 0 && (
        <p className="text-aui-xs text-aui-muted tabular-nums">{facts.join(" | ")}</p>
      )}
      {(login === "connected" || login === "unavailable") && (
        <p className="text-aui-xs text-aui-muted">
          {login === "connected" ? words.login.connected : words.login.unavailable}
        </p>
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
      {login === "missing" && (
        <div className="mt-auto pt-1">
          <button type="button" className={button} onClick={onConnect}>
            {words.login.connect}
          </button>
        </div>
      )}
    </li>
  );
}

/** A failure cause in words, where the console has words for it. */
function causeOf(cause: string): string {
  const known = (defaultCopy.sourceLifecycle.cause as Record<string, string>)[cause];
  return known ? known.charAt(0).toLowerCase() + known.slice(1) : cause;
}

/** Try a search: what an agent granted the corpus would find, with the reader's own access. */
function SearchTab({
  corpus,
  q,
  onSearch,
}: {
  corpus: Corpus;
  q?: string;
  onSearch?: (q: string) => void;
}) {
  const [text, setText] = useState(q ?? "");
  const [state, setState] = useState<
    { busy: true } | { busy: false; result?: CorpusSearch; failed?: string } | null
  >(null);
  const last = useRef<string | null>(null);
  const run = useCallback(
    (query: string) => {
      last.current = query;
      setState({ busy: true });
      api.searchCorpus(corpus.name, query).then(
        (result) => last.current === query && setState({ busy: false, result }),
        (e) =>
          last.current === query &&
          setState({
            busy: false,
            failed: e instanceof ApiError && e.status < 500 ? e.message : words.search.failed,
          }),
      );
    },
    [corpus.name],
  );
  // A search at its address (a reload, a copied link) runs once.
  useEffect(() => {
    if (q && q !== last.current) {
      setText(q);
      run(q);
    }
  }, [q, run]);

  const providerOf = (ref: CorpusMemberRef) =>
    corpus.members.find((m) => m.kind === ref.kind && m.name === ref.name)?.provider;
  const result = state && !state.busy ? state.result : undefined;
  return (
    <div className="flex min-w-0 flex-col gap-4">
      <form
        aria-label={words.search.label}
        className="flex min-w-0 gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          const query = text.trim();
          if (!query) return;
          run(query);
          onSearch?.(query);
        }}
      >
        <input
          type="search"
          aria-label={words.search.label}
          className={field}
          maxLength={1000}
          required
          value={text}
          onChange={(e) => setText(e.target.value)}
        />
        <button type="submit" className={button} disabled={state?.busy}>
          {words.search.action}
        </button>
      </form>
      {state?.busy && (
        <output className="text-aui-sm text-aui-muted">{words.search.searching}</output>
      )}
      {state && !state.busy && state.failed && <Problem message={state.failed} />}
      {result && (
        <>
          {result.warnings.map((w) => (
            <p
              key={w.member ? `${w.member.kind}:${w.member.name}` : "graph"}
              className="text-aui-xs text-aui-muted"
            >
              {words.search.warning(
                w.member ? w.member.name : words.search.graph,
                causeOf(w.cause),
              )}
            </p>
          ))}
          {result.hits.length === 0 ? (
            <p className="text-aui-sm text-aui-muted">{words.search.none}</p>
          ) : (
            <ol aria-label={words.search.label} className={rows}>
              {result.hits.map((hit) => (
                <SearchHit
                  key={`${hit.member.kind}:${hit.member.name}:${hit.document_id ?? hit.title}:${hit.index ?? 0}`}
                  hit={hit}
                  providerOf={providerOf}
                />
              ))}
            </ol>
          )}
        </>
      )}
    </div>
  );
}

function SearchHit({
  hit,
  providerOf,
}: {
  hit: CorpusHit;
  providerOf: (ref: CorpusMemberRef) => string | undefined;
}) {
  const link = safeLink(hit.uri);
  return (
    <li aria-label={hit.title} className="flex min-w-0 flex-col gap-1.5 py-3">
      {link ? (
        <a
          href={link}
          target="_blank"
          rel="noreferrer"
          className="min-w-0 truncate font-medium text-aui-sm text-aui-fg underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
        >
          {hit.title}
        </a>
      ) : (
        <span className="min-w-0 truncate font-medium text-aui-sm text-aui-fg">{hit.title}</span>
      )}
      <div className="flex min-w-0 flex-wrap items-center gap-1.5">
        <Tag icon={<ProviderMark provider={providerOf(hit.member) ?? "manual"} small />}>
          {hit.member.name}
        </Tag>
        {(hit.also ?? []).map((m) => (
          <Tag
            key={`${m.kind}:${m.name}`}
            icon={<ProviderMark provider={providerOf(m) ?? "manual"} small />}
          >
            {m.name}
          </Tag>
        ))}
        <span className="text-aui-xs text-aui-muted">{words.holds[hit.holds]}</span>
      </div>
      <p className="line-clamp-3 text-aui-sm text-aui-muted">{hit.text}</p>
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

/**
 * Add Source: a source or warehouse the owner can already see, or a new one
 * made here and added once it exists. A new Snowflake warehouse then asks for
 * the owner's own login, which every query of theirs runs on.
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
  const [step, setStep] = useState<NewSource | { login: string } | null>(null);
  const [connectors, setConnectors] = useState<CatalogConnectorView[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  const loadConnectors = useCallback(() => {
    api.catalog().then(
      (page) => setConnectors(page.connectors ?? []),
      () => {},
    );
  }, []);
  useEffect(() => {
    if (!open) {
      setStep(null);
      setProblem(null);
      return;
    }
    loadConnectors();
  }, [open, loadConnectors]);

  const add = async (ref: CorpusMemberRef, done: boolean) => {
    setBusy(`${ref.kind}:${ref.name}`);
    setProblem(null);
    try {
      onAdded(await api.addCorpusMember(corpus.name, ref), done);
      toast.success(words.added(ref.name));
      return true;
    } catch (e) {
      setProblem(message(e));
      return false;
    } finally {
      setBusy(null);
    }
  };
  const created = (ref: CorpusMemberRef, done = true) => {
    onSourcesChanged?.();
    return add(ref, done);
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
  const login = step && typeof step === "object" ? step.login : null;

  return (
    <SideSheet
      open={open}
      wide
      title={
        login
          ? words.login.title(login)
          : typeof step === "string"
            ? step === "manual"
              ? words.add.upload
              : providerLabel(step)
            : words.add.title
      }
      description={titleOf(corpus)}
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
                {NEW_SOURCES.map((p) => (
                  <li key={p}>
                    <button
                      type="button"
                      onClick={() => setStep(p)}
                      className="flex min-h-11 w-full items-center gap-3 rounded-aui border border-aui-border px-3 text-left text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                    >
                      <ProviderMark provider={p} />
                      <span className="min-w-0 flex-1 truncate">
                        {p === "manual" ? words.add.upload : providerLabel(p)}
                      </span>
                      <ChevronRightIcon size={16} className="text-aui-muted" />
                    </button>
                  </li>
                ))}
              </ul>
            </section>
          </>
        )}
        {step === "manual" && (
          <>
            {back}
            <UploadFiles
              corpus={corpus.name}
              onCreated={(source) => void created({ kind: "source", name: source })}
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
              onCreated={(source) => void created({ kind: "source", name: source })}
            />
          </>
        )}
        {step === "bigquery" && (
          <BigQueryConnection
            canWrite
            onCancel={() => setStep(null)}
            // Added once saved; the sheet stays open to verify and publish it.
            onSaved={(connector) => void created({ kind: "catalog", name: connector }, false)}
            onAuthorize={() => startBigQueryLogin(setProblem)}
          />
        )}
        {step === "snowflake" && (
          <>
            {back}
            <SnowflakeSetup
              corpus={corpus.name}
              onCreated={async (connector) => {
                if (await created({ kind: "catalog", name: connector }, false)) {
                  loadConnectors();
                  setStep({ login: connector });
                }
              }}
            />
          </>
        )}
        {login && (
          <>
            {connectors.find((c) => c.name === login)?.login === "unavailable" ? (
              <p className="text-aui-sm text-aui-muted">{words.login.unavailable}</p>
            ) : (
              <LoginForm connector={login} onSaved={onClose} />
            )}
            <div>
              <button type="button" className={button} onClick={onClose}>
                {words.login.done}
              </button>
            </div>
          </>
        )}
      </div>
    </SideSheet>
  );
}

/** A Snowflake warehouse that holds no credential, made inside the corpus. */
function SnowflakeSetup({
  corpus,
  onCreated,
}: {
  corpus: string;
  onCreated: (connector: string) => Promise<void>;
}) {
  const sf = words.snowflake;
  const [draft, setDraft] = useState({
    name: nameOf(words.add.snowflakeName(corpus)),
    account: "",
    database: "",
    warehouse: "",
    role: "",
    token: "OAUTH" as "OAUTH" | "PROGRAMMATIC_ACCESS_TOKEN",
  });
  const group = useId();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const set = (key: keyof typeof draft) => (e: { target: { value: string } }) =>
    setDraft((d) => ({ ...d, [key]: key === "name" ? nameOf(e.target.value) : e.target.value }));
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setProblem(null);
    try {
      const connector = await api.createSnowflakeConnector({
        name: draft.name,
        account: draft.account.trim(),
        database: draft.database.trim(),
        warehouse: draft.warehouse.trim(),
        ...(draft.role.trim() ? { role: draft.role.trim() } : {}),
        token_type: draft.token,
      });
      await onCreated(connector.name);
    } catch (err) {
      setProblem(message(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form
      aria-label={providerLabel("snowflake")}
      className="flex min-w-0 flex-col gap-4"
      onSubmit={submit}
    >
      <Problem message={problem} />
      <Label text={words.form.name} hint={words.form.nameHint}>
        <input
          className={field}
          required
          pattern={NAME_PATTERN}
          value={draft.name}
          onChange={set("name")}
        />
      </Label>
      <Label text={sf.account} hint={sf.accountHint}>
        <input className={field} required value={draft.account} onChange={set("account")} />
      </Label>
      <Label text={sf.database}>
        <input className={field} required value={draft.database} onChange={set("database")} />
      </Label>
      <Label text={sf.warehouse}>
        <input className={field} required value={draft.warehouse} onChange={set("warehouse")} />
      </Label>
      <Label text={sf.role}>
        <input className={field} value={draft.role} onChange={set("role")} />
      </Label>
      <fieldset className="flex min-w-0 flex-col gap-1">
        <legend className="pb-1 text-aui-xs font-medium text-aui-muted">{sf.tokenType}</legend>
        {(
          [
            ["OAUTH", sf.oauth],
            ["PROGRAMMATIC_ACCESS_TOKEN", sf.pat],
          ] as const
        ).map(([value, label]) => (
          <label key={value} className="flex min-h-8 items-center gap-2 text-aui-sm text-aui-fg">
            <input
              type="radio"
              className="accent-aui-fg"
              name={group}
              checked={draft.token === value}
              onChange={() => setDraft((d) => ({ ...d, token: value }))}
            />
            {label}
          </label>
        ))}
      </fieldset>
      <div>
        <button type="submit" className={primary} disabled={busy}>
          {sf.create}
        </button>
      </div>
    </form>
  );
}

/**
 * The reader's own login to a warehouse: a reference to their token, or the
 * token itself where the workspace keeps values. A refusal says why, in the
 * server's words, since the workspace decides which it takes.
 */
function LoginForm({ connector, onSaved }: { connector: string; onSaved: () => void }) {
  const lw = words.login;
  const group = useId();
  const [kind, setKind] = useState<"ref" | "value">("ref");
  const [ref, setRef] = useState("");
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setProblem(null);
    try {
      await api.putCatalogLogin(connector, kind === "ref" ? { ref: ref.trim() } : { value });
      toast.success(lw.saved);
      onSaved();
    } catch (err) {
      setProblem(message(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form
      aria-label={lw.title(connector)}
      className="flex min-w-0 flex-col gap-4"
      onSubmit={submit}
    >
      <Problem message={problem} />
      <fieldset className="flex min-w-0 flex-col gap-1">
        <legend className="pb-1 text-aui-xs font-medium text-aui-muted">{lw.kind}</legend>
        {(
          [
            ["ref", lw.ref],
            ["value", lw.value],
          ] as const
        ).map(([id, label]) => (
          <label key={id} className="flex min-h-8 items-center gap-2 text-aui-sm text-aui-fg">
            <input
              type="radio"
              className="accent-aui-fg"
              name={group}
              checked={kind === id}
              onChange={() => setKind(id)}
            />
            {label}
          </label>
        ))}
      </fieldset>
      {kind === "ref" ? (
        <Label text={lw.refLabel} hint={lw.refHint}>
          <input
            className={field}
            required
            maxLength={512}
            spellCheck={false}
            value={ref}
            onChange={(e) => setRef(e.target.value)}
          />
        </Label>
      ) : (
        <Label text={lw.valueLabel}>
          <input
            className={field}
            type="password"
            autoComplete="off"
            required
            maxLength={8192}
            value={value}
            onChange={(e) => setValue(e.target.value)}
          />
        </Label>
      )}
      <div>
        <button type="submit" className={primary} disabled={busy}>
          {lw.save}
        </button>
      </div>
    </form>
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
          pattern={NAME_PATTERN}
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

/** Who owns a corpus, who may read it and which agents it is granted to; its owner changes each. */
function Access({
  corpus,
  owner,
  agents,
  directory,
  onChanged,
  onConflict,
}: {
  corpus: Corpus;
  owner: boolean;
  agents: string[];
  directory: ReturnType<typeof useDirectory>;
  onChanged: (corpus: Corpus) => void;
  onConflict: () => void;
}) {
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
      else setProblem(refusal(e, corpus.label));
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
  const granted = corpus.agents ?? [];
  const agentOptions: Pickable[] = [...new Set([...agents, ...granted])].map((a) => ({
    value: a,
    label: a,
    mark: <AgentMark name={a} size={16} />,
  }));
  const heading = "text-aui-xs font-medium text-aui-muted";

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <Problem message={problem} />
      <section aria-label={words.access.owner} className={`${card} flex flex-col gap-3`}>
        <h2 className={heading}>{words.access.owner}</h2>
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
            <h2 className={heading}>{words.access.readers}</h2>
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
      <section aria-label={words.agents.title} className={`${card} flex flex-col gap-3`}>
        <h2 className={heading}>{words.agents.title}</h2>
        {owner ? (
          <>
            <PickMany
              label={words.agents.title}
              add={words.agents.add}
              empty={words.agents.none}
              disabled={busy}
              options={agentOptions}
              value={granted}
              onChange={(next) =>
                void change({ agents: next }, () => toast.success(words.form.saved))
              }
            />
            <p className="text-aui-xs text-aui-muted">{words.agents.hint}</p>
          </>
        ) : granted.length === 0 ? (
          <p className="text-aui-sm text-aui-muted">{words.agents.none}</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {granted.map((a) => (
              <li key={a} className="flex items-center gap-2 text-aui-sm text-aui-fg">
                <AgentMark name={a} size={16} />
                {a}
              </li>
            ))}
          </ul>
        )}
      </section>
      <section aria-label={words.links.title} className={`${card} flex flex-col gap-3`}>
        <div className="flex items-center gap-2">
          <h2 className={heading}>{words.links.title}</h2>
          <Pill>{corpus.links === "shared" ? words.shared : words.isolated}</Pill>
        </div>
        {owner && (
          <Switch
            label={words.links.shared}
            hint={words.links.sharedHint}
            checked={corpus.links === "shared"}
            disabled={busy}
            onChange={(on) =>
              void change({ links: on ? "shared" : "isolated" }, () =>
                toast.success(words.form.saved),
              )
            }
          />
        )}
      </section>
    </div>
  );
}

/**
 * A corpus's slice of the workspace graph: one line on its health, then its
 * documents and tables with who owns each and whether it is stale. Whoever
 * owns a row confirms, declines or hands it off; the corpus's owner claims or
 * hands on one nobody owns.
 */
function GraphTab({
  corpus,
  owner,
  viewer,
  directory,
}: {
  corpus: Corpus;
  owner: boolean;
  viewer: string;
  directory: ReturnType<typeof useDirectory>;
}) {
  const gw = words.graph;
  const [view, setView] = useState<GraphView | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [handing, setHanding] = useState<GraphNodeView | null>(null);
  const [to, setTo] = useState("");

  useEffect(() => {
    let live = true;
    api.corpusGraph(corpus.name).then(
      (next) => live && setView(next),
      (e) => {
        if (!live) return;
        setView({
          nodes: [],
          edges: [],
          health: { nodes: 0, edges: 0, by_type: {}, unowned: 0, stale: 0 },
        });
        setProblem(e instanceof ApiError && e.status < 500 ? e.message : gw.failed);
      },
    );
    return () => {
      live = false;
    };
  }, [corpus.name, gw.failed]);

  const rebuild = async () => {
    setBusy(true);
    setProblem(null);
    try {
      setView(await api.buildCorpusGraph(corpus.name));
      toast.success(gw.rebuilt);
    } catch (e) {
      setProblem(message(e));
    } finally {
      setBusy(false);
    }
  };
  const decide = async (node: GraphNodeView, action: OwnerAction) => {
    setBusy(true);
    try {
      const next = await api.decideGraphOwner(node.id, action);
      setView((v) => v && { ...v, nodes: v.nodes.map((n) => (n.id === node.id ? next : n)) });
      toast.success(
        action.action === "confirm"
          ? gw.confirmed(node.name)
          : action.action === "decline"
            ? gw.declined(node.name)
            : gw.handedOff(node.name, directory.personName(action.to ?? "")),
      );
      return true;
    } catch (e) {
      toast.error(message(e));
      return false;
    } finally {
      setBusy(false);
    }
  };

  if (!view) return <SkeletonRows count={4} />;
  const by = view.health.by_type;
  const count = (types: string[]) => types.reduce((n, t) => n + (by[t] ?? 0), 0);
  const documents = Object.entries(by)
    .filter(([type]) => isDocumentNode({ type }))
    .reduce((n, [, c]) => n + c, 0);
  const health = gw.health([
    gw.documents(documents),
    gw.systems(count(["system"])),
    gw.people(count(["person"])),
    gw.unowned(view.health.unowned),
    gw.stale(view.health.stale),
    view.health.built_at ? gw.built(ago(view.health.built_at)) : gw.notBuilt,
  ]);
  const rowsShown = view.nodes.filter(isDocumentNode);
  const providerOf = (member?: string) => {
    const ref = memberOfNode(member);
    return ref
      ? corpus.members.find((m) => m.kind === ref.kind && m.name === ref.name)?.provider
      : undefined;
  };
  const ownerName = (node: GraphNodeView) =>
    !node.owner || node.owner_state === "unowned"
      ? gw.noOwner
      : node.owner === viewer
        ? gw.you
        : directory.personName(node.owner);
  const people = directory.peopleOptions.filter((p) => p.value !== handing?.owner);

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
        <p className="text-aui-xs text-aui-muted tabular-nums">{health}</p>
        {owner && (
          <button type="button" className={button} disabled={busy} onClick={() => void rebuild()}>
            {gw.rebuild}
          </button>
        )}
      </div>
      <Problem message={problem} />
      {rowsShown.length === 0 ? (
        <Empty title={gw.empty} />
      ) : (
        <ul aria-label={words.tabs.graph} className={rows}>
          {rowsShown.map((node) => {
            const link = safeLink(node.uri);
            const choices = ownerChoices(node, viewer, owner);
            const stale = node.staleness?.stale;
            return (
              <li
                key={node.id}
                aria-label={node.name}
                className="flex min-w-0 items-start gap-3 py-3"
              >
                <ProviderMark provider={providerOf(node.member) ?? "manual"} small />
                <div className="flex min-w-0 flex-1 flex-col gap-1">
                  <div className="flex min-w-0 items-center gap-2">
                    {link ? (
                      <a
                        href={link}
                        target="_blank"
                        rel="noreferrer"
                        className="min-w-0 truncate font-medium text-aui-sm text-aui-fg underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
                      >
                        {node.name}
                      </a>
                    ) : (
                      <span className="min-w-0 truncate font-medium text-aui-sm text-aui-fg">
                        {node.name}
                      </span>
                    )}
                    {node.type !== "document" && <Tag>{node.type}</Tag>}
                  </div>
                  <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-aui-xs text-aui-muted">
                    <span>{ownerName(node)}</span>
                    {node.owner_state && <Pill>{gw.states[node.owner_state]}</Pill>}
                    {stale && <Pill>{gw.staleLabel}</Pill>}
                    {stale && (node.staleness?.reasons?.length ?? 0) > 0 && (
                      <span>
                        {(node.staleness?.reasons ?? []).map((r) => gw.reasons[r] ?? r).join(", ")}
                      </span>
                    )}
                  </div>
                </div>
                {choices.length > 0 && (
                  <More
                    label={words.more(node.name)}
                    items={choices.map((c) =>
                      c === "confirm"
                        ? { label: gw.confirm, run: () => void decide(node, { action: "confirm" }) }
                        : c === "hand_off"
                          ? {
                              label: gw.handOff,
                              run: () => {
                                setTo("");
                                setHanding(node);
                              },
                            }
                          : {
                              label: gw.decline,
                              destructive: true,
                              run: () => void decide(node, { action: "decline" }),
                            },
                    )}
                  />
                )}
              </li>
            );
          })}
        </ul>
      )}
      {view.more && <p className="text-aui-xs text-aui-muted">{gw.more}</p>}
      <SideSheet
        open={handing !== null}
        title={gw.handOffTitle(handing?.name ?? "")}
        description={titleOf(corpus)}
        onClose={() => setHanding(null)}
      >
        <form
          aria-label={gw.handOffTitle(handing?.name ?? "")}
          className="flex min-w-0 flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (!handing || !to) return;
            void decide(handing, { action: "hand_off", to }).then((ok) => ok && setHanding(null));
          }}
        >
          <Label text={gw.to}>
            <NativeSelect value={to} onChange={(e) => setTo(e.target.value)}>
              <NativeSelectOption value="" />
              {people.map((p) => (
                <NativeSelectOption key={p.value} value={p.value}>
                  {p.label}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </Label>
          <div>
            <button type="submit" className={primary} disabled={busy || !to}>
              {gw.handOff}
            </button>
          </div>
        </form>
      </SideSheet>
    </div>
  );
}

/**
 * The types a corpus's ontology adds to the graph, a proposal from its
 * warehouses waiting to be accepted, and, for its owner, the ontology as JSON.
 */
function TypesTab({
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
  const tw = words.types;
  const declared = ontologyTypes(corpus.ontology);
  const proposed = ontologyTypes(corpus.proposed_ontology);
  const [picked, setPicked] = useState<string[]>(proposed.map((t) => t.name));
  const [json, setJson] = useState(() => JSON.stringify(corpus.ontology ?? {}, null, 2));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);

  const act = async (run: () => Promise<Corpus>, done: string) => {
    setBusy(true);
    setProblem(null);
    try {
      onChanged(await run());
      toast.success(done);
    } catch (e) {
      if (conflict(e)) onConflict();
      else setProblem(message(e));
    } finally {
      setBusy(false);
    }
  };
  const save = (e: FormEvent) => {
    e.preventDefault();
    let ontology: Record<string, unknown>;
    try {
      const parsed = JSON.parse(json);
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error();
      ontology = parsed;
    } catch {
      setProblem(tw.badJson);
      return;
    }
    void act(() => api.updateCorpus(corpus.name, { ontology, version: corpus.version }), tw.saved);
  };

  const typeRow = (t: OntologyType) => (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="font-medium text-aui-sm text-aui-fg">{t.name}</span>
      {t.properties.length > 0 && (
        <span className="text-aui-xs text-aui-muted">
          {tw.properties}: {t.properties.join(", ")}
        </span>
      )}
      {t.relations.length > 0 && (
        <span className="text-aui-xs text-aui-muted">
          {tw.relations}: {t.relations.map((r) => tw.relation(r.name, r.to, r.many)).join(", ")}
        </span>
      )}
    </div>
  );

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <Problem message={problem} />
      <section aria-label={tw.declared} className="flex min-w-0 flex-col gap-2">
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
          <h2 className="text-aui-xs font-medium text-aui-muted">{tw.declared}</h2>
          {owner && (
            <button
              type="button"
              className={button}
              disabled={busy}
              onClick={() => void act(() => api.inferOntology(corpus.name), tw.inferred)}
            >
              {tw.infer}
            </button>
          )}
        </div>
        {declared.length === 0 ? (
          <p className="text-aui-sm text-aui-muted">{tw.none}</p>
        ) : (
          <ul className={rows}>
            {declared.map((t) => (
              <li key={t.name} aria-label={t.name} className="py-2.5">
                {typeRow(t)}
              </li>
            ))}
          </ul>
        )}
      </section>
      {proposed.length > 0 && (
        <section aria-label={tw.proposed} className={`${card} flex min-w-0 flex-col gap-3`}>
          <h2 className="text-aui-xs font-medium text-aui-muted">{tw.proposed}</h2>
          <ul className="flex flex-col gap-2">
            {proposed.map((t) => (
              <li key={t.name}>
                {owner ? (
                  <label className="flex min-w-0 items-start gap-2">
                    <input
                      type="checkbox"
                      className="mt-1 accent-aui-fg"
                      aria-label={t.name}
                      checked={picked.includes(t.name)}
                      onChange={(e) =>
                        setPicked((was) =>
                          e.target.checked ? [...was, t.name] : was.filter((n) => n !== t.name),
                        )
                      }
                    />
                    {typeRow(t)}
                  </label>
                ) : (
                  typeRow(t)
                )}
              </li>
            ))}
          </ul>
          {owner && (
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                className={primary}
                disabled={busy || picked.length === 0}
                onClick={() => void act(() => api.acceptOntology(corpus.name, picked), tw.accepted)}
              >
                {tw.acceptSelected}
              </button>
              <button
                type="button"
                className={button}
                disabled={busy}
                onClick={() => void act(() => api.acceptOntology(corpus.name), tw.accepted)}
              >
                {tw.acceptAll}
              </button>
              <button
                type="button"
                className={button}
                disabled={busy}
                onClick={() => void act(() => api.discardOntology(corpus.name), tw.discarded)}
              >
                {tw.discard}
              </button>
            </div>
          )}
        </section>
      )}
      {owner && (
        <details className="min-w-0 rounded-aui border border-aui-border p-3">
          <summary className="cursor-pointer text-aui-sm text-aui-fg">{tw.editJson}</summary>
          <form
            aria-label={tw.editJson}
            className="flex min-w-0 flex-col gap-3 pt-3"
            onSubmit={save}
          >
            <Label text={tw.json}>
              <textarea
                className={`${field} min-h-64 font-aui-mono text-aui-xs`}
                spellCheck={false}
                value={json}
                onChange={(e) => setJson(e.target.value)}
              />
            </Label>
            <div>
              <button type="submit" className={primary} disabled={busy}>
                {tw.save}
              </button>
            </div>
          </form>
        </details>
      )}
    </div>
  );
}
