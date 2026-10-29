# Usage and Activity implementation

Usage uses the existing analytics service with bounded exact intervals and 24-hour buckets. The 7, 14 and 30-day views use UTC calendar days, including the current partial day. Selecting a day requests its exact interval from the server; chart/table changes do not substitute a client estimate for the per-agent breakdown.

Completed without help means a completed run with a fully observed history and no recorded human question, including called work. A recorded question remains needed-person evidence after it is answered. Missing older or recovered history is unknown, never labelled autonomous. A lost observation followed by recovery cannot create a claim that no person helped. The state-backed run index retains this classification across restart.

Mean duration counts ended runs, including waiting. Handoffs out count handoff/delegate children started within the interval, only when both child and parent are readable. Latest failure links contain only readable run IDs. Source scope and immutable execution accounting remain unchanged.

Activity filters use real run modes, recorded tool spans and recorded human questions. The upper host supplies the additional authored-outcome decision predicate from its typed contract. Tool visibility and selected-run navigation are independent. The host can control filters, selected day, range and display through routes using the optional controlled props.

Verification includes fail-before memory/Postgres assistance and exact-boundary regressions, restart and failed-write recovery, real HTTP bounds, keyboard/pointer selection, chart/table semantics, stale response handling, retry and navigation callbacks. `scripts/screens/observability.ts` supplies native screen actions for the final routed host. Screens are not claimed complete until the upper integration mounts and runs those states.
