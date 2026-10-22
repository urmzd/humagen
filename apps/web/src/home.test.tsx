import assert from "node:assert/strict";
import { test } from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import type { HomeNotification, HomeUpdate } from "@humagen/core";
import { HomeBoxes, HomePage } from "./HomePage.tsx";

const prompt = <form aria-label="Message"><textarea placeholder="Ask anything" /><button type="submit">Send message</button></form>;

test("Home puts the prompt on the page before a conversation starts", () => {
  const html = renderToStaticMarkup(<HomePage greeting="Good morning, Ada" canRun hasAgents>{prompt}</HomePage>);
  assert.match(html, /Good morning, Ada/);
  assert.match(html, /<textarea/);
  assert.match(html, /Send message/);
  assert.doesNotMatch(html, /Recent conversations|Your projects|Open inbox|Reference|Start a conversation/);
});

test("Home hides the prompt when the person cannot run agents", () => {
  const html = renderToStaticMarkup(<HomePage greeting="Hello" canRun={false} hasAgents>{prompt}</HomePage>);
  assert.doesNotMatch(html, /<textarea|<form/);
  assert.match(html, /do not have permission/);
});

test("Home explains why a workspace with no agents cannot start a conversation", () => {
  const html = renderToStaticMarkup(<HomePage greeting="Hello" canRun hasAgents={false}>{prompt}</HomePage>);
  assert.doesNotMatch(html, /<textarea|<form/);
  assert.match(html, /agent needs to be configured/);
});

const at = (minutes: number) => new Date(Date.parse("2026-10-21T12:00:00Z") - minutes * 60_000).toISOString();
const need = (id: string, minutes: number, from = "lead", summary = `published report-${id}.md`): HomeNotification => ({
  kind: "inbox",
  id,
  from,
  summary,
  at: at(minutes),
  url: `/inbox?message=${id}`,
});
const update = (id: string, subject: string, projectTitle: string, unread: boolean, minutes = 10): HomeUpdate => ({
  id,
  project: projectTitle,
  projectTitle,
  subject,
  from: { kind: "person", name: "ana" },
  at: at(minutes),
  unread,
});
const ready = <T,>(value: T) => ({ state: "ready" as const, value });
const noop = () => {};
const boxes = (props: Partial<Parameters<typeof HomeBoxes>[0]>) =>
  renderToStaticMarkup(<HomeBoxes onOpen={noop} onInbox={noop} onUpdate={noop} onUpdates={noop} {...props} />);
/** The text of each box's heading, in order. */
const headings = (html: string) => [...html.matchAll(/<h2[^>]*>(.*?)<\/h2>/g)].map((m) => (m[1] ?? "").replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").replace(/ ,/g, ",").trim());

test("Needs you is a box of the five newest things waiting on the person, from whom, with the count of all of them and the way to the rest", () => {
  const items = [need("m-1", 50), need("m-2", 5, "any-run-fails", "triage failed"), need("m-3", 40), need("m-4", 30), need("m-5", 20), need("m-6", 10)];
  const html = boxes({ notifications: ready({ items, inbox: { total: 9 } }), updates: ready([update("a-1", "Launch moves", "Launch", true)]) });
  assert.deepEqual(headings(html), ["Needs you, 9", "What&#x27;s new"]);
  assert.match(html, />Open Inbox</);
  const shown = [...html.matchAll(/published (report-m-\d)\.md|(triage failed)/g)].map((m) => m[1] ?? m[2]);
  // Newest first, at most five: the oldest (m-1) is counted, not listed.
  assert.deepEqual(shown, ["triage failed", "report-m-6", "report-m-5", "report-m-4", "report-m-3"]);
  assert.match(html, /<span class="font-medium">any-run-fails<\/span> triage failed/);
  // Each row is one control that opens it.
  assert.equal([...html.matchAll(/<li><button type="button"/g)].length, 6);
  // Nothing waiting on a person moves.
  assert.doesNotMatch(html, /animate|humagen-enter|aui-skeleton/);
});

test("an item above normal says so in words before when it came, and a normal one says nothing more", () => {
  const urgent = { ...need("m-1", 5, "any-run-fails", "triage failed"), severity: "critical" as const };
  const html = boxes({ notifications: ready({ items: [urgent, need("m-2", 10)], inbox: { total: 2 } }) });
  assert.match(html, /triage failed<\/span><span[^>]*>Urgent · [^<]+</);
  assert.match(html, /report-m-2\.md<\/span><span[^>]*>(?!Urgent|Warning)[^<]+</);
});

test("What's new lists announcements as rows, unread first and marked in words, with where each was posted and the way to all of them", () => {
  const html = boxes({
    notifications: ready({ items: [need("m-1", 5)], inbox: { total: 1 } }),
    updates: ready([update("a-1", "Holiday", "Acme", false, 5), update("a-2", "Field offsite", "Field engineering", true, 60)]),
  });
  assert.match(html, />See All</);
  assert.match(html, /Unread: <\/span><span class="font-medium">Field offsite<\/span>.*Posted in Field engineering · .*Holiday<\/span>.*Posted in Acme · /);
  assert.equal([...html.matchAll(/class="sr-only">Unread/g)].length, 1);
  // Read and unread headlines start in the same column.
  assert.equal([...html.matchAll(/class="flex w-2 shrink-0/g)].length, 2);
});

test("a box with nothing in it says so in words, rather than vanishing", () => {
  const html = boxes({ notifications: ready({ items: [], inbox: { total: 0 } }), updates: ready([update("a-1", "Holiday", "Acme", false)]) });
  assert.deepEqual(headings(html), ["Needs you", "What&#x27;s new"]);
  assert.match(html, /Nothing needs you right now/);
  const quiet = boxes({ notifications: ready({ items: [need("m-1", 5)], inbox: { total: 1 } }), updates: ready([]) });
  assert.match(quiet, /Nothing new from your projects/);
});

test("a new person with nothing waiting and nothing new gets one box that says what Home is for", () => {
  const html = boxes({ notifications: ready({ items: [], inbox: { total: 0 } }), updates: ready([]) });
  assert.deepEqual(headings(html), ["Welcome"]);
  assert.match(html, /When an agent needs you/);
  assert.doesNotMatch(html, /Nothing needs you|Open Inbox/);
});

test("while Home reads, its boxes say so without moving, and a read that failed says where to look instead", () => {
  const loading = boxes({ notifications: { state: "loading" }, updates: { state: "loading" } });
  assert.deepEqual(headings(loading), ["Needs you", "What&#x27;s new"]);
  assert.equal([...loading.matchAll(/role="status"[^>]*>Loading</g)].length, 2);
  assert.doesNotMatch(loading, /Welcome|aui-skeleton|animate/);
  const failed = boxes({ notifications: { state: "failed" }, updates: ready([]) });
  assert.match(failed, /could not be loaded. It is all in the inbox/);
  assert.doesNotMatch(failed, /Welcome/);
});

test("a box the person may not read is left out, and with neither there is nothing", () => {
  const html = boxes({ notifications: ready({ items: [need("m-1", 5)], inbox: { total: 1 } }) });
  assert.deepEqual(headings(html), ["Needs you, 1"]);
  assert.equal(boxes({}), "");
});

test("on a phone a row keeps to its column: the headline truncates and what is under it takes a line of its own", () => {
  const html = boxes({
    notifications: ready({ items: [need("m-1", 5)], inbox: { total: 1 } }),
    updates: ready([update("a-1", "New site-visit checklist for installs at every Globex site", "Field engineering", true)]),
  });
  // The two boxes stack below the medium breakpoint and sit side by side from it.
  assert.match(html, /class="mx-auto w-full max-w-4xl px-4 pt-8 grid items-start gap-4 md:grid-cols-2"/);
  for (const m of html.matchAll(/<button type="button" class="([^"]*)"/g)) {
    const cls = (m[1] ?? "").split(" ");
    if (!cls.includes("text-left")) continue;
    assert.ok(cls.includes("w-full") && cls.includes("min-w-0") && cls.includes("min-h-11"), cls.join(" "));
  }
  assert.ok(html.includes('<span class="flex min-w-0 flex-1 flex-col"><span class="min-w-0 truncate text-aui-sm text-aui-fg">'));
});
