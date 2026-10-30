// Social preview images, drawn at build time from the site's own pieces: the
// Humagen mark, each page's card copy in content/site.ts, the console's light
// tokens read from render-web's theme.css and the self-hosted IBM Plex files.
// Nothing is hand-drawn, so a card cannot drift from the copy or the theme.
// Satori lays the card out to SVG, resvg rasterizes it to PNG.
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { Resvg } from "@resvg/resvg-js";
import { createElement as h, type ReactNode } from "react";
import satori from "satori";
import { type Card, hero, type Page, pages, site } from "../content/site";
import { mark } from "./console";

export const OG_WIDTH = 1200;
export const OG_HEIGHT = 630;

const require = createRequire(import.meta.url);
const file = (spec: string) => readFileSync(require.resolve(spec));

const fonts = [
  {
    name: "IBM Plex Sans",
    data: file("@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-400-normal.woff"),
    weight: 400 as const,
  },
  {
    name: "IBM Plex Sans",
    data: file("@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-600-normal.woff"),
    weight: 600 as const,
  },
  {
    name: "IBM Plex Mono",
    data: file("@fontsource/ibm-plex-mono/files/ibm-plex-mono-latin-400-normal.woff"),
    weight: 400 as const,
  },
];

// The light theme's tokens, as theme.css declares them first (its :root).
const theme = readFileSync(require.resolve("@humagen/render-web/theme.css"), "utf8");
const token = (name: string): string => {
  const m = theme.match(new RegExp(`--${name}:\\s*([^;]+);`));
  if (!m) throw new Error(`theme.css has no --${name}`);
  return m[1].trim();
};

/** oklch(L C H) to #rrggbb, through OKLab and linear sRGB. */
export function oklchToHex(l: number, c: number, hDeg: number): string {
  const hr = (hDeg * Math.PI) / 180;
  const a = c * Math.cos(hr);
  const b = c * Math.sin(hr);
  const l_ = (l + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m_ = (l - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s_ = (l - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const lin = [
    4.0767416621 * l_ - 3.3077115913 * m_ + 0.2309699292 * s_,
    -1.2684380046 * l_ + 2.6097574011 * m_ - 0.3413193965 * s_,
    -0.0041960863 * l_ - 0.7034186147 * m_ + 1.707614701 * s_,
  ];
  return `#${lin
    .map((v) => {
      const x = Math.min(1, Math.max(0, v));
      const s = x <= 0.0031308 ? 12.92 * x : 1.055 * x ** (1 / 2.4) - 0.055;
      return Math.round(s * 255)
        .toString(16)
        .padStart(2, "0");
    })
    .join("")}`;
}

const color = (name: string): string => {
  const m = token(name).match(/^oklch\(\s*([\d.]+)\s+([\d.]+)\s+([\d.]+)\s*\)$/);
  if (!m) throw new Error(`--${name} is not a plain oklch() color`);
  return oklchToHex(Number(m[1]), Number(m[2]), Number(m[3]));
};

const colors = {
  bg: color("aui-bg"),
  fg: color("aui-fg"),
  muted: color("aui-muted"),
  border: color("aui-border"),
  accent: oklchToHex(
    Number(token("aui-mode-accent-l")),
    Number(token("aui-mode-accent-c")),
    Number(token("aui-accent-h")),
  ),
};

function layout(card: Card): ReactNode {
  const logo = `data:image/svg+xml;base64,${Buffer.from(
    mark(56).replace(
      'fill="currentColor"',
      `xmlns="http://www.w3.org/2000/svg" fill="${colors.fg}"`,
    ),
  ).toString("base64")}`;
  const grid = `${colors.fg}0d`; // the page's quiet grid: fg at 5%
  return h(
    "div",
    {
      style: {
        width: OG_WIDTH,
        height: OG_HEIGHT,
        boxSizing: "border-box",
        display: "flex",
        flexDirection: "column",
        justifyContent: "space-between",
        padding: "64px 72px",
        backgroundColor: colors.bg,
        backgroundImage: `linear-gradient(to right, ${grid} 1px, transparent 1px), linear-gradient(to bottom, ${grid} 1px, transparent 1px)`,
        backgroundSize: "48px 48px",
        fontFamily: "IBM Plex Sans",
        color: colors.fg,
      },
    },
    h(
      "div",
      { style: { display: "flex", alignItems: "center", gap: 16 } },
      h("img", { src: logo, width: 56, height: 56 }),
      h("span", { style: { fontSize: 40, fontWeight: 600, letterSpacing: "-0.012em" } }, site.name),
    ),
    h(
      "div",
      { style: { display: "flex", flexDirection: "column", gap: 24 } },
      h("div", {
        style: { width: 64, height: 6, borderRadius: 3, backgroundColor: colors.accent },
      }),
      h(
        "div",
        {
          style: {
            fontSize: 68,
            fontWeight: 600,
            lineHeight: 1.12,
            letterSpacing: "-0.012em",
            maxWidth: 1000,
          },
        },
        card.title,
      ),
      h(
        "div",
        { style: { fontSize: 32, lineHeight: 1.35, color: colors.muted, maxWidth: 960 } },
        card.line,
      ),
    ),
    h(
      "div",
      {
        style: {
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          paddingTop: 24,
          borderTop: `1px solid ${colors.border}`,
          fontFamily: "IBM Plex Mono",
          fontSize: 24,
          color: colors.muted,
        },
      },
      h("span", {}, new URL(site.url).host),
      h("span", {}, hero.status.replace(/\.$/, "")),
    ),
  );
}

export type OgImage = {
  name: string;
  url: string;
  png: Buffer;
  width: number;
  height: number;
  alt: string;
};

type Key = keyof typeof pages;
const rendered = new Map<Key, Promise<OgImage>>();

async function render(key: Key): Promise<OgImage> {
  const card = pages[key].card;
  const svg = await satori(layout(card) as Parameters<typeof satori>[0], {
    width: OG_WIDTH,
    height: OG_HEIGHT,
    fonts,
  });
  const png = Buffer.from(
    new Resvg(svg, { fitTo: { mode: "width", value: OG_WIDTH } }).render().asPng(),
  );
  // Hashed name: the deploy caches images as immutable, so new art gets a new URL.
  const name = `${key}-${createHash("sha256").update(png).digest("hex").slice(0, 10)}`;
  return {
    name,
    url: new URL(`/og/${name}.png`, site.url).href,
    png,
    width: OG_WIDTH,
    height: OG_HEIGHT,
    alt: `${site.name}: ${card.title}. ${card.line}`,
  };
}

/** The card for one page key, rendered once per build. */
export function ogImage(key: Key): Promise<OgImage> {
  let p = rendered.get(key);
  if (!p) {
    p = render(key);
    rendered.set(key, p);
  }
  return p;
}

/** Every card, for the image endpoint. */
export const ogImages = () => Promise.all((Object.keys(pages) as Key[]).map(ogImage));

/** The card a page shows when shared: its own, or the home page's. */
export function ogImageFor(page: Page): Promise<OgImage> {
  const key = (Object.keys(pages) as Key[]).find((k) => pages[k].path === page.path);
  return ogImage(key ?? "home");
}
