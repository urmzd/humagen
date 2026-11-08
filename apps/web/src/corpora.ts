import type { Corpus, CorpusMemberRef, CorpusReader, Source } from "./api.ts";

/**
 * Corpora as the console shows them (docs/architecture/sources.md#corpora):
 * who may read one, what it holds, and the sources no corpus holds yet.
 * Nothing here calls the API.
 */

/** How a corpus is shared: with its owner alone, the workspace, or named people and projects. */
export type ReaderMode = "me" | "workspace" | "chosen";

export function readerMode(readers: CorpusReader[]): ReaderMode {
  if (readers.some((r) => r.kind === "workspace")) return "workspace";
  return readers.length === 0 ? "me" : "chosen";
}

/** The readers a choice names. Nobody chosen is the owner alone. */
export function readersOf(
  mode: ReaderMode,
  people: string[] = [],
  projects: string[] = [],
): CorpusReader[] {
  if (mode === "workspace") return [{ kind: "workspace" }];
  if (mode === "me") return [];
  return [
    ...people.map((name) => ({ kind: "person" as const, name })),
    ...projects.map((name) => ({ kind: "project" as const, name })),
  ];
}

/** The people (by subject) or projects (by name) a reader list names. */
export function namedReaders(readers: CorpusReader[], kind: "person" | "project"): string[] {
  return readers.filter((r) => r.kind === kind && r.name).map((r) => r.name as string);
}

/** The providers a corpus's members come from, once each, in the order they were added. */
export function providersOf(corpus: Corpus): string[] {
  return [
    ...new Set(
      corpus.members
        .filter((m) => m.status !== "hidden" && m.provider)
        .map((m) => m.provider as string),
    ),
  ];
}

/** Documents, tables and views across the members the reader can see. */
export function itemsOf(corpus: Corpus): number {
  return corpus.members.reduce((n, m) => n + (m.status === "hidden" ? 0 : m.items), 0);
}

/** A source's provider, as a member names it: `manual` for one without a lifecycle. */
export function providerOfSource(source: Source): string {
  return source.lifecycle?.provider ?? "manual";
}

/** The sources the reader can see that no corpus they can read holds. */
export function unfiledSources(sources: Source[], corpora: Corpus[]): Source[] {
  const held = new Set(
    corpora.flatMap((c) => c.members.filter((m) => m.kind === "source").map((m) => m.name)),
  );
  return sources.filter((s) => !held.has(s.name));
}

/** Whether a corpus holds a member, by kind and name. */
export function holds(corpus: Corpus, ref: CorpusMemberRef): boolean {
  return corpus.members.some((m) => m.kind === ref.kind && m.name === ref.name);
}

/** A corpus or source name, as the API takes it. */
export const NAME = /^[a-z0-9][a-z0-9-]{0,62}$/;

/** What a person types, made into a name: "HR policies" becomes "hr-policies". */
export function nameOf(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/^-+/, "")
    .slice(0, 63);
}

/** What people call a corpus: its title, or its name without one. */
export function titleOf(corpus: Pick<Corpus, "name" | "title">): string {
  return corpus.title?.trim() || corpus.name;
}

/** The name a title gives a new corpus: "HR policies" is hr-policies. Empty when it has no letter or digit a name can hold. */
export function nameFromTitle(title: string): string {
  return nameOf(title).replace(/-+$/, "");
}

/** Where a link from a search hit may go: the web, and nowhere a link could run code. */
export function safeLink(uri: string | undefined): string | undefined {
  if (!uri) return undefined;
  try {
    const url = new URL(uri);
    return url.protocol === "https:" || url.protocol === "http:" ? url.href : undefined;
  } catch {
    return undefined;
  }
}
