"use client";
import {useState} from "react";
import {NumberField,TextField} from "@/components/form";
import {dt} from "@/kn/cycle/common";
import {emptyContent,type KhtcFormSheet,type KhtcFormContent} from "../forms/api";
export function AdhocAnswer({form,content,onChange}:{form:KhtcFormSheet;content:KhtcFormContent;onChange?:(c:KhtcFormContent)=>void}){
 const request=form.supplement!;
 const [errors,setErrors]=useState<Record<string,string|undefined>>({});
 const seen=content.adhoc_request_id===request.id;
 const enabled=!!onChange && request.state!=="intent" && seen;
 return <section aria-label="Trả lời yêu cầu phát sinh"><h2>Yêu cầu bổ sung</h2><p>{request.instruction}</p><p>Hạn gửi {dt(request.due_at)}{request.overdue?" · Đã quá hạn":""}</p>
 {request.state==="intent"?<p role="status">KHTC đang đối chiếu trạng thái phiếu. Tải lại sau khi yêu cầu phát hành xong.</p>:!seen?<><p role="alert">Đọc yêu cầu hiện tại và bắt đầu câu trả lời mới. Nội dung của yêu cầu trước được giữ trong lịch sử.</p>{onChange&&<button type="button" className="btn" onClick={()=>onChange({...emptyContent(),adhoc_request_id:request.id,adhoc_values:{},adhoc_text:""})}>Đã đọc yêu cầu — nhập câu trả lời</button>}</>:null}
 {request.indicators.map(d=><NumberField key={d.code} label={d.name} unit={d.unit} value={seen?content.adhoc_values?.[d.code]??"":""} disabled={!enabled} required rule={{max:"999999999999999.999999",maxDecimals:6}} error={errors[d.code]} onBlurError={error=>setErrors(e=>({...e,[d.code]:error}))} onChange={raw=>{
 if(raw.length>23){setErrors(e=>({...e,[d.code]:"Số quá dài (tối đa15 chữ số và6 chữ số thập phân)."}));return}
 onChange?.({...content,adhoc_values:{...content.adhoc_values,[d.code]:raw}});setErrors(e=>({...e,[d.code]:undefined}));
 }}/>)}
 <TextField label="Nội dung trả lời bổ sung" multiline maxLength={8000} required={!request.indicators.length} value={seen?content.adhoc_text??"":""} disabled={!enabled} onChange={value=>onChange?.({...content,adhoc_text:value})}/>
 </section>;
}
