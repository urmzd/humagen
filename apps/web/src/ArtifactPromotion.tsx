import { defaultCopy } from "@humagen/components";
import { useEffect,useState } from "react";
import { href } from "./route.ts";
import type { Stored } from "./storage.ts";
import { button,field,Label,Problem } from "./ui.tsx";

const words=defaultCopy.conversation;
export interface ArtifactPromotionAccess {project:string;shared:boolean;knowledge:boolean;memory:boolean}
interface Source {name:string;lifecycle?:{mode:string;provider:string;project?:string}}

/** Explicit acceptance copies one immutable version, never private raw history. */
export function ArtifactPromotion({file,access}:{file:Stored;access:ArtifactPromotionAccess}) {
 const [sources,setSources]=useState<Source[]>();
 const [source,setSource]=useState("");
 const [accepted,setAccepted]=useState(false);
 const [busy,setBusy]=useState(false);
 const [problem,setProblem]=useState("");
 const [saved,setSaved]=useState("");
 const [retry,setRetry]=useState(0);
 useEffect(()=>{
  if(!access.shared||!access.knowledge)return;
  const abort=new AbortController();setSources(undefined);setProblem("");
  fetch("/api/knowledge",{credentials:"same-origin",signal:abort.signal}).then(async response=>{
   if(!response.ok)throw new Error(words.artifactSourceFailed);
   const body=await response.json() as {sources:Source[]};
   if(!abort.signal.aborted)setSources((body.sources??[]).filter(item=>item.lifecycle?item.lifecycle.mode==="indexed"&&item.lifecycle.provider==="manual"&&(item.lifecycle.project??"")===access.project:access.project===""));
  }).catch(()=>{if(!abort.signal.aborted)setProblem(words.artifactSourceFailed)});
  return ()=>abort.abort();
 },[access.shared,access.knowledge,access.project,retry]);
 if(!access.knowledge&&!access.memory)return null;
 if(!access.shared)return <p className="text-aui-xs text-aui-muted">{words.artifactSharedRequired}</p>;
 const promote=async(kind:"knowledge"|"memory")=>{
  setBusy(true);setProblem("");setSaved("");
  try {
   const response=await fetch(`/api/artifacts/${encodeURIComponent(file.id)}/promote-${kind}`,{method:"POST",credentials:"same-origin",headers:{"Content-Type":"application/json"},body:JSON.stringify({version:file.version,accepted:true,...(kind==="knowledge"?{source}:{})})});
   const body=await response.json().catch(()=>null);
   if(!response.ok)throw new Error(body?.error||words.artifactPromotionFailed);
   setSaved(kind==="knowledge"?words.artifactPromoted:words.artifactMemoryPending);
   setAccepted(false);
  }catch(error){setProblem(error instanceof Error?error.message:words.artifactPromotionFailed)}finally{setBusy(false)}
 };
 return <section aria-label={words.artifactUse} className="flex min-w-0 flex-col gap-3 border-aui-border border-t pt-4">
  <h3 className="font-medium text-aui-sm">{words.artifactUse}</h3>
  <label className="flex min-h-10 items-start gap-2 text-aui-xs"><input type="checkbox" className="mt-0.5 shrink-0" checked={accepted} onChange={event=>setAccepted(event.target.checked)} disabled={busy}/><span>{words.artifactAccept}</span></label>
  {access.knowledge && <>
   {sources?.length ? <Label text={words.artifactSource}><select className={field} value={source} onChange={event=>setSource(event.target.value)} disabled={busy}><option value="">{words.artifactChooseSource}</option>{sources.map(item=><option key={item.name} value={item.name}>{item.name}</option>)}</select></Label> : sources ? <p className="text-aui-xs text-aui-muted">{words.artifactNoSource} <a className="underline" href={href({page:"project",name:access.project,tab:"context"})}>{words.artifactOpenContext}</a></p> : <p className="text-aui-xs text-aui-muted">{words.artifactSourcesLoading}</p>}
   <button type="button" className={`${button} self-start`} disabled={!accepted||!source||busy} onClick={()=>promote("knowledge")}>{busy?words.artifactPromotionSaving:words.artifactAddKnowledge}</button>
  </>}
  {access.memory && <button type="button" className={`${button} self-start`} disabled={!accepted||busy} onClick={()=>promote("memory")}>{busy?words.artifactPromotionSaving:words.artifactSuggestMemory}</button>}
  <Problem message={problem}/>
  {problem===words.artifactSourceFailed&&<button type="button" className={`${button} self-start`} onClick={()=>setRetry(value=>value+1)}>{words.artifactSourceRetry}</button>}
  {saved&&<output className="text-aui-xs text-aui-muted">{saved}</output>}
 </section>;
}
