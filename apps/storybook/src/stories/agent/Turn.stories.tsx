import type { Meta, StoryObj } from "@storybook/react-vite";
import { emptyTurn, type Turn } from "@vultus/core";
import * as fx from "@vultus/core/fixtures";
import { CopyIcon, type TurnItem } from "@vultus/render-web";
import { AgentMessage, UserMessage } from "@vultus/web";
import { expect, fn, within } from "storybook/test";
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

export const StillWorking: Story = {
  name: "Still working, live output closed",
  args: { turn: fx.quietTurn },
  parameters: slack(
    "quietTurn",
    "The host closed the stream on a long-quiet run and keeps following it. Nothing moves on either surface",
  ),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.getByText(/is still working on this/)).toBeVisible();
    await expect(canvasElement.querySelector("[data-aui-still]")).not.toBeNull();
  },
};

export const Rated: Story = {
  name: "Done, already rated",
  args: { turn: fx.ratedTurn },
  parameters: slack(
    "ratedTurn",
    "Slack's rating buttons keep no pressed state, so a line under them says the rating",
  ),
  play: async ({ canvasElement }) => {
    const pressed = within(canvasElement).getByRole("button", { name: "Helpful" });
    await expect(pressed).toHaveAttribute("aria-pressed", "true");
  },
};

export const NoFeedback: Story = {
  name: "Done, host keeps no ratings",
  args: { turn: fx.doneTurn },
  render: ({ onAction, turn, question }) => (
    <Canvas onAction={onAction}>
      <div className="flex flex-col gap-6">
        {question && <UserMessage text={question} />}
        <AgentMessage turn={turn} capabilities={{ feedback: false }} />
      </div>
    </Canvas>
  ),
  parameters: slack("doneTurn.noFeedback", "`capabilities={{ feedback: false }}` on both surfaces"),
  play: async ({ canvasElement }) => {
    await expect(within(canvasElement).queryByRole("button", { name: "Helpful" })).toBeNull();
  },
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

export const Long: Story = {
  name: "Long answer",
  args: { turn: fx.longTurn, question: "List every payments-api deploy this quarter." },
  parameters: slack("longTurn", "Continues across messages when it outgrows one"),
};

export const SkillApproval: Story = {
  name: "Approve a skill",
  args: { turn: fx.skillApprovalTurn },
  parameters: slack("skillApprovalTurn"),
};

export const PromptApproval: Story = {
  name: "Approve a prompt change",
  args: { turn: fx.promptApprovalTurn },
  parameters: slack("promptApprovalTurn"),
};

export const Clarification: Story = {
  name: "Clarification",
  args: { turn: fx.clarificationTurn },
  parameters: slack("clarificationTurn"),
};

export const ArtifactApproval: Story = {
  name: "Approve a file",
  args: { turn: fx.artifactApprovalTurn },
  parameters: slack("artifactApprovalTurn"),
};

export const ArtifactProgress: Story = {
  name: "Files in progress",
  args: { turn: fx.artifactProgressTurn },
  parameters: slack("artifactProgressTurn"),
};

export const ArtifactFailed: Story = {
  name: "File failed",
  args: { turn: fx.artifactFailedTurn },
  parameters: slack("artifactFailedTurn"),
};

export const ArtifactRevised: Story = {
  name: "File revised",
  args: { turn: fx.artifactRevisedTurn },
  parameters: slack("artifactRevisedTurn"),
};

export const References: Story = {
  name: "References",
  args: { turn: fx.referencesTurn },
  parameters: slack("referencesTurn"),
};

export const RuntimeNotices: Story = {
  name: "Runtime notices",
  args: { turn: fx.noticesTurn },
  parameters: slack("noticesTurn"),
};

export const Reconnect: Story = {
  name: "Reconnect needed",
  args: { turn: fx.reconnectTurn },
  parameters: slack("reconnectTurn"),
};

export const RateLimited: Story = {
  name: "Rate limited",
  args: { turn: fx.rateLimitedTurn },
  parameters: slack("rateLimitedTurn"),
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

/** Stands in for a host's "Copy as" menu: one control per item, told which item it is on. */
function CopyAs({ item }: { item: TurnItem }) {
  const what = item.kind === "answer" ? "answer" : (item.task?.title ?? item.kind);
  return (
    <button
      type="button"
      aria-label={`Copy as, ${what}`}
      title="Copy as"
      className="inline-flex size-6 items-center justify-center rounded-aui-sm text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
      onClick={() => navigator.clipboard?.writeText(JSON.stringify(item.task ?? item.turn.text))}
    >
      <CopyIcon size={13} />
    </button>
  );
}

export const ItemActions: Story = {
  name: "Host controls on each item",
  args: { turn: { ...fx.delegatingTurn, status: "done" } },
  render: ({ onAction, turn, question }) => (
    <Canvas onAction={onAction}>
      <div className="flex flex-col gap-6">
        {question && <UserMessage text={question} />}
        <AgentMessage turn={turn} itemActions={(item) => <CopyAs item={item} />} />
      </div>
    </Canvas>
  ),
  parameters: slack("delegatingTurn", "Web only: Slack has no clipboard to offer"),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    // One control per tool call and per agent step, plus the answer's; all reachable without hover.
    await expect(canvas.getAllByRole("button", { name: /^Copy as, / })).toHaveLength(5);
  },
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
