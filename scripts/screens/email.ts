/** Native loopback SMTP and console states for reproducible email screenshots. */
import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { createServer, type Socket } from "node:net";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type { Page } from "playwright-core";
import { defaultCopy } from "../../packages/components/src/copy.ts";

type Call = <T>(method: string, path: string, body?: unknown) => Promise<T>;
type Screen = {
  name: string;
  path: string;
  act?: (page: Page) => Promise<void>;
  after?: () => Promise<void>;
};
export interface FixtureEmail {
  from: string;
  to: string;
  data: string;
  at: string;
}
interface Preferences {
  display_name: string;
  theme: string;
  accent: string;
  personal?: Record<string, unknown> & { email?: boolean; email_version?: number };
}
let fixtureConfigured = false;

// No external connection or real mailbox is used. The SMTP address is an
// ephemeral literal loopback listener, and only the dummy recipient is accepted.
export async function notificationEmailFixture(
  dir: string,
  publicURL: string,
  tenant: string,
  subject: string,
): Promise<{ file: string; close: () => Promise<void>; messages: ReadonlyArray<FixtureEmail> }> {
  for (const value of [tenant, subject])
    assert.ok(
      value.length > 0 &&
        value.length <= 512 &&
        ![...value].some((char) => char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127),
      "invalid email fixture identity",
    );
  const publicAddress = new URL(publicURL);
  assert.ok(
    ["127.0.0.1", "[::1]", "::1"].includes(publicAddress.hostname),
    "email screenshots require a literal loopback public URL",
  );
  assert.ok(publicAddress.protocol === "http:" || publicAddress.protocol === "https:");
  const sockets = new Set<Socket>();
  const messages: FixtureEmail[] = [];
  const server = createServer((socket) => {
    if (sockets.size >= 16) {
      socket.end("421 Fixture capacity reached\r\n");
      return;
    }
    sockets.add(socket);
    socket.setEncoding("utf8");
    socket.setTimeout(10_000, () => socket.destroy());
    socket.on("error", () => {});
    socket.once("close", () => sockets.delete(socket));
    let pending = "";
    let data: string[] | null = null;
    let bytes = 0;
    let sender = "";
    let recipient = "";
    const reset = () => {
      data = null;
      bytes = 0;
      sender = "";
      recipient = "";
    };
    socket.write("220 humagen-screens ESMTP loopback fixture\r\n");
    socket.on("data", (chunk) => {
      pending += chunk;
      if (pending.length > 131072) {
        socket.destroy();
        return;
      }
      for (let index = pending.indexOf("\r\n"); index >= 0; index = pending.indexOf("\r\n")) {
        const line = pending.slice(0, index);
        pending = pending.slice(index + 2);
        if (line.length > 8192) {
          socket.end("552 Fixture line limit\r\n");
          return;
        }
        if (data !== null) {
          if (line === ".") {
            if (messages.length >= 100) {
              socket.end("452 Fixture message limit\r\n");
              return;
            }
            messages.push({
              from: sender,
              to: recipient,
              data: data.join("\r\n"),
              at: new Date().toISOString(),
            });
            reset();
            socket.write("250 Captured locally\r\n");
            continue;
          }
          bytes += Buffer.byteLength(line) + 2;
          if (bytes > 65536) {
            socket.end("552 Fixture message size limit\r\n");
            return;
          }
          data.push(line.startsWith("..") ? line.slice(1) : line);
          continue;
        }
        const command = (line.split(" ", 1)[0] ?? "").toUpperCase();
        if (command === "EHLO" || command === "HELO") {
          reset();
          socket.write("250 humagen-screens\r\n");
        } else if (command === "MAIL") {
          const match = /^MAIL FROM:<([^<>\r\n]+)>$/i.exec(line);
          if (!match || match[1] !== "notifications@example.test") {
            socket.write("550 Dummy sender only\r\n");
            continue;
          }
          reset();
          sender = match[1];
          socket.write("250 Sender accepted\r\n");
        } else if (command === "RCPT") {
          const match = /^RCPT TO:<([^<>\r\n]+)>$/i.exec(line);
          if (!sender || !match || match[1] !== "dummy@example.test") {
            socket.write("550 Dummy recipient only\r\n");
            continue;
          }
          recipient = match[1];
          socket.write("250 Recipient accepted\r\n");
        } else if (command === "DATA") {
          if (!sender || !recipient) {
            socket.write("503 Sender and recipient required\r\n");
            continue;
          }
          data = [];
          bytes = 0;
          socket.write("354 End with a dot\r\n");
        } else if (command === "RSET") {
          reset();
          socket.write("250 Reset\r\n");
        } else if (command === "NOOP") socket.write("250 OK\r\n");
        else if (command === "QUIT") {
          socket.end("221 Goodbye\r\n");
          return;
        } else socket.write("502 Unsupported fixture command\r\n");
      }
    });
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      server.removeListener("error", reject);
      resolve();
    });
  });
  const close = async () => {
    fixtureConfigured = false;
    for (const socket of sockets) socket.destroy();
    if (server.listening)
      await new Promise<void>((resolve, reject) =>
        server.close((err) => (err ? reject(err) : resolve())),
      );
  };
  try {
    const address = server.address();
    assert.ok(address && typeof address !== "string");
    await mkdir(dir, { recursive: true, mode: 0o700 });
    const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
    const policy = await readFile(join(repo, "pkg/auth/default.authz"), "utf8");
    const policyFile = join(dir, "notification-email.authz");
    await writeFile(
      policyFile,
      `${policy}\n# Explicit standing role for the isolated screenshot account.\ngrant admin to subject ${JSON.stringify(subject)}\n`,
      { mode: 0o600 },
    );
    const file = join(dir, "notification-email.yaml");
    await writeFile(
      file,
      JSON.stringify(
        {
          api_version: "humagen/v1",
          kind: "System",
          metadata: { name: "screens-email" },
          spec: {
            server: { public_url: publicURL },
            auth: { kind: "dev", policy_file: policyFile },
            notifications: {
              email: {
                address: `127.0.0.1:${address.port}`,
                from: "Humagen <notifications@example.test>",
                tls: "insecure_loopback",
                recipients: { [tenant]: { [subject]: "dummy@example.test" } },
              },
            },
          },
        },
        null,
        2,
      ),
      { mode: 0o600 },
    );
    fixtureConfigured = true;
    return { file, close, messages };
  } catch (error) {
    await close();
    throw error;
  }
}

function editable(prefs: Preferences, email: boolean) {
  return {
    display_name: prefs.display_name,
    theme: prefs.theme,
    accent: prefs.accent,
    personal: { ...prefs.personal, email, email_version: prefs.personal?.email_version ?? 0 },
  };
}

// The configured opt-in state is included only when this module's real SMTP
// fixture was initialized before screen registration. Capability mismatches
// fail the capture instead of substituting a browser response.
export function emailScreens(call: Call): Screen[] {
  const configured = fixtureConfigured;
  const copy = defaultCopy.personalPreferences;
  const verifyCapability = async () => {
    const me = await call<{ email_notifications_available?: boolean }>("GET", "/api/me");
    assert.equal(
      me.email_notifications_available,
      configured,
      "native email capability does not match SMTP fixture wiring",
    );
  };
  const screens: Screen[] = [
    {
      name: configured ? "email-configured-default-off" : "email-unconfigured",
      path: "/you/notifications",
      act: async (page) => {
        await verifyCapability();
        const prefs = await call<Preferences>("GET", "/api/preferences");
        assert.equal(
          prefs.personal?.email ?? false,
          false,
          "fresh screenshot account must start opted out",
        );
        const checkbox = page.getByRole("checkbox", { name: copy.email, exact: true });
        await checkbox.waitFor();
        assert.equal(await checkbox.isChecked(), false);
        assert.equal(await checkbox.isDisabled(), !configured);
        await page
          .getByText(configured ? copy.emailDescription : copy.deliveryUnavailable, { exact: true })
          .first()
          .waitFor();
      },
    },
  ];
  if (configured) {
    let originalEmail = false;
    screens.push({
      name: "email-configured-opt-in",
      path: "/you/notifications",
      act: async (page) => {
        await verifyCapability();
        const before = await call<Preferences>("GET", "/api/preferences");
        originalEmail = before.personal?.email ?? false;
        assert.equal(originalEmail, false, "opt-in fixture must begin with email off");
        const checkbox = page.getByRole("checkbox", { name: copy.email, exact: true });
        await checkbox.waitFor();
        assert.equal(await checkbox.isDisabled(), false);
        await checkbox.check();
        await page.getByRole("button", { name: copy.save, exact: true }).click();
        await page.getByText(copy.saved, { exact: true }).waitFor();
        const after = await call<Preferences>("GET", "/api/preferences");
        assert.equal(
          after.personal?.email,
          true,
          "email opt-in did not persist through the real API",
        );
        assert.ok(
          (after.personal?.email_version ?? 0) > (before.personal?.email_version ?? 0),
          "email consent CAS version did not advance",
        );
        await page.reload();
        await checkbox.waitFor();
        assert.equal(await checkbox.isChecked(), true, "email opt-in did not survive reload");
      },
      after: async () => {
        const current = await call<Preferences>("GET", "/api/preferences");
        await call("PUT", "/api/preferences", editable(current, originalEmail));
        const restored = await call<Preferences>("GET", "/api/preferences");
        assert.equal(
          restored.personal?.email ?? false,
          originalEmail,
          "email screenshot did not restore consent",
        );
      },
    });
  }
  return screens;
}
