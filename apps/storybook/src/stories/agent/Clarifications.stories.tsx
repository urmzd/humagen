import { ClarificationCard } from "@agent-ui/components";
import { ActionId, type Clarification } from "@agent-ui/core";
import * as fx from "@agent-ui/core/fixtures";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, within } from "storybook/test";
import { type ActionArgs, Canvas, slack } from "../../support";

type Args = ActionArgs & { clarification: Clarification };

const meta = {
  title: "Agent/Clarifications",
  tags: ["slack"],
  parameters: {
    docs: {
      description: {
        component:
          "The agent needs a choice before it continues: two to four unstyled options, each with a line of context, plus a way out.",
      },
    },
  },
  args: { onAction: fn(), clarification: fx.pendingClarification },
  render: ({ onAction, clarification }) => (
    <Canvas onAction={onAction}>
      <ClarificationCard turnId="turn-1" clarification={clarification} />
    </Canvas>
  ),
} satisfies Meta<Args>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Pending: Story = {
  parameters: slack("pendingClarification"),
  play: async ({ args, canvasElement }) => {
    await userEvent.click(within(canvasElement).getByRole("button", { name: "Check Production" }));
    await expect(args.onAction).toHaveBeenCalledWith({
      id: ActionId.clarify,
      payload: { turnId: "turn-1", target: "q1", option: "prod" },
    });
  },
};

export const Answered: Story = {
  args: { clarification: fx.answeredClarification },
  parameters: slack("answeredClarification"),
};
