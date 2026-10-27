import { defaultCopy } from "@humagen/components";
import { messageState } from "@humagen/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { useMemo, useState } from "react";
import { expect, userEvent, within } from "storybook/test";
import { Monitor } from "../../../../web/src/Monitor.tsx";
import type { PlatformMessage, Transport } from "../../../../web/src/mail.ts";
import { NotificationsPage } from "../../../../web/src/Notifications.tsx";
import { href, type Route } from "../../../../web/src/route.ts";
import { WorkspaceTabs } from "../../../../web/src/WorkspaceTabs.tsx";
import { hubTabs } from "../../../../web/src/workspace-navigation.ts";

const at = new Date().toISOString();
const base = {
  at,
  from: { kind: "person" as const, name: "Ada" },
  sender: "ada",
  about: [],
  budgets: {},
};
const fixtures: PlatformMessage[] = [
  {
    ...base,
    id: "review",
    kind: "clarification",
    template: "question",
    subject: "Confirm the launch owner",
    unread: true,
    parts: [{ id: "owner", type: "reply", prompt: "Who should own the launch checklist?" }],
  },
  {
    ...base,
    id: "file",
    kind: "artifact",
    template: "artifact",
    subject: "The launch checklist is ready",
    parts: [
      {
        id: "file-note",
        type: "text",
        text: "The checklist now includes supplier dates and the handover notes.",
      },
    ],
  },
  {
    ...base,
    id: "done",
    kind: "mention",
    template: "mention",
    subject: "Release notes were reviewed",
    dismissedAt: at,
    parts: [{ id: "done-note", type: "text", text: "The release notes are ready to share." }],
  },
];

type Props = { state?: "ready" | "empty" | "failed"; phone?: boolean };
function NotificationsWorkbench({ state = "ready", phone = false }: Props) {
  const [route, go] = useState<Route>({ page: "notifications" });
  const [kind, setKind] = useState("");
  const via = useMemo<Transport>(() => {
    let records = structuredClone(fixtures);
    return {
      list: async (query) => {
        if (state === "failed") throw new Error("The preview service is unavailable.");
        return {
          messages:
            state === "empty"
              ? []
              : records.filter(
                  (message) =>
                    (!query.kind || message.kind === query.kind) &&
                    (query.state === "all" ||
                      (query.state === "open"
                        ? messageState(message) === "open"
                        : messageState(message) !== "open")),
                ),
          next: null,
          open:
            state === "empty"
              ? 0
              : records.filter((message) => messageState(message) === "open").length,
          unread: 1,
        };
      },
      get: async (id) => {
        const message = records.find((message) => message.id === id);
        if (!message) throw new Error("Message unavailable");
        return message;
      },
      send: async (message, request) => {
        const next = { ...message, unread: false };
        if (request.call === "dismiss") next.dismissedAt = new Date().toISOString();
        if (request.call === "snooze") next.snoozedUntil = request.until;
        if (request.call === "unhide") {
          next.dismissedAt = undefined;
          next.snoozedUntil = undefined;
        }
        if (request.call === "resolve")
          next.parts = next.parts.map((part) =>
            part.id === request.partId
              ? {
                  ...part,
                  resolution: {
                    outcome: "answered",
                    by: "Ada",
                    at: new Date().toISOString(),
                    detail: String(request.values?.text ?? "Confirmed"),
                  },
                }
              : part,
          );
        records = records.map((record) => (record.id === next.id ? next : record));
        return next;
      },
    };
  }, [state]);
  const rules = route.page === "monitor";
  return (
    <div
      className={`mx-auto flex h-dvh min-h-0 flex-col overflow-hidden bg-aui-bg ${phone ? "max-w-[390px] border-x border-aui-border" : "w-full"}`}
    >
      <WorkspaceTabs
        label="Notifications"
        items={hubTabs("notifications", route, () => true)}
        current={href(route)}
        onGo={go}
      />
      {rules ? (
        <Monitor
          embeddedRules
          tab="rules"
          alerts={[]}
          rules={[]}
          agents={[]}
          canWrite={false}
          onChanged={() => {}}
        />
      ) : (
        <NotificationsPage
          signal={0}
          viewer="Ada"
          via={via}
          onGo={go}
          onOpen={() => {}}
          kind={kind}
          onKind={setKind}
        />
      )}
    </div>
  );
}

const meta = {
  title: "Workspace/Notifications",
  component: NotificationsWorkbench,
  parameters: {
    layout: "fullscreen",
    workspace: true,
    docs: {
      description: {
        component:
          "The real Notifications page and shared navigation, backed by an isolated preview transport. Requests and updates share one list. One Filter panel contains status and type; Alert Rules is the only companion tab. Preview actions do not call a workspace API.",
      },
    },
  },
} satisfies Meta<typeof NotificationsWorkbench>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Unified: Story = {};
export const Light: Story = { globals: { theme: "light" } };
export const Phone: Story = { args: { phone: true } };
export const Empty: Story = { args: { state: "empty" } };
export const ReadFailed: Story = { args: { state: "failed" } };
export const Filters: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(
      await canvas.findByRole("button", { name: defaultCopy.inboxControls.notificationFilter }),
    );
    const panel = within(
      canvas.getByRole("dialog", { name: defaultCopy.inboxControls.notificationFilter }),
    );
    await expect(panel.getByRole("combobox", { name: "Status" })).toHaveValue("all");
    await expect(panel.getByRole("combobox", { name: "Type" })).toHaveValue("");
  },
};
export const AlertRules: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("link", { name: "Alert Rules" }));
    await expect(canvas.getByRole("heading", { name: "Alert Rules" })).toBeVisible();
  },
};
