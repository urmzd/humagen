# AGENTS.md

## What this is

A pnpm + turbo monorepo of React 19 packages that render one agent UI model to the web and to Slack Block Kit.

## Layout rules

- `packages/core`: types, the event reducer, the action contract, queue, session, inbox and fixtures. No React.
- `packages/primitives`: the primitive contract. Each renderer implements every primitive.
- `packages/components`: shared components built **only** from primitives. No DOM, no Block Kit, no hooks with effects (Slack renders synchronously).
- `packages/render-web`, `packages/render-slack`: one implementation per surface.
- `packages/web`: components that exist only on the web (composer, orb, statusline, rail, preview, inbox panel).
- `apps/storybook`: the web UI. `apps/slack-sandbox`: the real Slack test bench.

## Commands

Prefix with `eval "$(fnm env)" && fnm use 24` when node isn't on PATH.

| Task | Command |
|------|---------|
| Format and lint | `pnpm exec biome check --write` |
| Typecheck | `pnpm run typecheck` |
| Test | `pnpm test` |
| Slack validity | `pnpm sandbox validate` |
| Storybook | `pnpm storybook` |

## Conventions

- When you add a fixture to `@vultus/core/fixtures`, it is picked up by the sandbox registry and should get a story. Fixture names appear in the docs as `pnpm sandbox post <name>`.
- Every Slack-facing change must pass `pnpm sandbox validate`. Don't guess Block Kit shapes: check them against docs.slack.dev or `blocks.validate`.
- Copy lives in `packages/components/src/copy.ts`. Buttons use Title Case, everything else sentence case, and there are no emoji in buttons or headers.
- Anything blocked on a person (`waiting`) must not animate.
