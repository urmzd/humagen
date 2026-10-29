# Cross-project handoff contract

Status: proposed. This document does not enable cross-project execution or
change current permissions. It describes a staged design for review.

## Current boundary

Agent calls are governed by the caller's pinned spawn grant, the callee's call
policy, action policies and applicable approvals. Agent availability is checked
in the caller's execution project. A handoff contract records the parent project
and thread, accountable owner, goal, completion condition, deadline and bounded
chain. A cycle or more than three hops escalates.

`pkg/call` currently creates a child with a root and parent edge, deterministic
identity, depth and fan-out limits, and a budget lease. Placement derives the
child's project, ancestry and client from the root. Handoff mode may inherit
the parent's source and delivery metadata. The executor freezes explicit
resolved inputs before execution. Operation controls, private-context rules and
source grants remain independent of an agent-call grant. The ledger decides
the accepted run's outcome and usage is recorded at the run's place.

See [agent operations](../architecture/agent-operations.md),
[call primitive](../../pkg/call/call.go),
[placement and admission](../../internal/platform/admit.go),
[handoff recording](../../internal/platform/handoffs.go),
[call policy](../../internal/platform/call_policy.go) and
[usage accounting](../../pkg/usage/usage.go).

Merely changing the handoff's project or accepting a destination argument in
the current child edge would not establish a destination execution boundary.

## Decision

First support reviewed context transfer without transferring execution
authority. A destination owner may use the accepted package to initiate normal
work. Separately, propose opt-in destination execution after the required
authorization, accounting and recovery machinery exists.

Agent-to-agent invocation must remain within `pkg/call`, the repository's one
call primitive. Extend that primitive with an explicit, versioned
cross-project request and validated correlation contract. The destination
execution is admitted as a destination-owned root with its own runtime,
project ancestry, identity, source grants, budget and delivery. It is linked
to the source through a restricted correlation record, not through a normal
child edge whose root implicitly controls placement. This requires an explicit
extension to the current call and admission contracts, not a second agent-call
path or an untrusted tag pretending to be a root. A person independently
starting work from a reviewed transfer is an ordinary destination invocation,
not an agent-to-agent call.

The first execution version requires per-request destination acceptance.
Preauthorized service principals are a future design decision, not an implicit
grant. Same-tenant projects are in scope; cross-tenant delegation is not.

## Alternatives

| Approach | Authority and tradeoff |
| --- | --- |
| Reviewed transfer only | Source export is reviewed and destination accepts a frozen package. No run, credential or tool authority crosses the boundary. Destination work is independently initiated. |
| Explicit destination execution | Enables asynchronous delegation with destination authority and accounting, but needs two-sided authorization, separate result release and durable reconciliation. |
| Move a normal child to another project | Rejected. Root placement, inherited delivery and tree accounting currently assume one project boundary; changing a field would silently undermine those assumptions. |

## Reviewed transfer

A versioned proposal names the destination, goal, done-when, deadline and
bounded selected material. Resolve references with the source reader's ordinary
permissions and freeze the exact bytes, version and provenance before review.
An authorized source reviewer approves disclosure of the exact package to the
named destination. Edits require a new version and approval. Preserve the
existing [private-context publication guards](../architecture/company-workspace.md):
private connected sources, personal memory or skills, person-scoped tools and
retained operation contracts require a reviewed text-only summary. A frozen raw
copy is still raw and is refused. This summary path copies only the exact
reviewed human text, with no original thread, run, source, citation, file,
operation contract, private instruction or capability. Check original and
current source visibility when building the review preview and again before
releasing the approved version. Eligible non-private material may use a frozen
copy; provenance remains in the restricted source/correlation record.

A destination-authorized person separately accepts the package into a
destination-controlled location. The package is untrusted task data: it grants
no access and cannot become system instructions or tool permissions. Enforce
size and retention bounds, and preserve source provenance in a confined record.
A correlation ID grants no access to either project and must not reveal hidden
projects, private threads or their counts.

Returning a result is another transfer in the opposite direction. A completed
run does not authorize export. A destination-authorized reviewer approves the
exact result package and a source-authorized recipient accepts it under source
visibility rules. Neither side receives raw transcripts or access to the
other side's original objects by virtue of the link.

## Explicit execution contract

An agent-originated request identifies the tenant from the authenticated
principal and carries:

- Source project, run, handoff and requester; accountable source owner.
- Destination project, agent and pinned or explicitly selected revision;
  accountable destination owner and execution principal.
- Immutable transfer package ID, version and digest; goal, completion
  condition, deadline, output format and allowed return destination.
- A positive destination budget cap in micro-USD, expiry, cancellation behavior,
  unique request ID and policy restrictions carried from the source.
- Approval decisions bound to the canonical effective contract digest,
  reviewers, scope and expiry, plus a restricted correlation ID.

An approval for the agent name alone is insufficient. An edit to the payload,
destination, identity, output policy or other material term invalidates the
affected approval. Existing stricter approval requirements are independent:
source action Ask, callee approval and destination acceptance do not replace
one another.

### Admission and execution

1. Check source caller spawn and call policy, action policy, tool restrictions,
   pause and existing approvals. The request must not evade an unavailable
   target by claiming ordinary in-project execution.
2. Require the source disclosure approval and destination acceptance for the
   exact contract. The destination owner must be authorized to accept the
   package, choose the execution principal and commit the budget.
3. Immediately before admission revalidate both sides' authorization,
   current policy, approval validity and expiry, project existence, agent
   availability, pause and setup state, and destination source grants.
   Deny or require review when the checks cannot be completed.
4. Admit through the existing destination `Invoke` and run ledger after
   `pkg/call` has validated the cross-project request. Do not copy the source
   root, thread, actor, client, source handles, delivery routes or credentials.
   Destination project ancestry and normal budget policies apply.
5. Run with the actual authorized destination principal. A person-scoped tool
   uses only that person's eligible credential; a missing credential fails
   closed. Never impersonate the source requester or fall back to a process
   credential. Destination sources require destination grants and ordinary
   checks; the package is not a grant to retrieve source-project content.
6. Preserve source restrictions such as ForceAsk and exact tool restrictions
   as additional ceilings, alongside destination policy and approvals. If
   they cannot be represented and enforced at every relevant action, refuse
   the request. Destination tool approvals remain independently required.

The destination-owned root has its own normal call tree. To prevent cross-project
bounces from resetting the source's bounded delegation, the validated
cross-project contract also carries a durable, server-authenticated causal
chain. Enforce the existing cycle and three-hop ceiling across both ordinary
and cross-project calls before admission. The chain is an authorization input,
not an unrestricted visible trace; failure to recover or validate it denies
the call. Cancellation across projects must be explicitly authorized and
recorded; it must not rely on an ordinary child cascade.

## Accounting and attribution

Charge each destination run once to its destination project and ancestors,
agent and applicable client using ordinary admission and usage ledger semantics.
Source runs remain charged to their original place. A confined attribution
view can connect destination cost to the source request without appending the
same spend again as ordinary source-project usage.

A source tree lease does not automatically bound a separately admitted root.
Enforce the positive destination cap through destination admission and runtime
metering. If source-side budget enforcement is required, implement a separate
durable reservation and settlement mechanism before offering that guarantee:
atomically reserve against the source budget, bind it to the request and
destination cap, reconcile actual usage, and release unused reservation only
when terminal status and late usage are reconciled. Avoid double charging
shared ancestors and keep unpriced or non-USD usage explicit. Until this
exists, expose source cost attribution as reporting only.

## Durability, audit and failure

Use versioned compare-and-set transitions such as proposed, export-approved,
destination-accepted, reserved, admitted, terminal, result-reviewed and
delivered, with denied, expired, cancelled and needs-person outcomes. Approval
and payload persistence must succeed before admission. Bind a deterministic
destination run identity to the immutable request digest; replay with different
terms fails.

On restart, reconcile the reservation and actual ledger before retrying
admission. A repeated request joins the same run, including when a response was
lost. If admission is uncertain and cannot be resolved, block for review rather
than creating another run. A missed deadline retains the real run. Cancellation
or revocation races are recorded and request a destination-authorized stop;
they cannot undo completed external side effects. Never infer completion from
silence or release a result without its own review.

Record the request, digest, actors, decisions, relevant policy versions,
admission, destination run ID, usage, state transitions and release decision.
Apply both project and private-thread read rules to every projection. Each side
may see only what it is authorized to see; the correlation key is not a
cross-project read capability. Use bounded queries and durable audit records,
and avoid disclosing hidden names or existence through errors.

## Implementation and acceptance

1. Add versioned transfer and correlation records to `pkg/operations`, with
   exact-byte review, confined provenance, compare-and-set and durable recovery.
2. Extend `pkg/call` and platform admission to validate the explicit
   cross-project contract and create the destination root through existing
   invocation and ledger paths. Preserve ordinary child behavior and defaults.
3. Add scoped transfer, acceptance, execution and result-release permissions,
   APIs and presentation. Update OpenAPI and regenerate clients when routes are
   implemented. Check both original and current visibility at every read.
4. Implement destination caps and, if needed, separate source reservations.
   Add restricted attribution without duplicate spend.
5. Test memory and Postgres restart and failed writes at every transition;
   duplicate requests, changed approved bytes, expiry, revocation, concurrent
   policy tightening, cycle and hop limits, unavailable agents, private
   context, result leakage, credentials and source isolation, budget exhaustion,
   cancellation races, late usage and hidden-object confinement.
6. Roll out transfer first. Gate destination execution until these invariants
   and end-to-end tests pass. Keep cross-project execution disabled by default.

Open decision: whether a later version can replace per-request destination
acceptance with a narrowly scoped, explicitly provisioned service principal.
This proposal does not authorize that behavior.
