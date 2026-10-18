import type { Meta, StoryObj } from "@storybook/react-vite";
import { ClarificationCard } from "@warp/components";
import { ActionId, type Clarification } from "@warp/core";
import * as fx from "@warp/core/fixtures";
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

export const WithUnsure: Story = {
  name: "Pending, with I'm Not Sure",
  args: { clarification: { ...fx.pendingClarification, allowUnsure: true } },
  parameters: slack("pendingClarification"),
};

export const SomethingElse: Story = {
  name: "Something Else (inline text)",
  parameters: slack("pendingClarification", "Slack opens a modal"),
  play: async ({ args, canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Something Else" }));
    await userEvent.type(
      await canvas.findByRole("textbox", { name: /Tell me what you need/ }),
      "Check canary{Enter}",
    );
    await expect(args.onAction).toHaveBeenCalledWith({
      id: ActionId.clarifyOther,
      payload: { turnId: "turn-1", target: "q1", option: "Check canary" },
    });
  },
};

export const Unsure: Story = {
  name: "Answered: not sure",
  args: { clarification: fx.unsureClarification },
  parameters: slack("unsureClarification"),
};
