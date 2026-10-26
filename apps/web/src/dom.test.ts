import { GlobalRegistrator } from "@happy-dom/global-registrator";
import { loadTests } from "./testload.ts";

// Interaction tests: .dom.test.tsx files run against a DOM.
//
// node --test runs each *.test.ts file in a process of its own, so the DOM
// registered here (happy-dom's window, document, fetch and the rest, as
// globals) reaches only the files listed below, never the static renders in
// tsx.test.ts. A DOM file renders with @testing-library/react, drives the
// page with fireEvent, waits with findBy* or waitFor, calls cleanup after
// each test (node:test has no global afterEach for the library to hook),
// and answers the console's requests with stubFetch from ./dom.ts.
// To add one, name it something.dom.test.tsx and list it here.
//
// Assert on what an element says or is (assert.ok(!queryByText(...)),
// el.disabled), not on the element itself: a failing assertion prints its
// values, and printing a happy-dom node does not finish.
//
// The DOM stays registered for the life of the process, because node:test
// may still be running the tests after this module returns.
GlobalRegistrator.register({ url: "http://localhost:5173/" });
await loadTests(["/src/operations.dom.test.tsx","/src/workspace.dom.test.tsx", "/src/storage.dom.test.tsx", "/src/agents.dom.test.tsx", "/src/projects.dom.test.tsx", "/src/directory.dom.test.tsx", "/src/room.dom.test.tsx", "/src/chat.dom.test.tsx", "/src/roles.dom.test.tsx", "/src/governance.dom.test.tsx", "/src/read-audit.dom.test.tsx", "/src/layout.dom.test.tsx", "/src/settings.dom.test.tsx", "/src/connections.dom.test.tsx", "/src/preferences.dom.test.tsx", "/src/home.dom.test.tsx", "/src/library.dom.test.tsx"]);
