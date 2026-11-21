import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { useQueue } from "@humagen/web";
import { cleanup, render } from "@testing-library/react";
import { brandCopy, servedBranding, setTermsUrl } from "./branding.ts";
import { Composer } from "./Composer.tsx";

// Under every composer, one line says the product is AI and can make
// mistakes. It links to the workspace's page of terms only when its branding
// names one (branding.terms_url).

const product = defaultCopy.product;

afterEach(() => {
  cleanup();
  setTermsUrl(null);
  defaultCopy.product = product;
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

test("the notice is in the branded product's name and links to the workspace's terms", () => {
  const copy = { product: "Humagen" };
  brandCopy(copy, { ...servedBranding("Acme"), terms_url: "https://acme.example/terms" });
  defaultCopy.product = copy.product;
  const { container } = render(<Bar />);
  const line = notice(container);
  assert.ok(line);
  assert.equal(line.textContent, "Acme is AI and can make mistakes. Terms");
  const link = line.querySelector("a");
  assert.ok(link);
  assert.equal(link.getAttribute("href"), "https://acme.example/terms");
  assert.equal(link.getAttribute("target"), "_blank");
  assert.equal(link.getAttribute("rel"), "noreferrer");
});

test("an address that is not https is never a link", () => {
  for (const url of ["javascript:alert(1)", "http://acme.example/terms", "/terms", ""]) {
    setTermsUrl(url);
    const { container } = render(<Bar />);
    assert.equal(notice(container)?.querySelector("a"), null, url);
    cleanup();
  }
});
