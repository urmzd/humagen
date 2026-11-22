import { foldAll, type ResponseState, type Task, type Turn } from "@humagen/core";
import { responseStreams } from "@humagen/core/fixtures/responses";
import { WebSurface } from "@humagen/render-web";
import { AgentMessage, CallThreadContext, RetryCallContext, UserMessage } from "@humagen/web";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@humagen/render-web/fonts.css";
import "./styles.css";
import { callThreadsOf } from "./MessageRefs.tsx";
import { type TranscriptScene, transcriptScenes } from "./transcript.fixtures.ts";
import { AgentTint, setAgentHues } from "./ui.tsx";

/**
 * A conversation transcript, one scene at a time, drawn from fixtures alone:
 * `?scene=failed|connect|refs|tools|running|parallel|nested` and
 * `?theme=light|dark`. The last two are recorded responses with agents that
 * ran at the same time, each drawn as its own thread in the turn. Open
 * /transcript-gallery.html on the dev server. It calls nothing on the
 * platform and is not part of the built console; the screen captures of
 * this change are taken from it.
 */
setAgentHues({ humagen: 12, "github-reviewer": 48 });

const params = new URLSearchParams(window.location.search);
const theme = params.get("theme") === "light" ? "light" : "dark";
document.documentElement.dataset.theme = theme;
const sceneName = params.get("scene") ?? "failed";
const scene = sceneName as TranscriptScene;

/**
 * A recorded response as the turn a session shows: the answering agent's own
 * words and steps in order, and a call for each agent it brought in. Calls
 * with no words or steps between them started together.
 */
function turnOf(state: ResponseState, base: Turn): Turn {
  let text = "";
  const tasks: Task[] = [];
  const started = Date.parse("2026-11-21T15:00:00Z");
  let wave = 0;
  let last: "call" | "other" = "other";
  for (const b of state.content) {
    if (b.type === "text") {
      text += (text ? "\n\n" : "") + b.text;
      last = "other";
      continue;
    }
    if (b.type !== "tool") continue;
    const done = b.status === "done";
    const status: Task["status"] =
      b.status === "error"
        ? "error"
        : done
          ? "complete"
          : b.status === "running"
            ? "in_progress"
            : "waiting";
    if (b.kind === "agent") {
      if (last !== "call") wave++;
      last = "call";
      tasks.push({
        id: b.id,
        title: b.agent ?? b.name,
        tool: "call_agent",
        status,
        input: JSON.stringify({ agent: b.agent, title: b.title }),
        output: b.result?.text,
        durationMs: b.elapsedMs,
        startedAt: new Date(started + wave * 600_000).toISOString(),
        subagent: { name: b.agent ?? b.name, runId: b.runId, durationMs: b.elapsedMs },
        at: text.length,
      });
      continue;
    }
    last = "other";
    tasks.push({
      id: b.id,
      title: b.title ?? b.name,
      tool: b.name,
      status,
      input: JSON.stringify(b.input),
      output: b.result?.text,
      durationMs: b.elapsedMs,
      at: text.length,
    });
  }
  return {
    ...base,
    id: state.id || "recorded",
    agent: { ...base.agent, name: state.agent ?? base.agent.name },
    text,
    tasks,
    thoughts: [],
    status: state.status === "running" ? "working" : "done",
    durationMs: state.tMs,
  };
}

const recorded = {
  parallel: {
    ask: "Plan the launch: research why teams self-host, and draft the note.",
    events: responseStreams.parallelAgents,
  },
  nested: { ask: "Roll the fix out to production.", events: responseStreams.nestedApproval },
} as const;
const record = recorded[sceneName as keyof typeof recorded];
const recordedState = record ? foldAll(record.events) : undefined;
const shown = recordedState
  ? {
      ask: record.ask,
      turn: () => turnOf(recordedState, transcriptScenes.tools.turn()),
    }
  : (transcriptScenes[scene] ?? transcriptScenes.failed);

function Transcript() {
  const turn = shown.turn();
  return (
    <main className="h-full overflow-y-auto bg-aui-bg">
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-6 px-4 py-6">
        <div className="self-end">
          <UserMessage text={shown.ask} />
        </div>
        <WebSurface>
          <AgentTint name={turn.agent.name}>
            <RetryCallContext.Provider value={{ ask: async () => {} }}>
              <CallThreadContext.Provider
                value={recordedState ? callThreadsOf(recordedState) : null}
              >
                <AgentMessage turn={turn} disclaimer={false} activity={false} />
              </CallThreadContext.Provider>
            </RetryCallContext.Provider>
          </AgentTint>
        </WebSurface>
      </div>
    </main>
  );
}

const root = document.getElementById("root");
if (root)
  createRoot(root).render(
    <StrictMode>
      <Transcript />
    </StrictMode>,
  );
