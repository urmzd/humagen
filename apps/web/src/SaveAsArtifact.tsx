import { defaultCopy } from "@humagen/components";
import { ArchiveIcon } from "@humagen/render-web";
import { useState } from "react";
import type { Stored } from "./storage.ts";
import { type ArtifactSave, workspaceFiles } from "./workspace-files.ts";

const words = defaultCopy.workspace.save;

/**
 * Save as Artifact: keeps a message or a scratchpad as a versioned artifact of
 * its conversation. A structured review is kept as its JSON, so its findings
 * stay pinned to file and line. The host opens what was saved.
 */
export function SaveAsArtifact({
  thread,
  what,
  className,
  label = false,
  disabled = false,
  onSaved,
}: {
  thread: string;
  what: ArtifactSave;
  className: string;
  /** Show the words beside the icon, as a toolbar button does. */
  label?: boolean;
  disabled?: boolean;
  onSaved?: (artifact: Stored) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [said, setSaid] = useState("");
  const save = async () => {
    if (busy || disabled) return;
    setBusy(true);
    setSaid("");
    try {
      const artifact = await workspaceFiles.save(thread, what);
      setSaid(words.saved(artifact.version));
      onSaved?.(artifact);
    } catch (error) {
      setSaid(error instanceof Error && error.message ? error.message : words.failed);
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <button
        type="button"
        className={className}
        aria-label={words.action}
        title={said || words.action}
        disabled={busy || disabled}
        onClick={() => void save()}
      >
        <ArchiveIcon size={label ? 14 : 16} />
        {label && (busy ? words.saving : words.action)}
      </button>
      <output className="sr-only">{said}</output>
    </>
  );
}
