import {
  ActionId,
  emptyTurn,
  type PromptConfig,
  type TurnEvent,
  type UiAction,
} from "@agent-ui/core";
import * as fx from "@agent-ui/core/fixtures";
import { WebSurface } from "@agent-ui/render-web";
import { AgentMessage, PromptInput, Thread, tokenize, UserMessage, useReplay } from "@agent-ui/web";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { useMemo, useState } from "react";
import { expect, fn, userEvent, waitFor, within } from "storybook/test";
import { type ActionArgs, scriptFor, slack } from "../../support";

const prompt: PromptConfig = {
  placeholder: "Ask about an incident, a deploy, or a runbook",
  suggestions: [
    { title: "Why is payments-api erroring?", message: "Why is payments-api returning 5xx?" },
    { title: "Summarize today's deploys", message: "Summarize today's production deploys." },
    { title: "Who's on call?", message: "Who is on call for payments right now?" },
    { title: "Draft a status update", message: "Draft a status update for #incidents." },
  ],
};

const seed = emptyTurn("live", fx.doneTurn.agent);

type Args = ActionArgs & { intervalMs: number; withApproval: boolean };

/** The answer stream, optionally pausing on an approval before the final answer. */
function useScript(withApproval: boolean): TurnEvent[] {
  return useMemo(() => {
    const events = scriptFor(fx.doneTurn);
    if (!withApproval) return [...events, { type: "turn.completed" }];
    const answer = tokenize(fx.waitingTurn.text);
    return [
      ...events.filter((e) => e.type !== "text.delta"),
      ...answer,
      { type: "interrupt.raised", interrupt: fx.pendingApproval },
    ];
  }, [withApproval]);
}

function Conversation({ onAction, intervalMs, withApproval }: Args) {
  const script = useScript(withApproval);
  const { turn, playing, play, stop, push } = useReplay(script, {
    intervalMs,
    autoplay: false,
    seed,
  });
  const [asked, setAsked] = useState<string | null>(null);

  const running = playing && (turn.status === "working" || turn.status === "queued");

  const handle = (action: UiAction) => {
    onAction(action);
    const at = new Date().toISOString();
    if (action.id === ActionId.approve || action.id === ActionId.deny) {
      const approved = action.id === ActionId.approve;
      push({
        type: "interrupt.resolved",
        interrupt: {
          ...fx.pendingApproval,
          decision: approved
            ? { kind: "approved", by: "You", at }
            : { kind: "denied", by: "You", at },
        },
      });
      for (const e of tokenize(
        approved ? "\n\nRolled back. The 5xx rate is dropping." : "\n\nOkay, I left it as is.",
      ))
        push(e);
      push({ type: "turn.completed" });
    } else if (action.id === ActionId.feedback) {
      push({ type: "feedback", rating: action.payload.target === "up" ? "up" : "down" });
    }
  };

  return (
    <div className="mx-auto flex h-[calc(100vh-4rem)] max-h-[56rem] min-h-[32rem] w-full max-w-3xl flex-col">
      <WebSurface onAction={handle} className="min-h-0 flex-1">
        <Thread className="min-h-0 flex-1">
          {!asked && (
            <div className="flex flex-1 flex-col items-center justify-center gap-2 py-16 text-center">
              <h2 className="font-semibold text-aui-xl">How can I help?</h2>
              <p className="text-aui-muted text-aui-sm">
                Incidents, deploys, and runbooks. I'll ask before changing anything.
              </p>
            </div>
          )}
          {asked && <UserMessage text={asked} />}
          {asked && <AgentMessage turn={turn} />}
        </Thread>
      </WebSurface>
      <div className="pt-3">
        <PromptInput
          config={prompt}
          running={running}
          showSuggestions={!asked}
          autoFocus
          onSubmit={(message) => {
            setAsked(message);
            play();
          }}
          onStop={stop}
        />
        <p className="mt-2 text-center text-aui-2xs text-aui-muted">
          AI-generated. Check important answers.
        </p>
      </div>
    </div>
  );
}

const meta = {
  title: "Agent/Streaming conversation",
  tags: ["slack"],
  parameters: {
    docs: {
      description: {
        component:
          "The whole web chat: Thread, UserMessage, AgentMessage streaming from recorded events (useReplay + tokenize), and PromptInput with suggestions and Stop. Slack streams the same events with chat.startStream / appendStream / stopStream and supplies its own composer.",
      },
    },
  },
  args: { onAction: fn(), intervalMs: 45, withApproval: false },
  argTypes: { intervalMs: { control: { type: "range", min: 10, max: 200, step: 5 } } },
  render: (args) => <Conversation {...args} />,
} satisfies Meta<Args>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Answer: Story = {
  parameters: slack("doneTurn", "The sandbox streams it with chat.appendStream"),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: /Why is payments-api erroring/ }));
    await expect(await canvas.findByText("Why is payments-api returning 5xx?")).toBeInTheDocument();
    await waitFor(
      () => expect(canvas.getByRole("button", { name: "Stop generating" })).toBeEnabled(),
      {
        timeout: 3000,
      },
    );
  },
};

export const SubmitAndStop: Story = {
  name: "Submit, then stop",
  parameters: slack("stoppedTurn"),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const input = canvas.getByRole("textbox", { name: "Message" });
    await userEvent.type(input, "Check the payments deploys{Enter}");
    await expect(await canvas.findByText("Check the payments deploys")).toBeInTheDocument();
    const stopButton = await canvas.findByRole(
      "button",
      { name: "Stop generating" },
      { timeout: 3000 },
    );
    await userEvent.click(stopButton);
    await expect(await canvas.findByText("Okay, I stopped.")).toBeInTheDocument();
    await expect(canvas.getByRole("button", { name: "Send message" })).toBeInTheDocument();
  },
};

export const WithApproval: Story = {
  name: "Pauses for approval",
  args: { withApproval: true },
  parameters: slack("waitingTurn"),
};
