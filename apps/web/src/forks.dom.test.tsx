import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { defaultCopy } from "@humagen/components";
import type { Thread } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { BringBackResult, ForkContext, ForkConversation } from "./ForkConversation.tsx";
import { ConversationAccess } from "./ConversationAccess.tsx";
import { NewProject } from "./Projects.tsx";
import { ForkProject } from "./ForkProject.tsx";
import { ProjectRollup } from "./ProjectRollup.tsx";
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
 net=stubFetch({"GET /api/discovery":(_body,url)=>{query=url;return {items:[{kind:"project",id:"Launch",title:"Launch Plan"}]};}});
 const opened:unknown[]=[];
 render(<QuickSwitcher onClose={()=>{}} onGo={(route)=>opened.push(route)}/>);
 const link=await screen.findByRole("link",{name:/^Launch Plan/});
 assert.equal(query?.get("limit"),"20");
 fireEvent.click(link);
 assert.deepEqual(opened,[{page:"project",name:"Launch",tab:"inbox"}]);
});


test("project fork copies only selected confirmed memory and does not navigate before publication",async()=>{
 let calls=0;const bodies:unknown[]=[];const opened:string[]=[];
 net=stubFetch({
  "GET /api/projects/Launch/memory":()=>({notes:[{id:"n1",text:"Confirmed decision",status:"approved"},{id:"n2",text:"Pending proposal",status:"pending"}]}),
  "GET /api/knowledge":()=>({sources:[]}),
  "POST /api/projects/Launch/fork":(body)=>{bodies.push(body);calls++;return {id:"pf-1",source:"Launch",destination:"Launch-copy",state:calls===1?"failed":"ready",error:calls===1?"Copy stopped before publication.":undefined,projects:2,agents:1,memory:1};},
 });
 render(<ForkProject source="Launch" projects={[]} onClose={()=>{}} onReady={(name)=>opened.push(name)}/>);
 fireEvent.click(await screen.findByRole("checkbox",{name:"Confirmed decision"}));
 assert.equal(screen.queryByText("Pending proposal"),null);
 fireEvent.click(screen.getByRole("checkbox",{name:defaultCopy.projectFork.archive}));
 fireEvent.click(screen.getByRole("button",{name:defaultCopy.projectFork.create}));
 await screen.findByText("Copy stopped before publication.");
 assert.deepEqual(opened,[]);
 assert.equal((screen.getByLabelText(defaultCopy.projectFork.name) as HTMLInputElement).disabled,true);
 fireEvent.click(screen.getByRole("button",{name:defaultCopy.projectFork.retry}));
 await waitFor(()=>assert.deepEqual(opened,["Launch-copy"]));
 assert.equal(bodies.length,2);
 assert.deepEqual(bodies[0],bodies[1]);
 assert.deepEqual((bodies[0] as {memory:string[]}).memory,["n1"]);
 assert.equal((bodies[0] as {archive_conversations:boolean}).archive_conversations,true);
});


test("project creation chooses a native template and can live under an initiative",async()=>{
 let posted:unknown;
 net=stubFetch({
  "GET /api/project-templates":()=>({templates:[{name:"delivery",title:"Delivery Project",description:"Plan delivery",kind:"team",initiative:false}]}),
  "POST /api/projects":(body)=>{posted=body;return {name:"Launch"};},
 });
 render(<NewProject places={[]} parent={{name:"Strategy",kind:"team",depth:1}} canGovern={false} onCreated={()=>{}} onCancel={()=>{}}/>);
 await screen.findByRole("option",{name:"Delivery Project"});
 fireEvent.change(screen.getByLabelText(defaultCopy.projectTemplates.start),{target:{value:"delivery"}});
 fireEvent.change(screen.getByLabelText(new RegExp(`^${defaultCopy.projects.form.title}`)),{target:{value:"Launch"}});
 fireEvent.click(screen.getByRole("button",{name:defaultCopy.projects.createProject}));
 await waitFor(()=>assert.ok(posted));
 assert.deepEqual(posted,{name:"Launch",template:"delivery",kind:"team",parent:"Strategy"});
});


test("global discovery navigates real agent and conversation results using bounded kind filters",async()=>{
 const seen:URLSearchParams[]=[];const opened:unknown[]=[];
 net=stubFetch({"GET /api/discovery":(_body,query)=>{seen.push(query);return {items:query.get("kinds")==="conversation"?[{kind:"conversation",id:"th-task",title:"Review supplier plan",project:"Launch",channel:"general"}]:[{kind:"agent",id:"planner",title:"planner",hue:150}]};}});
 render(<QuickSwitcher onClose={()=>{}} onGo={(route)=>opened.push(route)}/>);
 fireEvent.click(await screen.findByRole("link",{name:/^planner/}));
 assert.deepEqual(opened,[{page:"agent",name:"planner",tab:"prompt"}]);
 fireEvent.change(screen.getByLabelText(defaultCopy.projectSwitcher.kinds),{target:{value:"conversation"}});
 fireEvent.click(await screen.findByRole("link",{name:/^Review supplier plan/}));
 assert.deepEqual(opened[1],{page:"conversation",id:"th-task"});
 assert.equal(seen.at(-1)?.get("limit"),"20");
 assert.equal(seen.at(-1)?.get("kinds"),"conversation");
 assert.ok(net.seen.every((request)=>request.path.startsWith("/api/discovery?")));
});

test("initiative rollup opens original child work and never invents progress",async()=>{
 net=stubFetch({"GET /api/projects/Strategy/rollup":()=>({project:"Strategy",since:at,projects:[{name:"Launch",title:"Launch Plan",parent:"Strategy",active:1,conversations:3,unread:2,needs:1,runs_30_days:9}],notes:[{id:"n1",project:"Launch",text:"Confirmed delivery risk",kind:"risk"}],needs:[{id:"i1",project:"Launch",subject:"Approve launch",kind:"approval"}],more_projects:false,more_notes:false,more_needs:false})});
 render(<ProjectRollup project="Strategy"/>);
 assert.ok(await screen.findByText("Confirmed delivery risk"));
 assert.equal(screen.getByRole("link",{name:"Launch Plan"}).getAttribute("href"),"/projects/Launch/overview");
 assert.equal(screen.getByRole("link",{name:"Review: Launch"}).getAttribute("href"),"/projects/Launch?message=i1");
 assert.ok(screen.getByText("Runs in 30 Days"));
 assert.equal(screen.queryByRole("progressbar"),null);
});
