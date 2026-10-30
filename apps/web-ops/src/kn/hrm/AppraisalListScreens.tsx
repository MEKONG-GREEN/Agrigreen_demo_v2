"use client";
import Link from "next/link";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { DetailPage } from "@/components/patterns/detail";
import { NumberField } from "@/components/form";
import { PageHeader, StatusPill, DataTable } from "@/components/patterns/list";
import { LoadState, useLoad } from "@/kn/cycle/common";
import { APPRAISAL_BASE, APPRAISAL_STATUS, type AppraisalPeriodInput } from "./appraisal-api";
import { appraisalYear, APPRAISAL_RATING_LABEL } from "./appraisal-form";
import { GROUPS, MINE_BASE, asISO, useAppraisalAction, useAppraisalApi } from "./appraisal-ui";
import { vnDate } from "./staff-api";

export function AppraisalListScreen({ mine = false }: { mine?: boolean }) {
 const api=useAppraisalApi(),access=useLoad(()=>api.access(),[]),periods=useLoad(()=>api.periods(),[]),base=mine?MINE_BASE:APPRAISAL_BASE;
 const center=access.data?.perms.assessment==="all"&&["admin","hcth"].includes(access.data.group);
 return <><PageHeader title={mine?"Đánh giá của tôi":"Đợt đánh giá"} actions={center?<><Link className="btn btn-o" href={APPRAISAL_BASE+"/mau"}>Mẫu và phiếu đánh giá</Link><Link className="btn btn-p" href={APPRAISAL_BASE+"/dot/them-moi"}>Mở đợt mới</Link></>:undefined}>Danh sách và chức vụ được chụp tại ngày chốt; kết quả quý và năm được kết luận độc lập.</PageHeader>
 <LoadState s={periods}>{v=><DataTable caption="Danh sách đợt đánh giá" rows={v.items} rowKey={p=>p.id} empty="Chưa có đợt đánh giá trong phạm vi của bạn." columns={[
 {key:"period",header:"Đợt",cell:p=>p.label},{key:"cutoff",header:"Ngày chốt",cell:p=>vnDate(p.cutoff_on)},
 {key:"self",header:"Hạn tự đánh giá",cell:p=>vnDate(p.self_due_at)},{key:"manager",header:"Hạn cấp quản lý",cell:p=>vnDate(p.manager_due_at)},
 {key:"status",header:"Trạng thái",cell:p=><StatusPill tone={p.closed?"ok":"info"}>{p.closed?"Đã chốt":"Đang mở"}</StatusPill>},
 {key:"actions",header:"Thao tác",cell:p=><Link href={`${base}/dot/${p.id}`}>Xem phiếu và tiến độ</Link>}
 ]}/>}</LoadState></>;
}
export function AppraisalOpenScreen(){const router=useRouter();return <DetailPage back={{href:APPRAISAL_BASE,label:"Đợt đánh giá"}} title="Mở đợt đánh giá"><OpenAppraisal onClose={()=>router.push(APPRAISAL_BASE)} onSaved={()=>router.push(APPRAISAL_BASE)}/></DetailPage>;}
function OpenAppraisal({ onClose, onSaved }: { onClose: () => void; onSaved: () => void }) {
  const api = useAppraisalApi(), forms = useLoad(() => api.templates("form"), []), summaries = useLoad(() => api.templates("summary"), []), act = useAppraisalAction();
  const [year, setYear] = useState(String(new Date().getFullYear())), [quarter, setQuarter] = useState("4"), [cutoff, setCutoff] = useState("");
  const [self, setSelf] = useState(""), [manager, setManager] = useState(""), [summary, setSummary] = useState("");
  const [picked, setPicked] = useState<Record<string, string>>({}), [papers, setPapers] = useState<Record<string, string>>({}), [ack, setAck] = useState(false), [error, setError] = useState("");
  return <section className="card" aria-label="Mở đợt đánh giá"><h2>Mở đợt mới</h2>{act.view}<form onSubmit={e => { e.preventDefault(); const y = appraisalYear(year); if (!y || !cutoff || !self || !manager || !summary || !Object.values(picked).some(Boolean) || !papers.MS05 || !papers.MS06) { setError("Chọn năm, ngày chốt, đủ ba hạn, ít nhất một nhóm và hai mẫu tổng hợp."); return; }
    const input: AppraisalPeriodInput = { year: y, quarter: Number(quarter), cutoff_on: cutoff, self_due_at: asISO(self), manager_due_at: asISO(manager), summary_due_at: asISO(summary), forms: Object.fromEntries(Object.entries(picked).filter(([,v]) => v)), summaries: papers, acknowledge_warnings: ack };
    void act.execute("open", input, k => api.open(input, k)).then(v => { if(v) onSaved(); }); }}>
    <div className="grid2"><NumberField label="Năm đánh giá" required value={year} onChange={setYear} rule={{ integer: true, max: 2100, maxInputLength: 4 }} /><label className="field">Kỳ<select aria-label="Kỳ" value={quarter} onChange={e => setQuarter(e.target.value)}>{[1,2,3,4].map(q => <option key={q} value={q}>Quý {q}</option>)}<option value="0">Đánh giá năm</option></select></label>
      <label className="field">Ngày chốt danh sách<input aria-label="Ngày chốt danh sách" required type="date" value={cutoff} onChange={e => setCutoff(e.target.value)} /></label>
      {[{k:"self", label:"Hạn tự đánh giá", value:self, set:setSelf}, {k:"manager", label:"Hạn cấp quản lý", value:manager, set:setManager}, {k:"summary", label:"Hạn tổng hợp", value:summary, set:setSummary}].map(x => <label className="field" key={x.k}>{x.label}<input aria-label={x.label} required type="datetime-local" value={x.value} onChange={e => x.set(e.target.value)} /></label>)}</div>
    <LoadState s={forms}>{v => <>{Object.entries(GROUPS).map(([g,label]) => <label key={g} className="field">{label}<select aria-label={`Phiếu ${label}`} value={picked[g] ?? ""} onChange={e => setPicked({ ...picked, [g]:e.target.value })}><option value="">Không chọn nhóm</option>{v.items.filter(x => x.status === "published" && x.form?.group === g).map(x => <option key={x.id} value={x.id}>{x.form?.name} · v{x.version}</option>)}</select></label>)}</>}</LoadState>
    <LoadState s={summaries}>{v => <>{["MS05","MS06"].map(kind => <label className="field" key={kind}>{kind}<select aria-label={`Mẫu ${kind}`} required value={papers[kind] ?? ""} onChange={e => setPapers({ ...papers, [kind]:e.target.value })}><option value="">Chọn mẫu đã phát hành</option>{v.items.filter(x => x.status === "published" && x.summary?.kind === kind).map(x => <option key={x.id} value={x.id}>{x.summary?.name} · v{x.version}</option>)}</select></label>)}</>}</LoadState>
    <label><input type="checkbox" checked={ack} onChange={e => setAck(e.target.checked)} /> Xác nhận cảnh báo hồ sơ còn thiếu sau khi đã kiểm danh sách</label>{error ? <p role="alert">{error}</p> : null}<div className="row"><button className="btn btn-p" disabled={act.busy}>Mở đợt và phát hành phiếu</button><button type="button" className="btn btn-o" onClick={onClose}>Đóng</button></div>
  </form></section>;
}
export function AppraisalPeriodScreen({ period, mine = false }: { period: string; mine?: boolean }) {
  const api = useAppraisalApi(), list = useLoad(() => api.periods(), [period]), entries = useLoad(() => api.entries(period), [period]);
  const [unit, setUnit] = useState(""), [status, setStatus] = useState(""), base = mine ? MINE_BASE : APPRAISAL_BASE;
  const p = list.data?.items.find(x => x.id === period);
  return <><Link className="back-link" href={base}>Đợt đánh giá</Link><PageHeader title={p?.label ?? "Phiếu và đề xuất xếp loại"}>Mẫu số 05 theo đơn vị quản lý tại ngày chốt; Mẫu số 06 do HCTH nhập theo kết luận tập thể.</PageHeader>
    <LoadState s={entries}>{v => { const visible = v.items.filter(x => (!unit || x.member.management_unit === unit) && (!status || x.status === status)); return <><div className="row"><label>Đơn vị<select aria-label="Đơn vị" value={unit} onChange={e => setUnit(e.target.value)}><option value="">Tất cả trong phạm vi</option>{[...new Set(v.items.map(x => x.member.management_unit))].map(u => <option key={u}>{u}</option>)}</select></label><label>Trạng thái<select aria-label="Trạng thái" value={status} onChange={e => setStatus(e.target.value)}><option value="">Tất cả</option>{Object.entries(APPRAISAL_STATUS).map(([k,label]) => <option key={k} value={k}>{label}</option>)}</select></label></div>
    <p>{visible.length} người trong phạm vi và bộ lọc hiện tại</p><div className="row">{Object.entries(APPRAISAL_STATUS).map(([key,label])=><p key={key}>{label}: {visible.filter(x=>x.status===key).length}</p>)}</div><DataTable caption="Phiếu và tổng hợp điểm đánh giá" rows={visible} rowKey={x=>x.member.staff_id} columns={[
 {key:"name",header:"Người được đánh giá",cell:x=>x.name||x.member.code},{key:"position",header:"Chức vụ, đơn vị",cell:x=>`${x.member.position_name} · ${x.member.unit_name}`},{key:"kind",header:"Mẫu",cell:x=>x.member.summary_kind},
 {key:"self_points",header:"Điểm tự chấm",cell:x=>x.self.total||"—"},{key:"manager_points",header:"Điểm cấp quản lý",cell:x=>x.manager.total||"—"},{key:"suggested",header:"Gợi ý theo phiếu",cell:x=>APPRAISAL_RATING_LABEL[x.manager.complete?x.manager.suggested:x.self.suggested]},
 {key:"self",header:"Cá nhân",cell:x=>APPRAISAL_RATING_LABEL[x.self_rating]},{key:"collective",header:"Tập thể",cell:x=>APPRAISAL_RATING_LABEL[x.collective_rating]},
 {key:"status",header:"Trạng thái",cell:x=>APPRAISAL_STATUS[x.status]+(x.self_late||x.manager_late?" · Trễ hạn":"")},{key:"actions",header:"Thao tác",cell:x=><Link href={`${base}/dot/${period}/phieu/${x.member.staff_id}`}>Mở phiếu</Link>}
 ]}/></>; }}</LoadState>
    {p && !mine ? <AppraisalPeriodActions period={p} onChanged={() => { list.reload(); entries.reload(); }} /> : null}
  </>;
}
import { AppraisalPeriodActions } from "./AppraisalPeriodActions";