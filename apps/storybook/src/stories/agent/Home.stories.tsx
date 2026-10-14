import type { Meta, StoryObj } from "@storybook/react-vite";
import { Home } from "@vultus/components";
import { ActionId, HOME_ID, type HomeModel } from "@vultus/core";
import * as fx from "@vultus/core/fixtures";
import { expect, fn, userEvent, within } from "storybook/test";
import { type ActionArgs, Canvas, slack } from "../../support";

type Args = ActionArgs & { home: HomeModel };

const meta = {
  title: "Agent/Home",
  tags: ["slack"],
  parameters: {
    docs: {
      description: {
        component:
          "Where a person lands: a greeting, ways to start, what needs them, who answers them, connections and access. One `Home` component: the web gets the ask box and a grid of starts; Slack publishes the same model as the App Home tab with `homeView`, with three buttons, a select for who answers direct messages, and access folded into one line.",
      },
    },
  },
  args: { onAction: fn(), home: fx.home },
  render: ({ onAction, home }) => (
    <Canvas onAction={onAction}>
      <Home home={home} />
    </Canvas>
  ),
} satisfies Meta<Args>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: { home: fx.homes.default },
  parameters: slack("homes.default@home", "Published to App Home"),
  play: async ({ args, canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.type(canvas.getByLabelText("What should we work on?"), "Why did it fail?");
    await userEvent.click(canvas.getByRole("button", { name: "Ask" }));
    await expect(args.onAction).toHaveBeenCalledWith({
      id: ActionId.homeAsk,
      payload: { turnId: HOME_ID, target: "ask", text: "Why did it fail?" },
    });
    await userEvent.selectOptions(canvas.getByLabelText("Answered by"), "triage");
    await expect(args.onAction).toHaveBeenCalledWith({
      id: ActionId.homeAnsweredBy,
      payload: { turnId: HOME_ID, target: "answered_by", option: "triage" },
    });
  },
};

export const NothingWaiting: Story = {
  args: { home: fx.homes.empty },
  parameters: slack("homes.empty@home", "Published to App Home"),
};

export const CannotConnect: Story = {
  args: { home: fx.homes.noConnect },
  parameters: slack("homes.noConnect@home", "Published to App Home"),
};

export const NoGrants: Story = {
  args: { home: fx.homes.noGrants },
  parameters: slack("homes.noGrants@home", "Published to App Home"),
};
