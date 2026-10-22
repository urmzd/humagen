# Changelog

## 0.16.0 (2026-10-22)

### Features

- **projects**: Slack App Home, replies under announcements, GitHub comments to the app, release notes, agent-name confinement (#46) ([a42bb5a](https://github.com/urmzd/warp/commit/a42bb5a172505fd51541fb58e1c93e8b6f584979))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.15.1...v0.16.0)


## 0.15.1 (2026-10-22)

### Bug Fixes

- **console**: names not sign-in ids, expert answers in the room, phone layout, refusals and settings (#45) ([60c8818](https://github.com/urmzd/warp/commit/60c8818c92562411720082d9a1f6577d54d8b3bc))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.15.0...v0.15.1)


## 0.15.0 (2026-10-22)

### Breaking

- **projects**: recaps, read confinement, Slack and GitHub coworking (#44) ([b102fe3](https://github.com/urmzd/warp/commit/b102fe3dc37130127615860c14ca311758879be7))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.14.0...v0.15.0)


## 0.14.0 (2026-10-22)

### Features

- **projects**: project roles, project inboxes and announcements (#43) ([f00fb8a](https://github.com/urmzd/warp/commit/f00fb8a8c5a91b258bea30787a8a4eeb7691ef1a))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.13.1...v0.14.0)


## 0.13.1 (2026-10-22)

### Bug Fixes

- **deps**: fiber v0.22.1 and anchor v0.5.1; drop the run queue's own connections (#42) ([04e6271](https://github.com/urmzd/warp/commit/04e6271d6cb43276abd43aed3648d57f149c1cb7))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.13.0...v0.13.1)


## 0.13.0 (2026-10-22)

### Features

- **projects**: channels and shared threads, tagging, experts (#41) ([7751b36](https://github.com/urmzd/warp/commit/7751b362070fd4acc046bccd879d72afec152987))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.12.1...v0.13.0)


## 0.12.1 (2026-10-22)

### Bug Fixes

- **auth**: a machine carries the deployment's policy for the people it maps (#40) ([527e553](https://github.com/urmzd/warp/commit/527e55383993fff207987cc566893cf30113222f))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.12.0...v0.12.1)


## 0.12.0 (2026-10-22)

### Breaking

- **projects**: explicit addressing, @auto, agents in projects, and the directory (#39) ([412b9c1](https://github.com/urmzd/warp/commit/412b9c18473805c9447521a6e315ec5ccab70187))

### Features

- **credentials**: each person's own credential for the tools that act as them (#38) ([371d10c](https://github.com/urmzd/warp/commit/371d10ca09c819ec4769ce4c21419aea9b3b9230))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.11.0...v0.12.0)


## 0.11.0 (2026-10-22)

### Breaking

- **projects**: the project tree, kinds, project policies and layered context (#37) ([2037501](https://github.com/urmzd/warp/commit/203750194cc832de005d9186e31a55dad1801c6b))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.10.0...v0.11.0)


## 0.10.0 (2026-10-21)

### Breaking

- one spec, warp start and warp deploy, durable stores, and the platform's public seam (#36) ([6b6fae9](https://github.com/urmzd/warp/commit/6b6fae9138e7453b773f5f7085ede6d9a4342fd7))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.9.0...v0.10.0)


## 0.9.0 (2026-10-21)

### Features

- inbox templates, Monitor and Context, built-in analyst and builder, phone-first console (#35) ([71c9898](https://github.com/urmzd/warp/commit/71c9898669c2ad778ab816ab734ed1d9adaf11b6))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.8.0...v0.9.0)


## 0.8.0 (2026-10-20)

### Features

- a console organized around work, with model policies and governed agent calls (#33) ([34d92b0](https://github.com/urmzd/warp/commit/34d92b0e1f521ffd606e163e86cbdc6b1484017c))

### Misc

- **deps**: fiber v0.20.0 (#34) ([1abd413](https://github.com/urmzd/warp/commit/1abd413e65c87dcd4399623bac0b832722f259d8))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.7.0...v0.8.0)


## 0.7.0 (2026-10-20)

### Features

- **slack**: bring people's thread messages into a mention's turn (#31) ([e065929](https://github.com/urmzd/warp/commit/e065929701924916e9ab5ff465bf302fd439310f))

### Bug Fixes

- S3 Stat from one read, built-in baseline switch, and DOM interaction tests (#32) ([bbc719d](https://github.com/urmzd/warp/commit/bbc719dced79de40b3e891ead2d9856b63a7f0e5))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.6.0...v0.7.0)


## 0.6.0 (2026-10-20)

### Features

- S3 blob store, artifacts unit tests and a locked built-in agent page (#28) ([20e4d18](https://github.com/urmzd/warp/commit/20e4d18a7b7376dd22369d4abae13bb5893787b7))
- **slack**: brand the app from a checked file and offer suggested prompts (#27) ([ccab5ad](https://github.com/urmzd/warp/commit/ccab5ad7e4e01369de42b74680e6ff81d96968a1))

### Bug Fixes

- **slack**: replace a dead Socket Mode connection and give up loudly (#30) ([f21c2bb](https://github.com/urmzd/warp/commit/f21c2bbc226900eee768cfc5d136e34d36614d2c))
- **slack**: close streams a previous process left open (#29) ([d9132c9](https://github.com/urmzd/warp/commit/d9132c93d93bd8561abe9f18bb884ae71d7e9a82))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.5.0...v0.6.0)


## 0.5.0 (2026-10-18)

### Breaking

- combine the platform and presentation as warp ([621d86c](https://github.com/urmzd/warp/commit/621d86c6bc45dbddcf422e2d806c9bd31f6fcff3))
- rename project from agent-ui to vultus (#3) ([8d263bd](https://github.com/urmzd/warp/commit/8d263bd5ce580dfa3c7d4dff91b5f833b0f22eae))

### Features

- add project navigation icons and accessible tooltips (#13) ([fa6f744](https://github.com/urmzd/warp/commit/fa6f7441e5b92cad6e2e61a7b2501cfc1c1d3ae1))
- Slack parity components, copy and a shared Slack host (#8) ([89ee84b](https://github.com/urmzd/warp/commit/89ee84bb4b9263838245a34b125ee2a62e938ecd))
- **home**: notifications with direct links, page title levels, and in-tab links (#9) ([7799303](https://github.com/urmzd/warp/commit/77993030cfda27faa00846681295a1bc63fa1ddf))
- **home**: a Home surface for the web and the Slack Home tab, and rename the Slack app to Collegium (#7) ([688a9fa](https://github.com/urmzd/warp/commit/688a9fabcea99e6341ee1f73458e6256f4f9af00))
- **go**: add the Go module with GitHub Markdown presentation (#6) ([8e3f506](https://github.com/urmzd/warp/commit/8e3f506b3410fc3d7b88cf54f8f973e38488d27f))
- **sandbox**: free-workspace manifest via SLACK_SANDBOX_SKIP, plus the Slack CLI hook (#3) ([76bda56](https://github.com/urmzd/warp/commit/76bda56da9750c87649231b4524a5e52338b1fdf))
- one run link above the disclaimer, and a comment prompt after a rating ([3dc700a](https://github.com/urmzd/warp/commit/3dc700a10f1827913699f000b136fcdc7d5550b9))
- mailbox with typed parts, agent items, a per-item slot, and Slack paging with source and disclaimer ([64b0049](https://github.com/urmzd/warp/commit/64b0049df319a8a7136caecf80a0ec3d4f487e74))
- **web**: orb, queue, status line, turn rail, artifacts, inbox, alerts, split buttons, and denser layout ([854d2c8](https://github.com/urmzd/warp/commit/854d2c85a25104c54dd01f07056f7777d2b889bb))
- inbox, system alerts, inline task decisions, and split-button menus ([b4d61c4](https://github.com/urmzd/warp/commit/b4d61c48f7a4bdb2b0764dd5e103baee5d62f92a))
- artifacts across core, Slack cards, and sandbox file upload ([6074998](https://github.com/urmzd/warp/commit/60749987a606518a563b9fdd955050092be83a06))
- queue, session state, batch approvals, stop phases, runtime notices, and Slack status mapping ([6c42a0c](https://github.com/urmzd/warp/commit/6c42a0c5bea9bbb2d20dc30c1d13095e2fd7934e))
- real Slack sandbox with manifest generator, and rebuilt web UI with web-only Storybook ([b496b6a](https://github.com/urmzd/warp/commit/b496b6ada8e405863fd8f6b6865068a8f8fe9a9a))
- agent-ui scaffold with shared components rendered to web and Slack Block Kit ([1858510](https://github.com/urmzd/warp/commit/18585103d844ee05d7f5979c02dde064c65d5a12))

### Bug Fixes

- keep orb effects within their layout footprint ([bd4b308](https://github.com/urmzd/warp/commit/bd4b3084cb7981e2d10caaa2bab0a673882b2fa4))
- **release**: align workspace manifests for the corrective patch (#11) ([80264f0](https://github.com/urmzd/warp/commit/80264f02a83b7fe5e628e8de0fd6e8ddee3f49f2))
- **sandbox**: reject inherited property names in SLACK_SANDBOX_SKIP (#4) ([9d358f3](https://github.com/urmzd/warp/commit/9d358f3daf100015b31e62db9e44f325ea16b1de))
- **sandbox**: skip fixtures with no blocks, thread ephemerals, and clear the waiting reaction ([4929be6](https://github.com/urmzd/warp/commit/4929be651bea09823044fb75d9ecfb60a46ee0ee))

### Misc

- pin Fiber and Saige migration releases ([572258b](https://github.com/urmzd/warp/commit/572258bcdda0bdd0cb5097ca6e16fcf310395553))
- publish versioned source releases after CI (#10) ([afcc776](https://github.com/urmzd/warp/commit/afcc7764478718efda5b1db7937e5e6e844b652a))
- **sandbox**: drop stale 1Password references from the env example (#2) ([a426378](https://github.com/urmzd/warp/commit/a42637885820c4341df8d8c45e7e30e7b700b173))
- keep generated slack manifest out of the formatter ([11f49ec](https://github.com/urmzd/warp/commit/11f49ec3f7e12108194446b0519e40daec8b266b))
- format slack manifest ([f879fea](https://github.com/urmzd/warp/commit/f879fead61e10e096efed4e07def5202082fa5a9))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.4.0...v0.5.0)


## 0.4.0 (2026-10-16)

### Features

- support seamless Okta and Tailscale console entry (#24) ([0908386](https://github.com/urmzd/collegium/commit/0908386f9bc333488b58a553e25d0ef288028cd2))

[Full Changelog](https://github.com/urmzd/collegium/compare/v0.3.0...v0.4.0)


## 0.3.0 (2026-10-16)

### Features

- add prompt-first Home and clear project navigation (#23) ([e12115e](https://github.com/urmzd/collegium/commit/e12115e25d215c7618f99f290143dd47be846093))

### Misc

- **deps**: upgrade saige to v0.37.0 (#22) ([907768c](https://github.com/urmzd/collegium/commit/907768c796ad86bea27083254a8ed4d4d15abb3f))

[Full Changelog](https://github.com/urmzd/collegium/compare/v0.2.1...v0.3.0)


## 0.2.1 (2026-10-16)

### Bug Fixes

- fit agent orbs naturally in compact console rows (#21) ([7c2adbb](https://github.com/urmzd/collegium/commit/7c2adbbb5f1a629dfc081256f797d3f07a6924dd))

[Full Changelog](https://github.com/urmzd/collegium/compare/v0.2.0...v0.2.1)


## 0.2.0 (2026-10-16)

### Features

- organize tasks and add structured helper calls (#19) ([748f0d0](https://github.com/urmzd/collegium/commit/748f0d001c5d98377a6dc345eb09804931b0e088))

### Bug Fixes

- preserve answer attribution across resumed events (#20) ([9648bc1](https://github.com/urmzd/collegium/commit/9648bc16e1e4e57452e3a18544fe992021910a1e))

[Full Changelog](https://github.com/urmzd/collegium/compare/v0.1.0...v0.2.0)


## 0.1.0 (2026-10-15)

### Breaking

- stop a run through the runtime, with tenant-scoped run ids (#10) ([8e73e34](https://github.com/urmzd/collegium/commit/8e73e341da96b55573f19dbc6bf022f45892f8f0))
- rename open-agent-platform to collegium (#3) ([c0a7a4a](https://github.com/urmzd/collegium/commit/c0a7a4a9500833765b62b9b884a217e5427d3f66))

### Features

- park runs on people and answer their questions in the inbox, on the run and in Slack (#17) ([fcfcc5c](https://github.com/urmzd/collegium/commit/fcfcc5c5423f77ba72340c30ba6edd280a433514))
- record usage per run and enforce spend limits (#16) ([1712eea](https://github.com/urmzd/collegium/commit/1712eeac75fa4f92551a3e5a7af18f8ca62692de))
- **web**: Home notifications from alerts and the inbox, each linking to its own page (#14) ([7bf8cf7](https://github.com/urmzd/collegium/commit/7bf8cf764e7f3737491930c9e26b461429c7488f))
- **slack**: Socket Mode Slack app on a shared collegium-to-vultus adapter (#9) ([63592be](https://github.com/urmzd/collegium/commit/63592bef9e807c46a6116ffbd3ee3a1210da8728))
- **web**: a console home on vultus Home, a grouped menu, and an access explanation (#13) ([42d7d83](https://github.com/urmzd/collegium/commit/42d7d8301eb0ad46376474b5792c8b1b82a76af6))
- **auth**: an authorization policy language and a mock OIDC provider for development (#11) ([2dc7107](https://github.com/urmzd/collegium/commit/2dc7107f69988bf9dec550702110a18140ef5426))
- OpenAPI contract, drift tests and a generated TypeScript client (#8) ([26da25e](https://github.com/urmzd/collegium/commit/26da25eb77679a5161f11f7334be1a3e184dffb1))
- GitHub and Slack integrations with clients per installation (#7) ([dd795b4](https://github.com/urmzd/collegium/commit/dd795b474dfb93dba5db1f545233700b969bc87b))
- **web**: a limits panel in the session gauge's gear, on fixtures ([206a258](https://github.com/urmzd/collegium/commit/206a25827d3bddc218c9487e94464b3752efa1cb))
- called agents as runs of their own, attached or delegated, with stop ([73d1289](https://github.com/urmzd/collegium/commit/73d1289bd10757f4eb0b62f5443afd74a88240f9))
- inbox, artifacts and storage, activity drawer, copy as, session gauge, limits pages, and motion ([2fbba13](https://github.com/urmzd/collegium/commit/2fbba13b66b5bc0bdeaf9d603afa2c999a05906d))
- onboarding, projects, usage limits, paged recents, agent strokes and console screenshots ([41474de](https://github.com/urmzd/collegium/commit/41474de7d29baf622ffdce115390b4b86a36e29c))
- memory approval, the orb as each agent's mark, menus, and a flatter console ([c624178](https://github.com/urmzd/collegium/commit/c624178fdb93f91018b5b96bdbd3c6d38c61f901))
- a composer with attach and dictation, agent memory, conversation titles, and knowledge variants ([d218f44](https://github.com/urmzd/collegium/commit/d218f4430aa429067d89722d2f9da5c12abaad1d))
- **web**: drop the top bar, color-code agents and recents, and flatten the composer dock ([af62e2f](https://github.com/urmzd/collegium/commit/af62e2f0f8e2e889e9c53bf1449eaac59ee53683))
- conversations with branches, agent editor, evals, preferences and a glass console ([cab1d91](https://github.com/urmzd/collegium/commit/cab1d914ac515b18bd4a9ffc7f036c90df301d1e))
- server, sign-in, console, schedules, analytics, alerts and knowledge ([00c5cf4](https://github.com/urmzd/collegium/commit/00c5cf425a1894a22762428cf0fcab89e61ec758))

### Bug Fixes

- **slack**: never repeat a broadcast mention or link as the app (#15) ([62a9e82](https://github.com/urmzd/collegium/commit/62a9e82a2f76591bd744e27778429a0188150e69))
- **client**: close streams, resume dropped ones, verify oag, tighten the contract tests (#12) ([351a4aa](https://github.com/urmzd/collegium/commit/351a4aa7e63ec148030421bd04790b6d5322e210))

### Misc

- release from main after the full CI suite (#18) ([dca9cb4](https://github.com/urmzd/collegium/commit/dca9cb4f4bfae7b6df1baca05d136963e1d13214))
- pin opentag v0.2.0 and saige v0.34.0, and drop the local replace ([b946480](https://github.com/urmzd/collegium/commit/b9464807cfe2f551bea6cbe6110a2fc99cef7261))
- initial project scaffold with the call primitive ([cc84e42](https://github.com/urmzd/collegium/commit/cc84e42251dd341500680aa505314b1a44aaa020))
