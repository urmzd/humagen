// One social preview image per page, at /og/<page>-<hash>.png (src/lib/og.ts).
import type { APIRoute, GetStaticPaths } from "astro";
import { type OgImage, ogImages } from "../../lib/og";

export const getStaticPaths = (async () =>
  (await ogImages()).map((img) => ({
    params: { name: img.name },
    props: { img },
  }))) satisfies GetStaticPaths;

export const GET: APIRoute<{ img: OgImage }> = ({ props }) =>
  new Response(new Uint8Array(props.img.png), { headers: { "Content-Type": "image/png" } });
