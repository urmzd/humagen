import type { Meta, StoryObj } from "@storybook/react-vite";
import { NoticeView } from "@warp/components";
import type { Notice } from "@warp/core";
import * as fx from "@warp/core/fixtures";
import { fn } from "storybook/test";
import { type ActionArgs, Canvas, slack } from "../../support";

type Args = ActionArgs & { notice: Notice };

const meta = {
  title: "Agent/Notices",
  tags: ["slack"],
  parameters: {
    docs: {
      description: {
        component:
          "Out-of-band messages that name the cause and the fix. Recovery actions sit inside the callout.",
      },
    },
  },
  args: { onAction: fn(), notice: fx.notices.slow },
  render: ({ onAction, notice }) => (
    <Canvas onAction={onAction}>
      <NoticeView notice={notice} />
    </Canvas>
  ),
} satisfies Meta<Args>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Slow: Story = { args: { notice: fx.notices.slow }, parameters: slack("notices.slow") };
export const ErrorNotice: Story = {
  name: "Error",
  args: { notice: fx.notices.error },
  parameters: slack("notices.error"),
};
export const Access: Story = {
  args: { notice: fx.notices.access },
  parameters: slack("notices.access"),
};
export const Done: Story = { args: { notice: fx.notices.done }, parameters: slack("notices.done") };
