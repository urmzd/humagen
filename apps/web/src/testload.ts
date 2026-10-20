import { createServer } from "vite";

/**
 * Loads test files through Vite, so they may use JSX and the linked warp
 * packages. The runner is plain node, which reads .ts but not JSX. Each
 * file's tests register with the node:test run of the file that called this.
 */
export async function loadTests(files: string[]): Promise<void> {
  const server = await createServer({
    configFile: false,
    root: new URL("..", import.meta.url).pathname,
    logLevel: "error",
    appType: "custom",
    resolve: { dedupe: ["react", "react-dom"] },
    server: { middlewareMode: true, hmr: false, ws: false, watch: null },
    optimizeDeps: { noDiscovery: true, include: [] },
  });
  try {
    for (const file of files) await server.ssrLoadModule(file);
  } finally {
    await server.close();
  }
}
