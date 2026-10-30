/**
 * The screen the installed app shows when the network is down: written at
 * build time (vite.config.ts) from the copy table, precached by the service
 * worker and served in place of any page it cannot fetch. It is plain HTML
 * with its styles inline and no script, which the server's content policy
 * would refuse; Try Again is a link to the address it was opened at.
 */

export interface OfflineCopy {
  offlineTitle: string;
  offlineBody: string;
  offlineRetry: string;
}

const escapeHtml = (s: string) =>
  s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

export function offlinePage(copy: OfflineCopy): string {
  const title = escapeHtml(copy.offlineTitle);
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover" />
<meta name="color-scheme" content="light dark" />
<meta name="theme-color" content="#000000" media="(prefers-color-scheme: dark)" />
<meta name="theme-color" content="#ffffff" media="(prefers-color-scheme: light)" />
<title>${title}</title>
<style>
:root { color-scheme: light dark; }
html, body { margin: 0; height: 100%; background: #ffffff; color: #111111; overscroll-behavior: none; }
body {
  display: flex; align-items: center; justify-content: center; box-sizing: border-box;
  padding: max(1.5rem, env(safe-area-inset-top)) max(1rem, env(safe-area-inset-right)) max(1.5rem, env(safe-area-inset-bottom)) max(1rem, env(safe-area-inset-left));
  font: 16px/1.5 ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
}
main { max-width: 26rem; }
h1 { margin: 0 0 0.5rem; font-size: 1.5rem; line-height: 1.25; letter-spacing: -0.01em; }
p { margin: 0 0 1.5rem; color: #5c5c5c; }
a {
  display: inline-flex; align-items: center; min-height: 44px; padding: 0 1.25rem; border-radius: 0.5rem;
  background: #111111; color: #ffffff; font-weight: 500; text-decoration: none;
}
a:focus-visible { outline: 2px solid #111111; outline-offset: 3px; }
@media (prefers-color-scheme: dark) {
  html, body { background: #000000; color: #f2f2f2; }
  p { color: #a3a3a3; }
  a { background: #f2f2f2; color: #000000; }
  a:focus-visible { outline-color: #f2f2f2; }
}
</style>
</head>
<body>
<main>
<h1>${title}</h1>
<p>${escapeHtml(copy.offlineBody)}</p>
<a href="">${escapeHtml(copy.offlineRetry)}</a>
</main>
</body>
</html>
`;
}
