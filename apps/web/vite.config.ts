import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import { apiTarget } from "./src/legacy.ts";

// The server from `make dev`. The console never talks to it cross-origin: the
// dev server proxies, so the session cookie and the cross-origin check behave
// exactly as they do when the server serves the built console itself.
const api = apiTarget(process.env);

export default defineConfig({
  plugins: [react(), tailwindcss()],
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
