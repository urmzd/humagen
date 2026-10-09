import { SessionNotices } from "@agent-ui/components";
import type { ApprovalMode, SessionState } from "@agent-ui/core";
import * as fx from "@agent-ui/core/fixtures";
import { PromptInput, StatusLine } from "@agent-ui/web";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";
import { expect, fn, userEvent, within } from "storybook/test";
import { Canvas, webOnly } from "../../support";

const SLACK = "SessionNotices: the same state as context lines and callouts, only when it matters";

type Args = { session: SessionState; onCompact: () => void };

function Live({ session, onCompact }: Args) {
  const [mode, setMode] = useState<ApprovalMode>(session.approvalMode ?? "ask");
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6">
      <PromptInput
        onSubmit={() => {}}
        footer={
          <StatusLine
            className="border-aui-border border-t px-1 pt-1"
            session={{ ...session, approvalMode: mode }}
            onApprovalMode={setMode}
            onCompact={onCompact}
          />
        }
      />
      <div className="flex flex-col gap-2">
        <p className="text-aui-2xs text-aui-muted">Slack equivalent (SessionNotices):</p>
        <Canvas>
          <SessionNotices session={session} />
        </Canvas>
      </div>
    </div>
  );
}

const meta = {
  title: "Web-only/Status line",
  tags: ["web-only"],
  parameters: {
    ...webOnly(SLACK),
    docs: {
      description: {
        component: `A thin segmented bar under the composer, like an editor statusline: activity, model, approval mode (click to toggle), context meter with the compaction tick (hover for tokens and Compact now), compaction state, spend against the cap, and queued messages. Segments drop away on narrow widths. Web only. Slack uses ${SLACK}.`,
      },
    },
  },
  args: { session: fx.sessions.fresh, onCompact: fn() },
  render: (args) => <Live {...args} />,
} satisfies Meta<Args>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Fresh: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const toggle = canvas.getByRole("button", { name: "Ask before acting" });
    await userEvent.click(toggle);
    await expect(canvas.getByRole("button", { name: "Act without asking" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  },
};
export const Busy: Story = { args: { session: fx.sessions.busy } };
export const Compacting: Story = { args: { session: fx.sessions.compacting } };
export const Compacted: Story = { args: { session: fx.sessions.compacted } };
export const NearLimit: Story = {
  name: "Near the context limit",
  args: { session: fx.sessions.nearLimit },
  play: async ({ args, canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.hover(canvas.getByRole("button", { name: "Context details" }));
    await userEvent.click(await canvas.findByRole("button", { name: "Compact now" }));
    await expect(args.onCompact).toHaveBeenCalled();
  },
};
export const OverBudget: Story = { name: "Over budget", args: { session: fx.sessions.overBudget } };
