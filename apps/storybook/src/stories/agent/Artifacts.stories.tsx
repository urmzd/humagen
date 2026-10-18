import type { Meta, StoryObj } from "@storybook/react-vite";
import { AttachmentView } from "@warp/components";
import type { Artifact } from "@warp/core";
import * as fx from "@warp/core/fixtures";
import { AgentMessage } from "@warp/web";
import { expect, fn, userEvent, within } from "storybook/test";
import { type ActionArgs, Canvas, slack } from "../../support";

type Args = ActionArgs & { artifact: Artifact };

const meta = {
  title: "Agent/Artifacts",
  tags: ["slack"],
  parameters: {
    docs: {
      description: {
        component:
          "A file the agent made, in every phase. The icon draws the file as it's written: the page draws itself and breathes while queued, fills mark by mark with a sweep while generating, and holds still while waiting on an approval. Ready files offer Open (the preview panel) and Download. Reduced motion shows a partly filled page instead.",
      },
    },
  },
  args: { onAction: fn(), artifact: fx.artifacts.ready },
  render: ({ onAction, artifact }) => (
    <Canvas onAction={onAction}>
      <AttachmentView attachment={artifact} />
    </Canvas>
  ),
} satisfies Meta<Args>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Queued: Story = {
  args: { artifact: fx.artifacts.queued },
  parameters: slack("artifacts.queued"),
};
export const Generating: Story = {
  args: { artifact: fx.artifacts.generating },
  parameters: slack("artifacts.generating"),
};
export const Waiting: Story = {
  name: "Waiting on approval (static)",
  args: { artifact: fx.artifacts.waiting },
  parameters: slack("artifacts.waiting"),
};
export const Finalizing: Story = {
  args: { artifact: fx.artifacts.finalizing },
  parameters: slack("artifacts.finalizing"),
};
export const Ready: Story = {
  args: { artifact: fx.artifacts.ready },
  parameters: slack("artifacts.ready"),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const download = canvas.getByRole("link", { name: "Download refunds-q3.xlsx" });
    await expect(download).toHaveAttribute("download", "refunds-q3.xlsx");
    await userEvent.click(canvas.getByRole("button", { name: "Open refunds-q3.xlsx" }));
    const panel = await within(document.body).findByRole("dialog", { name: "refunds-q3.xlsx" });
    await expect(panel).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    await expect(within(document.body).queryByRole("dialog")).toBeNull();
  },
};
export const Failed: Story = {
  args: { artifact: fx.artifacts.failed },
  parameters: slack("artifacts.failed"),
};
export const Denied: Story = {
  args: { artifact: fx.artifacts.denied },
  parameters: slack("artifacts.denied"),
};
export const Report: Story = {
  name: "Markdown report (preview)",
  args: { artifact: fx.artifacts.report },
  parameters: slack("artifacts.report"),
};
export const Image: Story = {
  args: { artifact: fx.artifacts.image },
  parameters: slack("artifacts.image"),
};

export const AllPhases: Story = {
  name: "All phases",
  render: ({ onAction }) => (
    <Canvas onAction={onAction}>
      {Object.values(fx.artifacts).map((a) => (
        <AttachmentView key={`${a.id}-${a.phase}-${a.title}`} attachment={a} />
      ))}
    </Canvas>
  ),
  parameters: slack("artifacts.ready", "Each phase posts as its own fixture"),
};

export const InTurn: Story = {
  name: "In a turn",
  render: ({ onAction }) => (
    <Canvas onAction={onAction}>
      <AgentMessage turn={fx.artifactTurn} />
    </Canvas>
  ),
  parameters: slack("artifactTurn"),
};
