# Visual acceptance evidence

These are actual captured pixels. Codex opened and inspected every listed image. Only public synthetic fixtures, public documentation, and a credential-free configuration check are included.

## Media transport, PR330

Same capture.png and draft, same canonical fixture, light/dark and both widths. Before:216ee14f5 rejects PNG. After:eca9872fe accepts it. The historical capture harness adds the scenario only; application code is unchanged.

| Theme and viewport | Before | After |
|---|---|---|
| light, 390x844 | ![](media-paired/before/light/390x844/new.png) | ![](media-paired/after/light/390x844/composer-attachments.png) |
| light, 1280x800 | ![](media-paired/before/light/1280x800/new.png) | ![](media-paired/after/light/1280x800/composer-attachments.png) |
| dark, 390x844 | ![](media-paired/before/dark/390x844/new.png) | ![](media-paired/after/dark/390x844/composer-attachments.png) |
| dark, 1280x800 | ![](media-paired/before/dark/1280x800/new.png) | ![](media-paired/after/dark/1280x800/composer-attachments.png) |

## Browser connector, PR334

Actual Playwright MCP0.0.85 through Humagen RichTool. Within each pair the same page changes its heading after a reference-based click; selected CSV bytes and PNG pixels are independently checked. The page itself verifies light/dark appearance.

| Theme and viewport | Before action | After action |
|---|---|---|
| light, 390x844 | ![](browser-paired/light/390x844/before.png) | ![](browser-paired/light/390x844/after.png) |
| light, 1280x800 | ![](browser-paired/light/1280x800/before.png) | ![](browser-paired/light/1280x800/after.png) |
| dark, 390x844 | ![](browser-paired/dark/390x844/before.png) | ![](browser-paired/dark/390x844/after.png) |
| dark, 1280x800 | ![](browser-paired/dark/1280x800/before.png) | ![](browser-paired/dark/1280x800/after.png) |

## Real configuration command

Same command and100x18 terminal size, run in an actual PTY. Before has no shipped browser config. After validates it and explicitly reports the unset model API key.

| Before | After |
|---|---|
| ![](browser-terminal/browser-config-before.png) | ![](browser-terminal/browser-config-after.png) |

## Rendered documentation

Same documentation section and viewport, with scroll settled. The mobile index table has pre-existing horizontal overflow in both states; its rightmost column was not fully visible.

| Theme and viewport | Before index | After index |
|---|---|---|
| light, 390x844 | ![](browser-doc-index/docs-index-section-before-light-390x844.png) | ![](browser-doc-index/docs-index-section-after-light-390x844.png) |
| light, 1280x800 | ![](browser-doc-index/docs-index-section-before-light-1280x800.png) | ![](browser-doc-index/docs-index-section-after-light-1280x800.png) |
| dark, 390x844 | ![](browser-doc-index/docs-index-section-before-dark-390x844.png) | ![](browser-doc-index/docs-index-section-after-dark-390x844.png) |
| dark, 1280x800 | ![](browser-doc-index/docs-index-section-before-dark-1280x800.png) | ![](browser-doc-index/docs-index-section-after-dark-1280x800.png) |

The browser guide is new, so no earlier guide existed to capture. Its first viewport and full page were opened in each theme and width. Fenced commands scroll horizontally on a phone.

| Theme and viewport | First viewport | Full guide |
|---|---|---|
| light, 390x844 | ![](browser-guide/browser-guide-top-light-390x844.png) | [Full guide](browser-guide/browser-guide-light-390x844.png) |
| light, 1280x800 | ![](browser-guide/browser-guide-top-light-1280x800.png) | [Full guide](browser-guide/browser-guide-light-1280x800.png) |
| dark, 390x844 | ![](browser-guide/browser-guide-top-dark-390x844.png) | [Full guide](browser-guide/browser-guide-dark-390x844.png) |
| dark, 1280x800 | ![](browser-guide/browser-guide-top-dark-1280x800.png) | [Full guide](browser-guide/browser-guide-dark-1280x800.png) |

## Unseen states

- Signed-in portal/extension handoff and a real model interpreting browser screenshots remain unverified.
- Native desktop capture/actions remain blocked by the locked Mac GUI.
- Documentation links were not followed, and mobile tables/code blocks were not panned horizontally.
- This is visual/connector evidence, not the final all-main end-to-end audit.

## Posted upload references

- [upload-display/before/light/390x844/session-uploads.png](upload-display/before/light/390x844/session-uploads.png)
- [upload-display/before/light/1280x800/session-uploads.png](upload-display/before/light/1280x800/session-uploads.png)
- [upload-display/before/dark/390x844/session-uploads.png](upload-display/before/dark/390x844/session-uploads.png)
- [upload-display/before/dark/1280x800/session-uploads.png](upload-display/before/dark/1280x800/session-uploads.png)
- [upload-display/after/light/390x844/session-uploads.png](upload-display/after/light/390x844/session-uploads.png)
- [upload-display/after/light/1280x800/session-uploads.png](upload-display/after/light/1280x800/session-uploads.png)
- [upload-display/after/dark/390x844/session-uploads.png](upload-display/after/dark/390x844/session-uploads.png)
- [upload-display/after/dark/1280x800/session-uploads.png](upload-display/after/dark/1280x800/session-uploads.png)
- [upload-display/after/light/390x844/session-uploads.full.png](upload-display/after/light/390x844/session-uploads.full.png)
- [upload-display/after/dark/390x844/session-uploads.full.png](upload-display/after/dark/390x844/session-uploads.full.png)

## Upload display documentation pairs

| Section and viewport | Before | After |
|---|---|---|
| architecture, light, 390x844 | [Before](upload-display-docs/captures/architecture-before-light-390x844.png) | [After](upload-display-docs/captures/architecture-after-light-390x844.png) |
| architecture, light, 1280x800 | [Before](upload-display-docs/captures/architecture-before-light-1280x800.png) | [After](upload-display-docs/captures/architecture-after-light-1280x800.png) |
| architecture, dark, 390x844 | [Before](upload-display-docs/captures/architecture-before-dark-390x844.png) | [After](upload-display-docs/captures/architecture-after-dark-390x844.png) |
| architecture, dark, 1280x800 | [Before](upload-display-docs/captures/architecture-before-dark-1280x800.png) | [After](upload-display-docs/captures/architecture-after-dark-1280x800.png) |
| requirements, light, 390x844 | [Before](upload-display-docs/captures/requirements-before-light-390x844.png) | [After](upload-display-docs/captures/requirements-after-light-390x844.png) |
| requirements, light, 1280x800 | [Before](upload-display-docs/captures/requirements-before-light-1280x800.png) | [After](upload-display-docs/captures/requirements-after-light-1280x800.png) |
| requirements, dark, 390x844 | [Before](upload-display-docs/captures/requirements-before-dark-390x844.png) | [After](upload-display-docs/captures/requirements-after-dark-390x844.png) |
| requirements, dark, 1280x800 | [Before](upload-display-docs/captures/requirements-before-dark-1280x800.png) | [After](upload-display-docs/captures/requirements-after-dark-1280x800.png) |
| screen-guide, light, 390x844 | [Before](upload-display-docs/captures/screen-guide-before-light-390x844.png) | [After](upload-display-docs/captures/screen-guide-after-light-390x844.png) |
| screen-guide, light, 1280x800 | [Before](upload-display-docs/captures/screen-guide-before-light-1280x800.png) | [After](upload-display-docs/captures/screen-guide-after-light-1280x800.png) |
| screen-guide, dark, 390x844 | [Before](upload-display-docs/captures/screen-guide-before-dark-390x844.png) | [After](upload-display-docs/captures/screen-guide-after-dark-390x844.png) |
| screen-guide, dark, 1280x800 | [Before](upload-display-docs/captures/screen-guide-before-dark-1280x800.png) | [After](upload-display-docs/captures/screen-guide-after-dark-1280x800.png) |

## Worked touch target

- [worked-target/before/light/390x844/session.png](worked-target/before/light/390x844/session.png)
- [worked-target/before/light/1280x800/session.png](worked-target/before/light/1280x800/session.png)
- [worked-target/before/dark/390x844/session.png](worked-target/before/dark/390x844/session.png)
- [worked-target/before/dark/1280x800/session.png](worked-target/before/dark/1280x800/session.png)
- [worked-target/after/light/390x844/session.png](worked-target/after/light/390x844/session.png)
- [worked-target/after/light/1280x800/session.png](worked-target/after/light/1280x800/session.png)
- [worked-target/after/dark/390x844/session.png](worked-target/after/dark/390x844/session.png)
- [worked-target/after/dark/1280x800/session.png](worked-target/after/dark/1280x800/session.png)
- [worked-target-docs/captures/requirements-after-dark-1280x800.png](worked-target-docs/captures/requirements-after-dark-1280x800.png)
- [worked-target-docs/captures/requirements-after-dark-390x844.png](worked-target-docs/captures/requirements-after-dark-390x844.png)
- [worked-target-docs/captures/requirements-after-light-1280x800.png](worked-target-docs/captures/requirements-after-light-1280x800.png)
- [worked-target-docs/captures/requirements-after-light-390x844.png](worked-target-docs/captures/requirements-after-light-390x844.png)
- [worked-target-docs/captures/requirements-before-dark-1280x800.png](worked-target-docs/captures/requirements-before-dark-1280x800.png)
- [worked-target-docs/captures/requirements-before-dark-390x844.png](worked-target-docs/captures/requirements-before-dark-390x844.png)
- [worked-target-docs/captures/requirements-before-light-1280x800.png](worked-target-docs/captures/requirements-before-light-1280x800.png)
- [worked-target-docs/captures/requirements-before-light-390x844.png](worked-target-docs/captures/requirements-before-light-390x844.png)
- [worked-target-docs/captures/status-after-dark-1280x800.png](worked-target-docs/captures/status-after-dark-1280x800.png)
- [worked-target-docs/captures/status-after-dark-390x844.png](worked-target-docs/captures/status-after-dark-390x844.png)
- [worked-target-docs/captures/status-after-light-1280x800.png](worked-target-docs/captures/status-after-light-1280x800.png)
- [worked-target-docs/captures/status-after-light-390x844.png](worked-target-docs/captures/status-after-light-390x844.png)
- [worked-target-docs/captures/status-before-dark-1280x800.png](worked-target-docs/captures/status-before-dark-1280x800.png)
- [worked-target-docs/captures/status-before-dark-390x844.png](worked-target-docs/captures/status-before-dark-390x844.png)
- [worked-target-docs/captures/status-before-light-1280x800.png](worked-target-docs/captures/status-before-light-1280x800.png)
- [worked-target-docs/captures/status-before-light-390x844.png](worked-target-docs/captures/status-before-light-390x844.png)

## Inline-code wrapping

- [inline-code/before/light/390x844/session-inline-code.png](inline-code/before/light/390x844/session-inline-code.png)
- [inline-code/before/light/1280x800/session-inline-code.png](inline-code/before/light/1280x800/session-inline-code.png)
- [inline-code/before/dark/390x844/session-inline-code.png](inline-code/before/dark/390x844/session-inline-code.png)
- [inline-code/before/dark/1280x800/session-inline-code.png](inline-code/before/dark/1280x800/session-inline-code.png)
- [inline-code/after/light/390x844/session-inline-code.png](inline-code/after/light/390x844/session-inline-code.png)
- [inline-code/after/light/1280x800/session-inline-code.png](inline-code/after/light/1280x800/session-inline-code.png)
- [inline-code/after/dark/390x844/session-inline-code.png](inline-code/after/dark/390x844/session-inline-code.png)
- [inline-code/after/dark/1280x800/session-inline-code.png](inline-code/after/dark/1280x800/session-inline-code.png)
- [inline-code-docs/captures/requirements-after-dark-1280x800.png](inline-code-docs/captures/requirements-after-dark-1280x800.png)
- [inline-code-docs/captures/requirements-after-dark-390x844.png](inline-code-docs/captures/requirements-after-dark-390x844.png)
- [inline-code-docs/captures/requirements-after-light-1280x800.png](inline-code-docs/captures/requirements-after-light-1280x800.png)
- [inline-code-docs/captures/requirements-after-light-390x844.png](inline-code-docs/captures/requirements-after-light-390x844.png)
- [inline-code-docs/captures/requirements-before-dark-1280x800.png](inline-code-docs/captures/requirements-before-dark-1280x800.png)
- [inline-code-docs/captures/requirements-before-dark-390x844.png](inline-code-docs/captures/requirements-before-dark-390x844.png)
- [inline-code-docs/captures/requirements-before-light-1280x800.png](inline-code-docs/captures/requirements-before-light-1280x800.png)
- [inline-code-docs/captures/requirements-before-light-390x844.png](inline-code-docs/captures/requirements-before-light-390x844.png)
- [inline-code-docs/captures/screen-guide-after-dark-1280x800.png](inline-code-docs/captures/screen-guide-after-dark-1280x800.png)
- [inline-code-docs/captures/screen-guide-after-dark-390x844.png](inline-code-docs/captures/screen-guide-after-dark-390x844.png)
- [inline-code-docs/captures/screen-guide-after-light-1280x800.png](inline-code-docs/captures/screen-guide-after-light-1280x800.png)
- [inline-code-docs/captures/screen-guide-after-light-390x844.png](inline-code-docs/captures/screen-guide-after-light-390x844.png)
- [inline-code-docs/captures/screen-guide-before-dark-1280x800.png](inline-code-docs/captures/screen-guide-before-dark-1280x800.png)
- [inline-code-docs/captures/screen-guide-before-dark-390x844.png](inline-code-docs/captures/screen-guide-before-dark-390x844.png)
- [inline-code-docs/captures/screen-guide-before-light-1280x800.png](inline-code-docs/captures/screen-guide-before-light-1280x800.png)
- [inline-code-docs/captures/screen-guide-before-light-390x844.png](inline-code-docs/captures/screen-guide-before-light-390x844.png)
- [inline-code-docs/captures/status-after-dark-1280x800.png](inline-code-docs/captures/status-after-dark-1280x800.png)
- [inline-code-docs/captures/status-after-dark-390x844.png](inline-code-docs/captures/status-after-dark-390x844.png)
- [inline-code-docs/captures/status-after-light-1280x800.png](inline-code-docs/captures/status-after-light-1280x800.png)
- [inline-code-docs/captures/status-after-light-390x844.png](inline-code-docs/captures/status-after-light-390x844.png)
- [inline-code-docs/captures/status-before-dark-1280x800.png](inline-code-docs/captures/status-before-dark-1280x800.png)
- [inline-code-docs/captures/status-before-dark-390x844.png](inline-code-docs/captures/status-before-dark-390x844.png)
- [inline-code-docs/captures/status-before-light-1280x800.png](inline-code-docs/captures/status-before-light-1280x800.png)
- [inline-code-docs/captures/status-before-light-390x844.png](inline-code-docs/captures/status-before-light-390x844.png)


Desktop connector and assignment guide, inspected by Codex. Native app interaction remains blocked by the locked Mac.

- [desktop-guide-after-dark-1280x800](desktop/desktop-guide-after-dark-1280x800.png)
- [desktop-guide-after-dark-390x844](desktop/desktop-guide-after-dark-390x844.png)
- [desktop-guide-after-light-1280x800](desktop/desktop-guide-after-light-1280x800.png)
- [desktop-guide-after-light-390x844](desktop/desktop-guide-after-light-390x844.png)
- [desktop-index-after-dark-1280x800](desktop/desktop-index-after-dark-1280x800.png)
- [desktop-index-after-dark-390x844](desktop/desktop-index-after-dark-390x844.png)
- [desktop-index-after-light-1280x800](desktop/desktop-index-after-light-1280x800.png)
- [desktop-index-after-light-390x844](desktop/desktop-index-after-light-390x844.png)
- [desktop-index-before-dark-1280x800](desktop/desktop-index-before-dark-1280x800.png)
- [desktop-index-before-dark-390x844](desktop/desktop-index-before-dark-390x844.png)
- [desktop-index-before-light-1280x800](desktop/desktop-index-before-light-1280x800.png)
- [desktop-index-before-light-390x844](desktop/desktop-index-before-light-390x844.png)
- [desktop-native-after](desktop/desktop-native-after.png)
- [desktop-native-before](desktop/desktop-native-before.png)
- [desktop-native-blocker](desktop/desktop-native-blocker.png)
- [desktop-requirements-after-dark-1280x800](desktop/desktop-requirements-after-dark-1280x800.png)
- [desktop-requirements-after-dark-390x844](desktop/desktop-requirements-after-dark-390x844.png)
- [desktop-requirements-after-light-1280x800](desktop/desktop-requirements-after-light-1280x800.png)
- [desktop-requirements-after-light-390x844](desktop/desktop-requirements-after-light-390x844.png)
- [desktop-requirements-before-dark-1280x800](desktop/desktop-requirements-before-dark-1280x800.png)
- [desktop-requirements-before-dark-390x844](desktop/desktop-requirements-before-dark-390x844.png)
- [desktop-requirements-before-light-1280x800](desktop/desktop-requirements-before-light-1280x800.png)
- [desktop-requirements-before-light-390x844](desktop/desktop-requirements-before-light-390x844.png)
- [desktop-status-after-dark-1280x800](desktop/desktop-status-after-dark-1280x800.png)
- [desktop-status-after-dark-390x844](desktop/desktop-status-after-dark-390x844.png)
- [desktop-status-after-light-1280x800](desktop/desktop-status-after-light-1280x800.png)
- [desktop-status-after-light-390x844](desktop/desktop-status-after-light-390x844.png)
- [desktop-status-before-dark-1280x800](desktop/desktop-status-before-dark-1280x800.png)
- [desktop-status-before-dark-390x844](desktop/desktop-status-before-dark-390x844.png)
- [desktop-status-before-light-1280x800](desktop/desktop-status-before-light-1280x800.png)
- [desktop-status-before-light-390x844](desktop/desktop-status-before-light-390x844.png)
- [desktop-subscription-after](desktop/desktop-subscription-after.png)
- [desktop-subscription-before](desktop/desktop-subscription-before.png)
- [assignment-guide-after-dark-1280x800](assignment-guide/assignment-guide-after-dark-1280x800.png)
- [assignment-guide-after-dark-390x844](assignment-guide/assignment-guide-after-dark-390x844.png)
- [assignment-guide-after-light-1280x800](assignment-guide/assignment-guide-after-light-1280x800.png)
- [assignment-guide-after-light-390x844](assignment-guide/assignment-guide-after-light-390x844.png)
- [assignment-guide-before-dark-1280x800](assignment-guide/assignment-guide-before-dark-1280x800.png)
- [assignment-guide-before-dark-390x844](assignment-guide/assignment-guide-before-dark-390x844.png)
- [assignment-guide-before-light-1280x800](assignment-guide/assignment-guide-before-light-1280x800.png)
- [assignment-guide-before-light-390x844](assignment-guide/assignment-guide-before-light-390x844.png)


Public-source research, all23 images inspected by Codex:

- [web-actual-acceptance](public-web/web-actual-acceptance.png)
- [web-config-after](public-web/web-config-after.png)
- [web-config-before](public-web/web-config-before.png)
- [web-guide-after-dark-1280x800](public-web/web-guide-after-dark-1280x800.png)
- [web-guide-after-dark-390x844](public-web/web-guide-after-dark-390x844.png)
- [web-guide-after-light-1280x800](public-web/web-guide-after-light-1280x800.png)
- [web-guide-after-light-390x844](public-web/web-guide-after-light-390x844.png)
- [web-requirements-after-dark-1280x800](public-web/web-requirements-after-dark-1280x800.png)
- [web-requirements-after-dark-390x844](public-web/web-requirements-after-dark-390x844.png)
- [web-requirements-after-light-1280x800](public-web/web-requirements-after-light-1280x800.png)
- [web-requirements-after-light-390x844](public-web/web-requirements-after-light-390x844.png)
- [web-requirements-before-dark-1280x800](public-web/web-requirements-before-dark-1280x800.png)
- [web-requirements-before-dark-390x844](public-web/web-requirements-before-dark-390x844.png)
- [web-requirements-before-light-1280x800](public-web/web-requirements-before-light-1280x800.png)
- [web-requirements-before-light-390x844](public-web/web-requirements-before-light-390x844.png)
- [web-status-after-dark-1280x800](public-web/web-status-after-dark-1280x800.png)
- [web-status-after-dark-390x844](public-web/web-status-after-dark-390x844.png)
- [web-status-after-light-1280x800](public-web/web-status-after-light-1280x800.png)
- [web-status-after-light-390x844](public-web/web-status-after-light-390x844.png)
- [web-status-before-dark-1280x800](public-web/web-status-before-dark-1280x800.png)
- [web-status-before-dark-390x844](public-web/web-status-before-dark-390x844.png)
- [web-status-before-light-1280x800](public-web/web-status-before-light-1280x800.png)
- [web-status-before-light-390x844](public-web/web-status-before-light-390x844.png)
