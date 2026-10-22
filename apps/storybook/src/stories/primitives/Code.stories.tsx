import type { TaskStatus } from "@humagen/core";
import * as fx from "@humagen/core/fixtures";
import { Code, Terminal } from "@humagen/primitives";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { Canvas, slackPrimitive } from "../../support";

type Args = { command: string; status: TaskStatus; exitCode?: number; output: string };

const meta = {
  title: "Primitives/Code and Terminal",
  tags: ["slack"],
  parameters: slackPrimitive(
    "a rich_text_preformatted block; Terminal keeps the tail and says how many lines were cut",
  ),
  args: {
    command: fx.commandFailed.command,
    status: "error",
    exitCode: 1,
    output: fx.commandFailed.output ?? "",
  },
  argTypes: {
    status: { control: "inline-radio", options: ["pending", "in_progress", "complete", "error"] },
  },
  render: (args) => (
    <Canvas>
      <Terminal {...args} cwd="~/src/platform" durationMs={2100} />
    </Canvas>
  ),
} satisfies Meta<Args>;

export default meta;
type Story = StoryObj<typeof meta>;

export const TerminalPlayground: Story = { name: "Terminal" };

export const LongOutput: Story = {
  name: "Terminal, long output (tail)",
  render: () => (
    <Canvas>
      <Terminal {...fx.commandRunning} />
    </Canvas>
  ),
};

export const CodeWithPath: Story = {
  name: "Code",
  render: () => (
    <Canvas>
      <Code text={fx.snippet.code} language={fx.snippet.language} title={fx.snippet.path} />
      <Code text={"SELECT id, reason FROM refunds WHERE reason IS NULL;"} language="sql" />
    </Canvas>
  ),
};

export const CodeLong: Story = {
  name: "Code, long (folded)",
  render: () => (
    <Canvas>
      <Code
        title="services/payments-api/src/handlers.ts"
        language="ts"
        text={Array.from({ length: 40 }, (_, i) => `export const handler${i} = () => ${i};`).join(
          "\n",
        )}
      />
    </Canvas>
  ),
};
