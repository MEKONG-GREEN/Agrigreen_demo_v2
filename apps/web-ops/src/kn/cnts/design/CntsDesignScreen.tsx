"use client";
import { useMemo, useState } from "react";
import Link from "next/link";
import { useAuth } from "@/auth/AuthProvider";
import { DataTable, PageHeader, StatusPill } from "@/components/patterns/list";
import { SelectField, TextField } from "@/components/form";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import { createDesignApi, DESIGN_HEADERS, designFieldErrors, designFileError, FIELD_TYPES, type DesignField, type DesignImport, type DesignView } from "./api";

type DesignApi = ReturnType<typeof createDesignApi>;
const TYPE_LABEL: Record<DesignField["type"], string> = { integer: "Số nguyên", decimal: "Số thập phân", text: "Văn bản", date: "Ngày", choice: "Lựa chọn" };

export function DesignStructure({ fields }: { fields: DesignField[] }) {
  return <DataTable caption="Cấu trúc chỉ tiêu" rows={fields} rowKey={(f) => f.key} columns={[
    { key: "key", header: "Mã", cell: f => f.key }, { key: "label", header: "Chỉ tiêu", cell: f => f.label },
    { key: "type", header: "Kiểu", cell: f => f.type }, { key: "unit", header: "Đơn vị", cell: f => f.unit ?? "—" },
    { key: "group", header: "Nhóm", cell: f => f.group ?? "—" }, { key: "required", header: "Bắt buộc", cell: f => f.required ? "Có" : "Không" },
    { key: "options", header: "Lựa chọn / điều kiện", cell: f => f.options?.join(" · ") || (f.non_negative ? "Không âm" : "—") },
  ]} />;
}
function Preview({ fields }: { fields: DesignField[] }) {
  return <section className="card sec" aria-label="Xem trước biểu"><h3>Xem trước biểu</h3><p className="mute">Bố cục theo cấu trúc đã chọn; các ô chỉ minh họa, không nhập hoặc nộp số liệu tại đây.</p><div className="form-grid">{fields.map(f => <div className="field" key={f.key}><label>{f.label}{f.required ? <span className="req"> *</span> : null}</label><div className="input" aria-label={`Xem trước ${f.label}`}>{f.type === "choice" ? f.options?.join(" / ") : f.type === "date" ? "Ngày / tháng / năm" : f.type === "integer" ? "Số nguyên" : f.type === "decimal" ? "Số thập phân" : "Nội dung"}{f.unit ? ` (${f.unit})` : ""}</div></div>)}</div></section>;
}

/** #192 FN-07 B1: sửa cấu trúc — chỉ thêm chỉ tiêu mới; chỉ tiêu đã phát hành sửa được tên, đơn vị, nhóm, bắt buộc, lựa chọn. */
function Editor({ capi, design, initial, onSaved, onCancel }: { capi: DesignApi; design: DesignView; initial: DesignField[]; onSaved: () => void; onCancel: () => void }) {
  const [title, setTitle] = useState(design.title); const [fields, setFields] = useState<DesignField[]>(() => initial.map(f => ({ ...f })));
  const [errors, setErrors] = useState<string[]>([]); const act = useAction();
  const published = new Set(design.fields.map(f => f.key));
  const set = (i: number, p: Partial<DesignField>) => setFields(xs => xs.map((x, j) => j === i ? { ...x, ...p } : x));
  const save = async () => {
    const e = designFieldErrors(fields, design.version ? design.fields : []); if (!title.trim()) e.unshift("Tên biểu bắt buộc"); setErrors(e); if (e.length) return;
    if (await act.run(() => capi.save(design.code, design.version ?? 0, title.trim(), fields), r => `Đã lưu phiên bản ${r.version.version}. Kỳ đã phát hành giữ phiên bản cũ.`)) onSaved();
  };
  return <section className="card sec" aria-label="Sửa cấu trúc biểu"><h3>Sửa cấu trúc — lưu thành phiên bản {(design.version ?? 0) + 1}</h3>
    <p className="mute">Kỳ đã phát hành và bài đã nộp giữ phiên bản cũ; kỳ mới dùng đúng phiên bản được chọn khi phát hành. Chỉ tiêu đã phát hành không xóa, không đổi kiểu.</p>
    <TextField label="Tên biểu" value={title} onChange={setTitle} required />
    <div className="table-wrap"><table className="table"><caption>Chỉ tiêu</caption><thead><tr><th>Mã</th><th>Chỉ tiêu</th><th>Kiểu</th><th>Đơn vị</th><th>Nhóm</th><th>Bắt buộc</th><th>Không âm</th><th>Lựa chọn (cách nhau |)</th><th /></tr></thead><tbody>
      {fields.map((f, i) => { const old = published.has(f.key) && i < initial.length; return <tr key={i}>
        <td><input className="input" aria-label={`Mã chỉ tiêu ${i + 1}`} value={f.key} disabled={old} onChange={e => set(i, { key: e.target.value })} /></td>
        <td><input className="input" aria-label={`Tên chỉ tiêu ${i + 1}`} value={f.label} onChange={e => set(i, { label: e.target.value })} /></td>
        <td><select className="select" aria-label={`Kiểu chỉ tiêu ${i + 1}`} value={f.type} disabled={old} onChange={e => set(i, { type: e.target.value as DesignField["type"] })}>{FIELD_TYPES.map(t => <option key={t} value={t}>{TYPE_LABEL[t]}</option>)}</select></td>
        <td><input className="input" aria-label={`Đơn vị chỉ tiêu ${i + 1}`} value={f.unit ?? ""} onChange={e => set(i, { unit: e.target.value || undefined })} /></td>
        <td><input className="input" aria-label={`Nhóm chỉ tiêu ${i + 1}`} value={f.group ?? ""} onChange={e => set(i, { group: e.target.value || undefined })} /></td>
        <td><input type="checkbox" aria-label={`Bắt buộc ${i + 1}`} checked={f.required} onChange={e => set(i, { required: e.target.checked })} /></td>
        <td><input type="checkbox" aria-label={`Không âm ${i + 1}`} checked={!!f.non_negative} onChange={e => set(i, { non_negative: e.target.checked || undefined })} /></td>
        <td><input className="input" aria-label={`Lựa chọn chỉ tiêu ${i + 1}`} value={f.options?.join("|") ?? ""} onChange={e => set(i, { options: e.target.value ? e.target.value.split("|").map(x => x.trim()) : undefined })} /></td>
        <td>{old ? null : <button className="btn" onClick={() => setFields(xs => xs.filter((_, j) => j !== i))}>Bỏ</button>}</td>
      </tr>; })}
    </tbody></table></div>
    <div className="row"><button className="btn" onClick={() => setFields(xs => [...xs, { key: "", label: "", type: "integer", required: false }])}>+ Thêm chỉ tiêu</button>
      <button className="btn btn-primary" disabled={act.busy} onClick={save}>Lưu phiên bản mới</button><button className="btn" onClick={onCancel}>Hủy</button></div>
    {errors.length ? <ul role="alert">{errors.map((x, i) => <li key={i}>{x}</li>)}</ul> : null}{act.view}
  </section>;
}

/** #192 FN-07 luồng phụ: sao chép biểu / tạo biểu điều tra từ tệp — mã CNTS-DT-<hậu tố>, phiên bản 1. */
function CreateSurvey({ capi, source, fields, onSaved }: { capi: DesignApi; source?: DesignView; fields?: DesignField[]; onSaved: () => void }) {
  const [suffix, setSuffix] = useState(""); const [title, setTitle] = useState(source ? `Bản sao ${source.title}` : ""); const act = useAction();
  const bad = !/^[A-Za-z0-9][A-Za-z0-9-]{0,39}$/.test(suffix) || !title.trim();
  const go = async () => {
    const body = source ? { code_suffix: suffix, title: title.trim(), source_code: source.code, source_version: source.version ?? 0 } : { code_suffix: suffix, title: title.trim(), fields };
    if (await act.run(() => capi.create(body), r => `Đã tạo ${r.code} phiên bản ${r.version.version}.`)) onSaved();
  };
  return <section className="card sec" aria-label={source ? "Sao chép biểu" : "Tạo biểu điều tra từ tệp"}><h3>{source ? `Sao chép ${source.bm} thành biểu điều tra` : "Tạo biểu điều tra từ tệp đã kiểm"}</h3>
    <TextField label="Mã biểu điều tra (CNTS-DT-…)" value={suffix} onChange={v => setSuffix(v.toUpperCase())} hint="Chữ in hoa, số, gạch ngang; tối đa 40 ký tự" required />
    <TextField label="Tên biểu điều tra" value={title} onChange={setTitle} required />
    <button className="btn btn-primary" disabled={bad || act.busy} onClick={go}>{source ? "Sao chép" : "Tạo biểu điều tra"}</button>{act.view}</section>;
}

export function CntsDesignScreen() {
  const { api } = useAuth(); const capi = useMemo(() => api ? createDesignApi(api) : null, [api]);
  const list = useLoad(capi ? () => capi.list() : null, [capi]); const [code, setCode] = useState("");
  const [preview, setPreview] = useState(false); const [file, setFile] = useState<File | null>(null); const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<DesignImport | null>(null); const act = useAction();
  const [mode, setMode] = useState<"view" | "edit" | "copy" | "excel-edit" | "excel-new">("view"); const [shown, setShown] = useState<number | null>(null);
  const selected = list.data?.find(x => x.code === code);
  const history = useLoad(capi && selected?.version ? () => capi.history(selected.code) : null, [capi, selected?.code, selected?.version]);
  const designer = !!list.data?.some(x => x.can_edit);
  const reload = () => { setMode("view"); setShown(null); setResult(null); list.reload(); };
  const pick = (v: string) => { setCode(v); setPreview(false); setMode("view"); setShown(null); };
  const old = shown !== null ? history.data?.find(v => v.version === shown) : undefined;
  const sample = () => saveBlob(new Blob([DESIGN_HEADERS.join(",") + "\nso_con,Số con,integer,true,con,Tổng đàn,true,\n"], { type: "text/csv;charset=utf-8" }), "cau-truc-bieu-cnts.csv");
  return <div className="page"><PageHeader title="Thiết kế biểu mẫu"><p>Chọn hoặc tạo biểu, sửa cấu trúc, xem trước và lưu phiên bản (FN-07).</p></PageHeader>
    <p className="mute">Mẫu cố định theo TTKN Quốc gia (BM-01, BM-04, BM-05) chỉ xem và sao chép. BM-17 và biểu điều tra sửa được; mỗi lần lưu tạo phiên bản mới, kỳ đã nộp giữ phiên bản cũ. Biểu điều tra được chọn kèm phiên bản khi tạo đợt ở Chiến dịch điều tra.</p>
    <LoadState s={list}>{items => <section className="card sec"><SelectField label="Biểu kỳ" value={code} onChange={pick} placeholder="— Chọn biểu —" options={items.map(x => ({ value: x.code, label: `${x.bm} · ${x.title}${x.kind === "survey" ? " (điều tra)" : ""}${x.locked ? " · KNQG" : ""}` }))} />
      {selected ? <><h2>{selected.title}</h2><p><StatusPill tone={selected.version ? "info" : "warn"}>{selected.version ? `Phiên bản ${selected.version}` : "Chưa cài — cấu trúc chuẩn tham khảo"}</StatusPill>{selected.locked ? <> <StatusPill tone="neutral">Mẫu KNQG — không sửa cấu trúc</StatusPill></> : null} · {selected.kind === "survey" ? "Biểu điều tra" : selected.period_type === "day" ? "Hằng ngày" : "Hằng tuần"} · {selected.due}</p>
        <DesignStructure fields={selected.fields} /><div className="row"><button className="btn" onClick={() => setPreview(x => !x)}>{preview ? "Đóng xem trước" : "Xem trước phiên bản"}</button>
          {selected.can_edit ? <button className="btn btn-primary" onClick={() => setMode("edit")}>Sửa cấu trúc</button> : null}
          {designer && selected.version ? <button className="btn" onClick={() => setMode("copy")}>Sao chép thành biểu điều tra</button> : null}
          <Link className="btn" href="/kn/cnts/bieu-mau">Phát hành, theo dõi nộp</Link><Link className="btn" href="/kn/cnts/chien-dich">Chiến dịch điều tra</Link></div>
        {preview ? <Preview fields={selected.fields} /> : null}
        {mode === "edit" && capi ? <Editor capi={capi} design={selected} initial={selected.fields} onSaved={reload} onCancel={() => setMode("view")} /> : null}
        {mode === "copy" && capi ? <CreateSurvey capi={capi} source={selected} onSaved={reload} /> : null}
        {history.data?.length ? <section className="card sec" aria-label="Lịch sử phiên bản"><h3>Lịch sử phiên bản</h3><ul>{history.data.map(v => <li key={v.version}>Phiên bản {v.version} · {v.title} · {v.fields.length} chỉ tiêu <button className="btn" onClick={() => setShown(shown === v.version ? null : v.version)}>{shown === v.version ? "Đóng" : "Xem"}</button></li>)}</ul>
          {old ? <><h4>Cấu trúc phiên bản {old.version}</h4><DesignStructure fields={old.fields} /></> : null}</section> : null}
      </> : null}</section>}</LoadState>
    <section className="card sec" aria-label="Kiểm tệp cấu trúc"><h2>Nạp cấu trúc từ tệp</h2><p>Tối đa 500 chỉ tiêu, 2 MB; Excel .xlsx hoặc CSV UTF-8. Cột bắt buộc theo đúng thứ tự: {DESIGN_HEADERS.join(", ")}. required/non_negative: true hoặc false; options cách nhau bằng |. Kiểm tệp không thay đổi biểu; chỉ lưu khi bấm lưu sau khi tệp hợp lệ.</p><button className="btn" onClick={sample}>Tải mẫu cấu trúc CSV</button><div className="field"><label>Tệp cấu trúc<input type="file" accept=".xlsx,.csv" onChange={e => { const f = e.target.files?.[0] ?? null; setFile(f); setError(f ? designFileError(f) : null); setResult(null); setMode("view"); }} /></label>{error ? <span role="alert" className="err">{error}</span> : null}</div>
      <button className="btn btn-primary" disabled={!file || !!error || act.busy} onClick={async () => { if (!file || !capi) return; setResult(null); const r = await act.run(async () => capi.preview(new Uint8Array(await file.arrayBuffer()))); if (r) setResult(r); }}>Kiểm tra và xem trước tệp</button>{act.view}
      {result ? <><p role="status">{result.valid ? `Cấu trúc hợp lệ: ${result.fields.length} chỉ tiêu. Chưa lưu phiên bản.` : `Tệp có ${result.issues.length} lỗi. Không có thay đổi nào được lưu.`}</p>{result.valid ? <><DesignStructure fields={result.fields} /><Preview fields={result.fields} />
        <div className="row">{selected?.can_edit ? <button className="btn" onClick={() => setMode("excel-edit")}>Dùng tệp cho {selected.bm}</button> : null}{designer ? <button className="btn" onClick={() => setMode("excel-new")}>Tạo biểu điều tra từ tệp</button> : null}</div>
        {mode === "excel-edit" && selected && capi ? <Editor capi={capi} design={selected} initial={result.fields} onSaved={reload} onCancel={() => setMode("view")} /> : null}
        {mode === "excel-new" && capi ? <CreateSurvey capi={capi} fields={result.fields} onSaved={reload} /> : null}</> : <ul>{result.issues.map((x, i) => <li key={i}>Dòng {x.line} · {x.field}: {x.message}</li>)}</ul>}</> : null}</section></div>;
}
