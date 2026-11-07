"use client";
import Link from "next/link";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { DetailPage } from "@/components/patterns/detail";
import type { components } from "@/api/generated/application";
import { TextField } from "@/components/form";
import { PageHeader, DataTable } from "@/components/patterns/list";
import { LoadState, useLoad } from "@/kn/cycle/common";
import { APPRAISAL_BASE, type AppraisalTemplate } from "./appraisal-api";
import { APPRAISAL_RATINGS, APPRAISAL_RATING_LABEL } from "./appraisal-form";
import { useAppraisalAction, useAppraisalApi } from "./appraisal-ui";
type Summary=components["schemas"]["HrmAppraisalSummary"];
function summaryDefault(kind:"MS05"|"MS06"):Summary {
  const titles=["STT","Họ và tên","Chức vụ, đơn vị công tác","Cá nhân tự đề xuất mức xếp loại","Tập thể đề xuất mức xếp loại","Tóm tắt căn cứ, cơ sở lý do","Đề xuất nội dung liên quan về công tác cán bộ","Ghi chú"];
  return {kind,code:kind.toLowerCase(),number:kind.slice(-2),name:`Mẫu số ${kind.slice(-2)} · Tổng hợp kết quả đánh giá`,title:"TỔNG HỢP KẾT QUẢ ĐÁNH GIÁ VÀ ĐỀ XUẤT XẾP LOẠI QUÝ {QUY}, NĂM {NAM}",audience:kind==="MS05"?"Viên chức và cán bộ khuyến nông cấp xã":"Cán bộ lãnh đạo, quản lý",signature:"XÁC NHẬN TẬP THỂ LÃNH ĐẠO CƠ QUAN (Ký, ghi rõ họ tên và đóng dấu)",columns:["ordinal","staff","position_unit","self_rating","collective_rating","reason","personnel_proposal","note"].map((key,i)=>({key,title:titles[i]!})),ratings:Object.fromEntries(APPRAISAL_RATINGS.map(r=>[r,APPRAISAL_RATING_LABEL[r]]))};
}

export function AppraisalTemplateScreen(){
  const api=useAppraisalApi(),list=useLoad(()=>api.templates("summary"),[]),legacy=useLoad(()=>api.templates("form"),[]);
  return <><Link className="back-link" href={APPRAISAL_BASE}>Đợt đánh giá</Link><PageHeader title="Mẫu số 05, 06">Mỗi lần lưu tạo phiên bản mới. Đợt đã mở giữ nguyên mẫu đã chọn.</PageHeader>
    <div className="row"><Link className="btn btn-p" href={`${APPRAISAL_BASE}/mau/them-moi?kind=summary&summary=MS05`}>Tạo Mẫu số 05</Link><Link className="btn btn-o" href={`${APPRAISAL_BASE}/mau/them-moi?kind=summary&summary=MS06`}>Tạo Mẫu số 06</Link></div>
    <LoadState s={list}>{v=><DataTable caption="Mẫu tổng hợp kết quả" rows={v.items} rowKey={x=>x.id} columns={[
      {key:"name",header:"Mẫu",cell:x=>x.summary?.name},{key:"kind",header:"Đối tượng",cell:x=>x.summary?.audience},{key:"version",header:"Phiên bản",cell:x=>x.version},{key:"status",header:"Trạng thái",cell:x=>x.status==="published"?"Đã phát hành":x.status==="retired"?"Ngừng dùng":"Nháp"},{key:"actions",header:"Thao tác",cell:x=><Link href={`${APPRAISAL_BASE}/mau/${x.id}`}>Xem và tạo phiên bản</Link>}
    ]}/>}</LoadState>
    <LoadState s={legacy}>{v=>v.items.length?<section className="card"><h2>Phiếu cũ chỉ đọc</h2><ul>{v.items.map(x=><li key={x.id}><Link href={`${APPRAISAL_BASE}/mau/${x.id}`}>{x.form?.name} · v{x.version}</Link></li>)}</ul></section>:null}</LoadState>
  </>;
}
export function AppraisalTemplateEditorScreen({kind="summary",summaryKind="MS05",id}:{kind?:"form"|"summary";summaryKind?:"MS05"|"MS06";id?:string}){
  const router=useRouter(),api=useAppraisalApi(),list=useLoad(id?async()=>{const[a,b]=await Promise.all([api.templates("summary"),api.templates("form")]);return [...a.items,...b.items];}:null,[id]);
  const close=()=>router.push(APPRAISAL_BASE+"/mau");
  return <DetailPage back={{href:APPRAISAL_BASE+"/mau",label:"Mẫu số 05, 06"}} title={id?"Phiên bản mẫu":"Tạo mẫu tổng hợp"}>{id?<LoadState s={list}>{items=>{const t=items.find(x=>x.id===id);return t?.kind==="form"?<LegacyTemplate template={t}/>:t?<SummaryEditor key={t.id} template={t} onSaved={close}/>:<p role="alert">Không có phiên bản mẫu trong phạm vi.</p>;}}</LoadState>:kind==="summary"?<SummaryEditor summaryKind={summaryKind} onSaved={close}/>:<p role="alert">Không còn chức năng thiết kế phiếu chấm điểm.</p>}</DetailPage>;
}
function LegacyTemplate({template:t}:{template:AppraisalTemplate}) {
  const api=useAppraisalApi(),history=useLoad(()=>api.templateHistory("form",t.code),[t.id]);
  const [selected,setSelected]=useState(t);
  return <section className="card"><h2>Phiếu lịch sử chỉ đọc</h2><h3>{selected.form?.name}</h3><table className="tbl"><thead><tr><th>Mã tiêu chí</th><th>Nội dung</th><th>Điểm tối đa</th></tr></thead><tbody>{selected.form?.criteria.map(c=><tr key={c.code}><td>{c.code}</td><td>{c.title}</td><td>{c.maximum}</td></tr>)}</tbody></table><LoadState s={history}>{h=><ul>{h.items.map(x=><li key={x.id}><button className="btn btn-o" onClick={()=>setSelected(x)}>Xem phiên bản {x.version}</button></li>)}</ul>}</LoadState></section>;
}
function SummaryEditor({template,summaryKind="MS05",onSaved}:{template?:AppraisalTemplate;summaryKind?:"MS05"|"MS06";onSaved:()=>void}){
  const api=useAppraisalApi(),act=useAppraisalAction(),[summary,setSummary]=useState<Summary>(()=>structuredClone(template?.summary??summaryDefault(summaryKind))),[code,setCode]=useState(template?.code??summaryDefault(summaryKind).code),[error,setError]=useState("");
  const history=useLoad(template?()=>api.templateHistory("summary",template.code):null,[template?.id]);
  async function save(status:"draft"|"published"|"retired") {
    if(!/^[a-z][a-z0-9_]{0,39}$/.test(code)){setError("Mã gồm chữ thường, số và _, tối đa 40 ký tự.");return;}
    const body={status,version:template?.code===code?template.version:0,summary:{...summary,code}};
    const value=await act.execute(`summary:${code}`,body,k=>api.saveTemplate("summary",code,body,k));if(value)onSaved();
  }
  return <section className="card"><h2>Nội dung biểu tổng hợp</h2>{act.view}<TextField label="Mã mẫu" value={code} onChange={setCode} maxLength={40} error={error}/>
    <TextField label="Số mẫu" value={summary.number} onChange={number=>setSummary({...summary,number})} required maxLength={20}/><TextField label="Tên mẫu" value={summary.name} onChange={name=>setSummary({...summary,name})} required maxLength={200}/><TextField label="Tiêu đề biểu" value={summary.title} onChange={title=>setSummary({...summary,title})} required maxLength={500}/><TextField label="Đối tượng" value={summary.audience} onChange={audience=>setSummary({...summary,audience})} required maxLength={500}/><TextField label="Phần ký" value={summary.signature} onChange={signature=>setSummary({...summary,signature})} multiline required maxLength={1000}/>
    <h3>Mức xếp loại</h3>{APPRAISAL_RATINGS.map(r=><TextField key={r} label={`Nhãn mức ${r}`} value={summary.ratings[r]??""} onChange={value=>setSummary({...summary,ratings:{...summary.ratings,[r]:value}})} required maxLength={200}/>)}
    <h3>Tám cột của biểu Sở</h3>{summary.columns.map((c,i)=><TextField key={c.key} label={`Cột ${i+1}`} value={c.title} onChange={title=>setSummary({...summary,columns:summary.columns.map((x,j)=>i===j?{...x,title}:x)})} required maxLength={300}/>)}
    <section aria-label="Xem trước biểu"><h3>Xem trước biểu</h3><h4>{summary.title}</h4><p>{summary.audience}</p><table className="tbl"><thead><tr>{summary.columns.map(c=><th key={c.key}>{c.title}</th>)}</tr></thead><tbody><tr>{summary.columns.map(c=><td key={c.key}>—</td>)}</tr></tbody></table><p>{summary.signature}</p></section>
    <div className="row"><button className="btn btn-o" disabled={act.busy} onClick={()=>void save("draft")}>Lưu nháp</button><button className="btn btn-p" disabled={act.busy} onClick={()=>void save("published")}>Phát hành phiên bản</button>{template?<button className="btn btn-o" disabled={act.busy} onClick={()=>void save("retired")}>Ngừng sử dụng</button>:null}</div>
    {template?<LoadState s={history}>{h=><section><h3>Lịch sử mẫu</h3><ol>{h.items.map(x=><li key={x.id}>v{x.version} · {x.status} · {x.recorded_at}<button className="btn btn-o" onClick={()=>{if(x.summary)setSummary(structuredClone(x.summary));}}>Dùng nội dung phiên bản {x.version} cho bản mới</button></li>)}</ol></section>}</LoadState>:null}
  </section>;
}
