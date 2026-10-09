import { AttachmentView } from "@agent-ui/components";
import * as fx from "@agent-ui/core/fixtures";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { Dual } from "../Dual";

const meta = {
  title: "Agent/Code and terminal",
  tags: ["web", "slack"],
  parameters: {
    docs: {
      description: {
        component:
          "Commands and code from coding agents. Slack keeps the last 40 lines and says how many were cut; the web keeps the tail and can expand.",
      },
    },
  },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

export const CommandPassed: Story = {
  render: () => (
    <Dual>
      <AttachmentView attachment={fx.commandOk} />
    </Dual>
  ),
};

export const CommandFailed: Story = {
  render: () => (
    <Dual>
      <AttachmentView attachment={fx.commandFailed} />
    </Dual>
  ),
};

export const CommandRunningLongOutput: Story = {
  render: () => (
    <Dual>
      <AttachmentView attachment={fx.commandRunning} />
    </Dual>
  ),
};

export const CodeSnippet: Story = {
  render: () => (
    <Dual>
      <AttachmentView attachment={fx.snippet} />
    </Dual>
  ),
};
