import type { Meta, StoryObj } from "@storybook/react-vite";
import { FailureAlert } from "@vultus/components";
import * as fx from "@vultus/core/fixtures";
import { WebSurface } from "@vultus/render-web";
import {
  SpendBlockedBanner,
  StopBanner,
  SystemStatusBanner,
  Toaster,
  useToasts,
} from "@vultus/web";
import { expect, fn, userEvent, within } from "storybook/test";
import { type ActionArgs, Canvas, webOnly } from "../../support";

const meta = {
  title: "Web-only/Alerts",
  tags: ["web-only"],
  parameters: {
    ...webOnly(
      "callouts in the message (FailureAlert, NoticeView, SystemAlertView) and ephemeral messages instead of toasts",
    ),
    docs: {
      description: {
        component:
          "System states on the web: the incident banner (SystemAlertView, dismissal per incident and status, polite live region), the spend-blocked banner above the composer, stop phases as an inline status row, failure references as a copy chip with the HTTP status, and a small toast system that pauses on hover.",
      },
    },
  },
  args: { onAction: fn() },
} satisfies Meta<ActionArgs>;

export default meta;
type Story = StoryObj<Meta<ActionArgs>>;

export const SystemStatus: Story = {
  name: "System status banner",
  render: ({ onAction }) => (
    <div className="mx-auto flex max-w-3xl flex-col gap-3">
      <SystemStatusBanner alert={fx.systemAlerts.down} onAction={onAction} />
      <SystemStatusBanner alert={fx.systemAlerts.maintenance} onAction={onAction} />
    </div>
  ),
};

export const SpendBlocked: Story = {
  name: "Spend blocked (above the composer)",
  render: ({ onAction }) => (
    <div className="mx-auto max-w-3xl">
      <SpendBlockedBanner
        budget={fx.sessions.overBudget.budget}
        onAddBudget={() =>
          onAction({ id: "aui.reconnect", payload: { turnId: "session", target: "budget" } })
        }
      />
    </div>
  ),
};

export const StopPhases: Story = {
  name: "Stop phases",
  render: ({ onAction }) => (
    <div className="mx-auto flex max-w-3xl flex-col gap-2">
      <WebSurface onAction={onAction}>
        {(["requested", "acknowledged", "failed", "confirmed"] as const).map((s) => (
          <StopBanner key={s} turnId="turn-stop" stop={s} />
        ))}
      </WebSurface>
    </div>
  ),
  play: async ({ args, canvasElement }) => {
    await userEvent.click(within(canvasElement).getByRole("button", { name: "Resume" }));
    await expect(args.onAction).toHaveBeenCalledWith({
      id: "aui.resume",
      payload: { turnId: "turn-stop", target: "turn-stop" },
    });
  },
};

export const FailureReference: Story = {
  name: "Failure with reference and HTTP status",
  render: ({ onAction }) => (
    <Canvas onAction={onAction}>
      <FailureAlert
        turnId="turn-1"
        failure={{ ...fx.failures.internal, reference: "req_8f2a91", httpStatus: 500 }}
      />
      <FailureAlert turnId="turn-2" failure={fx.failures.forbidden} />
    </Canvas>
  ),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: /Copy reference/ }));
    await expect(
      await canvas.findByRole("button", { name: "Reference copied" }),
    ).toBeInTheDocument();
  },
};

function ToastDemo() {
  const { toast } = useToasts();
  const btn =
    "rounded-aui border border-aui-border px-2.5 py-1 text-aui-sm hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent";
  return (
    <div className="mx-auto flex max-w-3xl gap-2">
      <button
        type="button"
        className={btn}
        onClick={() => toast("success", "Rolled back to #4811")}
      >
        Success
      </button>
      <button type="button" className={btn} onClick={() => toast("warn", "Approval expires in 5m")}>
        Warning
      </button>
      <button
        type="button"
        className={btn}
        onClick={() => toast("error", "Couldn't post to #payments-oncall")}
      >
        Error
      </button>
      <Toaster />
    </div>
  );
}

export const Toasts: Story = {
  render: () => <ToastDemo />,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Success" }));
    await expect(
      await within(document.body).findByText("Rolled back to #4811"),
    ).toBeInTheDocument();
  },
};
