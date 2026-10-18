import type { Meta, StoryObj } from "@storybook/react-vite";
import { ApprovalCard } from "@warp/components";
import { ActionId, type Approval, type UiAction } from "@warp/core";
import * as fx from "@warp/core/fixtures";
import { useState } from "react";
import { expect, fn, userEvent, within } from "storybook/test";
import { type ActionArgs, Canvas, slack } from "../../support";

type Args = ActionArgs & { approval: Approval };

const meta = {
  title: "Agent/Approvals",
  tags: ["slack"],
  parameters: {
    docs: {
      description: {
        component:
          "A gated side effect: the point first, a preview of exactly what will happen, a specific verb, and a record in place of the controls once decided. Destructive actions confirm first.",
      },
    },
  },
  args: { onAction: fn(), approval: fx.pendingApproval },
  render: ({ onAction, approval }) => (
    <Canvas onAction={onAction}>
      <ApprovalCard turnId="turn-1" approval={approval} />
    </Canvas>
  ),
} satisfies Meta<Args>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Pending: Story = {
  parameters: slack("pendingApproval"),
  play: async ({ args, canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: /^Roll Back:/ }));
    // The confirm step takes over the whole row: no Edit or Cancel beside "Yes, Continue".
    const dialog = await canvas.findByRole("alertdialog");
    await expect(canvas.queryByRole("button", { name: "Edit" })).toBeNull();
    await userEvent.click(within(dialog).getByRole("button", { name: "Yes, Continue" }));
    await expect(args.onAction).toHaveBeenCalledWith({
      id: ActionId.approve,
      payload: { turnId: "turn-1", target: "a1" },
    });
  },
};

function Interactive({ onAction }: ActionArgs) {
  const [approval, setApproval] = useState<Approval>(fx.pendingApproval);
  const handle = (action: UiAction) => {
    onAction(action);
    const at = new Date().toISOString();
    if (action.id === ActionId.approve) {
      setApproval({ ...approval, decision: { kind: "approved", by: "You", at } });
    } else if (action.id === ActionId.deny) {
      setApproval({ ...approval, decision: { kind: "denied", by: "You", at } });
    }
  };
  return (
    <Canvas onAction={handle}>
      <ApprovalCard turnId="turn-1" approval={approval} />
    </Canvas>
  );
}

export const ApproveFlow: Story = {
  name: "Approve → confirm → record",
  parameters: slack("pendingApproval", "Then `approvedApproval` for the record"),
  render: ({ onAction }) => <Interactive onAction={onAction} />,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: /^Roll Back:/ }));
    await userEvent.click(await canvas.findByRole("button", { name: "Yes, Continue" }));
    await expect(await canvas.findByText(/Approved by You/)).toBeInTheDocument();
    await expect(canvas.queryByRole("button", { name: /^Roll Back:/ })).toBeNull();
  },
};

export const CancelFlow: Story = {
  name: "Cancel → record",
  parameters: slack("pendingApproval", "Then `deniedApproval` for the record"),
  render: ({ onAction }) => <Interactive onAction={onAction} />,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: /^Cancel:/ }));
    await expect(await canvas.findByText(/Rejected by You/)).toBeInTheDocument();
  },
};

export const NotDestructive: Story = {
  name: "Pending, not destructive",
  args: {
    approval: {
      ...fx.pendingApproval,
      summary: "Send this summary to #eng (212 members)?",
      approveLabel: "Send Summary",
      destructive: false,
      editable: false,
      preview: [
        { label: "Channel", value: "#eng" },
        { label: "Message", value: "payments-api is back to baseline after rolling back #4812." },
      ],
      actingAs: undefined,
    },
  },
  parameters: slack("pendingApproval"),
};

export const Approved: Story = {
  args: { approval: fx.approvedApproval },
  parameters: slack("approvedApproval"),
};

export const Denied: Story = {
  args: { approval: fx.deniedApproval },
  parameters: slack("deniedApproval"),
};

export const TimedOut: Story = {
  name: "Timed out",
  args: { approval: fx.expiredApproval },
  parameters: slack("expiredApproval"),
};

export const AskInline: Story = {
  name: "Ask a question inline",
  parameters: slack("pendingApproval", "Slack opens a modal for the question"),
  play: async ({ args, canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Ask" }));
    const box = await canvas.findByRole("textbox", { name: /Ask a question/ });
    await userEvent.type(box, "Does this undo the config change?{Enter}");
    await expect(args.onAction).toHaveBeenCalledWith({
      id: ActionId.ask,
      payload: { turnId: "turn-1", target: "a1", option: "Does this undo the config change?" },
    });
  },
};

export const AlwaysAllow: Story = {
  name: "Rememberable (Always Allow)",
  args: { approval: fx.secondApproval },
  parameters: slack("secondApproval"),
};

export const Asked: Story = {
  name: "Asked instead of deciding",
  args: { approval: fx.askedApproval },
  parameters: slack("askedApproval"),
};

export const NotInitiator: Story = {
  name: "Viewer is not the initiator",
  args: { approval: { ...fx.pendingApproval, initiator: "dana" } },
  render: ({ onAction, approval }) => (
    <Canvas onAction={onAction}>
      <ApprovalCard turnId="turn-1" approval={approval} viewer="sam" />
    </Canvas>
  ),
  parameters: slack("pendingApproval", "Others see Ask only"),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.queryByRole("button", { name: /^Roll Back/ })).toBeNull();
    await expect(canvas.getByText(/Only the person who started this/)).toBeInTheDocument();
  },
};
