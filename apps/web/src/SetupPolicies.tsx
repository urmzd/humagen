import {defaultCopy} from "@humagen/components";
import {useEffect,useState} from "react";
import {api,type ActionPolicy} from "./api.ts";
import type {AgentSetupPlan} from "../../../packages/client/src/generated/types.ts";
import {button,field,Label,Problem} from "./ui.tsx";
const words=defaultCopy.operations;
const categories=["read_context","write_files","publish_artifacts","external_messages","call_agents","other"] as const;
type Rules=NonNullable<AgentSetupPlan["rules"]>;
type Level="allow"|"ask"|"never";
const rank=(level?:string)=>level==="never"?3:level==="ask"?2:level==="allow"?1:0;
/** A future agent inherits the actual home chain and the current person's restrictions. */
export function SetupPolicies({home,rules,disabled,onChange}:{home:string;rules:Rules;disabled:boolean;onChange:(rules:Rules)=>void}){
 const [policies,setPolicies]=useState<ActionPolicy[]>();
 const [problem,setProblem]=useState("");
 const [refresh,setRefresh]=useState(0);
 useEffect(()=>{let active=true,reading=false;setPolicies(undefined);setProblem("");const read=async()=>{if(reading)return;reading=true;try{const current=await Promise.all([home&&home!=="~"?api.actionPolicy("project",home):api.actionPolicy("workspace"),api.actionPolicy("personal")]);if(active){setPolicies(current);setProblem("")}}catch{if(active)setProblem(defaultCopy.agentSetup.policyUnknown)}finally{reading=false}};void read();const timer=setInterval(()=>void read(),5000);return()=>{active=false;clearInterval(timer)}},[home,refresh]);
 return <fieldset className="space-y-2"><legend className="mb-2 text-aui-sm font-medium">{defaultCopy.agentSetup.policy}</legend><p className="text-aui-xs text-aui-muted">{defaultCopy.agentSetup.policyHint}</p><Problem message={problem}/>{problem&&<button type="button" className={button} onClick={()=>setRefresh(value=>value+1)}>{defaultCopy.retry}</button>}
 <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">{categories.map(category=>{
  const inherited=policies?.reduce<Level|undefined>((level,policy)=>{const next=policy.effective?.[category]?.level??policy.rules[category];return rank(next)>rank(level)?next:level},undefined);
  const sources=[...new Set(policies?.flatMap(policy=>policy.effective?.[category]?.sources.filter(source=>rank(source.level)===rank(inherited)).map(source=>source.label)||[])??[])];
  const selected=inherited==="never"?"never":rules[category]&&rank(rules[category])<rank(inherited)?inherited:rules[category]??"";
  const localStricter=rank(rules[category])>rank(inherited);
  const effective=localStricter?rules[category]:inherited;
  const labels=localStricter?[defaultCopy.agentSetup.draftPolicy]:sources;
  return <div key={category} className="flex flex-col gap-1"><Label text={defaultCopy.agentSetup.categories[category]}><select className={field} value={selected} disabled={disabled||!policies||inherited==="never"} onChange={event=>{const next={...rules};if(event.target.value)next[category]=event.target.value as Level;else delete next[category];onChange(next)}}><option value="">{defaultCopy.agentSetup.inherit}</option>{(["allow","ask","never"] as const).map(level=><option key={level} value={level} disabled={rank(level)<rank(inherited)}>{words[level]}</option>)}</select></Label>{effective&&<p className="text-aui-xs text-aui-muted">{words.effective}: {words[effective]}{labels.length?` · ${labels.join(", ")}`:""}</p>}</div>
 })}</div></fieldset>
}
