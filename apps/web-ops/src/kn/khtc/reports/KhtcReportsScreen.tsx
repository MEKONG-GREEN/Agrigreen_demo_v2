"use client";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useAction, useLoad } from "@/kn/cycle/common";
import { DEPT_LABEL, TEMPLATE_LABEL } from "../api";
import { REPORT_TYPES, createKhtcPeriodApi } from "../periods/api";
import { createReportApi, filterRows, type ReportApi, type ReportDetail, type ReportFilter } from "./api";
import { ExternalCells } from "../external/ExternalCells";
const ADMIN: Record<string, string> = { document_no: "Số văn bản", place: "Địa danh", report_date: "Ngày báo cáo", recipient: "Kính gửi", basis: "Căn cứ", recipients: "Nơi nhận", authority: "Thẩm quyền ký", signer_title: "Chức vụ", signer: "Người ký" };
function saveFile(file: {
    blob: Blob;
    name: string;
}) {
    const url = URL.createObjectURL(file.blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = file.name;
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
}
export function KhtcReportsScreen({ mode = "editor" }: {
    mode?: "editor" | "progress" | "history";
}) {
    const { api } = useAuth();
    const reports = useMemo(() => api ? createReportApi(api) : null, [api]);
    const periods = useMemo(() => api ? createKhtcPeriodApi(api) : null, [api]);
    const [year, setYear] = useState(new Date().getFullYear());
    const [kind, setKind] = useState("");
    const [selected, setSelected] = useState("");
    const list = useLoad(periods ? () => periods.list(year) : null, [periods, year]);
    const detail = useLoad(reports && selected ? () => reports.get(selected) : null, [reports, selected]);
    return <>
  <PageHeader title={mode === "history" ? "Báo cáo đã xuất" : mode === "progress" ? "Tiến độ các chương trình" : "Soạn báo cáo KHTC"}><p>Mẫu 1–5 · Số liệu lấy từ nguồn đã chốt và phiếu đã gửi của các phòng.</p></PageHeader>
  <section className="card sec">
   <label>Năm <input type="number" min="2000" max="2100" value={year} onChange={e => { setYear(Number(e.target.value)); setSelected(""); }}/></label>{" "}
   <label>Loại báo cáo <select value={kind} onChange={e => setKind(e.target.value)}><option value="">Tất cả</option>{REPORT_TYPES.filter(t => t.value !== "adhoc").map(t => <option key={t.value} value={t.value}>{t.label}</option>)}</select></label>
   <LoadState s={list}>{items => <ul>{items.filter(p => p.report_type !== "adhoc" && p.state !== "cancelled" && (!kind || p.report_type === kind)).map(p => <li key={p.id}><button className="btn" aria-pressed={selected === p.id} onClick={() => setSelected(p.id)}>{p.name}</button></li>)}{!items.length && <li>Chưa có kỳ báo cáo trong năm.</li>}</ul>}</LoadState>
  </section>
  {selected && reports && <LoadState s={detail}>{d => <ReportEditor key={`${d.period_id}:${d.version}:${d.final?.export_id ?? ""}`} d={d} api={reports} mode={mode} reload={detail.reload}/>}</LoadState>}
 </>;
}
function ReportEditor({ d, api, mode, reload }: {
    d: ReportDetail;
    api: ReportApi;
    mode: "editor" | "progress" | "history";
    reload: () => void;
}) {
    const id = d.period_id;
    const { data } = d.content;
    const [admin, setAdmin] = useState<Record<string, string | null>>({});
    const [texts, setTexts] = useState<Record<string, string>>({});
    const [ack, setAck] = useState(false);
    const [filter, setFilter] = useState<ReportFilter>({});
    const [exportFiltered, setExportFiltered] = useState(false);
    const allItems = (data.rows ?? []).filter(r => r.kind === "item");
    const history = useLoad(() => api.history(id), [api, id]);
    const words = useLoad(() => api.words(id), [api, id]);
    const act = useAction();
    const dirty = Object.keys(admin).length > 0 || Object.keys(texts).length > 0;
    const writable = d.editable && !act.busy;
    const rows = filterRows(data.rows ?? [], filter);
    const depts = [...new Set((data.rows ?? []).map(r => r.department).filter(Boolean))];
    const communes = [...new Set((data.rows ?? []).flatMap(r => r.communes ?? []))];
    return <section className="card sec">
  <h2>{data.period.name} · {TEMPLATE_LABEL[data.template]}</h2>
  <p>Phiên bản soạn {d.version} · Kế hoạch v{data.plan.Version} · Ngày chốt {data.period.cutoff.slice(0, 10)}</p>
  {d.final ? <p><StatusPill tone="ok">Bản cuối</StatusPill> Đã khóa nội dung và cập nhật số liệu.</p> : null}
  {d.source_changed && <p role="alert" className="alert alert-warn">Nguồn đã thay đổi. Cập nhật số liệu trước khi đánh dấu Bản cuối. Các tệp đã xuất được giữ nguyên.</p>}
  {d.source_error && <p role="alert" className="alert alert-warn">{d.source_error}</p>}
  {!!data.provisional?.length && <div role="status" className="alert alert-warn"><strong>Tạm tính</strong><ul>{data.provisional.map((reason, i) => <li key={i}>{reason}</li>)}</ul></div>}
  {act.view}
  {mode !== "history" && <>
   {mode === "editor" && <>
    <h3>Thông tin văn bản</h3>
    {Object.entries(ADMIN).map(([key, label]) => {
                    const field = d.content.admin[key];
                    const value = key in admin ? admin[key] ?? field?.system ?? "" : field?.override ?? field?.system ?? "";
                    return <div key={key}><label>{label} <input type={key === "report_date" ? "date" : "text"} maxLength={2000} disabled={!writable} value={value} onChange={e => setAdmin({ ...admin, [key]: e.target.value })}/></label>{" "}<button className="btn" disabled={!writable} onClick={() => setAdmin({ ...admin, [key]: null })}>Về mặc định</button>{field?.override !== null && field?.override !== undefined && <small> Đã sửa thủ công {dt(field.at)}</small>}{field?.system_changed && <small> · Giá trị mặc định đã đổi: {field.system}</small>}</div>;
                })}
    <h3>Nội dung lời văn</h3>
    {data.template === "mau_5" && <p>Mục II được khởi tạo từ báo cáo tiến độ kỳ trước, nếu có. Nội dung đã lưu của kỳ này được giữ khi cập nhật số liệu.</p>}
    {d.sections.filter(s => s.editable).map(s => <div key={s.key}><label>{s.key} {s.title}<textarea rows={4} maxLength={12000} disabled={!writable} value={texts[s.key] ?? d.content.texts[s.key] ?? ""} onChange={e => setTexts({ ...texts, [s.key]: e.target.value })}/></label></div>)}
    <button className="btn btn-primary" disabled={!writable} onClick={() => act.run(async () => { await api.save(id, { base_version: d.version, admin, texts }); reload(); }, "Đã lưu bản soạn")}>Lưu bản soạn</button>{" "}
    <button className="btn" disabled={!d.can_refresh || act.busy || dirty} onClick={() => act.run(async () => { await api.refresh(id, d.version); reload(); }, "Đã cập nhật số liệu")}>Cập nhật số liệu</button>
    {dirty && <p role="status">Có thay đổi chưa lưu. Lưu bản soạn trước khi cập nhật hoặc xuất Excel.</p>}
   </>}
   <dl className="row" aria-label="Thẻ đếm tiến độ">{[["Tổng hạng mục", allItems.length], ["Đạt kế hoạch", allItems.filter(r => r.level === "achieved").length], ["30 – dưới 100%", allItems.filter(r => r.level === "progressing").length], ["Dưới 30%", allItems.filter(r => r.level === "slow").length]].map(([label, count]) => <div key={String(label)}><dt>{label}</dt><dd>{count}</dd></div>)}</dl>
   <h3>Số liệu tổng hợp</h3><p>Chỉ đọc · Các số lượng và tỷ lệ được hệ thống tính từ nguồn đã lưu.</p>
   {(data.template === "mau_5" || mode === "progress") && <div>
    <label>Phòng <select value={filter.department ?? ""} onChange={e => setFilter({ ...filter, department: e.target.value })}><option value="">Tất cả</option>{depts.map(x => <option key={x} value={x}>{DEPT_LABEL[x] ?? x}</option>)}</select></label>{" "}
    <label>Chương trình <select value={filter.program ?? ""} onChange={e => setFilter({ ...filter, program: e.target.value })}><option value="">Tất cả</option>{(data.rows ?? []).filter(r => r.kind === "program").map(r => <option key={r.ref} value={r.ref}>{r.name}</option>)}</select></label>{" "}
    <label>Mức đạt <select value={filter.level ?? ""} onChange={e => setFilter({ ...filter, level: e.target.value as ReportFilter["level"] })}><option value="">Tất cả</option><option value="achieved">Đạt kế hoạch (≥ 100%)</option><option value="progressing">30 – dưới 100%</option><option value="slow">Dưới 30%</option><option value="qualitative">Không định lượng</option></select></label>{" "}
    <label>Xã/điểm <select value={filter.commune ?? ""} onChange={e => setFilter({ ...filter, commune: e.target.value })}><option value="">Tất cả</option>{communes.map(x => <option key={x} value={x}>{x}</option>)}</select></label>
   </div>}
   <div className="table-wrap"><table><thead><tr>{["CT/DA, hạng mục", "ĐVT", "Kế hoạch", "Trong kỳ", "Điều chỉnh", "Lũy kế năm", "% KH", "Nội dung"].map(h => <th key={h}>{h}</th>)}</tr></thead><tbody>{rows.map(r => <tr key={r.ref}><td>{r.roman} {r.name}{r.missing && " — Chưa gửi số liệu"}</td><td>{r.unit}</td><td>{r.planned ?? "–"}</td><td>{r.in_period}</td><td>{r.adjustment}</td><td>{r.year_to_date}</td><td>{r.percent ?? "–"}</td><td>{r.content}</td></tr>)}</tbody></table></div>
   <ExternalCells items={data.external}/>
   <details><summary>Số liệu nguồn và phiếu đã gửi</summary>{(data.metrics ?? []).map(m => <details key={m.key}><summary>{m.label}: {m.value.Cumulative ?? m.value.Sum ?? m.value.Min ?? "–"} {m.unit}</summary><p>{m.value.Closed ? "Đã chốt" : "Tạm tính"} · Lũy kế năm KHTC {m.value.YearToDate ?? "–"} · Chưa xác nhận {m.value.Unconfirmed ?? "–"}</p><p>Trong kỳ {m.value.Increment ?? m.value.Sum ?? "–"} · Gốc {m.value.Base ?? "–"} · Cách lấy gốc: {m.value.BaseRef || "–"}</p>{(m.value.PriceDays ?? []).map(day => <p key={day.Day}>{day.Day}: {day.Min} – {day.Max} {m.unit}</p>)}<ul>{(m.trace ?? []).map((t, i) => <li key={i}>{t.module} · {t.calendar} · {t.native_key} · {t.from?.slice(0, 10)} – {t.to?.slice(0, 10)} · {t.closed ? "Đã chốt" : "Chưa chốt"} · Tuần KHTC {t.target_key}</li>)}</ul></details>)}<p>Ảnh chụp nguồn v{data.sources?.version ?? "–"} · {dt(data.captured_at)}</p><ul>{(data.submissions ?? []).map(s => <li key={s.submission_id}>{DEPT_LABEL[s.department] ?? s.department} · lần gửi {s.attempt} · mã bảng {s.sheet_id}</li>)}</ul></details>
   {!d.final && d.editable && <div>
    {(data.template === "mau_5" || mode === "progress") && <label><input type="checkbox" checked={exportFiltered} onChange={e => setExportFiltered(e.target.checked)}/> Xuất theo bộ lọc đang chọn</label>}
    {!!data.provisional?.length && <label><input type="checkbox" checked={ack} onChange={e => setAck(e.target.checked)}/> Tôi xác nhận xuất bản Tạm tính</label>}
    <button className="btn" disabled={act.busy || dirty || !d.version || (!!data.provisional?.length && !ack)} onClick={() => act.run(async () => { const x = await api.export(id, d.version, ack, exportFiltered ? filter : {}); saveFile(await api.download(id, x.id)); history.reload(); }, "Đã xuất và lưu phiên bản Excel")}>Xuất Excel</button>
   </div>}
  </>}
  <h3>Lịch sử xuất</h3>
  <LoadState s={history}>{items => <ul>{items.map(x => <li key={x.id}>Phiên bản {x.version} · {dt(x.at)} · {x.provisional ? "Tạm tính" : d.final?.export_id === x.id ? "Bản cuối" : "Đã xuất"}{" "}
   <button className="btn" disabled={act.busy} onClick={() => act.run(async () => saveFile(await api.download(id, x.id)), "Đã tải tệp đã lưu")}>Tải Excel v{x.version}</button>{" "}
   {d.terminal !== "cancelled" && (d.editable || !!d.final) && mode !== "history" && <button className="btn" disabled={act.busy} title="Word dựng từ đúng nội dung của bản Excel này; ô Số văn bản trống thì hệ thống tự cấp số" onClick={() => act.run(async () => { const w = await api.word(id, x.id); saveFile(await api.downloadWord(id, w.id)); words.reload(); }, "Đã xuất và lưu Word")}>Xuất Word v{x.version}</button>}{" "}
   {!d.final && d.editable && <button className="btn" disabled={act.busy || dirty || x.provisional || d.source_changed || !!d.source_error || x.draft_id !== d.draft_id} onClick={() => {
                        if (window.confirm("Đánh dấu Bản cuối sẽ khóa báo cáo và cập nhật nguồn của kỳ. Tiếp tục?"))
                            void act.run(async () => { await api.final(id, x.id); reload(); history.reload(); }, "Đã đánh dấu Bản cuối");
                    }}>Đánh dấu Bản cuối</button>}
  </li>)}{!items.length && <li>Chưa có bản xuất.</li>}</ul>}</LoadState>
  <h3>Lịch sử Word</h3>
  <LoadState s={words}>{items => <ul>{items.map(w => <li key={w.id}>Lần {w.seq} · {dt(w.at)} · Số {w.doc_number || "—"}{w.doc_number_auto ? " (tự cấp)" : ""} · <span title={w.body_hash}>SHA-256 {w.body_hash.slice(0, 12)}…</span>{" "}
   <button className="btn" disabled={act.busy} onClick={() => act.run(async () => saveFile(await api.downloadWord(id, w.id)), "Đã tải tệp Word đã lưu")}>Tải Word</button>
  </li>)}{!items.length && <li>Chưa xuất Word.</li>}</ul>}</LoadState>
 </section>;
}
