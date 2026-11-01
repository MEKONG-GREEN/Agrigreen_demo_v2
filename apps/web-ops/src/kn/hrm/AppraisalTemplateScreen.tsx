"use client";
import Link from "next/link";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { DetailPage } from "@/components/patterns/detail";
import type { components } from "@/api/generated/application";
import { NumberField, TextField } from "@/components/form";
import { PageHeader, DataTable } from "@/components/patterns/list";
import { LoadState, useLoad } from "@/kn/cycle/common";
import { APPRAISAL_BASE, type AppraisalTemplate } from "./appraisal-api";
import { APPRAISAL_RATINGS, APPRAISAL_RATING_LABEL, appraisalPointGuard, type AppraisalForm } from "./appraisal-form";
import { GROUPS, useAppraisalAction, useAppraisalApi } from "./appraisal-ui";
type Summary=components["schemas"]["HrmAppraisalSummary"];
function summaryDefault(kind:"MS05"|"MS06"):Summary {
  const titles=["STT","Họ và tên","Chức vụ, đơn vị công tác","Cá nhân tự đề xuất mức xếp loại","Tập thể đề xuất mức xếp loại","Tóm tắt căn cứ, cơ sở lý do","Đề xuất nội dung liên quan về công tác cán bộ","Ghi chú"];
  return {kind,code:kind.toLowerCase(),number:kind.slice(-2),name:`Mẫu số ${kind.slice(-2)} · Tổng hợp kết quả đánh giá`,title:"TỔNG HỢP KẾT QUẢ ĐÁNH GIÁ VÀ ĐỀ XUẤT XẾP LOẠI QUÝ {QUY}, NĂM {NAM}",audience:kind==="MS05"?"Viên chức và cán bộ khuyến nông cấp xã":"Cán bộ lãnh đạo, quản lý",signature:"XÁC NHẬN TẬP THỂ LÃNH ĐẠO CƠ QUAN (Ký, ghi rõ họ tên và đóng dấu)",columns:["ordinal","staff","position_unit","self_rating","collective_rating","reason","personnel_proposal","note"].map((key,i)=>({key,title:titles[i]!})),ratings:Object.fromEntries(APPRAISAL_RATINGS.map(r=>[r,APPRAISAL_RATING_LABEL[r]]))};
}
function formDefault():AppraisalForm{return {code:"phieu_xa",name:"Phiếu đánh giá cán bộ khuyến nông cấp xã",group:"can_bo_xa",criteria:Array.from({length:6},(_,i)=>({code:`tc_${i+1}`,title:`Tiêu chí ${i+1}`,group:"",guide:"",maximum:"15"})),bands:[{minimum:"81",rating:"HTXSNV"},{minimum:"63",rating:"HTTNV"},{minimum:"45",rating:"HTNV"},{minimum:"0",rating:"KHTNV"}]};}
export function AppraisalTemplateScreen(){
 const api=useAppraisalApi(),list=useLoad(async()=>{const[a,b]=await Promise.all([api.templates("summary"),api.templates("form")]);return [...a.items,...b.items];},[]);
 return <><Link className="back-link" href={APPRAISAL_BASE}>Đợt đánh giá</Link><PageHeader title="Mẫu số 05, 06 và phiếu đánh giá">Mỗi lần lưu tạo phiên bản mới. Đợt đang mở giữ nguyên mẫu đã chọn; không chứa thông tin lương, thưởng bằng tiền.</PageHeader>
 <div className="row"><Link className="btn btn-p" href={`${APPRAISAL_BASE}/mau/them-moi?kind=summary&summary=MS05`}>Tạo Mẫu số 05</Link><Link className="btn btn-o" href={`${APPRAISAL_BASE}/mau/them-moi?kind=summary&summary=MS06`}>Tạo Mẫu số 06</Link><Link className="btn btn-o" href={`${APPRAISAL_BASE}/mau/them-moi?kind=form`}>Thiết kế phiếu mới</Link></div>
 <LoadState s={list}>{items=><DataTable caption="Mẫu và phiếu đánh giá" rows={items} rowKey={x=>x.id} columns={[
 {key:"name",header:"Mẫu/phiếu",cell:x=>x.form?.name??x.summary?.name},{key:"group",header:"Nhóm",cell:x=>x.form?GROUPS[x.form.group]:x.summary?.kind},
 {key:"version",header:"Phiên bản",cell:x=>x.version},{key:"status",header:"Trạng thái",cell:x=>x.status==="published"?"Đã phát hành":x.status==="retired"?"Ngừng dùng":"Nháp"},
 {key:"actions",header:"Thao tác",cell:x=><Link className="btn btn-o btn-sm" href={`${APPRAISAL_BASE}/mau/${x.id}`}>Xem và tạo phiên bản</Link>}
 ]}/>}</LoadState></>;
}
export function AppraisalTemplateEditorScreen({kind="form",summaryKind="MS05",id}:{kind?:"form"|"summary";summaryKind?:"MS05"|"MS06";id?:string}){
 const router=useRouter(),api=useAppraisalApi(),list=useLoad(id?async()=>{const[a,b]=await Promise.all([api.templates("summary"),api.templates("form")]);return [...a.items,...b.items];}:null,[id]);
 const close=()=>router.push(APPRAISAL_BASE+"/mau");
 return <DetailPage back={{href:APPRAISAL_BASE+"/mau",label:"Mẫu và phiếu đánh giá"}} title={id?"Phiên bản mẫu/phiếu":"Tạo mẫu/phiếu đánh giá"}>{id?<LoadState s={list}>{items=>{const t=items.find(x=>x.id===id);return t?<TemplateEditor key={t.id} kind={t.kind} template={t} onSaved={close} onClose={close}/>:<p role="alert">Không có phiên bản mẫu/phiếu này trong phạm vi.</p>;}}</LoadState>:<TemplateEditor kind={kind} summaryKind={summaryKind} onSaved={close} onClose={close}/>}</DetailPage>;
}
function TemplateEditor({kind,template,summaryKind="MS05",onSaved,onClose}:{kind:"summary"|"form";template?:AppraisalTemplate;summaryKind?:"MS05"|"MS06";onSaved:()=>void;onClose:()=>void}){
 const api=useAppraisalApi(),act=useAppraisalAction(),[form,setForm]=useState<AppraisalForm>(()=>structuredClone(template?.form??formDefault())),[summary,setSummary]=useState<Summary>(()=>structuredClone(template?.summary??summaryDefault(summaryKind)));
 const [code,setCode]=useState(template?.code??(kind==="form"?form.code:summary.code)),[errors,setErrors]=useState<Record<string,string>>({});
 const history=useLoad(template?()=>api.templateHistory(kind,template.code):null,[template?.id]);
 async function save(status:"draft"|"published"|"retired"){
  const problems:Record<string,string>={};if(!/^[a-z][a-z0-9_]{0,39}$/.test(code))problems.code="Mã gồm chữ thường, số và _, tối đa 40 ký tự.";
  if(kind==="form"){for(const [i,c] of form.criteria.entries()){const e=appraisalPointGuard(c.maximum,"1000000");if(e)problems[`max${i}`]=e;}for(const [i,b] of form.bands.entries()){const e=appraisalPointGuard(b.minimum,"1000000");if(e)problems[`band${i}`]=e;}}
  setErrors(problems);if(Object.keys(problems).length)return;
  const body={status,version:template?.code===code?template.version:0,...(kind==="form"?{form:{...form,code}}:{summary:{...summary,code}})};
  const v=await act.execute(`${kind}:${code}`,body,k=>api.saveTemplate(kind,code,body,k));if(v)onSaved();
 }
 return <section className="card"><h2>{kind==="form"?"Thiết kế phiếu":"Nội dung biểu tổng hợp"}</h2>{act.view}<TextField label="Mã mẫu/phiếu" value={code} onChange={setCode} maxLength={40} error={errors.code}/>
 {kind==="form"?<><TextField label="Tên phiếu" value={form.name} onChange={name=>setForm({...form,name})} required maxLength={200}/><label className="field">Nhóm đối tượng<select aria-label="Nhóm đối tượng" value={form.group} onChange={e=>setForm({...form,group:e.target.value as AppraisalForm["group"]})}>{Object.entries(GROUPS).map(([k,v])=><option value={k} key={k}>{v}</option>)}</select></label>
 <table className="tbl"><thead><tr><th>Mã tiêu chí</th><th>Nội dung</th><th>Điểm tối đa</th><th/></tr></thead><tbody>{form.criteria.map((c,i)=><tr key={i}><td><TextField label={`Mã tiêu chí ${i+1}`} value={c.code} onChange={code=>setForm({...form,criteria:form.criteria.map((x,j)=>i===j?{...x,code}:x)})} maxLength={40}/></td><td><TextField label={`Nội dung tiêu chí ${i+1}`} value={c.title} onChange={title=>setForm({...form,criteria:form.criteria.map((x,j)=>i===j?{...x,title}:x)})} maxLength={500}/><TextField label={`Nhóm tiêu chí ${i+1}`} value={c.group} onChange={group=>setForm({...form,criteria:form.criteria.map((x,j)=>i===j?{...x,group}:x)})} maxLength={200}/><TextField label={`Hướng dẫn chấm ${i+1}`} value={c.guide} onChange={guide=>setForm({...form,criteria:form.criteria.map((x,j)=>i===j?{...x,guide}:x)})} multiline maxLength={2000}/></td><td><NumberField label={`Điểm tối đa ${i+1}`} value={c.maximum} error={errors[`max${i}`]} onChange={maximum=>setForm({...form,criteria:form.criteria.map((x,j)=>i===j?{...x,maximum}:x)})} rule={{max:"1000000",maxDecimals:2,maxInputLength:32}}/></td><td><button className="btn btn-o" onClick={()=>setForm({...form,criteria:form.criteria.filter((_,j)=>i!==j)})}>Bỏ tiêu chí {i+1}</button></td></tr>)}</tbody></table>
 <button className="btn btn-o" disabled={form.criteria.length>=200} onClick={()=>setForm({...form,criteria:[...form.criteria,{code:`tc_${form.criteria.length+1}`,title:"",group:"",guide:"",maximum:"0"}]})}>Thêm tiêu chí</button><h3>Bảng quy đổi tổng điểm</h3><div className="grid2">{form.bands.map((b,i)=><NumberField key={b.rating} label={`Từ điểm ${APPRAISAL_RATING_LABEL[b.rating]}`} value={b.minimum} onChange={minimum=>setForm({...form,bands:form.bands.map((x,j)=>i===j?{...x,minimum}:x)})} rule={{max:"1000000",maxDecimals:2,maxInputLength:32}} error={errors[`band${i}`]}/>)}</div><TextField label="Câu hỏi nhận xét (mỗi dòng một câu)" value={(form.questions??[]).join("\n")} onChange={value=>setForm({...form,questions:value.split("\n").map(x=>x.trim()).filter(Boolean)})} multiline maxLength={20000}/></>:<>
 <TextField label="Số mẫu" value={summary.number} onChange={number=>setSummary({...summary,number})} maxLength={20}/><TextField label="Tên mẫu" value={summary.name} onChange={name=>setSummary({...summary,name})} required maxLength={200}/><TextField label="Tiêu đề biểu" value={summary.title} onChange={title=>setSummary({...summary,title})} required maxLength={500}/><TextField label="Đối tượng" value={summary.audience} onChange={audience=>setSummary({...summary,audience})} required maxLength={500}/><TextField label="Phần ký" value={summary.signature} onChange={signature=>setSummary({...summary,signature})} multiline required maxLength={1000}/>
 <h3>Mức xếp loại</h3>{APPRAISAL_RATINGS.map(r=><TextField key={r} label={`Nhãn mức ${r}`} value={summary.ratings[r]??""} onChange={value=>setSummary({...summary,ratings:{...summary.ratings,[r]:value}})} required maxLength={200}/>)}
 <h3>Tám cột của biểu Sở</h3>{summary.columns.map((c,i)=><TextField key={c.key} label={`Cột ${i+1}`} value={c.title} onChange={title=>setSummary({...summary,columns:summary.columns.map((x,j)=>i===j?{...x,title}:x)})} required maxLength={300}/>)}
 <section aria-label="Xem trước biểu"><h3>Xem trước biểu</h3><h4>{summary.title}</h4><p>{summary.audience}</p><table className="tbl"><thead><tr>{summary.columns.map(c=><th key={c.key}>{c.title}</th>)}</tr></thead><tbody><tr>{summary.columns.map(c=><td key={c.key}>—</td>)}</tr></tbody></table><p>{summary.signature}</p></section>
 </>}
 <div className="row"><button className="btn btn-o" disabled={act.busy} onClick={()=>void save("draft")}>Lưu nháp</button><button className="btn btn-p" disabled={act.busy} onClick={()=>void save("published")}>Phát hành phiên bản</button>{template?<button className="btn btn-o" disabled={act.busy} onClick={()=>void save("retired")}>Ngừng sử dụng</button>:null}<button className="btn btn-o" onClick={onClose}>Đóng</button></div>
 {template?<LoadState s={history}>{h=><section><h3>Lịch sử mẫu</h3><ol>{h.items.map(x=><li key={x.id}>v{x.version} · {x.status} · {x.recorded_at} <button className="btn btn-o btn-sm" onClick={()=>{if(x.form)setForm(structuredClone(x.form));if(x.summary)setSummary(structuredClone(x.summary));}}>Dùng nội dung phiên bản {x.version} cho bản mới</button></li>)}</ol></section>}</LoadState>:null}
 </section>;
}