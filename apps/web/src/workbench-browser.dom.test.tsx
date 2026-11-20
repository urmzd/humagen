import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import {
  createHumanActivityStore,
  emptyResponse,
  type HumanActivityInput,
  type ResponseState,
  type ResponseTool,
  type Task,
} from "@humagen/core";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ApiError } from "./api.ts";
import type { BrowserAction, BrowserView } from "./browserApi.ts";
import { createBrowserShots, MAX_BROWSER_SESSIONS, MAX_BROWSER_SHOTS } from "./browserShots.ts";
import {
  type BrowserCall,
  browserNow,
  browserSteps,
  browserTool,
  callsOfResponses,
  callsOfTasks,
  pageOf,
} from "./browserSteps.ts";
import { WorkbenchBrowser } from "./WorkbenchBrowser.tsx";
import { BrowserCard } from "./WorkbenchBrowserCard.tsx";
import { WORKBENCH_TABS, WorkbenchOpen, type WorkbenchSession } from "./workbench.ts";

const tabWords = defaultCopy.workbenchBrowser;
const words = tabWords.step;
const thread = "s_browser";

afterEach(cleanup);

const tool = (id: string, name: string, more: Partial<ResponseTool> = {}): ResponseTool => ({
  type: "tool",
  id,
  name,
  kind: "tool" as ResponseTool["kind"],
  input: {},
  depth: 0,
  status: "done",
  content: [],
  ...more,
});
const page = (url: string, title: string) =>
  `### Ran Playwright code\n### Page\n- Page URL: ${url}\n- Page Title: ${title}\n### Snapshot\n- [Snapshot](out/page.yml)`;

/** A session whose agent went to a page, clicked, and failed to type. */
const worked: ResponseState[] = [
  {
    ...emptyResponse(),
    status: "done",
    content: [
      tool("c1", "browser__browser_navigate", {
        input: { url: "https://example.com" },
        result: {
          text: page("https://example.com/", "Example Domain"),
          sources: [],
          isError: false,
        },
      }),
      tool("c2", "browser__browser_click", {
        input: { element: "Learn more link", target: "e13" },
        result: { text: page("https://iana.org/help", "IANA help"), sources: [], isError: false },
      }),
      tool("c3", "browser__browser_type", {
        input: { element: "Search box", text: "secret" },
        status: "error",
        result: { text: "Error: Ref e99 not found", sources: [], isError: true },
      }),
    ],
  },
];

const view = (more: Partial<BrowserView> = {}): BrowserView => ({
  provider: "browser",
  action: "look",
  url: "https://iana.org/help",
  title: "IANA help",
  screenshot: { media_type: "image/png", data: "AAAA" },
  elements: [
    { ref: "e3", role: "link", name: "Domains", box: [10, 60, 82, 18] },
    { ref: "e4", role: "textbox", name: "Search", box: [10, 90, 200, 24] },
  ],
  ...more,
});

/** The tab, on a route that answers as told and keeps what it was asked. */
function tab(
  opts: {
    responses?: ResponseState[];
    canPost?: boolean;
    selection?: string;
    answer?: (action: BrowserAction) => BrowserView | Error;
    ask?: boolean;
  } = {},
) {
  const asked: BrowserAction[] = [];
  const records: HumanActivityInput[] = [];
  const selections: (string | undefined)[] = [];
  const opened: string[] = [];
  const seeds: string[] = [];
  const store = createHumanActivityStore();
  const shots = createBrowserShots();
  const canPost = opts.canPost ?? true;
  const session: WorkbenchSession = {
    thread,
    runs: [],
    responses: "responses" in opts ? opts.responses : worked,
    summary: undefined,
    canPost,
  };
  const out = render(
    <WorkbenchBrowser
      {...session}
      selection={opts.selection}
      onSelection={(ref) => selections.push(ref)}
      openTab={(id) => opened.push(id)}
      record={(input) => {
        records.push(input);
        return store.add(thread, input);
      }}
      ask={canPost && opts.ask !== false ? (text) => seeds.push(text) : undefined}
      expanded={false}
      shots={shots}
      act={async (session, action) => {
        assert.equal(session, thread);
        asked.push(action);
        const answer = (opts.answer ?? (() => view({ action: action.action })))(action);
        if (answer instanceof Error) throw answer;
        return answer;
      }}
    />,
  );
  return { asked, records, selections, opened, seeds, shots, ...out };
}

test("the tab is built: it is in the registry, fills the pane, and shows for a session that used a browser", () => {
  const entry = WORKBENCH_TABS.find((t) => t.id === "browser");
  assert.ok(entry);
  assert.equal(entry.fill, true);
  const session: WorkbenchSession = {
    thread,
    runs: [],
    responses: undefined,
    summary: undefined,
    canPost: true,
  };
  // Unknown is no: the summary is not read yet.
  assert.equal(entry.available?.(session), false);
  const summary = {
    environment: [{ kind: "browser", label: "Browser" }],
  } as WorkbenchSession["summary"];
  assert.equal(entry.available?.({ ...session, summary }), true);
  assert.equal(
    entry.available?.({
      ...session,
      summary: { ...summary, environment: [] } as WorkbenchSession["summary"],
    }),
    false,
  );
});

test("the steps are what the agent did, in order, with where each left the browser", () => {
  tab({ canPost: false });
  const steps = within(screen.getByTestId("browser-steps"));
  assert.deepEqual(
    steps.getAllByRole("listitem").map((li) => li.getAttribute("data-step")),
    ["c1", "c2", "c3"],
  );
  assert.ok(steps.getByText(words.navigate("https://example.com")));
  assert.ok(steps.getByText(words.click("Learn more link")));
  assert.ok(steps.getByText(tabWords.failed));
  // With no step named, the last is the open one, and says why it failed.
  assert.ok(steps.getByText(tabWords.stepOf(3, 3)));
  assert.ok(within(screen.getByTestId("browser-step-open")).getByText("Error: Ref e99 not found"));
  // The page the browser was left on heads the tab, with where it runs.
  assert.ok(screen.getByRole("heading", { name: "IANA help" }));
  assert.ok(screen.getByText(tabWords.card.on("browser")));
  // What the agent typed is not shown.
  assert.ok(!document.body.textContent?.includes("secret"));
});

test("a reader who may not post is shown the steps and nothing to act with", () => {
  const { asked } = tab({ canPost: false });
  assert.equal(screen.queryByRole("textbox", { name: tabWords.address }), null);
  assert.equal(screen.queryByRole("button", { name: tabWords.look }), null);
  assert.equal(screen.queryByRole("button", { name: tabWords.goThere }), null);
  // No picture is made up: the agent's own screenshots are not kept.
  assert.ok(within(screen.getByTestId("browser-no-shot")).getByText(tabWords.notKept));
  assert.equal(screen.queryByTestId("browser-shot"), null);
  assert.deepEqual(asked, []);
});

test("stepping back through the steps is an address", () => {
  const { selections } = tab({ selection: "c2" });
  const steps = within(screen.getByTestId("browser-steps"));
  assert.ok(steps.getByText(tabWords.stepOf(2, 3)));
  assert.equal(
    steps
      .getByRole("button", { current: "step" })
      .textContent?.includes(words.click("Learn more link")),
    true,
  );
  fireEvent.click(steps.getByRole("button", { name: tabWords.earlier }));
  fireEvent.click(steps.getByRole("button", { name: tabWords.later }));
  fireEvent.click(steps.getByText(words.navigate("https://example.com")));
  assert.deepEqual(selections, ["c1", "c3", "c1"]);
});

test("nothing reaches the browser until the person asks, and a screenshot is not something they did", async () => {
  const { asked, records, shots } = tab();
  assert.deepEqual(asked, []);
  assert.ok(within(screen.getByTestId("browser-no-shot")).getByText(tabWords.noShot));
  fireEvent.click(screen.getByRole("button", { name: tabWords.look }));
  const shot = await screen.findByTestId("browser-shot");
  assert.equal(shot.getAttribute("src"), "data:image/png;base64,AAAA");
  assert.equal(shot.getAttribute("alt"), tabWords.screenshot("IANA help"));
  // The steps name the browser's provider, so the request does too.
  assert.deepEqual(asked, [{ action: "look", provider: "browser" }]);
  assert.deepEqual(records, []);
  assert.equal(shots.list(thread).length, 1);
  // The address box follows the page.
  assert.equal(
    (screen.getByRole("textbox", { name: tabWords.address }) as HTMLInputElement).value,
    "https://iana.org/help",
  );
});

test("going to an address is done in the browser and recorded for the next message", async () => {
  const { asked, records } = tab({
    answer: (action) =>
      view({ action: action.action, url: "https://example.org/", title: "Example Org" }),
  });
  const box = screen.getByRole("textbox", { name: tabWords.address });
  fireEvent.change(box, { target: { value: "example.org" } });
  fireEvent.click(screen.getByRole("button", { name: tabWords.go }));
  await screen.findByTestId("browser-shot");
  assert.deepEqual(asked, [
    { action: "navigate", url: "https://example.org", provider: "browser" },
  ]);
  assert.deepEqual(records, [
    {
      source: "browser",
      summary: tabWords.did.navigate("https://example.org"),
      detail: "Page: Example Org\nhttps://example.org/",
    },
  ]);
  assert.ok(screen.getByRole("heading", { name: "Example Org" }));
});

test("picking something on the page and clicking it is one recorded action", async () => {
  const { asked, records } = tab();
  fireEvent.click(screen.getByRole("button", { name: tabWords.look }));
  await screen.findByTestId("browser-shot");
  const list = within(screen.getByTestId("browser-elements"));
  // Picking selects; nothing is pressed in the browser yet.
  fireEvent.click(list.getByRole("button", { name: /Domains/ }));
  assert.equal(asked.length, 1);
  const chosen = within(screen.getByTestId("browser-chosen"));
  assert.ok(chosen.getByText(tabWords.element("link", "Domains")));
  fireEvent.click(chosen.getByRole("button", { name: tabWords.click }));
  await waitFor(() => assert.equal(asked.length, 2));
  assert.deepEqual(asked[1], {
    action: "click",
    target: "e3",
    element: tabWords.element("link", "Domains"),
    provider: "browser",
  });
  await waitFor(() => assert.equal(records.length, 1));
  assert.equal(
    records[0]?.summary,
    tabWords.did.click(tabWords.element("link", "Domains"), "IANA help"),
  );
  assert.equal(records[0]?.source, "browser");
});

test("typing into a field is recorded by how much was typed, never by what", async () => {
  const { asked, records } = tab();
  fireEvent.click(screen.getByRole("button", { name: tabWords.look }));
  await screen.findByTestId("browser-shot");
  fireEvent.click(
    within(screen.getByTestId("browser-elements")).getByRole("button", { name: /Search/ }),
  );
  const chosen = within(screen.getByTestId("browser-chosen"));
  const what = tabWords.element("textbox", "Search");
  fireEvent.change(chosen.getByRole("textbox", { name: tabWords.typeInto(what) }), {
    target: { value: "hunter2" },
  });
  fireEvent.click(chosen.getByRole("checkbox", { name: tabWords.submit }));
  fireEvent.click(chosen.getByRole("button", { name: tabWords.type }));
  await waitFor(() => assert.equal(records.length, 1));
  assert.deepEqual(asked[1], {
    action: "type",
    target: "e4",
    element: what,
    text: "hunter2",
    submit: true,
    provider: "browser",
  });
  assert.equal(records[0]?.summary, tabWords.did.type(7, what, true));
  assert.ok(!JSON.stringify(records).includes("hunter2"));
});

test("an action the browser refused is said, and recorded as tried", async () => {
  const { records } = tab({
    answer: (action) =>
      action.action === "look" ? view() : new ApiError(502, "browser_click: Ref e3 not found"),
  });
  fireEvent.click(screen.getByRole("button", { name: tabWords.look }));
  await screen.findByTestId("browser-shot");
  fireEvent.click(
    within(screen.getByTestId("browser-elements")).getByRole("button", { name: /Domains/ }),
  );
  fireEvent.click(
    within(screen.getByTestId("browser-chosen")).getByRole("button", { name: tabWords.click }),
  );
  const alert = await screen.findByRole("alert");
  assert.equal(alert.textContent, "browser_click: Ref e3 not found");
  assert.equal(records.length, 1);
  assert.ok(records[0]?.summary.includes("Ref e3 not found"));
  // The page is still the one last seen.
  assert.ok(screen.getByTestId("browser-shot"));
});

test("a session with no browser to act on says so, takes the controls away and records nothing", async () => {
  const { records, asked } = tab({
    answer: () => new ApiError(404, "this session has no browser to act on"),
  });
  fireEvent.click(screen.getByRole("button", { name: tabWords.look }));
  const told = await screen.findByTestId("browser-unreachable");
  assert.equal(told.textContent, tabWords.noControl);
  assert.equal(screen.queryByRole("textbox", { name: tabWords.address }), null);
  assert.equal(screen.queryByRole("button", { name: tabWords.look }), null);
  assert.equal(screen.queryByRole("alert"), null);
  assert.deepEqual(records, []);
  assert.equal(asked.length, 1);
  // The steps are still read.
  assert.ok(screen.getByTestId("browser-steps"));
});

test("earlier screenshots taken here can be looked back through, and only the latest is acted on", async () => {
  let n = 0;
  tab({
    answer: (action) => {
      n += 1;
      return view({ action: action.action, title: `Page ${n}`, url: `https://example.com/${n}` });
    },
  });
  fireEvent.click(screen.getByRole("button", { name: tabWords.look }));
  await screen.findByRole("heading", { name: "Page 1" });
  fireEvent.click(screen.getByRole("button", { name: tabWords.refresh }));
  await screen.findByRole("heading", { name: "Page 2" });
  const strip = within(screen.getByTestId("browser-shots"));
  const taken = strip.getAllByRole("button");
  assert.equal(taken.length, 2);
  assert.equal(taken[0]?.getAttribute("aria-pressed"), "true");
  fireEvent.click(taken[1] as HTMLElement);
  assert.ok(screen.getByRole("heading", { name: "Page 1" }));
  assert.equal(
    screen.getByTestId("browser-shot").getAttribute("alt"),
    tabWords.screenshot("Page 1"),
  );
  // An earlier page's elements are gone from the browser: nothing to pick.
  assert.equal(screen.queryByTestId("browser-elements"), null);
  fireEvent.click(strip.getAllByRole("button")[0] as HTMLElement);
  assert.ok(screen.getByTestId("browser-elements"));
});

test("a session with no browser work has a title and one action", async () => {
  const first = tab({ responses: [] });
  const empty = within(screen.getByTestId("browser-empty"));
  assert.ok(empty.getByText(tabWords.none));
  assert.deepEqual(
    empty.getAllByRole("button").map((b) => b.textContent),
    [tabWords.look],
  );
  first.unmount();

  // One who may post but whose session has no browser is offered to ask for it.
  const second = tab({ responses: [], answer: () => new ApiError(404, "none") });
  fireEvent.click(screen.getByRole("button", { name: tabWords.look }));
  const ask = await screen.findByRole("button", { name: tabWords.ask });
  fireEvent.click(ask);
  assert.deepEqual(second.seeds, [tabWords.askSeed]);
  assert.equal(within(screen.getByTestId("browser-empty")).getAllByRole("button").length, 1);
  second.unmount();

  // One who may not post goes back to Details.
  const third = tab({ responses: [], canPost: false });
  fireEvent.click(screen.getByRole("button", { name: tabWords.toDetails }));
  assert.deepEqual(third.opened, ["details"]);
  third.unmount();

  // While the answers are read there is no empty state to mistake for one.
  tab({ responses: undefined });
  assert.equal(screen.queryByTestId("browser-empty"), null);
  assert.equal(screen.queryByTestId("browser-tab"), null);
});

const tasks: Task[] = [
  { id: "t1", title: "Thinking", status: "complete" },
  {
    id: "t2",
    title: "Browser navigate",
    status: "complete",
    tool: "browser__browser_navigate",
    input: '{\n  "url": "https://example.com"\n}',
    output: page("https://example.com/", "Example Domain"),
  },
  {
    id: "t3",
    title: "Browser click",
    status: "error",
    tool: "browser__browser_click",
    input: "not json",
  },
];

test("an answer that used the browser has one card, which opens the tab on its last step", () => {
  const opened: string[] = [];
  const shots = createBrowserShots();
  const { rerender } = render(
    <WorkbenchOpen.Provider value={(id, ref) => opened.push(`${id}:${ref}`)}>
      <BrowserCard thread={thread} tasks={tasks} shots={shots} />
    </WorkbenchOpen.Provider>,
  );
  const card = within(screen.getByTestId("browser-card"));
  const button = card.getByRole("button", { name: `${tabWords.card.open}: Example Domain` });
  assert.ok(card.getByText(tabWords.card.title));
  assert.ok(card.getByText("Example Domain"));
  assert.ok(card.getByText(`${tabWords.card.steps(2)} · ${tabWords.card.on("browser")}`));
  // No screenshot has been given to this console, so none is drawn.
  assert.equal(card.queryByRole("img"), null);
  fireEvent.click(button);
  assert.deepEqual(opened, ["browser:t3"]);

  // Outside a session's page it is the same card and opens nothing.
  rerender(<BrowserCard thread={thread} tasks={tasks} shots={shots} />);
  assert.equal(screen.queryByRole("button"), null);
  assert.ok(screen.getByText("Example Domain"));
});

test("the card's picture is the latest screenshot this console was given, and an answer without browser work has no card", async () => {
  const shots = createBrowserShots();
  const { rerender } = render(<BrowserCard thread={thread} tasks={tasks} shots={shots} />);
  shots.add(thread, view(), "Took a screenshot");
  const img = await screen.findByRole("img", { name: tabWords.card.thumbnail });
  assert.equal(img.getAttribute("src"), "data:image/png;base64,AAAA");
  rerender(
    <BrowserCard
      thread={thread}
      tasks={[{ id: "t1", title: "Search", status: "complete", tool: "web_search" }]}
      shots={shots}
    />,
  );
  assert.equal(screen.queryByTestId("browser-card"), null);
});

const call = (
  id: string,
  name: string,
  input = {},
  more: Partial<BrowserCall> = {},
): BrowserCall => ({
  id,
  name,
  input,
  failed: false,
  running: false,
  ...more,
});
test("a browser tool is known by its name, under any provider, and nothing else is", () => {
  assert.deepEqual(browserTool("browser__browser_navigate"), {
    provider: "browser",
    tool: "browser_navigate",
  });
  assert.deepEqual(browserTool("mcp__work-chrome__browser_click"), {
    provider: "work-chrome",
    tool: "browser_click",
  });
  assert.deepEqual(browserTool("browser_snapshot"), {
    provider: undefined,
    tool: "browser_snapshot",
  });
  assert.deepEqual(browserTool("computer"), { provider: undefined, tool: "computer" });
  // A provider named browser does not make its other tools browser work.
  assert.equal(browserTool("browser__search"), undefined);
  assert.equal(browserTool("github__create_issue"), undefined);
  assert.equal(browserTool("web_fetch"), undefined);
});

test("a result says where it left the browser", () => {
  assert.deepEqual(pageOf(page("https://example.com/", "Example Domain")), {
    url: "https://example.com/",
    title: "Example Domain",
  });
  assert.deepEqual(pageOf("### Result\n- [Screenshot of viewport](out/page.png)"), {
    url: undefined,
    title: undefined,
  });
  assert.deepEqual(pageOf(undefined), {});
});

test("each call is a step in words, on the page the browser was left on", () => {
  const steps = browserSteps([
    call("a", "web_search", { query: "docs" }),
    call(
      "1",
      "browser__browser_navigate",
      { url: "https://example.com" },
      {
        output: page("https://example.com/", "Example Domain"),
      },
    ),
    call("2", "browser__browser_snapshot", {}, { output: "### Snapshot\n- link" }),
    call(
      "3",
      "browser__browser_click",
      { element: "Learn more link", target: "e13" },
      {
        output: page("https://iana.org/help", "IANA help"),
      },
    ),
    call("4", "browser__browser_take_screenshot", { scale: "css" }, { output: "### Result" }),
    call(
      "5",
      "browser__browser_type",
      { element: "Search box", text: "secret" },
      {
        output: "Error: Ref e99 not found\n    at step",
        failed: true,
      },
    ),
    call("6", "browser__browser_press_key", { key: "Enter" }, { running: true }),
    call("7", "browser__browser_hover", {}),
  ]);
  assert.deepEqual(
    steps.map((s) => [s.id, s.kind, s.label, s.url, s.title]),
    [
      [
        "1",
        "navigate",
        words.navigate("https://example.com"),
        "https://example.com/",
        "Example Domain",
      ],
      ["2", "snapshot", words.snapshot, "https://example.com/", "Example Domain"],
      ["3", "click", words.click("Learn more link"), "https://iana.org/help", "IANA help"],
      ["4", "screenshot", words.screenshot, "https://iana.org/help", "IANA help"],
      ["5", "type", words.type("Search box"), "https://iana.org/help", "IANA help"],
      ["6", "press", words.press("Enter"), "https://iana.org/help", "IANA help"],
      ["7", "other", "hover", "https://iana.org/help", "IANA help"],
    ],
  );
  // What was typed is not in a step's words.
  assert.ok(!JSON.stringify(steps).includes("secret"));
  assert.equal(steps[4]?.failed, true);
  assert.equal(steps[4]?.error, "Error: Ref e99 not found");
  assert.deepEqual(browserNow(steps), {
    url: "https://iana.org/help",
    title: "IANA help",
    provider: "browser",
    running: true,
  });
  assert.deepEqual(browserNow([]), {
    url: undefined,
    title: undefined,
    provider: undefined,
    running: false,
  });
});

test("a navigation still running is on the address it was sent to, and a failed one is not", () => {
  const steps = browserSteps([
    call("1", "browser_navigate", { url: "https://a.example/" }, { running: true }),
    call(
      "2",
      "browser_navigate",
      { url: "https://b.example/" },
      { failed: true, output: "net::ERR" },
    ),
  ]);
  assert.deepEqual(
    steps.map((s) => s.url),
    ["https://a.example/", "https://a.example/"],
  );
});

test("calls are read from a session's answers and from an answer's steps alike", () => {
  const response: ResponseState = {
    ...emptyResponse(),
    content: [
      tool("1", "browser__browser_navigate", {
        input: { url: "https://example.com" },
        result: {
          text: page("https://example.com/", "Example Domain"),
          sources: [],
          isError: false,
        },
        // A call an agent it brought in made is the session's work too.
        content: [tool("2", "browser__browser_click", { status: "error", depth: 1 })],
      }),
      tool("3", "browser__browser_snapshot", { status: "running" }),
    ],
  };
  assert.deepEqual(
    callsOfResponses([response]).map((c) => [c.id, c.failed, c.running]),
    [
      ["1", false, false],
      ["2", true, false],
      ["3", false, true],
    ],
  );
  assert.deepEqual(
    browserSteps(callsOfTasks(tasks)).map((s) => [s.id, s.label, s.title, s.failed]),
    [
      ["t2", words.navigate("https://example.com"), "Example Domain", false],
      ["t3", words.click(words.thePage), "Example Domain", true],
    ],
  );
});

test("screenshots are kept per session, newest last, within their caps, and only images are", () => {
  const store = createBrowserShots();
  let told = 0;
  const stop = store.subscribe(() => {
    told += 1;
  });
  const view = (n: number) => ({
    provider: "browser",
    action: "look" as const,
    url: `https://example.com/${n}`,
    elements: [],
    screenshot: { media_type: "image/png", data: "AAAA" },
  });
  for (let n = 0; n < MAX_BROWSER_SHOTS + 3; n++) store.add("s_1", view(n), "Took a screenshot", n);
  const kept = store.list("s_1");
  assert.equal(kept.length, MAX_BROWSER_SHOTS);
  assert.equal(kept[kept.length - 1]?.url, `https://example.com/${MAX_BROWSER_SHOTS + 2}`);
  assert.equal(kept[0]?.src, "data:image/png;base64,AAAA");
  assert.equal(told, MAX_BROWSER_SHOTS + 3);
  // An answer without an image, or with something that is not one, keeps nothing.
  assert.equal(store.add("s_1", { ...view(0), screenshot: undefined }, "x"), undefined);
  assert.equal(
    store.add("s_1", { ...view(0), screenshot: { media_type: "text/html", data: "PGI+" } }, "x"),
    undefined,
  );
  assert.equal(store.list("s_1").length, MAX_BROWSER_SHOTS);
  for (let n = 2; n <= MAX_BROWSER_SESSIONS + 1; n++) store.add(`s_${n}`, view(n), "x");
  assert.equal(store.list("s_1").length, 0);
  assert.equal(store.list("s_2").length, 1);
  stop();
  store.clear();
  assert.equal(store.list("s_2").length, 0);
});
