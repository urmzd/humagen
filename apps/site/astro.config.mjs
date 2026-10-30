import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "astro/config";

// Static HTML only: no adapter, no client framework. React runs at build
// time alone, to render the console's icons and mark to markup. Pages build
// to dist/<page>/index.html; CloudFront rewrites /deploy to /deploy/index.html.
export default defineConfig({
  site: "https://withhumagen.com",
  output: "static",
  trailingSlash: "ignore",
  build: { format: "directory" },
  // Fetch an internal page on hover or focus, so a click lands at once and
  // the cross-document view transition (global.css) has the page in hand.
  prefetch: { prefetchAll: true, defaultStrategy: "hover" },
  vite: {
    plugins: [tailwindcss()],
    build: {
      rollupOptions: {
        // radix-ui (through the console's button classes) marks its modules
        // "use client"; it runs only at build time here, so the note is noise.
        onwarn(warning, warn) {
          if (warning.code === "MODULE_LEVEL_DIRECTIVE") return;
          warn(warning);
        },
      },
    },
  },
});
