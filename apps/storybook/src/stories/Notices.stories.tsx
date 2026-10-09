import { NoticeView } from "@agent-ui/components";
import * as fx from "@agent-ui/core/fixtures";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { Dual } from "../Dual";

const meta = {
  title: "Agent/Notices",
  tags: ["web", "slack"],
  parameters: {
    docs: {
      description: {
        component:
          "Slack has an `alert` block only in modals. In messages the same Callout degrades to a framed section; switch placement to compare.",
      },
    },
  },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

const all = (
  <>
    {Object.values(fx.notices).map((n) => (
      <NoticeView key={n.id} notice={n} />
    ))}
  </>
);

export const InMessage: Story = { render: () => <Dual>{all}</Dual> };
export const InModal: Story = { render: () => <Dual placement="modal">{all}</Dual> };
