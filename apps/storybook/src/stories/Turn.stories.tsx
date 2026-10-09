import { TurnView } from "@agent-ui/components";
import * as fx from "@agent-ui/core/fixtures";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { Dual } from "../Dual";

const meta = {
  title: "Agent/Turn",
  component: TurnView,
  tags: ["web", "slack"],
  render: (args) => (
    <Dual>
      <TurnView {...args} />
    </Dual>
  ),
} satisfies Meta<typeof TurnView>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Working: Story = { args: { turn: fx.streamingTurn } };
export const Done: Story = { args: { turn: fx.doneTurn } };
export const WaitingForApproval: Story = { args: { turn: fx.waitingTurn } };
export const Failed: Story = { args: { turn: fx.failedTurn } };
export const Stopped: Story = { args: { turn: fx.stoppedTurn } };
export const CodingAgent: Story = { args: { turn: fx.codingTurn } };
export const Timeline: Story = {
  name: "Timeline (no plan)",
  args: { turn: { ...fx.doneTurn, planTitle: undefined } },
};
export const WithNotices: Story = {
  args: { turn: { ...fx.doneTurn, notices: [fx.notices.slow, fx.notices.access] } },
};
export const StreamFinalBlocks: Story = {
  name: "Slack stopStream blocks (streamed)",
  parameters: {
    docs: {
      description: {
        story:
          "When the answer was streamed with chat.appendStream, only these blocks go in chat.stopStream: sources, decisions, feedback, footer.",
      },
    },
  },
  args: { turn: fx.doneTurn, streamed: true },
};
