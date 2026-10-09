import { Markdown } from "@agent-ui/primitives";
import { Shimmer, ShimmerVerb, Spinner, StatusIcon, useVerbSequence } from "@agent-ui/render-web";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { Canvas } from "../../support";

const meta = {
  title: "Foundations/Typography",
  tags: ["foundations"],
  parameters: {
    docs: {
      description: {
        component:
          "Figtree at a dense chat scale: 14px body, 13px secondary, 12px meta. Utilities are `text-aui-*`; mono is `font-aui-mono`. Motion: one spinner, one shimmer, all of it off under prefers-reduced-motion.",
      },
    },
  },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

const scale = [
  { token: "xl", cls: "text-aui-xl", px: "20 / 28", use: "Page titles (rare in chat)" },
  { token: "lg", cls: "text-aui-lg", px: "16 / 24", use: "Header primitive" },
  { token: "base", cls: "text-aui-base", px: "14 / 24", use: "Answers, prompt input" },
  { token: "sm", cls: "text-aui-sm", px: "13 / 20", use: "Task rows, buttons, callouts" },
  { token: "xs", cls: "text-aui-xs", px: "12 / 18", use: "Context, meta, status" },
  { token: "2xs", cls: "text-aui-2xs", px: "11 / 16", use: "Badges, terminal chrome" },
];

export const Scale: Story = {
  render: () => (
    <div className="mx-auto flex max-w-3xl flex-col divide-y divide-aui-border">
      {scale.map((s) => (
        <div key={s.token} className="grid grid-cols-[5rem_1fr] items-baseline gap-4 py-4">
          <code className="font-aui-mono text-aui-muted text-aui-xs">text-aui-{s.token}</code>
          <div className="flex min-w-0 flex-col gap-1">
            <span className={`${s.cls} truncate`}>Rolled back payments-api to deploy #4811</span>
            <span className="text-aui-muted text-aui-xs">
              {s.px}px · {s.use}
            </span>
          </div>
        </div>
      ))}
      <div className="grid grid-cols-[5rem_1fr] items-baseline gap-4 py-4">
        <code className="font-aui-mono text-aui-muted text-aui-xs">font-aui-mono</code>
        <span className="font-aui-mono text-aui-sm">pnpm test --filter payments-api</span>
      </div>
    </div>
  ),
};

const prose = [
  "## Rollback plan",
  "The error rate on **payments-api** started climbing at 14:52, two minutes after deploy `#4812` [[1]](https://example.com/deploys/4812).",
  "",
  "1. Roll back `#4812`.",
  "2. Watch the 5xx rate for 10 minutes.",
  "3. If it holds, open a ticket against the deploy owner.",
  "",
  "> Roll back before paging the on-call.",
  "",
  "| Deploy | Time | Status |",
  "| --- | --- | --- |",
  "| #4810 | 09:12 | Healthy |",
  "| #4812 | 14:50 | Errors |",
  "",
  "```ts",
  "export const rollbackTo = 4811;",
  "```",
].join("\n");

export const Prose: Story = {
  render: () => (
    <Canvas>
      <Markdown text={prose} />
    </Canvas>
  ),
};

function MotionDemo() {
  const verb = useVerbSequence(true);
  return (
    <div className="mx-auto grid max-w-3xl gap-6 sm:grid-cols-2">
      <section className="flex flex-col gap-3 rounded-aui-lg border border-aui-border bg-aui-surface p-4">
        <h3 className="font-semibold text-aui-sm">Status icons</h3>
        <p className="text-aui-muted text-aui-xs">
          16px squares. The spinner is an SVG ring rotating about its own center.
        </p>
        <div className="flex items-center gap-4">
          {(["pending", "in_progress", "complete", "error"] as const).map((s) => (
            <span key={s} className="flex items-center gap-1.5 text-aui-muted text-aui-xs">
              <StatusIcon status={s} />
              {s}
            </span>
          ))}
        </div>
        <div className="flex items-center gap-3 text-aui-accent">
          <Spinner size={12} />
          <Spinner size={16} />
          <Spinner size={24} />
          <Spinner size={32} />
        </div>
      </section>
      <section className="flex flex-col gap-3 rounded-aui-lg border border-aui-border bg-aui-surface p-4">
        <h3 className="font-semibold text-aui-sm">Shimmer</h3>
        <p className="text-aui-muted text-aui-xs">
          Live work only. Settled rows never animate. Reduced motion shows plain text.
        </p>
        <Shimmer className="text-aui-sm">Checking recent deploys</Shimmer>
        <span className="text-aui-sm">
          <ShimmerVerb>{verb}</ShimmerVerb>
        </span>
      </section>
    </div>
  );
}

export const Motion: Story = { render: () => <MotionDemo /> };
