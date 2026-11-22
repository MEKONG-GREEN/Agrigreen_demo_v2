"use client";
// W11.67 — Dữ liệu › Số liệu nguồn ngoài (BRD KHTC v2.2 FN-TI-02.11, Hình 20a; prototype KhuyenNong_KHTC.html "ext").
// Bố cục: nút Tải mẫu Excel / Nạp Excel tổng hợp / Thêm số liệu; dải thông tin; bộ lọc Năm, Kỳ, Cơ quan cung cấp, tìm
// chỉ tiêu; bảng Chỉ tiêu, Kỳ, Giá trị, ĐVT, Nguồn, Minh chứng, Dùng trong; khối Nạp Excel tổng hợp và Lịch sử.
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { PageHeader } from "@/components/patterns/list";
import { ApiProblem } from "@/api/problem";
import { LoadState, dt, useAction, useLoad } from "@/kn/cycle/common";
import { readUpload, saveBlob } from "@/lib/files";
import { createKhtcPeriodApi, type KhtcPeriod } from "../periods/api";
import { agencies, createExternalApi, fmtVn, parseVnNumber, sourceText, type ExtFigure, type ExtImport, type ExtIndicator, type ExternalApi } from "./api";

export function KhtcExternalScreen() {
    const { api } = useAuth();
    const ext = useMemo(() => (api ? createExternalApi(api) : null), [api]);
    const periodsApi = useMemo(() => (api ? createKhtcPeriodApi(api) : null), [api]);
    const [year, setYear] = useState(new Date().getFullYear());
    const [period, setPeriod] = useState("");
    const [agency, setAgency] = useState("");
    const [q, setQ] = useState("");
    const [form, setForm] = useState<{ edit?: ExtFigure } | null>(null);
    const [importing, setImporting] = useState(false);
    const periods = useLoad(periodsApi ? () => periodsApi.list(year) : null, [periodsApi, year]);
    const indicators = useLoad(ext ? () => ext.indicators() : null, [ext]);
    const all = useLoad(ext ? () => ext.figures(year) : null, [ext, year]);
    const history = useLoad(ext ? () => ext.history(year) : null, [ext, year]);
    const act = useAction();
    const reload = () => { all.reload(); history.reload(); indicators.reload(); };
    const open = (periods.data ?? []).filter(p => p.state !== "cancelled" && p.state !== "closed");
    const writable = (all.data ?? []).some(f => f.editable) || open.length > 0;
    const rows = (all.data ?? []).filter(f => (!period || f.period_id === period) && (!agency || f.agency === agency) && (!q || f.name.toLowerCase().includes(q.toLowerCase())));
    return <>
  <PageHeader title="Số liệu nguồn ngoài" actions={<>
    <button className="btn" disabled={!ext || act.busy} onClick={() => ext && act.run(async () => { const f = await ext.template(year); saveBlob(f.blob, f.name); }, "Đã tải mẫu Excel")}>Tải mẫu Excel</button>{" "}
    <button className="btn" disabled={!writable} onClick={() => setImporting(true)}>Nạp Excel tổng hợp</button>{" "}
    <button className="btn btn-p" disabled={!writable} onClick={() => setForm({})}>Thêm số liệu</button>
  </>}>Dữ liệu › Số liệu nguồn ngoài</PageHeader>
  <div className="alert alert-info" role="note">Số liệu không có trong hệ thống (Sở, Chi cục, Cục Thống kê, đơn vị khác) nhập tại đây, bắt buộc ghi nguồn. Mẫu báo cáo dùng các chỉ tiêu này như ô “Nhập ngoài” (ô màu tím, chỉ sửa tại màn này đến khi kỳ đóng).</div>
  {act.view}
  <section className="card sec toolbar" aria-label="Bộ lọc">
   <label>Năm <input type="number" min={2000} max={2100} value={year} onChange={e => { setYear(Number(e.target.value)); setPeriod(""); }}/></label>{" "}
   <label>Kỳ <select value={period} onChange={e => setPeriod(e.target.value)}><option value="">Tất cả</option>{(periods.data ?? []).filter(p => p.state !== "cancelled").map(p => <option key={p.id} value={p.id}>{shortName(p.name)}</option>)}</select></label>{" "}
   <label>Cơ quan cung cấp <select value={agency} onChange={e => setAgency(e.target.value)}><option value="">Tất cả</option>{agencies(all.data ?? []).map(a => <option key={a} value={a}>{a}</option>)}</select></label>{" "}
   <label>Tìm chỉ tiêu <input type="search" value={q} placeholder="Chỉ tiêu" onChange={e => setQ(e.target.value)}/></label>
  </section>
  <section className="card sec">
   <LoadState s={all}>{() => rows.length ? <div className="table-wrap"><table>
    <thead><tr>{["Chỉ tiêu", "Kỳ", "Giá trị", "ĐVT", "Nguồn (cơ quan, văn bản)", "Minh chứng", "Dùng trong", ""].map(x => <th key={x}>{x}</th>)}</tr></thead>
    <tbody>{rows.map(f => <tr key={f.entry_id}>
     <td>{f.name}{f.group && <small className="muted"> · {f.group}</small>}</td>
     <td>{f.period_name}{f.closed && <small> (đã đóng)</small>}</td>
     <td className="num c-violet" title="Nhập ngoài"><b>{fmtVn(f.value)}</b></td>
     <td>{f.unit}</td>
     <td>{sourceText(f)}</td>
     <td>{f.evidence.length ? f.evidence.map(ev => <button key={ev.id} className="btn btn-sm btn-o" onClick={() => ext && act.run(async () => { const x = await ext.evidence(ev.id, ev.file_name); saveBlob(x.blob, x.name); })}>{ev.file_name}</button>) : "—"}</td>
     <td>{f.used_in}</td>
     <td>{f.editable && <button className="btn" aria-label={`Sửa ${f.name}`} onClick={() => setForm({ edit: f })}>Sửa</button>}</td>
    </tr>)}</tbody></table></div> : <p className="empty">Không có số liệu phù hợp</p>}</LoadState>
  </section>
  <div className="grid2">
   <section className="card sec"><h2>Nạp Excel tổng hợp</h2><p>Tải mẫu → điền cột Chỉ tiêu, Kỳ, Giá trị, Nguồn (cơ quan, văn bản hoặc kênh, ngày) → nạp lại. Hệ thống kiểm từng dòng trước khi ghi: dòng thiếu nguồn hoặc chỉ tiêu không có trong danh mục bị báo lỗi, không ghi nhận; dòng trùng chỉ tiêu và kỳ hỏi ghi đè.</p></section>
   <section className="card sec"><h2>Lịch sử</h2><LoadState s={history}>{items => items.length ? <ul>{items.slice(0, 20).map(h => <li key={h.entry_id}>
    <b>{h.action === "withdraw" ? `Rút “${h.indicator_name}” ${fmtVn(h.old_value)}` : h.old_value !== null ? `Sửa “${h.indicator_name}” ${fmtVn(h.old_value)} → ${fmtVn(h.new_value)}` : `Nhập “${h.indicator_name}” ${fmtVn(h.new_value)}`}</b> {h.unit}
    <div><small className="muted">{h.actor_name || "Cán bộ KHTC"} · {dt(h.at)} · {h.period_name}{h.reason ? ` · lý do: ${h.reason}` : ""}{h.import_id ? " · nạp Excel" : ""}</small></div>
   </li>)}</ul> : <p>Chưa có thay đổi.</p>}</LoadState></section>
  </div>
  {form && ext && <FigureForm api={ext} edit={form.edit} periods={open} indicators={indicators.data ?? []} onClose={() => setForm(null)} onSaved={() => { setForm(null); reload(); act.setNote("Đã lưu số liệu. Bản báo cáo đã xuất dùng chỉ tiêu này sẽ chuyển “Nguồn đã thay đổi”."); }} onIndicator={indicators.reload}/>}
  {importing && ext && <ImportDialog api={ext} year={year} onClose={() => setImporting(false)} onDone={(n, e) => { setImporting(false); reload(); act.setNote(`Đã nạp ${n} chỉ tiêu${e ? `, báo lỗi ${e} dòng` : ""}`); }}/>}
 </>;
}

export function shortName(name: string): string {
    return name.replace(/\s*\(.*\)\s*$/, "").trim();
}

function FigureForm({ api, edit, periods, indicators, onClose, onSaved, onIndicator }: {
    api: ExternalApi;
    edit?: ExtFigure;
    periods: KhtcPeriod[];
    indicators: ExtIndicator[];
    onClose: () => void;
    onSaved: () => void;
    onIndicator: () => void;
}) {
    const [indicator, setIndicator] = useState(edit?.indicator_id ?? "");
    const [newName, setNewName] = useState("");
    const [newUnit, setNewUnit] = useState("");
    const [periodId, setPeriodId] = useState(edit?.period_id ?? periods[0]?.id ?? "");
    const [value, setValue] = useState(edit ? fmtVn(edit.value) : "");
    const [unit, setUnit] = useState(edit?.unit ?? "");
    const [agency, setAgency] = useState(edit?.agency ?? "");
    const [document, setDocument] = useState(edit?.document ?? "");
    const [date, setDate] = useState(edit?.source_date ?? "");
    const [evidence, setEvidence] = useState<{ id: string; file_name: string }[]>(edit?.evidence ?? []);
    const [reason, setReason] = useState("");
    const [confirm, setConfirm] = useState<string | null>(null);
    const [error, setError] = useState("");
    const act = useAction();
    const save = (overwrite: boolean) => act.run(async () => {
        setError("");
        let indicatorId = indicator;
        if (indicator === "__new") {
            const created = await api.addIndicator({ name: newName, unit: newUnit });
            indicatorId = created.id;
            setIndicator(created.id);
            onIndicator();
        }
        const v = parseVnNumber(value);
        const err = !indicatorId ? "Chọn chỉ tiêu." : v === null ? "Nhập giá trị số." : !agency.trim() || !document.trim() || !date ? "Thiếu nguồn (cơ quan, văn bản hoặc kênh, ngày) → không lưu." : (edit || overwrite) && reason.trim().length < 10 ? "Sửa giá trị bắt buộc nhập lý do (tối thiểu 10 ký tự)." : "";
        if (err) {
            setError(err);
            return;
        }
        try {
            await api.save({ period_id: periodId, indicator_id: indicatorId, value: v!, unit, agency, document, source_date: date, evidence_ids: evidence.map(e => e.id), ...(edit ? { base_version: edit.version, reason } : overwrite ? { overwrite: true, reason } : {}) });
            onSaved();
        }
        catch (e) {
            if (e instanceof ApiProblem && e.code === "extension.khtc_ext_duplicate") {
                setConfirm(e.problem.detail ?? "Chỉ tiêu đã có giá trị trong kỳ.");
                return;
            }
            throw e;
        }
    });
    const upload = async (files: FileList | null) => {
        for (const file of Array.from(files ?? [])) {
            const up = await readUpload(file);
            const body = typeof up.body === "string" ? new TextEncoder().encode(up.body) : up.body;
            const r = await act.run(() => api.upload({ name: file.name, body }));
            if (r)
                setEvidence(list => [...list, { id: r.id, file_name: r.file_name }]);
        }
    };
    return <Dialog title={edit ? "Sửa số liệu" : "Thêm số liệu nguồn ngoài"} onClose={onClose} actions={confirm ? <button className="btn btn-p" disabled={act.busy} onClick={() => void save(true)}>Ghi đè</button> : <button className="btn btn-p" disabled={act.busy} onClick={() => void save(false)}>Lưu</button>}>
  <div className="grid2">
   <label style={{ gridColumn: "span 2" }}>Chỉ tiêu *<select value={indicator} disabled={!!edit} onChange={e => setIndicator(e.target.value)}><option value="">— Chọn —</option>{indicators.map(i => <option key={i.id} value={i.id}>{i.name}{i.unit ? ` (${i.unit})` : ""}</option>)}<option value="__new">+ Thêm chỉ tiêu mới…</option></select></label>
   {indicator === "__new" && <><label>Tên chỉ tiêu mới *<input value={newName} onChange={e => setNewName(e.target.value)}/></label><label>ĐVT<input value={newUnit} onChange={e => setNewUnit(e.target.value)}/></label></>}
   <label>Kỳ *<select value={periodId} disabled={!!edit} onChange={e => setPeriodId(e.target.value)}>{edit && <option value={edit.period_id}>{edit.period_name}</option>}{!edit && periods.map(p => <option key={p.id} value={p.id}>{shortName(p.name)}</option>)}</select></label>
   <label>Giá trị *<input inputMode="decimal" value={value} onChange={e => setValue(e.target.value)}/></label>
   <label>ĐVT<input value={unit} placeholder={indicators.find(i => i.id === indicator)?.unit ?? ""} onChange={e => setUnit(e.target.value)}/></label>
   <label>Cơ quan cung cấp *<input value={agency} onChange={e => setAgency(e.target.value)}/></label>
   <label>Văn bản / kênh *<input value={document} onChange={e => setDocument(e.target.value)}/></label>
   <label>Ngày của nguồn *<input type="date" value={date} onChange={e => setDate(e.target.value)}/></label>
   <label>Minh chứng<input type="file" multiple accept=".pdf,.png,.jpg,.jpeg,.doc,.docx,.xls,.xlsx,.eml,.txt" onChange={e => void upload(e.target.files)}/></label>
   {evidence.length > 0 && <ul style={{ gridColumn: "span 2" }}>{evidence.map(ev => <li key={ev.id}>{ev.file_name} <button className="btn btn-sm btn-o" onClick={() => setEvidence(list => list.filter(x => x.id !== ev.id))}>Bỏ</button></li>)}</ul>}
   {(edit || confirm) && <label style={{ gridColumn: "span 2" }}>Lý do sửa *<input value={reason} onChange={e => setReason(e.target.value)}/></label>}
  </div>
  {confirm && <div className="alert alert-warn" role="alert">{confirm} Ghi đè sẽ lưu lịch sử giá trị cũ → mới.</div>}
  {error && <div className="alert alert-bad" role="alert">{error}</div>}
  {act.view}

 </Dialog>;
}

function ImportDialog({ api, year, onClose, onDone }: { api: ExternalApi; year: number; onClose: () => void; onDone: (recorded: number, errors: number) => void }) {
    const [staged, setStaged] = useState<ExtImport | null>(null);
    const [overwrite, setOverwrite] = useState(false);
    const [reason, setReason] = useState("");
    const act = useAction();
    const pick = async (files: FileList | null) => {
        const f = files?.[0];
        if (!f)
            return;
        const up = await readUpload(f);
        const r = await act.run(() => api.stage(year, up));
        if (r)
            setStaged(r);
    };
    const res = staged?.result;
    return <Dialog title="Nạp Excel tổng hợp" onClose={onClose} actions={res && staged ? <button className="btn btn-p" disabled={act.busy || res.valid + (overwrite ? res.overwrites : 0) === 0 || (overwrite && reason.trim().length < 10)} onClick={() => void act.run(async () => { const done = await api.commit(staged.import_id, overwrite, reason); onDone(done.recorded, done.result.errors); })}>Hoàn tất</button> : null}>
  {!staged && <label>Chọn file Excel/CSV<input type="file" accept=".xlsx,.csv" onChange={e => void pick(e.target.files)}/></label>}
  {act.view}
  {res && <>
   <p>Bước 1/2 — kiểm tra tệp <b>{staged.file_name}</b>. Chưa có số liệu nào được ghi.</p>
   <div className="alert alert-info" role="status"><b>Ghi nhận {res.valid} dòng hợp lệ</b>{res.overwrites ? ` · ${res.overwrites} dòng trùng chỉ tiêu và kỳ đã có giá trị` : ""}</div>
   {res.errors > 0 && <div className="alert alert-bad" role="alert"><b>{res.errors} dòng lỗi, không ghi nhận</b><ul>{res.rows.filter(r => r.status === "error").map(r => <li key={r.line}>Dòng {r.line} “{r.indicator}”: {r.errors.join("; ")}.</li>)}</ul></div>}
   {res.overwrites > 0 && <div className="alert alert-warn"><ul>{res.rows.filter(r => r.status === "overwrite").map(r => <li key={r.line}>Dòng {r.line} “{r.indicator_name}” {r.period_name}: {fmtVn(r.current_value)} → {fmtVn(r.normalized_value)}</li>)}</ul>
    <label><input type="checkbox" checked={overwrite} onChange={e => setOverwrite(e.target.checked)}/> Ghi đè các dòng trùng (lưu lịch sử)</label>
    {overwrite && <label>Lý do ghi đè *<input value={reason} onChange={e => setReason(e.target.value)}/></label>}
   </div>}
  </>}
 </Dialog>;
}
