import { ApprovalCard, ClarificationCard } from "@agent-ui/components";
import * as fx from "@agent-ui/core/fixtures";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { Dual } from "../Dual";

const meta = {
  title: "Agent/Interrupts",
  parameters: {
    docs: { description: { component: "Human-in-the-loop cards and their resolved states." } },
  },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

export const ApprovalPending: Story = {
  render: () => (
    <Dual>
      <ApprovalCard turnId="turn-1" approval={fx.pendingApproval} />
    </Dual>
  ),
};

export const ApprovalApproved: Story = {
  render: () => (
    <Dual>
      <ApprovalCard turnId="turn-1" approval={fx.approvedApproval} />
    </Dual>
  ),
};

export const ApprovalDenied: Story = {
  render: () => (
    <Dual>
      <ApprovalCard turnId="turn-1" approval={fx.deniedApproval} />
    </Dual>
  ),
};

export const ClarificationPending: Story = {
  render: () => (
    <Dual>
      <ClarificationCard turnId="turn-1" clarification={fx.pendingClarification} />
    </Dual>
  ),
};

export const ClarificationAnswered: Story = {
  render: () => (
    <Dual>
      <ClarificationCard turnId="turn-1" clarification={fx.answeredClarification} />
    </Dual>
  ),
};

export const ApprovalTimedOut: Story = {
  render: () => (
    <Dual>
      <ApprovalCard turnId="turn-1" approval={fx.expiredApproval} />
    </Dual>
  ),
};
