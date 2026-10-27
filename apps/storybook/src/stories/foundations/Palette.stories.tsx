import type { Meta, StoryObj } from "@storybook/react-vite";

const meta = {
  title: "Foundations/Palette",
  tags: ["foundations"],
  parameters: {
    docs: {
      description: {
        component:
          "Neutral light and charcoal surfaces with a restrained red interaction accent, gold attention and green success. Red, yellow and green presets lead the palette; existing saved accents remain supported. Use the toolbar to compare themes and presets.",
      },
    },
  },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

const groups: { title: string; note: string; tokens: string[] }[] = [
  {
    title: "Base",
    note: "Surfaces and text. Primary actions are neutral, not branded.",
    tokens: [
      "bg",
      "surface",
      "pop",
      "fg",
      "muted",
      "subtle",
      "border",
      "hover",
      "code-bg",
      "primary",
      "primary-fg",
    ],
  },
  {
    title: "Accent",
    note: "Brand, focus rings, live progress.",
    tokens: ["accent", "accent-fg", "accent-soft"],
  },
  {
    title: "Status",
    note: "State only. `info` follows the accent.",
    tokens: ["ok", "warn", "bad", "info", "neutral"],
  },
  { title: "Terminal", note: "Dark in both themes.", tokens: ["term-bg", "term-fg", "term-muted"] },
];

const presets = [
  { name: "red", hue: 27 },
  { name: "yellow", hue: 88 },
  { name: "green", hue: 152 },
  { name: "purple", hue: 305 },
  { name: "blue", hue: 255 },
  { name: "teal", hue: 185 },
];

function Swatch({ token }: { token: string }) {
  return (
    <div className="flex items-center gap-3">
      <div
        className="size-10 shrink-0 rounded-aui border border-aui-border shadow-aui-sm"
        style={{ background: `var(--aui-${token})` }}
      />
      <code className="truncate font-aui-mono text-aui-muted text-aui-xs">--aui-{token}</code>
    </div>
  );
}

export const Tokens: Story = {
  render: () => (
    <div className="mx-auto flex max-w-6xl flex-col gap-10">
      {groups.map((g) => (
        <section key={g.title} className="flex flex-col gap-3">
          <header>
            <h2 className="font-semibold text-aui-lg">{g.title}</h2>
            <p className="text-aui-muted text-aui-sm">{g.note}</p>
          </header>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
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
    <div className="mx-auto grid max-w-6xl gap-4 sm:grid-cols-2 lg:grid-cols-4">
      {presets.map((p) => (
        <div
          key={p.name}
          data-accent={p.name}
          className="flex flex-col gap-3 rounded-aui-lg border border-aui-border bg-aui-surface p-4"
        >
          <div className="flex items-center justify-between">
            <span className="font-semibold text-aui-sm capitalize">{p.name}</span>
            <code className="font-aui-mono text-aui-muted text-aui-xs">h {p.hue}</code>
          </div>
          <div className="h-14 rounded-aui bg-aui-accent" />
          <div className="flex flex-wrap gap-2">
            <span className="rounded-aui-sm bg-aui-accent px-2 py-1 font-medium text-aui-accent-fg text-aui-xs">
              Accent
            </span>
            <span className="rounded-aui-sm bg-aui-accent-soft px-2 py-1 font-medium text-aui-fg text-aui-xs">
              Soft
            </span>
            <span className="rounded-aui-sm bg-aui-primary px-2 py-1 font-medium text-aui-primary-fg text-aui-xs">
              Primary
            </span>
          </div>
          <a className="text-aui-accent text-aui-sm underline underline-offset-2" href="#top">
            A link in this accent
          </a>
          <button
            type="button"
            className="h-8 rounded-aui border border-aui-border px-3 text-aui-sm outline-none ring-2 ring-aui-accent ring-offset-2 ring-offset-aui-surface"
          >
            Focus ring
          </button>
        </div>
      ))}
    </div>
  ),
};

export const HueShift: Story = {
  name: "Hue shift",
  render: () => (
    <div className="mx-auto flex max-w-6xl flex-col gap-3">
      <p className="text-aui-muted text-aui-sm">
        One preset, nudged with <code className="font-aui-mono">--aui-accent-shift</code>. Lightness
        and chroma stay tuned per mode, so contrast holds.
      </p>
      <div className="grid grid-cols-3 gap-2 sm:grid-cols-9">
        {[-60, -45, -30, -15, 0, 15, 30, 45, 60].map((shift) => (
          <div
            key={shift}
            data-accent="red"
            style={{ ["--aui-accent-shift" as string]: String(shift) }}
            className="flex flex-col items-center gap-1.5"
          >
            <div className="h-12 w-full rounded-aui bg-aui-accent" />
            <code className="font-aui-mono text-aui-muted text-aui-xs">
              {shift > 0 ? `+${shift}` : shift}°
            </code>
          </div>
        ))}
      </div>
    </div>
  ),
};
