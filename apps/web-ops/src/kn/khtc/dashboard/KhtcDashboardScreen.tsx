"use client";
import Link from "next/link";
import {usePathname} from "next/navigation";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import type { ApiClient } from "@/api/client";
import { PageHeader, DataTable, type Column } from "@/components/patterns/list";
import { LoadState, useLoad, dt } from "@/kn/cycle/common";
import { createKhtcApi, DEPT_LABEL } from "../api";
import { createDashboardApi, type Dashboard, type DashboardQuery } from "./api";
const YEARS = Array.from({ length: 101 }, (_, i) => i + 2000);
const stateLabel: Record<string, string> = { opened: "Đã mở", collecting: "Đang thu thập", late: "Trễ hạn", complete: "Đủ dữ liệu", closed: "Đã đóng", exported: "Đã xuất", not_started: "Chưa gửi", submitted: "Đã gửi", supplement: "Cần bổ sung", confirmed: "Đã xác nhận", locked: "Đã khóa", unknown: "Chưa đọc được", open: "Chưa chốt" };
export function KhtcDashboardScreen() {
    const auth = useAuth();
    const pathname=usePathname();
    const p=auth.principal,c=auth.kn?.context;
    const principal=p?[p.subject,p.tenantId,p.orgRef,p.mfa,p.level,p.scopeAll,[...p.roles].sort(),[...p.scopeUnits].sort()]:null;
    const context=c?[c.context_id,c.assignment_id,c.role,c.department_code,c.org_ref,c.communes.map(x=>x.code).sort(),c.expires_at,c.support_session_id,c.on_behalf_of,c.read_only,c.manager,c.mgmt_level_code,c.province_group,c.duties,c.internal_unit_code,c.powers]:null;
    const assignments=auth.kn?.assignments.map(a=>[a.assignment_id,a.role,a.org_ref,a.mgmt_level_code,a.communes.map(x=>x.code).sort(),a.departments.map(d=>[d.code,d.status,d.read_only])]);
    const scope = JSON.stringify([pathname,auth.status,principal,auth.kn?.status,context,assignments,auth.kn?.support?.target,!!auth.scopeWarning]);
    if(!pathname||auth.status!=="authenticated"||!auth.principal||auth.kn?.status!=="active"||!auth.kn.context||!auth.api) return <p role="status">Chọn ngữ cảnh Khuyến nông để xem số liệu.</p>;
    return <DashboardApiBoundary key={scope} api={auth.api}/>;
}
function DashboardApiBoundary({api}:{api:ApiClient}) {
    // Raw client identity can change while principal/context values stay equal.
    // The monotonic key also separates a committed client A -> B -> A lifetime.
    const [identity,setIdentity]=useState({api,epoch:0});
    if(identity.api!==api){setIdentity({api,epoch:identity.epoch+1});return null;}
    return <DashboardWorkspace key={identity.epoch} api={api}/>;
}
function DashboardWorkspace({api}:{api:ApiClient}) {
    const [filter, setFilter] = useState<DashboardQuery>(() => { const d = new Date(Date.now() + 7 * 3600000); return { year: d.getUTCFullYear(), quarter: Math.floor(d.getUTCMonth() / 3) + 1 }; });
    const update = (q: Partial<DashboardQuery>) => setFilter(f => ({ ...f, ...q }));
    // Query replacement destroys both old data and its catalog immediately.
    // A late response can only update the unmounted resource that requested it.
    return <DashboardResource key={JSON.stringify(filter)} api={api} filter={filter} update={update}/>;
}
function DashboardResource({api,filter,update}:{api:ApiClient;filter:DashboardQuery;update:(q:Partial<DashboardQuery>)=>void}) {
    const service = useMemo(() => api ? createDashboardApi(api) : null, [api]);
    const catalogs = useMemo(() => api ? createKhtcApi(api) : null, [api]);
    const data = useLoad(service ? () => service.get(filter) : null, [service, filter]);
    const fields = useLoad(catalogs ? () => catalogs.catalog("field") : null, [catalogs]);
    const reports: Column<Dashboard["reports"][number]>[] = [{ key: "name", header: "Báo cáo", cell: r => <Link href={`/kn/khtc/tong-hop/${r.id}`}>{r.name}</Link> }, { key: "status", header: "Trạng thái", cell: r => stateLabel[r.state] ?? r.state }, { key: "cutoff", header: "Số liệu đến", cell: r => r.cutoff }, { key: "at", header: "Xuất lúc", cell: r => r.exported_at ? dt(r.exported_at) : "—" }];
    const status: Column<Dashboard["status"][number]>[] = [{ key: "name", header: "Phòng / nguồn", cell: r => <Link href={`/kn/khtc/tien-do-ky/${r.period_id}`}>{r.name || DEPT_LABEL[r.department] || r.department}</Link> }, { key: "period", header: "Kỳ", cell: r => r.period_name }, { key: "kind", header: "Loại", cell: r => r.kind === "form" ? "Phiếu" : "Nguồn module" }, { key: "state", header: "Trạng thái", cell: r => r.kind === "source" && r.state === "closed" ? "Đã chốt" : stateLabel[r.state] ?? r.state }, { key: "at", header: "Thời điểm", cell: r => r.at ? dt(r.at) : "—" }];
    return <>
  <PageHeader title="Trang chủ tổng hợp KHTC">Số liệu từ phiếu đã gửi và nguồn đã chốt; cập nhật lại khi mở trang hoặc làm mới.</PageHeader>
  <form className="card filters" aria-label="Bộ lọc dashboard" onSubmit={e => e.preventDefault()}>
   <label>Năm<select aria-label="Năm" value={filter.year} onChange={e => update({ year: Number(e.target.value), through_date: undefined })}>{YEARS.map(y => <option key={y}>{y}</option>)}</select></label>
   <label>Quý<select aria-label="Quý" value={filter.quarter} onChange={e => update({ quarter: Number(e.target.value), through_date: undefined })}>{[1, 2, 3, 4].map(q => <option key={q} value={q}>Quý {q}</option>)}</select></label>
   <label>Đến ngày<input type="date" aria-label="Đến ngày" value={filter.through_date ?? ""} min={`${filter.year}-${String(filter.quarter * 3 - 2).padStart(2,"0")}-01`} max={new Date(Date.UTC(filter.year,filter.quarter * 3,0)).toISOString().slice(0,10)} onChange={e => update({through_date:e.target.value || undefined})}/></label>
   <label>Phòng<select aria-label="Phòng" value={filter.department ?? ""} onChange={e => update({ department: e.target.value })}><option value="">Tất cả</option>{Object.entries(DEPT_LABEL).map(([code, name]) => <option key={code} value={code}>{name}</option>)}</select></label>
   <LoadState s={fields}>{f => <label>Lĩnh vực (tiến độ chương trình)<select aria-label="Lĩnh vực" value={filter.field ?? ""} onChange={e => update({ field: e.target.value })}><option value="">Tất cả</option>{f.items.filter(x => x.active).map(x => <option key={x.code} value={x.code}>{x.name}</option>)}</select></label>}</LoadState>
   <button type="button" className="btn btn-o" onClick={data.reload} disabled={data.loading}>Làm mới</button>
  </form>
  <LoadState s={data}>{d => <>
   <section aria-label="Chỉ số nóng" className="row" style={{ flexWrap: "wrap" }}>
    {[["Báo cáo đã xuất trong năm", String(d.counts.exported), "/kn/khtc/bao-cao-da-xuat"], ["Kỳ đang mở", String(d.counts.open), "/kn/khtc/ky-bao-cao"], ["Phiếu đã gửi", `${d.counts.sent}/${d.counts.forms}`, "/kn/khtc/ky-bao-cao"], ["Hạng mục đạt ít nhất 100% KH", !d.progress_window?.unavailable_reason && d.counts.items > 0 && (d.progress_window || d.progress_period) ? `${d.counts.achieved}/${d.counts.items}` : "—", "/kn/khtc/tien-do"], ["Hạng mục chậm tiến độ", !d.progress_window?.unavailable_reason && d.counts.items > 0 && (d.progress_window || d.progress_period) ? String(d.counts.slow) : "—", "/kn/khtc/tien-do"]].map(([label, value, path]) => <Link key={label} className="card" href={path ?? "/kn/khtc/trang-chu"}><span>{label}</span><strong style={{ display: "block", fontSize: 26 }}>{value}</strong></Link>)}
   </section>
   <section className="card"><h2>Tiến độ các chương trình</h2>
    {d.progress_window ? <p>Mẫu 5 trực tiếp · Yêu cầu đến {d.progress_window.through_date} · Số liệu tuần đến {d.progress_window.quantity_to} · Mốc thực tế {dt(d.progress_window.effective_cutoff)}. Đọc lúc {dt(d.as_of)}.</p> : d.progress_period ? <p>Mẫu 5: <Link href={`/kn/khtc/tong-hop/${d.progress_period.id}`}>{d.progress_period.name}</Link> · Số liệu đến {d.progress_period.cutoff}. Phiếu và nguồn được đọc lại lúc {dt(d.as_of)}.</p> : <p>Chưa có tuần báo cáo đủ kết thúc trong năm, quý đã chọn.</p>}
    {d.progress_window && d.progress_period && <p>Kỳ Mẫu 5 đã mở: <Link href={`/kn/khtc/tong-hop/${d.progress_period.id}`}>{d.progress_period.name}</Link> · Số liệu của kỳ đến {d.progress_period.cutoff}.</p>}
    {d.progress_window?.unavailable_reason && <p role="status">Tiến độ trực tiếp chưa khả dụng trong ngữ cảnh Trung ương. Các thẻ và trạng thái kỳ vẫn được đọc theo quyền hiện hành.</p>}
    {d.progress_window && !d.progress_window.unavailable_reason && !d.progress_window.plan_version_id && d.progress.rows.length === 0 && d.counts.items === 0 && !d.department && !d.field && <p role="status">Chưa có Kế hoạch năm hiệu lực cho mốc số liệu đã chọn.</p>}
    {!d.progress_window?.unavailable_reason && d.progress.missing > 0 && <p role="status">{d.progress.missing} hạng mục chưa đủ dữ liệu; chưa đưa vào tỷ lệ bình quân.</p>}
    {!d.progress_window?.unavailable_reason && d.progress.programs.map(p => <div key={p.ref} className="row">{d.progress_period && !d.progress_window ? <Link href={`/kn/khtc/tong-hop/${d.progress_period.id}`}>{p.name}</Link> : <span>{p.name}</span>}<strong>{p.percent === null ? "Chưa có số liệu" : `${p.percent.replace(".", ",")}%`}</strong><span>{p.known}/{p.items} hạng mục có số liệu</span></div>)}
   </section>
   <section className="card"><h2>Báo cáo theo quý, năm</h2><DataTable caption="Báo cáo theo quý, năm" rows={d.reports} columns={reports} rowKey={r => r.id}/>{!d.reports.length && <p>Chưa mở kỳ báo cáo quý hoặc năm.</p>}</section>
   <section className="card"><h2>Phiếu và nguồn</h2>{d.counts.open === 0 ? <p>Chưa có kỳ đang mở</p> : <DataTable caption="Phiếu và nguồn" rows={d.status} columns={status} rowKey={r => `${r.period_id}:${r.kind}:${r.department}:${r.name}`}/>}</section>
   <section className="card"><h2>Cảnh báo</h2>{d.progress_window?.unavailable_reason ? <p>Chưa có số liệu trực tiếp khả dụng để xác định cảnh báo.</p> : d.counts.slow > 0 ? <p role="status">{d.counts.slow} hạng mục đạt dưới 30% kế hoạch sau 3/4 năm kế hoạch.</p> : <p>Chưa có cảnh báo chậm tiến độ từ số liệu đã đủ.</p>}</section>
   {d.can_act && <Link className="btn btn-p" href="/kn/khtc/ky-bao-cao">Mở kỳ mới</Link>}
  </>}</LoadState>
 </>;
}
