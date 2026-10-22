import assert from "node:assert/strict";
import { test } from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import { HomeLines, HomePage } from "./HomePage.tsx";

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

test("Home says what needs the person and what is new in their projects, quietly", () => {
  const updates = [
    { id: "m-1", project: "Field", projectTitle: "Field engineering", subject: "Field offsite", from: { kind: "person" as const, name: "ana" }, at: "2026-10-21T09:00:00Z", unread: true },
    { id: "m-2", project: "", projectTitle: "Acme", subject: "Holiday", from: { kind: "person" as const, name: "root" }, at: "2026-10-20T09:00:00Z", unread: false },
  ];
  const html = renderToStaticMarkup(<HomeLines needsYou={2} updates={updates} onInbox={() => {}} onUpdate={() => {}} />);
  assert.match(html, /Needs you · 2/);
  assert.match(html, /What&#x27;s new/);
  assert.match(html, /Field offsite.*Posted in Field engineering.*Holiday.*Posted in Acme/);
  // Nothing waiting on a person moves.
  assert.doesNotMatch(html, /animate|warp-enter/);
  assert.equal(renderToStaticMarkup(<HomeLines needsYou={0} updates={[]} onInbox={() => {}} onUpdate={() => {}} />), "");
});
