import type { Turn } from "@agent-ui/core";
import * as fx from "@agent-ui/core/fixtures";
import { WebSurface } from "@agent-ui/render-web";
import { AgentMessage, railEntry, Thread, UserMessage } from "@agent-ui/web";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, userEvent, within } from "storybook/test";
import { webOnly } from "../../support";

const SOURCES: Turn[] = [
  fx.doneTurn,
  fx.codingTurn,
  fx.handoffTurn,
  fx.artifactTurn,
  fx.failedTurn,
  fx.usageTurn,
  fx.deniedTaskTurn,
  fx.waitingTurn,
];
const QUESTIONS = [
  "Why is payments-api returning 5xx?",
  "Fix the refund migration",
  "How many refunds failed last week?",
  "Export Q3 refunds and draft the postmortem",
  "Check the deploys service",
  "Summarize the incident",
  "Roll back payments-api",
  "Can you roll back #4812?",
];
const turns = SOURCES.map((t, i) => ({ ...t, id: `rail-${i}` }));
const entries = turns.map((t, i) =>
  railEntry(t, new Date(Date.parse("2026-10-08T14:00:00Z") + i * 7 * 60_000).toISOString()),
);

function Long() {
  return (
    <div className="mx-auto flex h-[38rem] w-full max-w-3xl flex-col">
      <WebSurface className="min-h-0 flex-1">
        <Thread className="min-h-0 flex-1" turns={entries}>
          {turns.map((t, i) => (
            <div key={t.id} className="flex flex-col gap-4">
              <UserMessage text={QUESTIONS[i] ?? ""} />
              <AgentMessage turn={t} />
            </div>
          ))}
        </Thread>
      </WebSurface>
    </div>
  );
}

const meta = {
  title: "Web-only/Turn rail",
  tags: ["web-only"],
  parameters: {
    ...webOnly("nothing: threads have their own scrollback, so there is no equivalent"),
    docs: {
      description: {
        component:
          "Left-edge turn scrubber: one tick per turn, longer when the turn did more (an equalizer, not a scrollbar). Ticks near the turn in view grow with a springy transition; the rail brightens while you scroll and rests faint. Hover for a preview; click or Enter to jump; arrows, Home, End move between turns. Web only; Slack has no equivalent.",
      },
    },
  },
  render: () => <Long />,
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

export const LongConversation: Story = {
  name: "Long conversation",
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const nav = canvas.getByRole("navigation", { name: "Jump to a turn" });
    const ticks = within(nav).getAllByRole("button");
    await expect(ticks).toHaveLength(turns.length);
    ticks[0]?.focus();
    await userEvent.keyboard("{End}");
    await expect(ticks.at(-1)).toHaveFocus();
    await expect(ticks.at(-1)).toHaveAttribute("aria-current", "true");
  },
};
