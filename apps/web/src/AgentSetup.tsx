import {defaultCopy} from "@humagen/components";
import {AgentMessage} from "@humagen/web";
import {answerOfAction,answers} from "@humagen/adapter/question";
import {WebSurface} from "@humagen/render-web";
import {useEffect,useMemo,useState} from "react";
import type {AgentSetup,AgentSetupPlan} from "../../../packages/client/src/generated/types.ts";
import {api,coworking,type DirectoryPerson,type Me,type ModelPolicy,type ProjectView,type Revision,type Source,type ThreadAsk} from "./api.ts";
import {turnOf,type Run} from "./adapter.ts";
import {agentTemplates,type AgentTemplate,setupTools} from "./agentTemplates.ts";
import {askedOf,copyFor} from "./asks.ts";
import {useRunTurn} from "./Runs.tsx";
import {useRunStop} from "./RunStop.tsx";
import {MessageTime} from "./MessageTime.tsx";
import {href} from "./route.ts";
import {button,field,Label,Page,primary,Problem,SkeletonRows} from "./ui.tsx";

const words=defaultCopy.agentSetup;
type Rules=NonNullable<AgentSetupPlan["rules"]>;
const categories=["read_context","write_files","publish_artifacts","external_messages","call_agents","other"] as const;
const terminal=(run?:Run)=>!!run&&["completed","failed","stopped"].includes(run.status);

function TestResult({run,onRefresh}:{run:Run;onRefresh:()=>void}) {
 const {run:shown,control}=useRunStop(run,!terminal(run));
 const live=useRunTurn(shown);
 const [ask,setAsk]=useState<ThreadAsk>();
 const [problem,setProblem]=useState("");
 useEffect(()=>{
  let active=true;
  if(shown.thread)coworking.thread(shown.thread).then(page=>{if(active)setAsk(page.turns.find(turn=>turn.run?.id===shown.id)?.ask)}).catch(()=>{});
  return()=>{active=false};
 },[shown.id,shown.thread,shown.status,shown.updated_at]);
 const turn=askedOf(live??turnOf(shown),ask);
 return <section className="flex min-w-0 flex-col gap-3 rounded-aui border border-aui-border p-3">
  {ask&&!ask.yours&&<p className="text-aui-sm text-aui-muted">{words.reviewerNeeded}</p>}
  <WebSurface onAction={action=>{
   if(!answers(action))return;
   const answer=answerOfAction(action);if(!answer)return;
   setProblem("");api.answer(answer).then(onRefresh).catch(error=>setProblem(error instanceof Error?error.message:words.failed));
  }}><AgentMessage turn={turn} copy={copyFor(ask)} timestamp={<MessageTime at={shown.created_at}/>} actions={control} capabilities={{regenerate:false}}/></WebSurface>
  <Problem message={problem}/>
  {shown.thread&&<a className={`${button} self-start`} href={href({page:"conversation",id:shown.thread})} target="_blank" rel="noreferrer">{words.openTest}</a>}
 </section>;
}

/** Saved inactive setup uses the normal runtime and native approvals. */
export function AgentSetupWizard({name,me,agents,projects,sources,policies,canBill,onSaved,onActivated,onBack,onModels}:{
 name?:string;me:Me;agents:Revision[];projects:ProjectView[];sources:Source[];policies:ModelPolicy[];canBill:boolean;
 onSaved:(name:string)=>void;onActivated:(name:string)=>void;onBack:()=>void;onModels:()=>void;
}) {
 const [providers,setProviders]=useState<{name:string;declared:boolean}[]>();
 const [saved,setSaved]=useState<AgentSetup>();
 const [step,setStep]=useState(name?2:0);
 const [editing,setEditing]=useState(false);
 const [busy,setBusy]=useState(false);
 const [problem,setProblem]=useState("");
 const [template,setTemplate]=useState<AgentTemplate>("analyst");
 const [plan,setPlan]=useState<AgentSetupPlan>({spec:{name:"",description:"",provider:"",model:"",system_prompt:agentTemplates.analyst.prompt,tools:[...agentTemplates.analyst.tools]},project:me.personal_project??"~",rules:{}});
 const [testRequest,setTestRequest]=useState("");
 const [run,setRun]=useState<Run>();
 const [refresh,setRefresh]=useState(0);
 const [reviewQuery,setReviewQuery]=useState("");
 const [reviewers,setReviewers]=useState<DirectoryPerson[]>([]);
 const [reviewer,setReviewer]=useState<DirectoryPerson>();
 const self=!!plan.spec.self_approval;
 useEffect(()=>{
  let active=true;setProblem("");
  Promise.all([api.modelProviders(),name?api.agentSetup(name):Promise.resolve(undefined)]).then(([available,record])=>{
   if(!active)return;setProviders(available.filter(provider=>provider.name!=="offline"));
   if(record){setSaved(record);setPlan(record.plan);setStep(2)}else{
    const declared=available.filter(provider=>provider.declared&&provider.name!=="offline");
    const example=agents.find(agent=>declared.some(provider=>provider.name===agent.spec.provider));
    setPlan(current=>({...current,spec:{...current.spec,provider:example?.spec.provider??declared[0]?.name??"",model:example?.spec.model??""}}));
   }
  }).catch(()=>{if(active)setProblem(words.failed)});
  return()=>{active=false};
 },[name]);
 useEffect(()=>{
  let active=true;const id=saved?.test_run;if(!id){setRun(undefined);return}
  let reading=false;
  const read=async()=>{if(reading)return;reading=true;try{const latest=await api.run(id);if(active)setRun(latest)}catch{if(active)setProblem(words.failed)}finally{reading=false}};
  void read();const timer=setInterval(()=>void read(),1000);
  return()=>{active=false;clearInterval(timer)};
 },[saved?.test_run,refresh]);
 useEffect(()=>{
  let active=true;if(!reviewQuery.trim()){setReviewers([]);return}
  const timer=setTimeout(()=>coworking.people(reviewQuery.trim(),8).then(people=>{if(active)setReviewers(people)}).catch(()=>{if(active)setReviewers([])}),150);
  return()=>{active=false;clearTimeout(timer)};
 },[reviewQuery]);
 const allowedProjects=useMemo(()=>{
  const names=new Set([""]);let project=plan.project==="~"?"":plan.project;
  for(let depth=0;project&&depth<32&&!names.has(project);depth++){names.add(project);project=projects.find(item=>item.name===project)?.parent??""}
  return names;
 },[plan.project,projects]);
 const contextSources=sources.filter(source=>!source.lifecycle?.project||allowedProjects.has(source.lifecycle.project));
 const hasTest=!!saved?.test_run;
 const inFlight=hasTest&&!terminal(run);
 const canEdit=!saved||editing;
 const spec=(next:Partial<AgentSetupPlan["spec"]>)=>setPlan(current=>({...current,spec:{...current.spec,...next}}));
 const changeProvider=(provider:string)=>{const example=agents.find(agent=>agent.spec.provider===provider);spec({provider,model:example?.spec.model??""})};
 const chooseTemplate=(id:AgentTemplate)=>{setTemplate(id);spec({system_prompt:agentTemplates[id].prompt,tools:[...agentTemplates[id].tools]})};
 const report=(record:AgentSetup)=>{setSaved(record);setPlan(record.plan);setEditing(false);setStep(2);if(record.problem)setProblem(words.phaseFailed(words.phase[record.phase]??record.phase));onSaved(record.name)};
 const act=async(action:()=>Promise<AgentSetup>,activate=false)=>{
  setBusy(true);setProblem("");
  try{const record=await action();report(record);if(activate&&record.phase==="ready")onActivated(record.name)}catch(error){setProblem(error instanceof Error?error.message:words.failed)}finally{setBusy(false)}
 };
 const valid=!!plan.spec.name.trim()&&!!plan.spec.provider&&!!plan.spec.model.trim()&&!!plan.spec.system_prompt.trim();
 const realPolicies=policies.filter(policy=>policy.models.length>0&&policy.models.every(model=>model.provider!=="offline"));
 const stage=<ol aria-label={words.saved} className="flex flex-wrap gap-2 text-aui-xs text-aui-muted">{words.steps.map((label,index)=><li key={label}><button type="button" className={`${button} ${step===index?"bg-aui-hover text-aui-fg":""}`} aria-current={step===index?"step":undefined} disabled={busy} onClick={()=>setStep(index)}>{index+1}. {label}</button></li>)}</ol>;
 return <Page title={words.title} lede={words.lede} back={{label:defaultCopy.navigation.agents,onClick:onBack}}>
  <div className="mx-auto flex w-full max-w-4xl flex-col gap-5 pb-8">
  {stage}<Problem message={problem}/>
  {!providers&&!problem&&<SkeletonRows count={3}/>}
  {saved&&<p role="status" className="text-aui-sm"><span className="font-medium">{words.saved}: </span>{words.phase[saved.phase]??saved.phase}</p>}
  {saved?.problem&&<Problem message={words.phaseFailed(words.phase[saved.phase]??saved.phase)}/>}{saved?.problem&&<button type="button" className={`${button} self-start`} disabled={busy} onClick={()=>void act(()=>api.resumeAgentSetup(saved.name))}>{words.resume}</button>}
  {saved&&editing&&<p className="text-aui-xs text-aui-muted">{words.locked}</p>}
  {step===0&&<div className="grid min-w-0 grid-cols-1 gap-4 sm:grid-cols-2">
   <Label text={words.name} hint={words.nameHint}><input className={field} value={plan.spec.name} maxLength={63} disabled={!!saved} onChange={event=>spec({name:event.target.value})}/></Label>
   <Label text={words.description}><input className={field} value={plan.spec.description??""} disabled={!canEdit} onChange={event=>spec({description:event.target.value})}/></Label>
   <Label text={words.template}><select className={field} value={template} disabled={!canEdit} onChange={event=>chooseTemplate(event.target.value as AgentTemplate)}>{Object.keys(agentTemplates).map(key=><option key={key} value={key}>{words.roles[key as AgentTemplate]}</option>)}</select></Label>
   <Label text={words.project} hint={words.projectHint}><select className={field} value={plan.project||"~"} disabled={!!saved} onChange={event=>setPlan(current=>({...current,project:event.target.value}))}><option value="~">{defaultCopy.projects.companyName}</option>{me.personal_project&&!projects.some(project=>project.name===me.personal_project)&&<option value={me.personal_project}>{defaultCopy.projects.personalTitle}</option>}{projects.filter(project=>project.name!=="").map(project=><option key={project.name} value={project.name}>{project.name===me.personal_project?defaultCopy.projects.personalTitle:project.title||project.name}</option>)}</select></Label>
   <Label text={words.provider} hint={words.modelHint}><select className={field} value={plan.spec.provider} disabled={!canEdit} onChange={event=>changeProvider(event.target.value)}><option value="">{words.provider}</option>{providers?.map(provider=><option key={provider.name} value={provider.name} disabled={!provider.declared}>{provider.name}</option>)}{realPolicies.length>0&&<option value="policy">{words.workspaceModelPolicy}</option>}</select></Label>
   <Label text={words.model}>{plan.spec.provider==="policy"?<select className={field} value={plan.spec.model} disabled={!canEdit} onChange={event=>spec({model:event.target.value})}><option value="">{words.model}</option>{realPolicies.map(policy=><option key={policy.name} value={policy.name}>{policy.name}</option>)}</select>:<input className={field} value={plan.spec.model} maxLength={200} disabled={!canEdit} onChange={event=>spec({model:event.target.value})}/>}</Label>
   {providers&&!providers.some(provider=>provider.declared)&&!realPolicies.length&&<div className="sm:col-span-2"><p className="text-aui-sm text-aui-muted">{words.noModels}</p><button type="button" className={button} onClick={onModels}>{words.models}</button></div>}
   <div className="sm:col-span-2"><Label text={words.instructions}><textarea className={field} rows={7} value={plan.spec.system_prompt} disabled={!canEdit} onChange={event=>spec({system_prompt:event.target.value})}/></Label></div>
  </div>}
  {step===1&&<div className="flex min-w-0 flex-col gap-5">
   <fieldset className="space-y-2"><legend className="mb-2 text-aui-sm font-medium">{words.sources}</legend>{!contextSources.length&&<p className="text-aui-xs text-aui-muted">{words.noSources}</p>}{contextSources.map(source=><label key={source.name} className="flex min-h-10 items-center gap-2 text-aui-sm"><input type="checkbox" disabled={!canEdit} checked={!!plan.spec.sources?.some(item=>item.name===source.name)} onChange={event=>spec({sources:event.target.checked?[...(plan.spec.sources??[]),{name:source.name,uri:`knowledge://${source.name}`}]:plan.spec.sources?.filter(item=>item.name!==source.name)})}/>{source.name}</label>)}</fieldset>
   <fieldset className="space-y-2"><legend className="mb-2 text-aui-sm font-medium">{words.tools}</legend><p className="text-aui-xs text-aui-muted">{words.toolsHint}</p><div className="grid grid-cols-1 sm:grid-cols-2">{setupTools.map(tool=><label key={tool} className="flex min-h-10 items-center gap-2 text-aui-sm"><input type="checkbox" disabled={!canEdit} checked={plan.spec.tools?.includes(tool)??false} onChange={event=>spec({tools:event.target.checked?[...(plan.spec.tools??[]),tool]:plan.spec.tools?.filter(item=>item!==tool)})}/>{words.toolNames[tool]}</label>)}</div></fieldset>
   <div className="grid grid-cols-1 gap-4 sm:grid-cols-2"><div><Label text={words.manager} hint={words.managerHint}><input className={field} value={reviewer?reviewer.name||reviewer.handle:reviewQuery||(saved?.plan.manager?words.reviewerSaved:"")} disabled={!!saved} placeholder={words.searchPeople} onChange={event=>{setReviewer(undefined);setReviewQuery(event.target.value);setPlan(current=>({...current,manager:undefined}))}}/></Label>{!reviewer&&reviewers.length>0&&<ul className="max-h-40 overflow-y-auto rounded-aui border border-aui-border">{reviewers.map(person=><li key={person.subject}><button type="button" className={`${button} w-full justify-start`} onClick={()=>{setReviewer(person);setReviewQuery("");setPlan(current=>({...current,manager:person.subject,spec:current.spec.self_approval?{...current.spec,approvers:[...new Set([...(current.spec.approvers??[]),person.subject])]}:current.spec}))}}>{person.name||person.handle}</button></li>)}</ul>}</div><div><label className="flex min-h-10 items-start gap-2 text-aui-sm"><input type="checkbox" className="mt-1" disabled={!canEdit} checked={self} onChange={event=>spec({self_approval:event.target.checked,approvers:event.target.checked?[...new Set([...(plan.spec.approvers??[]),me.principal.subject,...(plan.manager?[plan.manager]:[])])]:(plan.spec.approvers??[]).filter(person=>person!==me.principal.subject)})}/>{words.selfApproval}</label><p className="text-aui-xs text-aui-muted">{words.selfHint}</p></div></div>
   <fieldset className="space-y-2"><legend className="mb-2 text-aui-sm font-medium">{words.policy}</legend><p className="text-aui-xs text-aui-muted">{words.policyHint}</p><div className="grid grid-cols-1 gap-3 sm:grid-cols-2">{categories.map(category=><Label key={category} text={words.categories[category]}><select className={field} value={plan.rules?.[category]??""} disabled={!!saved} onChange={event=>setPlan(current=>{const rules={...current.rules};if(event.target.value)rules[category]=event.target.value as Rules[string];else delete rules[category];return {...current,rules}})}><option value="">{words.inherit}</option>{(["allow","ask","never"] as const).map(level=><option key={level} value={level}>{words.levels[level]}</option>)}</select></Label>)}</div></fieldset>
   <div className="grid grid-cols-1 gap-3 sm:grid-cols-2"><Label text={words.budget} hint={canBill?words.budgetHint:words.budgetReadOnly}><input className={field} type="number" min="0" step="0.01" max="1000000" value={(plan.daily_spend??0)/1e6} disabled={!canBill||!!saved} onChange={event=>setPlan(current=>({...current,daily_spend:Math.round(Number(event.target.value)*1e6)}))}/></Label><Label text={words.lease}><input className={field} type="number" min="0" step="0.01" max="1000000" value={(plan.max_lease??0)/1e6} disabled={!canBill||!!saved} onChange={event=>setPlan(current=>({...current,max_lease:Math.round(Number(event.target.value)*1e6)}))}/></Label></div>
   <p className="text-aui-xs text-aui-muted">{words.assignmentHint}</p>
   {!plan.assignment?<button type="button" className={`${button} self-start`} disabled={!!saved} onClick={()=>setPlan(current=>({...current,assignment:{goal:"",done_when:"",trigger:"manual"}}))}>{words.addAssignment}</button>:<fieldset className="grid grid-cols-1 gap-3 rounded-aui border border-aui-border p-3 sm:grid-cols-2"><Label text={words.goal}><textarea className={field} rows={3} value={plan.assignment.goal} disabled={!!saved} onChange={event=>setPlan(current=>({...current,assignment:{...current.assignment!,goal:event.target.value}}))}/></Label><Label text={words.doneWhen}><textarea className={field} rows={3} value={plan.assignment.done_when} disabled={!!saved} onChange={event=>setPlan(current=>({...current,assignment:{...current.assignment!,done_when:event.target.value}}))}/></Label><Label text={words.trigger}><select className={field} value={plan.assignment.trigger??"manual"} disabled={!!saved} onChange={event=>setPlan(current=>({...current,assignment:{...current.assignment!,trigger:event.target.value as "manual"|"cron"|"source_change",cron:undefined,source:undefined}}))}><option value="manual">{words.manual}</option><option value="cron">{words.cron}</option><option value="source_change">{words.sourceChange}</option></select></Label>{plan.assignment.trigger==="cron"&&<Label text={words.cronExpression}><input className={field} value={plan.assignment.cron??""} disabled={!!saved} onChange={event=>setPlan(current=>({...current,assignment:{...current.assignment!,cron:event.target.value}}))}/></Label>}{plan.assignment.trigger==="source_change"&&<Label text={words.triggerSource}><select className={field} value={plan.assignment.source??""} disabled={!!saved} onChange={event=>setPlan(current=>({...current,assignment:{...current.assignment!,source:event.target.value}}))}><option value="">{words.triggerSource}</option>{contextSources.map(source=><option key={source.name} value={source.name}>{source.name}</option>)}</select></Label>}<button type="button" className={`${button} justify-self-start`} disabled={!!saved} onClick={()=>setPlan(current=>({...current,assignment:undefined}))}>{words.removeAssignment}</button></fieldset>}
  </div>}
  {step===2&&<div className="flex flex-col gap-4">
   <dl className="grid grid-cols-1 gap-3 text-aui-sm sm:grid-cols-2"><div><dt className="text-aui-xs text-aui-muted">{words.name}</dt><dd>{plan.spec.name}</dd></div><div><dt className="text-aui-xs text-aui-muted">{words.model}</dt><dd>{plan.spec.provider} / {plan.spec.model}</dd></div><div><dt className="text-aui-xs text-aui-muted">{words.project}</dt><dd>{plan.project===me.personal_project?defaultCopy.projects.personalTitle:projects.find(project=>project.name===plan.project)?.title||((plan.project==="~"||!plan.project)?defaultCopy.projects.companyName:plan.project)}</dd></div></dl>
   {!saved?<button type="button" className={`${primary} self-start`} disabled={busy||!valid} onClick={()=>void act(()=>api.createAgentSetup(plan))}>{busy?words.saving:words.save}</button>:<>
    {saved.phase==="configured"&&<><button type="button" className={`${button} self-start`} disabled={busy||inFlight} onClick={()=>{setEditing(true);setStep(0)}}>{words.edit}</button>{inFlight&&<p className="text-aui-xs text-aui-muted">{words.stopBeforeEdit}</p>}<Label text={words.testRequest} hint={words.testHint}><textarea className={field} rows={3} value={testRequest} placeholder={words.testPlaceholder} onChange={event=>setTestRequest(event.target.value)} maxLength={8000}/></Label><button type="button" className={`${button} self-start`} disabled={busy||inFlight||!testRequest.trim()} onClick={()=>void act(()=>api.testAgentSetup(saved.name,testRequest))}>{words.test}</button></>}
    {run&&<TestResult run={run} onRefresh={()=>setRefresh(value=>value+1)}/>}
    {saved.phase!=="ready"&&<button type="button" className={`${primary} self-start`} disabled={busy||run?.status!=="completed"} onClick={()=>void act(()=>api.activateAgentSetup(saved.name),true)}>{words.activate}</button>}
    {saved.phase==="ready"&&<p role="status" className="text-aui-sm">{words.ready}</p>}
   </>}
  </div>}
  {editing&&saved&&<button type="button" className={`${primary} self-start`} disabled={busy||!valid||inFlight} onClick={()=>void act(()=>api.amendAgentSetup(saved.name,saved.version,{...saved.plan,spec:plan.spec}))}>{words.saveCorrection}</button>}
  <div className="flex flex-wrap gap-2 border-aui-border border-t pt-4"><button type="button" className={button} onClick={step>0?()=>setStep(step-1):onBack}>{step>0?words.back:words.cancel}</button>{step<2&&<button type="button" className={primary} disabled={step===0&&!valid} onClick={()=>setStep(step+1)}>{words.next}</button>}</div>
  </div>
 </Page>;
}
