import type {
  ContinueImportedDocumentBody,
  KnowledgeDocument,
  ThreadImport,
} from "@humagen/client/types";

/**
 * Continuing an imported conversation, between the platform's shapes and
 * what a surface shows: which documents offer it, what is sent to start one,
 * and what the new conversation says about its origin. No React and no DOM.
 */

/** A tool an import writes a session's document from. */
export type ImportTool = NonNullable<ThreadImport["tool"]>;

/** The tools, as the uri scheme of the documents an import writes. */
export const importTools: readonly ImportTool[] = ["claude", "codex", "chatgpt", "claude-ai"];

/**
 * The tool a document's uri names when an import wrote it from a session
 * (`claude:`, `codex:`, `chatgpt:`, `claude-ai:` and an id), else undefined.
 * The uri only decides whether the action is offered: the server decides
 * what the document is.
 */
export function importedTool(uri: string | undefined): ImportTool | undefined {
  const colon = uri?.indexOf(":") ?? -1;
  if (!uri || colon <= 0 || colon === uri.length - 1) return undefined;
  const scheme = uri.slice(0, colon);
  return importTools.find((tool) => tool === scheme);
}

/**
 * Whether a listed document can be offered for continuing: an imported
 * session's, with the digest the request must pin. A document kept before
 * digests were has none, so it cannot be named and is not offered.
 */
export function continuable(doc: Pick<KnowledgeDocument, "uri" | "digest">): boolean {
  return importedTool(doc.uri) !== undefined && !!doc.digest;
}

/**
 * The body that starts a continuation of a document as it was listed: its
 * digest, its structure's digest only when the listing records one, and the
 * project the source was seen filed in (`~` for the workspace itself).
 */
export function continueBody(
  doc: Pick<KnowledgeDocument, "digest" | "structure_digest">,
  project: string,
): ContinueImportedDocumentBody {
  return {
    document_digest: doc.digest ?? "",
    project: project || "~",
    ...(doc.structure_digest ? { structure_digest: doc.structure_digest } : {}),
  };
}

/** What a continuation says about what its agent was sent, every count from the record. */
export interface ImportFacts {
  /** Whether an import kept who said what. */
  known: boolean;
  /** Turns sent to the agent. */
  sent: number;
  /** Turns the import kept: those sent and those left out to fit. */
  kept: number;
  /** Turns sent cut to their opening to fit. */
  shortened: number;
  /** Branches not continued; undefined when not known. */
  branches?: number;
  /** Turns whose files were not imported; undefined when not known. */
  files?: number;
  /** Turns the import itself left out when it wrote the document; undefined when not known. */
  importLeftOut?: number;
  /** Turns the import itself cut to their opening; undefined when not known. */
  importShortened?: number;
}

/**
 * The facts of a continuation, as the API says a client shows them: how many
 * turns were sent of how many the import kept, the branches not continued, the
 * turns whose files were not imported, and what the import itself cut. Counts
 * an import did not state are undefined, never zero.
 */
export function importFacts(imp: ThreadImport): ImportFacts {
  const known = imp.structure === "kept";
  const sent = imp.context.length;
  const cut = imp.import_cut;
  return {
    known,
    sent,
    kept: sent + imp.omitted.turns + imp.omitted.results,
    shortened: imp.omitted.cut,
    branches: known ? (imp.branches ?? 0) : undefined,
    files: known ? (cut?.attachments ?? 0) : undefined,
    importLeftOut: known ? (cut ? cut.turns + cut.results : 0) : undefined,
    importShortened: known ? (cut?.cut_turns ?? 0) : undefined,
  };
}

/**
 * How the document a conversation continues stands now, from the source's
 * current listing: `same` at the pinned revision, `changed` when its digest
 * differs, `gone` when the listing no longer has it.
 */
export function importDrift(
  imp: Pick<ThreadImport, "document" | "document_digest">,
  listed: Pick<KnowledgeDocument, "id" | "digest">[],
): "same" | "changed" | "gone" {
  const now = listed.find((doc) => doc.id === imp.document);
  if (!now) return "gone";
  return now.digest === imp.document_digest ? "same" : "changed";
}
