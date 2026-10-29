# Application validation

Local validation on October 23, 2026:

| Check | Result |
|---|---|
| `pnpm test` | All 9 workspace test tasks passed, including the new Office parser and conversation interaction checks |
| `pnpm run typecheck` | All 15 package tasks passed |
| `pnpm exec biome check` | Passed |
| `pnpm --filter @humagen/console build` | Passed; Office parsing is a separate, lazily loaded chunk |
| `pnpm --filter @humagen/storybook build` | Passed |
| `go test -buildvcs=false -race -p 2 ./...` | All packages passed with Go 1.26.10 |
| `pnpm test:server` | 4 Slack integration tests passed against the real offline server, none skipped |
| Console browser sweep | 148 screenshots at 390 x 844 and 1280 x 800 passed overflow, gutter, target-size, title and text-overlap checks |
| `pnpm validate:slack` | Blocked: the enforced network policy rejects `slack.com` with HTTP 403 |

The browser suite covers the conversation shell, sharing, context, people,
Activity, saved Office files, slide navigation and presentation mode, plus
revision requests that preserve an unsent draft. Inspector controls are
checked for actual clickability above the conversation header. Activity's
run selection and Escape behavior have an additional targeted browser check
at both widths.

The real server uses development identity, seeded offline agents and
isolated state. No model or external collaboration system is called. The
new source case imports its document through the real knowledge APIs.
The Office cases explicitly fixture the artifact APIs with real PPTX,
DOCX and CSV bytes from `exports/`; they do not claim the offline agents
generated those files. Immutable artifact versions, failed history reads,
conversation boundaries, clarification submission and draft preservation
also have interaction tests. The parser tests cover formats, relationships,
table limits, inert HTML, XML declarations and bounded response reads.

The separate concept records describe 88 browser checks and 13 export
checks of `index.html`. They are not production app test results.

The existing console bundle size warning remains. The external Slack Block
Kit check is unverified here; local Slack rendering, parity and real-server
checks passed. No server contract changed, so no generated client update was
needed. See [the implementation notes](../architecture/conversation-workspace.md) for
the remaining placement, accounting and ingestion work and for the limits
of Office text previews.

Before merge, GitHub Actions also confirmed the public Slack Block Kit
validation that was blocked locally. Its screen job exposed a false overlap
from messages already scrolled out of view. The collector now clips both
axes to scrolling regions, and the routed conversation case explicitly
scrolls to its end. CI retains the hidden `.screens` directory as an artifact
so a failed capture can be inspected.
