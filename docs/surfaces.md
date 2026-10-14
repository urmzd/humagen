# One model, two surfaces

vultus defines each agent concept once, in `@vultus/core`, and draws it on the web and in Slack. This page maps every concept to both surfaces and lists the gaps that are still open.

- **Web**: `@vultus/render-web` (primitives), `@vultus/web` (web-only: composer, orb, status line, rail, preview)
- **Slack**: `@vultus/render-slack` (Block Kit, streaming, lifecycle), tested in a real workspace with `apps/slack-sandbox`
- **Check either one**: Storybook for the web; `pnpm sandbox post <fixture>` for Slack, where fixture names match `@vultus/core/fixtures`

## How a concept reaches each surface

```
core type ──► shared component (components/) ──► primitives ──► render-web  → DOM
                                                             └─► render-slack → Block Kit JSON
web-only component (web/) ─────────────────────────────────────► DOM
Slack-only signal (render-slack lifecycle/stream) ─────────────► Web API calls
```

Shared components use only primitives, never DOM or Block Kit. When a concept can't exist on one surface, the other surface gets its native equivalent, listed in the right-hand column below.

## Concept map

### A turn and its progress

| Concept | Web | Slack | Notes |
|---|---|---|---|
| Answer text | Markdown with a streaming caret | `markdown_text` stream chunks, then a `markdown` block | One 12k-character budget per message on Slack |
| Token streaming | `useTurn` plus the event reducer, live re-render | `chat.startStream` → `appendStream` → `stopStream` (`streamUpdate` diffs two turn snapshots) | Progress goes out before the text, and each append is awaited |
| Plan | Collapsible card with an N of M counter; it folds when done | `plan` block, or `task_display_mode: "plan"` | The title ends as the outcome: "Worked for 12s", "Error" or "Stopped" |
| Task (tool call) | One compact row with a status icon | `task_card`, or `task_update` chunks | Slack has no `pending` in task cards and no `waiting` or `denied`, so they are mapped (see `render-slack/src/status.ts`) |
| Waiting on approval | Static icon that never spins | `pending` in a plan, plus the note "Waiting for approval" | Anything blocked on a person must not animate |
| Sub-agent step | Task row: "Deploy agent · 6 steps · 12k tokens" | Same text in `details` | |
| Handoff | Muted line: "→ Switched to Billing agent · reason" | `context` line | |
| Sources | Compact chips under the answer, visible by default; inline `[n]` hover cards | Numbered `context` lines; `sources` attached to the task that used them | Cite where the claim is made |
| Code | Code block with path, language and copy | `context` line with the path, then `rich_text_preformatted` | |
| Terminal | Dark panel with a tail and "N earlier lines" | Exit/time/cwd line, then the last 40 lines preformatted | |
| Artifact (file) | A tile for each phase with an animated icon, Download and Open, and a preview panel | `card` (title, type · size, body, Download/Open) when ready; a muted line in other phases | The sandbox uploads a real file (`files.getUploadURLExternal`) |
| Request | The person's bubble (`RequestView`, `UserMessage`) | The message the answer threads under | On Slack `by` is a mention such as `<@U123>` |
| Feedback | Thumbs; a comment box opens after 👎; the chosen one stays pressed | `feedback_buttons` in `context_actions`, then "Rated helpful" once rated | Only after the final answer. `capabilities={{ feedback: false }}` leaves it out on both |
| Agents | `AgentList`: each agent's handle and what it is for | Same rows, for `/agent help` | |
| Usage | Tooltip: tokens, cache, cost, latency | Not shown | Cost is left out when there's no price, never shown as $0.00 |

### Asking a person

| Concept | Web | Slack | Notes |
|---|---|---|---|
| Approval | Card: summary first, preview fields, Approve, Edit, Ask, Always Allow, Cancel | `section`, `fields` and `actions`; a destructive action gets a `confirm` dialog | Only Approve is emphasized; Cancel stays unstyled |
| Batch approval | One card with a checkbox per row and an "Approve 2 of 2" button | `checkboxes` (all checked) plus Approve Selected / Cancel All | The host reads Slack's `state.values` when the button is clicked |
| Not the initiator | Ask only, plus "Only the person who started this…" | Same; the host answers other clickers with an ephemeral message | `canDecide(approval, user)` |
| Decision record | Buttons are replaced by "Approved by Dana · time" | `chat.update` to the same record | Rejected, timed out and cancelled all mean the action did not run |
| Clarification | Option buttons, an inline "Something Else" text field, "I'm Not Sure" | Unstyled buttons | |
| Raise a limit | Raise to fixed amounts, a number field with Raise Limit, Acknowledge when allowed | Same buttons; Raise Limit opens `amountModal` (`number_input`) | `number_input` is modal-only per the docs, so a message carries the button |
| Ask from a shortcut | The composer | `askForm` on the shortcut's trigger, `ENTRY.form` | |

### When things go wrong

| Concept | Web | Slack | Notes |
|---|---|---|---|
| Failure | Callout titled by who can fix it, with a recovery button, a copyable reference chip and the HTTP status | Section that states the level in words, plus action buttons | Retry is offered only when `isRetryable` is true. Report Issue only when the host can take a report (`capabilities.report`) |
| Alert in a modal | Callout | Native `alert` block (`text` is a text object) | Messages have no `alert` block |
| Silence | Muted "Still working, 2m so far" | `context` line | Sent once; never suggests retrying, because a retry would bill the turn twice |
| Fallback or retry by the runtime | Muted line | `context` line | |
| Stop | Status row for each phase (requested, acknowledged, failed, confirmed), with Resume | Native Stop button → `agent_session_stopped`; plan title "Stopped" | |
| System status | Banner that can be dismissed per incident | Not shown | |
| Toasts | `useToasts` | Ephemeral messages (`audience: "requester"`) | |

### Around the conversation

| Concept | Web | Slack | Notes |
|---|---|---|---|
| Lifecycle | Orb (thinking, searching, calling, delegating, composing, waiting, done, error) | One reaction on the user's message (👀 ✋ ✅ ❌ ⏹️) plus `agents.sessions.setStatus` | Each emoji has one meaning |
| Composer | `PromptInput`: suggestions, attachments, and Stop while running | Slack's own composer | Suggestions map to `setSuggestedPrompts`, up to 4 |
| Queue | Queued strip (edit, reorder, remove, send now), Cmd+Enter to send now, auto or manual mode | The host queues per thread and marks queued messages with ⏳ | `queueReduce`; nothing queued runs after a failure or a plain Stop |
| Status line | Lualine-style segments: status, model, approval mode, context meter, compaction, budget, queue | `SessionNotices` when something changes | `contextPressure`, `budgetPressure`, `headroom` |
| Compaction | Shimmer while compacting, then "freed 127k" | `context` line, plus a warning when compaction fails | |
| Budget | Meter that goes from warn to critical, and a spend banner | Callout plus "Add Budget" | Warns before the next turn would go over the cap |
| Turn rail | Waveform on the left: one tick per turn, hover to preview | Not shown (threads are the history) | |

## Open questions

1. **Interleaved turns.** A turn stores one `text` and one `tasks[]`. Text and tool calls aren't kept in the order they happened. Adding `blocks: (text | task)[]` would fix that for both surfaces.
2. **Branches and edits.** The sample has edit-and-resend, regenerate, fork and ◀ N/M ▶ navigation. The model has no tree yet; saige's conversation tree is a natural source for one.
3. **Multi-question clarifications.** A stepper on the web; on Slack, one modal with several inputs.
4. **Slash commands and skills in the composer.** Web only. Slack has native slash commands defined in the manifest.
5. **Evidence strength.** A score from unverified to strong, based on citation count and distinct domains. Show it on both surfaces, or only on the web?
6. **Agent sessions in Slack App Home.** A log of recent actions and the "Always Allow" grants that can be revoked.
7. **Delivery receipts.** Whether to borrow the sample's alerting copy ("didn't reach #x") for render-slack delivery errors.
8. **Producers.** Map saige's stream deltas and opentag's envelope events onto `TurnEvent`, so either runtime can feed both surfaces.
