import { AttachmentView } from "@agent-ui/components";
import type { Attachment, CommandRun } from "@agent-ui/core";
import * as fx from "@agent-ui/core/fixtures";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { useEffect, useState } from "react";
import { expect, userEvent, within } from "storybook/test";
import { Canvas, slack } from "../../support";

const meta = {
  title: "Agent/Code and terminal",
  tags: ["slack"],
  parameters: {
    docs: {
      description: {
        component:
          "Commands the agent ran and code it produced. Terminals keep the tail and say how much was cut; both copy in one click.",
      },
    },
  },
  args: { attachment: fx.commandOk as Attachment },
  render: ({ attachment }) => (
    <Canvas>
      <AttachmentView attachment={attachment} />
    </Canvas>
  ),
} satisfies Meta<{ attachment: Attachment }>;

export default meta;
type Story = StoryObj<typeof meta>;

export const CommandPassed: Story = {
  name: "Command passed",
  args: { attachment: fx.commandOk },
  parameters: slack("commandOk"),
};

export const CommandFailed: Story = {
  name: "Command failed",
  args: { attachment: fx.commandFailed },
  parameters: slack("commandFailed"),
};

export const CommandRunning: Story = {
  name: "Command running (tail)",
  args: { attachment: fx.commandRunning },
  parameters: slack("commandRunning"),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: /68 earlier lines/ }));
    await expect(canvas.getByText(/#1 \[build 1\/80\]/)).toBeInTheDocument();
  },
};

const buildLines = (fx.commandRunning.output ?? "").split("\n");

function LiveCommand() {
  const [n, setN] = useState(1);
  useEffect(() => {
    const t = setInterval(() => setN((x) => (x >= buildLines.length + 8 ? 1 : x + 1)), 120);
    return () => clearInterval(t);
  }, []);
  const done = n > buildLines.length;
  const run: CommandRun = {
    ...fx.commandRunning,
    status: done ? "complete" : "in_progress",
    exitCode: done ? 0 : undefined,
    durationMs: done ? 9_600 : undefined,
    output: buildLines.slice(0, Math.min(n, buildLines.length)).join("\n"),
  };
  return (
    <Canvas>
      <AttachmentView attachment={run} />
    </Canvas>
  );
}

export const CommandLive: Story = {
  name: "Command, live output",
  parameters: slack("commandRunning"),
  render: () => <LiveCommand />,
};

export const Snippet: Story = {
  args: { attachment: fx.snippet },
  parameters: slack("snippet"),
};
