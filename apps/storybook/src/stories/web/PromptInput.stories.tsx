import type { Meta, StoryObj } from "@storybook/react-vite";
import type { PromptConfig } from "@vultus/core";
import { type PromptAttachment, PromptInput } from "@vultus/web";
import { useState } from "react";
import { expect, fn, userEvent, within } from "storybook/test";
import { SLACK_COMPOSER, webOnly } from "../../support";

const config: PromptConfig = {
  placeholder: "Ask about an incident, a deploy, or a runbook",
  suggestions: [
    { title: "Why is payments-api erroring?", message: "Why is payments-api returning 5xx?" },
    { title: "Summarize today's deploys", message: "Summarize today's production deploys." },
    { title: "Who's on call?", message: "Who is on call for payments right now?" },
    { title: "Draft a status update", message: "Draft a status update for #incidents." },
  ],
};

type Args = {
  running: boolean;
  disabled: boolean;
  showSuggestions: boolean;
  placeholder: string;
  onSubmit: (message: string) => void;
  onStop: () => void;
};

const meta = {
  title: "Web-only/PromptInput",
  component: PromptInput,
  tags: ["web-only"],
  parameters: {
    ...webOnly(SLACK_COMPOSER),
    docs: {
      description: {
        component: `The web prompt bar: autosizing input, suggestion cards, optional attachments, and Send that becomes Stop while a turn runs (Escape stops too). Web only. Slack uses ${SLACK_COMPOSER}.`,
      },
    },
  },
  args: {
    running: false,
    disabled: false,
    showSuggestions: false,
    placeholder: config.placeholder ?? "",
    onSubmit: fn(),
    onStop: fn(),
  },
  render: ({ placeholder, ...args }) => (
    <div className="mx-auto max-w-3xl">
      <PromptInput {...args} config={{ ...config, placeholder }} />
    </div>
  ),
} satisfies Meta<Args>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  play: async ({ args, canvasElement }) => {
    const canvas = within(canvasElement);
    const input = canvas.getByRole("textbox", { name: "Message" });
    const send = canvas.getByRole("button", { name: "Send message" });
    await expect(send).toBeDisabled();
    // Shift+Enter adds a line; Enter sends the trimmed text and clears the input.
    await userEvent.type(input, "Roll back #4812{Shift>}{Enter}{/Shift}then page on-call");
    await expect(input).toHaveValue("Roll back #4812\nthen page on-call");
    await userEvent.keyboard("{Enter}");
    await expect(args.onSubmit).toHaveBeenCalledWith("Roll back #4812\nthen page on-call");
    await expect(input).toHaveValue("");
  },
};

export const WithSuggestions: Story = {
  name: "With suggestions",
  args: { showSuggestions: true },
  play: async ({ args, canvasElement }) => {
    await userEvent.click(within(canvasElement).getByRole("button", { name: /Who's on call/ }));
    await expect(args.onSubmit).toHaveBeenCalledWith("Who is on call for payments right now?");
  },
};

export const Running: Story = {
  name: "Running (Stop)",
  args: { running: true },
  play: async ({ args, canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Stop generating" }));
    await expect(args.onStop).toHaveBeenCalledTimes(1);
    await userEvent.type(canvas.getByRole("textbox", { name: "Message" }), "{Escape}");
    await expect(args.onStop).toHaveBeenCalledTimes(2);
    await expect(args.onSubmit).not.toHaveBeenCalled();
  },
};

export const Disabled: Story = { args: { disabled: true } };

function WithFiles(args: Args) {
  const [files, setFiles] = useState<PromptAttachment[]>([
    { id: "1", name: "payments-5xx.png" },
    { id: "2", name: "deploy-4812.log" },
  ]);
  return (
    <div className="mx-auto max-w-3xl">
      <PromptInput
        {...args}
        config={config}
        attachments={files}
        onAttach={() =>
          setFiles((f) => [...f, { id: String(f.length + 1), name: `notes-${f.length + 1}.md` }])
        }
        onRemoveAttachment={(id) => setFiles((f) => f.filter((x) => x.id !== id))}
      />
    </div>
  );
}

export const WithAttachments: Story = {
  name: "With attachments",
  render: (args) => <WithFiles {...args} />,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Remove deploy-4812.log" }));
    await expect(canvas.queryByText("deploy-4812.log")).toBeNull();
    await userEvent.click(canvas.getByRole("button", { name: "Attach files" }));
    await expect(canvas.getByText("notes-2.md")).toBeInTheDocument();
  },
};
