import type { Meta, StoryObj } from "@storybook/react-vite";
import { ActionId, type TaskStatus, type UiAction } from "@warp/core";
import { Plan, Task } from "@warp/primitives";
import { useState } from "react";
import { expect, fn, userEvent, waitFor, within } from "storybook/test";
import { type ActionArgs, Canvas } from "../../support";

const payload = { turnId: "turn-1", target: "a1" };
const ID = { checked: "t1", gated: "g1", next: "g2" };

function Gated({ onAction }: ActionArgs) {
  const [status, setStatus] = useState<TaskStatus>("waiting");
  const [record, setRecord] = useState<string | undefined>();
  const handle = (action: UiAction) => {
    onAction(action);
    if (action.id === ActionId.deny) {
      setStatus("denied");
      setRecord("You said no · just now");
    } else {
      setStatus("complete");
      setRecord(
        action.id === ActionId.remember
          ? "Approved, and always allowed from now on · just now"
          : action.id === ActionId.approveAll
            ? "Approved with the rest · just now"
            : "Approved by You · just now",
      );
    }
  };
  return (
    <Canvas onAction={handle}>
      <Plan title="Rolling back payments-api">
        <Task
          id={ID.checked}
          title="Checked recent deploys"
          status="complete"
          output="3 deploys today"
        />
        <Task
          id={ID.gated}
          title="Roll back payments-api to #4811"
          status={status}
          output={record}
          decision={
            status === "waiting"
              ? {
                  approve: {
                    label: "Roll Back",
                    action: { id: ActionId.approve, payload },
                    style: "primary",
                  },
                  decline: { label: "Decline", action: { id: ActionId.deny, payload } },
                  menu: [
                    {
                      label: "Approve All",
                      description: "Also run the other 2 waiting steps",
                      action: { id: ActionId.approveAll, payload },
                    },
                    {
                      label: "Always Allow deploys.rollback",
                      description: "Don't ask again in this workspace",
                      action: { id: ActionId.remember, payload },
                    },
                  ],
                }
              : undefined
          }
        />
        <Task id={ID.next} title="Post incident summary" status="pending" />
      </Plan>
    </Canvas>
  );
}

const meta = {
  title: "Agent/Approvals inline on task rows",
  tags: ["slack"],
  parameters: {
    docs: {
      description: {
        component:
          "A gated tool row holds its own options: a compact Approve split button (chevron menu: Approve All, Always Allow) and Decline. On a decision the same row updates in place to a one-line record. Slack plans can't hold controls, so Slack renders the approval card instead (`pnpm sandbox post batchTurn`).",
      },
    },
  },
  args: { onAction: fn() },
  render: (args) => <Gated {...args} />,
} satisfies Meta<ActionArgs>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Approve: Story = {
  play: async ({ args, canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: /^Roll Back:/ }));
    await expect(args.onAction).toHaveBeenCalledWith({ id: ActionId.approve, payload });
    await expect((await canvas.findAllByText("Approved by You · just now")).length).toBeGreaterThan(
      0,
    );
  },
};

export const SplitMenu: Story = {
  name: "Split menu (keyboard)",
  play: async ({ args, canvasElement }) => {
    const canvas = within(canvasElement);
    const chevron = canvas.getByRole("button", { name: /More options for Roll Back/ });
    chevron.focus();
    await userEvent.keyboard("{ArrowDown}");
    const menu = await canvas.findByRole("menu");
    await waitFor(() =>
      expect(within(menu).getByRole("menuitem", { name: /Approve All/ })).toHaveFocus(),
    );
    await userEvent.keyboard("{ArrowDown}");
    await expect(within(menu).getByRole("menuitem", { name: /Always Allow/ })).toHaveFocus();
    await userEvent.keyboard("{Enter}");
    await expect(args.onAction).toHaveBeenCalledWith({ id: ActionId.remember, payload });
  },
};

export const Decline: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: /^Decline:/ }));
    await expect((await canvas.findAllByText("You said no · just now")).length).toBeGreaterThan(0);
  },
};
