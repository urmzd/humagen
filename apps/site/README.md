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
- `src/styles/global.css`: the design tokens (color, type, space, radius, focus) for light and dark, then shared styles. Page-specific styles sit in each page.
- `src/assets/console/`: real console screens, copied from the repository's `showcase/` (captured by teasr from a local preview with seeded data). Astro converts them to WebP at build.

Facts come from the repository: the README, `docs/guides/deploy.md`, `docs/reference/spec.md` and the status table in `docs/architecture/overview.md`. When one of those changes, change the content module with it. Do not add a claim the repository does not support.

## Design

- **Type:** Bricolage Grotesque (variable, with an optical size axis) for everything, IBM Plex Mono for code. Both are self-hosted through `@fontsource`, under the SIL Open Font License 1.1; the licenses are served from `public/licenses/`.
- **Color:** lichen paper (`#edf0ea`), pine ink (`#15241c`) and one cobalt action (`#2340c8`), with a deep cobalt band for the self-hosting section. Dark mode follows the system and redefines every token.
- One filled button per page, descriptive links, landmarks and a skip link, visible focus, and no motion beyond what the reader starts. `prefers-reduced-motion` turns off smooth scrolling.

## Deploy

`.github/workflows/site.yml` builds the site on pull requests that touch it. On a push to `main` it uploads `dist` to the `withhumagen.com` S3 bucket and invalidates its CloudFront distribution, once the repository has the `AWS_ROLE_ARN` and `AWS_REGION` secrets. Until then the deploy job is skipped. Pages build to `dist/<page>/index.html`; CloudFront rewrites `/deploy` to `/deploy/index.html` and serves `404.html` for a missing page.
