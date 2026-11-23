import { emptyTurn, type Failure, type FailureCause, type Turn } from "@humagen/core";
import { describe, expect, it } from "vitest";
import { failureTitle, summarizeTurn } from "./components";
import { defaultCopy } from "./copy";

const words = defaultCopy.runFailure;

const failure = (cause: FailureCause): Failure => ({
  kind: "internal",
  cause,
  message: words.because[cause],
  detail: "raw error",
});

const failed = (cause: FailureCause): Turn => ({
  ...emptyTurn("r1", { name: "github-reviewer" }),
  status: "failed",
  failure: failure(cause),
});

describe("a failed run in plain words", () => {
  const title: Record<FailureCause, (agent: string) => string> = {
    no_model_provider: words.couldNotRun,
    credential: words.couldNotRun,
    budget: words.couldNotFinish,
    rate_limited: words.couldNotFinish,
    permission_denied: words.couldNotFinish,
    timeout: words.couldNotFinish,
    unavailable: words.couldNotFinish,
    failed_steps: words.couldNotFinish,
    max_iterations: words.couldNotFinish,
    undeliverable: words.couldNotReply,
    max_attempts: words.couldNotFinish,
    unknown: words.couldNotFinish,
  };

  it("names the agent by the name people see, and says why and what to do", () => {
    for (const cause of Object.keys(title) as FailureCause[]) {
      expect(failureTitle(failure(cause), defaultCopy, "Github Reviewer")).toBe(
        title[cause]("Github Reviewer"),
      );
      const summary = summarizeTurn(failed(cause), defaultCopy, "Github Reviewer");
      expect(summary).toBe(`${title[cause]("Github Reviewer")}: ${words.because[cause]}`);
      // The runtime's words are never the summary.
      expect(summary).not.toContain("raw error");
    }
  });

  it("says the model provider case as one sentence with where to fix it", () => {
    expect(summarizeTurn(failed("no_model_provider"), defaultCopy, "Github Reviewer")).toBe(
      "Github Reviewer could not run: Its model provider is not enabled. An admin can turn it on in Settings > Model providers.",
    );
  });

  it("falls back to the agent's handle, then to the agent", () => {
    expect(summarizeTurn(failed("timeout"))).toContain(words.couldNotFinish("github-reviewer"));
    expect(failureTitle({ kind: "internal", cause: "unknown", message: "" })).toBe(
      words.couldNotFinish(words.someone),
    );
  });

  it("keeps a failure with no cause as it was", () => {
    expect(failureTitle({ kind: "timeout", message: "It took too long." })).toBe("Timed out");
  });
});
