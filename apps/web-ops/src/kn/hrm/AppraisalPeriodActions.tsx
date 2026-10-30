"use client";
import Link from "next/link";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { TextField } from "@/components/form";
import { LoadState, useLoad } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import { APPRAISAL_RATING_LABEL } from "./appraisal-form";
import { type AppraisalPeriod, type AppraisalPeriodChange } from "./appraisal-api";
import { asISO, localDateTime, useAppraisalAction, useAppraisalApi } from "./appraisal-ui";
import { createDocApi } from "./doc-api";

export function AppraisalPeriodActions({period:p,onChanged}:{period:AppraisalPeriod;onChanged:()=>void}) {
  const api=useAppraisalApi(), access=useLoad(()=>api.access(),[]);
  return <LoadState s={access}>{a=><>{["admin","hcth"].includes(a.group)&&a.perms.assessment==="all" ? <CenterActions period={p} onChanged={onChanged}/> : a.group==="manager" ? <SendUnit period={p} unit={a.unit} onChanged={onChanged}/> : null}
    {(a.perms.export==="all"||a.perms.export==="unit")&&["admin","hcth","manager"].includes(a.group)?<PaperExports period={p} unit={a.group==="manager"?a.unit:undefined}/>:null}</>}</LoadState>;
}
function SendUnit({period:p,unit,onChanged}:{period:AppraisalPeriod;unit:string;onChanged:()=>void}) {
  const api=useAppraisalApi(),act=useAppraisalAction();return <section className="card"><h2>Gửi đề xuất của đơn vị</h2><p>Chỉ gửi khi mọi người thuộc phân công đã hoàn tất đề xuất và căn cứ; máy chủ kiểm lại toàn bộ cùng giao dịch.</p>{act.view}<button className="btn btn-p" disabled={act.busy||p.closed||p.manager_locked} onClick={()=>{const b:AppraisalPeriodChange={action:"send",version:p.version,unit};void act.execute(`send:${p.id}`,b,k=>api.changePeriod(p.id,b,k)).then(x=>{if(x)onChanged();});}}>Gửi Phòng HCTH</button></section>;
}
function CenterActions({period:p,onChanged}:{period:AppraisalPeriod;onChanged:()=>void}) {
  const api=useAppraisalApi(),act=useAppraisalAction(),{api:raw}=useAuth(),docsApi=useMemo(()=>createDocApi(raw),[raw]);
  const docs=useLoad(()=>docsApi.list({doc_type:"danh_gia",year:p.year,limit:100}),[p.year]);
  const [reason,setReason]=useState(""),[self,setSelf]=useState(localDateTime(p.self_due_at)),[manager,setManager]=useState(localDateTime(p.manager_due_at)),[summary,setSummary]=useState(localDateTime(p.summary_due_at));
  const [signed,setSigned]=useState<Record<string,string>>({...p.signed_documents}),[ack,setAck]=useState(false);
  function execute(action:AppraisalPeriodChange["action"],extra:Partial<AppraisalPeriodChange>={}) {const b:AppraisalPeriodChange={action,version:p.version,reason,...extra};void act.execute(`${p.id}:${action}`,b,k=>api.changePeriod(p.id,b,k)).then(x=>{if(x)onChanged();});}
  return <section className="card"><h2>Theo dõi, nhắc và chốt đợt</h2>{act.view}<p>{Object.entries(p.counts).map(([k,n])=>`${k}: ${n}`).join(" · ")}</p>{p.warnings.length?<ul>{p.warnings.map((w,i)=><li key={i}>{w}</li>)}</ul>:null}
    <TextField label="Lý do thay đổi đợt" value={reason} onChange={setReason} multiline maxLength={2000}/><div className="grid2">{[{label:"Hạn tự đánh giá",v:self,set:setSelf},{label:"Hạn cấp quản lý",v:manager,set:setManager},{label:"Hạn tổng hợp",v:summary,set:setSummary}].map(x=><label className="field" key={x.label}>{x.label}<input aria-label={x.label} type="datetime-local" value={x.v} onChange={e=>x.set(e.target.value)}/></label>)}</div>
    <div className="row"><button className="btn btn-o" disabled={act.busy||p.closed||!self||!manager||!summary} onClick={()=>execute("extend",{self_due_at:asISO(self),manager_due_at:asISO(manager),summary_due_at:asISO(summary)})}>Gia hạn</button>
    <button className="btn btn-o" disabled={act.busy||p.closed} onClick={()=>execute(p.self_locked?"unlock_self":"lock_self")}>{p.self_locked?"Mở bước tự đánh giá":"Khóa bước tự đánh giá"}</button><button className="btn btn-o" disabled={act.busy||p.closed} onClick={()=>execute(p.manager_locked?"unlock_manager":"lock_manager")}>{p.manager_locked?"Mở bước cấp quản lý":"Khóa bước cấp quản lý"}</button>
    {(["self","manager","summary"] as const).map(step=><button className="btn btn-o" disabled={act.busy||p.closed} key={step} onClick={()=>execute("remind",{step})}>Nhắc {step==="self"?"người chưa tự nộp":step==="manager"?"cấp quản lý":"đơn vị chưa gửi"}</button>)}</div>
    <h3>Bản đã ký</h3><p><Link href="/kn/nhan-su/kho-tai-lieu">Tải bản ký lên Hồ sơ đánh giá</Link>, sau đó tải lại danh sách tài liệu.</p><button className="btn btn-o btn-sm" onClick={docs.reload}>Tải lại tài liệu</button>
    <LoadState s={docs}>{d=><div className="grid2">{["MS05","MS06"].map(kind=><label className="field" key={kind}>Bản ký {kind}<select aria-label={`Bản ký ${kind}`} value={signed[kind]??""} onChange={e=>setSigned({...signed,[kind]:e.target.value})}><option value="">Chưa chọn</option>{d.items.map(x=><option key={x.doc_id} value={x.doc_id}>{x.title}</option>)}</select></label>)}</div>}</LoadState>
    <button className="btn btn-o" disabled={act.busy||p.closed||!Object.values(signed).some(Boolean)} onClick={()=>execute("attach_signed",{signed_documents:Object.fromEntries(Object.entries(signed).filter(([,v])=>v))})}>Gắn bản ký vào đợt</button>
    <label className="field"><span><input type="checkbox" checked={ack} onChange={e=>setAck(e.target.checked)}/> Tôi xác nhận cảnh báo nếu còn mẫu chưa gắn bản ký</span></label><button className="btn btn-p" disabled={act.busy||p.closed} onClick={()=>execute("close",{acknowledge_unsigned:ack})}>Chốt kết quả đợt</button><p className="small mute">Sau chốt chỉ mở lại riêng một người có lý do ở chi tiết phiếu. Đợt năm ghi kết luận năm; không tự quy đổi kết quả bốn quý.</p>
  </section>;
}
function PaperExports({period:p,unit}:{period:AppraisalPeriod;unit?:string}) {
  const api=useAppraisalApi(),act=useAppraisalAction(),[kind,setKind]=useState<"MS05"|"MS06">("MS05"),[show,setShow]=useState(false);
  const paper=useLoad(show?()=>api.paper(p.id,kind,unit):null,[p.id,kind,unit,show]),exports=useLoad(()=>api.exports(p.id),[p.id]);
  async function download(id:string){const f=await api.file(p.id,id);saveBlob(f.blob,f.name);if(f.drift)act.setNote("Đã dựng lại Excel theo các phiên bản đã chọn; tên trong kho định danh đã đổi nên băm tệp khác lần xuất.");}
  return <section className="card"><h2>Biểu Mẫu số 05, 06</h2>{act.view}<label>Mẫu<select aria-label="Mẫu xuất" value={kind} onChange={e=>{setKind(e.target.value as "MS05"|"MS06");setShow(false);}}><option value="MS05">Mẫu số 05</option>{!unit?<option value="MS06">Mẫu số 06</option>:null}</select></label><button className="btn btn-o" onClick={()=>{setShow(true);paper.reload();}}>Xem trước tám cột</button>
    {show?<LoadState s={paper}>{v=><><h3>{v.template.title.replace("{QUY}",String(p.quarter)).replace("{NAM}",String(p.year))}</h3><p>{v.template.audience}</p><div className="table-wrap"><table className="tbl"><thead><tr>{v.template.columns.map(c=><th key={c.key}>{c.title}</th>)}</tr></thead><tbody>{v.items.map((x,i)=><tr key={x.member.staff_id}><td>{i+1}</td><td>{x.name||x.member.code}</td><td>{x.member.position_name} · {x.member.unit_name}</td><td>{APPRAISAL_RATING_LABEL[x.self_rating]}</td><td>{APPRAISAL_RATING_LABEL[x.collective_rating]}</td><td>{x.reason}</td><td>{x.personnel_proposal}</td><td>{x.note}</td></tr>)}</tbody></table></div><p>{v.template.signature}</p></>}</LoadState>:null}
    <div className="row">{(["xlsx","docx"] as const).map(format=><button className="btn btn-p" disabled={act.busy} key={format} onClick={()=>{const b={kind,format,version:p.version,unit};void act.execute(`export:${p.id}`,b,k=>api.export(p.id,b,k)).then(x=>{if(x){exports.reload();void act.run(()=>download(x.id));}});}}>Xuất {format==="xlsx"?"Excel":"Word"}</button>)}</div>
    <button className="btn btn-o" disabled={act.busy} onClick={()=>{const b={kind:"PHIEU_DIEM" as const,format:"xlsx" as const,version:p.version,unit};void act.execute(`scores:${p.id}`,b,k=>api.export(p.id,b,k)).then(x=>{if(x){exports.reload();void act.run(()=>download(x.id));}});}}>Xuất tổng hợp điểm (Excel)</button>
    <LoadState s={exports}>{v=><ul>{v.items.map(x=><li key={x.id}>{x.file_name} · {x.count} người · {x.status} <button className="btn btn-o btn-sm" disabled={act.busy||x.status!=="ready"} onClick={()=>void act.run(()=>download(x.id))}>Tải lại lần xuất</button></li>)}</ul>}</LoadState>
  </section>;
}