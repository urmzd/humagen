import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ConnectedAccounts, LinkedPeople } from "./ConnectedAccounts.tsx";
import { Connections } from "./Connections.tsx";
import type { ConnectionPreset } from "./connectionApi.ts";
import { signInAddress, takeConsentReturn } from "./consentReturn.ts";
import { stubFetch } from "./dom.ts";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
  sessionStorage.clear();
  window.history.replaceState(null, "", "/");
});

const github = (over: Partial<ConnectionPreset> = {}): ConnectionPreset => ({
  id: "github-account",
  name: "GitHub",
  kind: "account_oauth",
  available: true,
  connected: false,
  scope: "person",
  ...over,
});
const slack = (over: Partial<ConnectionPreset> = {}): ConnectionPreset => ({
  id: "slack-account",
  name: "Slack",
  kind: "account_oauth",
  available: true,
  connected: false,
  scope: "person",
  ...over,
});
const linkedGitHub = github({
  connected: true,
  account: {
    surface: "github",
    id: "42",
    login: "octocat",
    avatar_url: "https://avatars.githubusercontent.com/u/42",
    subject: "ana",
    linked_at: "2026-11-22T12:00:00Z",
  },
});

test("each surface is one row with one control: Connect, or the account and Disconnect", () => {
  const connected: string[] = [];
  const disconnected: string[] = [];
  render(
    <ConnectedAccounts
      presets={[linkedGitHub, slack()]}
      onConnect={(p) => connected.push(p.id)}
      onDisconnect={(p) => disconnected.push(p.id)}
    />,
  );
  const list = screen.getByRole("list", { name: "Connected accounts" });
  const gh = within(list).getByRole("listitem", { name: "GitHub" });
  assert.ok(within(gh).getByText("@octocat"));
  assert.equal(
    gh.querySelector("img")?.getAttribute("src"),
    "https://avatars.githubusercontent.com/u/42",
  );
  assert.equal(within(gh).getAllByRole("button").length, 1);
  fireEvent.click(within(gh).getByRole("button", { name: "Disconnect GitHub" }));
  assert.deepEqual(disconnected, ["github-account"]);

  const sl = within(list).getByRole("listitem", { name: "Slack" });
  assert.ok(within(sl).getByText("Not connected"));
  assert.equal(within(sl).getAllByRole("button").length, 1);
  fireEvent.click(within(sl).getByRole("button", { name: "Connect Slack" }));
  assert.deepEqual(connected, ["slack-account"]);
});

test("a surface nobody set up is not shown, and nothing is shown with none", () => {
  const { container } = render(
    <ConnectedAccounts
      presets={[github({ available: false }), slack({ available: false })]}
      onConnect={() => {}}
      onDisconnect={() => {}}
    />,
  );
  assert.equal(container.textContent, "");
  cleanup();
  render(
    <ConnectedAccounts
      presets={[github(), slack({ available: false })]}
      onConnect={() => {}}
      onDisconnect={() => {}}
    />,
  );
  assert.deepEqual(
    screen.getAllByRole("listitem").map((li) => li.getAttribute("aria-label")),
    ["GitHub"],
  );
});

test("someone who may not connect sees the account and no control", () => {
  render(
    <ConnectedAccounts
      presets={[linkedGitHub]}
      canConnect={false}
      onConnect={() => {}}
      onDisconnect={() => {}}
    />,
  );
  assert.ok(screen.getByText("@octocat"));
  assert.equal(screen.queryAllByRole("button").length, 0);
});

const page = (presets: () => ConnectionPreset[], extra: Record<string, () => unknown> = {}) =>
  stubFetch({
    "GET /api/connections/presets": presets,
    "GET /api/me/credentials": () => ({
      providers: [],
      references: [],
      values: false,
      allowed_hosts: [],
      oauth_available: true,
    }),
    "GET /api/me/context": () => ({ run: {}, version: 0, tool_servers: [] }),
    "GET /api/clients": () => ({ clients: [], caps: { max: 10, max_per_surface: 10 } }),
    "GET /api/library/skills": () => ({ skills: [] }),
    ...extra,
  });

test("Disconnect unlinks the account and the row offers Connect again", async () => {
  let linked = true;
  net = page(() => [linked ? linkedGitHub : github()], {
    "DELETE /api/connections/github-account": () => {
      linked = false;
      return undefined;
    },
  });
  render(<Connections />);
  fireEvent.click(await screen.findByRole("button", { name: "Disconnect GitHub" }));
  await screen.findByRole("button", { name: "Connect GitHub" });
  assert.ok(
    net.seen.some((r) => r.method === "DELETE" && r.path === "/api/connections/github-account"),
  );
});

test("the consent's return completes the link with its state and code", async () => {
  let linked = false;
  net = page(() => [linked ? linkedGitHub : github()], {
    "POST /api/connections/github-account/callback": () => {
      linked = true;
      return undefined;
    },
  });
  sessionStorage.setItem("humagen.connection.provider", "github-account");
  window.history.replaceState(null, "", "/connections/callback?state=s1&code=c1");
  render(<Connections provider="callback" />);
  await screen.findByText("@octocat");
  const sent = net.seen.find((r) => r.path === "/api/connections/github-account/callback");
  assert.deepEqual(sent?.body, { state: "s1", code: "c1" });
  // The code is gone from the address before anything else reads it.
  assert.equal(window.location.search, "");
});

test("the consent's return leaves the address before the console draws, and is completed once", async () => {
  let posts = 0;
  net = page(() => [posts ? linkedGitHub : github()], {
    "POST /api/connections/github-account/callback": () => {
      posts++;
      return undefined;
    },
  });
  sessionStorage.setItem("humagen.connection.provider", "github-account");
  window.history.replaceState(null, "", "/connections/callback?state=s1&code=c1&installation_id=9");
  const before = window.history.length;
  takeConsentReturn(window);
  // Nothing drawn yet, and the address already holds no authorization.
  assert.equal(window.location.pathname, "/connections");
  assert.equal(window.location.search, "");
  assert.equal(window.history.length, before);
  render(<Connections />);
  await screen.findByText("@octocat");
  const sent = net.seen.filter((r) => r.path === "/api/connections/github-account/callback");
  assert.deepEqual(
    sent.map((r) => r.body),
    [{ state: "s1", code: "c1", installation_id: "9" }],
  );
  cleanup();
  // Drawn again, nothing is completed a second time.
  render(<Connections />);
  await screen.findByText("@octocat");
  assert.equal(posts, 1);
});

test("signing in again does not carry a consent's return", () => {
  assert.equal(
    signInAddress({
      pathname: "/connections/callback",
      search: "?state=s1&code=c1&iss=x&error=denied",
    }),
    `/auth/login?return_to=${encodeURIComponent("/connections/callback")}`,
  );
  assert.equal(
    signInAddress({ pathname: "/connections/callback", search: "?code=c1&view=x" }),
    `/auth/login?return_to=${encodeURIComponent("/connections/callback")}`,
  );
  // Any other page comes back as it was.
  assert.equal(
    signInAddress({ pathname: "/sessions", search: "?state=open" }),
    `/auth/login?return_to=${encodeURIComponent("/sessions?state=open")}`,
  );
});

test("a claim another person holds is said on the page", async () => {
  net = page(() => [github()], {
    "POST /api/connections/github-account/callback": () =>
      new Response(
        JSON.stringify({
          error:
            "this account is linked to another person in this workspace; they must disconnect it first",
        }),
        { status: 409 },
      ),
  });
  sessionStorage.setItem("humagen.connection.provider", "github-account");
  window.history.replaceState(null, "", "/connections/callback?state=s1&code=c1");
  render(<Connections provider="callback" />);
  await waitFor(() => assert.ok(screen.getByText(/linked to another person in this workspace/)));
  assert.ok(screen.getByRole("button", { name: "Connect GitHub" }));
});

test("an app lists who linked an account there, and nothing before anyone has", async () => {
  net = stubFetch({
    "GET /api/clients/gh/accounts": () => ({
      accounts: [
        {
          surface: "github",
          id: "42",
          login: "octocat",
          subject: "ana",
          linked_at: "2026-11-22T12:00:00Z",
        },
      ],
    }),
    "GET /api/clients/empty/accounts": () => ({ accounts: [] }),
  });
  render(<LinkedPeople client="gh" />);
  const list = await screen.findByRole("list", { name: "Linked people" });
  assert.ok(within(list).getByText("@octocat"));
  assert.ok(within(list).getByText("ana"));
  cleanup();
  const { container } = render(<LinkedPeople client="empty" />);
  await waitFor(() => assert.ok(net?.seen.some((r) => r.path === "/api/clients/empty/accounts")));
  assert.equal(container.textContent, "");
});

test("an administrator disconnects someone's account from the app's list", async () => {
  let linked = true;
  net = stubFetch({
    "GET /api/clients/gh/accounts": () => ({
      accounts: linked
        ? [
            {
              surface: "github",
              id: "42",
              login: "octocat",
              subject: "ana",
              linked_at: "2026-11-22T12:00:00Z",
            },
          ]
        : [],
    }),
    "DELETE /api/clients/gh/accounts/42": () => {
      linked = false;
      return undefined;
    },
  });
  const { container } = render(<LinkedPeople client="gh" canUnlink />);
  const row = await screen.findByRole("listitem", { name: "@octocat" });
  assert.equal(within(row).getAllByRole("button").length, 1);
  fireEvent.click(within(row).getByRole("button", { name: "Disconnect @octocat" }));
  await waitFor(() => assert.equal(container.textContent, ""));
  assert.ok(
    net.seen.some((r) => r.method === "DELETE" && r.path === "/api/clients/gh/accounts/42"),
  );
  cleanup();
  linked = true;
  render(<LinkedPeople client="gh" />);
  await screen.findByRole("listitem", { name: "@octocat" });
  assert.equal(screen.queryAllByRole("button").length, 0);
});
