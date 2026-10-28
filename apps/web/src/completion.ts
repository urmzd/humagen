import { defaultCopy } from "@humagen/components";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  coworking,
  type DirectoryPerson,
  type MentionItem,
  type MentionKind,
  type Revision,
  type Skill,
} from "./api.ts";
import { availableOf } from "./availability.ts";
import { type TagOption, type Typing, tagOptions } from "./tags.ts";

/** What the chat bar completes, and the call it makes as a tag is typed. */
export interface Completion {
  label: string;
  options: TagOption[];
  onQuery: (typing: Typing) => void;
  /** The agents that may answer in the project, for the + menu; null while they load. */
  agents: { name: string; description?: string }[] | null;
  /** Skills loaded for this exact project, also used by the composer picker. */
  skills: { name: string; description?: string }[] | null;
  skillsFailed: boolean;
}

/**
 * Completion for a message in a project: `@` people, the agents that can
 * answer there and the mention kinds (`@sme:`, completed further to
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
export function useCompletion(
  project: string,
  agents: Revision[],
  homes: Record<string, string>,
  topics: string[],
  me: string,
  /** How the platform names the reader, which a person's subject may not show. */
  handle = "",
): Completion {
  // undefined while it loads, null when it could not be read.
  const [names, setNames] = useState<{ all: boolean; names: string[] } | null | undefined>(
    undefined,
  );
  const [people, setPeople] = useState<DirectoryPerson[]>([]);
  const [skills, setSkills] = useState<{ project: string; items: Skill[]; failed: boolean } | null>(
    null,
  );
  const [kinds, setKinds] = useState<MentionKind[] | null>(null);
  const [items, setItems] = useState<{ kind: string; name: string; description?: string }[]>([]);
  const timer = useRef<number | undefined>(undefined);
  // The newest lookup alone may answer: an older one that returns late is dropped.
  const asked = useRef(0);
  // The kinds read for the project, for a lookup made as one is typed.
  const kindsNow = useRef<MentionKind[] | null>(null);

  useEffect(() => {
    let live = true;
    // A lookup for the project before is not this one's.
    window.clearTimeout(timer.current);
    asked.current++;
    setNames(undefined);
    setPeople([]);
    setSkills(null);
    coworking
      .available(project)
      .then((a) => live && setNames(a.available))
      .catch(() => live && setNames(null));
    coworking
      .skills(project)
      .then((items) => live && setSkills({ project, items, failed: false }))
      .catch(() => live && setSkills({ project, items: [], failed: true }));
    coworking
      .mentionKinds(project)
      .then((k) => {
        if (!live) return;
        kindsNow.current = k;
        setKinds(k);
      })
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
          const q = t.query.slice(colon + 1);
          // A kind that resolves to people never offers the person typing.
          // Items name people by handle, so the directory's own lookup of the
          // same words says which handle is theirs.
          const people = kindsNow.current?.find((k) => k.kind === kind)?.resolves === "people";
          Promise.all([
            coworking.mentionItems(project, kind, q),
            people
              ? coworking.people(q, 21).catch(() => [] as DirectoryPerson[])
              : Promise.resolve([] as DirectoryPerson[]),
          ])
            .then(([got, found]: [MentionItem[], DirectoryPerson[]]) => {
              if (n !== asked.current) return;
              const mine = new Set(found.filter((p) => p.subject === me).map((p) => p.handle));
              mine.add(me);
              if (handle) mine.add(handle);
              const kept = people ? got.filter((i) => !mine.has(i.name)) : got;
              setItems(
                kept.slice(0, 20).map((i) => ({
                  kind,
                  name: i.name,
                  ...(i.description ? { description: i.description } : {}),
                })),
              );
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
    [project, me, handle],
  );
  useEffect(() => () => window.clearTimeout(timer.current), []);

  const here = useMemo(() => {
    const offered = new Set(
      availableOf(
        agents.map((a) => a.spec.name),
        names ? { available: names } : null,
        homes,
      ),
    );
    return agents
      .filter((a) => offered.has(a.spec.name))
      .map((a) => ({
        name: a.spec.name,
        ...(a.spec.description ? { description: a.spec.description } : {}),
      }));
  }, [agents, homes, names]);

  const scopedSkills = skills?.project === project ? skills : null;
  const skillOptions = useMemo(
    () => (skills?.project === project ? skills.items : []),
    [skills, project],
  );

  const options = useMemo(() => {
    // The directory leaves out a subject that holds an address, so the reader is known by their handle too.
    const others = people.filter(
      (p) => p.subject !== me && p.handle !== me && (!handle || p.handle !== handle),
    );
    return tagOptions(
      here,
      others,
      topics,
      { experts: defaultCopy.threads.experts, kind: defaultCopy.threads.mentionKind },
      {
        ...(kinds
          ? {
              kinds: kinds.map((k) => ({
                kind: k.kind,
                resolves: k.resolves,
                ...(k.title ? { title: k.title } : {}),
              })),
            }
          : {}),
        items,
        skills: skillOptions.map((s) => ({
          name: s.name,
          ...(s.description ? { description: s.description } : {}),
        })),
      },
    );
  }, [here, people, topics, me, handle, kinds, items, skillOptions]);

  return {
    label: defaultCopy.threads.typeahead,
    options,
    onQuery,
    agents: names === undefined ? null : here,
    skills: scopedSkills ? skillOptions : null,
    skillsFailed: scopedSkills?.failed ?? false,
  };
}
