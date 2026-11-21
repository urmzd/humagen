import assert from "node:assert/strict";
import { afterEach, beforeEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Component, type ComponentType, type ReactNode, useState } from "react";
import { again, LazyFrame, lazyHost, lazyPage, lazyView, moduleAddress, sparing } from "./lazy.tsx";
import { HubTabLabel } from "./ui.tsx";

// Loading the console again is counted, not done.
let reloads = 0;
const reload = lazyHost.reload;
// What the person was told, in place of a notice.
let told: string[] = [];
const tell = lazyHost.tell;
const readAgain = lazyHost.again;
// React reports each error a frame catches; the tests assert on what is drawn instead.
const report = console.error;
beforeEach(() => {
  reloads = 0;
  told = [];
  lazyHost.reload = () => {
    reloads++;
  };
  lazyHost.tell = (message) => {
    told.push(message);
  };
  console.error = () => {};
});
afterEach(() => {
  lazyHost.reload = reload;
  lazyHost.tell = tell;
  lazyHost.again = readAgain;
  console.error = report;
  cleanup();
});

const words = defaultCopy.pageLoad;
/** The test's code is the view itself. */
const same = <T,>(view: T) => view;
const Reports = ({ who }: { who: string }) => <h1>Reports for {who}</h1>;

/** A page's code, read when the test says: `land` answers the read, `fail` refuses it. */
function code<P>(view: ComponentType<P>) {
  let calls = 0;
  let settle: { land: () => void; fail: () => void } | undefined;
  return {
    load: () => {
      calls++;
      return new Promise<ComponentType<P>>((land, fail) => {
        settle = { land: () => land(view), fail: () => fail(new TypeError("Failed to fetch")) };
      });
    },
    land: () => act(async () => settle?.land()),
    fail: () => act(async () => settle?.fail()),
    calls: () => calls,
  };
}

test("a page read on demand holds its place with rows, then draws", async () => {
  const reports = code(Reports);
  const Page = lazyPage(reports.load, same, { title: () => "Reports" });
  render(<Page who="Ada" />);
  // Its own header over rows that stand in for its list: no bare word, no spinner.
  assert.ok(screen.getByRole("heading", { name: "Reports" }));
  assert.ok(screen.getByTestId("skeleton"));
  assert.equal(screen.queryByText("Reports for Ada"), null);
  await reports.land();
  assert.ok(await screen.findByRole("heading", { name: "Reports for Ada" }));
  assert.equal(screen.queryByTestId("skeleton"), null);
  assert.equal(reports.calls(), 1);
});

test("a page read ahead draws at once, with no loading state", async () => {
  const reports = code(Reports);
  const Page = lazyPage(reports.load, same, { title: () => "Reports" });
  const ahead = Page.preload();
  await reports.land();
  await ahead;
  render(<Page who="Ada" />);
  // In the same step as the render: nothing stood in for it.
  assert.ok(screen.getByRole("heading", { name: "Reports for Ada" }));
  assert.equal(screen.queryByTestId("skeleton"), null);
  assert.equal(reports.calls(), 1);
});

test("a page whose code cannot be read says so and offers Retry, which reads it again", async () => {
  const reports = code(Reports);
  const Page = lazyPage(reports.load, same, { title: () => "Reports" });
  render(<Page who="Ada" />);
  await reports.fail();
  // Never an empty page: a title that says what happened, what to do, and a way to try again.
  assert.ok(await screen.findByRole("heading", { name: words.title }));
  assert.equal(screen.getByRole("alert").textContent, words.hint);
  // The way to try again is one icon on the message's row, named for a screen reader.
  const retry = screen.getByRole("button", { name: defaultCopy.retry });
  assert.equal(retry.textContent, "");
  assert.equal(screen.getAllByRole("button").length, 1);
  assert.equal(screen.queryByText("Reports for Ada"), null);

  fireEvent.click(screen.getByRole("button", { name: defaultCopy.retry }));
  await waitFor(() => assert.equal(reports.calls(), 2));
  await reports.land();
  assert.ok(await screen.findByRole("heading", { name: "Reports for Ada" }));
  assert.equal(screen.queryByRole("alert"), null);
  assert.equal(reloads, 0, "read again where it stood, without loading the console again");
});

test("under a hub's tabs the open tab heads the failure, which is said once", async () => {
  const reports = code(Reports);
  const Page = lazyPage(reports.load, same, { title: () => "Reports" });
  render(
    <HubTabLabel.Provider value="Agents">
      <Page who="Ada" />
    </HubTabLabel.Provider>,
  );
  await reports.fail();
  // The tab already names the page: no second title, and the message says what happened.
  assert.equal((await screen.findByRole("alert")).textContent, words.failed);
  assert.equal(screen.queryByRole("heading", { name: words.title }), null);
  assert.ok(screen.getByRole("heading", { name: "Agents" }).className.includes("sr-only"));
  assert.ok(screen.getByRole("button", { name: defaultCopy.retry }));
});

test("when trying again fails too, the console is loaded again at its address", async () => {
  // A browser may hold the failed read against the address, and after an update the code is gone.
  const reports = code(Reports);
  const Page = lazyPage(reports.load, same, { title: () => "Reports" });
  render(<Page who="Ada" />);
  await reports.fail();
  assert.equal(reloads, 0, "the first failure only says so");
  fireEvent.click(await screen.findByRole("button", { name: defaultCopy.retry }));
  await waitFor(() => assert.equal(reports.calls(), 2));
  await reports.fail();
  await waitFor(() => assert.equal(reloads, 1));
  // The failure stays on view until the console loads again.
  assert.equal(screen.getByRole("alert").textContent, words.hint);
  assert.ok(screen.getByRole("button", { name: defaultCopy.retry }));
});

test("offline, trying again never loads the console again, which would lose it", async () => {
  const online = Object.getOwnPropertyDescriptor(navigator, "onLine");
  Object.defineProperty(navigator, "onLine", { configurable: true, get: () => false });
  try {
    const reports = code(Reports);
    const Page = lazyPage(reports.load, same);
    render(<Page who="Ada" />);
    await reports.fail();
    fireEvent.click(await screen.findByRole("button", { name: defaultCopy.retry }));
    await waitFor(() => assert.equal(reports.calls(), 2));
    await reports.fail();
    assert.ok(await screen.findByRole("alert"));
    assert.equal(reloads, 0);
  } finally {
    if (online) Object.defineProperty(navigator, "onLine", online);
    else delete (navigator as { onLine?: boolean }).onLine;
  }
});

test("a failed read ahead is not held against the page: opening it reads again", async () => {
  const reports = code(Reports);
  const Page = lazyPage(reports.load, same);
  const ahead = Page.preload();
  await reports.fail();
  await ahead;
  render(<Page who="Ada" />);
  await waitFor(() => assert.equal(reports.calls(), 2));
  await reports.land();
  assert.ok(await screen.findByRole("heading", { name: "Reports for Ada" }));
});

test("a part of a headed page fails in place: its message and Retry, under the page's own title", async () => {
  const reports = code(Reports);
  const Part = lazyPage(reports.load, same, { frame: "rows" });
  render(
    <main>
      <h1>Monitor</h1>
      <Part who="Ada" />
    </main>,
  );
  assert.ok(screen.getByTestId("skeleton"));
  await reports.fail();
  assert.ok(await screen.findByRole("alert"));
  assert.ok(screen.getByRole("button", { name: defaultCopy.retry }));
  // The page keeps its one title.
  assert.equal(screen.getAllByRole("heading").length, 1);
});

/** Something opened from a button, as the console opens search: put away when it cannot be read. */
function Opener({ Dialog }: { Dialog: ReturnType<typeof lazyPage<unknown, { who: string }>> }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Search
      </button>
      {open && <Dialog who="Ada" onUnavailable={() => setOpen(false)} />}
    </>
  );
}

test("a control that cannot be read says so, and the next press reads it again and opens it", async () => {
  const reports = code(Reports);
  render(<Opener Dialog={lazyPage(reports.load, same, { frame: "none" })} />);
  const press = () => fireEvent.click(screen.getByRole("button", { name: "Search" }));
  press();
  await waitFor(() => assert.equal(reports.calls(), 1));
  await reports.fail();
  await waitFor(() => assert.deepEqual(told, [words.partFailed]));
  // Nothing is drawn in its place, and no page failure either.
  assert.equal(screen.queryByRole("alert"), null);
  assert.equal(screen.queryByRole("heading"), null);

  press();
  await waitFor(() => assert.equal(reports.calls(), 2, "the second press is not swallowed"));
  await reports.land();
  assert.ok(await screen.findByRole("heading", { name: "Reports for Ada" }));
  assert.equal(told.length, 1);
});

test("a control that fails twice says so twice, and still opens on the third press", async () => {
  const reports = code(Reports);
  render(<Opener Dialog={lazyPage(reports.load, same, { frame: "none" })} />);
  const press = () => fireEvent.click(screen.getByRole("button", { name: "Search" }));
  for (const attempt of [1, 2]) {
    press();
    await waitFor(() => assert.equal(reports.calls(), attempt, `press ${attempt} reads`));
    await reports.fail();
    await waitFor(() => assert.equal(told.length, attempt, `press ${attempt} is answered`));
  }
  press();
  await waitFor(() => assert.equal(reports.calls(), 3));
  await reports.land();
  assert.ok(await screen.findByRole("heading", { name: "Reports for Ada" }));
  // Every press was answered: by a notice or by the control.
  assert.equal(told.length, 2);
  assert.equal(reloads, 0);
});

test("a read made again asks at an address the browser has not seen fail", async () => {
  // Chromium answers a second import() of a failed address with the first failure, without asking.
  const asked: string[] = [];
  let calls = 0;
  const failing = () => {
    calls++;
    return Promise.reject(
      new TypeError(
        "Failed to fetch dynamically imported module: http://localhost:5173/assets/QuickSwitcher-abc123.js",
      ),
    );
  };
  let answer: "fail" | "land" = "fail";
  lazyHost.again = (address) => {
    asked.push(address);
    return answer === "land"
      ? Promise.resolve(Reports)
      : Promise.reject(new TypeError(`Failed to fetch dynamically imported module: ${address}`));
  };
  render(<Opener Dialog={lazyPage(failing, same, { frame: "none" })} />);
  const press = () => fireEvent.click(screen.getByRole("button", { name: "Search" }));
  press();
  await waitFor(() => assert.equal(told.length, 1));
  press();
  await waitFor(() => assert.equal(told.length, 2));
  answer = "land";
  press();
  assert.ok(await screen.findByRole("heading", { name: "Reports for Ada" }));
  assert.equal(calls, 1, "the bundler's own import is asked once");
  assert.deepEqual(asked, [
    "http://localhost:5173/assets/QuickSwitcher-abc123.js?again=1",
    "http://localhost:5173/assets/QuickSwitcher-abc123.js?again=2",
  ]);
});

test("the address of a failed read is taken only from the browser's own words about this build", () => {
  const at = "http://localhost:5173/assets/Room-x_9-Z.js";
  assert.equal(
    moduleAddress(new TypeError(`Failed to fetch dynamically imported module: ${at}`)),
    at,
  );
  assert.equal(
    moduleAddress(new TypeError(`error loading dynamically imported module: ${at}`)),
    at,
  );
  assert.equal(moduleAddress(new TypeError("Importing a module script failed.")), undefined);
  assert.equal(
    moduleAddress(new TypeError("Failed to fetch: https://elsewhere.example/assets/x.js")),
    undefined,
  );
  assert.equal(
    moduleAddress(new TypeError("Failed to fetch: http://localhost:5173/api/x.js")),
    undefined,
  );
  assert.equal(moduleAddress("not an error"), undefined);
  assert.equal(again(`${at}?again=1`, 2), `${at}?again=2`);
});

test("a part with a plain form falls back to it without a word, and is read again when next due", async () => {
  // The composer's light: the bar works without it, so nothing is said.
  const reports = code(Reports);
  const Light = lazyView(reports.load, same);
  const bar = (due: number) => (
    <LazyFrame fallback={<p>plain</p>} instead={<p>plain</p>} resetKey={String(due)}>
      <Light who="Ada" />
    </LazyFrame>
  );
  const page = render(bar(1));
  assert.ok(screen.getByText("plain"));
  await reports.fail();
  assert.ok(await screen.findByText("plain"));
  assert.deepEqual(told, []);
  assert.equal(reports.calls(), 1, "it is not asked for again until it is due again");

  page.rerender(bar(2));
  await waitFor(() => assert.equal(reports.calls(), 2));
  await reports.fail();
  assert.ok(await screen.findByText("plain"));

  page.rerender(bar(3));
  await waitFor(() => assert.equal(reports.calls(), 3));
  await reports.land();
  assert.ok(await screen.findByRole("heading", { name: "Reports for Ada" }));
  assert.deepEqual(told, []);
});

test("nothing is read ahead for a person saving data or on a slow connection", () => {
  const set = (connection: unknown) =>
    Object.defineProperty(navigator, "connection", { configurable: true, value: connection });
  try {
    set({ saveData: true });
    assert.equal(sparing(), true);
    set({ saveData: false, effectiveType: "2g" });
    assert.equal(sparing(), true);
    set({ effectiveType: "slow-2g" });
    assert.equal(sparing(), true);
    set({ saveData: false, effectiveType: "4g" });
    assert.equal(sparing(), false);
    set(undefined);
    assert.equal(sparing(), false);
  } finally {
    delete (navigator as { connection?: unknown }).connection;
  }
});

test("a failure is left behind when the person goes somewhere else", async () => {
  const reports = code(Reports);
  const View = lazyView(reports.load, same);
  const Other = () => <h1>Agents</h1>;
  const at = (path: string) => (
    <LazyFrame resetKey={path} fallback={null}>
      {path === "/reports" ? <View who="Ada" /> : <Other />}
    </LazyFrame>
  );
  const page = render(at("/reports"));
  await reports.fail();
  assert.ok(await screen.findByRole("alert"));
  page.rerender(at("/agents"));
  assert.ok(await screen.findByRole("heading", { name: "Agents" }));
  assert.equal(screen.queryByRole("alert"), null);
});

/** Whatever is above a lazy frame: it is handed the errors the frame does not answer. */
class Above extends Component<{ children: ReactNode }, { error: unknown }> {
  override state: { error: unknown } = { error: null };
  static getDerivedStateFromError(error: unknown) {
    return { error };
  }
  override render() {
    const { error } = this.state;
    return error === null ? this.props.children : <p>above: {(error as Error).name}</p>;
  }
}

test("an error that is not a failed read is not this frame's to answer", async () => {
  const Broken = (): never => {
    throw new RangeError("a page's own mistake");
  };
  const broken = code(Broken);
  const Page = lazyPage(broken.load, same);
  render(
    <Above>
      <Page />
    </Above>,
  );
  await broken.land();
  assert.ok(await screen.findByText("above: RangeError"));
  assert.equal(screen.queryByText(words.failed), null);
});
