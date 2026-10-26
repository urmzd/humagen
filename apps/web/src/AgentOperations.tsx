import {defaultCopy} from "@humagen/components";
import {useEffect,useState} from "react";
import {api,type AgentControl} from "./api.ts";
import {href} from "./route.ts";
import {button,Problem} from "./ui.tsx";
import {MessageTime} from "./MessageTime.tsx";
const words=defaultCopy.agentOperations;
/** Current operational state comes from the bounded, read-confined roster. */
export function AgentOperations({name,revision,canReadRuns,canWrite,onRun}:{name:string;revision:number;canReadRuns:boolean;canWrite:boolean;onRun:(id:string)=>void}){
 const [control,setControl]=useState<AgentControl>();
 const [draft,setDraft]=useState(false);
 const [staleSkills,setStaleSkills]=useState(false);
 const [problem,setProblem]=useState("");
 const [refresh,setRefresh]=useState(0);
 useEffect(()=>{let live=true;const read=()=>api.agentRoster([name]).then(result=>{if(live){setControl(result[name]);setProblem("")}}).catch(()=>{if(live)setProblem(words.failed)});const skills=()=>api.agentSkillDefaults(name).then(bindings=>{if(live)setStaleSkills(bindings.version>0&&bindings.agent_revision!==revision)}).catch(()=>{});void read();void skills();const timer=setInterval(()=>{void read();void skills()},5000);if(canWrite)api.agentSetup(name).then(setup=>{if(live)setDraft(setup.phase!=="ready")}).catch(()=>{});return()=>{live=false;clearInterval(timer)}},[name,revision,canWrite,refresh]);
 const needsReview=control?.needs_skill_review===true||staleSkills;
 return <section aria-label={words.title} className="flex flex-col gap-3 rounded-aui border border-aui-border p-3">
 <div className="flex flex-wrap items-center gap-3 text-aui-sm"><output>{control?words.states[control.state??"ready"]:words.loading}</output>{control?.manager_name&&<span>{words.manager(control.manager_name)}</span>}{control?.last_run_at&&<MessageTime at={control.last_run_at}/>}</div>
 {needsReview&&<p role="status" className="text-aui-sm text-aui-warn">{words.skillsReview} <a className="underline" href={href({page:"agent",name,tab:"context"})}>{words.reviewSkills}</a></p>}
 <Problem message={problem}/>{problem&&<button type="button" className={`${button} self-start`} onClick={()=>setRefresh(value=>value+1)}>{defaultCopy.retry}</button>}
 <nav aria-label={words.title} className="flex flex-wrap gap-2"><a className={button} href={href({page:"agent-control",agent:name})}>{defaultCopy.navigation.controls}</a><a className={button} href={href({page:"action-policy",scope:"agent",name})}>{defaultCopy.navigation.policies}</a>{canReadRuns&&<a className={button} href={href({page:"assignments",agent:name})}>{words.addAssignment}</a>}{control?.last_run&&canReadRuns&&<button type="button" className={button} onClick={()=>onRun(control.last_run!)}>{words.lastRun}</button>}{draft&&<a className={button} href={href({page:"agent-setup",name})}>{defaultCopy.agentSetup.resume}</a>}</nav>
 </section>
}
