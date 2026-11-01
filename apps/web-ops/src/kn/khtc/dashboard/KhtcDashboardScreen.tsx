"use client";
import Link from "next/link";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, DataTable, type Column } from "@/components/patterns/list";
import { LoadState, useLoad, dt } from "@/kn/cycle/common";
import { createKhtcApi, DEPT_LABEL } from "../api";
import { createDashboardApi, type Dashboard, type DashboardQuery } from "./api";
const YEARS = Array.from({ length: 101 }, (_, i) => i + 2000);
const stateLabel: Record<string, string> = { opened: "Đã mở", collecting: "Đang thu thập", late: "Trễ hạn", complete: "Đủ dữ liệu", closed: "Đã đóng", exported: "Đã xuất", not_started: "Chưa gửi", submitted: "Đã gửi", supplement: "Cần bổ sung", confirmed: "Đã xác nhận", locked: "Đã khóa", unknown: "Chưa đọc được", open: "Chưa chốt" };
export function KhtcDashboardScreen() {
    const { api } = useAuth();
    const service = useMemo(() => api ? createDashboardApi(api) : null, [api]);
    const catalogs = useMemo(() => api ? createKhtcApi(api) : null, [api]);
    const [filter, setFilter] = useState<DashboardQuery>(() => { const d = new Date(Date.now() + 7 * 3600000); return { year: d.getUTCFullYear(), quarter: Math.floor(d.getUTCMonth() / 3) + 1 }; });
    const data = useLoad(service ? () => service.get(filter) : null, [service, filter]);
    const fields = useLoad(catalogs ? () => catalogs.catalog("field") : null, [catalogs]);
    const update = (q: Partial<DashboardQuery>) => setFilter(f => ({ ...f, ...q }));
    const reports: Column<Dashboard["reports"][number]>[] = [{ key: "name", header: "Báo cáo", cell: r => <Link href={`/kn/khtc/tong-hop/${r.id}`}>{r.name}</Link> }, { key: "status", header: "Trạng thái", cell: r => stateLabel[r.state] ?? r.state }, { key: "cutoff", header: "Số liệu đến", cell: r => r.cutoff }, { key: "at", header: "Xuất lúc", cell: r => r.exported_at ? dt(r.exported_at) : "—" }];
    const status: Column<Dashboard["status"][number]>[] = [{ key: "name", header: "Phòng / nguồn", cell: r => <Link href={`/kn/khtc/tien-do-ky/${r.period_id}`}>{r.name || DEPT_LABEL[r.department] || r.department}</Link> }, { key: "period", header: "Kỳ", cell: r => r.period_name }, { key: "kind", header: "Loại", cell: r => r.kind === "form" ? "Phiếu" : "Nguồn module" }, { key: "state", header: "Trạng thái", cell: r => r.kind === "source" && r.state === "closed" ? "Đã chốt" : stateLabel[r.state] ?? r.state }, { key: "at", header: "Thời điểm", cell: r => r.at ? dt(r.at) : "—" }];
    return <>
  <PageHeader title="Trang chủ tổng hợp KHTC">Số liệu từ phiếu đã gửi và nguồn đã chốt; cập nhật lại khi mở trang hoặc làm mới.</PageHeader>
  <form className="card filters" aria-label="Bộ lọc dashboard" onSubmit={e => e.preventDefault()}>
   <label>Năm<select aria-label="Năm" value={filter.year} onChange={e => update({ year: Number(e.target.value) })}>{YEARS.map(y => <option key={y}>{y}</option>)}</select></label>
   <label>Quý<select aria-label="Quý" value={filter.quarter} onChange={e => update({ quarter: Number(e.target.value) })}>{[1, 2, 3, 4].map(q => <option key={q} value={q}>Quý {q}</option>)}</select></label>
   <label>Phòng<select aria-label="Phòng" value={filter.department ?? ""} onChange={e => update({ department: e.target.value })}><option value="">Tất cả</option>{Object.entries(DEPT_LABEL).map(([code, name]) => <option key={code} value={code}>{name}</option>)}</select></label>
   <LoadState s={fields}>{f => <label>Lĩnh vực (tiến độ chương trình)<select aria-label="Lĩnh vực" value={filter.field ?? ""} onChange={e => update({ field: e.target.value })}><option value="">Tất cả</option>{f.items.filter(x => x.active).map(x => <option key={x.code} value={x.code}>{x.name}</option>)}</select></label>}</LoadState>
   <button type="button" className="btn btn-o" onClick={data.reload} disabled={data.loading}>Làm mới</button>
  </form>
  <LoadState s={data}>{d => <>
   <section aria-label="Chỉ số nóng" className="row" style={{ flexWrap: "wrap" }}>
    {[["Báo cáo đã xuất trong năm", String(d.counts.exported), "/kn/khtc/bao-cao-da-xuat"], ["Kỳ đang mở", String(d.counts.open), "/kn/khtc/ky-bao-cao"], ["Phiếu đã gửi", `${d.counts.sent}/${d.counts.forms}`, "/kn/khtc/ky-bao-cao"], ["Hạng mục đạt ít nhất 100% KH", d.progress_period ? `${d.counts.achieved}/${d.counts.items}` : "—", "/kn/khtc/tien-do"], ["Hạng mục chậm tiến độ", d.progress_period ? String(d.counts.slow) : "—", "/kn/khtc/tien-do"]].map(([label, value, path]) => <Link key={label} className="card" href={path ?? "/kn/khtc/trang-chu"}><span>{label}</span><strong style={{ display: "block", fontSize: 26 }}>{value}</strong></Link>)}
   </section>
   <section className="card"><h2>Tiến độ các chương trình</h2>
    {d.progress_period ? <><p>Mẫu 5: <Link href={`/kn/khtc/tong-hop/${d.progress_period.id}`}>{d.progress_period.name}</Link> · Số liệu đến {d.progress_period.cutoff}. Phiếu và nguồn được đọc lại lúc {dt(d.as_of)}.</p>{d.progress.missing > 0 && <p role="status">{d.progress.missing} hạng mục chưa đủ dữ liệu; chưa đưa vào tỷ lệ bình quân.</p>}</> : <p>Chưa có kỳ Mẫu 5 phù hợp với năm, quý đã chọn.</p>}
    {d.progress.programs.map(p => <div key={p.ref} className="row"><Link href={d.progress_period ? `/kn/khtc/tong-hop/${d.progress_period.id}` : "/kn/khtc/tien-do"}>{p.name}</Link><strong>{p.percent === null ? "Chưa có số liệu" : `${p.percent.replace(".", ",")}%`}</strong><span>{p.known}/{p.items} hạng mục có số liệu</span></div>)}
   </section>
   <section className="card"><h2>Báo cáo theo quý, năm</h2><DataTable caption="Báo cáo theo quý, năm" rows={d.reports} columns={reports} rowKey={r => r.id}/>{!d.reports.length && <p>Chưa mở kỳ báo cáo quý hoặc năm.</p>}</section>
   <section className="card"><h2>Phiếu và nguồn</h2>{d.counts.open === 0 ? <p>Chưa có kỳ đang mở</p> : <DataTable caption="Phiếu và nguồn" rows={d.status} columns={status} rowKey={r => `${r.period_id}:${r.kind}:${r.department}:${r.name}`}/>}</section>
   <section className="card"><h2>Cảnh báo</h2>{d.counts.slow > 0 ? <p role="status">{d.counts.slow} hạng mục đạt dưới 30% kế hoạch sau 3/4 năm kế hoạch.</p> : <p>Chưa có cảnh báo chậm tiến độ từ số liệu đã đủ.</p>}</section>
   {d.can_act && <Link className="btn btn-p" href="/kn/khtc/ky-bao-cao">Mở kỳ mới</Link>}
  </>}</LoadState>
 </>;
}
