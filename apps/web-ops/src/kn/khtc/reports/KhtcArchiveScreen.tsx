"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import type { ApiClient } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { DataTable, PageHeader, StatusPill, type Column } from "@/components/patterns/list";
import { LoadState, dt, useAction, useLoad } from "@/kn/cycle/common";
import { ArchiveSelectionBoundary, ArchiveBatchBar, useArchiveSelection, useArchiveAlive } from "./archive-selection";
import { saveBlob } from "@/lib/files";
import { REPORT_TYPES } from "../periods/api";
import { createArchiveApi, type ArchiveQuery, type ArchiveRow } from "./archive-api";
import { CompiledTemplateDocument } from "../templates/CompiledTemplateDocument";
import { createReportApi } from "./api";

import { ArchivePeriodPicker } from "./ArchivePeriodPicker";

const PAGE = 20;
// Bounded choices: filter inputs never parse free-text numbers or allocate from user-supplied sizes.
const YEARS = Array.from({ length: 101 }, (_, i) => 2000 + i);
export function KhtcArchiveScreen() {
  const auth = useAuth();
  // A changed KN tenant/session must discard names, selection, preview and pending lookup state.
  return <ArchiveSelectionBoundary><KhtcArchiveBody key={JSON.stringify([auth.principal, auth.kn?.context, auth.kn?.support])} api={auth.api}/></ArchiveSelectionBoundary>;
}
function KhtcArchiveBody({api}: {api: ApiClient | null}) {
  const archive = useMemo(() => api ? createArchiveApi(api) : null, [api]);
  const reports = useMemo(() => api ? createReportApi(api) : null, [api]);
  const {filter,setFilter,period,setPeriod,files:selected,setFiles:setSelected,toggle} = useArchiveSelection();
  const alive = useArchiveAlive();
  const [choosingPeriod, setChoosingPeriod] = useState(false);
  const [search, setSearch] = useState("");
  const rows = useLoad(archive ? () => archive.list(filter) : null, [archive, filter]);
  const action = useAction();
  const [preview, setPreview] = useState<ArchiveRow | null>(null);
  const snapshot = useLoad(archive && preview ? () => archive.preview(preview.period_id, preview.id) : null, [archive, preview]);
  const update = (patch: Partial<ArchiveQuery>) => {
    if (patch.year !== undefined || patch.kind !== undefined) { setPeriod(null); setChoosingPeriod(false); patch.period_id = undefined; }
    setSelected([]); setPreview(null);
    setFilter(f => ({ ...f, ...patch, offset: 0 }));
  };
  const offset = filter.offset ?? 0;
  const columns: Column<ArchiveRow>[] = [
    { key: "select", header: "Chọn", cell: r => <input type="checkbox" aria-label={`Chọn ${r.period_name} v${r.version}`} checked={selected.some(x => x.export_id === r.id && x.format !== "word")} disabled={!selected.some(x => x.export_id === r.id && x.format !== "word") && selected.length >= 10} onChange={e => toggle({period_id:r.period_id, export_id:r.id, label:`Excel ${r.period_name} v${r.version}`}, e.target.checked)} /> },
    { key: "preview", header: "Xem", cell: r => <button type="button" className="btn btn-o btn-sm" onClick={() => setPreview(r)} aria-label={`Xem ${r.period_name} v${r.version}`}>Xem trước</button> },
    { key: "kind", header: "Loại báo cáo", cell: r => REPORT_TYPES.find(t => t.value === r.report_type)?.label ?? r.report_type },
    { key: "period", header: "Kỳ / Nội dung", cell: r => r.period_name },
    { key: "number", header: "Số văn bản", cell: r => r.document_no || "—" },
    { key: "at", header: "Xuất lúc", cell: r => dt(r.at) },
    { key: "actor", header: "Người xuất", cell: r => <span title={r.actor_ref}>{r.actor_ref.slice(0, 8)}</span> },
    { key: "version", header: "Phiên bản", cell: r => <>{`v${r.version}`} {r.final ? <StatusPill tone="ok">Bản cuối</StatusPill> : r.provisional ? <StatusPill tone="warn">Tạm tính</StatusPill> : null}</> },
    { key: "actions", header: "Thao tác", cell: r => <><Link className="btn btn-o btn-sm" href={`/kn/khtc/bao-cao-da-xuat/${r.period_id}/${r.id}/word`} aria-label={`Word đã lưu ${r.period_name} v${r.version}`}>Word đã lưu</Link> <button type="button" className="btn btn-o btn-sm" disabled={action.busy || !reports}
      aria-label={`Tải Excel ${r.period_name} v${r.version}`}
      onClick={() => { if (reports) void action.run(async () => { const file = await reports.download(r.period_id, r.id); if (alive.current) saveBlob(file.blob, file.name); return true; }, "Đã tải bản Excel đã xuất."); }}>Excel</button></> },
  ];
  return <>
    <PageHeader title="Báo cáo KHTC đã xuất">Chỉ xem và tải tệp đã xuất trong phạm vi Trung tâm của phiên. Mặc định hiện Bản cuối của mỗi kỳ; nếu chưa có thì hiện bản xuất mới nhất.</PageHeader>
    <form className="card filters" aria-label="Bộ lọc báo cáo đã xuất" onSubmit={e => { e.preventDefault(); update({ q: search.trim() }); }}>
      <label>Năm <select aria-label="Năm" value={filter.year} onChange={e => update({ year: Number(e.target.value) })}>{YEARS.map(y => <option key={y} value={y}>{y}</option>)}</select></label>
      <label>Loại báo cáo <select aria-label="Loại báo cáo" value={filter.kind ?? ""} onChange={e => update({ kind: e.target.value as ArchiveQuery["kind"] })}><option value="">Tất cả</option>{REPORT_TYPES.map(t => <option key={t.value} value={t.value}>{t.label}</option>)}</select></label>
      <span>Kỳ: {period?.name ?? "Tất cả"}</span>
      <button type="button" className="btn btn-o" disabled={!archive} onClick={() => setChoosingPeriod(true)}>Chọn kỳ</button>
      {period && <button type="button" className="btn btn-o" onClick={() => {setPeriod(null);update({period_id:undefined});}}>Bỏ lọc kỳ</button>}
      <label>Tên kỳ hoặc số văn bản <input type="search" maxLength={200} value={search} onChange={e => setSearch(e.target.value)} /></label>
      <button type="submit" className="btn btn-o">Tìm kiếm</button>
      <label><input type="checkbox" checked={filter.versions ?? false} onChange={e => update({ versions: e.target.checked })} /> Mọi phiên bản</label>
      <button type="button" className="btn btn-o" onClick={rows.reload}>Làm mới</button>
    </form>
    {choosingPeriod && archive && <ArchivePeriodPicker key={`${filter.year}:${filter.kind ?? ""}`} api={archive} year={filter.year} kind={filter.kind} onClose={() => setChoosingPeriod(false)} onChoose={p => {setPeriod(p);setChoosingPeriod(false);update({period_id:p.id});}}/>}
    <ArchiveBatchBar />
    {action.view}
    {preview && <section className="card sec" aria-label="Bản xuất đã lưu"><h2>{preview.period_name} · v{preview.version}</h2><button type="button" className="btn btn-o" onClick={() => setPreview(null)}>Đóng xem trước</button><LoadState s={snapshot}>{data => <>{data.template_document ? <CompiledTemplateDocument document={data.template_document} /> : <><h3>Nội dung bản xuất</h3>{Object.entries(data.content.texts).map(([key, text]) => <p key={key}>{text}</p>)}<table><caption>Số liệu bản xuất</caption><thead><tr><th>Hạng mục</th><th>Trong kỳ</th><th>Lũy kế</th></tr></thead><tbody>{data.content.data.rows.map(r => <tr key={r.ref}><td>{r.name}</td><td>{r.in_period}</td><td>{r.year_to_date}</td></tr>)}</tbody></table></>}</>}</LoadState></section>}
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