import type { Meta, StoryObj } from "@storybook/react-vite";
import { AgentOrb, type OrbSize, type OrbState, type OrbVariant, orbLabel } from "@warp/web";
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

export const ClippedRows: Story = {
  name: "Fits clipped rows at exact pixel sizes",
  render: () => (
    <div className="grid grid-cols-2 gap-4 sm:grid-cols-5">
      {STATES.map((state) => (
        <div key={state} className="flex flex-col gap-2">
          <span className="text-aui-xs text-aui-muted">{orbLabel[state]}</span>
          {[12, 14, 16, 18, 24, 32, 48].map((size) => (
            <div
              key={size}
              className="flex items-center gap-2 overflow-hidden"
              style={{ height: size }}
            >
              <AgentOrb state={state} size={size} />
              <span className="text-aui-xs">{size}px</span>
            </div>
          ))}
        </div>
      ))}
    </div>
  ),
  play: async ({ canvasElement }) => {
    for (const orb of canvasElement.querySelectorAll<HTMLElement>(".aui-orb")) {
      const size = Number.parseFloat(orb.style.getPropertyValue("--orb-size"));
      const frame = orb.getBoundingClientRect();
      await expect(frame.width).toBe(size);
      await expect(frame.height).toBe(size);
      const animations = orb.getAnimations({ subtree: true });
      for (const animation of animations) animation.pause();
      try {
        const outside: string[] = [];
        // Sample a complete cycle, including the ripple's largest frame.
        for (const phase of [0, 0.25, 0.5, 0.75, 0.999]) {
          for (const animation of animations) {
            animation.currentTime = Number(animation.effect?.getTiming().duration) * phase;
          }
          for (const effect of orb.querySelectorAll<HTMLElement>(
            ".aui-orb-sphere, .aui-orb-halo",
          )) {
            const bounds = effect.getBoundingClientRect();
            if (
              bounds.left < frame.left - 0.1 ||
              bounds.top < frame.top - 0.1 ||
              bounds.right > frame.right + 0.1 ||
              bounds.bottom > frame.bottom + 0.1
            ) {
              outside.push(`${orb.dataset.state} ${size}px: ${effect.className} at ${phase}`);
            }
          }
        }
        await expect(outside).toEqual([]);
        // The satellite's full circular path, including its own diameter.
        const satellite = orb.querySelector<HTMLElement>(".aui-orb-sat");
        if (satellite) {
          const orbit = Number.parseFloat(getComputedStyle(satellite).width);
          const dot = Number.parseFloat(getComputedStyle(satellite, "::before").width);
          await expect(orbit + dot).toBeLessThanOrEqual(size);
        }
        if (orb.dataset.state === "waiting") await expect(animations).toHaveLength(0);
      } finally {
        for (const animation of animations) animation.play();
      }
    }
  },
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
