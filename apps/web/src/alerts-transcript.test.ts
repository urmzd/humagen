import { GlobalRegistrator } from "@happy-dom/global-registrator";
import { loadTests } from "./testload.ts";

GlobalRegistrator.register({ url: "http://localhost:5173/" });
await loadTests(["/src/alert-placement.dom.test.tsx", "/src/alerts-transcript.dom.test.tsx"]);
