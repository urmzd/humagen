// Builds llms.txt from the same content module the pages render, so the
// agent-readable summary cannot drift from the site.
import {
  alternatives,
  architecture,
  contact,
  deployIntro,
  doc,
  hero,
  limits,
  pages,
  points,
  policySnippet,
  prerequisites,
  selfHost,
  site,
  sizing,
  steps,
  why,
} from "../content/site.ts";

const url = (path: string) => new URL(path, site.url).href;
const fence = (lang: string, text: string) => `\`\`\`${lang}\n${text}\n\`\`\``;

export function llmsTxt(): string {
  const out: string[] = [];
  out.push(`# ${site.name}`, "", `> ${site.summary}`, "", hero.lede, "", hero.status, "");

  out.push("## Pages", "");
  out.push(`- [${pages.home.title}](${url(pages.home.path)}): ${pages.home.description}`);
  out.push(`- [${pages.deploy.title}](${url(pages.deploy.path)}): ${pages.deploy.description}`);
  out.push(`- [Source on GitHub](${site.repo}): code, docs and issues, ${site.license}`);
  out.push(`- [Spec reference](${doc("docs/reference/spec.md")}): every setting`);
  out.push(`- [${contact.label}](${contact.href}): ${contact.email}`, "");

  out.push("## What it does", "");
  for (const p of [points.together, points.operate, ...points.grid]) {
    out.push(`- **${p.title}.** ${p.body}`);
  }
  out.push("");

  out.push(`## ${why.heading}`, "", ...why.intro.flatMap((p) => [p, ""]));
  out.push(`| ${why.columns.join(" | ")} |`, `|${why.columns.map(() => "---").join("|")}|`);
  for (const r of why.rows) out.push(`| ${r.join(" | ")} |`);
  out.push("");

  out.push(`## ${selfHost.heading}`, "", selfHost.intro, "");
  for (const p of selfHost.points) out.push(`- **${p.title}.** ${p.body}`);
  out.push("");

  out.push("## Deploy guide", "", ...deployIntro.flatMap((p) => [p, ""]));
  out.push("### Prerequisites", "", ...prerequisites.map((p) => `- ${p}`), "", sizing, "");
  out.push("### Architecture", "", architecture.caption, "");
  for (const z of architecture.zones) {
    out.push(`- ${z.name}`, ...z.items.map((i) => `  - ${i}`));
  }
  out.push("");
  steps.forEach((s, i) => {
    out.push(`### ${i + 1}. ${s.title}`, "", ...s.body.flatMap((p) => [p, ""]));
    if (s.code) out.push(`${s.code.label}:`, "", fence(s.code.lang, s.code.text), "");
    for (const p of s.after ?? []) out.push(p, "");
    if (s.id === "sign-in") {
      out.push(`${policySnippet.label}:`, "", fence(policySnippet.lang, policySnippet.text), "");
    }
  });

  out.push(`## ${alternatives.heading}`, "", alternatives.intro, "");
  for (const r of alternatives.rows) {
    out.push(`- ${r.name} (${r.when}): ${r.what} [${r.link.label}](${r.link.href})`);
  }
  out.push("");

  out.push(`## ${limits.heading}`, "", limits.intro, "");
  for (const l of limits.items) out.push(`- ${l}`);
  out.push("", `[${limits.link.label}](${limits.link.href})`, "");
  return out.join("\n");
}
