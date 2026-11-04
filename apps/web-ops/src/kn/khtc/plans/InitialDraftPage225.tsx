"use client";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { ApiProblem } from "@/api/problem";
import { newIdempotencyKey } from "@/api/idempotency";
import { ProblemAlert } from "@/components/ProblemAlert";
import { DetailPage, LogPanel } from "@/components/patterns/detail";
import { LoadState, ReadOnlyNotice, useAction, useLoad } from "@/kn/cycle/common";
import { blankEntry, childrenOf, removeSubtree, toSave, useKhtcApi, useKhtcRights, type KhtcPlanEntry, type KhtcTab } from "../api";
import { createDraftApi225, type InitialDraft225, type InitialDraftState225, type InitialDraftSave225 } from "./draft-api225";
import { EntryPage } from "./EntryPages";
import { entryDraft, type EntryDraft } from "./entry-form";
import { ProgramPage, type ProgramDraft } from "./ProgramPages";
import { PlanTree } from "./PlanTree";
import { entryHref, newEntryHref, planHref, programHref, type PlanRoute } from "./routes";

const HELP="Thay đổi trên màn chưa ghi máy chủ. Dùng Lưu nháp máy chủ để xác nhận; không tạo phiên bản áp dụng. Log là các thao tác nháp đã xác nhận.";
type Pending={key:string;body:InitialDraftSave225;discard:boolean;edit:number};
const draftClients225=new WeakMap<object,number>();let nextDraftClient225=0;
function draftAuthority225(auth:ReturnType<typeof useAuth>){
 let client=draftClients225.get(auth.api);if(client===undefined){client=++nextDraftClient225;draftClients225.set(auth.api,client);}
 const p=auth.principal,k=auth.kn.context;
 return JSON.stringify([client,auth.status,!!auth.scopeWarning,p?.subject,p?.tenantId,p?.orgRef,p?.mfa,p?.level,[...(p?.roles??[])].sort(),p?.scopeAll,[...(p?.scopeUnits??[])].sort(),auth.kn.status,k?.context_id,k?.assignment_id,k?.role,k?.department_code,k?.org_ref,(k?.communes??[]).map(x=>x.code).sort(),!!k?.read_only,!!k?.manager,k?.mgmt_level_code,k?.province_group,k?.internal_unit_code,[...(k?.duties??[])].sort(),[...(k?.powers??[])].sort(),k?.support_session_id,k?.on_behalf_of]);
}
export function InitialDraftPage225({route}:{route:PlanRoute}){
 const auth=useAuth();const c=auth.kn.context;
 const canRead=auth.status==="authenticated"&&auth.kn.status==="active"&&!auth.scopeWarning&&!!c&&(c.role==="admin"||(c.role==="kn_tinh"&&c.department_code==="ke_hoach_tai_chinh"));
 return canRead?<DraftResource225 key={JSON.stringify([draftAuthority225(auth),route.year])} route={route}/>:<ReadOnlyNotice>Nháp chưa áp dụng chỉ dành cho Admin và Phòng Kế hoạch – Tài chính. Kế hoạch đã áp dụng vẫn xem ở màn kế hoạch năm.</ReadOnlyNotice>;
}
function DraftResource225({route}:{route:PlanRoute}){
 const auth=useAuth();const api=useMemo(()=>createDraftApi225(auth.api),[auth.api]);const khtc=useKhtcApi();
 const s=useLoad(async()=>{const [state,programs,catalog,history]=await Promise.all([api.current(route.year),khtc.programs(true),khtc.catalog(),api.history(route.year)]);return {state,programs:programs.items,catalog:catalog.items,history};},[api,khtc,route.year]);
 // A failed read is not a mutation rollback. Retain only this authority/year
 // owner's last accepted resource so a known ACK and unsaved tree stay visible.
 const [accepted,setAccepted]=useState<typeof s.data>(null);
 if(s.data&&s.data!==accepted)setAccepted(s.data);
 const current=s.data??accepted;
 return <>{current&&s.problem?<ProblemAlert problem={s.problem}/>:null}<LoadState s={{...s,data:current,problem:current?null:s.problem}}>{data=><DraftEditor225 route={route} data={data} api={api} reload={s.reload}/>}</LoadState></>;
}
type Resource=Awaited<ReturnType<ReturnType<typeof createDraftApi225>["current"]>>;
function DraftEditor225({route,data,api,reload}:{route:PlanRoute;data:{state:Resource;programs:Awaited<ReturnType<ReturnType<typeof useKhtcApi>["programs"]>>["items"];catalog:Awaited<ReturnType<ReturnType<typeof useKhtcApi>["catalog"]>>["items"];history:Awaited<ReturnType<ReturnType<typeof createDraftApi225>["history"]>>};api:ReturnType<typeof createDraftApi225>;reload:()=>void}){
 const khtc=useKhtcApi();const rights=useKhtcRights();
 const [head,setHead]=useState<InitialDraftState225>(data.state);const headRef=useRef(head);useLayoutEffect(()=>{headRef.current=head;},[head]);
 const [entries,setEntries]=useState<KhtcPlanEntry[]>(data.state.draft?.entries??[]);
 const [dirty,setDirty]=useState(false);const dirtyRef=useRef(dirty);useLayoutEffect(()=>{dirtyRef.current=dirty;},[dirty]);const [formDirty,setFormDirty]=useState<Record<string,boolean>>({});const hasFormDirty=Object.values(formDirty).some(Boolean);const edit=useRef(0);const alive=useRef(true);
 useLayoutEffect(()=>{alive.current=true;return()=>{alive.current=false;};},[]);
 const [pending,setPending]=useState<Pending|null>(null);const pendingRef=useRef<Pending|null>(null);
 const [busy,setBusy]=useState(false);const busyRef=useRef(false);const [problem,setProblem]=useState<ApiProblem|null>(null);const [notice,setNotice]=useState("");
 const [tab,setTab]=useState<KhtcTab>(route.tab??"programs");const [drafts,setDrafts]=useState<Record<string,EntryDraft>>({});
 const [programDraft,setProgramDraft]=useState<ProgramDraft>({code:"",name:"",kind:"program"});const programAction=useAction();
 useEffect(()=>{if(!dirtyRef.current&&!pendingRef.current){setHead(data.state);setEntries(data.state.draft?.entries??[]);}},[data.state]);
 const [older,setOlder]=useState<{source:typeof data.history;page:typeof data.history}|null>(null);const history=older?.source===data.history?older.page:data.history;const historySource=useRef(data.history);useLayoutEffect(()=>{historySource.current=data.history;},[data.history]);
 const canEdit=rights.plan&&head.available&&!busy&&!pending;
 const change=(next:KhtcPlanEntry[])=>{if(!canEdit)return;edit.current++;setEntries(next);setDirty(true);};
 const back=()=>route.go(planHref(route.base,route.year));
 const submit=async(discard=false)=>{
  if(busyRef.current||!rights.plan)return;
  let op=pendingRef.current;
  if(!op){if(hasFormDirty||!head.available||!Number.isSafeInteger(head.revision))return;if(discard&&!head.draft)return;
   op={key:newIdempotencyKey(),body:{expected_revision:head.revision,...(head.draft?{draft_id:head.draft.draft_id}:{}),entries:discard?[]:toSave(entries,{base_version:0}).entries},discard,edit:edit.current};pendingRef.current=op;setPending(op);
  }
  busyRef.current=true;setBusy(true);setProblem(null);setNotice("");
  try{
   let ack:InitialDraft225;
   if(op.discard){if(!op.body.draft_id)throw new Error("missing generation");ack=await api.discard(route.year,{draft_id:op.body.draft_id,expected_revision:op.body.expected_revision},op.key);}
   else ack=await api.save(route.year,op.body,op.key);
   if(!alive.current||pendingRef.current!==op)return;
   const stillOwn=headRef.current.revision===op.body.expected_revision&&(headRef.current.draft?.draft_id??null)===(op.body.draft_id??null);
   pendingRef.current=null;setPending(null);
   setNotice(`Đã xác nhận ${ack.action==="discard"?"hủy nháp":"lưu nháp máy chủ"} — thế hệ ${ack.draft_id}, lần ghi ${ack.revision}. Đây không phải phiên bản áp dụng.`);
   if(stillOwn){setHead({...headRef.current,revision:ack.revision,draft:ack.action==="discard"?null:ack});if(edit.current===op.edit){setEntries(ack.action==="discard"?[]:ack.entries);setDirty(false);setDrafts({});}}
   reload();
  }catch(e){if(!alive.current)return;if(e instanceof ApiProblem){setProblem(e);if(e.status>=400&&e.status<500&&e.status!==429&&e.code!=="idempotency_in_progress"){pendingRef.current=null;setPending(null);}}else setNotice("Chưa biết kết quả ghi máy chủ. Thử lại đúng thao tác để lấy xác nhận, không tạo thao tác mới.");}
  finally{busyRef.current=false;if(alive.current)setBusy(false);}
 };
 const logs=history.items.map(e=>({at:e.event_time,actor_display:e.actor_ref,event:`${e.action} — thế hệ ${e.draft_id}; lần ghi ${e.revision}`,reason:e.note||undefined}));
 const shell=<><h1>Nháp máy chủ ban đầu — năm {route.year}</h1><p>Chỉ dùng cho năm chưa có phiên bản hoặc tham chiếu kỳ. Thay đổi trên màn, nháp máy chủ và phiên bản áp dụng là ba việc khác nhau.</p>
 <div className="row"><button className="btn btn-o" onClick={()=>route.go(planHref(route.base.replace(/\/nhap-may-chu$/,""),route.year))}>Kế hoạch đã áp dụng</button><button className="btn btn-o" onClick={back}>Cây nháp</button><button className="btn btn-o" onClick={()=>route.go(planHref(route.base,route.year,"history"))}>Log nháp máy chủ</button><button className="btn btn-o" onClick={()=>route.go(`${route.base}/so-sanh?year=${route.year}`)}>So sánh với nền rỗng</button></div>
 <p role="status">{head.draft?`Đã nhận nháp máy chủ lần ghi ${head.revision}.`:`Chưa có nháp máy chủ đang giữ; mốc ghi ${head.revision}.`} {dirty||hasFormDirty?"Có thay đổi trên màn chưa lưu.":"Màn khớp lần xác nhận đã nhận."}</p>
 {!head.available?<p role="alert">Năm không còn đủ điều kiện tạo nháp ban đầu: {head.unavailable_reason||"đã có dữ liệu tham chiếu"}.</p>:null}
 {hasFormDirty?<p role="note">Có ô đang chỉnh sửa chưa đưa vào cây. Mở lại dòng và bấm Lưu vào nháp trước khi lưu máy chủ; Hủy chỉ quay lại, không hủy nháp máy chủ.</p>:null}{problem?<ProblemAlert problem={problem}/>:null}{notice?<p role="status">{notice}</p>:null}
 <div className="row">{pending?<button className="btn btn-p" disabled={busy} onClick={()=>void submit()}>{busy?"Đang chờ xác nhận…":"Thử lại đúng thao tác chưa xác nhận"}</button>:<><button className="btn btn-p" disabled={!canEdit||hasFormDirty} onClick={()=>void submit()}>{head.draft?"Lưu nháp máy chủ":"Tạo nháp máy chủ"}</button><button className="btn btn-o" disabled={!canEdit||hasFormDirty||!head.draft} onClick={()=>void submit(true)}>Hủy đúng nháp máy chủ lần {head.revision}</button></>}
 <button className="btn btn-o" disabled={busy||!!pending} onClick={()=>{setHead(data.state);setEntries(data.state.draft?.entries??[]);setDirty(false);setDrafts({});setFormDirty({});edit.current++;reload();}}>Bỏ thay đổi trên màn và tải lại</button><button className="btn btn-o" disabled>Áp dụng chưa được hỗ trợ cho nháp này</button></div>
 {!rights.plan?<ReadOnlyNotice>Chỉ xem nháp trong ngữ cảnh hiện tại.</ReadOnlyNotice>:null}</>;
 let content;
 const key=[route.year,route.view,route.ref,route.parent,route.kind,route.tab].join(":");
 const routeIdentity=useRef(key);useLayoutEffect(()=>{routeIdentity.current=key;},[key]);
 const selected=entries.find(e=>e.ref===route.ref);const parent=entries.find(e=>e.ref===route.parent);
 const fresh=useMemo(()=>route.view==="new-entry"&&parent&&parent.kind!=="item"&&(route.kind!=="group"||parent.kind==="program")&&parent.tab===route.tab?blankEntry(parent.tab,route.kind!,parent,Math.min(100000,(childrenOf(entries,parent.ref).length+1)*10)):null,
 // A form ref is stable through its local edits and acknowledged head updates.
 // eslint-disable-next-line react-hooks/exhaustive-deps
 [key]);
 const current=(selected||fresh)?drafts[key]??entryDraft((selected||fresh)!):null;
 if(["entry","edit-entry","new-entry"].includes(route.view))content=current?<EntryPage key={key} route={route} draft={current} onChange={v=>{setDrafts(old=>({...old,[key]:v}));setFormDirty(old=>({...old,[key]:true}));}} canEdit={canEdit} catalog={data.catalog} parentName={entries.find(e=>e.ref===current.entry.parent_ref)?.name} log={logs} draftHelp={HELP} onSave={e=>{if(!canEdit)return;setFormDirty(old=>({...old,[key]:false}));change(entries.some(x=>x.ref===e.ref)?entries.map(x=>x.ref===e.ref?e:x):[...entries,e]);back();}} onCancel={back}/>:<p role="alert">Không tìm thấy dòng hoặc cấp cha hợp lệ trong cây.</p>;
 else if(route.view==="programs"||route.view==="new-program")content=<ProgramPage route={route} programs={data.programs} used={new Set(entries.filter(e=>e.tab===(route.tab??"programs")&&e.kind==="program").map(e=>e.program_ref??""))} draft={programDraft} onChange={setProgramDraft} canEdit={canEdit} busy={programAction.busy} problem={programAction.problem} log={logs} draftHelp={HELP} onPick={p=>{if(!canEdit||!p.active)return;const t=route.tab??"programs";const e=blankEntry(t,"program",null,Math.min(100000,(childrenOf(entries.filter(e=>e.tab===t),null).length+1)*10));change([...entries,{...e,program_ref:p.ref,code:p.code,name:p.name}]);setTab(t);back();}} onCreate={async()=>{if(!canEdit)return;const p=await programAction.run(()=>khtc.createProgram(programDraft));if(p&&alive.current){reload();if(routeIdentity.current!==key)return;route.go(programHref(route.base,route.year,route.tab??"programs"));}}}/>;
 else if(route.view==="history")content=<DetailPage title="Log nháp máy chủ" back={{href:planHref(route.base,route.year),label:"Cây nháp"}}><LogPanel entries={logs}/>{history.next_before_revision?<button className="btn btn-o" onClick={async()=>{const source=data.history;try{const page=await api.history(route.year,history.next_before_revision!);if(alive.current&&historySource.current===source)setOlder({source,page:{...page,items:[...history.items,...page.items]}});}catch(e){if(alive.current&&e instanceof ApiProblem)setProblem(e);}}}>Xem các lần ghi trước</button>:null}</DetailPage>;
 else content=<><p>{route.view==="version"?"Nền so sánh có 0 dòng và 0 phiên bản áp dụng; cây dưới là thay đổi trên màn.":HELP}</p>{rights.plan?<button className="btn btn-o" disabled={!canEdit} onClick={()=>route.go(programHref(route.base,route.year,tab))}>Chọn CT/DA vào tab</button>:null}<PlanTree entries={entries} initialTab={tab} onTabChange={setTab} actions={e=><><button className="btn btn-o" onClick={()=>route.go(entryHref(route.base,route.year,e.ref))}>Xem</button>{canEdit?<><button className="btn btn-o" onClick={()=>route.go(entryHref(route.base,route.year,e.ref,true))}>Sửa</button>{e.kind!=="item"?<><button className="btn btn-o" onClick={()=>route.go(newEntryHref(route.base,route.year,e.tab,e.ref,"item"))}>Thêm hạng mục</button>{e.kind==="program"?<button className="btn btn-o" onClick={()=>route.go(newEntryHref(route.base,route.year,e.tab,e.ref,"group"))}>Thêm nhóm</button>:null}</>:null}<button className="btn btn-o" onClick={()=>change(removeSubtree(entries,e.ref))}>Bỏ dòng khỏi màn</button></>:null}</>}/></>;
 return <>{shell}{content}</>;
}
