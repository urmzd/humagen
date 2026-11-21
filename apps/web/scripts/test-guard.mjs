#!/usr/bin/env node
// Runs a test command so that it cannot take the machine down with it.
//
//   node scripts/test-guard.mjs <command> [args...]
//
// - Memory: the command and every process it starts share a process group,
//   whose resident memory is read twice a second. The whole group is killed
//   when one process passes HUMAGEN_TEST_MAX_PROCESS_MB (default 3072), the
//   size of a runaway rather than of a busy test file, or the group passes
//   HUMAGEN_TEST_MAX_MB (default 8192). Node's own heap cap,
//   --max-old-space-size=2048, is added to NODE_OPTIONS so node --test's
//   child processes carry it too.
// - Time: past HUMAGEN_TEST_MAX_MINUTES (default 15) the group is killed.
// - Concurrency: at most HUMAGEN_TEST_SLOTS (default 2) guarded runs at once
//   on this machine, across every checkout and worktree. A run past that
//   waits for a slot rather than stacking another test process on top.
//   A slot is a TCP port on 127.0.0.1 (HUMAGEN_TEST_SLOT_PORT, default 47611,
//   and the ports after it) held by listening on it: taking one is a single
//   atomic bind, and the kernel frees it the moment the holder exits, however
//   it exits, so there is no lock file to go stale.
// - The guard will not run a command whose memory it cannot read: if ps
//   fails before or during the run, it refuses or stops.
// - Nothing outlives the guard. The group is led by a second copy of this
//   script (--lead), joined to the guard by an IPC channel. If the guard
//   dies, even by SIGKILL, the kernel closes the channel and the leader kills
//   the whole group at once. The leader also holds the slot's socket, so the
//   slot is not free until the group is gone.
//
// macOS enforces no per-process memory limit (RLIMIT_AS and RLIMIT_RSS are
// ignored), so the ceiling is watched from here.
import { execFileSync, spawn } from "node:child_process";
import { createServer } from "node:net";

const number = (name, fallback) => {
  const n = Number(process.env[name]);
  return Number.isFinite(n) && n > 0 ? n : fallback;
};
const maxMB = number("HUMAGEN_TEST_MAX_MB", 8192);
const maxProcessMB = number("HUMAGEN_TEST_MAX_PROCESS_MB", 3072);
const maxMinutes = number("HUMAGEN_TEST_MAX_MINUTES", 15);
const slots = Math.floor(number("HUMAGEN_TEST_SLOTS", 2));
const slotPort = Math.floor(number("HUMAGEN_TEST_SLOT_PORT", 47611));

// The group leader: run the command in this process group, hold the slot the
// guard hands over, and take the group down the moment the guard is gone.
if (process.argv[2] === "--lead") {
  const killGroup = () => {
    try {
      process.kill(-process.pid, "SIGKILL");
    } catch {}
    process.exit(1);
  };
  process.on("disconnect", killGroup);
  if (!process.connected) killGroup();
  let held;
  process.on("message", (_, handle) => {
    held = handle;
  });
  const [cmd, ...rest] = process.argv.slice(3);
  const run = spawn(cmd, rest, { stdio: "inherit" });
  run.on("error", (e) => {
    console.error(`test-guard: ${e.message}`);
    process.exit(127);
  });
  run.on("exit", (code, signal) => {
    held?.close();
    process.exit(code ?? (signal ? 1 : 0));
  });
  for (const signal of ["SIGINT", "SIGTERM", "SIGHUP"]) process.on(signal, () => run.kill(signal));
} else {
  await guardRun();
}

async function guardRun() {
  const [command, ...args] = process.argv.slice(2);
  if (!command) {
    console.error("usage: test-guard.mjs <command> [args...]");
    process.exit(2);
  }

  const say = (message) => console.error(`test-guard: ${message}`);

  const listen = (port) =>
    new Promise((resolve) => {
      const server = createServer();
      server.once("error", () => resolve(undefined));
      server.listen({ port, host: "127.0.0.1", exclusive: true }, () => {
        server.unref();
        resolve(server);
      });
    });

  const take = async () => {
    for (let i = 0; i < slots; i++) {
      const server = await listen(slotPort + i);
      if (server) return server;
    }
    return undefined;
  };

  /** Resident memory of every process, in KB by process group, or undefined when ps fails. */
  const sample = () => {
    try {
      return execFileSync("ps", ["-axo", "pgid=,rss="], { encoding: "utf8" });
    } catch {
      return undefined;
    }
  };
  if (sample() === undefined) {
    say("refusing to run: `ps -axo pgid=,rss=` failed, so memory could not be watched");
    process.exit(2);
  }

  let slot = await take();
  if (!slot) {
    say(`${slots} test runs are already going on this machine; waiting for one to finish`);
    while (!(slot = await take())) await new Promise((r) => setTimeout(r, 1000));
  }
  const release = () => slot.close();

  const heapCap = "--max-old-space-size=2048";
  const nodeOptions = process.env.NODE_OPTIONS ?? "";
  const env = {
    ...process.env,
    NODE_OPTIONS: nodeOptions.includes("--max-old-space-size")
      ? nodeOptions
      : `${nodeOptions} ${heapCap}`.trim(),
  };

  // detached makes the leader the head of a new process group, so the group
  // is everything the run started and can be read and killed as one.
  const child = spawn(process.execPath, [import.meta.filename, "--lead", command, ...args], {
    stdio: ["inherit", "inherit", "inherit", "ipc"],
    env,
    detached: true,
  });
  child.send("slot", slot);

  let stopped = "";
  const kill = (signal) => {
    try {
      process.kill(-child.pid, signal);
    } catch {}
  };
  const stop = (why) => {
    if (stopped) return;
    stopped = why;
    say(why);
    kill("SIGKILL");
  };

  /** The group's resident memory, in all and in its largest process, in MB. */
  const groupMB = () => {
    const out = sample();
    if (out === undefined) return undefined;
    let total = 0;
    let largest = 0;
    for (const line of out.split("\n")) {
      const [pgid, rss] = line.trim().split(/\s+/).map(Number);
      if (pgid !== child.pid || !rss) continue;
      total += rss;
      largest = Math.max(largest, rss);
    }
    return { total: total / 1024, largest: largest / 1024 };
  };

  const cause = "A test that prints a DOM node or never stops rendering is the usual cause";
  let peak = 0;
  let peakProcess = 0;
  const watch = setInterval(() => {
    const mb = groupMB();
    if (!mb) {
      stop("killed: `ps -axo pgid=,rss=` failed, so memory could no longer be watched");
      return;
    }
    const { total, largest } = mb;
    peak = Math.max(peak, total);
    peakProcess = Math.max(peakProcess, largest);
    if (largest > maxProcessMB)
      stop(
        `killed: one test process used ${Math.round(largest)} MB, past ${maxProcessMB} MB (HUMAGEN_TEST_MAX_PROCESS_MB). ${cause}`,
      );
    else if (total > maxMB)
      stop(
        `killed: the run used ${Math.round(total)} MB, past ${maxMB} MB (HUMAGEN_TEST_MAX_MB). ${cause}`,
      );
  }, 500);
  const deadline = setTimeout(
    () => stop(`killed: the run took longer than ${maxMinutes} minutes (HUMAGEN_TEST_MAX_MINUTES)`),
    maxMinutes * 60_000,
  );

  for (const signal of ["SIGINT", "SIGTERM", "SIGHUP"])
    process.on(signal, () => {
      kill(signal);
      stop(`stopped by ${signal}`);
    });

  child.on("exit", (code, signal) => {
    clearInterval(watch);
    clearTimeout(deadline);
    // Anything the run left behind in its group goes with it.
    kill("SIGKILL");
    release();
    if (process.env.HUMAGEN_TEST_REPORT)
      say(`peak ${Math.round(peak)} MB, largest process ${Math.round(peakProcess)} MB`);
    process.exit(stopped ? 1 : (code ?? (signal ? 1 : 0)));
  });
}
