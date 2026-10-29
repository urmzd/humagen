# Integrated local browser validation

This is incremental evidence, not full acceptance of both design sets.

## Persistent preview

The isolated preview uses loopback port 8081, a dedicated Postgres database and disk blob/workspace stores. The original 8080 preview remains available independently. The executable and console bundle come from an immutable integration snapshot.

At snapshot `68e0900` plus Company address fix `46d6b3e`, a real browser verified:

| Flow | Evidence | Result |
| --- | --- | --- |
| Profile save | Display name and briefing preference saved through the console | Passed |
| Server restart | Both preferences, conversation records and seed artifacts survived a native service restart | Passed |
| Personal Home | Personal label rendered before project-list hydration; the announcement appeared above the greeting | Passed |
| Start private work | Browser sent a first message to humagen-analyst in the canonical personal project returned by `/api/me` | Passed after the address regression fix |
| Real provider | Run `r759f806786a384be914455cd22ffed71` completed through Claude Haiku with one recorded model call and response `HUMAGEN_UI_VALIDATED` | Passed |
| Usage | Selected UTC day, chart and semantic table displayed actual bounded server data | Passed |

The provider is configured through an environment credential reference. The existing local direnv provider environment is reused; no credential value is checked in or included in this report. The earlier 1Password credential-resolution failure remains in its conversation as actual history.

## Regressions discovered by browser and CI checks

- `/api/me` prefixed the personal project name with an unsupported tilde. The exact returned name now works unchanged through agent discovery and private thread creation on memory and Postgres.
- Explicit Company agent homes disappeared from inherited agent pickers. Availability now normalizes the root address and preserves narrower project restrictions.
- Failed posts cleared the only copy of a message. Rejected or uncertain posts are retained in an editable held queue for explicit retry, without retrying automatically or replacing a newer draft.
- The screenshot seed attempted to publish completed private work without the current audience-preview token. The seed now uses the same preview/confirmation contract as the UI.

The final integrated wizard, source/integration controls, complete responsive screen suite, refreshed showcase and stack-wide CI remain separate acceptance gates.

## Inactive setup and native file checkpoint

The setup form now uses real registered providers and saved model choices, an explicit personal home before project hydration, bounded source/tool selections, recorded reviewer configuration and native approval rendering. Saving creates an inactive durable setup. Correction invalidates prior test capability before changing the specification; activation requires a completed actual model call. Starter assignments remain inactive until activation. Memory/Postgres race tests cover partial-write recovery, real registry restart, exact owner/revision test capability and explicit self-approval without overriding stricter native rules.

Validation for this checkpoint: 215 console DOM tests, setup/native platform race tests, API response-contract and read-confinement sweeps, console typecheck, and OpenAPI lint pass with no skipped tests. Full integrated acceptance and refreshed showcase captures remain pending.

Native DOCX tables now occupy actual Word XML and preview cells. Native rendering rejects meaningful unsupported fields instead of silently dropping them. Structured revision reading, text extraction and PDF export validate actual authored parts against retained metadata; original downloads remain unchanged. Model instructions require successful writes before dependent reads or publication.

## Real agent setup

At preview checkpoint `91ab15a` plus Company `5835f11`, the browser created `local-ui-validation` in the reader's personal project with no tool grants and self-approval disabled. The saved inactive agent ran test `rd1660fbc7b8b4a9f60cbd757d823ad5b` through the configured Claude provider. Its native ledger records one model call, completion, and response `HUMAGEN_AGENT_SETUP_VALIDATED`. Activation was enabled only after completion; clicking it opened the saved agent at revision 1. This browser check did not exercise a tool approval; native approval round trips have separate backend coverage.

## Real editable document finding

The initial live writer run published a DOCX while claiming a table was present. Independent inspection of `word/document.xml` found no table: the native encoder had retained rows in its revision metadata without rendering them in the document. The correction writes real Word table cells and rejects unsupported meaningful field combinations. Historical artifacts remain immutable, and inconsistent old revision metadata cannot create a phantom table in a preview or export. The real browser Request Changes flow then published version 2 (`art-HTD2QFTWPS7LAPKVI6LXMN4IXM`). Its preview rendered an actual two-row table, and independent inspection of `word/document.xml` confirmed exactly `[[Check, Result], [Document generation, Completed]]`. The original version 1 checksum remained unchanged. Clicking Download PDF produced a real browser download; `pdftotext` independently confirmed the title, paragraph and both table rows in the PDF.

## Reviewed publication and learning

The real browser reviewed the generated validation conversation's Launch audience and published it using the current preview capability. The same conversation address then showed `Shared in Launch`, and that placement survived the next native preview restart. Original run accounting and immutable artifact versions remained unchanged.

From completed real writer run `re40ea95ddc39deed6641d0b913778b7e`, Save as Skill opened the explicit whole-text review. The browser replaced the seed with reviewed reusable instructions and created personal draft `local-docx-review`, version 1, with no allowed tools. Its exact Library link opened the saved definition. It was not published or attached automatically. The review correctly withheld project learning from a reader whose Launch access came only from governance.

## Integrated native checks

At runtime checkpoint `a0d7059`, the complete Go race suite passed across 47 packages with 2,380 passing test/subtest results, zero failures and zero test skips, using isolated Postgres and S3 fixtures. Lint reported zero issues. All 15 workspace typechecks, all nine frontend test tasks, 556 console tests and 18 client tests passed; spec and generated-client checks passed. Later source graph and Library follow-ups have separate checkpoint evidence and require final integration checks.

Actual native DOCX and XLSX phone/desktop journeys passed original ZIP download, PDF export, a reviewed exact-version revision, new version 2 and an unchanged original checksum. Native email preference, Usage, Activity and exact default-skill controls passed 12 responsive captures. Personal project selection and conversation Instructions passed six further captures. These focused results do not replace the final complete screen suite or the eight showcase exports.

GitHub installation and new full-stack external webhook delivery are still unverified: the GitHub settings page continues waiting for passkey input, and an approved 1Password item read timed out after 60 seconds. No credential values are included here.

## Final private skill test

After correcting the client destination, the canonical native request tested `local-docx-review` version 1 using the existing personal `local-ui-validation` agent. Real Claude run `rd595bc4475bfe862423280db90be21fe` completed in the caller's canonical personal project with one recorded model call and `HUMAGEN_SKILL_TEST_VALIDATED`. This final request used the native API while Chrome awaited authentication; the component regression separately verifies the exact request body and eligible-agent controls. The draft remains unpublished, with no tools granted.

The complete responsive acceptance now passes 266 phone/desktop captures and 399 dark captures across 320, 768 and 1440 pixels. All eight showcase files were re-captured from the same seed and visually inspected. The standalone gallery's fixed-height clipping was corrected, and the native run image retains its complete turn heading. Full console checks after the personal-scope correction pass 574 tests with zero skips.

GitHub CLI authenticates successfully as `urmzd` and confirms App 5473438 with only Issues read/write and metadata read. Installation lookup with the ordinary CLI token returns HTTP 401, requiring the App JWT. The direct approved App-key read from 1Password returned `error initializing client: response: promptError`; no credential value was returned or logged. Browser confirmation also remained pending. These results do not establish sandbox installation or new webhook delivery.

## XLSX and PPTX acceptance coverage

`TestNativeAgentOfficeRevisionAcceptance` scripts native agent tool calls to write, publish, read, revise and republish XLSX and PPTX files. It inspects the actual worksheet and slide XML for the requested values, checks both PDF exports, captures the original bytes before revision and compares them afterward, checks publication approval and checks cross-tenant artifact access. `TestNativeOfficePDFExportPermissionsAndVersions` exercises both versions through the HTTP PDF endpoint, including missing read permission and cross-tenant access. These are deterministic scripted-model cases, not real-model validation; the existing separate publication approval tests cover approval and denial. Run with `go test -race ./internal/platform ./internal/httpapi -run 'TestNativeAgentOfficeRevisionAcceptance|TestNativeOfficePDFExportPermissionsAndVersions|TestPublishingWaitsForApproval'`.

No new live-model XLSX or PPTX execution is recorded by these tests. The earlier browser XLSX journey and real-Claude DOCX verification remain as described above; neither establishes a real-model PPTX journey or independently inspected model-authored XLSX content.

To run the optional real-model case, provide a model ID and a credential reference supported by `pkg/credref`, then explicitly opt in:

```sh
export HUMAGEN_LIVE_OFFICE_TEST=1
export HUMAGEN_LIVE_OFFICE_MODEL=your-anthropic-model-id
export HUMAGEN_LIVE_OFFICE_KEY_REF=env:ANTHROPIC_API_KEY
go test -tags live_integration ./internal/platform -run '^TestLiveNativeOfficeAcceptance$' -count=1 -timeout 10m -v
```

This sends synthetic prompts to Anthropic and may incur charges. The case verifies the actual Office parts and PDF text for two published versions. Its model calls are nondeterministic. The `live_integration` build tag keeps paid external checks out of the normal race suite; the tagged case fails unless its opt-in flag, model and credential reference are configured. Publication approval and original bytes captured before revision are covered by the deterministic case, not this opt-in case. No live execution was performed for this checkpoint.
