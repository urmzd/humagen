import type { Meta, StoryObj } from "@storybook/react-vite";
import type { Tone } from "@vultus/core";
import { Callout } from "@vultus/primitives";
import { Canvas, slackPrimitive } from "../../support";

const meta = {
  title: "Primitives/Callout",
  tags: ["slack"],
  parameters: slackPrimitive(
    "messages have no alert block, so a section with the title in bold; modals use the alert block with the matching level",
  ),
  args: {
    tone: "warn",
    title: "Linear needs a reconnect",
    text: "Your Linear connection expired, so I skipped the issue search.",
  },
  argTypes: {
    tone: { control: "inline-radio", options: ["ok", "warn", "bad", "info", "neutral"] },
  },
  render: (args) => (
    <Canvas>
      <Callout {...args} />
    </Canvas>
  ),
} satisfies Meta<{ tone: Tone; title: string; text: string }>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Playground: Story = {};

export const AllTones: Story = {
  name: "All tones",
  render: () => (
    <Canvas>
      <Callout tone="ok" title="Rollback complete" text="Error rate is back to baseline." />
      <Callout tone="info" title="Still working" text="Large deploy histories take a minute." />
      <Callout
        tone="warn"
        title="Linear needs a reconnect"
        text="Your Linear connection expired."
      />
      <Callout
        tone="bad"
        title="Rate limited"
        text="The model provider is rate limiting requests."
      />
      <Callout tone="neutral" text="Policy: agents can't post in #exec." />
    </Canvas>
  ),
};
