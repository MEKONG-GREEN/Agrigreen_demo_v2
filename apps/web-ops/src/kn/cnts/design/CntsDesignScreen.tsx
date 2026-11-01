"use client";
import { useMemo, useState } from "react";
import Link from "next/link";
import { useAuth } from "@/auth/AuthProvider";
import { DataTable, PageHeader, StatusPill } from "@/components/patterns/list";
import { SelectField } from "@/components/form";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import { createDesignApi, DESIGN_HEADERS, designFileError, type DesignField, type DesignImport } from "./api";

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
export function CntsDesignScreen() {
  const { api } = useAuth(); const capi = useMemo(() => api ? createDesignApi(api) : null, [api]);
  const list = useLoad(capi ? () => capi.list() : null, [capi]); const [code, setCode] = useState("");
  const [preview, setPreview] = useState(false); const [file, setFile] = useState<File | null>(null); const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<DesignImport | null>(null); const act = useAction();
  const selected = list.data?.find(x => x.code === code);
  const sample = () => saveBlob(new Blob([DESIGN_HEADERS.join(",") + "\nso_con,Số con,integer,true,con,Tổng đàn,true,\n"], { type: "text/csv;charset=utf-8" }), "cau-truc-bieu-cnts.csv");
  return <div className="page"><PageHeader title="Thiết kế biểu mẫu"><p>Xem cấu trúc và kiểm tệp trước khi lập phiên bản biểu.</p></PageHeader>
    <p className="mute">Hiện hỗ trợ xem năm biểu kỳ BM-01, BM-04, BM-17 và hai phần BM-05. Sửa, lưu phiên bản, sao chép, mẫu nhật ký và biểu điều tra đang được bổ sung. Kiểm tệp dưới đây chỉ xem trước, không thay đổi biểu đang dùng.</p>
    <LoadState s={list}>{items => <section className="card sec"><SelectField label="Biểu kỳ" value={code} onChange={v => { setCode(v); setPreview(false); }} placeholder="— Chọn biểu —" options={items.map(x => ({ value: x.code, label: `${x.bm} · ${x.title}` }))} />
      {selected ? <><h2>{selected.title}</h2><p><StatusPill tone={selected.version ? "info" : "warn"}>{selected.version ? `Phiên bản ${selected.version}` : "Chưa cài — cấu trúc chuẩn tham khảo"}</StatusPill> · {selected.period_type === "day" ? "Hằng ngày" : "Hằng tuần"} · {selected.due}</p><DesignStructure fields={selected.fields} /><div className="row"><button className="btn" onClick={() => setPreview(x => !x)}>{preview ? "Đóng xem trước" : "Xem trước phiên bản"}</button><Link className="btn" href="/kn/cnts/bieu-mau">Phát hành, theo dõi nộp</Link></div>{preview ? <Preview fields={selected.fields} /> : null}</> : null}</section>}</LoadState>
    <section className="card sec" aria-label="Kiểm tệp cấu trúc"><h2>Kiểm tệp cấu trúc</h2><p>Tối đa 500 chỉ tiêu, 2 MB; Excel .xlsx hoặc CSV UTF-8. Cột bắt buộc theo đúng thứ tự: {DESIGN_HEADERS.join(", ")}. required/non_negative: true hoặc false; options cách nhau bằng |.</p><button className="btn" onClick={sample}>Tải mẫu cấu trúc CSV</button><div className="field"><label>Tệp cấu trúc<input type="file" accept=".xlsx,.csv" onChange={e => { const f = e.target.files?.[0] ?? null; setFile(f); setError(f ? designFileError(f) : null); setResult(null); }} /></label>{error ? <span role="alert" className="err">{error}</span> : null}</div>
      <button className="btn btn-primary" disabled={!file || !!error || act.busy} onClick={async () => { if (!file || !capi) return; setResult(null); const r = await act.run(async () => capi.preview(new Uint8Array(await file.arrayBuffer()))); if (r) setResult(r); }}>Kiểm tra và xem trước tệp</button>{act.view}
      {result ? <><p role="status">{result.valid ? `Cấu trúc hợp lệ: ${result.fields.length} chỉ tiêu. Chưa lưu phiên bản.` : `Tệp có ${result.issues.length} lỗi. Không có thay đổi nào được lưu.`}</p>{result.valid ? <><DesignStructure fields={result.fields} /><Preview fields={result.fields} /></> : <ul>{result.issues.map((x, i) => <li key={i}>Dòng {x.line} · {x.field}: {x.message}</li>)}</ul>}</> : null}</section></div>;
}
