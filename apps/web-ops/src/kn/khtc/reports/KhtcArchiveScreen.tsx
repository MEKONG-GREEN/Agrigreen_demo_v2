"use client";

import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { DataTable, PageHeader, StatusPill, type Column } from "@/components/patterns/list";
import { LoadState, dt, useAction, useLoad } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import { REPORT_TYPES } from "../periods/api";
import { createArchiveApi, type ArchiveQuery, type ArchiveRow } from "./archive-api";
import { createReportApi } from "./api";

const PAGE = 20;
// Bounded choices: filter inputs never parse free-text numbers or allocate from user-supplied sizes.
const YEARS = Array.from({ length: 101 }, (_, i) => 2000 + i);
export function KhtcArchiveScreen() {
  const { api } = useAuth();
  const archive = useMemo(() => api ? createArchiveApi(api) : null, [api]);
  const reports = useMemo(() => api ? createReportApi(api) : null, [api]);
  const [filter, setFilter] = useState<ArchiveQuery>(() => ({ year: new Date(Date.now() + 7 * 3_600_000).getUTCFullYear(), offset: 0, limit: PAGE }));
  const [search, setSearch] = useState("");
  const rows = useLoad(archive ? () => archive.list(filter) : null, [archive, filter]);
  const action = useAction();
  const update = (patch: Partial<ArchiveQuery>) => setFilter(f => ({ ...f, ...patch, offset: 0 }));
  const offset = filter.offset ?? 0;
  const columns: Column<ArchiveRow>[] = [
    { key: "kind", header: "Loại báo cáo", cell: r => REPORT_TYPES.find(t => t.value === r.report_type)?.label ?? r.report_type },
    { key: "period", header: "Kỳ / Nội dung", cell: r => r.period_name },
    { key: "number", header: "Số văn bản", cell: r => r.document_no || "—" },
    { key: "at", header: "Xuất lúc", cell: r => dt(r.at) },
    { key: "actor", header: "Người xuất", cell: r => <span title={r.actor_ref}>{r.actor_ref.slice(0, 8)}</span> },
    { key: "version", header: "Phiên bản", cell: r => <>{`v${r.version}`} {r.final ? <StatusPill tone="ok">Bản cuối</StatusPill> : r.provisional ? <StatusPill tone="warn">Tạm tính</StatusPill> : null}</> },
    { key: "actions", header: "Thao tác", cell: r => <button type="button" className="btn btn-o btn-sm" disabled={action.busy || !reports}
      aria-label={`Tải Excel ${r.period_name} v${r.version}`}
      onClick={() => { if (reports) void action.run(async () => { const file = await reports.download(r.period_id, r.id); saveBlob(file.blob, file.name); return true; }, "Đã tải bản Excel đã xuất."); }}>Excel</button> },
  ];
  return <>
    <PageHeader title="Báo cáo KHTC đã xuất">Chỉ xem và tải tệp đã xuất trong phạm vi Trung tâm của phiên. Mặc định hiện Bản cuối của mỗi kỳ; nếu chưa có thì hiện bản xuất mới nhất.</PageHeader>
    <form className="card filters" aria-label="Bộ lọc báo cáo đã xuất" onSubmit={e => { e.preventDefault(); update({ q: search.trim() }); }}>
      <label>Năm <select aria-label="Năm" value={filter.year} onChange={e => update({ year: Number(e.target.value) })}>{YEARS.map(y => <option key={y} value={y}>{y}</option>)}</select></label>
      <label>Loại báo cáo <select aria-label="Loại báo cáo" value={filter.kind ?? ""} onChange={e => update({ kind: e.target.value as ArchiveQuery["kind"] })}><option value="">Tất cả</option>{REPORT_TYPES.map(t => <option key={t.value} value={t.value}>{t.label}</option>)}</select></label>
      <label>Tên kỳ hoặc số văn bản <input type="search" maxLength={200} value={search} onChange={e => setSearch(e.target.value)} /></label>
      <button type="submit" className="btn btn-o">Tìm kiếm</button>
      <label><input type="checkbox" checked={filter.versions ?? false} onChange={e => update({ versions: e.target.checked })} /> Mọi phiên bản</label>
      <button type="button" className="btn btn-o" onClick={rows.reload}>Làm mới</button>
    </form>
    {action.view}
    <LoadState s={rows}>{data => <>
      <DataTable caption="Báo cáo đã xuất" columns={columns} rows={data.items} rowKey={r => r.id} page={Math.floor(offset / PAGE) + 1} pageSize={PAGE} />
      <nav className="row" aria-label="Phân trang báo cáo">
        <button className="btn" disabled={offset === 0 || rows.loading} onClick={() => setFilter(f => ({ ...f, offset: Math.max(0, offset - PAGE) }))}>Trước</button>
        <span>Trang {Math.floor(offset / PAGE) + 1}</span>
        <button className="btn" disabled={!data.has_more || offset + PAGE > 100000 || rows.loading} onClick={() => setFilter(f => ({ ...f, offset: offset + PAGE }))}>Tiếp</button>
      </nav>
    </>}</LoadState>
  </>;
}