import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import {
  createHumanActivityStore,
  foldHumanActivity,
  humanActivity,
  splitHumanActivity,
} from "@humagen/core";
import { ListChecksIcon, TerminalIcon } from "@humagen/render-web";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { HumanActivityList, HumanActivityNote, useHumanActivity } from "./humanActivity.tsx";
import { lazyView } from "./lazy.tsx";
import type { ConversationView } from "./route.ts";
import { WORKBENCH_TAB_IDS } from "./route.ts";
import { DetailsTab } from "./SessionPanel.tsx";
import { SessionWorkbench } from "./Workbench.tsx";
import {
  activeBench,
  openBench,
  useOpenWorkbench,
  WORKBENCH_TABS,
  WorkbenchOpen,
  type WorkbenchSession,
  type WorkbenchTab,
  type WorkbenchTabProps,
  workbenchTabs,
} from "./workbench.ts";

const words = defaultCopy.workbench;
const details = defaultCopy.details;
const thread = "th-workbench";

afterEach(() => {
  cleanup();
  humanActivity.clear(thread);
});

/** A tab that says what it was given and does one thing by hand. */
function Probe({ thread: id, selection, onSelection, record, openTab, ask }: WorkbenchTabProps) {
  return (
    <div data-testid="probe">
      <p>
        {id} {selection ?? "nothing selected"} {ask ? "may ask" : "may not ask"}
      </p>
      <button type="button" onClick={() => onSelection("box-2")}>
        Select
      </button>
      <button type="button" onClick={() => openTab("details")}>
        To Details
      </button>
      <button
        type="button"
        onClick={() =>
          record({ source: "terminal", summary: "Ran pnpm test, exit 1", detail: "FAIL a.test.ts" })
        }
      >
        Run
      </button>
    </div>
  );
}

const tab = (id: WorkbenchTab["id"], more: Partial<WorkbenchTab> = {}): WorkbenchTab => ({
  id,
  label: words.tab[id],
  icon: id === "details" ? ListChecksIcon : TerminalIcon,
  view: id === "details" ? DetailsTab : Probe,
  ...more,
});
const fake: WorkbenchTab[] = [
  tab("details"),
  tab("browser", { available: () => false }),
  tab("terminal", { fill: true }),
];
const session: WorkbenchSession = {
  thread,
  runs: [],
  responses: [],
  summary: undefined,
  canPost: true,
};

function bench(view: ConversationView, tabs: readonly WorkbenchTab[] = fake, canPost = true) {
  const views: ConversationView[] = [];
  const asked: string[] = [];
  const out = render(
    <SessionWorkbench
      runs={[]}
      thread={thread}
      view={view}
      onView={(next) => views.push(next)}
      onClose={() => {}}
      canPost={canPost}
      onAsk={(text) => asked.push(text)}
      tabs={tabs}
    />,
  );
  return { views, asked, ...out };
}

test("the registry builds Details first, and reserves every other tab's address", () => {
  assert.equal(WORKBENCH_TABS[0]?.id, "details");
  assert.equal(WORKBENCH_TABS[0]?.label, words.tab.details);
  // A tab that is built is one of the reserved ids, in the strip's order, once.
  const built = WORKBENCH_TABS.map((t) => t.id);
  assert.deepEqual(
    built,
    WORKBENCH_TAB_IDS.filter((id) => built.includes(id)),
  );
  for (const id of WORKBENCH_TAB_IDS) assert.ok(words.tab[id], `${id} has a label`);
});

test("the strip shows Details, the tabs with something to show, and the one the address names", () => {
  assert.deepEqual(
    workbenchTabs(session, undefined, fake).map((t) => t.id),
    ["details", "terminal"],
  );
  // A link or a card may open a tab before the session has anything for it.
  assert.deepEqual(
    workbenchTabs(session, "browser", fake).map((t) => t.id),
    ["details", "browser", "terminal"],
  );
  // A tab that is not built opens Details.
  assert.equal(activeBench({ panel: "details", bench: "search" }, fake)?.id, "details");
  assert.equal(activeBench({ panel: "details", bench: "terminal" }, fake)?.id, "terminal");
  assert.equal(activeBench({ panel: "details" }, fake)?.id, "details");
});

test("opening a tab is an address: the tab, its selection, and whether the pane fills the page", () => {
  assert.deepEqual(openBench({}, "terminal"), { panel: "details", bench: "terminal" });
  assert.deepEqual(
    openBench({ panel: "workspace", expanded: true, file: "a.ts" }, "browser", "x"),
    {
      panel: "details",
      bench: "browser",
      benchRef: "x",
    },
  );
  assert.deepEqual(openBench({ panel: "details", bench: "terminal", expanded: true }, "details"), {
    panel: "details",
    expanded: true,
  });
});

test("with only Details built the pane is the Details it was: no strip, the same rows", async () => {
  bench({ panel: "details" }, [tab("details")]);
  assert.ok(await screen.findByTestId("session-panel"));
  assert.equal(screen.queryByRole("tablist"), null);
  assert.equal(screen.getByTestId("workbench-tab").getAttribute("data-bench"), "details");
  assert.ok(screen.getByRole("heading", { name: details.title }));
});

test("a tab opens at its address, is given the session and its selection, and names the pane", async () => {
  const { views } = bench({ panel: "details", bench: "terminal", benchRef: "box-1" });
  const strip = screen.getByRole("tablist", { name: words.tabs });
  assert.deepEqual(
    within(strip)
      .getAllByRole("tab")
      .map((t) => [t.textContent, t.getAttribute("aria-selected")]),
    [
      [words.tab.details, "false"],
      [words.tab.terminal, "true"],
    ],
  );
  assert.ok(screen.getByRole("heading", { name: words.tab.terminal }));
  const probe = within(screen.getByRole("tabpanel", { name: words.tab.terminal }));
  assert.ok(probe.getByText(`${thread} box-1 may ask`));
  // A tab that fills the pane, and any tab but Details, takes the wide pane.
  const pane = document.querySelector("[data-layer=inspector]");
  assert.equal(pane?.hasAttribute("data-wide"), true);
  assert.equal(pane?.hasAttribute("data-fill"), true);

  fireEvent.click(probe.getByRole("button", { name: "Select" }));
  fireEvent.click(probe.getByRole("button", { name: "To Details" }));
  fireEvent.click(within(strip).getByRole("tab", { name: words.tab.details }));
  fireEvent.click(screen.getByRole("button", { name: words.expand }));
  assert.deepEqual(views, [
    { panel: "details", bench: "terminal", benchRef: "box-2" },
    { panel: "details" },
    { panel: "details" },
    { panel: "details", bench: "terminal", benchRef: "box-1", expanded: true },
  ]);
});

test("a reader who may not post is given no way to ask from a tab", () => {
  bench({ panel: "details", bench: "terminal" }, fake, false);
  assert.ok(screen.getByText(`${thread} nothing selected may not ask`));
});

test("a tab read on first use shows the pane's loading rows, then itself", async () => {
  let landed: (() => void) | undefined;
  const Later = lazyView(
    () =>
      new Promise<{ Probe: typeof Probe }>((resolve) => {
        landed = () => resolve({ Probe });
      }),
    (m) => m.Probe,
  );
  bench({ panel: "details", bench: "terminal" }, [
    tab("details"),
    tab("terminal", { view: Later }),
  ]);
  assert.equal(screen.queryByTestId("probe"), null);
  assert.ok(document.querySelector("[data-testid=workbench-tab] .aui-skeleton"));
  landed?.();
  assert.ok(await screen.findByTestId("probe"));
});

test("what is done by hand in a tab shows above the composer, and each line can be taken out", async () => {
  bench({ panel: "details", bench: "terminal" });
  render(<HumanActivityList conversation={thread} />);
  // Nothing is drawn until something is done.
  assert.equal(screen.queryByTestId("human-activity"), null);
  fireEvent.click(screen.getByRole("button", { name: "Run" }));
  humanActivity.add(thread, { source: "changes", summary: "Kept the change to route.ts" });
  const list = await screen.findByTestId("human-activity");
  assert.equal(list.getAttribute("aria-label"), words.byHand.pending(2));
  assert.deepEqual(
    within(list)
      .getAllByRole("listitem")
      .map((li) => [li.getAttribute("data-source"), li.textContent]),
    [
      ["terminal", `${words.byHand.source.terminal}: Ran pnpm test, exit 1`],
      ["changes", `${words.byHand.source.changes}: Kept the change to route.ts`],
    ],
  );
  fireEvent.click(
    within(list).getByRole("button", { name: words.byHand.remove("Ran pnpm test, exit 1") }),
  );
  await waitFor(() =>
    assert.equal(
      screen.getByTestId("human-activity").getAttribute("aria-label"),
      words.byHand.pending(1),
    ),
  );
  fireEvent.click(screen.getByRole("button", { name: words.byHand.clear }));
  await waitFor(() => assert.equal(screen.queryByTestId("human-activity"), null));
  assert.deepEqual(humanActivity.list(thread), []);
});

test("the list says when the next message has no room for all of it", () => {
  const store = createHumanActivityStore();
  for (let i = 0; i < 40; i++)
    store.add(thread, { source: "terminal", summary: `Ran step ${i}`, detail: "x".repeat(600) });
  const { omitted } = foldHumanActivity("", store.list(thread));
  assert.ok(omitted > 0);
  render(<HumanActivityList conversation={thread} store={store} />);
  assert.equal(screen.getByRole("status").textContent, words.byHand.overflow(omitted));
});

test("a conversation's record is its own, and the hook follows it", async () => {
  const store = createHumanActivityStore();
  function Count({ id }: { id: string }) {
    const { records, record } = useHumanActivity(id, store);
    return (
      <button type="button" onClick={() => record({ source: "browser", summary: "Signed in" })}>
        {id}: {records.length}
      </button>
    );
  }
  render(
    <>
      <Count id="a" />
      <Count id="b" />
    </>,
  );
  fireEvent.click(screen.getByRole("button", { name: "a: 0" }));
  assert.ok(await screen.findByRole("button", { name: "a: 1" }));
  assert.ok(screen.getByRole("button", { name: "b: 0" }));
});

test("a message that carried what was done by hand shows it closed, and opens to each thing", () => {
  const store = createHumanActivityStore();
  store.add(thread, { source: "terminal", summary: "Ran pnpm test, exit 1", detail: "FAIL a" });
  store.add(thread, { source: "browser", summary: "Signed in at example.com" });
  const { activity, text } = splitHumanActivity(
    foldHumanActivity("Why did it fail?", store.list(thread)).text,
  );
  assert.equal(text, "Why did it fail?");
  assert.ok(activity);
  render(<HumanActivityNote activity={activity} mine />);
  const note = screen.getByTestId("human-activity-note") as HTMLDetailsElement;
  assert.equal(note.open, false);
  assert.equal(note.querySelector("summary")?.textContent, words.byHand.did(2, true));
  assert.deepEqual(
    within(note)
      .getAllByRole("listitem")
      .map((li) => li.getAttribute("data-source")),
    ["terminal", "browser"],
  );
  assert.equal(note.querySelector("pre")?.textContent, "FAIL a");
  cleanup();
  render(<HumanActivityNote activity={activity} />);
  assert.equal(
    screen.getByTestId("human-activity-note").querySelector("summary")?.textContent,
    words.byHand.did(2, false),
  );
});

test("a card in the thread opens a tab that is built, and offers nothing for one that is not", () => {
  const opened: string[] = [];
  function Card({ id }: { id: WorkbenchTab["id"] }) {
    const open = useOpenWorkbench(id, fake);
    return open ? (
      <button type="button" onClick={() => open("box-9")}>
        Open {id}
      </button>
    ) : (
      <span>no {id}</span>
    );
  }
  const { rerender } = render(
    <WorkbenchOpen.Provider value={(id, ref) => opened.push(`${id}:${ref}`)}>
      <Card id="browser" />
      <Card id="search" />
    </WorkbenchOpen.Provider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Open browser" }));
  assert.deepEqual(opened, ["browser:box-9"]);
  assert.ok(screen.getByText("no search"));
  // Outside a session's page there is no workbench to open.
  rerender(<Card id="browser" />);
  assert.ok(screen.getByText("no browser"));
});
