import type { ComposerSeed } from "./Composer.tsx";
import { mentionsIn } from "./tags.ts";

/** A draft's placement. A null project has not been chosen, and "" is the workspace. */
export interface Selection {
  project: string | null;
  channel: string;
}

export const emptySelection: Selection = {
  project: null,
  channel: "general",
};

/** Toggle an agent's mention in the draft. The draft is the source of its agent selections. */
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
