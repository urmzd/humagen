# @humagen/site

The public website at https://withhumagen.com: the product page and the deploy guide. Astro, static HTML, no client JavaScript.

| Path | What it is |
|------|------------|
| `/` | The product page |
| `/deploy` | The recommended deployment, step by step, then the alternatives and current limits |
| `/llms.txt` | The same content for agents, generated from the content module |
| `/sitemap.xml`, `/robots.txt` | For crawlers |

## Develop

```sh
pnpm install                               # once, at the repository root
pnpm --filter @humagen/site dev            # http://127.0.0.1:4321
pnpm --filter @humagen/site build          # writes apps/site/dist
pnpm --filter @humagen/site preview        # serves dist
pnpm --filter @humagen/site test           # content checks
pnpm --filter @humagen/site typecheck
```

## Where things live

- `src/content/site.ts`: every fact and every line of copy. Pages, metadata and `llms.txt` read it, so change copy there, not in a page. Inline code is written in backticks and bold in `**`, rendered by `src/lib/inline.ts`.
- `src/lib/llms.ts`: builds `llms.txt` from the content module.
- `src/styles/global.css`: imports the console's theme and fonts, and adds the few rules a document page needs.
- `src/assets/console/`: real console screens, copied from the repository's `showcase/` (captured by teasr from a local preview with seeded data). Astro converts them to WebP at build.

Facts come from the repository: the README, `docs/guides/deploy.md`, `docs/reference/spec.md` and the status table in `docs/architecture/overview.md`. When one of those changes, change the content module with it. Do not add a claim the repository does not support.

## Design

The site wears the console's design system, imported rather than copied:

- **Tokens and utilities:** `@humagen/render-web/theme.css` through Tailwind, so the pages use the console's classes (`bg-aui-bg`, `text-aui-muted`, `border-aui-border`, `rounded-aui-lg`). Dark mode is theme.css's own: the system preference, or `data-theme`.
- **Type:** `@humagen/render-web/fonts.css`: IBM Plex Sans for text, IBM Plex Mono for code, at the console's `text-aui-*` scale. Both are under the SIL Open Font License 1.1 (`public/licenses/`).
- **Pieces:** `src/lib/console.ts` renders the console's icons (`@humagen/render-web/icons`), the Humagen mark (`@humagen/web/HumagenMark`), agent initials (`@humagen/web/AgentOrb`) and button classes (`@humagen/render-web/ui/button`) to static markup at build time. React runs only during the build.
- **Icons:** `public/favicon.svg`, `favicon-32.png`, `icon.svg` and `apple-touch-icon.png` are copies of `apps/web/public`; a test fails if they drift.
- One primary button per page, in the accent. Everything else is an outline button or a text link. Copy follows `packages/components/src/copy.ts`: Title Case buttons, sentence case elsewhere.

## Deploy

`.github/workflows/site.yml` builds the site on pull requests that touch it. On a push to `main` it uploads `dist` to the `withhumagen.com` S3 bucket and invalidates its CloudFront distribution, once the repository has the `AWS_ROLE_ARN` and `AWS_REGION` secrets. Until then the deploy job is skipped. Pages build to `dist/<page>/index.html`; CloudFront rewrites `/deploy` to `/deploy/index.html` and serves `404.html` for a missing page.
