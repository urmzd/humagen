import { GlobalRegistrator } from "@happy-dom/global-registrator";
import { loadTests } from "./testload.ts";

// Keep notification polling and reader-switch cases in their own DOM process.
GlobalRegistrator.register({ url: "http://localhost:5173/" });
await loadTests(["/src/notifications.dom.test.tsx"]);
