import { emptyTurn, type Turn } from "@agent-ui/core";
import * as fx from "@agent-ui/core/fixtures";
import { AgentMessage, UserMessage } from "@agent-ui/web";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { fn } from "storybook/test";
import { type ActionArgs, Canvas, reasoningText, slack } from "../../support";

type Args = ActionArgs & { turn: Turn; question: string };

const meta = {
  title: "Agent/Turn states",
  tags: ["slack"],
  parameters: {
    docs: {
      description: {
        component:
          "One agent turn as the web draws it: AgentMessage (avatar, name, live status, reasoning) around the shared TurnView. Slack renders the same TurnView to Block Kit; its status is a reaction plus the assistant status, and reasoning is web only.",
      },
    },
  },
  args: { onAction: fn(), turn: fx.doneTurn, question: "Why is payments-api returning 5xx?" },
  argTypes: { turn: { control: "object" } },
  render: ({ onAction, turn, question }) => (
    <Canvas onAction={onAction}>
      <div className="flex flex-col gap-6">
        {question && <UserMessage text={question} />}
        <AgentMessage turn={turn} />
      </div>
    </Canvas>
  ),
} satisfies Meta<Args>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Queued: Story = {
  args: { turn: { ...emptyTurn("q", { name: "Assistant" }), status: "queued" } },
  parameters: {
    docs: {
      description: {
        story:
          "Slack: no fixture; a queued turn is the assistant status line (`assistant.threads.setStatus`).",
      },
    },
  },
};

export const Thinking: Story = {
  args: {
    turn: {
      ...emptyTurn("t", { name: "Assistant" }),
      status: "working",
      reasoning: reasoningText.slice(0, 140),
    },
  },
  parameters: slack("streamingTurn", "Reasoning is web only"),
};

export const Streaming: Story = {
  args: { turn: fx.streamingTurn },
  parameters: slack("streamingTurn"),
};

export const Waiting: Story = {
  name: "Waiting on approval",
  args: { turn: fx.waitingTurn },
  parameters: slack("waitingTurn"),
};

export const Done: Story = {
  args: { turn: { ...fx.doneTurn, reasoning: reasoningText } },
  parameters: slack("doneTurn", "Reasoning is web only"),
};

export const Failed: Story = {
  args: { turn: fx.failedTurn },
  parameters: slack("failedTurn"),
};

export const Stopped: Story = {
  args: { turn: fx.stoppedTurn },
  parameters: slack("stoppedTurn"),
};

export const Coding: Story = {
  name: "Coding turn",
  args: {
    turn: fx.codingTurn,
    question: "The refund migration is failing in prod. Can you fix it?",
  },
  parameters: slack("codingTurn"),
};

export const DeniedTask: Story = {
  name: "Denied task",
  args: { turn: fx.deniedTaskTurn },
  parameters: slack("deniedTaskTurn", "The denied row is struck and reads Not run"),
};

export const Delegating: Story = {
  name: "Delegating to a sub-agent",
  args: { turn: fx.delegatingTurn },
  parameters: slack("delegatingTurn"),
};

export const Handoff: Story = {
  args: { turn: fx.handoffTurn },
  parameters: slack("handoffTurn"),
};

export const Usage: Story = {
  name: "Done, with usage",
  args: { turn: fx.usageTurn },
  parameters: slack("usageTurn", "The usage tooltip is web only"),
};

export const Stopping: Story = {
  name: "Stop requested",
  args: { turn: fx.stoppingTurn },
  parameters: slack("stoppingTurn"),
};

export const StopFailed: Story = {
  name: "Stop not confirmed",
  args: { turn: fx.stopFailedTurn },
  parameters: slack("stopFailedTurn"),
};

export const StopConfirmed: Story = {
  name: "Stop confirmed (Resume)",
  args: { turn: fx.stopConfirmedTurn },
  parameters: slack("stopConfirmedTurn"),
};

export const QuietNotices: Story = {
  name: "Quiet runtime notices",
  args: {
    turn: {
      ...fx.doneTurn,
      notices: [fx.runtimeNotices.silence, fx.runtimeNotices.fallback, fx.runtimeNotices.retried],
    },
  },
  parameters: slack(
    "runtimeNotices.silence",
    "Fallback and retried are `runtimeNotices.fallback` / `runtimeNotices.retried`",
  ),
};

export const SpendBlocked: Story = {
  name: "Spend blocked notice",
  args: { turn: { ...fx.doneTurn, notices: [fx.runtimeNotices.spend] } },
  parameters: slack("runtimeNotices.spend"),
};
