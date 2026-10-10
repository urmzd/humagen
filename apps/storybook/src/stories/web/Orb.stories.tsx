import type { Meta, StoryObj } from "@storybook/react-vite";
import { AgentOrb, type OrbSize, type OrbState, type OrbVariant, orbLabel } from "@vultus/web";
import { expect } from "storybook/test";
import { webOnly } from "../../support";

const SLACK =
  "a reaction on the message plus `assistant.threads.setStatus` / agents.sessions.setStatus for the live activity";

const STATES: OrbState[] = [
  "idle",
  "thinking",
  "searching",
  "calling",
  "delegating",
  "composing",
  "waiting",
  "done",
  "error",
  "stopped",
];
const VARIANTS: OrbVariant[] = ["orb", "ring", "dots", "wave"];

type Args = { state: OrbState; variant: OrbVariant; size: OrbSize };

const meta = {
  title: "Web-only/Orb",
  component: AgentOrb,
  tags: ["web-only"],
  parameters: {
    ...webOnly(SLACK),
    docs: {
      description: {
        component: `The agent's ambient activity mark, driven by AgentActivity and the turn outcome. Thinking breathes, searching sweeps, calling ripples out, delegating sends a satellite, composing converges. Waiting on a person holds still: it never animates. Accent by default (follows --aui-accent). Reduced motion shows one static frame. Web only. Slack uses ${SLACK}.`,
      },
    },
  },
  args: { state: "thinking", variant: "orb", size: "lg" },
  argTypes: {
    state: { control: "select", options: STATES },
    variant: { control: "inline-radio", options: VARIANTS },
    size: { control: "inline-radio", options: ["xs", "sm", "md", "lg"] },
  },
  render: (args) => (
    <div className="flex justify-center py-8">
      <AgentOrb {...args} />
    </div>
  ),
} satisfies Meta<Args>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Playground: Story = {};

export const Gallery: Story = {
  name: "Every state and variant",
  render: () => (
    <table className="mx-auto text-aui-xs">
      <thead>
        <tr>
          <th />
          {VARIANTS.map((v) => (
            <th key={v} className="px-4 pb-3 font-medium text-aui-muted">
              {v}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {STATES.map((s) => (
          <tr key={s}>
            <th className="py-2 pr-4 text-left font-medium text-aui-muted">{orbLabel[s]}</th>
            {VARIANTS.map((v) => (
              <td key={v} className="px-4 py-2 text-center">
                <AgentOrb state={s} variant={v} size="md" />
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  ),
};

export const Sizes: Story = {
  render: (args) => (
    <div className="flex items-center justify-center gap-6 py-8">
      {(["xs", "sm", "md", "lg"] as const).map((size) => (
        <AgentOrb key={size} {...args} size={size} />
      ))}
    </div>
  ),
};

export const WaitingIsStatic: Story = {
  name: "Waiting is static",
  args: { state: "waiting" },
  play: async ({ canvasElement }) => {
    const orb = canvasElement.querySelector<HTMLElement>('.aui-orb[data-state="waiting"]');
    await expect(orb).not.toBeNull();
    for (const el of orb?.querySelectorAll<HTMLElement>("*") ?? []) {
      await expect(getComputedStyle(el).animationName).toBe("none");
      await expect(getComputedStyle(el, "::before").animationName).toBe("none");
    }
    await expect(orb).toHaveAccessibleName("Waiting for you");
  },
};

export const WaitingVariants: Story = {
  name: "Waiting, every variant (static)",
  render: () => (
    <div className="flex items-center justify-center gap-8 py-8">
      {VARIANTS.map((v) => (
        <AgentOrb key={v} state="waiting" variant={v} size="lg" />
      ))}
    </div>
  ),
  play: async ({ canvasElement }) => {
    for (const el of canvasElement.querySelectorAll<HTMLElement>(".aui-orb *")) {
      await expect(getComputedStyle(el).animationName).toBe("none");
    }
  },
};
