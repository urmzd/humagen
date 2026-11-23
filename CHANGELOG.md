# Changelog

## 0.195.3 (2026-11-23)

### Bug Fixes

- **ci**: push the release image when CI was skipped for a tested tree ([cd922b0](https://github.com/urmzd/humagen/commit/cd922b0c071cb8a9cf910dca202d4bfc0b66ff93))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.195.2...v0.195.3)


## 0.195.2 (2026-11-23)

### Bug Fixes

- **core**: compare a call's identifying arguments, keeping paths case-sensitive ([28a5e51](https://github.com/urmzd/humagen/commit/28a5e51fa8000719f6d84f3d12ca4af809d81ff3))
- **console**: recover a failure only by the same tool at the same target; read address as the transport does ([9d6c324](https://github.com/urmzd/humagen/commit/9d6c324a99f0c434b46ecda77f183e60b79bf7be))
- **console**: say a recovered tool failure as retried; read repositories with github_read_context ([af8399d](https://github.com/urmzd/humagen/commit/af8399d766e8a537b326bbd418791ff86f142456))

### Misc

- **copy**: regenerate the product copy keys after merging main ([bb07b2e](https://github.com/urmzd/humagen/commit/bb07b2e057c6d33f8c960f3fa6950b18a85d6935))
- **deps**: saige v0.50.0, repository reads and untrusted-data notice ([571deb4](https://github.com/urmzd/humagen/commit/571deb4c4c9dc8f84fc52b854d4b4769d464c58a))
- **deps**: tidy go.sum after the saige pin ([6c8b92d](https://github.com/urmzd/humagen/commit/6c8b92d375131887356787fc66db4f136d1a25ef))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.195.1...v0.195.2)


## 0.195.1 (2026-11-23)

### Bug Fixes

- **slack**: stop fails closed, the sender's team on every check, requested_by on runs ([b29a59e](https://github.com/urmzd/humagen/commit/b29a59e697779578a5d5b0e1b2d554048d361579))
- **slack**: start runs only for people the client maps ([9696d57](https://github.com/urmzd/humagen/commit/9696d57d6dc2265407306fbaa9c7e7023a7f2ddc))

### Misc

- **slack**: the server test signs the app in as its own machine, mapping a person ([155b905](https://github.com/urmzd/humagen/commit/155b9055c1d99f4ee30b7bce0113a61fdc56fdea))
- **copy**: regenerate copy keys for notConnected ([4a0bc37](https://github.com/urmzd/humagen/commit/4a0bc3783f5da216a6ad60a83d04927f4c27ae39))
- **platform**: a Slack app's credential is held to asking for a person ([34c7ea0](https://github.com/urmzd/humagen/commit/34c7ea02e168e205c9e358266e301de5f388eb8e))
- **slack**: deny by default on every entry; api: asked_by on runs ([a7e2ee7](https://github.com/urmzd/humagen/commit/a7e2ee7c918a7abc755a1d119b92b297724052ae))
- **slack**: every Slack test asks as a person the client maps ([3e1a158](https://github.com/urmzd/humagen/commit/3e1a158a0567d0b136a55d367d41c8db524bec2e))
- **slack**: move app tests to people the client maps (wip) ([ca6bd67](https://github.com/urmzd/humagen/commit/ca6bd67ad3a0345343eb9462870573348b638510))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.195.0...v0.195.1)


## 0.195.0 (2026-11-23)

### Features

- **web**: show session alerts in the transcript as one line each ([5ad5920](https://github.com/urmzd/humagen/commit/5ad5920218616667a9312c5ae0e435bf471b059c))

### Bug Fixes

- **web**: keep a failed answer's alert reachable and reopenable ([1ad867c](https://github.com/urmzd/humagen/commit/1ad867cb8a6dd289894d6b24a851572b3684ce1e))
- **web**: align settled alert states and keep the repeat count on a phone ([5f1797d](https://github.com/urmzd/humagen/commit/5f1797d35d589b5b8073e51d95e2edfa0d31e996))
- **web**: fold a failed answer's alert into its card and steady alert lines ([4b8e24c](https://github.com/urmzd/humagen/commit/4b8e24c2371f2c7a3ca2e571268095b737a5209a))
- **web**: keep alert lines readable on a phone and Undo beside the composer ([692a429](https://github.com/urmzd/humagen/commit/692a4292ca7a4efbf23f18f4f192eda188a25e42))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.194.0...v0.195.0)


## 0.194.0 (2026-11-23)

### Features

- **github**: read the place an answer was asked on, and post only the answer ([d30ae93](https://github.com/urmzd/humagen/commit/d30ae93d3f0682cea991498019ee66e54ca77dcb))

### Bug Fixes

- **github**: treat comments deleted mid-read as a change, and count reads in the e2e test ([038f65e](https://github.com/urmzd/humagen/commit/038f65e266e168b9b1f7a3bbe6d44a58b62ebd90))
- **github**: read the latest comments through the last page, not every page ([2a0fda1](https://github.com/urmzd/humagen/commit/2a0fda14795beb4cbe9f2d73cc140cf2fcf6c00f))
- **github**: read the latest comments, skip the client's own login, and read through a client only on its own place ([d9bce74](https://github.com/urmzd/humagen/commit/d9bce744dec5b996ab9da63a32b0e4be83945565))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.193.0...v0.194.0)


## 0.193.0 (2026-11-23)

### Breaking

- **github**: only the humagen trigger asks; agents are named after it ([4e51145](https://github.com/urmzd/humagen/commit/4e5114515147df57bc18ed627fb5a45a19498e15))

### Features

- **github**: humagen is the one trigger on GitHub and Slack ([dc4b24d](https://github.com/urmzd/humagen/commit/dc4b24d561254634d0f3eb2e833e621273adc871))

### Bug Fixes

- **trigger**: whole tag names, and list paragraphs are prose ([421f0aa](https://github.com/urmzd/humagen/commit/421f0aa4048b15b1ac011bc3b6e93af356decb56))

### Misc

- **github**: declare the trigger's answer without an unused value ([1292e3c](https://github.com/urmzd/humagen/commit/1292e3c87f19128bef9d271268fbfbf78d6add4d))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.192.1...v0.193.0)


## 0.192.1 (2026-11-23)

### Bug Fixes

- **migrate**: build the environment's clients as serve does before the backfill ([96d4e5e](https://github.com/urmzd/humagen/commit/96d4e5efc10c820657a531b61f5c61bc76f8b334))

### Misc

- fail the image job when it cannot tell whether a tag exists ([f3efca8](https://github.com/urmzd/humagen/commit/f3efca890bfb1bafe5dee285ed8804c119969f8f))
- never overwrite either release image tag, report Test failed on a cancelled run ([0699a06](https://github.com/urmzd/humagen/commit/0699a0626268d1a1671e841378f3609b69202383))
- shard the Go tests, skip a second CI run at release, push the release image ([f6eb74b](https://github.com/urmzd/humagen/commit/f6eb74b36b44b5661fc916dc2139159f00b1f693))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.192.0...v0.192.1)


## 0.192.0 (2026-11-23)

### Features

- **migrate**: file past GitHub runs into their sessions ([0bc1fd0](https://github.com/urmzd/humagen/commit/0bc1fd0d4aacd558577abe9c2ab02733d61d3ec3))

### Bug Fixes

- **migrate**: print usage and name the unknown migration in the error (lint ST1005) ([4cba5d3](https://github.com/urmzd/humagen/commit/4cba5d3d9f0909045d62d9e9e79cd7bfa6a5f21a))
- **sessionstate**: read again when a run is bound between BindRunsInOrder's reads ([a18d0f1](https://github.com/urmzd/humagen/commit/a18d0f10f43e6400981fff829e64bc58d077077e))
- **migrate**: refuse an empty store, warn of unmapped clients, leave a second client's place, bind in time order ([e027198](https://github.com/urmzd/humagen/commit/e0271981641ba5f1621d369c3bcb5476a8f97b40))
- **migrate**: show only the platform's errors, and log the dry run's lines in the test ([8a1ead4](https://github.com/urmzd/humagen/commit/8a1ead4030c03f6f7395a7bfd8a9c15218e8c863))

### Misc

- **migrate**: cover the sessions backfill, Refile and the command; document it ([d6a101e](https://github.com/urmzd/humagen/commit/d6a101e30733c95d63ff98533817d71fec7e307b))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.191.0...v0.192.0)


## 0.191.0 (2026-11-22)

### Breaking

- **github**: automatic review is off unless switched on ([a4e83b3](https://github.com/urmzd/humagen/commit/a4e83b365d1c0ef78896bb27b45337f4b5aa2272))

### Features

- **console**: a Repositories view for a GitHub app, and each repository's settings ([7507e03](https://github.com/urmzd/humagen/commit/7507e03d4c3ef99859ffdb3aea37ab76da1801dc))
- **github**: repository settings drive review, agent, instructions, reads and cross-surface reads ([7fd9f3a](https://github.com/urmzd/humagen/commit/7fd9f3a83d9861a90609891068e48918ffe39542))
- **github**: per-repository settings for GitHub clients (wip) ([6ea2619](https://github.com/urmzd/humagen/commit/6ea26197910f98e6b3ecc7d8c85e013ddf775a71))

### Bug Fixes

- **github**: quote repository settings descriptions in the API spec; lint ([87d553c](https://github.com/urmzd/humagen/commit/87d553c98313b4a5e67d868eeb56f684d48083d5))
- **github**: show the workspace's cross-surface answer and who keeps it on every client; close a fence the read budget cuts ([ca89994](https://github.com/urmzd/humagen/commit/ca899945647616d4a916747206722f3a3bd4e645))
- **console**: name the GitHub app in the path, one left edge for both pages, round row ends on a phone, even reads card ([1454f4f](https://github.com/urmzd/humagen/commit/1454f4f8ed708a9569a78d027fe8ecfe5a2041dc))
- **github**: keep a repository to its own runs across every client; fence pre-run reads in a nonce block within 3 seconds; refuse an empty write over unread settings; cover only installed, named or kept repositories ([ae49fab](https://github.com/urmzd/humagen/commit/ae49fab7acdd55ec6c66fc39a5e5f91d77701192))
- **console**: repository list fits a phone; empty state spacing ([fa1548c](https://github.com/urmzd/humagen/commit/fa1548c8495055f85be7816609a67df10ee43608))

### Misc

- **copy**: regenerate copy keys for the repositories view ([d87af1b](https://github.com/urmzd/humagen/commit/d87af1bcfd957e2c3cebdda4b302de511410c3da))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.190.0...v0.191.0)


## 0.190.0 (2026-11-22)

### Features

- **github**: map GitHub logins to people from HUMAGEN_GITHUB_PEOPLE ([f36601b](https://github.com/urmzd/humagen/commit/f36601bbe17b7eb4de513ca8182050e523aafe8a))
- **github**: file each pull request and issue as one session ([27e3ef2](https://github.com/urmzd/humagen/commit/27e3ef2207ad5eb1d1ef9ba0f571ab005dc29215))

### Bug Fixes

- **github**: judge GitHub sessions by the root run, keep memory as before, and keep clients apart ([50dc0e8](https://github.com/urmzd/humagen/commit/50dc0e80abe1c01ebf1fc4ec511ce9c66493cce2))
- **console**: keep the GitHub link out of trace.ts and give Home's budget room for it ([ee3a7ae](https://github.com/urmzd/humagen/commit/ee3a7ae0e7878f1482bc5aab3003fc20d4977961))

### Misc

- **github**: grant the mapped askers runs:write and ask on an issue through the connector ([147d372](https://github.com/urmzd/humagen/commit/147d372c07cf441209426e5a78dc222d90d559b5))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.189.0...v0.190.0)


## 0.189.0 (2026-11-22)

### Features

- **github**: offer and read the repositories the app installation covers ([ca422e0](https://github.com/urmzd/humagen/commit/ca422e0a77df533dbf50f51aee0c539ebc649d62))

### Bug Fixes

- **github**: share one installation read outside the lock, keep each caller's deadline ([56a639a](https://github.com/urmzd/humagen/commit/56a639a25d5caa8fcc62db0a3d8807b257f7d685))

### Misc

- **github**: installed repositories, the picker and reads; docs and contract ([ed0031c](https://github.com/urmzd/humagen/commit/ed0031c9cb8f681e4cc0d129cbbcb04a1938dd28))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.188.1...v0.189.0)


## 0.188.1 (2026-11-22)

### Bug Fixes

- **platform**: read corpora for a Slack session tree's audience in every session of it ([0e67e4e](https://github.com/urmzd/humagen/commit/0e67e4e9481664f6a662a9924597e475ce458718))
- **slack**: check delivery before posting a new inbox item about a run ([5fb6937](https://github.com/urmzd/humagen/commit/5fb6937282387922f7eaec04d8095cd159ca349e))
- **platform**: keep a Slack session tree out of personal tools and deliver only to its own thread ([ca178e9](https://github.com/urmzd/humagen/commit/ca178e968d659d0d4f5c89a8e3c824ad20f40621))
- **slack**: deliver a Slack thread's own private session back to its conversation ([3bb0562](https://github.com/urmzd/humagen/commit/3bb05629c6b0731fee12615e038edfcaad48ce43))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.188.0...v0.188.1)


## 0.188.0 (2026-11-22)

### Features

- **artifacts**: match list queries on text, say where a published file opens ([aea5451](https://github.com/urmzd/humagen/commit/aea5451d37f6dfe04d2df79a3db01f8b819e6b14))
- **artifacts**: artifact_read, artifact_list and publish targets for agents ([c36c15d](https://github.com/urmzd/humagen/commit/c36c15d6681ab9c6a9531e1821862dcd52be5faa))
- **deploy**: the chart and compose run the browser image ([8415670](https://github.com/urmzd/humagen/commit/84156707a7c4e551ad0344e2ea390680b807174a))
- **deploy**: the browser image is one browser for every Humagen process ([ad69268](https://github.com/urmzd/humagen/commit/ad69268b2f9817ae1731af9a7062a8cfe76b635b))
- **deploy**: a browser image, Chromium with the pinned Playwright MCP server ([a15cd53](https://github.com/urmzd/humagen/commit/a15cd533d64a137fb8f9497789f80568029e0d0f))

### Bug Fixes

- **deploy**: name the browser's reach under Compose and let the chart sandbox its pod ([d6cd138](https://github.com/urmzd/humagen/commit/d6cd1382b3c39d5eedfc34aba80e6aa5304863b4))
- **deploy**: the browser overlay, a keep-open client that checks, and bounds ([abb8de1](https://github.com/urmzd/humagen/commit/abb8de1a2ae33703facdf3dcdb0901dcc055692e))
- **ci**: restore the five jobs an edit cut from the workflow ([6eeb47b](https://github.com/urmzd/humagen/commit/6eeb47b800c498727d0625fa1251387bd7266466))
- **deploy**: the bundled browser holds to what it says, from a review ([f94aa5f](https://github.com/urmzd/humagen/commit/f94aa5f75760beba70ff7ffb7b3c8aa2dbc66c10))
- **platform**: a tool server that starts late is reached without a restart ([872f033](https://github.com/urmzd/humagen/commit/872f0336f3d406b777a7cd15b98f12b4cd00cd0f))

### Misc

- **browser**: print the browser's logs when it is not up to refuse a request ([7308cab](https://github.com/urmzd/humagen/commit/7308cab11ddeebbc30d4a86e942ad050f7511171))
- **artifacts**: a run for an administrator outside the project reads by their own roles ([868bda8](https://github.com/urmzd/humagen/commit/868bda875b4aeb50dbd4b7d8f17da57078589447))
- **console**: the Recent order test gives each row its own time ([f54cbd8](https://github.com/urmzd/humagen/commit/f54cbd8bad67bb2733a624f04c2b26b9279c3357))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.187.0...v0.188.0)


## 0.186.0 (2026-11-22)

### Features

- **sessions**: generate short session titles, keep a person's rename ([55e6260](https://github.com/urmzd/humagen/commit/55e6260e137c4750f7d689254d44f872d9b75220))
- **web**: open an alert's document, and keep the alert out of its review ([c1993dd](https://github.com/urmzd/humagen/commit/c1993dddf5e5bd70e8bd4939bc5842487955f2a7))
- **alerts**: start a review with the alert as an attached document ([dcf9e40](https://github.com/urmzd/humagen/commit/dcf9e400a0b51a5865fe3aa1f9225d3a3de82d26))
- **web**: one settings menu beside + on Home, and approval as a pill ([24f2e18](https://github.com/urmzd/humagen/commit/24f2e18d0efce07414f1c35866609f09276a4d37))
- run a ! message as a command in the session sandbox ([0d783c0](https://github.com/urmzd/humagen/commit/0d783c0f41227df63487033f2a32751666172ab6))
- **web**: lean alert card with View, Review and Done ([ea6d287](https://github.com/urmzd/humagen/commit/ea6d28711917da1661db79d0f2ab55ec5473eee6))
- **alerts**: put a health alert on the answer that failed and add Review ([c91b621](https://github.com/urmzd/humagen/commit/c91b621a8ccdff0e42666e2cf2edcb9b9931519a))
- **web**: mascots spin in 3D, the body drawn at each angle of the turn ([1b6de4e](https://github.com/urmzd/humagen/commit/1b6de4eb836fccd72d87d27fbd3531516281cdc2))
- **web**: mascot bodies and arms in bot-avatars' 3D plush ([6e3b42d](https://github.com/urmzd/humagen/commit/6e3b42d79ed0eca0e224f5d22695b70392c6f1e6))
- **web**: say the approval mode as a word under the composer ([404acad](https://github.com/urmzd/humagen/commit/404acadff196002376861e13e3a2be1fb778022a))
- **web**: idle mascots hop, spin, wave and wiggle now and then, and when pointed at ([17566cd](https://github.com/urmzd/humagen/commit/17566cd9fc48d6c8478ec214d45a30f8b8437b67))
- **web**: agent marks draw Humagen's own mascots ([1bbb61a](https://github.com/urmzd/humagen/commit/1bbb61aeffe1a81d5f22a4bff3b4c97fff452ce0))
- **web**: mascot tools drawn as rendered objects with livelier motion ([3568778](https://github.com/urmzd/humagen/commit/35687782c0c23e45bf976302d8801eccc82a388f))
- **web**: draw mascot fur once for smooth motion; Humagen as a tuxedoed secret agent ([12d48b5](https://github.com/urmzd/humagen/commit/12d48b50ac238828e5530db6feed1bcf64222bdb))
- **web**: Humagen's own mascots: personas with arms that use their props (prototype) ([6a72a5a](https://github.com/urmzd/humagen/commit/6a72a5aafdbf9562a5dc08429cfadbdae87b8255))
- **web**: mascots hold their props with their own furry hands ([63c85bd](https://github.com/urmzd/humagen/commit/63c85bd6668118892d4d03b7f9b11c97072f3cd4))
- **web**: draw mascot props as real objects, with a gallery of every state ([0f568bb](https://github.com/urmzd/humagen/commit/0f568bb4430eb3f8304e5e81f636ef7f0741f43c))
- **web**: marks finish their move on open eyes, and mascots carry props, not orbs ([3306761](https://github.com/urmzd/humagen/commit/33067619bed5ce3e5ac6a77aa2d3225d6763ba08))
- **web**: idle glances and activity props on agent marks ([d3f2f82](https://github.com/urmzd/humagen/commit/d3f2f82a0fe38d2eaf37c9b16dac3f804c2837a2))

### Bug Fixes

- **platform**: command error strings end without punctuation ([83c5819](https://github.com/urmzd/humagen/commit/83c58193a364b0cca2d25f88abfda4c50d834b73))
- clear a stale hold, key the upper arm by theme, retry a picture whose job threw ([1f4f994](https://github.com/urmzd/humagen/commit/1f4f99498fed2832364b237736909a4bac1933b4))
- cross-review findings: held queue stays held, proper names, in-app links, Workspace breadcrumb, search history, mascot job queue and caches ([26ec662](https://github.com/urmzd/humagen/commit/26ec66200e5be49e1d1780b41cebf79e916c74ac))
- **web**: a ! command never carries files, and ! is plain text outside a session ([71b8374](https://github.com/urmzd/humagen/commit/71b8374b8256c41983ef5337b4e8f181f29bada3))
- **platform**: never title a session from a command; start one review per alert ([63c6f34](https://github.com/urmzd/humagen/commit/63c6f3495c2c60ab59e9867d6175d7b61525605b))
- **console**: a sidebar row's ⋯ is 28px on a mouse, so its status dot never meets it ([135d12e](https://github.com/urmzd/humagen/commit/135d12e815cb817dc7c86eee12d3bde79e7c784b))
- **screens**: composer screens follow the settings menu and the approval pill ([b58e25b](https://github.com/urmzd/humagen/commit/b58e25bfc904b6395171709b2419bbfb197b0c7b))
- **alerts**: quote tool errors as code and give the document a Close ([5be7948](https://github.com/urmzd/humagen/commit/5be79486cf596eb21c567dcc3ac6a786f07ddb79))
- **web**: mark Auto's risk on its icon, not a filled row ([8b83567](https://github.com/urmzd/humagen/commit/8b8356713094d6aed1dd5854c66c5c61a1e3fe9d))
- **web**: center a sidebar row's status dot under its menu button ([54a2475](https://github.com/urmzd/humagen/commit/54a2475dfd6fd0087e59a41b2c2c8883b5900381))
- **alerts**: link a recap to the answer by its console path ([c1d2bf5](https://github.com/urmzd/humagen/commit/c1d2bf552a8951970b2ec310c94767aa2567cb1e))
- **web**: center an agent mark's mascot on its body so it lines up with the name ([a9cfe5a](https://github.com/urmzd/humagen/commit/a9cfe5af740b3598c6aed3b5aee21f529cb6fa20))
- **web**: mascot arms grow from the body as one shaded, furred piece ([71fe44b](https://github.com/urmzd/humagen/commit/71fe44b948d3afa4beee78a60b0c3da43bd00a75))
- **web**: center Home's greeting and prompt on desktop ([ce59202](https://github.com/urmzd/humagen/commit/ce59202b0ddbdc17238e08a673e89a076ef24616))
- **web**: show a failed answer as a quiet line with a small Retry ([85b94f2](https://github.com/urmzd/humagen/commit/85b94f2b1b49f3357dfc5feaf1e08d400349a1cd))

### Performance

- **web**: draw mascots in idle-time jobs, on first view, as blob URLs; turns on first need ([8eb9dbf](https://github.com/urmzd/humagen/commit/8eb9dbfd62d2a52ceca87aea91a818c773a1c3aa))

### Misc

- **console**: the retry button names its agent properly ([81f70d5](https://github.com/urmzd/humagen/commit/81f70d5abda6187df50d44e9f309dbd6a70fc2ec))
- **spec**: regenerate product copy keys ([f841bcb](https://github.com/urmzd/humagen/commit/f841bcb08da16c6c7658017f08c16f3426670bd6))
- **screens**: Home's composer scenarios use the settings menu and typed skills ([37c658f](https://github.com/urmzd/humagen/commit/37c658fe58bbaf58509f2ef48598daeb5e65dc5f))
- **spec**: regenerate product copy keys after the merges ([5fc3d88](https://github.com/urmzd/humagen/commit/5fc3d88c6cbb0f23359205dba30ed506427ca2dd))
- **spec**: regenerate product copy keys ([2bb98b7](https://github.com/urmzd/humagen/commit/2bb98b77c57283f65aabe922d56356c231e2a549))
- **web**: check Auto's warning on its icon, with no filled row ([e3b9351](https://github.com/urmzd/humagen/commit/e3b93518e1580002fb789771fad6f8e4d7fdefb4))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.185.0...v0.186.0)


## 0.185.0 (2026-11-22)

### Features

- **console**: tests read Sessions and Projects as tables; the pin mark and alert label are announced apart ([0020c46](https://github.com/urmzd/humagen/commit/0020c46cb2ad30a76b42dcd5bc8422b3ede7402a))
- **console**: Sessions as a table: aligned columns, soft rows with space between them ([07acffb](https://github.com/urmzd/humagen/commit/07acffbeec5fdff224d71816c7f902c151bcdfa6))
- **console**: sidebar status as one dot, and no count beside Recent ([9e7b0e9](https://github.com/urmzd/humagen/commit/9e7b0e959c129616942c98310892aa860aedd2a5))
- **console**: proper agent names in Details; document the People search ([114b1be](https://github.com/urmzd/humagen/commit/114b1be84c2f815855cd97cc1da423e756ce5a0f))
- **console**: search and add people in the People drawer ([4bc9e11](https://github.com/urmzd/humagen/commit/4bc9e11b168a0864791bb783a1c8c7ad356e74ad))
- **console**: drop Progress from session Details ([42ae79b](https://github.com/urmzd/humagen/commit/42ae79bb8999b6c38aa7a30cb393d1185075beca))
- **console**: a knowledge base is made with its sources, in one form ([5067702](https://github.com/urmzd/humagen/commit/5067702bc7c77365cef0c6d51e0a7592350b9c47))
- **console**: focused drawers: no Search, Steps, Sources or Sessions tabs, and People invites people only ([0c3befd](https://github.com/urmzd/humagen/commit/0c3befde60a2fccbb688bab5339fa908ac7419f3))
- **console**: a connection row has one control: the verb to connect it, or Connected with a menu ([3ed7249](https://github.com/urmzd/humagen/commit/3ed72498e7b9fcf15d5f85e75548bd967a51d18e))
- **console**: the Store explains itself and lists packages as rows with a split button ([beb18fe](https://github.com/urmzd/humagen/commit/beb18fe963126ff55bfad964a2159100dd681150))
- **console**: shadcn Item, Card, Field, ButtonGroup, Breadcrumb and Table; one title line and a side list for an item's sections ([cf49c78](https://github.com/urmzd/humagen/commit/cf49c78fd7df989cf464b6c3dd6d67097097d36f))
- **console**: no Reply on a message, and a person's message time sits at the left end ([4b29d9b](https://github.com/urmzd/humagen/commit/4b29d9bb0fd850b6a8e719bfd576c35b8fb9d8ca))
- **console**: the composer's footer names no agent and no model ([91a03d7](https://github.com/urmzd/humagen/commit/91a03d797b3a258dee51697e989bb05921251401))
- **console**: a message's actions are always shown, and only its time shows on hover ([ac0eb60](https://github.com/urmzd/humagen/commit/ac0eb609bba121c5f1be9536138eb1528b797487))
- **console**: an agent's mascot moves while the pointer is over it ([7ee9fa1](https://github.com/urmzd/humagen/commit/7ee9fa1b2e551af4d9653ea5665a0bdb1bc7405b))
- **console**: a message's actions show in full or not at all, and are smaller ([66459f9](https://github.com/urmzd/humagen/commit/66459f90af919064f5baf287cf38eb761fefbb71))
- **console**: remove the Side Chat ([e454023](https://github.com/urmzd/humagen/commit/e4540234a96b1c34e3e45a3a5cdab918b3946e13))
- **console**: Automations, and background work in its session ([1d406af](https://github.com/urmzd/humagen/commit/1d406af3956d511473e5fdebbcde155645f1eb61))
- **console**: clearer icons in the composer's + menu ([52e177c](https://github.com/urmzd/humagen/commit/52e177c9b572dfae4565060eb20ea47e127248a9))
- **console**: the Usage popover is one grid of names and values ([dc1fc8c](https://github.com/urmzd/humagen/commit/dc1fc8cb09512f8580dc9ea48516d72a49ac7a53))
- **console**: Home lists nothing under the chat bar ([7c4bd1a](https://github.com/urmzd/humagen/commit/7c4bd1acb11dd50e0d1c51f965160fa0c3851169))
- **console**: a flat composer footer, and waiting messages in the conversation ([e94f1a4](https://github.com/urmzd/humagen/commit/e94f1a4f8125560125666c750fa6e8f5224bed19))

### Bug Fixes

- **console**: screens follow the redesigned drawers, Apps and status dot; table and title links meet touch size ([151d42b](https://github.com/urmzd/humagen/commit/151d42b55f8e1672b56210e8fc314df4669d9e01))
- **console**: the Projects table's Updated column says only when ([29093ed](https://github.com/urmzd/humagen/commit/29093edad13cf1d84bd25ceceb0b5b50fb2ebfb6))
- **console**: align People search results with the list ([1b326d8](https://github.com/urmzd/humagen/commit/1b326d833fdab673a8f96c55b2500c1ca39a252c))
- **console**: a working answer says what it is doing before its first word ([b4e256c](https://github.com/urmzd/humagen/commit/b4e256c6077b01a5d8828f2ad2367e3663ee8165))
- **console**: "Connect GitHub", and short words in capitals in agents' names (QA Reviewer) ([ce6c4b4](https://github.com/urmzd/humagen/commit/ce6c4b4b7f0a180640fce9c09ce9115cee82efe5))
- **console**: restore what an unsafe lint fix changed in the session center ([bac9616](https://github.com/urmzd/humagen/commit/bac96166a3ae89672822216ac6e1a3a8271822ec))
- **console**: the latest message keeps its action row when something follows the turns ([05b9804](https://github.com/urmzd/humagen/commit/05b98045e99ad65b16716ba87befc4dd85e0905b))

### Misc

- **screens**: Home has 27 session states after dropping its project and made strips ([ff51695](https://github.com/urmzd/humagen/commit/ff516951adca77799de198501b75bb0af17fb50e))
- **console**: format a test ([1db1288](https://github.com/urmzd/humagen/commit/1db1288967f0e7c0feb61ae8735dd2fb82303811))
- **console**: align the usage popover's agent row and drop an unused import ([7b6d7a2](https://github.com/urmzd/humagen/commit/7b6d7a2be0c63f6d244bde0514007d2a713a0725))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.184.0...v0.185.0)


## 0.183.0 (2026-11-21)

### Features

- **console**: the composer says the product is AI, with a Terms link from branding (#511) ([e9e424a](https://github.com/urmzd/humagen/commit/e9e424ae1ad7b4f9ca8b67d109b9afb9b51e1d16))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.182.0...v0.183.0)


## 0.182.0 (2026-11-21)

### Features

- **site**: a terms page at withhumagen.com/terms (#512) ([f741b57](https://github.com/urmzd/humagen/commit/f741b5738f429506b19e1f80670770b80f6e70d0))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.181.0...v0.182.0)


## 0.181.0 (2026-11-21)

### Features

- **console**: the session panel says where a session is attached (#501) ([f805cfa](https://github.com/urmzd/humagen/commit/f805cfa038e0bf36aaa2de8289a6ec866afed160))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.180.0...v0.181.0)


## 0.179.1 (2026-11-21)

### Bug Fixes

- **console**: settings titles alone, one amber for waiting, Audit filters folded on phones (#509) ([0e706c8](https://github.com/urmzd/humagen/commit/0e706c84ec10f00304de13e05319b051bda06a90))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.179.0...v0.179.1)


## 0.179.0 (2026-11-21)

### Features

- **console**: agent health shows online checks beside eval suites and their schedules (#482) ([7b5400e](https://github.com/urmzd/humagen/commit/7b5400eef2d222e56a41f88cde20fd3382dc01e6))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.178.1...v0.179.0)


## 0.178.1 (2026-11-21)

### Bug Fixes

- **console**: clean up evals, drop dev and permission noise (#480) ([280843f](https://github.com/urmzd/humagen/commit/280843fcfdbef8ce05099416c5028327a2f69026))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.178.0...v0.178.1)


## 0.178.0 (2026-11-21)

### Features

- **agents**: goal-first creation, grouped settings and an agent home (#476) ([53f88f5](https://github.com/urmzd/humagen/commit/53f88f5a501ebe988eee090d5a45bd00935e99fa))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.177.2...v0.178.0)


## 0.177.1 (2026-11-21)

### Bug Fixes

- **surfaces**: Slack sessions listen, read and list as people expect (stack 7/7) (#466) ([4a0ed23](https://github.com/urmzd/humagen/commit/4a0ed23ecb590e8dc38bc575febfc1976b8c9b58))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.177.0...v0.177.1)


## 0.176.1 (2026-11-21)

### Bug Fixes

- **slack**: follow a thread only once Slack holds the app's answer (#469) ([4f35b63](https://github.com/urmzd/humagen/commit/4f35b63ead581b9c1838bb39f0206d77d23cd26e))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.176.0...v0.176.1)


## 0.174.2 (2026-11-21)

### Bug Fixes

- **platform**: called runs get no caller person tool servers (#507) ([13ad1d9](https://github.com/urmzd/humagen/commit/13ad1d95858f07993b31b80119cd371a273ef5b9))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.174.1...v0.174.2)


## 0.174.1 (2026-11-21)

### Bug Fixes

- **console**: stack workspace Apps as connection rows (#502) ([3541b4c](https://github.com/urmzd/humagen/commit/3541b4c34c7fc3e960f5b2b92f002d388d7c1b9e))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.174.0...v0.174.1)


## 0.169.0 (2026-11-21)

### Features

- **web**: an agent's page opens on its home (stack 6/6) (#468) ([81da130](https://github.com/urmzd/humagen/commit/81da130c7c7610d6534107c13740c42141c5d3fb))
- **agents**: an agent's own Slack app, from its manifest and linked by its token (stack 5/6) (#467) ([5034ce7](https://github.com/urmzd/humagen/commit/5034ce765bb7b53539bd018ea6c9e9b0438115ee))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.168.0...v0.169.0)


## 0.166.0 (2026-11-21)

### Features

- **console**: sidebar rows show their source and a labelled state, calm focus (#470) ([3a718dc](https://github.com/urmzd/humagen/commit/3a718dcfc21fb37c0442cd275801d8e39564e4e8))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.165.0...v0.166.0)


## 0.164.1 (2026-11-21)

### Bug Fixes

- **console**: sidebar row preview card (#416) ([ae44611](https://github.com/urmzd/humagen/commit/ae44611277f8755246fbb642582c85c3b0fad75f))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.164.0...v0.164.1)


## 0.163.0 (2026-11-21)

### Features

- **web**: Continue Conversation (#443) ([25a2804](https://github.com/urmzd/humagen/commit/25a2804e6b98d2d8413c6cab2e6f1bb57c3ec91b))
- **slack**: an alert for a person reaches the Slack app as a direct message notice (#424) ([730a20b](https://github.com/urmzd/humagen/commit/730a20b62e856dd17c3f3662d8d7a503abc6715c))

### Bug Fixes

- **slack**: no disclaimer or retry line on Slack answers (#423) ([182a702](https://github.com/urmzd/humagen/commit/182a702e98865ce54707a65cd9b408c6be1b60a4))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.162.1...v0.163.0)


## 0.161.0 (2026-11-20)

### Features

- **background**: every agent's turn for a person has the background tools (#458) ([03c20f3](https://github.com/urmzd/humagen/commit/03c20f3d078da409d3172667297fa5a365d738ae))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.160.0...v0.161.0)


## 0.160.0 (2026-11-20)

### Features

- **approvals**: Run and Answer, where the approval agent also answers what the session settles (stack 6/6) (#457) ([a0156ea](https://github.com/urmzd/humagen/commit/a0156ea3602e88b3e314c2419b424564e1a9e826))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.159.0...v0.160.0)


## 0.156.0 (2026-11-20)

### Features

- **console**: list and stop background work (#449) ([6a61eda](https://github.com/urmzd/humagen/commit/6a61edad9756fc27e948c0d640aa88a0b038893d))

### Misc

- **client**: a stream test names the request it reads (#451) ([32dc2fa](https://github.com/urmzd/humagen/commit/32dc2faf41b7cf913a07fcd2238a7e21c23a2ca4))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.155.0...v0.156.0)


## 0.153.0 (2026-11-20)

### Features

- **auth**: the Slack app the server starts signs in under Tailscale (#444) ([67991c9](https://github.com/urmzd/humagen/commit/67991c9bff2d33a1f713368acf2c19c48df4b0cc))

### Misc

- publish the guide through docs.urmzd.com (#446) ([d7d71a5](https://github.com/urmzd/humagen/commit/d7d71a5ffceb406dd38607c640d46cecb9f64927))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.152.0...v0.153.0)


## 0.152.0 (2026-11-20)

### Features

- **api**: the messages route takes a client's machine posting for a person (#442) ([7838895](https://github.com/urmzd/humagen/commit/783889589d4e0217533a30cd872721e923f9d162))
- **image**: the image carries Node and the Slack app, so surfaces.slack runs in it (#445) ([c9fd2ce](https://github.com/urmzd/humagen/commit/c9fd2ce54cb4997f777bbe47a615cf9f4eb906ef))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.151.0...v0.152.0)


## 0.151.0 (2026-11-20)

### Features

- **cli**: humagen install registers the GitHub or Slack app in the browser and connects it (#440) ([8e0d8ec](https://github.com/urmzd/humagen/commit/8e0d8eca1c58262e283f30109ff1c2c606f92c2e))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.150.0...v0.151.0)


## 0.150.0 (2026-11-20)

### Features

- **slack**: continue a conversation in a code channel, off by default (#406) ([b4e3d48](https://github.com/urmzd/humagen/commit/b4e3d48edbaab4ec7cf88cc07d4d0140a04b406e))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.149.0...v0.150.0)


## 0.146.0 (2026-11-20)

### Features

- **slack**: count a finished turn's work, show a failed command's output, and answer run details on request (#405) ([d289c38](https://github.com/urmzd/humagen/commit/d289c3851bee7facfc8a2d60796d9fe1539f10e4))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.145.0...v0.146.0)


## 0.144.0 (2026-11-20)

### Breaking

- **skills**: read a skill's own files without a tool grant (#397) ([49ddd83](https://github.com/urmzd/humagen/commit/49ddd83c9cc1e48e253139651ccfcc4dee7847ac))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.143.1...v0.144.0)


## 0.143.0 (2026-11-20)

### Features

- **console**: give the agent store its own tab, a catalog and package pages (#395) ([52f290f](https://github.com/urmzd/humagen/commit/52f290f14f1d94c98e9a447eec45b31e84b5629d))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.142.0...v0.143.0)


## 0.142.0 (2026-11-20)

### Features

- **threads**: imported origin and seed (#387) ([a310a33](https://github.com/urmzd/humagen/commit/a310a33c8df384a0596c9416df0cc768bfb29056))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.141.2...v0.142.0)


## 0.141.1 (2026-11-19)

### Bug Fixes

- **console**: keep a session answer's steps after a reload (#390) ([e275587](https://github.com/urmzd/humagen/commit/e275587af2aad7409b5b0e1815db2e0a9117efda))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.141.0...v0.141.1)


## 0.141.0 (2026-11-19)

### Features

- **import**: keep each imported session's structure with its document (#379) ([7a1ae7d](https://github.com/urmzd/humagen/commit/7a1ae7d777efba84a4ad7ece46e43efcaed8af4f))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.140.0...v0.141.0)


## 0.140.0 (2026-11-19)

### Features

- **slack**: the app manifest carries a long description (#388) ([96da0e3](https://github.com/urmzd/humagen/commit/96da0e361bafd5206588dcc218dcfe65ca1168de))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.139.0...v0.140.0)


## 0.139.0 (2026-11-19)

### Breaking

- remove the Claude Code and Codex model kinds (#383) ([af51a6c](https://github.com/urmzd/humagen/commit/af51a6cefda9993c6e6d358421703f37a0ecd9b0))

### Features

- **connections**: add Atlassian and Google Workspace tool server presets (#385) ([658510f](https://github.com/urmzd/humagen/commit/658510fb530418c9ad6eebe63c565a651a0e090f))
- **platform**: add web_search and web_fetch tools (#384) ([5b064f2](https://github.com/urmzd/humagen/commit/5b064f217b1614013cbba28d50075b288643e8ec))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.138.2...v0.139.0)


## 0.138.2 (2026-11-19)

### Bug Fixes

- **library**: take skill packages with larger and more supporting files (#380) ([c12b99f](https://github.com/urmzd/humagen/commit/c12b99f9c67b0edf6b75087ed62ba29b81811fed))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.138.1...v0.138.2)


## 0.138.1 (2026-11-19)

### Bug Fixes

- **api**: record a HEAD as its GET, and end a refused stream at once ([6e1f54d](https://github.com/urmzd/humagen/commit/6e1f54db5fd6b64ed249026c4f967e27b9b4d4e4))
- **import**: hold a skill import to the head it listed, and keep the offline dry run ([c3ad0f0](https://github.com/urmzd/humagen/commit/c3ad0f0acc67165f2b05849d44b2126cab96364f))
- **api**: write a response's read records together, before it is sent ([39e8cf5](https://github.com/urmzd/humagen/commit/39e8cf577df4ea8ab88871cc887bbbeaaa853561))
- **import**: keep drafted in the skills JSON for the skills first drafted ([eca16e7](https://github.com/urmzd/humagen/commit/eca16e75b7c8b524478f4af87f04b57211c8d5d6))
- **api**: refuse a session list whole when a read record cannot be written ([f0deeb9](https://github.com/urmzd/humagen/commit/f0deeb9e7c1bd2374e2d45b6871af08f364a1ada))
- **skills**: draft a changed skill folder on import, and use skills a direct run names ([d930660](https://github.com/urmzd/humagen/commit/d93066060e3b3ff233a16c939ebe81d9ea2c756e))
- **api**: record only the exceptional reads a session page shows, batch alert runs ([6289ed0](https://github.com/urmzd/humagen/commit/6289ed08ed1cc6114e2e68b98dec996050083c01))

### Performance

- **api**: read a page, not a workspace, for runs, sessions and alert counts ([9971d7e](https://github.com/urmzd/humagen/commit/9971d7eba13e4bd18b9d0a1f4925f50e68c33cb2))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.138.0...v0.138.1)


## 0.138.0 (2026-11-19)

### Features

- **import**: file an export's sessions in a project with --project ([03c973d](https://github.com/urmzd/humagen/commit/03c973d4fecfc56b3bd00ef08c71dbbccda377ff))

### Bug Fixes

- **import**: check a project-named source is the session's before replacing in it ([65a3429](https://github.com/urmzd/humagen/commit/65a3429fa412813078bcd768c0cb8d18c3375d17))
- **import**: never write a document or a source that is not the run's own ([4c92898](https://github.com/urmzd/humagen/commit/4c928984c7a9df62e32e9623540fce41aeeeeb18))
- **import**: an export's project source says what it holds and is its own ([41588da](https://github.com/urmzd/humagen/commit/41588da7f9dde2ed60d5e1c8f2b5fe3b285d53e5))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.137.4...v0.138.0)


## 0.137.4 (2026-11-18)

### Bug Fixes

- **console**: precache the shell only, and read a failed control again ([f5d2911](https://github.com/urmzd/humagen/commit/f5d2911593157e5dc84099d825714179dcd66676))

### Performance

- **console**: load pages when they are opened, not with Home ([256841b](https://github.com/urmzd/humagen/commit/256841b6c6ec158efa956b1b8a86460f7198a7b2))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.137.3...v0.137.4)


## 0.137.3 (2026-11-18)

### Bug Fixes

- **console**: draw the runs chart at its width so labels stay 12px ([155a40d](https://github.com/urmzd/humagen/commit/155a40d463e5bbc7c3bac9c7c0d4fe6c00845619))
- **console**: keep every text size at or above 12px ([8dc502d](https://github.com/urmzd/humagen/commit/8dc502d88be76d5cc5bcb5cb767104a1ccbdf9eb))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.137.2...v0.137.3)


## 0.137.2 (2026-11-18)

### Bug Fixes

- **web**: tell apart two run changes inside one millisecond ([72ed1c3](https://github.com/urmzd/humagen/commit/72ed1c3cf18ddcc695cd54c7c87f5c83470a4c02))

### Performance

- **console**: read the list of runs once on load ([5b86db1](https://github.com/urmzd/humagen/commit/5b86db11372fabc21eefa9dd1d5f9dda9a285ffe))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.137.1...v0.137.2)


## 0.137.0 (2026-11-18)

### Features

- **import**: keep times, ids, tool links, attachments and branches of imported sessions ([179eae1](https://github.com/urmzd/humagen/commit/179eae1ca5738e339fe72f01b448d9ff960c0a23))

### Bug Fixes

- **import**: never write over a winner, and condition on title and text ([62d6de4](https://github.com/urmzd/humagen/commit/62d6de476d9d783fb72ba380f7591e85b83a0bc2))
- **import**: replace a changed session document in place, by digest ([10300dc](https://github.com/urmzd/humagen/commit/10300dce90a87399d039040980d54bddf8945e7d))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.136.0...v0.137.0)


## 0.136.0 (2026-11-18)

### Features

- **skills**: give coding agents the pinned skill's supporting files ([b26e1f4](https://github.com/urmzd/humagen/commit/b26e1f4d9c07c74c725de8c14122fca5252bdaf0))

### Bug Fixes

- **skills**: grant a coding agent only its own run's skill files ([706e209](https://github.com/urmzd/humagen/commit/706e2090612dbb9cbe4183f6c495fa42ea601d56))

### Misc

- **deps**: bump saige to v0.45.1 (#369) ([b1ba463](https://github.com/urmzd/humagen/commit/b1ba46363595a8bc8a1d3975e5fe6833e58d5a33))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.135.6...v0.136.0)


## 0.135.6 (2026-11-18)

### Bug Fixes

- **platform**: hand back a run delivered again while this process executes it (#368) ([92f678c](https://github.com/urmzd/humagen/commit/92f678ce7c1c8d4b35eaf915977fc22a84be267d))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.135.5...v0.135.6)


## 0.135.4 (2026-11-17)

### Bug Fixes

- **vendorcli**: take a session once across processes ([1d0351a](https://github.com/urmzd/humagen/commit/1d0351a082c6af829409bd248fc3aac1d468b602))
- **vendorcli**: continue no session where history was left out ([96688f2](https://github.com/urmzd/humagen/commit/96688f2ba18e9cc45fb76817c929614b46d2f1ab))
- **vendorcli**: give each branch of a conversation its own CLI session ([ffa0746](https://github.com/urmzd/humagen/commit/ffa074647e9df4a224a33b52d8f8eb4b1507a7ab))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.135.3...v0.135.4)


## 0.135.3 (2026-11-17)

### Bug Fixes

- **platform**: drop a paused schedule's clock tick ([a4486a7](https://github.com/urmzd/humagen/commit/a4486a797d723d270826e664145d7b6720038aac))
- **platform**: give a clock tick its schedule's agent ([16322a0](https://github.com/urmzd/humagen/commit/16322a073112e0bfdaba0fb07d858540202afccc))

### Misc

- **platform**: bound the waits of the clock tick test ([6092bfc](https://github.com/urmzd/humagen/commit/6092bfc36264a66678f05d5d3f01eac527aac397))
- **platform**: discard the ingest error the test cancels into ([5873e47](https://github.com/urmzd/humagen/commit/5873e4702608f43714f0d00b2dd23229dae71de5))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.135.2...v0.135.3)


## 0.135.2 (2026-11-17)

### Bug Fixes

- **models**: a failed Codex turn is an error even when it gives no reason ([9aa051c](https://github.com/urmzd/humagen/commit/9aa051c8a2309019f51971dcd0309803cf72739a))
- **models**: a notice from Codex does not fail the turn ([96d2c40](https://github.com/urmzd/humagen/commit/96d2c40a8f461fb4d5aa80231c9558ebc105924f))

### Misc

- **examples**: format mail connector profile ([79b4a6c](https://github.com/urmzd/humagen/commit/79b4a6c93198cf71b760774fb07bc1fb3efef4cf))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.135.1...v0.135.2)


## 0.135.0 (2026-11-17)

### Features

- **models**: configure reconciling account exports and drafting a sourced plan ([31fc5ed](https://github.com/urmzd/humagen/commit/31fc5ed31c272649847ab3726381cea59a227d20))

### Bug Fixes

- **rooms**: answer a room stream only once its source is listening ([d2c9758](https://github.com/urmzd/humagen/commit/d2c9758ff9e3dd43fec9be55934bdfe4011c7406))
- **models**: the finance profile writes under out/ wherever the shell is, and the checker validates the whole summary ([3710043](https://github.com/urmzd/humagen/commit/371004367b8b6dc8f16c800607403e4bd16fc907))
- **models**: the finance script stops on copies that disagree and the checker validates every field ([154134f](https://github.com/urmzd/humagen/commit/154134f3573fc8f509b1aa8e24123da8cbc90fd9))

### Misc

- **rooms**: bound the readiness waits and pin a failed source's 500 ([6d0f354](https://github.com/urmzd/humagen/commit/6d0f354847dcfced469a2dbe6901b25c57c4a0fa))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.134.0...v0.135.0)


## 0.133.0 (2026-11-17)

### Features

- **models**: configure local mixed evidence and timestamped ASR ([9e75057](https://github.com/urmzd/humagen/commit/9e75057ceaac427caf219fea693abbbfd0a729c3))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.132.0...v0.133.0)


## 0.129.0 (2026-11-17)

### Features

- **cli**: import sessions by project (#342) ([60ed4c4](https://github.com/urmzd/humagen/commit/60ed4c4820b9d6564cff0aff5788b63625c6e697))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.128.0...v0.129.0)


## 0.125.0 (2026-11-17)

### Features

- **operations**: an assignment is continued until its answer passes its checks ([4f77d3e](https://github.com/urmzd/humagen/commit/4f77d3e1169ffa45cecf79947a6302551424e8fc))

### Misc

- **operations**: a continuation reserved before a restart is admitted after it ([b06d849](https://github.com/urmzd/humagen/commit/b06d849e061f356efef78b65cb32f95ad2fa1309))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.124.1...v0.125.0)


## 0.124.0 (2026-11-17)

### Features

- **models**: show images and PDFs to Claude Code and Codex (#335) ([b84680f](https://github.com/urmzd/humagen/commit/b84680f25127c78a9df86f96ba5f661608b5fee4))

### Bug Fixes

- **console**: give Worked a 44px touch target (#339) ([50d1080](https://github.com/urmzd/humagen/commit/50d1080943c01a4b1ef76840af2878a1bb3c69b4))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.123.1...v0.124.0)


## 0.123.0 (2026-11-17)

### Features

- **models**: pass effort and the program's own commands to Claude Code and Codex (#333) ([4d1657d](https://github.com/urmzd/humagen/commit/4d1657dade89d8b9e30f00299f3fd209501ef4d1))
- **console**: calm Home strip and states that say whose move it is (#332) ([04ea8fd](https://github.com/urmzd/humagen/commit/04ea8fd99018d0848871f1ad2afe9f4cae66cebb))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.122.0...v0.123.0)


## 0.121.0 (2026-11-17)

### Features

- **models**: run a turn on Claude Code or Codex (#329) ([f2ab255](https://github.com/urmzd/humagen/commit/f2ab2558c394ff5f64ba9314d75fce4d9aa7c869))
- **cli**: import sessions, skills and agents from other assistants (#328) ([bf8fa87](https://github.com/urmzd/humagen/commit/bf8fa87e5f569f09af0f58e182b33b343fcb2928))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.120.0...v0.121.0)


## 0.119.0 (2026-11-14)

### Features

- **cli**: add humagen store list, install and remove (#327) ([e6a905b](https://github.com/urmzd/humagen/commit/e6a905b40eaf9d105e7ed0b144ae889e22405cc0))
- **platform**: refuse more than one replica on the in-memory bus (#326) ([027c2a1](https://github.com/urmzd/humagen/commit/027c2a172f81f2f36c885cb79952ba46b4e6c53f))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.118.0...v0.119.0)


## 0.118.0 (2026-11-11)

### Features

- **console**: agents are bots, activity is drawn, and the composer lights up ([ad815f6](https://github.com/urmzd/humagen/commit/ad815f6aa7ba417e4bd166ed73626db1eb35dacc))
- **evals**: review feedback candidates into eval suites ([dde57c6](https://github.com/urmzd/humagen/commit/dde57c63fddc4aac1241cc6ae7a2b37ce2ed4b40))
- **console**: create and delete memory files, list a file's versions, guard drafts ([147190e](https://github.com/urmzd/humagen/commit/147190ef00cdc5ddda39badf1350bab62c1eb9d4))
- **memory**: delete one memory file and list one file's versions over the API ([f8c33b8](https://github.com/urmzd/humagen/commit/f8c33b86516cfefd78a1985640884cd8934e47be))
- **store**: show what each agent is granted before install ([eaa1c10](https://github.com/urmzd/humagen/commit/eaa1c10f53e9ba98b3ffbe62878034ee29e2a684))
- **console**: a workspace Knowledge graph page beside Agents > Knowledge ([7d8aff1](https://github.com/urmzd/humagen/commit/7d8aff1fc3a45a4bd239f51ad50ed669317d40a1))
- **notifications**: start quiet hours from the browser's time zone ([623faaf](https://github.com/urmzd/humagen/commit/623faafe7fd5093b79b47d5aee70a24f3ab9329e))
- **console**: add an Evals tab to a corpus ([938871a](https://github.com/urmzd/humagen/commit/938871a02126a0b001de1cb7259f3b07b187c9fd))
- **console**: edit memory files ([8863ebc](https://github.com/urmzd/humagen/commit/8863ebc08064704f569e894e03a183cb7f3d5e65))
- **memory**: write one memory file over the API ([c2a86b3](https://github.com/urmzd/humagen/commit/c2a86b3bfd72ac9328e2df36b7322718f269894a))
- **console**: browse memory as files ([d215aac](https://github.com/urmzd/humagen/commit/d215aac2eb39616855831145f642f2cbc4fdb5ec))
- **console**: show how fresh a corpus's sources are ([14ac8f2](https://github.com/urmzd/humagen/commit/14ac8f23c60109c3299dfc47ed35821adbd8ac1e))
- **console**: agent calls as items, one activity tree, working threads in the prompt ([bcf30b4](https://github.com/urmzd/humagen/commit/bcf30b42b4a9539490a493fc09f3f1777585f555))

### Bug Fixes

- **console**: store requirement labels sit at the top of their row ([2b8d492](https://github.com/urmzd/humagen/commit/2b8d492c85b203bc7f1a145ddf47a3a7c18a7784))
- **store**: each agent is one line that opens to aligned rows ([d57a29a](https://github.com/urmzd/humagen/commit/d57a29a1ef6cb300de09427fc86d589e1da8c04c))

### Misc

- regenerate copy keys for the consolidated console changes ([f1a88ef](https://github.com/urmzd/humagen/commit/f1a88ef16d5f092ef1bc342a7164fc0b3686b5c4))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.117.4...v0.118.0)


## 0.117.4 (2026-11-11)

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.117.3...v0.117.4)


## 0.117.3 (2026-11-11)

### Bug Fixes

- **messages**: the root and session listings page by place, not by position (#323) ([b298185](https://github.com/urmzd/humagen/commit/b2981851cb4a7160abd5530d85eadacd91d250f5))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.117.2...v0.117.3)


## 0.117.1 (2026-11-11)

### Bug Fixes

- **egress**: a refused write to Slack or GitHub stops, pauses or backs off instead of repeating (#321) ([ba6b515](https://github.com/urmzd/humagen/commit/ba6b515d1b7e6a454d4f1f2e500e90b36b4a8456))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.117.0...v0.117.1)


## 0.117.0 (2026-11-10)

### Features

- **console**: Workspace settings › Voice sets up the workspace's voice provider (#261) ([e1b9659](https://github.com/urmzd/humagen/commit/e1b96595b1d8de0ab9abad1a3108e8e0264cb0d7))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.116.0...v0.117.0)


## 0.115.0 (2026-11-10)

### Features

- **agents**: optional agents install from an agent store (#311) ([b49fa92](https://github.com/urmzd/humagen/commit/b49fa92e7152ce1581a936a536b9846ce492b0d5))

### Misc

- **slack**: mention-first app manifest, generated from code and maintained with the Slack CLI (#278) ([23c02f5](https://github.com/urmzd/humagen/commit/23c02f5d0941fb26b68292da2dd6ecae6225bc46))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.114.3...v0.115.0)


## 0.114.3 (2026-11-10)

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.114.2...v0.114.3)


## 0.114.2 (2026-11-10)

### Bug Fixes

- three intermittent CI failures: a write deadlock, a test expectation, an empty module cache (#319) ([cd9a004](https://github.com/urmzd/humagen/commit/cd9a004fba2852fe3b8fc7030a323c697c246a7d))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.114.1...v0.114.2)


## 0.114.1 (2026-11-09)

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.114.0...v0.114.1)


## 0.114.0 (2026-11-09)

### Features

- **console**: Agents › Knowledge lists corpora, each with its own sources (#252) ([67f7d4f](https://github.com/urmzd/humagen/commit/67f7d4f6d74bc041ca90e7fcb851f8125beaffc5))
- **knowledge**: OneDrive as a knowledge source (#284) ([5ed19b6](https://github.com/urmzd/humagen/commit/5ed19b6514fd24cbfb71b77b00a72006b3ab6f95))
- **knowledge**: resolve one corpus item by its address (#276) ([44dc29c](https://github.com/urmzd/humagen/commit/44dc29ce2b0b5f61f0e523017559da8e3b88f892))
- **knowledge**: a session reaches the corpora its project attaches (#271) ([a83f902](https://github.com/urmzd/humagen/commit/a83f902a7dc42b1caa32aec0e38e5bd88795874e))
- **knowledge**: retrieval evals for corpora (#269) ([283d844](https://github.com/urmzd/humagen/commit/283d844659c5c636fd1e24db9c13dbb70ea2bac0))
- **knowledge**: corpus labels decide where their content may go (#267) ([7c43c52](https://github.com/urmzd/humagen/commit/7c43c52177a93adbde3ec7a852d6d09b61eb23a1))
- **knowledge**: maintenance tells owners what their corpora need (#265) ([3e59676](https://github.com/urmzd/humagen/commit/3e59676c8f00e6fb7113e5e1e2f475e9ddbbef05))
- **knowledge**: a workspace graph built from what corpora hold (#253) ([3353791](https://github.com/urmzd/humagen/commit/3353791b3aaadc65ba9cd52ab37b001ec7cadfdc))
- **knowledge**: search a corpus across what its members hold (#248) ([332075d](https://github.com/urmzd/humagen/commit/332075d27ab22fee2dca14b126242878415cf1f4))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.113.0...v0.114.0)


## 0.112.0 (2026-11-09)

### Features

- **agents**: one agent tool makes every agent call (#313) ([6bf4d65](https://github.com/urmzd/humagen/commit/6bf4d655a09e7f4332cc89f35fb164efa0e565b6))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.111.1...v0.112.0)


## 0.111.1 (2026-11-09)

### Bug Fixes

- **web**: console streams, Recents, times, tabs and loading (#314) ([c0d16fc](https://github.com/urmzd/humagen/commit/c0d16fc132853f64bb52bded7289411052a0e906))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.111.0...v0.111.1)


## 0.111.0 (2026-11-09)

### Features

- **mcp**: a first-party MCP over one registry of system tools (#312) ([1533557](https://github.com/urmzd/humagen/commit/1533557cfd91311fd150eb246b03c3e1399f269f))
- **agents**: output schemas; the approval agent answers in one (#308) ([cceb148](https://github.com/urmzd/humagen/commit/cceb1489f4999b79e0c2a3fd04541d817507da5f))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.110.0...v0.111.0)


## 0.106.0 (2026-11-09)

### Features

- **web**: answer in Activity by the call that waits, on the message (#301) ([4bc3e43](https://github.com/urmzd/humagen/commit/4bc3e43562c614f88bed2a75649fd916d6109881))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.105.0...v0.106.0)


## 0.103.0 (2026-11-09)

### Features

- **messages**: trace what ran to answer a message (#297) ([58645d6](https://github.com/urmzd/humagen/commit/58645d65e05e2eb184b8083019ad560ccf7d0604))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.102.0...v0.103.0)


## 0.102.0 (2026-11-09)

### Features

- **messages**: read a message as a tree, list roots (#285) ([6f7a179](https://github.com/urmzd/humagen/commit/6f7a179347434a5b206a891af82d0f255a63ad00))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.101.0...v0.102.0)


## 0.100.0 (2026-11-09)

### Features

- **api**: one messages API for every surface (#277) ([759c9c7](https://github.com/urmzd/humagen/commit/759c9c7b577eb926754ecbfe06720d4fba0b2684))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.99.2...v0.100.0)


## 0.99.2 (2026-11-09)

### Bug Fixes

- **join**: route on what agents are for, not on instruction filler (#296) ([c1a8ded](https://github.com/urmzd/humagen/commit/c1a8ded5f04c4b1dd726cfc575f013ad9af35520))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.99.1...v0.99.2)


## 0.99.0 (2026-11-08)

### Features

- **github**: review pull requests automatically; labels start nothing (#259) ([cdc0dd3](https://github.com/urmzd/humagen/commit/cdc0dd3331aaa86fd6503be1eb6a5765dec27179))
- **console**: one Details panel: progress, agents, outputs, environment and what a session used (#293) ([26a466c](https://github.com/urmzd/humagen/commit/26a466c7154a66a3815231156c6913bcf7e3ee94))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.98.0...v0.99.0)


## 0.97.1 (2026-11-08)

### Bug Fixes

- **sidebar**: Recents is one list by local day; Recents and Sessions list newest first (#291) ([03f5c1f](https://github.com/urmzd/humagen/commit/03f5c1f36d19453bab80cf3014dffae636588a12))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.97.0...v0.97.1)


## 0.97.0 (2026-11-08)

### Features

- **console**: an item page and Sessions filters by place and item (#282) ([2a7d801](https://github.com/urmzd/humagen/commit/2a7d8014905ae644d1acddfac4dcba2a6a93d342))
- **console**: every message has one address, its references and its delegated work (#281) ([a7a57f3](https://github.com/urmzd/humagen/commit/a7a57f3188bfcd872c6796c3994ceabbe12c6cc7))
- **console**: one card, one reference chip and one menu for every page (#279) ([240ae87](https://github.com/urmzd/humagen/commit/240ae8737864d3ae461c372248d7a8833937c244))
- **approvals**: the approval agent is a built-in that decides calls in Run (#255) ([5742393](https://github.com/urmzd/humagen/commit/574239398af3d6aa471e72426dedfdd1dba1227e))
- **models**: admin model providers; no built-in or offline model (#254) ([ef75213](https://github.com/urmzd/humagen/commit/ef752139865715cf90bd6014d34740fc14ab3959))
- **knowledge**: query a corpus's warehouses live with the reader's own login (#247) ([e71dc08](https://github.com/urmzd/humagen/commit/e71dc08634532ae3da4c5e2e1364737c62433bbc))
- **followups**: queue, replace or stop work when a message arrives mid-run (#256) ([a3b0710](https://github.com/urmzd/humagen/commit/a3b07101a2f4f9995c8c833abb64f0af3a0eb89b))

### Bug Fixes

- **spec**: retired fields load with a note instead of stopping a start (#292) ([bc242a4](https://github.com/urmzd/humagen/commit/bc242a4ffb06537296e1d2eb9cde0ced23d77c7d))

### Misc

- **platform**: entry agent tests use the test builtin model fields (#290) ([51cb54f](https://github.com/urmzd/humagen/commit/51cb54fd2b157faeeb944c8572f8fb02ba550437))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.96.0...v0.97.0)


## 0.95.0 (2026-11-08)

### Breaking

- **projects**: agents are independent of projects (#251) ([6756c49](https://github.com/urmzd/humagen/commit/6756c492d876f35e3acb9ebd5c5a859a9f3d0944))

### Features

- **projects**: a project's context is the corpora it attaches (#262) ([ffe374c](https://github.com/urmzd/humagen/commit/ffe374cd56db851d2b4468ae23b50a1fa3e1c5b3))
- **console**: Projects is a grid of cards with search, sort and pins (#268) ([33aeb44](https://github.com/urmzd/humagen/commit/33aeb44285156db16eea063881a847f9b9a861fc))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.94.0...v0.95.0)


## 0.93.0 (2026-11-08)

### Features

- **console**: per-agent models, settings split and automation delivery (#241) ([c9a5d80](https://github.com/urmzd/humagen/commit/c9a5d80a57368183dce9c0408d76714fff8aa2d5))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.92.0...v0.93.0)


## 0.92.0 (2026-11-08)

### Features

- **console**: add session workspace connections and review controls (#240) ([b296b5b](https://github.com/urmzd/humagen/commit/b296b5b489864b37bf938d695e3faf0886d8c33e))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.91.0...v0.92.0)


## 0.91.0 (2026-11-08)

### Features

- **catalog**: add durable managed BigQuery connection setup (#238) ([a6c2571](https://github.com/urmzd/humagen/commit/a6c2571cf91cfd1831ccf23e0c22c133948813df))
- **connections**: add scoped consent health and private builder tools (#237) ([13bdf5c](https://github.com/urmzd/humagen/commit/13bdf5c71ef6d20f23aab04a2a62645dd9d68398))
- **sessions**: connect native reviews feedback and source alerts (#236) ([b4004c7](https://github.com/urmzd/humagen/commit/b4004c7fccb7229f0f16afaa0c7ccced0e43b980))
- **connections**: add durable consent and native installation foundation (#235) ([72ad896](https://github.com/urmzd/humagen/commit/72ad896fe9732792a2118c3db9976a0d854ae432))
- **evals**: gate agent changes with pinned prompts and private trace evaluations (#234) ([5d2af9c](https://github.com/urmzd/humagen/commit/5d2af9c603b9f79f00d72b9eb0859a3f5345cb7b))
- **alerts**: project actionable session alerts with private priority delivery (#233) ([d720871](https://github.com/urmzd/humagen/commit/d720871b9d56a82d7b16acf53f713b42afe2adc9))
- **evals**: persist evaluation receipts and bounded prompt snapshots (#232) ([29465a0](https://github.com/urmzd/humagen/commit/29465a007a927fafdad8abac9fbbb4ba9e3fd49e))
- expose governed agent sessions and resumable transcript APIs (#231) ([ac2a543](https://github.com/urmzd/humagen/commit/ac2a5431037737addce14d0663b5942076f98ddf))
- persist session alert lifecycle and preserve pending attention (#230) ([097f63d](https://github.com/urmzd/humagen/commit/097f63d736dfa65bd3c979596a76bce46566a074))
- persist agent session identity and admission state (#228) ([98942eb](https://github.com/urmzd/humagen/commit/98942ebea2c3af57a23f2f8596ce1481be0e0a5c))

### Bug Fixes

- **connections**: fence native rejection to current consent and owner (#239) ([9ae5e81](https://github.com/urmzd/humagen/commit/9ae5e81d2af85cb64a1f86715058f3f022260bcb))
- **artifacts**: reuse saved content across retries and concurrent saves (#227) ([ea988a6](https://github.com/urmzd/humagen/commit/ea988a6b1db19542a61589bb421d6be26c230e0c))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.90.0...v0.91.0)


## 0.90.0 (2026-11-07)

### Features

- **helm**: configurable server rollout, so object-store deploys keep a pod serving (#225) ([6fdb095](https://github.com/urmzd/humagen/commit/6fdb095687b33f21c0defd2fceaf4cf7ba2583c0))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.89.2...v0.90.0)


## 0.89.2 (2026-11-07)

### Bug Fixes

- **console**: design review fixes from an independent pass (#224) ([1f660c2](https://github.com/urmzd/humagen/commit/1f660c275866b797a874ed4c26f8f205b401ad9e))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.89.1...v0.89.2)


## 0.89.1 (2026-11-07)

### Bug Fixes

- **console**: titles and project picking in every conversation (#223) ([f16ffe3](https://github.com/urmzd/humagen/commit/f16ffe316911d21d91292ee4356cab59985e9033))

### Misc

- preserve public host in forwarded replica checks (#222) ([6c1cfb8](https://github.com/urmzd/humagen/commit/6c1cfb88f06df319fa65d6d485d9e9936685520d))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.89.0...v0.89.1)


## 0.89.0 (2026-11-06)

### Features

- one chat for all work, built-in reviewers, agent packages and humagen create (#218) ([b2cd619](https://github.com/urmzd/humagen/commit/b2cd619dd572bcecb765e55ed6c27f1f665a1112))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.88.0...v0.89.0)


## 0.88.0 (2026-11-06)

### Features

- preserve connector delivery across replica changes (#221) ([9be066e](https://github.com/urmzd/humagen/commit/9be066e369271c131c12a9b1078d7a141832b146))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.87.0...v0.88.0)


## 0.87.0 (2026-11-05)

### Features

- edit per-message choices in the agent model tab (#219) ([84adec1](https://github.com/urmzd/humagen/commit/84adec1632965c381bd3231b15fdf72acf3a2e7c))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.86.1...v0.87.0)


## 0.86.1 (2026-11-05)

### Bug Fixes

- reconcile run completion after a replica crash (#220) ([ee73cc4](https://github.com/urmzd/humagen/commit/ee73cc48f2b3019d6fe377b0aac6d8e35ba6a97e))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.86.0...v0.86.1)


## 0.86.0 (2026-11-05)

### Features

- model and effort override per message (#216) ([4162ae7](https://github.com/urmzd/humagen/commit/4162ae71edbaba456694776a30e409c8eedb743c))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.85.0...v0.86.0)


## 0.85.0 (2026-11-05)

### Features

- **platform**: accept runs through the bus outbox; audit observer redelivery (#215) ([196a93a](https://github.com/urmzd/humagen/commit/196a93aae6b6a17f82f5029e9cf9f94c23cd62b6))

### Misc

- **e2e**: two-replica end-to-end check, and three cross-replica fixes (#217) ([527de3d](https://github.com/urmzd/humagen/commit/527de3d934a35de4a9018c3181c54d5c58b60135))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.84.0...v0.85.0)


## 0.83.0 (2026-11-04)

### Features

- **speech**: Groq models and voice notes (#210) ([225dfdc](https://github.com/urmzd/humagen/commit/225dfdc7be4a35ceaa03024b62f5bd0a7690be39))
- **console**: add pull requests board and list (#201) ([da86a3f](https://github.com/urmzd/humagen/commit/da86a3f7e5ce9d52274038a8802ef7977a0f21c1))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.82.0...v0.83.0)


## 0.83.0 (2026-11-04)

### Features

- **console**: add pull requests board and list (#201) ([da86a3f](https://github.com/urmzd/humagen/commit/da86a3f7e5ce9d52274038a8802ef7977a0f21c1))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.82.0...v0.83.0)


## 0.82.0 (2026-11-04)

### Features

- **review**: add GitHub issue and pull request workspace tabs (#203) ([761a7d8](https://github.com/urmzd/humagen/commit/761a7d820e4bbd54d5e7b58c03a9c55e15983090))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.81.0...v0.82.0)


## 0.79.0 (2026-11-04)

### Features

- **composer**: Home's project over the chat bar, and Approve for Me per conversation (#209) ([ed969c2](https://github.com/urmzd/humagen/commit/ed969c21b67d7f67d8d19b191bc822d556e2574b))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.78.0...v0.79.0)


## 0.78.0 (2026-11-04)

### Features

- **customize**: unify setup and connect personal MCP tools (#199) ([e21dbab](https://github.com/urmzd/humagen/commit/e21dbab77a8f663d38e7da0fb4fa470bdcd3baa3))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.77.0...v0.78.0)


## 0.77.0 (2026-11-03)

### Features

- GitHub App tokens, people maps and the data catalog (ported) (#207) ([3b4c845](https://github.com/urmzd/humagen/commit/3b4c845eb288d6b071fc080616b96a1237fe09b8))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.76.0...v0.77.0)


## 0.76.0 (2026-11-03)

### Features

- **automations**: build schedules and signed GitHub event rules (#200) ([e9f4019](https://github.com/urmzd/humagen/commit/e9f4019a441c82e1be1b7cdfff0f0cc663ef45b4))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.75.0...v0.76.0)


## 0.74.0 (2026-11-03)

### Features

- **sidebar**: Recents, Pinned, Projects and Archived with per-person marks (#191) ([75038aa](https://github.com/urmzd/humagen/commit/75038aa6f1faedcd2ef13d230af545c03748cf6c))
- notification views, per-reader hide and delete, bulk marks with undo (#190) ([7d39054](https://github.com/urmzd/humagen/commit/7d39054aa8c261bd878d8a88fb236083d0125de7))
- **console**: save messages, reviews and scratchpads as artifacts, and diff versions (#193) ([0d47db3](https://github.com/urmzd/humagen/commit/0d47db36a6282ecf25545dca0a244aa6409a19dc))
- **console**: the Workspace is a pane of tabs beside the conversation (#186) ([016a470](https://github.com/urmzd/humagen/commit/016a47031fbf260f7a2ae2cecb8198915f2bd063))
- **console**: memories by conversation, session and message (#183) ([9e214de](https://github.com/urmzd/humagen/commit/9e214deea48eb193016c665aa96ab9ae62c7dfa0))
- **console**: named scratchpads per conversation, read by its agents (#182) ([1078f6e](https://github.com/urmzd/humagen/commit/1078f6eb15de6ac55ca77f4750a297bae4acf756))
- **console**: one Workspace panel and a compact conversation header (#180) ([6845161](https://github.com/urmzd/humagen/commit/68451613b08b347daaf21b0d151da884cb515460))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.73.0...v0.74.0)


## 0.73.0 (2026-11-03)

### Features

- **console**: a finished turn ends with the files it changed (#189) ([b958c19](https://github.com/urmzd/humagen/commit/b958c19b0d00e1ca4144902595b1297d74752b27))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.72.0...v0.73.0)


## 0.72.0 (2026-11-03)

### Features

- **console**: a turn reads as a transcript, with tool runs as one line each (#188) ([4a253e4](https://github.com/urmzd/humagen/commit/4a253e4d939b5745e45325c5211c0f5142d4a288))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.71.0...v0.72.0)


## 0.71.0 (2026-11-03)

### Features

- slash commands are the only way to offer skills in the composer (#187) ([b55cd47](https://github.com/urmzd/humagen/commit/b55cd47442d5b651fc0d568fb223d33fccecb503))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.70.0...v0.71.0)


## 0.70.0 (2026-11-03)

### Features

- **console**: structured reviews read as a card, with cited code in place (#184) ([49e8b93](https://github.com/urmzd/humagen/commit/49e8b938028c298676004905cd3956ecc4fc47df))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.69.0...v0.70.0)


## 0.68.0 (2026-11-01)

### Features

- **console**: a phone that reads like a native chat app, the brand's glow, and a conversation wave (#179) ([995e628](https://github.com/urmzd/humagen/commit/995e628a3d8a60e9b698a7a474790cc6700b25a6))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.67.0...v0.68.0)


## 0.67.0 (2026-11-01)

### Features

- **clients**: seat clients on every replica and lease Socket Mode Slack apps (#178) ([45a6f14](https://github.com/urmzd/humagen/commit/45a6f14e04b522bef3308faa0c59ff9c43e00355))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.66.0...v0.67.0)


## 0.66.0 (2026-11-01)

### Features

- **notice**: watch notices reach every replica through Postgres NOTIFY (#177) ([bde73fe](https://github.com/urmzd/humagen/commit/bde73fe1a72dc79be3917641175fab60ee3c3aed))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.65.0...v0.66.0)


## 0.65.0 (2026-11-01)

### Features

- **state**: drop the single-writer lock and allow replicas (RFC 0004, change 9, step 3) (#175) ([3b41a42](https://github.com/urmzd/humagen/commit/3b41a4238007e56975fd1087551e2fd4fba4234d))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.64.0...v0.65.0)


## 0.64.0 (2026-11-01)

### Features

- **stores**: every store reads and writes per request (RFC 0004, change 9) (#176) ([86f42c5](https://github.com/urmzd/humagen/commit/86f42c549d18dfb7dd3eeaac697af75255576cc4))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.63.0...v0.64.0)


## 0.63.0 (2026-10-31)

### Features

- production-ready deploy: /readyz, graceful drain, gcs blobs, chart scaling (#161) ([b23f659](https://github.com/urmzd/humagen/commit/b23f65988f5983fc2c74ceb20aaae8bcb6733506))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.62.0...v0.63.0)


## 0.59.0 (2026-10-31)

### Features

- agents are @mentions, and every agent a message tags answers (#160) ([b50d50a](https://github.com/urmzd/humagen/commit/b50d50a962d0eae4a2676a1955cd33c9a2f11456))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.58.0...v0.59.0)


## 0.58.0 (2026-10-31)

### Features

- **limits**: ask a person to approve a run at a limit (#157) ([8dd9ddd](https://github.com/urmzd/humagen/commit/8dd9ddd6fc43626bdcc7fc0a0046d6f319f40189))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.57.0...v0.58.0)


## 0.57.0 (2026-10-31)

### Features

- **catalog**: connectors in the spec, catalog tools and routes (RFC 0005, change 2, 3/3) (#156) ([89b09a3](https://github.com/urmzd/humagen/commit/89b09a34746b4789ad73c12784578f970674b136))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.56.0...v0.57.0)


## 0.56.0 (2026-10-31)

### Features

- **fiber**: let a host park a run on a question of its own (#153) ([76764d6](https://github.com/urmzd/humagen/commit/76764d625438b201cc1d0a5e0eab40894df8f13d))
- **sandbox**: drive a Docker daemon on a sandbox host (#150) ([4ea7208](https://github.com/urmzd/humagen/commit/4ea72080538bee08bfc2091852fff0a98b8051b2))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.55.0...v0.56.0)


## 0.54.0 (2026-10-31)

### Features

- **catalog**: tenant-scoped catalog store with hybrid search (#154) ([12df401](https://github.com/urmzd/humagen/commit/12df4014cb047edde78ea0f5929b5280e4f535ad))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.53.0...v0.54.0)


## 0.52.0 (2026-10-31)

### Features

- **sandbox**: egress gate so a run's tools reach allowed hosts (#151) ([f19ec7f](https://github.com/urmzd/humagen/commit/f19ec7fe2b6bd1111b95dfad706e530efe49d04d))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.51.1...v0.52.0)


## 0.51.1 (2026-10-31)

### Bug Fixes

- feedback prompt capture, configured-runtime compaction budget, held-call docs (#149) ([ece3f25](https://github.com/urmzd/humagen/commit/ece3f257d99641342def7d8ffa04c1c642c7badf))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.51.0...v0.51.1)


## 0.50.0 (2026-10-31)

### Features

- **console**: list, add and remove a person's own tool servers (#148) ([f012088](https://github.com/urmzd/humagen/commit/f012088b5ac335893b7f198647923445be8a5a11))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.49.0...v0.50.0)


## 0.49.0 (2026-10-30)

### Features

- **console**: work on a phone and install as an app (#144) ([dbf6d38](https://github.com/urmzd/humagen/commit/dbf6d3815d142c5bbd6cd327e7e48758f801e282))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.48.2...v0.49.0)


## 0.48.2 (2026-10-30)

### Bug Fixes

- **deps**: bump saige to v0.44.0 so Gemini runs are priced (#143) ([5644167](https://github.com/urmzd/humagen/commit/5644167a2c0402bd530d34f86ac5212247db19c3))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.48.1...v0.48.2)


## 0.48.1 (2026-10-30)

### Bug Fixes

- **site**: social previews, structured data and accessibility fixes (#142) ([bfc3303](https://github.com/urmzd/humagen/commit/bfc3303621434bd87f1c13e7cad089f29e239d22))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.48.0...v0.48.1)


## 0.48.0 (2026-10-30)

### Features

- **console**: smooth screen changes, delayed skeletons and a glass app frame (#141) ([c11268b](https://github.com/urmzd/humagen/commit/c11268bc548aa45f52ed05b7e30efa8f28adb69f))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.47.0...v0.48.0)


## 0.47.0 (2026-10-30)

### Features

- **site**: withhumagen.com product site and deploy guide (#140) ([1e57ad6](https://github.com/urmzd/humagen/commit/1e57ad68d18b812e690fe6711ba7f9665471f5b3))

### Misc

- remove the legacy Storybook and the design boards Pages site (#139) ([278001c](https://github.com/urmzd/humagen/commit/278001cefc27d92e6117003a1c26852843e91b85))
- build the container image and run the Postgres suites (#134) ([4af2ac8](https://github.com/urmzd/humagen/commit/4af2ac8288747db59678267b70a33732ec7f8115))
- **deps**: bump playscape to v0.10.0 (#137) ([ed7afbe](https://github.com/urmzd/humagen/commit/ed7afbe1c297536f7502ccbc275722b758e4621c))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.46.2...v0.47.0)


## 0.46.1 (2026-10-30)

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.46.0...v0.46.1)


## 0.46.0 (2026-10-30)

### Features

- **knowledge**: hybrid search with a configurable embedder (RFC 0005, change 1) (#132) ([c7c6736](https://github.com/urmzd/humagen/commit/c7c6736367f23c13d5e0a5814f4a9133fdaa20db))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.45.0...v0.46.0)


## 0.45.0 (2026-10-30)

### Features

- **usage**: striped admission counters instead of a lock per tenant (#130) ([bf7e7bf](https://github.com/urmzd/humagen/commit/bf7e7bf1c5dd6c68a1d7c0622b85b5fbd589b86b))
- **feedback**: read and write per request, without the lock (#126) ([22679ff](https://github.com/urmzd/humagen/commit/22679ff3257510c5d05f38512c253f3088ce66c2))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.44.0...v0.45.0)


## 0.44.0 (2026-10-30)

### Features

- run a run's tools in its isolated instance (RFC 0004, layer 4) (#127) ([3b4f288](https://github.com/urmzd/humagen/commit/3b4f2883246f9d5cb7bde39435f219ed73488cc3))
- **console**: keep the New Source intro fixed while the catalog scrolls (#129) ([cd683a8](https://github.com/urmzd/humagen/commit/cd683a8e8e9fdd4c9a5064f087a25f3201dfb36f))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.43.0...v0.44.0)


## 0.42.0 (2026-10-30)

### Features

- memory as files (RFC 0004, change 6) (#128) ([7e5c673](https://github.com/urmzd/humagen/commit/7e5c673e6b0ac70c5dff615438accab1b0756541))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.41.0...v0.42.0)


## 0.41.0 (2026-10-30)

### Features

- **rooms**: large rooms, coalesced reads and per-room admission (RFC 0004) (#125) ([f28d631](https://github.com/urmzd/humagen/commit/f28d6318dc645bdaad024cf4e0c1472c5f9c3423))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.40.0...v0.41.0)


## 0.40.0 (2026-10-30)

### Features

- run context, person store and setting kinds (RFC 0004, change 5) (#122) ([06a5051](https://github.com/urmzd/humagen/commit/06a5051a478ee115d95be63da7f742b60dd3d3b7))

### Misc

- **deps**: skip npm versions younger than 24h (#123) ([3b66823](https://github.com/urmzd/humagen/commit/3b66823e8ebc2c8f03a846688674e893c4a76940))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.39.0...v0.40.0)


## 0.39.0 (2026-10-30)

### Features

- **usage**: keep admission and spend counts in the shared store (#120) ([f62b3a2](https://github.com/urmzd/humagen/commit/f62b3a2a6835d2dc05186b324b74a5c1a927c60e))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.38.2...v0.39.0)


## 0.38.2 (2026-10-30)

### Bug Fixes

- **docker**: copy fiber into the Go build stage (#118) ([b8505b8](https://github.com/urmzd/humagen/commit/b8505b89eeb1238993151787657f21079b017a36))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.38.1...v0.38.2)


## 0.38.1 (2026-10-29)

### Bug Fixes

- **deps**: patch vulnerable dependencies (#117) ([7e61ec6](https://github.com/urmzd/humagen/commit/7e61ec6daf80b81b64d3fe9de2ff08321693802f))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.38.0...v0.38.1)


## 0.38.0 (2026-10-29)

### Features

- **dev**: reusable Tilt development workflow (#116) ([872fb61](https://github.com/urmzd/humagen/commit/872fb6188085d69cc285914f68023e3a17161299))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.37.1...v0.38.0)


## 0.37.1 (2026-10-29)

### Bug Fixes

- **web**: copy an answer as Markdown in one click (#115) ([f0dbbb1](https://github.com/urmzd/humagen/commit/f0dbbb130534d5ee1ea6d0f410d6a8d58c4c3c7a))

### Misc

- check console screens on every push to main (#113) ([a3c1266](https://github.com/urmzd/humagen/commit/a3c12665422d33243be0093bf61284eca851caeb))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.37.0...v0.37.1)


## 0.37.0 (2026-10-29)

### Features

- **console**: quieter, ready-to-read console screens (#112) ([20deae0](https://github.com/urmzd/humagen/commit/20deae0a812c7b5a2c9e605c5b96b2ae413bc99d))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.36.0...v0.37.0)


## 0.36.0 (2026-10-29)

### Features

- **console**: Worked duration, and docs for the agent experience (#111) ([59648b7](https://github.com/urmzd/humagen/commit/59648b74b4187a12b13722aa1914393f77ebc4ef))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.35.0...v0.36.0)


## 0.35.0 (2026-10-29)

### Breaking

- agent experience in the console, Google models and provider health (#110) ([a273654](https://github.com/urmzd/humagen/commit/a273654e175157c06215d58d6f7ae6b988e6135b))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.34.0...v0.35.0)


## 0.33.0 (2026-10-29)

### Features

- brand the console, its text and emails from the spec; run on Tilt (#107) ([d479987](https://github.com/urmzd/humagen/commit/d479987cc06a3bd84ea3cc8915cf70dec81e6bda))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.32.0...v0.33.0)


## 0.32.0 (2026-10-28)

### Breaking

- fold fiber into humagen on the typed event union (#106) ([0fa78dc](https://github.com/urmzd/humagen/commit/0fa78dc2469876d3837143a84e118cea1d415c85))
- **web,slack**: console and Slack app on the union ([a77f1ca](https://github.com/urmzd/humagen/commit/a77f1ca94caa2480f9a2b265cf2186f1682b91ad))
- **api**: event union in the API contract, client and adapter ([6529729](https://github.com/urmzd/humagen/commit/65297297b978341b29d88a3afb3dc987a069b310))
- Humagen Go on the event union ([3c7bce4](https://github.com/urmzd/humagen/commit/3c7bce4cfd1970c6188a5c8a86feb076f6309a45))
- remove bridges and old packages ([337e06b](https://github.com/urmzd/humagen/commit/337e06b5f73d12650d877d8c3bef980e00a71af8))
- **connectors**: ingress and egress adapters, webhook body v2, Tag removed ([8644cb8](https://github.com/urmzd/humagen/commit/8644cb86a4b511b92cdd4bf9c2c50a9eae64b6c7))
- **definition**: saige definitions with fail-closed pins replace agentspec and registry ([382b955](https://github.com/urmzd/humagen/commit/382b95550d86637444ba81454d025b11154cb7d3))
- **bus**: channel bus on envelopes, consumers read typed bodies ([e960432](https://github.com/urmzd/humagen/commit/e96043242857ca061f5bc6e6a2268bef7b0255fe))
- fiber is a library composed in code; no binary, no server, no mandatum.v1 (#39) ([ea8ca50](https://github.com/urmzd/humagen/commit/ea8ca501f1d061cb36d00ac97c332a17c300ca12))
- rename mandatum to fiber (#19) ([78aa579](https://github.com/urmzd/humagen/commit/78aa5799001388fb2226f9c0ef14c25ad758f1eb))
- park a run on a person and resume it with the answer (#16) ([8aaec82](https://github.com/urmzd/humagen/commit/8aaec82b8fa753d3438258f60da17b94629cc09f))
- **runtime**: scope a run's identity to its tenant ([3a4ad50](https://github.com/urmzd/humagen/commit/3a4ad507137532d818e047f09348bd79e704546b))
- **server**: accept file uploads and take a tag's actor from the credential ([45fa707](https://github.com/urmzd/humagen/commit/45fa707eec021e32e1fc85f1626dbf74ffad5e3a))
- **registry**: add a Postgres store and an atomic expected-revision delete ([7d345e4](https://github.com/urmzd/humagen/commit/7d345e4eca353daec9546585401345c209531b30))
- **connectors**: separate policy and bind Slack ingress to teams (#11) ([612a002](https://github.com/urmzd/humagen/commit/612a0027ac7ce73407d5e95ce8847ce322ae7f42))
- depend on legatus, the renamed dispatch (#8) ([61ff1f1](https://github.com/urmzd/humagen/commit/61ff1f1aa4dba9ed04e29901d49d1c5fd65a36ca))
- rename the project from opentag to mandatum (#7) ([92e7d1b](https://github.com/urmzd/humagen/commit/92e7d1b5e0275abcef0779dc755a39e966379173))

### Features

- **agents**: tenant definition store on pkg/state with conversion ([4872c02](https://github.com/urmzd/humagen/commit/4872c02a2ba47669f5ca4ffcc4882b05b8625165))
- **dispatch**: channel membership and global bus dispatch ([11e94c1](https://github.com/urmzd/humagen/commit/11e94c1a61af7f6445b570545bda3e90a13fc125))
- **turn**: Runtime interface, native saige implementation, Sandbox decorator ([4291f10](https://github.com/urmzd/humagen/commit/4291f106a015721e0321d87ce0e5c028f091635b))
- **event**: ingress bodies, byte-stable renderer and legacy decoders ([2618dc5](https://github.com/urmzd/humagen/commit/2618dc5adc86fb9c0a9ac0fe10ebce1a1d76617b))
- **event**: envelope, sealed body union, Unknown and catalog for run and control types ([fac5e74](https://github.com/urmzd/humagen/commit/fac5e74b611bfbd26b14f128bd20dae50857fd4b))
- **agentspec**: add tagged_only so hosts can keep an agent out of untagged messages (#46) ([eb7a9db](https://github.com/urmzd/humagen/commit/eb7a9db3c56f7ab6b1b3c34424a8dbe3b03685f9))
- **envelope**: a meta key starting with _ is the host's and is never sent to the model (#43) ([20d3a27](https://github.com/urmzd/humagen/commit/20d3a27bd75070773e9d665b34e8d93f1ff7dd76))
- **github**: raise a comment addressed to the app for its default agent (#42) ([804e2f5](https://github.com/urmzd/humagen/commit/804e2f5909a21fd7e7c88bef5070fbc33ee7352a))
- per-person tool credentials, and fiber run for local tests (#40) ([b588324](https://github.com/urmzd/humagen/commit/b588324b836e2550627b28de8b6250726ce3f2ac))
- **fiberapp**: public composition with extension points and a providers file (#36) ([6045747](https://github.com/urmzd/humagen/commit/6045747758eb6ea9c4a9f5a92749ad8352b9ea32))
- **mcptools**: expand mcp:<provider>[:write] grants from the tool provider store (#35) ([55b362a](https://github.com/urmzd/humagen/commit/55b362aa4107f814208cabeaf146f9704169ca21))
- **envelope**: carry the host's user id on a tag's actor (#34) ([5c5fc31](https://github.com/urmzd/humagen/commit/5c5fc315c6be07facb4ab7df191f84ee45edb681))
- model fallbacks with fallback conditions, and per-call approvals (#32) ([0d43e9b](https://github.com/urmzd/humagen/commit/0d43e9b72b0b6e948ce81681df7664e381b93db2))
- **agentrt**: OpenAI-compatible endpoints and MCP tool groups (#31) ([a108412](https://github.com/urmzd/humagen/commit/a108412281f8b769f7e8e5405d89ee131ecb4b6f))
- host-triggered compaction and tool arguments on tool events (#23) ([c8c1d6d](https://github.com/urmzd/humagen/commit/c8c1d6d7c7c19a3939d82e40b1d13857b48566b6))
- add MCP tool providers as versioned records (#21) ([1fa1854](https://github.com/urmzd/humagen/commit/1fa1854c8482e677ba46bc46de9da8a751fe4055))
- support host-constructed helper invocations (#17) ([799b78e](https://github.com/urmzd/humagen/commit/799b78eb12773400f08323034e9af2c754197124))
- report usage per run (#15) ([2a1db37](https://github.com/urmzd/humagen/commit/2a1db3783c89a52aff1f9f43d046498420e338bc))
- **cli**: reconcile threads through their connectors and bring files in from Slack, GitHub and Jira ([9b45449](https://github.com/urmzd/humagen/commit/9b454490ad91c2b1b127c8cead30fd1a030a153c))
- **jira**: bring issue attachments in through the site's content endpoint ([3bda4e4](https://github.com/urmzd/humagen/commit/3bda4e49c8d023be23c5ac55fe1bc08471108364))
- **github**: bring attached images and videos in as attachments and list conversations for reconciliation ([51fb1a8](https://github.com/urmzd/humagen/commit/51fb1a812fd3a20a7d17edfd904055009689a8e1))
- **slack**: bring message files in as attachments and list threads for reconciliation ([d23632e](https://github.com/urmzd/humagen/commit/d23632e4dd0a2db3b5e131c2bd9a0f0eaeb21274))
- **attachment**: recognise voice clips in audio-only WebM and M4A containers ([a675aea](https://github.com/urmzd/humagen/commit/a675aea9600dcc4c58a6d543c0feeab08a022de0))
- **intake**: collect a message's files under per-tag caps without failing the tag ([61b039e](https://github.com/urmzd/humagen/commit/61b039e4dcb38b1fc6f440adfee8e8c2738c6adc))
- **runtime**: journal a request's files and replay them byte-stably in later turns ([3b21ba2](https://github.com/urmzd/humagen/commit/3b21ba2520fe4ed2462f68fa546a1574f5d52bf4))
- **thread**: journal attachments with each entry and reconcile surface files once ([c6d6e97](https://github.com/urmzd/humagen/commit/c6d6e97b5d85fb98f2c2f6e8b1661e46c82a7f63))
- **attachment**: reference a file that could not be fetched so the agent is told ([1eb1ca5](https://github.com/urmzd/humagen/commit/1eb1ca5fd4c865e54cb0db9dacfbb9cb6984b490))
- **runtime**: stop a run mid-turn and keep its partial answer (#13) ([662e2d3](https://github.com/urmzd/humagen/commit/662e2d3253488972bf56c96f2dbfb83b32f6eb61))
- **cli**: run on Postgres and wire knowledge, threads, files and the author ([16e2109](https://github.com/urmzd/humagen/commit/16e210930f08d2a561f3bc0a21de26432d959460))
- **author**: add the registry as agent tools and a built-in author agent ([25620c7](https://github.com/urmzd/humagen/commit/25620c7e7ac447771fcf13e9441798be3ea57b71))
- **runtime**: pin thread history at accept and ground runs in knowledge ([0d4f383](https://github.com/urmzd/humagen/commit/0d4f3833eee7f71bb9c034d8e869d4471380b004))
- **agentrt**: add compaction, prompt caching, history and attachments ([fea5d5e](https://github.com/urmzd/humagen/commit/fea5d5e339dc256e54280d6478ce09660d507533))
- **envelope**: carry attachments on a tag ([bb01994](https://github.com/urmzd/humagen/commit/bb01994e0f48723c46b98f456156de890e9e2e11))
- **thread**: add a per-thread conversation journal ([fcc0852](https://github.com/urmzd/humagen/commit/fcc085255775ab500ccd5c227ce9cd47b58249b8))
- **attachment**: add a content-addressed blob store and file resolution ([325d43f](https://github.com/urmzd/humagen/commit/325d43fa7619a8b471cf32c869aa60a17b9e23af))
- **knowledge**: add hybrid retrieval with citations ([8dc8632](https://github.com/urmzd/humagen/commit/8dc8632010890c481dc66efed22857f416365c6b))
- **durable**: own the Postgres schema for duraturo's ledger and queue ([88462f1](https://github.com/urmzd/humagen/commit/88462f19bc017ecdd0b42ddceed0b2e615f91340))
- **github**: add structured reviews and PR collaboration ([8703695](https://github.com/urmzd/humagen/commit/8703695d17a61b405acccdc41b493319765836a5))
- **runtime**: let a host carry out a delegation for a turn that is a run (#6) ([f8d8fda](https://github.com/urmzd/humagen/commit/f8d8fda4a91831e5f6595fc9f456d724aee8c174))
- opentag, an agent platform and pub/sub bus for agents ([946ab53](https://github.com/urmzd/humagen/commit/946ab53d59c5314bfff7f6ca24c924882868c6ea))

### Bug Fixes

- **fiber/definition**: stamp records at microsecond precision so a Postgres read matches the write ([f53e5c9](https://github.com/urmzd/humagen/commit/f53e5c95359026c4615e9a4afd41111fe146c82a))
- keep call depth within the hop limit, read old journal rows and agent tables, bump the manifest to v2 ([69720bc](https://github.com/urmzd/humagen/commit/69720bce1079d7edfc64f80d20ee1492622e9b6f))
- **deps**: anchor v0.5.1, so a stop mid-claim never leases a run to nobody (#41) ([1132e20](https://github.com/urmzd/humagen/commit/1132e207d9528f79623404a26c7e0b4568e385a4))
- **mcptools**: inline local $defs references before converting a tool schema (#38) ([66b9f81](https://github.com/urmzd/humagen/commit/66b9f81753fddc19060065186ecd91a6797b2385))
- **agentrt**: drop a tool group that cannot be expanded, with a warning (#37) ([e9e07c0](https://github.com/urmzd/humagen/commit/e9e07c0a8b919577902740d04c1d9d212a050499))
- **agentrt**: drop a granted tool the deployment cannot resolve (#27) ([d1ff855](https://github.com/urmzd/humagen/commit/d1ff855e7611e6bc456a2db1e1649cb99b4a8a05))
- **runtime**: forward per-call approval through the host call tool (#33) ([aa7cc4c](https://github.com/urmzd/humagen/commit/aa7cc4c0105f67ebb981511d6ee8e60546edf21b))
- **mcptools**: confine the credential to its origin and harden tool schemas (#25) ([05018fd](https://github.com/urmzd/humagen/commit/05018fd418af3a8c3575a7136048b2d7d979b278))
- **runtime**: run in-turn sub-tasks on the delegating turn's node (#20) ([d3438db](https://github.com/urmzd/humagen/commit/d3438dbc0bf829e8d7f06f0554087b4be4d65327))
- **server**: preserve SSE slow-consumer reconnect hints ([9ead27b](https://github.com/urmzd/humagen/commit/9ead27bf7bca6575dda03a0288896be07e4bea0a))
- **server**: always announce draining before ending an SSE stream ([af472cb](https://github.com/urmzd/humagen/commit/af472cb9f3e91164af70d97bb57851bffc478e0b))
- remove a lost wakeup in the retry test and repair CI ([f77c845](https://github.com/urmzd/humagen/commit/f77c845cff664d8cb2319d34b41e2e2e6317fa91))

### Performance

- **bus**: wake only the Postgres readers a publish concerns (#45) ([b6ec42a](https://github.com/urmzd/humagen/commit/b6ec42aefc91e4d9775ebaf69aaf59fea228fa7f))

### Misc

- **web**: check the console with biome ([2e45361](https://github.com/urmzd/humagen/commit/2e45361a89e81123623ae1ed7f99315d29ec13e8))
- **legacy**: one home for old names after the fold ([0bf75af](https://github.com/urmzd/humagen/commit/0bf75af5553742252303bf08ff30de4fe5d7edd1))
- fold fiber into fiber/ with history ([1ae0e48](https://github.com/urmzd/humagen/commit/1ae0e48da768735bdf74117df5a61e8ba89dd874))
- add fiber at fiber/ with history ([df6c670](https://github.com/urmzd/humagen/commit/df6c67062272c2c06836a576310b0b6cb93153a4))
- **turn**: pin saige imports in runtime tests to the existing files ([9e1862a](https://github.com/urmzd/humagen/commit/9e1862aaa01faef320f64afc32a4ef5526012975))
- **runtime**: prove an inline sub-task's workspace and spawn confinement (#22) ([52b5e2a](https://github.com/urmzd/humagen/commit/52b5e2ae571e3b800547749d21e713fe4dfc975b))
- **deps**: upgrade saige to v0.37.0 (#18) ([97e11d3](https://github.com/urmzd/humagen/commit/97e11d3480368d539868cbd5394550438d486362))
- **slack**: merge declaration flagged by staticcheck ([924bba0](https://github.com/urmzd/humagen/commit/924bba0aa75c16a27730ec57ec14db1a859ad8ef))
- **github**: gofmt attachment tests ([2fbda0f](https://github.com/urmzd/humagen/commit/2fbda0f6f3e4342ec57927b4c9df24d72e3dc9d6))
- run the Postgres and Redis backends in an integration job ([09a3efb](https://github.com/urmzd/humagen/commit/09a3efb335892489982abb73f021eae603b77fb9))
- add a Dockerfile and a compose stack with an end-to-end check ([1e3476e](https://github.com/urmzd/humagen/commit/1e3476ef7d7650c3346968b855127bef3a373d12))
- **deps**: bump saige to v0.34.0 (#5) ([f4be729](https://github.com/urmzd/humagen/commit/f4be729ab035a22755b2c8e9256c5250a9b53872))
- **release**: read the sr-releaser key from the release environment (#4) ([0d66d8f](https://github.com/urmzd/humagen/commit/0d66d8f70c16a5424c61d6c45e7f5244589aa8e4))
- **deps**: bump saige to v0.24.0 (#3) ([e6cb2ac](https://github.com/urmzd/humagen/commit/e6cb2acf04073e84cb17acf467dcf0b1b6f16624))
- upgrade urmzd/sr action to v9 (#2) ([d16c081](https://github.com/urmzd/humagen/commit/d16c08121bac6ac35b91d33972bad917b6ec98ef))
- update model examples and bump saige to v0.22.0 (#1) ([5a43167](https://github.com/urmzd/humagen/commit/5a4316754d95c652ff3610b0d805339085a11ad7))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.31.2...v0.32.0)


## 0.31.2 (2026-10-28)

### Bug Fixes

- **web**: make message actions icon-only and drop the duplicate Share in People & Access (#105) ([d81109d](https://github.com/urmzd/humagen/commit/d81109dea5f458a017c730135860b421c4d79589))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.31.1...v0.31.2)


## 0.31.1 (2026-10-28)

### Bug Fixes

- **github**: render reviews the agent wraps in a json code fence (#104) ([9e54c40](https://github.com/urmzd/humagen/commit/9e54c4054d06e48d097f730e2b5be98e9125e7c8))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.31.0...v0.31.1)


## 0.30.0 (2026-10-27)

### Features

- **web**: consolidate conversation archive and deletion review (#101) ([73c093c](https://github.com/urmzd/humagen/commit/73c093c772c42f5531a76c7e33939b4b012f72c0))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.29.0...v0.30.0)


## 0.29.0 (2026-10-27)

### Breaking

- Postgres is the event bus; the Redis bus is removed (#100) ([fe2294a](https://github.com/urmzd/humagen/commit/fe2294a7a087e8a5936ea385d4f15a05835c08de))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.28.0...v0.29.0)


## 0.27.3 (2026-10-27)

### Bug Fixes

- **web**: group composer context into scoped menus (#97) ([b7e4651](https://github.com/urmzd/humagen/commit/b7e465194a3c751b37ff59fa26b5280fa7c660b2))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.27.2...v0.27.3)


## 0.27.2 (2026-10-27)

### Bug Fixes

- **web**: make notifications a top-level destination (#91) ([fb83c58](https://github.com/urmzd/humagen/commit/fb83c5868b792ac5e7d30d4459dbe7f57dd184e2))
- **web**: unify action icons and inbox recovery spacing (#90) ([a51a201](https://github.com/urmzd/humagen/commit/a51a201d1c499f2dc7503c16eeb480474b88b0a7))
- **platform**: isolate project fork and conversation locks (#94) ([a1e49df](https://github.com/urmzd/humagen/commit/a1e49dfdb24dee2b72c55ca047bfddee3c77e7ce))

### Misc

- verify native XLSX and PPTX agent artifacts (#89) ([bd9b687](https://github.com/urmzd/humagen/commit/bd9b6872da2a28221b5f6078ec6aab0968ccf1ea))
- **sources**: add opt-in live provider smoke tests (#87) ([393efbb](https://github.com/urmzd/humagen/commit/393efbb1388e725e739cf41533b3d52381edc57d))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.27.1...v0.27.2)


## 0.27.1 (2026-10-26)

### Bug Fixes

- **web**: keep agent discovery inside the composer picker (#86) ([6a2339e](https://github.com/urmzd/humagen/commit/6a2339e54b86bb5c79d17600a81f504e34e99053))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.27.0...v0.27.1)


## 0.27.0 (2026-10-26)

### Features

- **workspace**: complete native outcomes and the integrated console (#85) ([55167c7](https://github.com/urmzd/humagen/commit/55167c7236218e238e7d170970d94ffe915352d5))
- **notifications**: deliver opted-in updates through native SMTP (#84) ([3db89b0](https://github.com/urmzd/humagen/commit/3db89b039e275e72d715cd261b3847ee492a952e))
- **usage**: add recorded assistance and daily analytics (#83) ([2721236](https://github.com/urmzd/humagen/commit/2721236794905cdebe85771a026c6a3ea16c9861))
- **conversations**: add confined forks and project workflows (#82) ([cacc710](https://github.com/urmzd/humagen/commit/cacc710f8af6bc2512b1cbd2d8b578ea88490ec5))
- **library**: add scoped skills and personal memory controls (#81) ([6596685](https://github.com/urmzd/humagen/commit/6596685815b2cb79336344f047a914b194397d36))
- **sources**: complete lifecycle controls and trusted citation provenance (#80) ([d708cdf](https://github.com/urmzd/humagen/commit/d708cdf691b592b0f71f3848afde961a0e6210eb))
- **agents**: add durable assignments and scoped action controls (#79) ([d8bb10c](https://github.com/urmzd/humagen/commit/d8bb10ccf19fb6a352988ec031284e7d20de0fc5))
- **workspace**: add Company homes and reviewed publication (#78) ([6a18f1c](https://github.com/urmzd/humagen/commit/6a18f1c087c9daa8ba355296a4f6a6266ac0b772))
- add shared audit and source foundations for the workspace (#76) ([5fcde6c](https://github.com/urmzd/humagen/commit/5fcde6cf1d9bbb8dc6c3f6631bb7e0c503c9ef4f))

### Bug Fixes

- **web**: keep message actions and agent badges consistent (#75) ([ba38dd7](https://github.com/urmzd/humagen/commit/ba38dd75d319b7723347fa0dc3544f5d4dbbcab8))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.26.4...v0.27.0)


## 0.26.4 (2026-10-26)

### Bug Fixes

- **web**: keep sidebar and page scrolling independent (#74) ([db28474](https://github.com/urmzd/humagen/commit/db2847401506ee32df62f46c4d973c855506d322))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.26.3...v0.26.4)


## 0.26.3 (2026-10-26)

### Bug Fixes

- **web**: use the Humagen mark for icons and title each browser tab (#73) ([ae504dc](https://github.com/urmzd/humagen/commit/ae504dc86adb5bda5a02b061095cf5d57e9f73a7))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.26.2...v0.26.3)


## 0.26.2 (2026-10-26)

### Bug Fixes

- rename built-in agents and restore conversation links (#71) ([a17548a](https://github.com/urmzd/humagen/commit/a17548a89ec617ae90e4501f64eb6764c777da1a))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.26.1...v0.26.2)


## 0.26.1 (2026-10-26)

### Bug Fixes

- **helm**: omit retained null store values during upgrades ([b959877](https://github.com/urmzd/humagen/commit/b9598770359371b55b2fc8ae8d80c9b0d402a12a))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.26.0...v0.26.1)


## 0.26.0 (2026-10-26)

### Features

- **web**: connect Slack and GitHub and refine conversation UI ([a4db3bc](https://github.com/urmzd/humagen/commit/a4db3bc51b73e38c35407cf4b5401bef4c3bf6e6))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.25.1...v0.26.0)


## 0.25.1 (2026-10-25)

### Bug Fixes

- answer a question in the moment it is asked (#68) ([3968fc8](https://github.com/urmzd/humagen/commit/3968fc81b9f08d71d89613f9a93220bf1b4fbf8f))

### Misc

- publish the design boards to GitHub Pages (#67) ([d62b043](https://github.com/urmzd/humagen/commit/d62b0436371f252e5b76dc977d11a6458edb84b8))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.25.0...v0.25.1)


## 0.25.0 (2026-10-25)

### Features

- **web**: add the Humagen project conversation workspace (#64) ([8cd34d5](https://github.com/urmzd/humagen/commit/8cd34d5715f0f3e8091feb99e99a154cdbd5e2ee))

### Misc

- verify installs and Helm/Flux rename upgrades ([cbb95fc](https://github.com/urmzd/humagen/commit/cbb95fcc4e16253abe933e66f573eb6b36b4daec))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.24.0...v0.25.0)


## 0.24.0 (2026-10-23)

### Features

- **projects**: preview composed turn context layers ([b3eda7a](https://github.com/urmzd/humagen/commit/b3eda7a12a3a2a970cebb97e30c2ce3473a45b1e))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.23.0...v0.24.0)


## 0.23.0 (2026-10-23)

### Features

- record break-glass reads ([b904b1c](https://github.com/urmzd/humagen/commit/b904b1c296f9488adebd7f44ea6de261aac5e97a))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.22.0...v0.23.0)


## 0.22.0 (2026-10-23)

### Features

- show project agents and unread updates ([9e5e661](https://github.com/urmzd/humagen/commit/9e5e661c8300305346e72682027656e8d0d0e5e8))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.21.2...v0.22.0)


## 0.21.2 (2026-10-23)

### Bug Fixes

- **console**: reserve stable AgentMark colors ([91c05b3](https://github.com/urmzd/humagen/commit/91c05b3c1d3fc101be2d26fbc65b06f435729055))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.21.1...v0.21.2)


## 0.21.1 (2026-10-22)

### Bug Fixes

- keep installs from before the rename working, from the install-safety review (#54) ([f713fa2](https://github.com/urmzd/humagen/commit/f713fa2f380534ca32372a42a26f1990d330d4c2))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.21.0...v0.21.1)


## 0.21.0 (2026-10-22)

### Breaking

- rename warp to Humagen, with old names still read (#53) ([54edc7b](https://github.com/urmzd/humagen/commit/54edc7bad20bacda5e9dd7e899b5f864e267e23c))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.20.2...v0.21.0)


## 0.20.2 (2026-10-22)

### Bug Fixes

- **console**: say who Share did not add in the console's words (#52) ([2553e22](https://github.com/urmzd/warp/commit/2553e227fa98d933de4afc33db7deaf7d50496c4))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.20.1...v0.20.2)


## 0.20.1 (2026-10-22)

### Bug Fixes

- **console**: Share and Move for a conversation's starter, Share tells the people it adds, the + menu on a conversation of runs (#51) ([26c6416](https://github.com/urmzd/warp/commit/26c6416d5a9d209dc5fc6eb6641091fa10595890))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.20.0...v0.20.1)


## 0.20.0 (2026-10-22)

### Features

- **console**: chat by mention, private conversations with Share, the + menu with Best Fit and All of Them, skills, Conversations, Home boxes and UI patterns (#50) ([909edd3](https://github.com/urmzd/warp/commit/909edd31a0e648ed8f17d69da282b7f214cafb69))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.19.0...v0.20.0)


## 0.19.0 (2026-10-22)

### Features

- **projects**: edit a project's knowledge in the console, guard source deletes, keep source names from the model (#49) ([7509a18](https://github.com/urmzd/warp/commit/7509a1837e894c21645eb67ea26ab1c51e5cbbcc))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.18.0...v0.19.0)


## 0.18.0 (2026-10-22)

### Features

- **projects**: Slack catch-up, Home's Needs you and Your projects, phase 9 knowledge and inbox retention, agent names confined (#48) ([22d1d2a](https://github.com/urmzd/warp/commit/22d1d2acee3a2731fababfb616799c57728150d9))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.17.0...v0.18.0)


## 0.17.0 (2026-10-22)

### Features

- **slack**: add uploads, skill commands, canvases and feedback collection (#47) ([37857f8](https://github.com/urmzd/warp/commit/37857f8cfb2a65d855823b687236a367f8b46bdd))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.16.0...v0.17.0)


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
