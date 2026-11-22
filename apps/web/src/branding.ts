import { agentLook } from "./agentHues.ts";
import type { Branding } from "./api.ts";

export type { Branding };

/** The page's own name, which the server writes into index.html from the deployment's branding. */
function servedName(): string {
  if (typeof document === "undefined") return "Humagen";
  const meta = document.querySelector<HTMLMetaElement>('meta[name="application-name"]');
  return meta?.content || "Humagen";
}

/** What the console calls itself before the workspace's branding is read. */
export function servedBranding(name = servedName()): Branding {
  return {
    name,
    wordmark: name,
    title: name,
    logo: null,
    logo_dark: null,
    logo_alt: name,
    style: null,
    starters: null,
    copy: {},
    entry_agent: "humagen",
    terms_url: null,
  };
}

/** The product's name and the workspace's page of terms, once branding is applied. */
let brand: { name: string; terms: string | null } = { name: "", terms: null };

/** The product's name from the branding, "" before it is applied. Not the copy key `product`. */
export function productName(): string {
  return brand.name;
}

/** The page of terms the composer's notice links to, or null. Only https with a host is a link. */
export function termsUrl(): string | null {
  return brand.terms;
}

/** Remember the product's name and its page of terms. Anything but an https address is no page. */
export function setBrand(name: string, terms?: string | null): void {
  brand = { name, terms: httpsAddress(terms) };
}

function httpsAddress(address: string | null | undefined): string | null {
  if (!address?.startsWith("https://")) return null;
  try {
    const url = new URL(address);
    return url.protocol === "https:" && url.hostname ? address : null;
  } catch {
    return null;
  }
}

/** The entry agent's own name, and the name people see it by, once branding is applied. */
let entry = { agent: "", name: "" };

/** Remember the workspace's entry agent and the name it is shown by. Empty forgets it. */
export function setEntryAgent(agent: string, name: string): void {
  entry = { agent, name };
}

/**
 * The name people see for an agent: its look's display name, else the
 * workspace's name for its entry agent, else its handle as a name
 * ("security-reviewer" is "Security Reviewer"). People still tag the handle.
 */
export function agentShown(name: string): string {
  return agentLook(name)?.display_name || titled(brandShown(name));
}

/** A handle as a name: each word capitalized, dashes and underscores as spaces. */
export function titled(handle: string): string {
  return handle
    .split(/[-_\s]+/)
    .filter(Boolean)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ");
}

/**
 * The name an agent is known by before any look: what its default bot is
 * drawn from, so naming an agent never changes its face.
 */
export function brandShown(name: string): string {
  return entry.agent !== "" && name === entry.agent && entry.name ? entry.name : name;
}

/**
 * Write the workspace's text over the console's own, in place, by key such
 * as navigation.agents. Only a key that names a string is written: the
 * server checks the keys, and this keeps a stale one from breaking a page.
 * Text a module reads once, as it loads, keeps the console's own.
 */
export function applyCopy(target: object, overrides: Record<string, string>): void {
  for (const [key, value] of Object.entries(overrides)) {
    const path = key.split(".");
    const last = path.pop();
    let node: unknown = target;
    for (const part of path) {
      node = node && typeof node === "object" ? (node as Record<string, unknown>)[part] : undefined;
    }
    if (
      last &&
      node &&
      typeof node === "object" &&
      typeof (node as Record<string, unknown>)[last] === "string"
    ) {
      (node as Record<string, unknown>)[last] = value;
    }
  }
}

/**
 * The data-accent a person's accent choice is drawn with. workspace, and no
 * choice yet, is the workspace's branded accent, which is red without one.
 */
export function accentAttribute(accent?: string): string {
  return !accent || accent === "workspace" ? "brand" : accent;
}

/**
 * Brand the console's text: the product's own sender and the entry agent
 * are called by the product's name, then the workspace's text by key goes
 * over the console's own, so a copy key for the sender still wins.
 */
export function brandCopy(target: { product?: unknown }, branding: Branding): void {
  if (typeof target.product === "string") target.product = branding.name;
  setEntryAgent(branding.entry_agent || "", branding.name);
  setBrand(branding.name, branding.terms_url);
  applyCopy(target, branding.copy ?? {});
}
