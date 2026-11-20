/**
 * What a person did to a session's files by hand, recorded for their next
 * message (packages/core/src/human-activity.ts). Saving one file over and
 * over is one record: it says what the file is now against what it was
 * before the first of those saves, and is replaced on each. Once that record
 * has gone with a message, or the person took it out, the next save starts
 * again from the text it found. Nothing here sends anything.
 */
import { defaultCopy } from "@humagen/components";
import { type HumanActivityRecord, type HumanActivityStore, humanActivity } from "@humagen/core";
import { diffText, fileDiff } from "./workbenchChanges.ts";

const words = defaultCopy.workbenchChanges;

const lineCount = (text: string) => (text === "" ? 0 : text.replace(/\n$/, "").split("\n").length);

// The text each file had before the person's first save that has not been
// sent yet, and the record that says so, by conversation and path.
const unsent = new Map<string, { base: string | undefined; record: string }>();

/**
 * Records that a person saved a file in the session's files. `before` is the
 * text the save replaced, undefined when the save created the file.
 */
export function recordFileSave(
  conversation: string,
  path: string,
  before: string | undefined,
  after: string,
  store: HumanActivityStore = humanActivity,
): HumanActivityRecord | undefined {
  const slot = `${conversation}\n${path}`;
  const earlier = unsent.get(slot);
  const pending = earlier && store.list(conversation).some((r) => r.id === earlier.record);
  const base = pending ? earlier.base : before;
  if (pending) store.remove(conversation, earlier.record);
  unsent.delete(slot);
  // Saved back to what it was: nothing was done.
  if (base === after) return undefined;
  const diff = fileDiff(base ?? "", after, path);
  const record = store.add(conversation, {
    source: "files",
    summary:
      base === undefined
        ? words.recordCreated(path, lineCount(after))
        : words.recordEdited(path, diff.added, diff.removed),
    detail: diff.error ?? diffText(diff),
    key: `save:${path}`,
  });
  if (record) unsent.set(slot, { base, record: record.id });
  return record;
}

/** Records that a person deleted a file from the session's files. */
export function recordFileDelete(
  conversation: string,
  path: string,
  store: HumanActivityStore = humanActivity,
): HumanActivityRecord | undefined {
  const slot = `${conversation}\n${path}`;
  const earlier = unsent.get(slot);
  unsent.delete(slot);
  // A file made and deleted between two messages was never there for the agent.
  if (earlier && store.list(conversation).some((r) => r.id === earlier.record)) {
    store.remove(conversation, earlier.record);
    if (earlier.base === undefined) return undefined;
  }
  return store.add(conversation, {
    source: "files",
    summary: words.recordDeleted(path),
    key: `save:${path}`,
  });
}
