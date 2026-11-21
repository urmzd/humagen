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
// el.disabled), not on the element itself. Printed in full, a happy-dom node
// walks its whole window; ./test-assert.ts and ./dom-print.ts make a failing
// assert or a log print it in a few words instead.
//
// The DOM stays registered for the life of the process, because node:test
// may still be running the tests after this module returns.
GlobalRegistrator.register({ url: "http://localhost:5173/" });
await loadTests([
  "/src/composer-overlay.dom.test.tsx",
  "/src/pane-menu.dom.test.tsx",
  "/src/bigquery-connection.dom.test.tsx",
  "/src/transcript.dom.test.tsx",
  "/src/connection-flows.dom.test.tsx",
  "/src/sidebar-recents.dom.test.tsx",
  "/src/agent-builder.dom.test.tsx",
  "/src/session-panel.dom.test.tsx",
  "/src/session-context-panel.dom.test.tsx",
  "/src/session-setup.dom.test.tsx",
  "/src/workbench.dom.test.tsx",
  "/src/workbench-browser.dom.test.tsx",
  "/src/workbench-terminal.dom.test.tsx",
  "/src/workbench-changes.dom.test.tsx",
  "/src/workbench-sidechat.dom.test.tsx",
  "/src/workbench-search.dom.test.tsx",
  "/src/session-center.dom.test.tsx",
  "/src/session-ux.dom.test.tsx",
  "/src/conversation-overview.dom.test.tsx",
  "/src/github-tab.dom.test.tsx",
  "/src/model-providers.dom.test.tsx",
  "/src/agent-store.dom.test.tsx",
  "/src/agent-effort.dom.test.tsx",
  "/src/agent-look.dom.test.tsx",
  "/src/agent-slack-app.dom.test.tsx",
  "/src/agent-home.dom.test.tsx",
  "/src/eval-candidates.dom.test.tsx",
  "/src/history.dom.test.tsx",
  "/src/voice-notes.dom.test.tsx",
  "/src/voice-settings.dom.test.tsx",
  "/src/automations.dom.test.tsx",
  "/src/background.dom.test.tsx",
  "/src/catalog.dom.test.tsx",
  "/src/corpora.dom.test.tsx",
  "/src/corpus-evals.dom.test.tsx",
  "/src/email-preferences.dom.test.tsx",
  "/src/patterns.dom.test.tsx",
  "/src/lazy.dom.test.tsx",
  "/src/message-refs.dom.test.tsx",
  "/src/item-page.dom.test.tsx",
  "/src/wave.dom.test.tsx",
  "/src/sidebar-views.dom.test.tsx",
  "/src/approval-toggle.dom.test.tsx",
  "/src/replay.dom.test.tsx",
  "/src/usage-page.dom.test.tsx",
  "/src/operations.dom.test.tsx",
  "/src/source-lifecycle.dom.test.tsx",
  "/src/onedrive-source.dom.test.tsx",
  "/src/workspace.dom.test.tsx",
  "/src/workspace-files.dom.test.tsx",
  "/src/workspace-hub.dom.test.tsx",
  "/src/scratchpads.dom.test.tsx",
  "/src/memories.dom.test.tsx",
  "/src/memory-files.dom.test.tsx",
  "/src/artifact-save.dom.test.tsx",
  "/src/code-editor.dom.test.tsx",
  "/src/navigation-guard.dom.test.tsx",
  "/src/storage.dom.test.tsx",
  "/src/agents.dom.test.tsx",
  "/src/projects.dom.test.tsx",
  "/src/directory.dom.test.tsx",
  "/src/room.dom.test.tsx",
  "/src/session-answers.dom.test.tsx",
  "/src/chat.dom.test.tsx",
  "/src/timeline.dom.test.tsx",
  "/src/roles.dom.test.tsx",
  "/src/governance.dom.test.tsx",
  "/src/read-audit.dom.test.tsx",
  "/src/layout.dom.test.tsx",
  "/src/analytics-chart.dom.test.tsx",
  "/src/settings.dom.test.tsx",
  "/src/connections.dom.test.tsx",
  "/src/preferences.dom.test.tsx",
  "/src/home.dom.test.tsx",
  "/src/home-runs.dom.test.tsx",
  "/src/composer-early.dom.test.tsx",
  "/src/library.dom.test.tsx",
  "/src/agent-setup.dom.test.tsx",
  "/src/conversation-sources.dom.test.tsx",
  "/src/outcome-actions.dom.test.tsx",
  "/src/mobile.dom.test.tsx",
  "/src/slash-commands.dom.test.tsx",
  // Waits on real timers, so it loads last: every hook is registered by then.
  "/src/sidebar-peek.dom.test.tsx",
  "/src/workspace-graph.dom.test.tsx",
]);
