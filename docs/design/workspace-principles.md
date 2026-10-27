# Workspace design principles

Humagen is a place to move shared work forward. A person should immediately
understand where work belongs, who is doing it, and what they can do next.
These project choices apply the [reference research](reference-research.md),
checked October 26, 2026, to the existing React console and shared UI packages.

## Principles and references

| Principle | Reference | Humagen decision |
| --- | --- | --- |
| Organize around the work | [Harvey II](https://www.harvey.ai/blog/introducing-harvey-ii) | Projects hold conversations, people, context and decisions. Keep the project title and primary action on its page. |
| Keep human control understandable | [Codex designer Ed Bayes](https://softwareengineeringdaily.com/wp-content/uploads/2026/01/SED1898-OpenAI.txt) | Show states beside their work. A failed attempt is not an approval request or a completed outcome awaiting review. |
| Spend complexity deliberately | [Karri Saarinen](https://karrisaarinen.com/posts/every-feature-should-earn-its-place/) | Keep the permission-aware Library, Agents, Notifications and Operations hubs as direct destinations; reveal their related tools through existing tabs. |
| Prove behavior before polish | [Dust's Édouard Wautier](https://dust.tt/blog/field-study-prototypes-over-mockups) | Storybook renders the console's actual components with loading, empty, read-failure, paging-failure and interaction fixtures. |
| Judge the delivered experience | [Jenny Wen](https://jennywen.substack.com/p/dont-trust-the-design-process) | Preserve working flows. Inspect narrow screens, keyboard focus, real content and error recovery. |

These are Humagen's interpretations, not claims that the referenced designers
designed or endorsed Humagen. The research distinguishes brand work from UI,
historical contributions from current roles, and Harvey II's August launch from
the October UI refresh. Individual authorship of that refresh is unverified.

## Visual system

Use the shared renderer font entry and semantic typography. The newer workspace
request selects IBM Plex Sans Variable and IBM Plex Mono; this pass must not
restore the older Figtree/Bricolage imports. Body text follows the shared 14px
scale, secondary text is 13px, metadata is 12px. Storybook loads the same font
entry and console CSS.
Use the current semantic colors: page `--aui-bg`, supporting plane
`--humagen-plane`, text `--aui-fg`, secondary text `--aui-muted`, boundaries
`--humagen-rule`, and focus `--aui-accent`. Do not introduce a competing palette.

Use flat surfaces and whitespace to express hierarchy. The conversation list
uses stable rows, not separate cards. The context column is one supporting
plane. Agent identity keeps its assigned color and `AgentMark`; work status
always includes words, so meaning never depends on color alone. Work waiting
on a person remains still.

```text
Navigation           Project title                 New Conversation
New Conversation     Project description
Projects             Overview   Context   Agents   ...
Notifications
Agents               Conversations                 Shared context
Library              All  In progress  Attention   Notes
Operations           Agent  Work title    State    Schedules
Conversations        Last activity
```

The reading order on a phone is title, primary action, tabs, work, then
supporting context. Rows reflow their status under their title. Filters wrap;
metadata uses spacing, not separator dots. Existing native addresses, access
checks, project selection and independent scrolling remain authoritative.

## Evidence and boundaries

The `Workspace/Project work` Storybook group demonstrates the first pass using
`ProjectWork`, `SidebarItem`, `Page` and `Tabs` from the real
console. Fixture-only actions are logged locally; no model calls or workspace
writes happen in stories. Project filters cover loaded rows, with an explicit
notice when older pages exist. A failed read is recoverable and never reported
as an empty project. Pending pagination from an obsolete scope is discarded.

This pass changes the project work presentation and navigation. Approval,
execution and outcome semantics come from their existing APIs; the new filters
do not invent approval counts or infer run completion. Requirements and current
verification evidence are tracked in [design.md](../../design.md).
