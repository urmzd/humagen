import { defaultCopy } from "@humagen/components";
import { useCallback, useEffect, useState } from "react";
import { ApiError, api, type Corpus, type GraphNodeView, type GraphView } from "./api.ts";
import { ProviderMark } from "./Corpora.tsx";
import { isDocumentNode, memberOfNode, titleOf } from "./corpora.ts";
import { GraphNodeCards, graphHealthParts } from "./GraphNodes.tsx";
import { href } from "./route.ts";
import { button, Empty, Page, Problem, SkeletonRows } from "./ui.tsx";

const words = defaultCopy.corpora;
const gw = words.graph;

/**
 * The workspace graph across every corpus the reader may read: one line on
 * its health, then its documents and tables with who owns each, whether it is
 * stale, and the corpora it comes from. It only reads: owners decide about a
 * document on its corpus's Graph tab.
 */
export function WorkspaceGraph({
  viewer,
  onBack,
  onOpenCorpus,
}: {
  viewer: string;
  onBack: () => void;
  onOpenCorpus: (name: string) => void;
}) {
  const [view, setView] = useState<GraphView | null>(null);
  const [corpora, setCorpora] = useState<Corpus[]>([]);
  const [problem, setProblem] = useState<string | null>(null);

  const load = useCallback(() => {
    setProblem(null);
    api.workspaceGraph().then(
      (next) => setView(next),
      (e) => setProblem(e instanceof ApiError && e.status < 500 ? e.message : gw.failed),
    );
    // Corpora give each node's corpus its title and its source its mark. Without them a corpus goes by its name.
    api.corpora().then(setCorpora, () => undefined);
  }, []);
  useEffect(() => load(), [load]);

  const corpusOf = (name: string) => corpora.find((c) => c.name === name);
  const providerOf = (node: GraphNodeView) => {
    const ref = memberOfNode(node.member);
    if (!ref) return undefined;
    for (const name of node.corpora ?? []) {
      const member = corpusOf(name)?.members.find(
        (m) => m.kind === ref.kind && m.name === ref.name,
      );
      if (member) return member.provider;
    }
    return undefined;
  };
  const shown = view?.nodes.filter(isDocumentNode) ?? [];

  return (
    <Page title={gw.workspaceTitle} back={{ label: words.title, onClick: onBack }}>
      <Problem message={problem} />
      {problem ? (
        <div>
          <button type="button" className={button} onClick={load}>
            {gw.retry}
          </button>
        </div>
      ) : !view ? (
        <SkeletonRows count={4} />
      ) : (
        <div className="flex min-w-0 flex-col gap-4">
          <p className="text-aui-xs text-aui-muted tabular-nums">
            {gw.health([
              gw.nodes(view.health.nodes),
              gw.relations(view.health.edges),
              ...graphHealthParts(view.health),
            ])}
          </p>
          {shown.length === 0 ? (
            <div className="flex flex-col items-start gap-3">
              <Empty title={gw.empty}>{gw.howBuilt}</Empty>
              <button type="button" className={button} onClick={onBack}>
                {gw.openKnowledge}
              </button>
            </div>
          ) : (
            <GraphNodeCards
              label={gw.workspaceTitle}
              nodes={shown}
              viewer={viewer}
              mark={(node) => <ProviderMark provider={providerOf(node) ?? "manual"} />}
              corpora={(node) =>
                (node.corpora ?? []).map((name) => {
                  const corpus = corpusOf(name);
                  return {
                    name,
                    label: corpus ? titleOf(corpus) : name,
                    href: href({ page: "corpus", name, tab: "graph" }),
                    onOpen: () => onOpenCorpus(name),
                  };
                })
              }
            />
          )}
          {view.more && <p className="text-aui-xs text-aui-muted">{gw.more}</p>}
        </div>
      )}
    </Page>
  );
}
