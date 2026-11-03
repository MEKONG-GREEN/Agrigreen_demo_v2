"use client";

import { useId, useState } from "react";
import { SelectField, TextField, validateForm } from "@/components/form";
import { DetailPage, LogPanel, type LogEntry } from "@/components/patterns/detail";
import { DataTable } from "@/components/patterns/list";
import { ProblemAlert } from "@/components/ProblemAlert";
import { fieldErrorMap, type ApiProblem } from "@/api/problem";
import { PROGRAM_KINDS, PROGRAM_KIND_LABEL, TAB_KIND, type KhtcProgram, type KhtcProgramKind } from "../api";
import { planHref, programHref, type PlanRoute } from "./routes";

export interface ProgramDraft { code:string; name:string; kind:KhtcProgramKind }
export function ProgramPage({route,programs,used,draft,onChange,onCreate,onPick,busy,problem,canEdit,log}: {
  route:PlanRoute;programs:KhtcProgram[];used:Set<string>;draft:ProgramDraft;onChange:(d:ProgramDraft)=>void;onCreate:()=>Promise<void>;
  onPick:(p:KhtcProgram)=>void;busy:boolean;problem:ApiProblem|null;canEdit:boolean;log:LogEntry[];
}) {
  const server=problem?fieldErrorMap(problem):{};
  const change=(next:ProgramDraft,field:string)=>{onChange(next);setErrors(old=>{const out={...old};delete out[field];return out;});};
  const id=useId();const [errors,setErrors]=useState<Record<string,string>>({});const [all,setAll]=useState(false);
  const tab=route.tab??"programs";const creating=route.view==="new-program";
  const back={href:planHref(route.base,route.year),label:"Kế hoạch năm"};
  const choices=programs.filter(p=>p.active&&!used.has(p.ref)&&(all||!TAB_KIND[tab]||p.kind===TAB_KIND[tab]));
  return <DetailPage title={creating?"CT/DA chủ mới":"Chọn CT/DA vào tab"} back={back} log={<LogPanel entries={log}/>} actions={<><button type="button" className="btn btn-o" disabled={busy} onClick={()=>route.go(planHref(route.base,route.year))}>Hủy</button>{canEdit?(creating?<button type="submit" form={id} className="btn btn-p" disabled={busy}>Lưu CT/DA chủ</button>:<button type="button" className="btn btn-p" onClick={()=>route.go(programHref(route.base,route.year,tab,true))}>CT/DA mới…</button>):null}</>}>
    {!canEdit?<p role="alert">Chỉ xem — không có quyền lập kế hoạch.</p>:creating?<>{problem?<ProblemAlert problem={problem}/>:null}<form id={id} noValidate onSubmit={event=>{event.preventDefault();if(busy)return;const checked=validateForm({code:draft.code,name:draft.name},{code:{kind:"text",required:true,maxLength:40},name:{kind:"text",required:true}});if(Array.from(draft.name.trim()).length>500)checked.errors.name="Tối đa 500 ký tự.";setErrors(checked.errors);if(Object.keys(checked.errors).length===0)void onCreate();}}>
      <TextField label="Mã CT/DA" required maxLength={40} disabled={busy} value={draft.code} error={errors.code||server.code} onChange={code=>change({...draft,code},"code")}/>
      <TextField label="Tên CT/DA" required maxLength={1000} disabled={busy} value={draft.name} error={errors.name||server.name} onChange={name=>change({...draft,name},"name")}/>
      <SelectField label="Loại CT/DA" disabled={busy} value={draft.kind} options={PROGRAM_KINDS.map(value=>({value,label:PROGRAM_KIND_LABEL[value]}))} onChange={kind=>onChange({...draft,kind:kind as KhtcProgramKind})}/>
    </form><p className="mute">Lưu tạo CT/DA chủ thật. Chọn vào cây sau đó vẫn là nháp cho tới khi lưu phiên bản.</p></>:<><label className="row"><input type="checkbox" checked={all} onChange={e=>setAll(e.target.checked)}/>Hiện mọi loại</label><DataTable caption="CT/DA chủ có thể chọn" rows={choices} rowKey={p=>p.ref} columns={[{key:"code",header:"Mã",cell:p=>p.code},{key:"name",header:"Tên",cell:p=>p.name},{key:"kind",header:"Loại",cell:p=>PROGRAM_KIND_LABEL[p.kind]},{key:"pick",header:"Thao tác",cell:p=><button type="button" className="btn btn-o" onClick={()=>onPick(p)}>Chọn {p.code}</button>}]} /></>}
  </DetailPage>;
}
