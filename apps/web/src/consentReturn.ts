// A consent's return leaves the address first and is held for Connections, once.
const path = "/connections/callback";
let held: URLSearchParams | null = null;

export function takeConsentReturn(w: Window = window): void {
  if (w.location.pathname !== path) return;
  const q = new URLSearchParams(w.location.search);
  if (q.has("state") || q.has("code")) held = q;
  w.history.replaceState(w.history.state, "", "/connections");
}

export function consentReturn(): URLSearchParams | null {
  if (!held) takeConsentReturn();
  const out = held;
  held = null;
  return out;
}

/** Sign-in comes back to this page; to the callback page, with nothing of its return. */
export const signInAddress = ({ pathname, search }: Pick<Location, "pathname" | "search">) =>
  `/auth/login?return_to=${encodeURIComponent(pathname === path ? path : pathname + search)}`;
