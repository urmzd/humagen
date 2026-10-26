import { defaultCopy } from "@humagen/components";
import * as fx from "@humagen/core/fixtures";
import { Sources } from "@humagen/primitives";
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
        labels={defaultCopy.citation}
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

/** Deterministic recorded provenance fixtures, not a live provider validation. */
export const RecordedProvenance: Story = {
  render: () => (
    <Canvas>
      <Sources
        labels={defaultCopy.citation}
        items={[
          {
            id: "indexed",
            title: "Recorded runbook copy",
            url: "https://example.com/recorded-runbook",
            mode: "indexed",
            copyAt: "2026-10-20T12:00:00Z",
            refreshFailed: true,
          },
          {
            id: "live",
            title: "Recorded live retrieval",
            url: "https://example.com/recorded-live",
            mode: "live",
          },
          {
            id: "historic",
            title: "Historical citation without retrieval evidence",
            url: "https://example.com/historical",
          },
        ]}
      />
    </Canvas>
  ),
};
