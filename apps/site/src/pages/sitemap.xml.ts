import type { APIRoute } from "astro";
import { pages, site } from "../content/site";

export const GET: APIRoute = () => {
  const urls = Object.values(pages)
    .map((p) => `  <url><loc>${new URL(p.path, site.url).href}</loc></url>`)
    .join("\n");
  const body = `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${urls}\n</urlset>\n`;
  return new Response(body, { headers: { "Content-Type": "application/xml" } });
};
