import { ActionId } from "@agent-ui/core";
import { Actions, Button } from "@agent-ui/primitives";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, within } from "storybook/test";
import { type ActionArgs, Canvas, slackPrimitive } from "../../support";

const action = { id: ActionId.approve, payload: { turnId: "turn-1", target: "a1" } };
const confirm = {
  title: "Are you sure?",
  text: "This can't be undone.",
  confirm: "Yes, Continue",
  deny: "Go Back",
};

type Args = ActionArgs & { label: string; style?: "primary" | "danger"; withConfirm: boolean };

const meta = {
  title: "Primitives/Button",
  tags: ["slack"],
  parameters: slackPrimitive(
    "a button element in an actions block; `confirm` becomes Slack's native confirmation dialog",
  ),
  args: { onAction: fn(), label: "Roll Back", style: "primary", withConfirm: false },
  argTypes: {
    style: { control: "inline-radio", options: [undefined, "primary", "danger"] },
  },
  render: ({ onAction, label, style, withConfirm }) => (
    <Canvas onAction={onAction}>
      <Actions blockId="demo">
        <Button
          label={label}
          style={style}
          action={action}
          confirm={withConfirm ? confirm : undefined}
        />
      </Actions>
    </Canvas>
  ),
} satisfies Meta<Args>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Playground: Story = {};

export const Variants: Story = {
  render: ({ onAction }) => (
    <Canvas onAction={onAction}>
      <Actions blockId="demo">
        <Button label="Send Summary" style="primary" action={action} />
        <Button label="Delete Branch" style="danger" action={action} />
        <Button label="Edit" action={action} />
        <Button label="Open Runbook" url="https://example.com/runbooks/payments" />
      </Actions>
    </Canvas>
  ),
};

export const ConfirmStep: Story = {
  name: "Confirm step",
  args: { style: "danger", label: "Delete Branch", withConfirm: true },
  render: ({ onAction, label, style }) => (
    <Canvas onAction={onAction}>
      <Actions blockId="demo">
        <Button label={label} style={style} action={action} confirm={confirm} />
        <Button label="Cancel" action={{ ...action, id: ActionId.deny }} />
      </Actions>
    </Canvas>
  ),
  play: async ({ args, canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Delete Branch" }));
    const dialog = await canvas.findByRole("alertdialog");
    // Focus lands on the safe choice; Escape backs out.
    await expect(within(dialog).getByRole("button", { name: "Go Back" })).toHaveFocus();
    await userEvent.keyboard("{Escape}");
    await expect(canvas.queryByRole("alertdialog")).toBeNull();
    await expect(args.onAction).not.toHaveBeenCalled();
    await userEvent.click(canvas.getByRole("button", { name: "Delete Branch" }));
    await userEvent.click(await canvas.findByRole("button", { name: "Yes, Continue" }));
    await expect(args.onAction).toHaveBeenCalledWith(action);
  },
};
