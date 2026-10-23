import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, screen, render } from "@testing-library/react";
import { defaultCopy } from "@humagen/components";
import { stubFetch } from "./dom.ts";
import { ReadAudit } from "./ReadAudit.tsx";
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});
test("recorded reads show the reader, place and route",async()=>{
 net=stubFetch({"GET /api/read-audit":()=>({records:[{subject:"ana",project:"Account",thread:"private-1",route:"GET /api/threads/{id}",at:"2026-10-22T10:00:00Z"}]})});
 render(<ReadAudit/>);
 await screen.findByText("ana");
 assert.ok(screen.getByText("Account / private-1"));
 assert.ok(screen.getByText("GET /api/threads/{id}"));
});
test("the audit names an empty window",async()=>{
 net=stubFetch({"GET /api/read-audit":()=>({records:[]})});render(<ReadAudit/>);
 await screen.findByText(defaultCopy.readAudit.empty);
});

test("a failed read says the audit could not load", async () => {
 net = stubFetch({});
 render(<ReadAudit />);
 await screen.findByText(defaultCopy.readAudit.failed);
});
