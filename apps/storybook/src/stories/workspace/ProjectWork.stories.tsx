import { defaultCopy } from "@humagen/components";
import { FolderIcon } from "@humagen/render-web";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { type ComponentProps, useEffect, useState } from "react";
import { expect, fn, userEvent, within } from "storybook/test";
import { setAgentHues } from "../../../../web/src/agentHues.ts";
import type { Me, Preferences, ProjectView, ThreadSummary } from "../../../../web/src/api.ts";
import { ProjectWork } from "../../../../web/src/ProjectWork.tsx";
import { href, PROJECT_TABS, type ProjectTab, type Route } from "../../../../web/src/route.ts";
import { DrawerButton, Sidebar } from "../../../../web/src/Sidebar.tsx";
import { AgentMark, Page, PageActions, Tabs } from "../../../../web/src/ui.tsx";

const now = Date.now();
const thread = (
  id: string,
  title: string,
  agents: string[],
  people: string[],
  active = 0,
  failed = 0,
  minutes = 12,
  project = "northstar",
): ThreadSummary => ({
  id,
  title,
  named: true,
  origin: "api",
  project,
  channel: "general",
  agents,
  people,
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
  thread(
    "research",
    "Compare supplier lead times for the launch",
    ["research"],
    ["ada", "sam"],
    1,
    0,
    2,
  ),
  thread("pricing", "Check the revised pricing model", ["analyst"], ["ada"], 0, 1, 8),
  thread(
    "brief",
    "Draft the launch brief",
    ["writer", "research"],
    ["ada", "sam", "lee", "kim"],
    0,
    0,
    24,
  ),
  thread("sources", "Find the latest customer interview notes", ["research"], ["sam"], 0, 0, 48),
];
/** Other projects' work, so the sidebar groups conversations the way the console does. */
const elsewhere = [
  thread("retro", "Summarize the Q3 retro", ["writer"], ["ada"], 0, 0, 90, "ops"),
  thread("plan", "Plan next week", ["analyst"], ["ada"], 0, 0, 180, "ada-personal"),
];

const me: Me = {
  personal_project: "ada-personal",
  principal: {
    subject: "ada",
    tenant: "northstar",
    kind: "user",
    name: "Ada Chen",
    roles: ["member"],
  },
  permissions: [],
  handle: "ada",
};
const prefs: Preferences = {
  display_name: "Ada Chen",
  theme: "system",
  accent: "red",
  has_photo: false,
  photo_version: 0,
  pinned: ["pricing"],
  pinned_projects: ["northstar"],
  muted: [],
};
const project = (name: string, title: string): ProjectView =>
  ({
    name,
    title,
    kind: "team",
    depth: 1,
    path: [{ name, kind: "team" }],
  }) as unknown as ProjectView;
const projects = [project("northstar", "Northstar launch"), project("ops", "Operations")];
const company: Route = { page: "project", name: "", tab: "inbox" };

/**
 * The sidebar pages its conversations from the API. Previews answer that one
 * request from the fixtures; nothing else leaves the page.
 */
function answerThreads() {
  const real = window.fetch;
  window.fetch = async (input, init) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
    if (new URL(url, location.href).pathname === "/api/threads") {
      return new Response(JSON.stringify({ threads: [...threads, ...elsewhere], next: null }), {
        headers: { "Content-Type": "application/json" },
      });
    }
    return real(input, init);
  };
  return () => {
    window.fetch = real;
  };
}

type Args = ComponentProps<typeof ProjectWork> & { phone?: boolean };

function Workbench({ phone, ...props }: Args) {
  const [tab, setTab] = useState<ProjectTab>("overview");
  const [route, setRoute] = useState<Route>({
    page: "project",
    name: "northstar",
    tab: "overview",
  });
  const [open, setOpen] = useState(!phone);
  const [pinned, setPinned] = useState(prefs.pinned);
  const [pinnedProjects, setPinnedProjects] = useState(prefs.pinned_projects ?? []);
  const onNavigate = fn();
  useEffect(() => {
    setAgentHues({ research: 255, analyst: 155, writer: 35 });
  }, []);
  const go = (next: Route) => {
    onNavigate(next);
    setRoute(next);
    if (next.page === "conversation") props.onOpenThread(next.id);
    if (phone) setOpen(false);
  };
  const words = defaultCopy.projects;
  const tabLabel = (id: ProjectTab) =>
    words.tab[id as keyof typeof words.tab] ??
    defaultCopy.project.tabs[id as keyof typeof defaultCopy.project.tabs] ??
    id;
  const projectPinned = pinnedProjects.includes("northstar");
  return (
    <div
      className={`relative mx-auto flex h-dvh min-h-0 overflow-hidden bg-aui-bg ${phone ? "max-w-[390px] border-x border-aui-border" : "w-full"}`}
    >
      <Sidebar
        open={open}
        onToggle={() => setOpen((o) => !o)}
        route={route}
        go={go}
        me={me}
        prefs={{ ...prefs, pinned, pinned_projects: pinnedProjects }}
        can={() => true}
        threads={threads}
        projects={projects}
        workspaceName="Northstar"
        pinned={pinned}
        onPin={async (id, pin) =>
          setPinned((list) =>
            pin ? [id, ...list.filter((p) => p !== id)] : list.filter((p) => p !== id),
          )
        }
        onPinProject={async (name, pin) =>
          setPinnedProjects((list) =>
            pin ? [name, ...list.filter((p) => p !== name)] : list.filter((p) => p !== name),
          )
        }
        inboxCount={2}
        live
        onSignOut={onNavigate}
        onSearch={onNavigate}
      />
      {phone && <DrawerButton open={open} inboxCount={2} onOpen={() => setOpen(true)} />}
      <main
        className={`flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden ${phone ? "pt-[calc(2.5rem+env(safe-area-inset-top))]" : "max-md:pt-[calc(2.5rem+env(safe-area-inset-top))]"}`}
      >
        <Page
          title="Northstar launch"
          icon={<FolderIcon size={28} />}
          meta={
            <span className="inline-flex flex-wrap items-center gap-x-1.5">
              <span>{defaultCopy.project.kind("team")}</span>
              <span className="inline-flex items-center gap-1.5">
                <span>{words.lead}</span>
                <span className="inline-flex items-center gap-1">
                  <AgentMark name="research" size={12} />
                  research
                </span>
              </span>
            </span>
          }
          crumbs={
            <nav aria-label={words.breadcrumb}>
              <ol className="flex flex-wrap items-center gap-x-1 text-aui-xs text-aui-muted">
                <li className="flex items-center gap-1">
                  <a
                    href={href(company)}
                    onClick={(e) => {
                      e.preventDefault();
                      go(company);
                    }}
                    className="inline-flex min-h-6 items-center rounded-aui hover:text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
                  >
                    {words.companyName}
                  </a>
                  <span aria-hidden>›</span>
                </li>
                <li aria-current="page" className="text-aui-fg">
                  Northstar launch
                </li>
              </ol>
            </nav>
          }
          lede="A shared place for the launch brief, supplier research and decisions."
          back={{ label: words.allProjects, onClick: () => go({ page: "projects" }) }}
          actions={
            <PageActions
              primary={{ label: words.newTask, onSelect: () => go({ page: "new" }) }}
              actions={[
                {
                  label: projectPinned
                    ? defaultCopy.pins.unpinProject
                    : defaultCopy.pins.pinProject,
                  onSelect: () =>
                    setPinnedProjects((list) =>
                      projectPinned
                        ? list.filter((p) => p !== "northstar")
                        : ["northstar", ...list],
                    ),
                },
                { label: defaultCopy.announcements.mute, onSelect: onNavigate },
                { label: words.editProject, onSelect: onNavigate },
              ]}
            />
          }
          tabs={
            <Tabs
              label="Northstar launch"
              value={tab}
              tabs={PROJECT_TABS.map((id) => ({ id, label: tabLabel(id) }))}
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
          ) : tab === "agents" ? (
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
          ) : (
            <p className="max-w-2xl text-aui-sm text-aui-muted">
              {tabLabel(tab)} reads live workspace data. This preview covers Overview, Context and
              Agents.
            </p>
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
          "The console's actual project-work renderer, sidebar, page header and tabs, with isolated example data. Compare light/dark themes and phone/desktop layouts. Filters apply only to loaded rows; failed reads never become empty projects. Story actions are logged locally and do not call a model or save workspace data. Design references: Harvey II workspace structure, Codex agent oversight, Linear interaction consistency, Dust working prototypes.",
      },
    },
  },
  args: { threads, project: "northstar", onOpenThread: fn(), onRetry: fn(), onMore: fn() },
  beforeEach: answerThreads,
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
    const work = within(canvas.getByRole("main")).getByRole("region", { name: "Conversations" });
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
