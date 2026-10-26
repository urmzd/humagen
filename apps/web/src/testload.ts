import { after } from "node:test";
import { createServer } from "vite";

/**
 * Loads test files through Vite, so they may use JSX and the linked humagen
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
  } catch (error) {
    await server.close();
    throw error;
  }
  // Lazy preview modules are imported while tests run, after registration.
  after(() => server.close());
}
