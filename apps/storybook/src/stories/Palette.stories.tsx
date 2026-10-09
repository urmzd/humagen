import type { Meta, StoryObj } from "@storybook/react-vite";

const meta = {
  title: "Foundations/Palette",
  parameters: {
    docs: {
      description: {
        component:
          "Neutral black base, one swappable accent. Use the toolbar to change theme, accent preset, and hue shift.",
      },
    },
  },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

const groups: { title: string; tokens: string[] }[] = [
  {
    title: "Base",
    tokens: [
      "bg",
      "surface",
      "fg",
      "muted",
      "subtle",
      "border",
      "code-bg",
      "primary",
      "primary-fg",
    ],
  },
  { title: "Accent", tokens: ["accent", "accent-fg", "accent-soft"] },
  { title: "Status (state only)", tokens: ["ok", "warn", "bad", "info", "neutral"] },
];

const presets = [
  { name: "purple", hue: 305 },
  { name: "blue", hue: 255 },
  { name: "teal", hue: 185 },
  { name: "yellow", hue: 90 },
];

function Swatch({ token }: { token: string }) {
  return (
    <div className="flex items-center gap-3">
      <div
        className="size-10 rounded-md border border-aui-border"
        style={{ background: `var(--aui-${token})` }}
      />
      <code className="text-xs text-aui-muted">--aui-{token}</code>
    </div>
  );
}

export const Tokens: Story = {
  render: () => (
    <div className="flex flex-col gap-8">
      {groups.map((g) => (
        <section key={g.title}>
          <h2 className="mb-3 text-sm font-semibold">{g.title}</h2>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
            {g.tokens.map((t) => (
              <Swatch key={t} token={t} />
            ))}
          </div>
        </section>
      ))}
    </div>
  ),
};

export const AccentPresets: Story = {
  render: () => (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      {presets.map((p) => (
        <div
          key={p.name}
          data-accent={p.name}
          className="flex flex-col gap-3 rounded-aui border border-aui-border bg-aui-surface p-4"
        >
          <div className="flex items-center justify-between">
            <span className="text-sm font-semibold capitalize">{p.name}</span>
            <code className="text-xs text-aui-muted">h {p.hue}</code>
          </div>
          <div className="h-12 rounded-md bg-aui-accent" />
          <div className="flex gap-2">
            <span className="rounded-md bg-aui-accent px-2 py-1 text-xs font-medium text-aui-accent-fg">
              Accent
            </span>
            <span className="rounded-md bg-aui-accent-soft px-2 py-1 text-xs font-medium text-aui-fg">
              Soft
            </span>
            <span className="rounded-md bg-aui-primary px-2 py-1 text-xs font-medium text-aui-primary-fg">
              Primary
            </span>
          </div>
          <a className="text-sm text-aui-accent underline" href="#top">
            A link in this accent
          </a>
        </div>
      ))}
    </div>
  ),
};
