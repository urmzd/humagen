import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { useQueue } from "@humagen/web";
import { cleanup, render } from "@testing-library/react";
import { brandCopy, servedBranding, setBrand, setEntryAgent } from "./branding.ts";
import { aiNoticeHeight, Composer } from "./Composer.tsx";
import { ComposerFooter } from "./SessionUsage.tsx";

// A composer ends with one line: the product is AI and can make mistakes
// (DES-085). The sentence is itself the link to the workspace's page of
// terms, only when its branding names one (branding.terms_url). A footer row
// takes the line into its middle (DES-087).

const product = defaultCopy.product;
const sentence = defaultCopy.dock.aiNotice;

afterEach(() => {
  cleanup();
  // Everything brandCopy sets, so no test after this one reads another's brand.
  setBrand("", null);
  setEntryAgent("", "");
  defaultCopy.product = product;
  defaultCopy.dock.aiNotice = sentence;
});

function Bar() {
  const queue = useQueue({ onStart: () => {}, onInterrupt: () => {} });
  return <Composer placeholder="Reply" queue={queue} footer={<div data-testid="footer" />} />;
}

const notice = (container: HTMLElement) =>
  container.querySelector<HTMLElement>('[data-layer="ai-notice"]');

test("the composer says the product is AI and can make mistakes, with no link when no terms are named", () => {
  const { container } = render(<Bar />);
  const line = notice(container);
  assert.ok(line);
  assert.equal(line.textContent, "Humagen is AI and can make mistakes.");
  assert.equal(line.querySelector("a"), null);
  // It is the last line of the composer, after the footer row.
  assert.equal(line.previousElementSibling?.getAttribute("data-testid"), "footer");
});

test("branding gives the notice the product's name and the workspace's terms", () => {
  brandCopy(defaultCopy, {
    ...servedBranding("Acme"),
    terms_url: "https://acme.example/terms",
  });
  const { container } = render(<Bar />);
  const line = notice(container);
  assert.ok(line);
  // The sentence is the link: no word is added for it.
  assert.equal(line.textContent, "Acme is AI and can make mistakes.");
  const link = line.querySelector("a");
  assert.ok(link);
  assert.equal(link.textContent, "Acme is AI and can make mistakes.");
  assert.equal(link.getAttribute("title"), "Terms");
  assert.equal(link.getAttribute("href"), "https://acme.example/terms");
  assert.equal(link.getAttribute("target"), "_blank");
  assert.equal(link.getAttribute("rel"), "noreferrer");
});

test("the name is the product's, whatever a workspace calls the sender of its messages", () => {
  // The copy key `product` names who the product's own messages are from.
  brandCopy(defaultCopy, { ...servedBranding("Acme"), copy: { product: "Updates" } });
  assert.equal(defaultCopy.product, "Updates");
  const { container } = render(<Bar />);
  assert.equal(notice(container)?.textContent, "Acme is AI and can make mistakes.");
});

test("a workspace may reword the sentence, and the name still goes where it says", () => {
  brandCopy(defaultCopy, {
    ...servedBranding("Acme"),
    copy: { "dock.aiNotice": "Check what {name} tells you." },
  });
  const { container } = render(<Bar />);
  assert.equal(notice(container)?.textContent, "Check what Acme tells you.");
});

test("an address the browser would not open as https is never a link", () => {
  for (const url of [
    "javascript:alert(1)",
    "http://acme.example/terms",
    "HTTPS://acme.example/terms",
    "https://:443/terms",
    "/terms",
    "",
  ]) {
    setBrand("Acme", url);
    const { container } = render(<Bar />);
    assert.equal(notice(container)?.querySelector("a"), null, url);
    cleanup();
  }
});

test("a name with a dollar sign, and a sentence that names the product twice, are written as they are", () => {
  // String.replace reads $$ and $& in a replacement, and fills one {name}.
  brandCopy(defaultCopy, {
    ...servedBranding("Cash$&Co"),
    copy: { "dock.aiNotice": "{name} is AI. Check what {name} says." },
  });
  const { container } = render(<Bar />);
  assert.equal(notice(container)?.textContent, "Cash$&Co is AI. Check what Cash$&Co says.");
});

test("the Side Chat's composer says it too: on a phone, and with the workbench expanded, it is the only composer on view", () => {
  const sidechat = readFileSync(new URL("./WorkbenchSidechat.tsx", import.meta.url), "utf8");
  assert.match(sidechat, /<Composer /);
  // No composer turns the line off: there is no option to.
  const composer = readFileSync(new URL("./Composer.tsx", import.meta.url), "utf8");
  assert.match(
    composer,
    /\{noticeSlot \? createPortal\(<AiNotice \/>, noticeSlot\) : <AiNotice \/>\}/,
  );
});

test("a footer row takes the notice into its middle, between where the session runs and who answers", () => {
  function Rowed() {
    const queue = useQueue({ onStart: () => {}, onInterrupt: () => {} });
    return (
      <Composer
        placeholder="Reply"
        queue={queue}
        footer={<ComposerFooter start={<span data-testid="start" />} agent="lead" />}
      />
    );
  }
  const { container } = render(<Rowed />);
  const lines = container.querySelectorAll('[data-layer="ai-notice"]');
  assert.equal(lines.length, 1, "said once");
  const row = container.querySelector('[data-layer="composer-footer"]');
  assert.ok(row);
  assert.deepEqual(
    [...row.children].map((c) => c.getAttribute("data-layer")),
    ["composer-footer-start", "composer-footer-notice", "composer-footer-end"],
  );
  assert.equal(lines[0]?.parentElement?.getAttribute("data-layer"), "composer-footer-notice");
});

test("the place kept for the composer while a session loads keeps a line for the notice", () => {
  // The placeholder keeps a line for the notice, of the height the notice has.
  // Where the composer is wide enough to hold the notice in its footer row,
  // styles.css takes that line away.
  const css = readFileSync(new URL("./styles.css", import.meta.url), "utf8");
  assert.match(
    css,
    /@container composer \(min-width: 44rem\) \{[\s\S]*?\[data-layer="composer-placeholder-notice"\] \{ display: none; \}/,
  );
  const chat = readFileSync(new URL("./Chat.tsx", import.meta.url), "utf8");
  assert.match(chat, /data-layer="composer-placeholder-notice" className=\{aiNoticeHeight\}/);
  assert.equal(aiNoticeHeight, "h-6");
  const { container } = render(<Bar />);
  assert.match(notice(container)?.className ?? "", /\bmin-h-6\b/);
  assert.match(notice(container)?.className ?? "", /\bleading-6\b/);
});
