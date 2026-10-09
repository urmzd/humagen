import * as fx from "@agent-ui/core/fixtures";
import { Sources } from "@agent-ui/primitives";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { Canvas, slackPrimitive } from "../../support";

const meta = {
  title: "Primitives/Sources",
  tags: ["slack"],
  parameters: slackPrimitive("a context block of numbered links, matching the inline [n] markers"),
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

export const List: Story = {
  render: () => (
    <Canvas>
      <Sources
        items={[
          ...fx.doneTurn.citations,
          { id: "c3", title: "Payments on-call schedule", source: "PagerDuty" },
          {
            id: "c4",
            title: "Postmortem: refunds outage, March",
            url: "https://docs.example.com/postmortems/refunds-march",
            snippet: "A migration added a column that already existed in two shards.",
          },
        ]}
      />
    </Canvas>
  ),
};
