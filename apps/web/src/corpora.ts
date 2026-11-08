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

/** A corpus's label, from least to most sensitive. */
export const LABELS = ["public", "internal", "confidential", "restricted"] as const;
export type CorpusLabel = (typeof LABELS)[number];

/** One type an ontology adds to the graph: its properties and the relations that start at it. */
export interface OntologyType {
  name: string;
  properties: string[];
  relations: { name: string; to: string; many: boolean }[];
}

/**
 * The types a corpus ontology (a JSON Schema with `$defs` and `x-relations`,
 * docs/architecture/sources.md#ontologies) declares, by name. Anything not
 * shaped as one reads as no types; the server is what checks an ontology.
 */
export function ontologyTypes(ontology: Record<string, unknown> | undefined): OntologyType[] {
  const defs = ontology?.$defs;
  if (!defs || typeof defs !== "object") return [];
  const relations = Array.isArray(ontology?.["x-relations"])
    ? (ontology["x-relations"] as Record<string, unknown>[])
    : [];
  return Object.entries(defs as Record<string, Record<string, unknown>>)
    .map(([name, type]) => ({
      name,
      properties: Object.keys(
        type && typeof type.properties === "object" && type.properties ? type.properties : {},
      ),
      relations: relations
        .filter((r) => r && r.from === name && typeof r.name === "string")
        .map((r) => ({
          name: String(r.name),
          to: String(r.to ?? ""),
          many: r.cardinality === "many",
        })),
    }))
    .sort((a, b) => a.name.localeCompare(b.name));
}

/** The graph's node types that are not documents or tables. */
const NOT_DOCUMENTS = new Set(["person", "team", "project", "system", "topic"]);

/** Whether a graph node is a document or a warehouse table (typed by an ontology or not). */
export function isDocumentNode(node: { type: string }): boolean {
  return !NOT_DOCUMENTS.has(node.type);
}

/** The corpus member a graph node was built from, as `source/<name>` or `catalog/<name>`. */
export function memberOfNode(member: string | undefined): CorpusMemberRef | undefined {
  const [kind, ...rest] = (member ?? "").split("/");
  const name = rest.join("/");
  return (kind === "source" || kind === "catalog") && name ? { kind, name } : undefined;
}

/** What the reader may do about who owns a document or table. */
export function ownerChoices(
  node: { owner?: string; owner_state: "" | "suggested" | "confirmed" | "unowned" },
  viewer: string,
  corpusOwner: boolean,
): ("confirm" | "hand_off" | "decline")[] {
  if (node.owner && node.owner === viewer) {
    if (node.owner_state === "suggested") return ["confirm", "hand_off", "decline"];
    if (node.owner_state === "confirmed") return ["hand_off", "decline"];
  }
  // A corpus's owner may claim, or hand on, a document of it nobody owns.
  if (corpusOwner && (node.owner_state === "unowned" || !node.owner))
    return ["confirm", "hand_off"];
  return [];
}
