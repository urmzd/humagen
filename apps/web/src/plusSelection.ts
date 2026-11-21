import type { ComposerSeed } from "./Composer.tsx";
import { mentionsIn } from "./tags.ts";

/*
 * What the + menu picks and the mentions it toggles, apart from the menu
 * itself (Plus.tsx), so the bar can hold them while the menu is read on
 * demand (pages.ts).
 */

export interface Selection {
  project: string | null;
  channel: string;
}

export const emptySelection: Selection = {
  project: null,
  channel: "general",
};

/**
 * The + menu's agents, read from the draft: the agents it mentions are the
 * ones picked, a pick mentions one at the cursor, and picking a mentioned
 * one takes its mention out.
 */
export function mentionPicks(
  draft: string,
  available: { name: string }[] | null,
  seed: (next: ComposerSeed) => void,
): { mentioned: string[]; onMention: (name: string) => void } {
  const mentioned = mentionsIn(
    draft,
    (available ?? []).map((a) => a.name),
  );
  return {
    mentioned,
    onMention: (name) => seed({ mention: name, remove: mentioned.includes(name) }),
  };
}

/** Every item the arrow keys move between inside the active pane. */
