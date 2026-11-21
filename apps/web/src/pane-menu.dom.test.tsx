import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { type KeyboardEvent, useRef, useState } from "react";
import { PaneMenu, type PaneMenuRow } from "./PaneMenu.tsx";

// The pane menu on its own, apart from the + menu and the usage sheet built
// on it: rows that say a label and a value, panes under Back and a title, the
// arrow keys, the way back to the row a pane was opened from, one Escape step
// per press, and the sheet it becomes on a phone.

afterEach(cleanup);

/** What the focused control says, or its name when it has no words. */
const focused = () =>
  document.activeElement?.getAttribute("aria-label") ?? document.activeElement?.textContent ?? "";
const row = (name: string) => screen.getByRole("button", { name: new RegExp(`^${name}`) });
const press = (key: string, on: Element | null = document.activeElement) =>
  fireEvent.keyDown(on as Element, { key });
const title = () => screen.queryByRole("heading")?.textContent ?? null;

function Menu({
  picked = [],
  onClose = () => {},
  onKey,
  without,
}: {
  picked?: string[];
  onClose?: () => void;
  onKey?: (event: KeyboardEvent<HTMLDivElement>, pane: string | null) => void;
  /** A row to leave out. */
  without?: string;
}) {
  const [query, setQuery] = useState("");
  const rows: PaneMenuRow[] = [
    { id: "print", label: "Print", onSelect: () => picked.push("print") },
    {
      id: "size",
      label: "Size",
      value: "Medium",
      pane: {
        title: "Size",
        body: (
          <div role="radiogroup" aria-label="Size">
            {["Small", "Medium", "Large"].map((size) => (
              // biome-ignore lint/a11y/useSemanticElements: a radio drawn as a row, as a pane lists them
              <button
                key={size}
                type="button"
                role="radio"
                aria-checked={size === "Medium"}
                data-pane-item="action"
              >
                {size}
              </button>
            ))}
          </div>
        ),
      },
    },
    { id: "owner", label: "Owner", value: "You", chevron: false },
    { id: "more", label: "More", chevron: true },
    {
      id: "fruit",
      label: "Fruit",
      note: "Pick any number.",
      pane: {
        title: "Fruit",
        body: (
          <>
            <input
              type="search"
              aria-label="Find a fruit"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
            {["apple", "pear"]
              .filter((name) => name.includes(query))
              .map((name) => (
                <button key={name} type="button" data-pane-item="choice">
                  {name}
                </button>
              ))}
          </>
        ),
        rows: [
          {
            id: "new",
            label: "New Fruit",
            pane: {
              ownKeys: true,
              body: (
                <>
                  <input aria-label="Name" />
                  <textarea aria-label="About" />
                  <button type="button">Save</button>
                </>
              ),
            },
          },
        ],
      },
    },
  ];
  return (
    <PaneMenu
      label="Options"
      rows={rows.filter((item) => item.id !== without)}
      onClose={onClose}
      onKey={onKey}
    />
  );
}

test("a row says its label and its value, and ends in a chevron when it opens a pane or asks for one", () => {
  const picked: string[] = [];
  render(<Menu picked={picked} />);
  assert.ok(screen.getByRole("dialog", { name: "Options" }));
  assert.equal(focused(), "Print", "focus starts on the first row");
  assert.equal(row("Size").textContent, "SizeMedium");
  assert.equal(row("Size").getAttribute("aria-haspopup"), "dialog");
  assert.equal(row("Size").querySelectorAll("svg").length, 1);
  assert.equal(row("Print").querySelectorAll("svg").length, 0);
  assert.equal(row("Print").getAttribute("aria-haspopup"), null);
  assert.equal(row("Owner").textContent, "OwnerYou");
  assert.equal(row("Owner").querySelectorAll("svg").length, 0, "a fact has no chevron");
  assert.equal(row("More").querySelectorAll("svg").length, 1, "a chevron that was asked for");
  assert.equal(row("Fruit").nextElementSibling?.textContent, "Pick any number.");
  fireEvent.click(row("Print"));
  assert.deepEqual(picked, ["print"]);
  assert.equal(title(), null, "a row without a pane opens nothing");
});

test("Up and Down move between the rows, and past either end to the other", () => {
  render(<Menu />);
  const seen: string[] = [];
  for (let i = 0; i < 5; i++) {
    assert.equal(press("ArrowDown"), false, "the menu takes the key");
    seen.push(focused());
  }
  assert.deepEqual(seen, ["SizeMedium", "OwnerYou", "More", "Fruit", "Print"]);
  press("ArrowUp");
  assert.equal(focused(), "Fruit");
  press("ArrowUp");
  assert.equal(focused(), "More");
});

test("Right opens a row's pane under Back and its title, and does nothing on a row without one", () => {
  render(<Menu />);
  assert.equal(press("ArrowRight", row("Print")), true, "nothing to open");
  assert.equal(title(), null);
  row("Size").focus();
  assert.equal(press("ArrowRight"), false);
  assert.equal(title(), "Size");
  assert.ok(screen.getByRole("button", { name: "Back" }));
  assert.equal(
    screen.queryByRole("button", { name: /^Print/ }),
    null,
    "the pane replaces the rows",
  );
  assert.equal(focused(), "Back", "a pane without a field starts on Back");
  assert.equal(screen.getAllByRole("radio").length, 3);
});

for (const [way, leave] of [
  ["Back", () => fireEvent.click(screen.getByRole("button", { name: "Back" }))],
  ["Left", () => press("ArrowLeft")],
  ["Escape", () => press("Escape")],
] as const)
  test(`${way} returns focus to the row that opened the pane`, () => {
    render(<Menu />);
    fireEvent.click(row("Size"));
    assert.equal(title(), "Size");
    leave();
    assert.equal(title(), null);
    assert.equal(focused(), "SizeMedium");
    fireEvent.click(row("Fruit"));
    assert.equal(focused(), "Find a fruit", "a pane with a field starts in it");
    screen.getByRole("button", { name: "pear" }).focus();
    leave();
    assert.equal(focused(), "Fruit");
  });

test("Left keeps its own meaning in a field and on a radio", () => {
  let closed = 0;
  render(<Menu onClose={() => closed++} />);
  assert.equal(press("ArrowLeft"), true, "at the root there is nothing to go back to");
  fireEvent.click(row("Fruit"));
  const search = screen.getByRole("searchbox", { name: "Find a fruit" });
  fireEvent.change(search, { target: { value: "pe" } });
  assert.equal(press("ArrowLeft", search), true, "the caret moves, the pane stays");
  assert.equal(title(), "Fruit");
  assert.equal((search as HTMLInputElement).value, "pe");
  press("Escape");
  fireEvent.click(row("Size"));
  const radio = screen.getByRole("radio", { name: "Large" });
  radio.focus();
  assert.equal(press("ArrowLeft"), true, "a radio group moves its own choice");
  assert.equal(title(), "Size");
  assert.equal(press("ArrowLeft", screen.getByRole("button", { name: "Back" })), false);
  assert.equal(title(), null);
  assert.equal(closed, 0);
});

test("Escape goes back one step per press: a pane inside a pane, the pane, then the menu", () => {
  let closed = 0;
  let outer = 0;
  render(
    // biome-ignore lint/a11y/noStaticElementInteractions: stands for what the menu is opened over
    // biome-ignore lint/a11y/noNoninteractiveElementInteractions: the same
    <div onKeyDown={() => outer++}>
      <Menu onClose={() => closed++} />
    </div>,
  );
  fireEvent.click(row("Fruit"));
  fireEvent.click(row("New Fruit"));
  assert.equal(title(), null, "this pane has no title");
  assert.equal(focused(), "Name");
  assert.equal(press("Escape"), false);
  assert.equal(title(), "Fruit");
  assert.equal(focused(), "New Fruit", "back on the row inside the pane");
  press("Escape");
  assert.equal(title(), null);
  assert.equal(focused(), "Fruit");
  assert.equal(closed, 0);
  press("Escape");
  assert.equal(closed, 1);
  assert.equal(outer, 0, "no Escape reaches what is under the menu");
});

test("in a pane the arrows run from Back to its field, its choices and its rows", () => {
  render(<Menu />);
  fireEvent.click(row("Fruit"));
  const seen: string[] = [];
  for (let i = 0; i < 5; i++) {
    press("ArrowDown");
    seen.push(focused());
  }
  assert.deepEqual(seen, ["apple", "pear", "New Fruit", "Back", "Find a fruit"]);
  press("ArrowUp");
  assert.equal(focused(), "Back");
  screen.getByRole("button", { name: "apple" }).focus();
  press("ArrowUp");
  assert.equal(focused(), "Find a fruit", "above the first choice is the field");
  assert.equal(
    press("ArrowRight", row("New Fruit")),
    true,
    "a row inside a pane opens on a press or Enter",
  );
  assert.equal(title(), "Fruit");
});

test("a pane that takes its own keys keeps the arrows, and Down from Back enters it", () => {
  render(<Menu />);
  fireEvent.click(row("Fruit"));
  fireEvent.click(row("New Fruit"));
  const name = screen.getByRole("textbox", { name: "Name" });
  for (const key of ["ArrowDown", "ArrowUp", "ArrowLeft", "ArrowRight"])
    assert.equal(press(key, name), true, `${key} stays the field's`);
  assert.equal(press("ArrowLeft", screen.getByRole("textbox", { name: "About" })), true);
  assert.equal(focused(), "Name");
  const back = screen.getByRole("button", { name: "Back" });
  back.focus();
  assert.equal(press("ArrowDown"), false);
  assert.equal(focused(), "Name");
  assert.equal(press("ArrowLeft", screen.getByRole("button", { name: "Save" })), false);
  assert.equal(title(), "Fruit");
  assert.equal(focused(), "New Fruit");
});

test("a key the menu does not take is handed to its host with the open pane", () => {
  const got: [string, string | null][] = [];
  render(<Menu onKey={(event, pane) => got.push([event.key, pane])} />);
  press("Enter");
  press("ArrowDown");
  fireEvent.click(row("Fruit"));
  press("Enter");
  press("ArrowDown");
  press("Escape");
  assert.deepEqual(got, [
    ["Enter", null],
    ["Enter", "fruit"],
  ]);
});

test("a row that goes away takes its open pane with it", () => {
  const { rerender } = render(<Menu />);
  fireEvent.click(row("Size"));
  assert.equal(title(), "Size");
  rerender(<Menu without="size" />);
  assert.equal(title(), null);
  assert.equal(focused(), "Print");
});

function Sheet({ onClose, onLeave }: { onClose: () => void; onLeave: () => void }) {
  const control = useRef<HTMLButtonElement>(null);
  return (
    <>
      <button type="button">Elsewhere</button>
      <button ref={control} type="button">
        Open
      </button>
      <PaneMenu
        sheet
        anchor={control}
        label="Details"
        data-layer="details"
        className="p-4 sm:right-0"
        onClose={onClose}
        onLeave={onLeave}
      >
        <a href="/all">See all</a>
      </PaneMenu>
    </>
  );
}

test("as a sheet it lies along the bottom over a dimmed page below sm, and a press outside leaves it", () => {
  let closed = 0;
  let left = 0;
  render(<Sheet onClose={() => closed++} onLeave={() => left++} />);
  const sheet = screen.getByRole("dialog", { name: "Details" });
  for (const part of [
    "max-sm:fixed",
    "max-sm:inset-x-0",
    "max-sm:bottom-0",
    "max-sm:z-(--aui-z-sheet)",
    "sm:absolute",
    "sm:z-(--aui-z-popover)",
    "p-4",
    "sm:right-0",
  ])
    assert.ok(sheet.className.split(" ").includes(part), `the sheet lacks ${part}`);
  assert.equal(sheet.dataset.layer, "details");
  const dim = sheet.previousElementSibling;
  assert.equal(dim?.getAttribute("aria-hidden"), "true");
  assert.match(
    dim?.className ?? "",
    /\bfixed inset-0\b.*\bsm:hidden\b/,
    "the page is dimmed on a phone only",
  );
  assert.equal(document.activeElement === sheet, true, "with no row, focus is on the sheet");
  assert.ok(screen.getByRole("link", { name: "See all" }));
  fireEvent.pointerDown(screen.getByRole("link", { name: "See all" }));
  fireEvent.pointerDown(screen.getByRole("button", { name: "Open" }));
  assert.equal(left, 0, "a press inside it or on its control is not a press outside");
  fireEvent.pointerDown(screen.getByRole("button", { name: "Elsewhere" }));
  assert.equal(left, 1);
  assert.equal(closed, 0);
  assert.equal(press("Escape", sheet), false);
  assert.equal(closed, 1);
});

test("a menu its host places has no dimmed page and listens for no press outside", () => {
  let closed = 0;
  render(<Menu onClose={() => closed++} />);
  const menu = screen.getByRole("dialog", { name: "Options" });
  assert.equal(menu.previousElementSibling, null);
  assert.equal(menu.getAttribute("tabindex"), null);
  fireEvent.pointerDown(document.body);
  assert.equal(closed, 0);
});
