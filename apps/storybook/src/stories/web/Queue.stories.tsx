import type { Meta, StoryObj } from "@storybook/react-vite";
import { emptyTurn, type QueuedMessage, type Turn } from "@warp/core";
import * as fx from "@warp/core/fixtures";
import { WebSurface } from "@warp/render-web";
import { AgentMessage, PromptInput, StatusLine, Thread, UserMessage, useQueue } from "@warp/web";
import { useRef, useState } from "react";
import { expect, userEvent, waitFor, within } from "storybook/test";
import { webOnly } from "../../support";

const SLACK =
  "host-side queueing per thread: queued messages get a :hourglass_flowing_sand: reaction until their turn starts";

interface Entry {
  id: string;
  text: string;
  turn: Turn;
}

type Args = { turnMs: number; failFirst: boolean };

/** A fake runtime that honors QueueEffect: `start` runs a turn, `interrupt` stops it. */
function Simulated({ turnMs, failFirst }: Args) {
  const [entries, setEntries] = useState<Entry[]>([]);
  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);
  const running = useRef<string | null>(null);
  const runs = useRef(0);
  const queueRef = useRef<ReturnType<typeof useQueue> | null>(null);

  const patch = (id: string, change: Partial<Turn>) =>
    setEntries((list) =>
      list.map((e) => (e.id === id ? { ...e, turn: { ...e.turn, ...change } } : e)),
    );
  const later = (ms: number, fn: () => void) => timers.current.push(setTimeout(fn, ms));

  const start = (m: QueuedMessage) => {
    const id = `run-${m.id}`;
    const fail = failFirst && runs.current === 0;
    runs.current += 1;
    running.current = id;
    setEntries((list) => [
      ...list,
      {
        id,
        text: m.text,
        turn: { ...emptyTurn(id, fx.doneTurn.agent), status: "working", activity: "thinking" },
      },
    ]);
    later(turnMs * 0.25, () =>
      patch(id, {
        activity: "searching",
        tasks: [{ id: "s", title: "Searching the runbook", status: "in_progress" }],
      }),
    );
    later(turnMs * 0.55, () =>
      patch(id, {
        activity: "composing",
        tasks: [{ id: "s", title: "Searched the runbook", status: "complete" }],
        text: `Looking into "${m.text}"…`,
      }),
    );
    later(turnMs, () => {
      running.current = null;
      if (fail) {
        patch(id, { status: "failed", failure: fx.failures.unavailable, activity: undefined });
        queueRef.current?.turnEnded("failed");
      } else {
        patch(id, {
          status: "done",
          text: `Done: "${m.text}".`,
          activity: undefined,
          durationMs: turnMs,
        });
        queueRef.current?.turnEnded("done");
      }
    });
  };

  const interrupt = () => {
    for (const t of timers.current) clearTimeout(t);
    timers.current = [];
    const id = running.current;
    running.current = null;
    if (id) patch(id, { status: "stopped", activity: undefined });
    setTimeout(() => queueRef.current?.turnEnded("stopped"), 150);
  };

  const queue = useQueue({ onStart: start, onInterrupt: interrupt });
  queueRef.current = queue;
  const current = entries.at(-1)?.turn;

  return (
    <div className="mx-auto flex h-[40rem] w-full max-w-3xl flex-col">
      <WebSurface className="min-h-0 flex-1">
        <Thread className="min-h-0 flex-1" busy={queue.running}>
          {entries.length === 0 && (
            <p className="py-10 text-center text-aui-muted text-aui-sm">
              Send a message, then keep typing while it runs.
            </p>
          )}
          {entries.map((e) => (
            <div key={e.id} className="flex flex-col gap-4">
              <UserMessage text={e.text} />
              <AgentMessage turn={e.turn} />
            </div>
          ))}
        </Thread>
      </WebSurface>
      <div className="pt-2">
        <PromptInput
          queue={queue}
          autoFocus
          footer={
            <StatusLine
              className="border-aui-border border-t px-1 pt-1"
              session={{
                ...fx.sessions.busy,
                queued: queue.state.pending.length,
                status: queue.running ? "working" : "idle",
                activity: queue.running ? (current?.activity ?? "thinking") : "idle",
              }}
            />
          }
        />
      </div>
    </div>
  );
}

const meta = {
  title: "Web-only/Queue",
  tags: ["web-only"],
  parameters: {
    ...webOnly(SLACK),
    docs: {
      description: {
        component: `The composer queue (useQueue over core queueReduce) against a simulated runtime. While a turn runs, Enter queues; Cmd/Ctrl+Enter sends now (interrupts and runs next); Esc stops. Queued messages can be edited, reordered (drag or Alt+Up/Down), removed, or sent now. After a failure or a Stop, the queue holds until you Resume. Web only. Slack uses ${SLACK}.`,
      },
    },
  },
  args: { turnMs: 1600, failFirst: false },
  render: (args) => <Simulated {...args} />,
} satisfies Meta<Args>;

export default meta;
type Story = StoryObj<typeof meta>;

export const QueueWhileRunning: Story = {
  name: "Queue while running",
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const input = canvas.getByRole("textbox", { name: "Message" });
    await userEvent.type(input, "Check payments-api{Enter}");
    await expect(await canvas.findByText("Check payments-api")).toBeInTheDocument();
    await userEvent.type(input, "Then draft a status update{Enter}");
    const list = await canvas.findByRole("region", { name: "Queued messages" });
    await expect(within(list).getByText("Then draft a status update")).toBeInTheDocument();
    // Auto mode: it runs on its own once the first turn ends.
    await waitFor(
      () => expect(canvas.queryByRole("region", { name: "Queued messages" })).toBeNull(),
      { timeout: 4000 },
    );
    await expect(canvas.getAllByText(/Then draft a status update/).length).toBeGreaterThan(0);
  },
};

export const SendNow: Story = {
  name: "Send now interrupts",
  args: { turnMs: 4000 },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const input = canvas.getByRole("textbox", { name: "Message" });
    await userEvent.type(input, "Summarize the week{Enter}");
    await expect(await canvas.findByText("Summarize the week")).toBeInTheDocument();
    await userEvent.type(input, "Actually, page on-call");
    await userEvent.keyboard("{Meta>}{Enter}{/Meta}");
    await expect(await canvas.findByText("Actually, page on-call")).toBeInTheDocument();
    await waitFor(() => expect(canvas.getAllByText("Stopped").length).toBeGreaterThan(0));
  },
};

export const ResumeAfterFailure: Story = {
  name: "Held after a failure, then Resume",
  args: { failFirst: true, turnMs: 1200 },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const input = canvas.getByRole("textbox", { name: "Message" });
    await userEvent.type(input, "Roll back #4812{Enter}");
    await userEvent.type(input, "Post the summary{Enter}");
    await expect(
      await canvas.findByText("Paused after a failure", {}, { timeout: 4000 }),
    ).toBeInTheDocument();
    // Never runs queued work after a failure without the user saying so.
    await expect(canvas.queryAllByRole("article", { name: "You message" })).toHaveLength(1);
    await userEvent.click(canvas.getByRole("button", { name: "Resume" }));
    await waitFor(() =>
      expect(canvas.getAllByRole("article", { name: "You message" })).toHaveLength(2),
    );
  },
};
