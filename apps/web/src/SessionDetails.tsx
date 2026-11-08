import { createContext, type ReactNode, useEffect, useState } from "react";
import { call } from "./api.ts";
import { type ConversationPanel, href } from "./route.ts";
import { followSession } from "./sessionStream.ts";
import { AgentMark, Problem, SkeletonRows, Tabs } from "./ui.tsx";

export const DetailsNavigation = createContext<ReactNode>(null);

/** All session inspectors share a single set of addressed sections. */
export function SessionDetailsProvider({
  panel,
  onPanel,
  children,
}: {
  panel?: ConversationPanel;
  onPanel: (panel: ConversationPanel) => void;
  children: ReactNode;
}) {
  const tabs = [
    { id: "workspace", label: "Files" },
    { id: "context", label: "Sources" },
    { id: "activity", label: "Steps" },
    { id: "overview", label: "Sessions" },
  ] as const;
  return (
    <DetailsNavigation.Provider
      value={
        <Tabs
          label="Session details"
          value={panel ?? "workspace"}
          tabs={[...tabs]}
          onChange={(next) => onPanel(next as ConversationPanel)}
        />
      }
    >
      {children}
    </DetailsNavigation.Provider>
  );
}

type Related = { id: string; agent: string; title?: string; status?: string };
type SessionLinks = {
  children: Related[];
  links: { target: string; kind?: string }[];
  summary?: string;
};

export function SessionRelations({ id, canWrite = false }: { id: string; canWrite?: boolean }) {
  const [data, setData] = useState<SessionLinks>();
  const [problem, setProblem] = useState<string>();
  const [summary, setSummary] = useState<string>();
  const [target, setTarget] = useState("");
  const [busy, setBusy] = useState(false);
  const [actionProblem, setActionProblem] = useState("");
  const path = `/api/sessions/${encodeURIComponent(id)}`;
  const catchUp = async () => {
    setBusy(true);
    setActionProblem("");
    try {
      const next = await call<SessionLinks>("GET", `${path}?summary=true`);
      setSummary(next.summary || "There are no messages to summarize yet.");
    } catch (error) {
      setActionProblem(error instanceof Error ? error.message : "Summary could not be loaded.");
    } finally {
      setBusy(false);
    }
  };
  const link = async () => {
    setActionProblem("");
    const match = /(?:^|\/)sessions\/(s_[a-z0-9]+)(?:[/?#]|$)/i.exec(target.trim());
    if (!match?.[1]) {
      setActionProblem("Paste a session link from this workspace.");
      return;
    }
    setBusy(true);
    try {
      await call("POST", `${path}/links`, { target: match[1], kind: "context" });
      setData(await call<SessionLinks>("GET", path));
      setTarget("");
    } catch (error) {
      setActionProblem(error instanceof Error ? error.message : "Session could not be linked.");
    } finally {
      setBusy(false);
    }
  };
  useEffect(() => {
    let live = true;
    setData(undefined);
    setProblem(undefined);
    setSummary(undefined);
    setTarget("");
    setActionProblem("");
    if (!id.startsWith("s_")) {
      setData({ children: [], links: [] });
      return;
    }
    call<SessionLinks>("GET", `/api/sessions/${encodeURIComponent(id)}`).then(
      (next) => {
        if (live) setData(next);
      },
      (error: unknown) => {
        if (live)
          setProblem(error instanceof Error ? error.message : "Sessions could not be loaded.");
      },
    );
    const stop = id.startsWith("s_")
      ? followSession(id, {
          session: (next) => {
            if (live) setData(next);
          },
        })
      : () => {};
    return () => {
      live = false;
      stop();
    };
  }, [id]);
  if (problem) return <Problem message={problem} />;
  if (!data) return <SkeletonRows count={3} />;
  const children = data.children ?? [];
  const links = data.links ?? [];
  return (
    <div className="flex flex-col gap-5">
      {id.startsWith("s_") && (
        <button
          type="button"
          className="self-start rounded-aui border border-aui-border px-3 py-2 text-aui-sm hover:bg-aui-hover"
          disabled={busy}
          onClick={() => void catchUp()}
        >
          Catch me up
        </button>
      )}
      {summary && (
        <section
          aria-label="Session summary"
          className="whitespace-pre-wrap rounded-aui bg-aui-hover p-3 text-aui-sm"
        >
          {summary}
        </section>
      )}
      <Problem message={actionProblem} />
      <section>
        <h3 className="mb-2 text-aui-sm font-medium">Child sessions</h3>
        {children.length ? (
          <ul>
            {children.map((child) => (
              <li key={child.id}>
                <a
                  href={href({ page: "conversation", id: child.id })}
                  className="flex min-h-11 items-center gap-2 rounded-aui px-2 text-aui-sm hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                >
                  <AgentMark name={child.agent} size={18} />
                  <span className="min-w-0 flex-1 truncate">{child.title || child.agent}</span>
                  <span className="text-aui-xs text-aui-muted">{child.status}</span>
                </a>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-aui-sm text-aui-muted">This agent has not called another agent yet.</p>
        )}
      </section>
      <section>
        <h3 className="mb-2 text-aui-sm font-medium">Linked sessions</h3>
        {links.length ? (
          <ul>
            {links.map((link, i) => (
              <li key={link.target}>
                <a
                  className="inline-flex min-h-11 items-center text-aui-sm underline"
                  href={href({ page: "conversation", id: link.target })}
                >
                  {link.kind || `Related session ${i + 1}`}
                </a>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-aui-sm text-aui-muted">
            Sessions read as context or cited as evidence appear here.
          </p>
        )}
      </section>
      {canWrite && id.startsWith("s_") && (
        <form
          className="flex flex-col gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            void link();
          }}
        >
          <label htmlFor="linked-session" className="text-aui-sm font-medium">
            Link a session
          </label>
          <input
            id="linked-session"
            className="min-w-0 rounded-aui border border-aui-border bg-transparent px-3 py-2 text-aui-sm"
            placeholder="Paste a session link"
            value={target}
            onChange={(event) => setTarget(event.target.value)}
            required
          />
          <button
            type="submit"
            disabled={busy || !target.trim()}
            className="self-start rounded-aui border border-aui-border px-3 py-2 text-aui-sm disabled:opacity-50"
          >
            Link session
          </button>
        </form>
      )}
    </div>
  );
}
