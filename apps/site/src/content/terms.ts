// The terms page's text, in a file of its own so the page's history is this
// file's history. Each statement is one the repository supports: LICENSE,
// SECURITY.md, README.md and docs/reference/spec.md. It names no company,
// court or contact the project does not have, and it adds no condition to
// the license. A test pins the text to the day below: change both together.

import { site } from "./site.ts";

export type TermsLink = { label: string; href: string };
export type TermsSection = { id: string; title: string; body: string[]; links?: TermsLink[] };

/** This file, from the repository's root: what the page's history link shows. */
export const termsFile = "apps/site/src/content/terms.ts";

export const terms = {
  heading: "Terms",
  /** The words before the date on the page. */
  changed: "Last changed",
  /** The day the text last changed. Change it with the text. */
  updated: "2026-11-21",
  intro: [
    "Humagen is open source software that you host yourself. This page says what the license gives you, what an AI answer is, and who answers for a deployment.",
    "The Apache License 2.0 is the agreement for the software. This page adds no condition to it.",
    "Whoever runs a deployment may link this page as its terms, and may add terms of its own.",
  ],
  sections: [
    {
      id: "license",
      title: "The software is under the Apache License 2.0",
      body: [
        "You may use, change and share Humagen as the Apache License 2.0 allows. Where this page and the license differ, the license decides. Some parts Humagen bundles, such as fonts and icons, keep their own licenses.",
        "The license provides the software as is, without warranties, unless the law requires otherwise or someone agrees to more in writing. It also limits what contributors are liable for. Read sections 7 and 8 of the license before you depend on Humagen.",
      ],
      links: [{ label: "Read the Apache License 2.0", href: `${site.repo}/blob/main/LICENSE` }],
    },
    {
      id: "ai",
      title: "Humagen is AI and can make mistakes",
      body: [
        "Agents in Humagen answer with AI models. An answer can be wrong, incomplete or out of date, and it can sound sure when it is wrong.",
        "Check an answer before you rely on it. An answer is not legal, medical, financial or other professional advice.",
        "An agent can act with the tools and permissions it is given. You are responsible for what you ask an agent to do, what you approve, and what you do with its work.",
      ],
    },
    {
      id: "deployment",
      title: "Someone runs each deployment",
      body: [
        "Humagen is self-hosted. The person or organization that runs a deployment decides who may sign in and which models and tools its agents may use.",
        "If you use a deployment that someone else runs, their own terms and policies may apply to you as well. Ask them.",
      ],
    },
    {
      id: "services",
      title: "Other services have their own terms",
      body: [
        "A deployment calls the model providers and the services that people connect to it, such as Slack or GitHub. Each of those has its own terms and prices. Whoever connects a service is responsible for following its terms.",
      ],
    },
    {
      id: "site",
      title: "This website and the documentation",
      body: [
        "This website and the documentation describe the software, and they can be wrong or out of date. Like the software, they are provided as is.",
      ],
    },
    {
      id: "changes",
      title: "Changes to this page",
      body: [
        "This page can change. It shows the day it last changed, and the repository keeps every earlier version.",
      ],
      links: [
        { label: "See the history of this page", href: `${site.repo}/commits/main/${termsFile}` },
      ],
    },
    {
      id: "questions",
      title: "Questions and security reports",
      body: [
        "Ask a question or report a problem on GitHub.",
        "Report a security problem privately: the security policy says how. If its private form is closed to you, open an issue that asks for a private contact, and leave the details out.",
      ],
      links: [
        { label: "Ask on GitHub", href: `${site.repo}/issues` },
        { label: "Read the security policy", href: `${site.repo}/blob/main/SECURITY.md` },
      ],
    },
  ] satisfies TermsSection[] as TermsSection[],
};

/** The day the terms last changed, in words: "November 21, 2026". */
export function termsDate(iso = terms.updated): string {
  return new Date(`${iso}T00:00:00Z`).toLocaleDateString("en-US", {
    timeZone: "UTC",
    year: "numeric",
    month: "long",
    day: "numeric",
  });
}
