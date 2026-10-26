import {defaultCopy} from "@humagen/components";
import {useEffect,useState} from "react";
import {api,type Thread} from "./api.ts";
import type {Run} from "./adapter.ts";
import {ForkConversation,BringBackResult} from "./ForkConversation.tsx";
import {Inspector} from "@humagen/web";
import {Problem} from "./ui.tsx";

export interface ConversationForkActions {
 point?:string;
 bringBack?:string;
 onFork?:(point:string)=>void;
 onBringBack?:(run:string)=>void;
 onClose?:()=>void;
 onOpenThread?:(id:string)=>void;
}
/** Route-selected review panels. A loaded result must belong to this thread. */
export function ConversationForkPanels({thread,runs,actions}:{thread:Thread;runs:Run[];actions?:ConversationForkActions}) {
 const [selected,setSelected]=useState<Run>();
 const [failed,setFailed]=useState(false);
 const runID=actions?.bringBack;
 useEffect(()=>{
  let live=true;setSelected(undefined);setFailed(false);
  if(!runID)return;
  const found=runs.find(run=>run.id===runID);
  Promise.resolve(found??api.run(runID)).then(run=>{
   if(!live)return;
   if(run.thread!==thread.id||run.status!=="completed"){setFailed(true);return}
   setSelected(run);
  }).catch(()=>{if(live)setFailed(true)});
  return()=>{live=false};
 },[runID,thread.id,runs]);
 if(!actions||thread.read_only)return null;
 if(actions.point)return <ForkConversation thread={thread} through={actions.point} onClose={()=>actions.onClose?.()} onForked={next=>actions.onOpenThread?.(next.id)}/>;
 if(runID&&thread.fork){
  if(selected)return <BringBackResult thread={thread} run={selected.id} text={selected.answer??""} onClose={()=>actions.onClose?.()} onPosted={()=>actions.onOpenThread?.(thread.fork!.source_thread)}/>;
  return <Inspector open title={defaultCopy.forking.bringBack} onClose={()=>actions.onClose?.()}>{failed?<Problem message={defaultCopy.forking.bringFailed}/>:<p className="text-aui-sm text-aui-muted">{defaultCopy.forking.loading}</p>}</Inspector>;
 }
 return null;
}
