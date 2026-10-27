import { defaultCopy } from "@humagen/components";
import {
  BellIcon,
  BotIcon,
  ComposeIcon,
  DatabaseIcon,
  FolderIcon,
  ProjectIcon,
  SearchIcon,
  ShieldIcon,
  UsersIcon,
} from "@humagen/render-web";
import { HumagenMark } from "@humagen/web";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { type ComponentProps, type ReactNode, useEffect, useState } from "react";
import { expect, fn, userEvent, within } from "storybook/test";
import { setAgentHues } from "../../../../web/src/agentHues.ts";
import type { ThreadSummary } from "../../../../web/src/api.ts";
import { ProjectWork } from "../../../../web/src/ProjectWork.tsx";
import { SidebarItem } from "../../../../web/src/Sidebar.tsx";
import { AgentMark, Page, primary, Tabs } from "../../../../web/src/ui.tsx";

const now = Date.now();
const thread = (
  id: string,
  title: string,
  agent: string,
  active = 0,
  failed = 0,
  minutes = 12,
): ThreadSummary => ({
  id,
  title,
  named: true,
  origin: "api",
  project: "Northstar",
  channel: "general",
  agents: [agent],
  people: ["ada"],
  visibility: "shared",
  messages: 2,
  turns: 3,
  branches: 0,
  active,
  failed,
  created_at: new Date(now - 86_400_000).toISOString(),
  updated_at: new Date(now - minutes * 60_000).toISOString(),
});
const threads = [
  thread("research", "Compare supplier lead times for the launch", "research", 1, 0, 2),
  thread("pricing", "Check the revised pricing model", "analyst", 0, 1, 8),
  thread("brief", "Draft the launch brief", "writer", 0, 0, 24),
  thread("sources", "Find the latest customer interview notes", "research", 0, 0, 48),
];

type Args = ComponentProps<typeof ProjectWork> & { phone?: boolean };

function Workbench({ phone, ...props }: Args) {
  const [tab, setTab] = useState("overview");
  const onNavigate = fn();
  useEffect(() => {
    setAgentHues({ research: 255, analyst: 155, writer: 35 });
  }, []);
  const nav = (label: string, icon: ReactNode, active = false) => (
    <li key={label}>
      <SidebarItem icon={icon} label={label} href="#" active={active} open onClick={onNavigate} />
    </li>
  );
  return (
    <div
      className={`mx-auto flex h-dvh min-h-0 overflow-hidden bg-aui-bg ${phone ? "max-w-[390px] border-x border-aui-border" : "w-full"}`}
    >
      <aside
        aria-label="Workspace navigation"
        className={`humagen-glass-bar flex w-60 shrink-0 flex-col border-r border-aui-border ${phone ? "hidden" : "max-md:hidden"}`}
      >
        <div className="flex h-16 items-center gap-2.5 px-5 text-aui-base font-semibold">
          <HumagenMark size={23} />
          humagen
        </div>
        <div className="aui-scroll min-h-0 flex-1 overflow-y-auto px-2">
          <ul className="flex flex-col gap-0.5">
            {nav(
              defaultCopy.navigation.newConversation,
              <span aria-hidden className="text-lg">
                +
              </span>,
            )}
          </ul>
          <button
            type="button"
            onClick={onNavigate}
            className="mb-2 flex min-h-10 w-full items-center gap-2 rounded-aui px-2.5 text-aui-xs text-aui-muted hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            <SearchIcon size={16} />
            Search work<kbd className="ml-auto">⌘ K</kbd>
          </button>
          <ul className="flex flex-col gap-0.5">
            {nav("Projects", <ProjectIcon />, true)}
            {nav("People", <UsersIcon />)}
            {nav("Library", <DatabaseIcon />)}
            {nav("Notifications", <BellIcon />)}
            {nav("Agents", <BotIcon />)}
            {nav("Operations", <ShieldIcon />)}
          </ul>
          <section aria-label="Recent conversations" className="mt-7 px-2.5">
            <h2 className="mb-3 text-aui-xs font-medium text-aui-muted">Conversations</h2>
            {threads.slice(0, 3).map((t) => (
              <button
                key={t.id}
                type="button"
                onClick={() => props.onOpenThread(t.id)}
                className="mb-1 flex min-h-10 w-full items-center gap-2 rounded-aui text-left text-aui-xs text-aui-muted hover:text-aui-fg"
              >
                <AgentMark name={t.agents[0] ?? "research"} size={13} />
                <span className="truncate">{t.title}</span>
              </button>
            ))}
          </section>
        </div>
        <div className="mx-4 border-t border-aui-border py-4 text-aui-xs text-aui-muted">
          Ada Chen<span className="mt-1 block text-aui-2xs">Northstar workspace</span>
        </div>
      </aside>
      <main className="flex min-h-0 min-w-0 flex-1 flex-col">
        <Page
          title="Northstar launch"
          icon={<FolderIcon size={27} />}
          meta="Team project"
          lede="A shared place for the launch brief, supplier research and decisions."
          back={{ label: "All projects", onClick: onNavigate }}
          actions={
            <button type="button" className={primary} onClick={onNavigate}>
              <ComposeIcon size={15} />
              New Conversation
            </button>
          }
          tabs={
            <Tabs
              label="Northstar launch"
              value={tab}
              tabs={[
                { id: "overview", label: "Overview" },
                { id: "context", label: "Context" },
                { id: "agents", label: "Agents" },
              ]}
              onChange={setTab}
            />
          }
        >
          {tab === "overview" ? (
            <div className="grid items-start gap-8 @4xl:grid-cols-[minmax(0,1fr)_17rem]">
              <ProjectWork {...props} />
              <aside className="humagen-project-context flex flex-col gap-6">
                <section>
                  <h2 className="text-aui-sm font-semibold">Shared notes</h2>
                  <p className="mt-2 text-aui-sm leading-relaxed text-aui-muted">
                    Launch on November 12. Use the approved supplier shortlist and link supporting
                    evidence in every recommendation.
                  </p>
                  <button
                    type="button"
                    onClick={() => setTab("context")}
                    className="mt-3 min-h-8 text-aui-xs font-medium underline underline-offset-4"
                  >
                    View Context
                  </button>
                </section>
                <section className="border-t border-aui-border pt-5">
                  <h2 className="text-aui-sm font-semibold">Scheduled</h2>
                  <p className="mt-2 text-aui-xs text-aui-muted">Weekly launch recap</p>
                  <p className="mt-1 text-aui-xs text-aui-muted">Every Monday at 9:00 AM</p>
                </section>
              </aside>
            </div>
          ) : tab === "context" ? (
            <section className="max-w-2xl">
              <h2 className="text-aui-base font-semibold">Project instructions</h2>
              <p className="mt-3 text-aui-sm leading-relaxed text-aui-muted">
                Launch on November 12. Use the approved supplier shortlist and link supporting
                evidence in every recommendation.
              </p>
            </section>
          ) : (
            <section>
              <h2 className="text-aui-base font-semibold">Available here</h2>
              <ul className="mt-4 flex flex-wrap gap-6">
                {["research", "analyst", "writer"].map((agent) => (
                  <li key={agent} className="flex items-center gap-2 text-aui-sm">
                    <AgentMark name={agent} size={20} />
                    {agent}
                  </li>
                ))}
              </ul>
            </section>
          )}
        </Page>
      </main>
    </div>
  );
}

const meta = {
  title: "Workspace/Project work",
  component: ProjectWork,
  parameters: {
    workspace: true,
    docs: {
      description: {
        component:
          "The console's actual project-work renderer and navigation components, with isolated example data. Compare light/dark themes and phone/desktop layouts. Filters apply only to loaded rows; failed reads never become empty projects. Story actions are logged locally and do not call a model or save workspace data. Design references: Harvey II workspace structure, Codex agent oversight, Linear interaction consistency, Dust working prototypes.",
      },
    },
  },
  args: { threads, project: "Northstar", onOpenThread: fn(), onRetry: fn(), onMore: fn() },
  render: (args) => <Workbench {...args} />,
} satisfies Meta<Args>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Overview: Story = {};
export const Light: Story = { globals: { theme: "light" } };
export const Phone: Story = { args: { phone: true }, globals: { theme: "light" } };
export const Empty: Story = { args: { threads: [] } };
export const Loading: Story = { args: { threads: null } };
export const ReadFailed: Story = {
  args: { threads: null, failed: true },
  play: async ({ canvasElement, args }) => {
    const canvas = within(canvasElement);
    await expect(canvas.getByRole("alert")).toHaveTextContent(defaultCopy.projectWork.failed);
    await expect(canvas.queryByText(defaultCopy.projectWork.empty)).toBeNull();
    await userEvent.click(canvas.getByRole("button", { name: "Try Again" }));
    await expect(args.onRetry).toHaveBeenCalled();
  },
};
export const OlderPageFailed: Story = { args: { hasMore: true, moreFailed: true } };
export const FilterAndOpen: Story = {
  play: async ({ canvasElement, args }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Needs Attention 1" }));
    const work = canvas.getByRole("region", { name: "Conversations" });
    await expect(within(work).queryByText("Draft the launch brief")).toBeNull();
    await userEvent.click(
      within(work).getByRole("button", { name: /Check the revised pricing model/ }),
    );
    await expect(args.onOpenThread).toHaveBeenCalledWith("pricing");
  },
};
export const Navigation: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.getByRole("link", { name: "Library" })).toBeVisible();
    await expect(canvas.getByRole("link", { name: "Agents" })).toBeVisible();
    await expect(canvas.getByRole("link", { name: "Operations" })).toBeVisible();
    await expect(canvas.getByRole("link", { name: "Projects" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  },
};
