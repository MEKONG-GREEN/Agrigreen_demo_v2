"use client";

// FN-09 HCTH v4.0: current profiles, regular promotion and three processing states.
import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { NumberField, SelectField, TextField, parseNumberInput, validateForm } from "@/components/form";
import { DetailPage, LabelValueBlock, LogPanel } from "@/components/patterns/detail";
import { DataTable, FilterBar, FilterChip, PageHeader, SearchBox, StatusPill, type Column } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import { createPromotionApi, FORM_LABEL, fromTo, STATUS_LABEL, vnNum, type PromoCase, type PromoStatus, type PromotionApi } from "./promotion-api";
import { usePromotionSelection } from "./promotion-selection";
import { createStaffApi, vnDate } from "./staff-api";

export const PROMO_BASE = "/kn/nhan-su/nang-bac";
const vnToday = () => new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
const yearRule = { integer: true, positive: true, max: 9999, maxInputLength: 4 };
const monthsRule = { max: 12, maxDecimals: 1, maxInputLength: 4 };
const reservedRule = { max: 100, maxDecimals: 2, maxInputLength: 6 };
const caseHref = (key: string) => `${PROMO_BASE}/${encodeURIComponent(key)}`;

function usePromotionApi(): PromotionApi {
  const { api } = useAuth();
  return useMemo(() => createPromotionApi(api), [api]);
}

export function PromoStatusPill({ s }: { s: PromoStatus }) {
  return <StatusPill tone={s === "decided" ? "ok" : s === "exported" ? "info" : "neutral"}>{STATUS_LABEL[s]}</StatusPill>;
}

export function HrmPromotionScreen() {
  const selection = usePromotionSelection();
  const pa = usePromotionApi();
  const { kn } = useAuth();
  const [nonce, setNonce] = useState(0);
  const data = useLoad(() => pa.list(), [nonce]);
  const [year, setYear] = useState("");
  const [month, setMonth] = useState("");
  const [status, setStatus] = useState("");
  const [unit, setUnit] = useState("");
  const [q, setQ] = useState("");
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [filesOpen, setFilesOpen] = useState(false);
  const y = parseNumberInput(year, yearRule);
  const canExport = !kn.context?.read_only;
  return <>
    <PageHeader title="Danh sách nâng bậc" actions={<div className="row">
      <button type="button" className="btn btn-o btn-sm" onClick={() => { setSelected({}); setNonce(n => n + 1); }}>Tải lại hồ sơ hiện hành</button>
      <button type="button" className="btn btn-o btn-sm" onClick={() => setFilesOpen(v => !v)}>File đã xuất</button>
    </div>}>Nâng lương thường xuyên</PageHeader>
    <LoadState s={data}>{l => {
      const rows = l.items.filter(r => y.ok && (!y.value || Number(r.due.slice(0, 4)) === Number(y.value)) && (!month || r.due.slice(5, 7) === month)
        && (!status || r.status === status) && (!unit || r.unit === unit) && (!q || r.name.toLowerCase().includes(q.toLowerCase())));
      const picked = l.items.filter(r => selected[r.key] && r.status !== "decided");
      const columns: Column<PromoCase>[] = [
        { key: "pick", header: "Chọn", cell: r => <input type="checkbox" aria-label={`Chọn ${r.name || r.code}`} disabled={!canExport || r.status === "decided"} checked={!!selected[r.key]} onChange={e => setSelected(p => ({ ...p, [r.key]: e.target.checked }))} /> },
        { key: "name", header: "Họ tên", cell: r => <Link href={caseHref(r.key)}>{r.name || r.code}</Link> },
        { key: "unit", header: "Đơn vị", cell: r => r.unit_name || r.unit },
        { key: "step", header: "Bậc hiện tại → sau nâng", cell: r => { const [a, b] = fromTo(r); return `${a} → ${b}`; } },
        { key: "months", header: "Thời gian xét (tháng)", cell: r => r.promotion_months || "—" },
        { key: "due", header: "Ngày đến kỳ", cell: r => vnDate(r.due) },
        { key: "status", header: "Trạng thái xử lý", cell: r => <PromoStatusPill s={r.status} /> },
      ];
      return <>
        {l.missing.length ? <div className="alert alert-warn" role="note">Chưa tính được ngày nâng lương. Kiểm tra ngày hưởng bậc hiện tại, thời gian xét và ngạch/bậc của: {l.missing.map(p => p.name || p.code).join(", ")}.</div> : null}
        <FilterBar onReset={() => { setYear(""); setMonth(""); setStatus(""); setUnit(""); setQ(""); }}>
          <NumberField label="Năm" value={year} onChange={setYear} rule={yearRule} error={y.ok ? undefined : y.error} hint="Để trống để xem tất cả năm" />
          <FilterChip label="Tháng" value={month} onChange={setMonth} options={[{ value: "", label: "Tất cả" }, ...Array.from({ length: 12 }, (_, i) => ({ value: String(i + 1).padStart(2, "0"), label: String(i + 1) }))]} />
          <FilterChip label="Trạng thái" value={status} onChange={setStatus} options={[{ value: "", label: "Tất cả" }, ...Object.entries(STATUS_LABEL).map(([value, label]) => ({ value, label }))]} />
          <FilterChip label="Đơn vị" value={unit} onChange={setUnit} options={[{ value: "", label: "Tất cả" }, ...Array.from(new Map(l.items.map(r => [r.unit, r.unit_name || r.unit])).entries()).map(([value, label]) => ({ value, label }))]} />
          <SearchBox value={q} onChange={setQ} placeholder="Tìm họ tên" />
        </FilterBar>
        <div className="row" style={{ marginBottom: 12 }}>
          {canExport && picked.length ? <Link className="btn btn-p btn-sm" href={`${PROMO_BASE}/xuat`} onClick={() => selection.select(picked.map(r=>r.key))}>Xuất danh sách ({picked.length})</Link> : <button type="button" className="btn btn-p btn-sm" disabled>Xuất danh sách</button>}
          {canExport ? <button className="btn btn-o btn-sm" type="button" onClick={()=>setSelected(p=>({...p,...Object.fromEntries(rows.filter(r=>r.status!=="decided").map(r=>[r.key,true]))}))}>Chọn tất cả đang hiển thị</button> : null}
          <span className="mute small">Chọn người để xuất Mẫu số 1.</span>
        </div>
        <DataTable caption="Danh sách nâng lương thường xuyên" rows={rows} columns={columns} rowKey={r => r.key} empty="Không có người đến kỳ phù hợp bộ lọc." />
      </>;
    }}</LoadState>
    {filesOpen ? <PromotionFiles pa={pa} /> : null}
  </>;
}

function PromotionFiles({ pa }: { pa: PromotionApi }) {
  const list = useLoad(() => pa.exports(), []);
  const act = useAction();
  return <section><h2>File đã xuất</h2>{act.view}<LoadState s={list}>{l => <DataTable caption="File đã xuất" rows={l.items} rowKey={r => r.id} columns={[
    { key: "file", header: "Tên file", cell: r => r.file_name },
    { key: "form", header: "Mẫu", cell: r => FORM_LABEL[r.form] },
    { key: "count", header: "Số người", cell: r => r.count },
    { key: "at", header: "Thời điểm xuất", cell: r => new Date(r.recorded_at).toLocaleString("vi-VN") },
    { key: "decision", header: "Quyết định", cell: r => r.decision_no || "—" },
    { key: "download", header: "Tải file", cell: r => <button className="btn btn-o btn-sm" type="button" disabled={act.busy} onClick={() => void act.run(async () => { const f = await pa.file(r.id); saveBlob(f.blob, f.name); return f; }, f => f.drift ? "Đã tải lại; định danh đã thay đổi so với bản gốc." : "Đã tải lại tệp đúng bản gốc.")}>Tải lại</button> },
  ]} empty="Chưa có file nào được xuất." />}</LoadState></section>;
}

export function HrmPromotionExportScreen() {
  const pa = usePromotionApi();
  const {keys:selectedKeys,isCurrent,clear} = usePromotionSelection();
  const [keys] = useState(() => isCurrent() ? selectedKeys : []);
  useEffect(() => () => clear(), [clear]);
  const data = useLoad(() => pa.list(), []);
  return <LoadState s={data}>{l => <PromotionExport pa={pa} rows={l.items.filter(r => keys.includes(r.key) && r.status !== "decided")} />}</LoadState>;
}

function PromotionExport({ pa, rows }: { pa: PromotionApi; rows: PromoCase[] }) {
  const selection = usePromotionSelection();
  const [authorityError,setAuthorityError]=useState<string>();
  const { api } = useAuth();
  const sa = useMemo(() => createStaffApi(api), [api]);
  const staff = useLoad(() => sa.list({ status: "active", limit: 500 }), []);
  const [month, setMonth] = useState(vnToday().slice(0, 7));
  const [preparer, setPreparer] = useState("");
  const [signer, setSigner] = useState<"Giám đốc" | "Phó Giám đốc">("Giám đốc");
  const [preview, setPreview] = useState(false);
  const [done, setDone] = useState(false);
  const [values, setValues] = useState<Record<string, string>>(() => Object.fromEntries(rows.flatMap(r => [[`months:${r.key}`, r.months_enjoyed], [`reserved:${r.key}`, r.reserved], [`after:${r.key}`, r.reserved], [`note:${r.key}`, ""]])));
  const [errors, setErrors] = useState<Record<string, string | undefined>>({});
  const [monthError, setMonthError] = useState<string>();
  const act = useAction();
  const change = (key: string, value: string) => { setValues(p => ({ ...p, [key]: value })); setPreview(false); act.resetIdempotency(); };
  const validate = () => {
    const rules = Object.fromEntries(rows.flatMap(r => [
      [`months:${r.key}`, { kind: "number" as const, ...monthsRule, required: true }],
      [`reserved:${r.key}`, { kind: "number" as const, ...reservedRule }],
      [`after:${r.key}`, { kind: "number" as const, ...reservedRule }],
      [`note:${r.key}`, { kind: "text" as const, maxLength: 300 }],
    ]));
    const checked = validateForm(values, rules);
    setErrors({...checked.errors,...(!preparer?{preparer:"Chọn người lập."}:{})});
    const monthOK = /^[0-9]{4}-(?:0[1-9]|1[0-2])$/.test(month) && month.slice(0, 4) !== "0000";
    setMonthError(monthOK ? undefined : "Nhập tháng xét hợp lệ (YYYY-MM).");
    return checked.valid && monthOK && preparer ? checked.values : null;
  };
  const send = () => {
    if (!selection.isCurrent()) {selection.clear();setAuthorityError("Ngữ cảnh đã thay đổi. Quay lại danh sách để chọn lại người.");return;}
    const checked = validate(); if (!checked || !rows.length) return;
    const map = (prefix: string) => Object.fromEntries(rows.map(r => [r.key, checked[`${prefix}:${r.key}`] ?? ""]));
    void act.run(async () => {
      const out = await pa.exportLists({ keys: rows.map(r => r.key), review_month: month, ...(preparer ? { preparer_id: preparer } : {}), signer_title: signer, months: map("months"), reserved: map("reserved"), reserved_after: map("after"), notes: map("note") });
      selection.clear();
      for (const item of out.items) { const f = await pa.file(item.id); saveBlob(f.blob, f.name); }
      setDone(true); return out;
    }, "Đã xuất Mẫu số 1. Người đã xuất chuyển Chờ quyết định.");
  };
  return <DetailPage back={{ href: PROMO_BASE, label: "Danh sách nâng bậc" }} title="Xuất danh sách nâng bậc" actions={<div className="row">
    <button className="btn btn-o btn-sm" type="button" disabled={!rows.length || done} onClick={() => { const v = validate(); if (v) setPreview(true); }}>Xem trước</button>
    <button className="btn btn-p btn-sm" type="button" disabled={act.busy || !preview || done} onClick={send}>Xuất và tải Mẫu số 1</button>
  </div>}>
    {act.view}
    {authorityError ? <p role="alert">{authorityError}</p> : null}
    {!rows.length ? <p role="alert">Chưa chọn người đến kỳ còn xử lý được. Quay lại danh sách để chọn.</p> : <>
      <div className="grid2">
        <TextField label="Tháng xét (YYYY-MM)" required value={month} onChange={v => { setMonth(v); setPreview(false); act.resetIdempotency(); }} error={monthError} />
        <SelectField label="Người ký" value={signer} onChange={v => { setSigner(v as typeof signer); setPreview(false); act.resetIdempotency(); }} options={[{ value: "Giám đốc", label: "Giám đốc" }, { value: "Phó Giám đốc", label: "Phó Giám đốc" }]} />
        <LoadState s={staff}>{s => <SelectField label="Người lập" required error={errors.preparer} value={preparer} onChange={v => { setPreparer(v); act.resetIdempotency(); }} options={s.items.map(p => ({ value: p.id, label: `${p.label || p.code} (${p.code})` }))} />}</LoadState>
      </div>
      {rows.map(r => <section key={r.key}><h2>{r.name || r.code}</h2><p>{r.unit_name || r.unit} · {fromTo(r).join(" → ")} · Ngày đến kỳ {vnDate(r.due)}</p><div className="grid2">
        <NumberField label={`Số tháng được hưởng của ${r.name || r.code}`} required value={values[`months:${r.key}`] ?? ""} onChange={v => change(`months:${r.key}`, v)} rule={monthsRule} error={errors[`months:${r.key}`]} />
        <NumberField label={`HS bảo lưu hiện có của ${r.name || r.code}`} value={values[`reserved:${r.key}`] ?? ""} onChange={v => { setValues(p => ({...p,[`reserved:${r.key}`]:v,...(p[`after:${r.key}`]===p[`reserved:${r.key}`]?{[`after:${r.key}`]:v}:{})})); setPreview(false); act.resetIdempotency(); }} rule={reservedRule} error={errors[`reserved:${r.key}`]} />
        <NumberField label={`HS bảo lưu sau nâng của ${r.name || r.code}`} value={values[`after:${r.key}`] ?? ""} onChange={v => change(`after:${r.key}`, v)} rule={reservedRule} error={errors[`after:${r.key}`]} />
        <TextField label={`Ghi chú của ${r.name || r.code}`} value={values[`note:${r.key}`] ?? ""} onChange={v => change(`note:${r.key}`, v)} error={errors[`note:${r.key}`]} />
      </div></section>)}
      {preview ? <section><h2>Xem trước Mẫu số 1</h2><p>Tháng {month.slice(5)}/{month.slice(0, 4)} · {signer}. File xuất đủ 20 cột; cột tiền lương tăng thêm để trống.</p><DataTable caption="Xem trước Mẫu số 1" rows={rows} rowKey={r => r.key} columns={[
        { key: "name", header: "Họ tên - đơn vị", cell: r => `${r.name || r.code} - ${r.unit_name || r.unit}` },
        { key: "since", header: "Ngày hưởng bậc hiện tại", cell: r => vnDate(r.step_since ?? "") },
        { key: "steps", header: "Bậc hiện tại → sau nâng", cell: r => fromTo(r).join(" → ") },
        { key: "diff", header: "HS chênh lệch", cell: r => vnNum(r.diff) },
        { key: "next", header: "Thời gian tính lần sau", cell: r => vnDate(r.next_due ?? r.due) },
        { key: "months", header: "Số tháng được hưởng", cell: r => values[`months:${r.key}`] },
        { key: "salary", header: "Tiền lương tăng thêm", cell: () => "" },
        { key: "note", header: "Ghi chú", cell: r => values[`note:${r.key}`] },
      ]} /></section> : null}
    </>}
  </DetailPage>;
}

const EVENT_LABEL: Record<string, string> = { include: "Đưa vào danh sách (lịch sử)", exclude: "Bỏ khỏi danh sách (lịch sử)", export: "Xuất danh sách", decide: "Lưu quyết định", undecide: "Hủy quyết định", return: "Ghi nhận không có tên (lịch sử)" };

export function HrmPromotionCaseScreen() {
  const params = useParams<{ key: string }>();
  const key = decodeURIComponent(params.key);
  const pa = usePromotionApi();
  const detail = useLoad(() => pa.detail(key), [key]);
  return <LoadState s={detail}>{c => <DetailPage back={{ href: PROMO_BASE, label: "Danh sách nâng bậc" }} title={c.name || c.code} badge={<PromoStatusPill s={c.status} />} actions={<Link className="btn btn-o btn-sm" href={`/kn/nhan-su/ho-so/${c.staff_id}`}>Mở hồ sơ</Link>} log={<LogPanel title="Lịch sử xử lý" entries={c.history.map(e => ({ event: EVENT_LABEL[e.action] ?? e.action, actor_display: e.actor, at: e.recorded_at, reason: e.reason }))} />}>
    <LabelValueBlock title="Căn cứ tính ngày đến kỳ" items={[
      { label: "Ngạch", value: c.grade_name || c.grade },
      { label: "Ngày hưởng bậc hiện tại", value: vnDate(c.step_since ?? "") },
      { label: "Thời gian xét nâng lương (tháng)", value: c.promotion_months || "—" },
      { label: "Ngày đến kỳ", value: vnDate(c.due) },
      { label: "Bậc hiện tại → sau nâng", value: fromTo(c).join(" → ") },
      { label: "Hệ số chênh lệch", value: vnNum(c.diff) },
      { label: "Quyết định", value: c.decision_id ? <Link href={`/kn/nhan-su/quyet-dinh-nang-bac/${c.decision_id}`}>{c.decision_no}</Link> : "—" },
    ]} />
  </DetailPage>}</LoadState>;
}
