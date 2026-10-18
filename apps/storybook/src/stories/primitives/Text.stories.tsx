import type { Meta, StoryObj } from "@storybook/react-vite";
import { Context, Divider, Header, Text } from "@warp/primitives";
import { expect, within } from "storybook/test";
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

/** `level` sets the heading element on the web: 1 is the screen's one title. Slack keeps one header block. */
export const HeaderLevels: Story = {
  render: () => (
    <Canvas>
      <Header text="Hi Ada, I'm Warp." level={1} />
      <Header text="Notifications" level={2} />
      <Header text="Inside a card" />
    </Canvas>
  ),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.getByRole("heading", { level: 1 })).toHaveTextContent("Hi Ada");
    await expect(canvas.getByRole("heading", { level: 2 })).toHaveTextContent("Notifications");
    await expect(canvas.getByRole("heading", { level: 3 })).toHaveTextContent("Inside a card");
  },
};

/** Links on the same site stay in the tab; links elsewhere open a new one. */
export const Links: Story = {
  render: () => (
    <Canvas>
      <Context items={["[The inbox](/?view=inbox)", "[Slack status](https://slack-status.com)"]} />
    </Canvas>
  ),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.getByRole("link", { name: "The inbox" })).not.toHaveAttribute("target");
    const away = canvas.getByRole("link", { name: "Slack status" });
    await expect(away).toHaveAttribute("target", "_blank");
    await expect(away).toHaveAttribute("rel", "noopener noreferrer");
  },
};

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
