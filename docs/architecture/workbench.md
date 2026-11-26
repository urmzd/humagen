# The session workbench

A session's page keeps the thread on the left. The pane Details opens on the
right is a workbench: tabs that show what the agent is working with (a
browser or computer view, a terminal, its changes) and tools of the reader's
own (a search). This file is the contract for whoever builds a
tab. Read it before adding one.

## What exists

| Piece | File | Exports |
| --- | --- | --- |
| Tab registry | `apps/web/src/workbench.ts` | `WORKBENCH_TABS`, `WorkbenchTab`, `WorkbenchTabEntry`, `WorkbenchTabProps`, `WorkbenchSession`, `workbenchTabs`, `activeBench`, `openBench`, `WorkbenchOpen`, `useOpenWorkbench` |
| The pane | `apps/web/src/Workbench.tsx` | `SessionWorkbench` |
| Details, the first tab | `apps/web/src/SessionPanel.tsx` | `DetailsTab`, `useSessionSummary` |
| Tab addresses | `apps/web/src/route.ts` | `WORKBENCH_TAB_IDS`, `WorkbenchTabId`, `MAX_BENCH_REF`, `ConversationView.bench`, `ConversationView.benchRef` |
| What was done by hand, no React | `packages/core/src/human-activity.ts` | `humanActivity`, `createHumanActivityStore`, `HumanActivityStore`, `HumanActivityRecord`, `HumanActivityInput`, `HumanActivitySource`, `foldHumanActivity`, `splitHumanActivity`, `postWithHumanActivity`, `HUMAN_DETAIL_LEFT_OUT`, the `MAX_*` limits |
| The same, on the page | `apps/web/src/humanActivity.tsx` | `useHumanActivity`, `HumanActivityList`, `HumanActivityNote` |
| Words | `packages/components/src/copy.ts` | `workbench`, and one block per tab: `workbenchBrowser`, `workbenchTerminal`, `workbenchChanges`, `workbenchSearch` |

`SessionWorkbench` is mounted by `ThreadRoom` in `apps/web/src/Room.tsx`
where `SessionDetailsPanel` was. With only Details to show there is no tab
strip and the pane is the Details it always was. The strip is a component of
its own (`BenchStrip`), so that it brings the open tab into view when the
pane draws its tabs after a reload.

## Tabs and their addresses

The five ids are fixed in `WORKBENCH_TAB_IDS`: `details`, `browser`,
`terminal`, `changes`, `search`. The strip shows them in that
order. All five are addresses already, so a tab adds no route.

| Address | Opens |
| --- | --- |
| `/messages/{id}?panel=details` | Details |
| `/messages/{id}?panel=details&bench=terminal` | the Terminal tab |
| `...&bench=terminal&benchRef=box-2` | that tab with its own selection |
| `...&full=1` | the pane filling the page |

`benchRef` is the tab's own selection: a terminal, a file, a query. It is
one string of at most `MAX_BENCH_REF` (512) characters with no line breaks.
A tab that needs two values packs them into it and unpacks them itself. A
tab that is not built, or an id that does not exist, opens Details.

Files has no tab. A session's files, artifacts, scratchpads and sandboxes
already open in the Workspace pane (`pane.ts`, `?panel=files&tabs=`), which
has its own tabs, split and drafts. Details lists outputs and opens them
there through `openFile`, and its Files row opens the pane's files through
`openWorkspace`, as the header's Files control does (DES-080). A tab that
wants to show a file calls `openFile` with an artifact id, or
`openWorkspace` with a pane tab; it does not draw a second file viewer.

## Adding a tab

A tab's change is one new file and one entry.

1. Write `apps/web/src/Workbench<Name>.tsx` exporting a component that takes
   `WorkbenchTabProps`: `WorkbenchBrowser`, `WorkbenchTerminal`,
   `WorkbenchChanges`, `WorkbenchSearch`.
2. In `workbench.ts`, replace your slot's one line in `entries`. Keep the
   comment line above it. Import `lazyView` from `./lazy.tsx` if no tab has
   yet.

```ts
  // Terminal slot: a terminal in the session's sandbox (WorkbenchTerminal.tsx).
  terminal: {
    view: lazyView(() => import("./WorkbenchTerminal.tsx"), (m) => m.WorkbenchTerminal),
    available: (session) => session.summary?.environment.some((e) => e.kind === "sandbox") ?? false,
    fill: true,
  },
```

3. Put your words in your own block of `copy.ts` (`workbenchTerminal: {}`
   becomes an object). The tab's label and icon are already declared.
4. Add `apps/web/src/workbench-<name>.dom.test.tsx` and list it in
   `apps/web/src/dom.test.ts` next to `workbench.dom.test.tsx`.

`WorkbenchTabEntry`:

| Field | Meaning |
| --- | --- |
| `view` | The component. Read on first use through `lazyView`; the pane draws `SkeletonRows` while it is read and a failure with Retry when it cannot be. |
| `available` | Optional. Whether the session has anything for the tab. A tab that says no is left out of the strip. It is asked on every draw with what the pane already holds, so it must not fetch. `responses` and `summary` are undefined while they are read; return false for unknown. |
| `fill` | Optional. The view lays out its own height and scrolling (a terminal, a browser). Without it the pane pads the view by 20px and scrolls it. |

A tab named by the address is drawn even when `available` says no, since a
link or a card may open it first. It then shows its own empty state: a
title and one action, no paragraph of explanation.

`WorkbenchTabProps`:

| Prop | Meaning |
| --- | --- |
| `thread` | The conversation id. A session's starts with `s_`. |
| `runs` | Every run the page has read, oldest first. |
| `responses`, `summary` | Each answer's activity and the session's summary (`sessionSummary.ts`). Undefined while read. |
| `canPost` | Whether the reader may post here. Acting for them needs it. |
| `origin` | Where the session started, if anywhere. |
| `selection`, `onSelection(ref?)` | The tab's `benchRef`, and the way to set or clear it. |
| `openTab(id, ref?)` | Opens another tab. |
| `openFile(artifactId)` | Opens a saved file in the Workspace pane. |
| `record(input)` | Records something the reader did by hand. See below. |
| `ask(text)` | Puts words in the composer for the reader to send. Absent when they may not post. |
| `expanded` | The pane fills the page. |

Details is narrow. Any other tab gets the wide pane (44rem, at most 42% of
the window) and the header's Expand control fills the page. The pane is not
resized by dragging. Under 1024px it is the Inspector's sheet with the same
tabs; a `fill` tab gets the sheet's full height.

### A card in the thread opens a tab

```tsx
import { useOpenWorkbench } from "./workbench.ts";

function BrowserCard({ step }: { step: string }) {
  const open = useOpenWorkbench("browser");
  // Undefined outside a session's page, and while the tab is not built.
  if (!open) return <Plain />;
  return <button type="button" onClick={() => open(step)}>...</button>;
}
```

`ThreadRoom` provides `WorkbenchOpen` around the whole page. Shared packages
(`packages/web`, `packages/components`) cannot import it: pass an `onOpen`
prop down from a console file instead.

### Side Chat

Removed on 2026-11-21 (DES-089). A second conversation is a new session that
refers to this one.

## What a person did by hand

Anything the reader does by hand in a tab before their next message is
recorded, listed above the composer where each line can be removed, and
folded into that message, so the agent reads one message that covers it.
Nothing is sent before they submit.

A detail is text the person did not write (a command's output, a page's
title, an agent's answer) and it goes out under their name, so the list shows
all of it before sending: a line with a detail opens in place, by a press or
from the keyboard, and a long summary wraps. The list shows the newest four
lines and a button that counts and shows the earlier ones. When it is taller
than its box, a rule marks the edge that lines are out of view past.

```tsx
export function WorkbenchTerminal({ record }: WorkbenchTabProps) {
  const ran = (command: string, exit: number, output: string) =>
    record({
      source: "terminal",
      summary: `Ran ${command}, exit ${exit}`,
      detail: output,
    });
  // ...
}
```

Outside a tab's own component, use the hook with the conversation id:

```tsx
const { record, records, remove, clear } = useHumanActivity(thread);
```

`HumanActivityInput`:

| Field | Meaning |
| --- | --- |
| `source` | `terminal`, `browser`, `files`, `changes` or `other`. |
| `summary` | One line: what was done and how it came out. Line breaks become spaces; at most 200 characters. |
| `detail` | Optional. What the agent should read: output, a diff, an address. At most 600 characters; the rest is cut with an ellipsis, never through the middle of a character, so send the part that matters (the end of a failing command's output). |
| `at` | Optional ISO time. Defaults to now. |
| `key` | Optional. A later record with the same source and key replaces the earlier one, for something done over and over (saving one file). |

Record an outcome, not a keystroke: one record when a command finishes, when
a change is kept or discarded, when a file is saved. Record only what the
person did. What the agent did is already in its trace.

### How it is carried

`SessionPostInput` has no field for it and this change adds none, so it
rides in the message's text, after the person's words:

````text
Why did the test fail?

[Done by hand since my last message: 2]
```
1. Terminal 2026-11-19T14:02Z: Ran pnpm test, exit 1
    FAIL src/route.test.ts
2. Changes 2026-11-19T14:02Z: Discarded the change to route.ts
```
[End of done by hand]
````

The entries sit in a code block because the platform reads a message's text
for tags (`@agent`, `/skill`, `$skill`) and never reads a code block
(`pkg/threads/tags.go`). A record is kept as plain text: escape sequences
and control characters are dropped, since a post refuses them, and a
backtick becomes an apostrophe so nothing closes the block.
`TestWhatWasDoneByHandTagsNobody` pins this on the Go side.

`RoomComposer` posts through `postWithHumanActivity`, and `MessageView`
reads the section back with `splitHumanActivity` and draws it closed under
the person's words ("You did 2 things by hand"). Other surfaces (Slack, an
export) show the text as it is.

### Limits

| Limit | Value | At the limit |
| --- | --- | --- |
| Records per conversation | 50 | The oldest is dropped. It goes with no message, and the list says how many were dropped (`store.dropped`) until it is cleared or a message is posted. |
| Summary | 200 characters | Cut, with an ellipsis. |
| Detail | 600 characters | Cut, with an ellipsis. |
| The section | 4000 characters, or what the person's words leave of the API's 8000 | Records are written oldest first. One that does not fit with its detail is written with the line `(detail left out: no room in this message)` in its place, so the agent knows there was more; the list marks each such line and counts them before sending (`bare` of `foldHumanActivity`). One that does not fit at all is left out with every one after it; the section's last line says how many, the list marks each and says so before sending, and they stay recorded for the message after. |

A post that fails clears nothing. A message queued behind a reply carries
what is recorded when its turn comes, not when it was typed. The record is
kept in memory: it survives moving between pages and is lost on reload.

## Rules for a tab's change

- Touch your one file, your one entry in `workbench.ts`, your block in
  `copy.ts`, your test and its line in `dom.test.ts`. Anything the tab needs
  from the server goes through `api/openapi.yaml`, then `make client`, with
  one permission per route.
- Do not edit `Workbench.tsx`, `humanActivity.tsx`,
  `packages/core/src/human-activity.ts`, `Room.tsx`, `SessionPanel.tsx`,
  `packages/web/src/Inspector.tsx`, `route.ts` or the `workbench` block of
  `copy.ts`. If the contract is missing something, say so in your report
  instead of changing it in a tab's branch.
- Do not post, call an agent or write to the session from `record`. It only
  records.
- No fake data. A tab with no backend for something leaves it out.
- Text a person reads is `text-aui-base` (14px) or larger. Secondary text is
  `text-aui-muted`, which is above 7:1 on the pane in light and dark. Do not
  use `text-aui-xs`, `text-aui-2xs` or `text-aui-sm` in a tab.
- Use the console's own pieces: `Reference`, `ItemCard`, `Pill`,
  `SkeletonRows`, `LoadMore`, `menuRow`, and `openStream` for any event
  stream. Icons come from `@humagen/render-web`.
