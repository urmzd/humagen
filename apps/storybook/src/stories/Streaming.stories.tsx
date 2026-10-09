import { TurnView } from "@agent-ui/components";
import { emptyTurn, type PromptConfig, type Turn, type TurnEvent } from "@agent-ui/core";
import * as fx from "@agent-ui/core/fixtures";
import {
  lifecycle,
  renderToSlack,
  type StreamChunk,
  streamStart,
  streamStopTitle,
  streamUpdate,
  suggestedPrompts,
} from "@agent-ui/render-slack";
import { BlockKitPreview } from "@agent-ui/render-slack/preview";
import { WebSurface } from "@agent-ui/render-web";
import { PromptInput, tokenize, useTurn } from "@agent-ui/web";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { useEffect, useRef, useState } from "react";
import { Dual } from "../Dual";

const meta = {
  title: "Agent/Streaming and prompt bar",
  parameters: {
    docs: {
      description: {
        component:
          "Token streaming on both surfaces, and the web-only prompt bar. Slack streams with chat.startStream/appendStream/stopStream and has its own composer; only suggested prompts carry over.",
      },
    },
  },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

const prompt: PromptConfig = {
  placeholder: "Ask about an incident, a deploy, or a runbook",
  suggestions: [
    { title: "Why is payments-api erroring?", message: "Why is payments-api returning 5xx?" },
    { title: "Summarize today's deploys", message: "Summarize today's production deploys." },
    { title: "Who's on call?", message: "Who is on call for payments right now?" },
  ],
};

function scriptFor(): TurnEvent[] {
  const done = fx.doneTurn;
  const [t1, t2, t3] = done.tasks;
  return [
    { type: "turn.started", turnId: "live", agent: done.agent },
    { type: "plan.updated", title: "Reading the alert" },
    {
      type: "task.upserted",
      task: {
        ...(t1 as Turn["tasks"][number]),
        title: "Reading the PagerDuty alert",
        status: "in_progress",
        output: undefined,
      },
    },
    { type: "task.upserted", task: t1 as Turn["tasks"][number] },
    { type: "plan.updated", title: "Searching runbooks" },
    {
      type: "task.upserted",
      task: {
        ...(t2 as Turn["tasks"][number]),
        title: "Searching runbooks",
        status: "in_progress",
        output: undefined,
        sources: undefined,
      },
    },
    { type: "task.upserted", task: t2 as Turn["tasks"][number] },
    { type: "plan.updated", title: "Checking recent deploys" },
    {
      type: "task.upserted",
      task: {
        ...(t3 as Turn["tasks"][number]),
        title: "Checking recent deploys",
        status: "in_progress",
        output: undefined,
        sources: undefined,
      },
    },
    { type: "task.upserted", task: t3 as Turn["tasks"][number] },
    ...done.citations.map((citation) => ({ type: "citation.added" as const, citation })),
    ...tokenize(done.text),
    { type: "turn.completed" },
  ];
}

/** Web: prompt bar plus a live turn. Slack: the exact appendStream calls the same events produce. */
function Conversation() {
  const { turn, push } = useTurn();
  const [asked, setAsked] = useState<string | null>(null);
  const [calls, setCalls] = useState<StreamChunk[][]>([]);
  const prev = useRef<Turn | null>(null);
  const timer = useRef<ReturnType<typeof setInterval> | null>(null);

  // Mirror every web update as Slack stream chunks.
  useEffect(() => {
    if (!turn.id) return;
    const { chunks } = streamUpdate(prev.current, turn);
    if (chunks.length > 0) setCalls((c) => [...c, chunks]);
    prev.current = turn;
  }, [turn]);

  const start = (message: string) => {
    setAsked(message);
    setCalls([]);
    prev.current = null;
    const script = scriptFor();
    let i = 0;
    timer.current = setInterval(() => {
      const event = script[i++];
      if (!event) {
        if (timer.current) clearInterval(timer.current);
        return;
      }
      push(event);
    }, 70);
  };

  const stop = () => {
    if (timer.current) clearInterval(timer.current);
    push({ type: "turn.stopped" });
  };

  const running = turn.status === "working" || turn.status === "queued";
  const finished = ["done", "failed", "stopped"].includes(turn.status) && turn.id !== "";
  const final = finished ? renderToSlack(<TurnView turn={turn} streamed />) : null;
  const signals = turn.id ? lifecycle(turn) : null;

  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <section className="flex min-w-0 flex-col gap-3">
        <h2 className="text-xs font-semibold uppercase tracking-wide text-aui-muted">Web</h2>
        <div className="flex min-h-96 flex-col justify-between gap-4 rounded-aui border border-aui-border bg-aui-surface p-4">
          <div className="flex flex-col gap-3">
            {asked && (
              <p className="self-end rounded-aui bg-aui-subtle px-3 py-2 text-sm">{asked}</p>
            )}
            {turn.id && (
              <WebSurface>
                <TurnView turn={turn} />
              </WebSurface>
            )}
          </div>
          <PromptInput
            config={prompt}
            running={running}
            showSuggestions={!asked}
            onSubmit={start}
            onStop={stop}
          />
        </div>
      </section>
      <section className="flex min-w-0 flex-col gap-3">
        <h2 className="text-xs font-semibold uppercase tracking-wide text-aui-muted">
          Slack · native streaming
        </h2>
        {!asked && (
          <Call name="assistant.threads.setSuggestedPrompts" body={suggestedPrompts(prompt)} />
        )}
        {signals && (
          <Call
            name="reactions + agents.sessions.setStatus"
            body={{
              reaction: signals.reaction ? `:${signals.reaction}:` : null,
              status: signals.sessionStatus,
              ...(signals.statusText ? { text: signals.statusText } : {}),
            }}
          />
        )}
        {turn.id && <Call name="chat.startStream" body={streamStart(turn)} />}
        <div className="flex max-h-80 flex-col gap-1 overflow-auto">
          {calls.map((chunks, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: append-only log
            <Call key={i} name={`chat.appendStream #${i + 1}`} body={{ chunks }} compact />
          ))}
        </div>
        {final && (
          <>
            <Call
              name="chat.stopStream"
              body={{
                ...(streamStopTitle(turn) ? { plan_title: streamStopTitle(turn) } : {}),
                text: final.text,
                blocks: `${final.blocks.length} blocks, below`,
              }}
            />
            <BlockKitPreview blocks={final.blocks} />
          </>
        )}
      </section>
    </div>
  );
}

function Call({ name, body, compact }: { name: string; body: unknown; compact?: boolean }) {
  return (
    <div className="rounded-md border border-aui-border bg-aui-surface text-xs">
      <p className="border-b border-aui-border px-2 py-1 font-mono font-semibold text-aui-accent">
        {name}
      </p>
      <pre className={`overflow-auto px-2 py-1 font-mono ${compact ? "whitespace-pre-wrap" : ""}`}>
        {JSON.stringify(body, compact ? undefined : null, compact ? undefined : 2)}
      </pre>
    </div>
  );
}

export const LiveConversation: Story = {
  name: "Live conversation (web + Slack stream)",
  render: () => <Conversation />,
};

export const PromptBar: Story = {
  name: "Prompt bar (web only)",
  render: () => (
    <Dual
      availability={{
        kind: "web-only",
        slackNative:
          "Slack provides the composer. Suggested prompts carry over via assistant.threads.setSuggestedPrompts (max 4), shown at the top of the agent's Messages tab.",
        slackPayload: suggestedPrompts(prompt, "Try asking"),
      }}
      web={<PromptInput config={prompt} onSubmit={() => {}} />}
    />
  ),
};

export const PromptBarRunning: Story = {
  name: "Prompt bar while running (Stop)",
  render: () => (
    <Dual
      availability={{
        kind: "web-only",
        slackNative:
          'Slack shows its native Stop button while the session is processing and sends agent_session_stopped; end the stream with plan title "Stopped".',
      }}
      web={<PromptInput config={prompt} running showSuggestions={false} onSubmit={() => {}} />}
    />
  ),
};

export const EmptyTurn: Story = {
  name: "Queued (before any output)",
  render: () => (
    <Dual>
      <TurnView turn={{ ...emptyTurn("q", { name: "Assistant" }), status: "queued" }} />
    </Dual>
  ),
};
