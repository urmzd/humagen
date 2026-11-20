import { defaultCopy } from "@humagen/components";
import type { ReactNode } from "react";
import type { GraphNodeView, GraphView } from "./api.ts";
import { ItemCard, itemGrid, Pill, Reference, Tag } from "./cards.tsx";
import { isDocumentNode, safeLink } from "./corpora.ts";
import { ago, type PageAction } from "./ui.tsx";

const gw = defaultCopy.corpora.graph;

/**
 * A graph's health as the parts of one line: its documents, systems and
 * people, how many are unowned or stale, and when it was built.
 */
export function graphHealthParts(health: GraphView["health"]): string[] {
  const by = health.by_type;
  const count = (types: string[]) => types.reduce((n, t) => n + (by[t] ?? 0), 0);
  const documents = Object.entries(by)
    .filter(([type]) => isDocumentNode({ type }))
    .reduce((n, [, c]) => n + c, 0);
  return [
    gw.documents(documents),
    gw.systems(count(["system"])),
    gw.people(count(["person"])),
    gw.unowned(health.unowned),
    gw.stale(health.stale),
    health.built_at ? gw.built(ago(health.built_at)) : gw.notBuilt,
  ];
}

/** A corpus a node comes from, as the chip a card shows for it. */
export interface NodeCorpus {
  name: string;
  label: string;
  href: string;
  onOpen: () => void;
}

/**
 * The graph's documents and tables as cards: who owns each, whether the owner
 * is suggested or confirmed, and whether it is stale and why. A card opens the
 * document where it lives. `corpora` adds the corpora a node comes from, and
 * `menu` what the reader may decide about its owner.
 */
export function GraphNodeCards({
  label,
  nodes,
  viewer,
  mark,
  menu,
  corpora,
}: {
  label: string;
  nodes: GraphNodeView[];
  viewer: string;
  mark: (node: GraphNodeView) => ReactNode;
  menu?: (node: GraphNodeView) => PageAction[];
  corpora?: (node: GraphNodeView) => NodeCorpus[];
}) {
  // The graph names each owner itself; the reader's own rows say You.
  const ownerName = (node: GraphNodeView) =>
    !node.owner || node.owner_state === "unowned"
      ? gw.noOwner
      : node.owner === viewer
        ? gw.you
        : node.owner_name || gw.someone;
  return (
    <ul aria-label={label} className={itemGrid}>
      {nodes.map((node) => {
        const link = safeLink(node.uri);
        const reasons = node.staleness?.stale ? (node.staleness.reasons ?? []) : [];
        const from = corpora?.(node) ?? [];
        return (
          <li key={node.id} aria-label={node.name}>
            <ItemCard
              title={node.name}
              mark={mark(node)}
              href={link}
              onOpen={link ? () => window.open(link, "_blank", "noopener,noreferrer") : undefined}
              status={node.owner_state ? <Pill>{gw.states[node.owner_state]}</Pill> : undefined}
              description={ownerName(node)}
              facts={
                from.length > 0 || node.type !== "document" || node.staleness?.stale ? (
                  <>
                    {from.map((c) => (
                      <Reference
                        key={c.name}
                        kind="corpus"
                        label={c.label}
                        href={c.href}
                        onOpen={c.onOpen}
                      />
                    ))}
                    {node.type !== "document" && <Reference label={node.type} />}
                    {node.staleness?.stale && <Pill>{gw.staleLabel}</Pill>}
                    {reasons.length > 0 && (
                      <Tag>{reasons.map((r) => gw.reasons[r] ?? r).join(", ")}</Tag>
                    )}
                  </>
                ) : undefined
              }
              menu={menu?.(node)}
            />
          </li>
        );
      })}
    </ul>
  );
}
