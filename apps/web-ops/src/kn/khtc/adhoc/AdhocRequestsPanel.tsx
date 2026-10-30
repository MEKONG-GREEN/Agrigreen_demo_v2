"use client";
import {useEffect,useMemo,useRef,useState} from "react";
import {useAuth} from "@/auth/AuthProvider";
import {SelectField,TextField} from "@/components/form";
import {LoadState,dt,useAction,useLoad} from "@/kn/cycle/common";
import {DEPT_LABEL} from "../api";
import {createAdhocApi,type AdhocRequestInput} from "./api";
import {ApiProblem} from "@/api/problem";
export function AdhocRequestsPanel({id,departments,disabled,onChanged}:{id:string;departments:string[];disabled:boolean;onChanged:()=>void}){
 const {api}=useAuth();const requests=useMemo(()=>createAdhocApi(api),[api]);
 const list=useLoad(()=>requests.list(id),[requests,id]);const act=useAction();
 const [dept,setDept]=useState(departments[0]??"");const [instruction,setInstruction]=useState("");
 const [due,setDue]=useState("");const [fields,setFields]=useState<AdhocRequestInput["indicators"]>([]);
 const pending=useRef<AdhocRequestInput|null>(null);const [uncertain,setUncertain]=useState(false);
 useEffect(()=>{const timer=setInterval(list.reload,30000);return()=>clearInterval(timer)},[list.reload]);
 const locked=disabled||act.busy||uncertain;
 const reconciled=async(requestID:string)=>{
  const actual=await requests.reconcile(id,requestID),body=pending.current;
  const canonical=(x:AdhocRequestInput)=>JSON.stringify({department:x.department,instruction:x.instruction.trim(),due:Date.parse(x.due_at),indicators:x.indicators.map(f=>({code:f.code.trim(),name:f.name.trim(),unit:f.unit.trim()}))});
  if(body&&actual.state!=="intent"&&canonical(actual)===canonical(body)){
   requests.resolvedCreate(id);pending.current=null;setUncertain(false);
  }
  list.reload();onChanged();
 };
 const send=()=>act.run(async()=>{
  const body=pending.current??{department:dept,instruction,due_at:new Date(due).toISOString(),indicators:fields};
  pending.current=structuredClone(body);setUncertain(true);
  try{await requests.create(id,body);pending.current=null;setUncertain(false);list.reload();onChanged();}
  catch(e){if(e instanceof ApiProblem && e.problem.status>=400 && e.problem.status<500){pending.current=null;setUncertain(false)}throw e}
 },"Đã phát hành yêu cầu bổ sung");
 const dueValid=/^\d{4}-\d\d-\d\dT\d\d:\d\d$/.test(due);
 return <section aria-label="Yêu cầu số liệu bổ sung"><h3>Yêu cầu số liệu bổ sung mới</h3><p>Phiếu xuất hiện trong Báo cáo cho KHTC của phòng; người rà soát khác người gửi, tối đa3 lần gửi. Phiếu đã xác nhận hoặc kỳ đã kết thúc không mở lại.</p>
 {act.view}
 <SelectField label="Phòng bổ sung" value={dept} options={departments.map(value=>({value,label:DEPT_LABEL[value]??value}))} disabled={locked} onChange={setDept} required/>
 <TextField label="Nội dung yêu cầu" multiline value={instruction} maxLength={4000} onChange={setInstruction} disabled={locked} required/>
 <label>Hạn bổ sung <input type="datetime-local" value={due} onChange={e=>{if(e.target.value.length<=16)setDue(e.target.value)}} disabled={locked}/></label>
 <p>Chỉ tiêu số mới ({fields.length}/20); để trống để chỉ yêu cầu lời văn.</p>
 {fields.map((f,i)=><fieldset key={i} disabled={locked}><legend>Chỉ tiêu {i+1}</legend>
 <TextField label={"Mã chỉ tiêu "+(i+1)} value={f.code} maxLength={40} onChange={v=>setFields(xs=>xs.map((x,j)=>j===i?{...x,code:v.toUpperCase()}:x))}/>
 <TextField label={"Tên chỉ tiêu "+(i+1)} value={f.name} maxLength={200} onChange={v=>setFields(xs=>xs.map((x,j)=>j===i?{...x,name:v}:x))}/>
 <TextField label={"Đơn vị tính "+(i+1)} value={f.unit} maxLength={50} onChange={v=>setFields(xs=>xs.map((x,j)=>j===i?{...x,unit:v}:x))}/>
 <button type="button" className="btn" onClick={()=>setFields(xs=>xs.filter((_,j)=>j!==i))}>Bỏ chỉ tiêu {i+1}</button></fieldset>)}
 <button type="button" className="btn" disabled={locked||fields.length>=20} onClick={()=>setFields(xs=>[...xs,{code:"",name:"",unit:""}])}>Thêm chỉ tiêu số</button>{" "}
 <button type="button" className="btn btn-primary" disabled={disabled||act.busy||!uncertain&&(!dept||!instruction.trim()||!dueValid||fields.some(f=>!f.code||!f.name))} onClick={send}>{uncertain?"Gửi lại để đối chiếu thao tác trước":"Phát hành yêu cầu"}</button>
 {uncertain&&<p role="status">Chưa xác nhận kết quả. Gửi lại cùng nội dung để hệ thống đối chiếu, tránh phát hành trùng.</p>}
 <LoadState s={list}>{items=><ul>{items.map(r=><li key={r.id}>{DEPT_LABEL[r.department]??r.department} · {r.instruction} · Hạn {dt(r.due_at)} · {({intent:"Đang đối chiếu",issued:"Đã phát hành",fulfilled:"Đã nhận bài nộp",failed:"Chưa phát hành được",cancelled:"Đã dừng"})[r.state]}{r.return_applied===true?" · Đã trả phiếu":""} {["intent","issued"].includes(r.state)&&<button type="button" className="btn" disabled={disabled||act.busy} onClick={()=>act.run(async()=>{await reconciled(r.id)},"Đã đối chiếu trạng thái")}>Đối chiếu trạng thái</button>}</li>)}</ul>}</LoadState>
 </section>;
}
