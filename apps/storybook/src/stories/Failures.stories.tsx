import { FailureAlert } from "@agent-ui/components";
import type { FailureKind } from "@agent-ui/core";
import * as fx from "@agent-ui/core/fixtures";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { Dual } from "../Dual";

const meta = {
  title: "Agent/Failures",
  tags: ["web", "slack"],
  parameters: {
    docs: {
      description: {
        component:
          "One alert per failure kind, sorted by who can fix it: the user (reconnect), time (retry), or us (report). Slack messages have no alert block, so severity is stated in words; modals get the native alert block.",
      },
    },
  },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

const story = (kind: FailureKind, placement: "message" | "modal" = "message"): Story => ({
  render: () => (
    <Dual placement={placement}>
      <FailureAlert turnId="turn-1" failure={fx.failures[kind]} />
    </Dual>
  ),
});

export const AuthRequired = story("auth_required");
export const RateLimited = story("rate_limited");
export const Unavailable = story("unavailable");
export const Timeout = story("timeout");
export const ToolError = story("tool_error");
export const Forbidden = story("forbidden");
export const Internal = story("internal");
export const InModal = { ...story("rate_limited", "modal"), name: "In a modal (alert block)" };
