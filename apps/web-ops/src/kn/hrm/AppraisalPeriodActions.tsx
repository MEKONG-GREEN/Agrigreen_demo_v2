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

export function AppraisalPeriodActions({period:p,hasReopened=false,onChanged}:{period:AppraisalPeriod;hasReopened?:boolean;onChanged:()=>void}) {
  const api=useAppraisalApi(), access=useLoad(()=>api.access(),[]);
  if(p.workflow_version!=="hrm.appraisal-proposal/v4")return <section className="card"><h2>Lịch sử chỉ đọc</h2><p>Đợt, phiếu và lần xuất cũ được giữ nguyên.</p><PaperExports period={p} legacy /></section>;
  return <LoadState s={access}>{a=><>{["admin","hcth"].includes(a.group)&&a.perms.assessment==="all" ? <CenterActions period={p} hasReopened={hasReopened} onChanged={onChanged}/> : a.group==="manager" ? <SendUnit period={p} unit={a.unit} hasReopened={hasReopened} onChanged={onChanged}/> : null}
    {a.perms.export==="all"&&["admin","hcth"].includes(a.group)?<PaperExports period={p} unit={a.group==="manager"?a.unit:undefined}/>:null}</>}</LoadState>;
}
function SendUnit({period:p,unit,hasReopened,onChanged}:{period:AppraisalPeriod;unit:string;hasReopened:boolean;onChanged:()=>void}) {
  const api=useAppraisalApi(),act=useAppraisalAction();return <section className="card"><h2>Gửi đề xuất của đơn vị</h2><p>Chỉ gửi khi mọi người thuộc phân công đã hoàn tất đề xuất và căn cứ; máy chủ kiểm lại toàn bộ cùng giao dịch.</p>{act.view}<button className="btn btn-p" disabled={act.busy||(p.closed&&!hasReopened)} onClick={()=>{const b:AppraisalPeriodChange={action:"send",version:p.version,unit};void act.execute(`send:${p.id}`,b,k=>api.changePeriod(p.id,b,k)).then(x=>{if(x)onChanged();});}}>Gửi Phòng HCTH</button></section>;
}
function CenterActions({period:p,hasReopened,onChanged}:{period:AppraisalPeriod;hasReopened:boolean;onChanged:()=>void}) {
  const api=useAppraisalApi(),act=useAppraisalAction(),{api:raw}=useAuth(),docsApi=useMemo(()=>createDocApi(raw),[raw]);
  const docs=useLoad(()=>docsApi.list({doc_type:"danh_gia",year:p.year,limit:100}),[p.year]);
  const [reason,setReason]=useState(""),[manager,setManager]=useState(localDateTime(p.manager_due_at));
  const [signed,setSigned]=useState<Record<string,string>>({...p.signed_documents}),[ack,setAck]=useState(false);
  function execute(action:AppraisalPeriodChange["action"],extra:Partial<AppraisalPeriodChange>={}) {const b:AppraisalPeriodChange={action,version:p.version,reason,...extra};void act.execute(`${p.id}:${action}`,b,k=>api.changePeriod(p.id,b,k)).then(x=>{if(x)onChanged();});}
  return <section className="card"><h2>Theo dõi, nhắc và chốt đợt</h2>{act.view}<p>{Object.entries(p.counts).map(([k,n])=>`${k}: ${n}`).join(" · ")}</p>{p.warnings.length?<ul>{p.warnings.map((w,i)=><li key={i}>{w}</li>)}</ul>:null}
    <TextField label="Lý do thay đổi đợt" value={reason} onChange={setReason} multiline maxLength={2000}/><label className="field">Hạn gửi Phòng HCTH<input aria-label="Hạn gửi Phòng HCTH" type="datetime-local" value={manager} onChange={e=>setManager(e.target.value)}/></label>
    <div className="row"><button className="btn btn-o" disabled={act.busy||p.closed||!manager} onClick={()=>execute("extend",{manager_due_at:asISO(manager)})}>Gia hạn</button><button className="btn btn-o" disabled={act.busy||p.closed} onClick={()=>execute("remind",{step:"manager"})}>Nhắc đơn vị chưa gửi</button></div>
    <h3>Bản đã ký</h3><p><Link href="/kn/nhan-su/kho-tai-lieu">Tải bản ký lên Hồ sơ đánh giá</Link>, sau đó tải lại danh sách tài liệu.</p><button className="btn btn-o btn-sm" onClick={docs.reload}>Tải lại tài liệu</button>
    <LoadState s={docs}>{d=><div className="grid2">{["MS05","MS06"].map(kind=><label className="field" key={kind}>Bản ký {kind}<select aria-label={`Bản ký ${kind}`} value={signed[kind]??""} onChange={e=>setSigned({...signed,[kind]:e.target.value})}><option value="">Chưa chọn</option>{d.items.map(x=><option key={x.doc_id} value={x.doc_id}>{x.title}</option>)}</select></label>)}</div>}</LoadState>
    <button className="btn btn-o" disabled={act.busy||p.closed||!Object.values(signed).some(Boolean)} onClick={()=>execute("attach_signed",{signed_documents:Object.fromEntries(Object.entries(signed).filter(([,v])=>v))})}>Gắn bản ký vào đợt</button>
    <label className="field"><span><input type="checkbox" checked={ack} onChange={e=>setAck(e.target.checked)}/> Tôi xác nhận cảnh báo nếu còn mẫu chưa gắn bản ký</span></label><button className="btn btn-p" disabled={act.busy||(p.closed&&!hasReopened)} onClick={()=>execute("close",{acknowledge_unsigned:ack})}>Chốt kết quả đợt</button><p className="small mute">Sau chốt chỉ mở lại riêng một người có lý do ở chi tiết đề xuất. Kết quả quý và năm chỉ ghi vào hồ sơ, không làm căn cứ nâng bậc.</p>
  </section>;
}
function PaperExports({period:p,unit,legacy=false}:{period:AppraisalPeriod;unit?:string;legacy?:boolean}) {
  const api=useAppraisalApi(),act=useAppraisalAction(),[kind,setKind]=useState<"MS05"|"MS06">("MS05"),[show,setShow]=useState(false);
  const paper=useLoad(show?()=>api.paper(p.id,kind,unit):null,[p.id,kind,unit,show]),exports=useLoad(()=>api.exports(p.id),[p.id]);
  async function download(id:string){const f=await api.file(p.id,id);saveBlob(f.blob,f.name);if(f.drift)act.setNote("Đã dựng lại Excel theo các phiên bản đã chọn; tên trong kho định danh đã đổi nên băm tệp khác lần xuất.");}
  return <section className="card"><h2>Biểu Mẫu số 05, 06</h2>{act.view}{!legacy?<><label>Mẫu<select aria-label="Mẫu xuất" value={kind} onChange={e=>{setKind(e.target.value as "MS05"|"MS06");setShow(false);}}><option value="MS05">Mẫu số 05</option>{!unit?<option value="MS06">Mẫu số 06</option>:null}</select></label><button className="btn btn-o" onClick={()=>{setShow(true);paper.reload();}}>Xem trước tám cột</button>
    {show?<LoadState s={paper}>{v=><><h3>{v.template.title.replace("{QUY}",String(p.quarter)).replace("{NAM}",String(p.year))}</h3><p>{v.template.audience}</p><div className="table-wrap"><table className="tbl"><thead><tr>{v.template.columns.map(c=><th key={c.key}>{c.title}</th>)}</tr></thead><tbody>{v.items.map((x,i)=><tr key={x.member.staff_id}>{v.template.columns.map(column=><td key={column.key}>{({ordinal:String(i+1),staff:x.name||x.member.code,position_unit:`${x.member.position_name} · ${x.member.unit_name}`,self_rating:APPRAISAL_RATING_LABEL[x.self_rating],collective_rating:APPRAISAL_RATING_LABEL[x.collective_rating],reason:x.reason,personnel_proposal:x.personnel_proposal,note:x.note} as Record<string,string>)[column.key]}</td>)}</tr>)}</tbody></table></div><p>{v.template.signature}</p></>}</LoadState>:null}
    <div className="row">{(["xlsx","docx"] as const).map(format=><button className="btn btn-p" disabled={act.busy} key={format} onClick={()=>{const b={kind,format,version:p.version,unit};void act.execute(`export:${p.id}`,b,k=>api.export(p.id,b,k)).then(x=>{if(x){exports.reload();void act.run(()=>download(x.id));}});}}>Xuất {format==="xlsx"?"Excel":"Word"}</button>)}</div>

    </>:null}<LoadState s={exports}>{v=><ul>{v.items.map(x=><li key={x.id}>{x.file_name} · {x.count} người · {x.status} <button className="btn btn-o btn-sm" disabled={act.busy||x.status!=="ready"} onClick={()=>void act.run(()=>download(x.id))}>Tải lại lần xuất</button></li>)}</ul>}</LoadState>
  </section>;
}