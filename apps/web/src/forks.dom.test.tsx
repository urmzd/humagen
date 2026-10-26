import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { defaultCopy } from "@humagen/components";
import type { Thread } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { BringBackResult, ForkContext, ForkConversation } from "./ForkConversation.tsx";
import { ConversationAccess } from "./ConversationAccess.tsx";
import { QuickSwitcher } from "./QuickSwitcher.tsx";

const at="2026-10-20T12:00:00Z";
const source:Thread={id:"th-0123456789abcdef",project:"Launch",channel:"general",title:"Plan",visibility:"private",participants:[{kind:"person",name:"ana",owner:true,at}],version:1,created_by:"ana",created_at:at,updated_at:at};
let net:ReturnType<typeof stubFetch>|undefined;
afterEach(()=>{cleanup();net?.restore();net=undefined;});

test("fork submits a real saved turn and explicit empty source scope without copying future messages",async()=>{
 let posted:unknown;let opened:Thread|undefined;
 net=stubFetch({
  "GET /api/projects/Launch/agents":()=>({available:{all:true,names:[]},homes:[],version:1}),
  "GET /api/agents":()=>({agents:[{spec:{name:"helper"}}]}),
  "GET /api/knowledge":()=>({sources:[]}),
  "POST /api/threads/th-0123456789abcdef/fork":(body)=>{posted=body;return {...source,id:"th-1111111111111111"};},
 });
 render(<ForkConversation thread={source} through="m-2" onClose={()=>{}} onForked={(thread)=>{opened=thread;}}/>);
 fireEvent.click(await screen.findByRole("checkbox",{name:"helper"}));
 fireEvent.change(screen.getByLabelText(defaultCopy.forking.sources),{target:{value:"none"}});
 fireEvent.click(screen.getByRole("button",{name:defaultCopy.forking.create}));
 await waitFor(()=>assert.ok(opened));
 assert.deepEqual(posted,{through:"m-2",title:"Plan (fork)",agents:["helper"],sources:[]});
});

test("fork history is read-only and bring-back requires an explicit post",async()=>{
 const fork:Thread={...source,fork:{source_thread:"th-1111111111111111",through:"m-2",by:"ana",at,context:[{kind:"agent",by:"helper",text:"Saved answer",at}]}};
 render(<ForkContext thread={fork}/>);
 assert.ok(screen.getByText("Saved answer"));
 assert.equal(screen.queryByRole("button"),null);
 cleanup();let posted:unknown;
 net=stubFetch({"POST /api/threads/th-0123456789abcdef/bring-back":(body)=>{posted=body;return {id:"m-3"};}});
 render(<BringBackResult thread={fork} run="r1" text="@helper unexecuted draft" onClose={()=>{}} onPosted={()=>{}}/>);
 assert.equal(posted,undefined);
 fireEvent.click(screen.getByRole("button",{name:defaultCopy.forking.bringBack}));
 await waitFor(()=>assert.deepEqual(posted,{run:"r1",text:"@helper unexecuted draft"}));
});

test("People & Access changes membership with the recorded version and explains shared visibility",async()=>{
 let posted:unknown;
 net=stubFetch({
 "GET /api/me":()=>({principal:{subject:"ana"},handle:"ana",permissions:["runs:write"]}),
 "GET /api/projects/Launch":()=>({project:{access:{role:"owner"}}}),
 "PUT /api/threads/th-0123456789abcdef/participants":(body)=>{posted=body;return {...source,version:2,participants:[...source.participants,{kind:"person",name:"bo",at}]};},
 });
 render(<ConversationAccess thread={{...source,visibility:"shared"}}/>);
 assert.ok(screen.getByText(defaultCopy.conversationAccess.sharedHint));
 fireEvent.change(await screen.findByLabelText(defaultCopy.conversationAccess.handle),{target:{value:"@bo"}});
 fireEvent.click(screen.getByRole("button",{name:defaultCopy.conversationAccess.add}));
 await waitFor(()=>assert.deepEqual(posted,{version:1,add:[{kind:"person",name:"bo"}]}));
 assert.ok(await screen.findByText("bo"));
});

test("project switcher uses a bounded server search and preserves navigable links",async()=>{
 let query:URLSearchParams|undefined;
 net=stubFetch({"GET /api/projects":(_body,url)=>{query=url;return {projects:[{name:"Launch",title:"Launch Plan",path:[]}],next:null};}});
 const opened:unknown[]=[];
 render(<QuickSwitcher onClose={()=>{}} onGo={(route)=>opened.push(route)}/>);
 const link=await screen.findByRole("link",{name:"Launch Plan"});
 assert.equal(query?.get("limit"),"20");
 fireEvent.click(link);
 assert.deepEqual(opened,[{page:"project",name:"Launch",tab:"inbox"}]);
});
