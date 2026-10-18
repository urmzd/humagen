import type { Meta, StoryObj } from "@storybook/react-vite";
import { ActionId, type Rating } from "@warp/core";
import { Feedback } from "@warp/primitives";
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
    // The rating goes out on the click, before anything is asked.
    await expect(args.onAction).toHaveBeenCalledWith(up);
    await expect(within(canvasElement).getByRole("dialog")).toBeInTheDocument();
  },
};

export const RatedUp: Story = { args: { selected: "up" } };

export const SkippedComment: Story = {
  name: "Rating kept when the comment is skipped",
  play: async ({ args, canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Not helpful" }));
    await userEvent.click(canvas.getByRole("button", { name: "Skip" }));
    await expect(canvas.queryByRole("dialog")).not.toBeInTheDocument();
    // One action only: the rating. Skipping added nothing and took nothing away.
    await expect(args.onAction).toHaveBeenCalledTimes(1);
    await expect(args.onAction).toHaveBeenCalledWith(down);
    await expect(canvas.getByRole("button", { name: "Not helpful" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await expect(canvas.getByRole("button", { name: "Not helpful" })).toHaveFocus();
  },
};

export const CommentAdded: Story = {
  name: "Comment and reason added to the rating",
  play: async ({ args, canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Not helpful" }));
    await expect(canvas.getByRole("textbox")).toHaveFocus();
    await userEvent.click(canvas.getByRole("button", { name: "Wrong" }));
    await userEvent.type(canvas.getByRole("textbox"), "It named the wrong deploy");
    await userEvent.click(canvas.getByRole("button", { name: "Send" }));
    await expect(args.onAction).toHaveBeenNthCalledWith(1, down);
    await expect(args.onAction).toHaveBeenNthCalledWith(2, {
      id: ActionId.feedbackComment,
      payload: {
        turnId: "turn-1",
        target: "down",
        text: "It named the wrong deploy",
        option: "wrong",
      },
    });
  },
};

export const EscapeSkips: Story = {
  name: "Escape counts as skip",
  play: async ({ args, canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Helpful" }));
    await userEvent.keyboard("{Escape}");
    await expect(canvas.queryByRole("dialog")).not.toBeInTheDocument();
    await expect(args.onAction).toHaveBeenCalledTimes(1);
  },
};

export const TakenBack: Story = {
  name: "Clicking the selected rating clears it",
  args: { selected: "up" },
  play: async ({ args, canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Helpful" }));
    await expect(args.onAction).toHaveBeenCalledWith({
      id: ActionId.feedback,
      payload: { turnId: "turn-1", target: "clear" },
    });
    await expect(canvas.queryByRole("dialog")).not.toBeInTheDocument();
  },
};
