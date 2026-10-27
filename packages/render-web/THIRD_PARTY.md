# Third-party components

`src/ui` adapts the Radix new-york-v4 component sources from
[shadcn/ui](https://ui.shadcn.com), licensed under the
[MIT license](https://github.com/shadcn-ui/ui/blob/main/LICENSE.md).
The sources were retrieved from its public registry on 2026-10-26.
Humagen uses its own existing Lucide geometry, semantic theme tokens and
coarse-pointer target sizes. The shared surface contracts remain in
`@humagen/primitives`.

## Fonts

IBM Plex Sans Variable and IBM Plex Mono are self-hosted through Fontsource.
The original [IBM Plex project](https://github.com/IBM/plex) distributes them
under the SIL Open Font License 1.1. Copyright and license texts are retained
in `licenses/IBM_PLEX_SANS_LICENSE` and `licenses/IBM_PLEX_MONO_LICENSE`.
Adobe also lists [Plex Sans](https://fonts.adobe.com/fonts/ibm-plex-sans) and
[Plex Mono](https://fonts.adobe.com/fonts/ibm-plex-mono). No Adobe-hosted kit is
required by the console.

## Provider icons

Drive, Slack and GitHub SVG paths come from Font Awesome Free 7.x, licensed
under CC BY 4.0. Source: [FortAwesome/Font-Awesome](https://github.com/FortAwesome/Font-Awesome/tree/7.x/svgs/brands).
The license is retained in `licenses/FONT_AWESOME_LICENSE`.
