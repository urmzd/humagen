import { defineConfig } from "astro/config";

// Static HTML only: no adapter, no client framework. Pages build to
// dist/<page>/index.html; CloudFront rewrites /deploy to /deploy/index.html.
export default defineConfig({
  site: "https://withhumagen.com",
  output: "static",
  trailingSlash: "ignore",
  build: { format: "directory" },
});
