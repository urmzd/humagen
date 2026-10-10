import type { Meta, StoryObj } from "@storybook/react-vite";
import { Context, Divider, Header, Text } from "@vultus/primitives";
import { Canvas, slackPrimitive } from "../../support";

const meta = {
  title: "Primitives/Text",
  tags: ["slack"],
  parameters: slackPrimitive(
    "Header is a header block, Text a section with mrkdwn, Context a context block, Divider a divider. `{{time:ISO}}` tokens become `<!date>` so each reader sees their own timezone",
  ),
  args: {
    header: "Payments incident",
    text: "Error rate on **payments-api** is back to baseline since {{time:2026-10-08T15:04:00Z}}. See the `#4811` deploy.",
  },
  render: ({ header, text }) => (
    <Canvas>
      <Header text={header} />
      <Text text={text} />
      <Divider />
      <Context
        items={[
          "**Assistant** · Done",
          "Rolled back by Dana",
          "[Open full session](https://example.com)",
        ]}
      />
    </Canvas>
  ),
} satisfies Meta<{ header: string; text: string }>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Playground: Story = {};

export const ContextLine: Story = {
  name: "Context",
  render: () => (
    <Canvas>
      <Context
        items={[
          "Will run as the deploy bot, on behalf of Dana",
          "Expires {{time:2026-10-08T15:34:00Z}}",
        ]}
      />
      <Context items={["AI-generated. Check important answers."]} />
    </Canvas>
  ),
};
