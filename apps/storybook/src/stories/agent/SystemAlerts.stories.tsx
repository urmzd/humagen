import type { Meta, StoryObj } from "@storybook/react-vite";
import { SystemAlertView } from "@vultus/components";
import type { SystemAlert } from "@vultus/core";
import * as fx from "@vultus/core/fixtures";
import { SystemStatusBanner } from "@vultus/web";
import { useEffect, useState } from "react";
import { fn } from "storybook/test";
import { type ActionArgs, Canvas, slack } from "../../support";

type Args = ActionArgs & { alert: SystemAlert };

const meta = {
  title: "Agent/System alerts",
  tags: ["slack"],
  parameters: {
    docs: {
      description: {
        component:
          "A dependency incident in words: what's affected and what to do, updated in place as it develops. Resolved incidents shrink to a quiet line.",
      },
    },
  },
  args: { onAction: fn(), alert: fx.systemAlerts.degraded },
  render: ({ onAction, alert }) => (
    <Canvas onAction={onAction}>
      <SystemAlertView alert={alert} />
    </Canvas>
  ),
} satisfies Meta<Args>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Degraded: Story = { parameters: slack("systemAlerts.degraded") };
export const Down: Story = {
  args: { alert: fx.systemAlerts.down },
  parameters: slack("systemAlerts.down"),
};
export const Maintenance: Story = {
  args: { alert: fx.systemAlerts.maintenance },
  parameters: slack("systemAlerts.maintenance"),
};
export const Resolved: Story = {
  args: { alert: fx.systemAlerts.resolved },
  parameters: slack("systemAlerts.resolved"),
};

const steps = [fx.systemAlerts.degraded, fx.systemAlerts.resolved];

function Developing({ onAction }: ActionArgs) {
  const [i, setI] = useState(0);
  useEffect(() => {
    const t = setInterval(() => setI((n) => (n + 1) % 2), 3000);
    return () => clearInterval(t);
  }, []);
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4">
      <SystemStatusBanner alert={steps[i]} onAction={onAction} />
      <p className="text-aui-muted text-aui-xs">
        The banner updates in place every 3s (degraded → resolved). Dismissing hides this incident
        at this status only.
      </p>
    </div>
  );
}

export const UpdatedInPlace: Story = {
  name: "Updated in place (banner)",
  render: (args) => <Developing {...args} />,
  parameters: slack(
    "systemAlerts.degraded",
    "Then `systemAlerts.resolved` updates the same message",
  ),
};
