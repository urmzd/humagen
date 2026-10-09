import { ActionId, type Rating } from "@agent-ui/core";
import { Feedback } from "@agent-ui/primitives";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, within } from "storybook/test";
import { type ActionArgs, Canvas, slackPrimitive } from "../../support";

const up = { id: ActionId.feedback, payload: { turnId: "turn-1", target: "up" } };
const down = { id: ActionId.feedback, payload: { turnId: "turn-1", target: "down" } };

const meta = {
  title: "Primitives/Feedback",
  tags: ["slack"],
  parameters: slackPrimitive("a context_actions block with feedback_buttons"),
  args: { onAction: fn(), selected: undefined },
  argTypes: { selected: { control: "inline-radio", options: [undefined, "up", "down"] } },
  render: ({ onAction, selected }) => (
    <Canvas onAction={onAction}>
      <Feedback
        up={up}
        down={down}
        selected={selected}
        labels={{ up: "Helpful", down: "Not helpful" }}
      />
    </Canvas>
  ),
} satisfies Meta<ActionArgs & { selected?: Rating }>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Unrated: Story = {
  play: async ({ args, canvasElement }) => {
    await userEvent.click(within(canvasElement).getByRole("button", { name: "Helpful" }));
    await expect(args.onAction).toHaveBeenCalledWith(up);
  },
};

export const RatedUp: Story = { args: { selected: "up" } };
