# Final native acceptance

Both approved design sets are implemented through the native platform and console. Requirements 001–064 have implementation and evidence mappings; external-account authorization remains separately identified for requirements 029–031.

## Automated checks

| Check | Recorded result |
| --- | --- |
| Full Go race suite | 47 packages, 2,397 passing test/subtest results, zero failures or test skips |
| Persistence fixtures | Isolated Postgres and S3 enabled; seven packages have no test files |
| Go lint | Zero issues |
| Workspace types and tests | All 15 typecheck tasks and all nine frontend test tasks pass |
| Console after responsive corrections | 574 tests pass without skips, including the personal test-project correction |
| Network Slack validation | 79 tests pass without skips |
| OpenAPI and client | Spec valid, 18 client tests pass, regeneration has no diff |
| Default responsive suite | 266 phone/desktop captures pass |
| Full dark responsive suite | 399 captures pass at 320×740, 768×1024 and 1440×900 |
| Showcase | All eight PNGs regenerated and visually reviewed; native result and setup flows replace placeholder shots |

The full Go result is recorded at `1f5bdcd`; later production changes affect console components and the standalone gallery, with focused native checks. The private-skill project correction preserves backend authorization and is covered by a real configured adapter on memory and Postgres, including rejected root/other-owner destinations, private-context write failure and replay.

## Reproduce

```sh
HUMAGEN_POSTGRES_TEST_URL=postgres://... go test -race ./...
pnpm install --frozen-lockfile
pnpm typecheck
pnpm test
pnpm validate:slack
make spec-lint client client-check
SCREENS_SHOWCASE=1 make screens
SCREENS_COLOR_SCHEME=dark \
  SCREENS_VIEWPORTS=320x740,768x1024,1440x900 \
  SCREENS_OUT=/tmp/humagen-dark-screens \
  pnpm --filter @humagen/screens screens
```

`SCREENS_SHOWCASE_ONLY=1` exports from an existing complete passing `.screens/report.json`. The primitive gallery has its own document flow, and the native run image includes the complete turn heading. Native file journeys use the real model adapter, sandbox, publication and PDF paths with a deterministic loopback model. They do not intercept successful artifact API responses. Older explicit presentation fixtures remain UI-state examples.

The responsive runs found and corrected the narrow Home gutter, a tablet menu positioned behind the sidebar, artifact action wrapping and squeezed model fields. Actual pointer checks and regression tests cover those corrections. Screenshot helpers now select exact project/message instances and compare revisions with the immutable source they actually revise.

## Live browser evidence

The persistent loopback preview is at `http://localhost:8081`. Its saved profile, conversations, reviewed publication, artifacts and personal skill draft survived native service restarts. Real Claude calls completed the basic turn, inactive-agent test/activation and native DOCX correction. Independent inspection verified actual Word table cells and PDF text while the original artifact checksum stayed unchanged. The reviewed outcome created a private tool-free skill draft with an exact Library link.

The final live skill check exposed a client request using the Company root for a personal draft. The correction derives the canonical personal destination at action time and uses server-approved agent availability. Backend refusal of the wrong destination remains enforced. The corrected canonical request completed real Claude run `rd595bc4475bfe862423280db90be21fe` with one model call and the expected validation marker. The component regression verifies the exact destination payload. Details are recorded in [the browser checkpoint](2026-10-workspace-local-browser-validation.md).

## Review and authorization

[Native stack #77](https://github.com/urmzd/humagen/pull/77) contains the ordered subsystem PRs. [PR #85](https://github.com/urmzd/humagen/pull/85) is the final console layer; its checks expose current remote status. No PR is merged by this implementation session.

GitHub App identity and the limited Issues/metadata permission configuration were read successfully. Sandbox installation and fresh full-stack webhook delivery still require successful credential/authentication verification. Existing real Slack/Claude delivery evidence predates this final host snapshot. Drive/Notion provider boundaries are tested against HTTP fixtures; live account authorization is not claimed. Credential values are absent from the source, evidence and screenshots.
