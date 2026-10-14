import type { Meta, StoryObj } from "@storybook/react-vite";
import { FailureAlert } from "@vultus/components";
import { ActionId, type Failure } from "@vultus/core";
import * as fx from "@vultus/core/fixtures";
import { expect, fn, userEvent, within } from "storybook/test";
import { type ActionArgs, Canvas, slack } from "../../support";

type Args = ActionArgs & { failure: Failure };

const meta = {
  title: "Agent/Failures",
  tags: ["slack"],
  parameters: {
    docs: {
      description: {
        component:
          "A failed run, by who can fix it: the user (reconnect), time (retry later), or us (report). Severity is in the words; recovery is a button inside the alert, never a dead end. Red only for faults; access problems are warnings.",
      },
    },
  },
  args: { onAction: fn(), failure: fx.failures.timeout },
  render: ({ onAction, failure }) => (
    <Canvas onAction={onAction}>
      <FailureAlert turnId="turn-1" failure={failure} />
    </Canvas>
  ),
} satisfies Meta<Args>;

export default meta;
type Story = StoryObj<typeof meta>;

export const AuthRequired: Story = {
  name: "Auth required",
  args: { failure: fx.failures.auth_required },
  parameters: slack("failures.auth_required"),
  play: async ({ args, canvasElement }) => {
    await userEvent.click(within(canvasElement).getByRole("button", { name: "Reconnect Linear" }));
    await expect(args.onAction).toHaveBeenCalledWith({
      id: ActionId.reconnect,
      payload: { turnId: "turn-1", target: "auth_required" },
    });
  },
};

export const RateLimited: Story = {
  name: "Rate limited",
  args: { failure: fx.failures.rate_limited },
  parameters: slack("failures.rate_limited"),
};

export const Unavailable: Story = {
  args: { failure: fx.failures.unavailable },
  parameters: slack("failures.unavailable"),
};

export const Timeout: Story = {
  args: { failure: fx.failures.timeout },
  parameters: slack("failures.timeout"),
};

export const ToolError: Story = {
  name: "Tool error",
  args: { failure: fx.failures.tool_error },
  parameters: slack("failures.tool_error"),
};

export const Forbidden: Story = {
  args: { failure: fx.failures.forbidden },
  parameters: slack("failures.forbidden"),
};

export const Internal: Story = {
  args: { failure: fx.failures.internal },
  parameters: slack("failures.internal"),
};

export const InternalNoReport: Story = {
  name: "Internal, host has nowhere to report",
  args: { failure: fx.failures.internal },
  render: ({ onAction, failure }) => (
    <Canvas onAction={onAction}>
      <FailureAlert turnId="turn-1" failure={failure} report={false} />
    </Canvas>
  ),
  parameters: slack("failedTurn.noReport", "TurnView takes `capabilities={{ report: false }}`"),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.queryByRole("button", { name: "Report Issue" })).toBeNull();
  },
};
