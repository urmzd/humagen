import { defaultCopy } from "@warp/components";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { coworking, type DirectoryPerson, type MentionItem, type MentionKind, type Revision, type Skill } from "./api.ts";
import { availableOf } from "./availability.ts";
import { type TagOption, type Typing, tagOptions } from "./tags.ts";

/** What the chat bar completes, and the call it makes as a tag is typed. */
export interface Completion {
  label: string;
  options: TagOption[];
  onQuery: (typing: Typing) => void;
}

/**
 * Completion for a message in a project: `@` people, the agents that can
 * answer there, @auto and the mention kinds (`@sme:`, completed further to
 * their items as `@kind:` is typed); `$` the project's skills. With `*`, an
 * agent that lives in a project is offered only where the platform lists
 * it, on its home's chain: `*` never reaches it. The person typing is never
 * offered: a tag of oneself tells nobody anything.
 *
 * It reads `GET /api/projects/{p}/agents`, `GET /api/people`,
 * `GET /api/skills` and `GET /api/mentions`. A list that cannot be read is
 * left out, so the rest still complete; topics stand in for sme's items
 * where mention kinds cannot be read.
 */
export function useCompletion(project: string, agents: Revision[], homes: Record<string, string>, topics: string[], me: string): Completion {
  const [names, setNames] = useState<{ all: boolean; names: string[] } | null>(null);
  const [people, setPeople] = useState<DirectoryPerson[]>([]);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [kinds, setKinds] = useState<MentionKind[] | null>(null);
  const [items, setItems] = useState<{ kind: string; name: string; description?: string }[]>([]);
  const timer = useRef<number | undefined>(undefined);
  // The newest lookup alone may answer: an older one that returns late is dropped.
  const asked = useRef(0);

  useEffect(() => {
    let live = true;
    coworking
      .available(project)
      .then((a) => live && setNames(a.available))
      .catch(() => live && setNames(null));
    coworking
      .skills(project)
      .then((s) => live && setSkills(s))
      .catch(() => live && setSkills([]));
    coworking
      .mentionKinds(project)
      .then((k) => live && setKinds(k))
      .catch(() => live && setKinds(null));
    setItems([]);
    return () => {
      live = false;
    };
  }, [project]);

  const onQuery = useCallback(
    (t: Typing) => {
      if (t.sigil !== "@") return;
      window.clearTimeout(timer.current);
      const n = ++asked.current;
      const colon = t.query.indexOf(":");
      timer.current = window.setTimeout(() => {
        if (colon > 0) {
          const kind = t.query.slice(0, colon);
          coworking
            .mentionItems(project, kind, t.query.slice(colon + 1))
            .then((got: MentionItem[]) => {
              if (n === asked.current) setItems(got.slice(0, 20).map((i) => ({ kind, name: i.name, ...(i.description ? { description: i.description } : {}) })));
            })
            .catch(() => n === asked.current && setItems([]));
          return;
        }
        // One more than the list shows, for the person typing, who is left out.
        coworking
          .people(t.query, 9)
          .then((p) => n === asked.current && setPeople(p))
          .catch(() => n === asked.current && setPeople([]));
      }, 150);
    },
    [project],
  );
  useEffect(() => () => window.clearTimeout(timer.current), []);

  const options = useMemo(() => {
    const offered = new Set(
      availableOf(
        agents.map((a) => a.spec.name),
        names ? { available: names } : null,
        homes,
      ),
    );
    const here = agents.filter((a) => offered.has(a.spec.name)).map((a) => ({ name: a.spec.name, description: a.spec.description }));
    const others = people.filter((p) => p.subject !== me && p.handle !== me);
    return tagOptions(
      here,
      others,
      topics,
      { auto: defaultCopy.auto.description, experts: defaultCopy.threads.experts, kind: defaultCopy.threads.mentionKind },
      {
        ...(kinds ? { kinds: kinds.map((k) => ({ kind: k.kind, ...(k.title ? { title: k.title } : {}) })) } : {}),
        items,
        skills: skills.map((s) => ({ name: s.name, ...(s.description ? { description: s.description } : {}) })),
      },
    );
  }, [agents, homes, names, people, topics, me, kinds, items, skills]);

  return { label: defaultCopy.threads.typeahead, options, onQuery };
}
