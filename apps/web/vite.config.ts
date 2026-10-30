import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";
import { defaultCopy } from "../../packages/components/src/copy.ts";
import { apiTarget } from "./src/legacy.ts";
import { offlinePage } from "./src/offlinePage.ts";
import { precacheList } from "./src/swPolicy.ts";

// The server from `make dev`. The console never talks to it cross-origin: the
// dev server proxies, so the session cookie and the cross-origin check behave
// exactly as they do when the server serves the built console itself.
const api = apiTarget(process.env);

/**
 * The installed app's service worker (src/sw.ts), built to /sw.js beside the
 * offline screen. Once the build is written, the worker gets the list of
 * files to precache and a version derived from them, so a build that
 * changes any asset installs as a new version and one that changes nothing
 * does not.
 */
function serviceWorker(): Plugin {
  return {
    name: "humagen-service-worker",
    apply: "build",
    buildStart() {
      this.emitFile({ type: "chunk", id: "src/sw.ts", fileName: "sw.js" });
    },
    generateBundle() {
      this.emitFile({
        type: "asset",
        fileName: "offline.html",
        source: offlinePage(defaultCopy.app),
      });
    },
    writeBundle(options, bundle) {
      const dir = options.dir ?? "dist";
      const precache = precacheList(Object.keys(bundle));
      const hash = createHash("sha256");
      for (const url of precache) hash.update(url).update(readFileSync(join(dir, url)));
      const manifest = { version: hash.digest("hex").slice(0, 16), precache };
      const file = join(dir, "sw.js");
      const code = readFileSync(file, "utf8");
      // The minifier may quote the marker with any of the three quotes.
      const marker = /(["'`])__HUMAGEN_SW_MANIFEST__\1/;
      if (!marker.test(code)) throw new Error("sw.js has no manifest marker");
      writeFileSync(
        file,
        code.replace(marker, () => JSON.stringify(JSON.stringify(manifest))),
      );
    },
  };
}

export default defineConfig({
  plugins: [react(), tailwindcss(), serviceWorker()],
  resolve: {
    // Shared workspace packages use the console's React instance.
    dedupe: ["react", "react-dom"],
  },
  server: {
    host: "127.0.0.1",
    port: 5173,
    fs: { allow: ["../.."] },
    proxy: {
      "/api": { target: api, changeOrigin: false },
      "/auth": { target: api, changeOrigin: false },
    },
  },
});
