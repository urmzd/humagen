import { defaultCopy, linkCitations } from "@humagen/components";
import * as fx from "@humagen/core/fixtures";
import { Markdown, Sources } from "@humagen/primitives";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, userEvent, within } from "storybook/test";
import { Canvas, slackPrimitive } from "../../support";

const meta = {
  title: "Primitives/Markdown",
  tags: ["slack"],
  parameters: slackPrimitive(
    "a markdown block; while streaming, chat.appendStream carries the same text and Slack draws its own caret",
  ),
  args: { text: linkCitations(fx.doneTurn.text, fx.doneTurn.citations), streaming: false },
  render: ({ text, streaming }) => (
    <Canvas>
      <Markdown text={text} streaming={streaming} />
      <Sources labels={defaultCopy.citation} items={fx.doneTurn.citations} />
    </Canvas>
  ),
} satisfies Meta<{ text: string; streaming: boolean }>;

export default meta;
type Story = StoryObj<typeof meta>;

export const WithCitations: Story = {
  name: "With citations",
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const chip = await canvas.findByRole("link", { name: /Source 2: Deploy #4812/ });
    await userEvent.hover(chip);
    await expect(await canvas.findByRole("tooltip")).toHaveTextContent("Deploy #4812");
  },
};

export const Streaming: Story = {
  args: { text: fx.streamingTurn.text, streaming: true },
};

export const RichContent: Story = {
  name: "Lists, tables, code",
  args: {
    text: [
      "### What changed",
      "",
      "- `refund.ts` now defaults empty reasons",
      "- Migration `0042` checks before adding the column",
      "",
      "| File | Lines |",
      "| --- | ---: |",
      "| refund.ts | +3 −1 |",
      "| 0042_add_refund_reason.sql | +4 −1 |",
      "",
      "```sql",
      "ALTER TABLE refunds ADD COLUMN IF NOT EXISTS reason text;",
      "```",
    ].join("\n"),
  },
};
