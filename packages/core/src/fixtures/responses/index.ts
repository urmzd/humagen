/// <reference path="./raw.d.ts" />
/**
 * Recorded response streams, one event per line, for tests and the gallery:
 * one agent; two agents in parallel; delegation two deep waiting on an
 * approval; a stopped run.
 */
import type { ResponseEvent } from "../../response";
import nestedApproval from "./nested-approval.jsonl?raw";
import parallelAgents from "./parallel-agents.jsonl?raw";
import singleAgent from "./single-agent.jsonl?raw";
import stopped from "./stopped.jsonl?raw";

const parse = (text: string): ResponseEvent[] =>
  text
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line) as ResponseEvent);

export const responseStreams = {
  singleAgent: parse(singleAgent),
  parallelAgents: parse(parallelAgents),
  nestedApproval: parse(nestedApproval),
  stopped: parse(stopped),
};
