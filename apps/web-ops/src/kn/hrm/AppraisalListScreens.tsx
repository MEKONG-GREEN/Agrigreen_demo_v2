"use client";
import Link from "next/link";
import { useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/auth/AuthProvider";
import { DetailPage } from "@/components/patterns/detail";
import { NumberField, TextField } from "@/components/form";
import { PageHeader, StatusPill, DataTable } from "@/components/patterns/list";
import { LoadState, useLoad } from "@/kn/cycle/common";
import { APPRAISAL_BASE, APPRAISAL_STATUS, appraisalEntryPath, type AppraisalPeriodInput } from "./appraisal-api";
import { appraisalYear, APPRAISAL_RATINGS, APPRAISAL_RATING_LABEL } from "./appraisal-form";
import { GROUPS, asISO, useAppraisalAction, useAppraisalApi } from "./appraisal-ui";
import { createStaffApi, vnDate } from "./staff-api";
import { AppraisalImport } from "./AppraisalImport";
import { AppraisalPeriodActions } from "./AppraisalPeriodActions";
const STATES = ["not_started","entered","sent","change_requested","closed"] as const;

export function AppraisalListScreen() {
  const api=useAppraisalApi(),access=useLoad(()=>api.access(),[]),periods=useLoad(()=>api.periods(),[]);
  const center=access.data?.perms.assessment==="all"&&["admin","hcth"].includes(access.data.group);
  return <><PageHeader title="Đợt đánh giá" actions={center?<><Link className="btn btn-o" href={APPRAISAL_BASE+"/mau"}>Mẫu số 05, 06</Link><Link className="btn btn-p" href={APPRAISAL_BASE+"/dot/them-moi"}>Mở đợt mới</Link></>:undefined}>Danh sách và chức vụ được chụp tại ngày chốt. Kết quả quý, năm được lưu trong hồ sơ.</PageHeader>
    <LoadState s={periods}>{v=><DataTable caption="Danh sách đợt đánh giá" rows={v.items} rowKey={p=>p.id} empty="Chưa có đợt đánh giá trong phạm vi của bạn." columns={[
      {key:"period",header:"Đợt",cell:p=>p.label},{key:"cutoff",header:"Ngày chốt",cell:p=>vnDate(p.cutoff_on)},
      {key:"ms05",header:"Mẫu số 05",cell:p=>p.counts.MS05??"—"},{key:"ms06",header:"Mẫu số 06",cell:p=>p.counts.MS06??"—"},
      {key:"due",header:"Hạn gửi HCTH",cell:p=>vnDate(p.manager_due_at)},{key:"sent",header:"Đơn vị đã gửi",cell:p=>p.counts.units_sent??"—"},
      {key:"status",header:"Trạng thái",cell:p=><StatusPill tone={p.closed?"ok":"info"}>{p.workflow_version!=="hrm.appraisal-proposal/v4"?"Lịch sử chỉ đọc":p.closed?"Đã chốt":"Đang mở"}</StatusPill>},
      {key:"actions",header:"Thao tác",cell:p=><Link href={`${APPRAISAL_BASE}/dot/${p.id}`}>Xem đề xuất và tiến độ</Link>}
    ]}/>}</LoadState></>;
}
export function AppraisalOpenScreen(){const router=useRouter();return <DetailPage back={{href:APPRAISAL_BASE,label:"Đợt đánh giá"}} title="Mở đợt đánh giá"><OpenAppraisal onSaved={()=>router.push(APPRAISAL_BASE)}/></DetailPage>;}
function OpenAppraisal({onSaved}:{onSaved:()=>void}) {
  const api=useAppraisalApi(),{api:raw}=useAuth(),staffApi=useMemo(()=>createStaffApi(raw),[raw]),summaries=useLoad(()=>api.templates("summary"),[]),act=useAppraisalAction();
  const [year,setYear]=useState(String(new Date().getFullYear())),[quarter,setQuarter]=useState("4"),[cutoff,setCutoff]=useState(""),[due,setDue]=useState("");
  const [groups,setGroups]=useState<Array<keyof typeof GROUPS>>(["vien_chuc","lanh_dao","can_bo_xa"]),[papers,setPapers]=useState<Record<string,string>>({}),[ack,setAck]=useState(false),[error,setError]=useState("");
  const [offset,setOffset]=useState(0),[excluded,setExcluded]=useState<Record<string,{name:string;reason:string}>>({});
  const staff=useLoad(cutoff?()=>staffApi.list({at:cutoff,status:"active",limit:100,offset}):null,[cutoff,offset]);
  const kinds=[...(groups.some(g=>g!=="lanh_dao")?["MS05"]:[]),...(groups.includes("lanh_dao")?["MS06"]:[])];
  return <section className="card" aria-label="Mở đợt đánh giá">{act.view}<form onSubmit={e=>{e.preventDefault();const y=appraisalYear(year);
    if(!y||!cutoff||!due||!groups.length||kinds.some(k=>!papers[k])||Object.values(excluded).some(x=>[...x.reason.trim()].length<10)){setError("Chọn kỳ, ngày chốt, hạn gửi, nhóm và mẫu; người loại trừ cần lý do ít nhất 10 ký tự.");return;}
    const input:AppraisalPeriodInput={year:y,quarter:Number(quarter),cutoff_on:cutoff,manager_due_at:asISO(due),groups,summaries:Object.fromEntries(kinds.map(k=>[k,papers[k]!])),excluded:Object.entries(excluded).map(([staff_id,x])=>({staff_id,reason:x.reason})),acknowledge_warnings:ack};
    void act.execute("open",input,k=>api.open(input,k)).then(v=>{if(v)onSaved();});}}>
    <div className="grid2"><NumberField label="Năm đánh giá" required value={year} onChange={setYear} rule={{integer:true,max:2100,maxInputLength:4}}/><label className="field">Kỳ<select aria-label="Kỳ" value={quarter} onChange={e=>setQuarter(e.target.value)}>{[1,2,3,4].map(q=><option key={q} value={q}>Quý {q}</option>)}<option value="0">Đánh giá năm</option></select></label>
      <label className="field">Ngày chốt danh sách<input aria-label="Ngày chốt danh sách" required type="date" value={cutoff} onChange={e=>{setCutoff(e.target.value);setOffset(0);setExcluded({});}}/></label><label className="field">Hạn gửi Phòng HCTH<input aria-label="Hạn gửi Phòng HCTH" required type="datetime-local" value={due} onChange={e=>setDue(e.target.value)}/></label></div>
    <fieldset><legend>Nhóm đối tượng</legend>{Object.entries(GROUPS).map(([g,label])=><label className="field" key={g}><span><input type="checkbox" checked={groups.includes(g as keyof typeof GROUPS)} onChange={e=>setGroups(e.target.checked?[...groups,g as keyof typeof GROUPS]:groups.filter(x=>x!==g))}/>{label}</span></label>)}</fieldset>
    <LoadState s={summaries}>{v=><>{kinds.map(kind=><label className="field" key={kind}>Mẫu số {kind.slice(-2)}<select aria-label={`Mẫu ${kind}`} required value={papers[kind]??""} onChange={e=>setPapers({...papers,[kind]:e.target.value})}><option value="">Chọn mẫu đã phát hành</option>{v.items.filter(x=>x.status==="published"&&x.summary?.kind===kind).map(x=><option key={x.id} value={x.id}>{x.summary?.name} · v{x.version}</option>)}</select></label>)}</>}</LoadState>
    {cutoff?<details><summary>Loại trừ người có lý do</summary><LoadState s={staff}>{v=><><p>Danh sách đang công tác tại ngày chốt; máy chủ kiểm lại nhóm và chức vụ khi mở đợt.</p>{v.items.filter(x=>x.type!=="hop_dong").map(x=><label className="field" key={x.id}><span><input type="checkbox" checked={!!excluded[x.id]} onChange={e=>setExcluded(current=>{const next={...current};if(e.target.checked)next[x.id]={name:x.label||x.code,reason:""};else delete next[x.id];return next;})}/>{x.label||x.code} · {x.code}</span></label>)}<div className="row"><button type="button" className="btn btn-o" disabled={!offset} onClick={()=>setOffset(Math.max(0,offset-100))}>Trang trước</button><button type="button" className="btn btn-o" disabled={offset+100>=v.total} onClick={()=>setOffset(offset+100)}>Trang sau</button></div></>}</LoadState>{Object.entries(excluded).map(([id,x])=><TextField key={id} label={`Lý do loại trừ ${x.name}`} value={x.reason} onChange={reason=>setExcluded({...excluded,[id]:{...x,reason}})} multiline required maxLength={2000}/>)}</details>:null}
    <label className="field"><span><input type="checkbox" checked={ack} onChange={e=>setAck(e.target.checked)}/>Xác nhận cảnh báo thiếu chức vụ hoặc đơn vị quản lý sau khi kiểm danh sách</span></label>{error?<p role="alert">{error}</p>:null}<button className="btn btn-p" disabled={act.busy}>Mở đợt và giao các phòng nhập đề xuất</button>
  </form></section>;
}
export function AppraisalPeriodScreen({period}:{period:string}) {
  const api=useAppraisalApi(),list=useLoad(()=>api.periods(),[period]),entries=useLoad(()=>api.entries(period),[period]);
  const [unit,setUnit]=useState(""),[status,setStatus]=useState(""),[kind,setKind]=useState(""),[rating,setRating]=useState(""),[different,setDifferent]=useState(false);
  const p=list.data?.items.find(x=>x.id===period),hasReopened=entries.data?.items.some(x=>x.reopened)??false;
  return <><Link className="back-link" href={APPRAISAL_BASE}>Đợt đánh giá</Link><PageHeader title={p?.label??"Đề xuất xếp loại"}>Mẫu số 05 do lãnh đạo đơn vị nhập; Mẫu số 06 do Phòng HCTH nhập theo tập thể lãnh đạo cơ quan.</PageHeader>
    <LoadState s={entries}>{v=>{const visible=v.items.filter(x=>(!unit||x.member.management_unit===unit)&&(!status||x.status===status)&&(!kind||x.member.summary_kind===kind)&&(!rating||x.collective_rating===rating)&&(!different||x.self_rating!==x.collective_rating));return <><div className="row"><label>Mẫu<select aria-label="Lọc mẫu" value={kind} onChange={e=>setKind(e.target.value)}><option value="">Tất cả</option><option value="MS05">Mẫu số 05</option><option value="MS06">Mẫu số 06</option></select></label><label>Đơn vị<select aria-label="Đơn vị" value={unit} onChange={e=>setUnit(e.target.value)}><option value="">Tất cả trong phạm vi</option>{[...new Set(v.items.map(x=>x.member.management_unit))].map(u=><option key={u}>{u}</option>)}</select></label><label>Trạng thái<select aria-label="Trạng thái" value={status} onChange={e=>setStatus(e.target.value)}><option value="">Tất cả</option>{STATES.map(k=><option key={k} value={k}>{APPRAISAL_STATUS[k]}</option>)}</select></label><label>Mức tập thể đề xuất<select aria-label="Lọc mức tập thể" value={rating} onChange={e=>setRating(e.target.value)}><option value="">Tất cả</option>{APPRAISAL_RATINGS.map(r=><option key={r}>{r}</option>)}</select></label><label><input type="checkbox" checked={different} onChange={e=>setDifferent(e.target.checked)}/>Cá nhân khác tập thể</label></div>
    <p>{visible.length} người trong phạm vi và bộ lọc</p><div className="row">{STATES.map(key=><p key={key}>{APPRAISAL_STATUS[key]}: {visible.filter(x=>x.status===key).length}</p>)}{["HTXSNV","KHTNV"].map(r=><p key={r}>Đề xuất {r}: {visible.filter(x=>x.collective_rating===r).length}</p>)}</div><DataTable caption="Đề xuất xếp loại và tiến độ" rows={visible} rowKey={x=>x.member.staff_id} columns={[
      {key:"name",header:"Người được đánh giá",cell:x=>x.name||x.member.code},{key:"position",header:"Chức vụ, đơn vị",cell:x=>`${x.member.position_name} · ${x.member.unit_name}`},{key:"kind",header:"Mẫu",cell:x=>x.member.summary_kind},
      {key:"self",header:"Cá nhân đề xuất",cell:x=>APPRAISAL_RATING_LABEL[x.self_rating]},{key:"collective",header:"Tập thể đề xuất",cell:x=>APPRAISAL_RATING_LABEL[x.collective_rating]},{key:"reason",header:"Tóm tắt căn cứ",cell:x=>x.reason},
      {key:"status",header:"Trạng thái",cell:x=>APPRAISAL_STATUS[x.status]+(x.manager_late?" · Trễ hạn":"")},{key:"actions",header:"Thao tác",cell:x=><Link href={appraisalEntryPath(period,x.member.staff_id)}>Xem đề xuất</Link>}
    ]}/></>;}}</LoadState>
    {p?.workflow_version==="hrm.appraisal-proposal/v4"&&entries.data?.items.some(x=>x.can_manage&&["not_started","entered","change_requested"].includes(x.status))?<AppraisalImport key={p.version} period={p} kinds={[...new Set(entries.data.items.filter(x=>x.can_manage).map(x=>x.member.summary_kind))]} onChanged={()=>{list.reload();entries.reload();}}/>:null}
    {p?<AppraisalPeriodActions period={p} hasReopened={hasReopened} onChanged={()=>{list.reload();entries.reload();}}/>:null}
  </>;
}
