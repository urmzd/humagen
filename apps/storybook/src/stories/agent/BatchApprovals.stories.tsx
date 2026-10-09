import { ApprovalBatch } from "@agent-ui/components";
import { ActionId } from "@agent-ui/core";
import * as fx from "@agent-ui/core/fixtures";
import { AgentMessage } from "@agent-ui/web";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, within } from "storybook/test";
import { type ActionArgs, Canvas, slack } from "../../support";

const meta = {
  title: "Agent/Batch approvals",
  tags: ["slack"],
  parameters: {
    docs: {
      description: {
        component:
          "Several gated calls at once: one compact card, every item checked by default, and a primary button whose label counts the selection. Unchecked items are denied, never run. The web reads the checklist at click time and sends it as `payload.selection`; Slack reads `state.values`.",
      },
    },
  },
  args: { onAction: fn() },
  render: ({ onAction }) => (
    <Canvas onAction={onAction}>
      <ApprovalBatch turnId="turn-batch" approvals={[fx.pendingApproval, fx.secondApproval]} />
    </Canvas>
  ),
} satisfies Meta<ActionArgs>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  parameters: slack("batchTurn"),
  play: async ({ args, canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.getByRole("button", { name: "Approve 2 of 2" })).toBeEnabled();
    await userEvent.click(canvas.getByRole("checkbox", { name: /Post the incident summary/ }));
    const approve = await canvas.findByRole("button", { name: "Approve 1 of 2" });
    await userEvent.click(approve);
    await expect(args.onAction).toHaveBeenCalledWith({
      id: ActionId.approveSelected,
      payload: { turnId: "turn-batch", target: "batch:turn-batch", selection: ["a1"] },
    });
  },
};

export const NoneSelected: Story = {
  name: "Select none disables approve",
  parameters: slack("batchTurn"),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Select none" }));
    await expect(canvas.getByRole("button", { name: "Approve 0 of 2" })).toBeDisabled();
    await userEvent.click(canvas.getByRole("button", { name: "Select all" }));
    await expect(canvas.getByRole("button", { name: "Approve 2 of 2" })).toBeEnabled();
  },
};

export const InTurn: Story = {
  name: "In a turn",
  render: ({ onAction }) => (
    <Canvas onAction={onAction}>
      <AgentMessage turn={fx.batchTurn} />
    </Canvas>
  ),
  parameters: slack("batchTurn", "Waiting rows hold still (hourglass), never spin"),
};
