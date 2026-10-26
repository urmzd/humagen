# Prototype control acceptance audit

This inventory covers each interactive element in all 27 checked-in HTML references, including repeated navigation and reference-only controls. It is a traceability record, not a claim that untested controls have passed acceptance. `control-audit.json` retains source/control identity, the original label, implementation paths and outstanding status for every entry.

The canonical host is checkpoint `1974d93`. The integrated console suite passes 514 tests without skips; API contract/private and outsider confinement sweeps pass. Those tests do not replace phone/desktop capture or full journey acceptance of every control.

| Status | Controls |
| --- | ---: |
| integration_validation_pending | 239 |
| implemented | 161 |
| reference | 74 |
| source_followup_pending | 3 |
| needs_semantic_review | 1 |
| operations_followup_pending | 1 |
| implementation_pending | 5 |

Concrete remaining acceptance work:

- Source suggestions and Library integration capabilities/Slack direction need their final lower checkpoints and mounted browser journeys.
- Effective policy inheritance/provenance, effective pause causes, reminders and handoff input/owner detail are under the Operations follow-up.
- The email delivery toggle has no native email transport implementation. Native console notifications alone do not satisfy it.
- The prototype affected-work dependency-map action needs a precise corresponding real workflow. Existing source dependency links and the recorded Agent Network are available.
- Agent setup saves one inactive starter assignment. Multiple starters require a separate durable replacement/activation design; the existing Assignments page supports additional real assignments after activation.
- Initiative progress uses actual confined records. The prototype's sample milestone fractions are not an implemented milestone system.
- All current integration-validation entries still need final phone/desktop states and primary-journey checks. The eight showcase images must be refreshed from that final reproducible seed.
