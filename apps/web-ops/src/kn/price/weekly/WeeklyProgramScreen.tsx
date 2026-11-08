"use client";

import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { Decimal, formatDateVi } from "@/lib/exactDecimal";
import { saveBlob } from "@/lib/files";
import { createWeeklyApi, PROGRAM_TITLE, type Program, type WeeklyApi, type WeeklyCommand, type WeeklyNewItem, type WeeklyPreview, type WeeklyRow, type WeeklyView } from "./api";
import { displayWeekly, rateWeekly, weeklyBalances, weeklyNumber, weeklyPatches, type Draft, type Drafts } from "./model";
import "./weekly.css";

const clients = new WeakMap<object, number>(); let clientNumber = 0;
function clientIdentity(client: object) { let id = clients.get(client); if (id === undefined) { id = ++clientNumber; clients.set(client, id); } return id; }

/** Remount on an authority change so a late response cannot restore another session's editable form. */
export function WeeklyProgramScreen({ program }: { program: Program }) {
  const auth = useAuth(), context = auth.kn.context;
  const api = useMemo(() => createWeeklyApi(auth.api), [auth.api]);
  const allowed = auth.status === "authenticated" && auth.kn.status === "active" && context?.department_code === "thong_tin_truyen_thong" &&
    !context.read_only && (context.role === "kn_tinh" || (context.role === "admin" && !!context.support_session_id)) &&
    (!context.support_session_id || auth.principal?.mfa === true);
  if (!allowed) return <><PageHeader title={PROGRAM_TITLE[program]} /><p role="alert">Chức năng dành cho Cấp Tỉnh Phòng TTTT và Admin trong ngữ cảnh hỗ trợ.</p></>;
  const identity = JSON.stringify([clientIdentity(auth.api), auth.principal, context, auth.kn.status, auth.kn.support]);
  return <WeeklyWorkspace key={`${program}:${identity}`} program={program} api={api} />;
}

function WeeklyWorkspace({ program, api }: { program: Program; api: WeeklyApi }) {
  const [selected, setSelected] = useState("");
  const loaded = useLoad(() => api.read(program, selected), [program, selected]);
  const [notice, setNotice] = useState("");
  const [committed, setCommitted] = useState<WeeklyView | null>(null);
  const editor = (view: WeeklyView) => view.report ? <WeeklyEditor key={`${view.report.id}:${view.version}`} view={view} program={program} api={api}
    onSelect={id => { setCommitted(null); setSelected(id); loaded.reload(); }} onReload={() => { setSelected(view.report!.id); setCommitted(null); loaded.reload(); }}
    onChanged={(next, message) => { setCommitted(next); setNotice(message); }} /> : <p role="status">Chưa có báo cáo tuần trong phạm vi của bạn.</p>;
  return <div className="weekly-program">
    <PageHeader title={PROGRAM_TITLE[program]}>Theo dõi tiến độ và kinh phí thực hiện kế hoạch khuyến nông năm. Quyết định 2855/QĐ-UBND ngày 19/6/2026.</PageHeader>
    {notice && <p className="alert alert-info" role="status">{notice}</p>}
    {committed ? editor(committed) : <LoadState s={loaded}>{view => selected && view.report?.id !== selected ? <p role="status">Đang tải kỳ báo cáo…</p> : editor(view)}</LoadState>}
  </div>;
}

type Modal = { kind: "close" | "discard" } | { kind: "delete" | "adjust"; row: WeeklyRow } | { kind: "add"; parent?: WeeklyRow } | { kind: "import" };

function WeeklyEditor({ view, program, api, onSelect, onReload, onChanged }: { view: WeeklyView; program: Program; api: WeeklyApi; onSelect: (id: string) => void; onReload: () => void; onChanged: (v: WeeklyView, message: string) => void }) {
  const report = view.report!;
  const [drafts, setDrafts] = useState<Drafts>({});
  const [modal, setModal] = useState<Modal | null>(null);
  const action = useAction();
  const patches = weeklyPatches(report.rows, drafts), dirty = Object.keys(drafts).length > 0;
  const errors = patches.errors, invalid = Object.keys(errors).length > 0;
  const draft = report.status === "draft";
  const current = report.id === view.reports.at(-1)?.id;
  const change = (id: string, field: keyof Draft, value: string) => setDrafts(previous => ({ ...previous, [id]: { ...previous[id], [field]: value } }));
  const totals = report.rows.filter(row => !row.group).map(row => weeklyBalances(row, drafts[row.id]));
  const totalMoney = totals.every(row => row.money !== null) ? totals.reduce((sum, row) => sum.add(row.money!), Decimal.ZERO).toString() : null;
  const command = (kind: WeeklyCommand["action"]): WeeklyCommand => ({ version: view.version, week_id: report.id, action: kind });
  const write = async (input: WeeklyCommand, message: string) => {
    const result = await action.run(() => api.write(program, input));
    if (result) { setModal(null); onChanged(result, message); }
  };
  const download = (kind: "export" | "template" | "issues") => action.run(async () => { const file = await api[kind](program, report.id); saveBlob(file.blob, file.name); });
  const input = (row: WeeklyRow, field: "progress" | "money" | "money_override" | "evaluation" | "note", label: string) => {
    const balance = weeklyBalances(row, drafts[row.id]);
    const original = field === "money_override" ? row.money_override ?? balance.money?.toString() ?? "" : field === "progress" ? row.progress_set ? row.progress : "" : field === "money" ? row.money_set ? row.money : "" : row[field];
    const value = drafts[row.id]?.[field] ?? original;
    const id = `weekly-${row.id}-${field}`, error = errors[`${row.id}.${field}`];
    return <div className="weekly-field">
      {field === "evaluation" || field === "note" ? <textarea id={id} className="input" rows={2} aria-label={`${label} ${row.name}`} value={value} disabled={action.busy} aria-invalid={!!error} aria-describedby={error ? `${id}-error` : undefined} onChange={event => change(row.id, field, event.target.value)} /> :
        <input id={id} className="input" inputMode={field === "progress" ? "decimal" : "numeric"} aria-label={`${label} ${row.name}`} value={value} disabled={action.busy} aria-invalid={!!error} aria-describedby={error ? `${id}-error` : undefined} onChange={event => change(row.id, field, event.target.value)} />}
      {error && <small id={`${id}-error`} className="err" role="alert">{error}</small>}
      {field === "money_override" && <small className="muted">{row.money_override !== null || drafts[row.id]?.money_override ? "Số nhập tay thay thế lũy kế từ tuần này." : "Tự cộng dồn; để trống để dùng số tự cộng."}</small>}
    </div>;
  };
  return <>
    <div className="weekly-toolbar">
      <label htmlFor="weekly-period">Kỳ báo cáo</label>
      <select id="weekly-period" className="input" value={report.id} disabled={action.busy || dirty} onChange={event => onSelect(event.target.value)}>
        {[...view.reports].reverse().map(week => <option key={week.id} value={week.id}>Tuần {week.number}, đến ngày {formatDateVi(week.to)} — {week.status === "draft" ? "Bản nháp" : "Đã chốt"}</option>)}
      </select>
      <StatusPill tone={draft ? "warn" : "ok"}>{draft ? "Bản nháp" : "Đã chốt"}</StatusPill>
      <button type="button" className="btn btn-o" disabled={action.busy || dirty} onClick={() => void download("export")}>Xuất Excel</button>
      {draft && <button type="button" className="btn btn-o" disabled={action.busy || dirty} onClick={() => setModal({ kind: "import" })}>Nhập từ Excel</button>}
    </div>
    {dirty && <p role="status">Có thay đổi chưa lưu. Lưu nháp hoặc hủy thay đổi trước khi đổi kỳ, nhập/xuất hoặc thay đổi hạng mục.</p>}
    {!draft && <p role="note">Báo cáo đã chốt — chỉ xem. Lũy kế là số đầu kỳ của tuần tiếp theo.</p>}
    {!modal && action.view}
    {action.problem?.status === 409 && <button type="button" className="btn btn-o" onClick={onReload}>Tải lại số liệu mới</button>}
    <div className={`weekly-kpis ${program === "information" ? "weekly-kpis-info" : ""}`} aria-live="polite">
      <div className="card"><span>Số hạng mục</span><strong>{totals.length}</strong></div>
      <div className="card"><span>Đạt 100% kế hoạch</span><strong>{totals.filter(row => row.achieved).length}</strong></div>
      {program === "training" && <div className="card"><span>Kinh phí lũy kế (đồng)</span><strong data-testid="weekly-total-money">{totalMoney === null ? "—" : displayWeekly(totalMoney)}</strong></div>}
    </div>
    <div className="tbl-wrap weekly-table" tabIndex={0} aria-label="Bảng báo cáo tuần, có thể cuộn ngang">
      <table className="dt">
        <caption className="sr-only">{PROGRAM_TITLE[program]} — Tuần {report.number}, đến ngày {formatDateVi(report.to)}</caption>
        <thead><tr><th rowSpan={2} scope="col">TT</th><th rowSpan={2} scope="col">Tên chương trình khuyến nông</th><th rowSpan={2} scope="col">Quy mô</th><th colSpan={2} scope="colgroup">Thực hiện tuần {report.number}</th><th rowSpan={2} scope="col">Đánh giá tuần</th><th colSpan={2} scope="colgroup">Lũy kế</th>{program === "information" && <th rowSpan={2} scope="col">Ghi chú</th>}{draft && <th rowSpan={2} scope="col">Thao tác</th>}</tr>
          <tr><th scope="col">Tiến độ</th><th scope="col">Kinh phí (đồng)</th><th scope="col">Tiến độ / Kế hoạch</th><th scope="col">Kinh phí (đồng)</th></tr></thead>
        <tbody>{report.rows.map(row => {
          const balance = weeklyBalances(row, drafts[row.id]);
          const buttons = draft && <td className="weekly-row-actions"><button type="button" className="btn btn-o btn-sm" aria-label={`Thêm mục con ${row.name}`} disabled={action.busy || dirty || !!row.parent_id || (!row.group && (row.progress_set || row.money_set || row.opening_progress !== "0" || row.opening_money !== "0" || !!row.money_override || !!row.evaluation || !!row.note))} onClick={() => setModal({ kind: "add", parent: row })}>+</button>
            {!row.group && <button type="button" className="btn btn-o btn-sm" disabled={action.busy || dirty} aria-label={`Điều chỉnh kế hoạch ${row.name}`} onClick={() => setModal({ kind: "adjust", row })}>Điều chỉnh</button>}
            <button type="button" className="btn btn-o btn-sm" disabled={action.busy || dirty} aria-label={`Xóa ${row.name}`} onClick={() => setModal({ kind: "delete", row })}>Xóa</button></td>;
          if (row.group) return <tr key={row.id} className="weekly-group"><td>{row.number}</td><th scope="row" colSpan={program === "information" ? 8 : 7}>{row.name}</th>{buttons}</tr>;
          return <tr key={row.id}><td>{row.number}</td><th scope="row" className={row.parent_id ? "weekly-child" : ""}>{row.name}</th><td>{displayWeekly(row.plan)}{row.adjusted_plan !== null && <div><strong>Điều chỉnh: {displayWeekly(row.adjusted_plan)}</strong><small>{row.plan_reason}</small></div>}</td>
            <td>{draft ? input(row, "progress", "Tiến độ tuần") : displayWeekly(row.progress)}</td><td>{draft ? input(row, "money", "Kinh phí tuần") : displayWeekly(row.money)}</td><td>{draft ? input(row, "evaluation", "Đánh giá tuần") : row.evaluation || "—"}</td>
            <td><strong>{balance.progress === null ? "—" : displayWeekly(balance.progress.toString())} / {displayWeekly(row.adjusted_plan ?? row.plan)}</strong><small className={balance.achieved ? "weekly-achieved" : "muted"}>{rateWeekly(balance.rate)}</small></td>
            <td>{draft ? input(row, "money_override", "Kinh phí lũy kế") : displayWeekly(row.cumulative_money)}</td>{program === "information" && <td>{draft ? input(row, "note", "Ghi chú") : row.note || "—"}{row.plan_reason && <small>Điều chỉnh kế hoạch: {row.plan_reason}</small>}</td>}{buttons}</tr>;
        })}{report.rows.length === 0 && <tr><td colSpan={program === "information" ? draft ? 10 : 9 : draft ? 9 : 8}>Chưa có hạng mục. Thêm hạng mục để lập báo cáo tuần.</td></tr>}</tbody>
      </table>
    </div>
    <div className="weekly-footer">
      {draft ? <><button type="button" className="btn btn-o" disabled={action.busy || dirty} onClick={() => setModal({ kind: "add" })}>Thêm hạng mục</button><button type="button" className="btn btn-o" disabled={!dirty || action.busy} onClick={() => setModal({ kind: "discard" })}>Hủy thay đổi</button>
        <button type="button" className="btn" disabled={action.busy || invalid} onClick={() => void write({ ...command("save"), rows: patches.rows }, "Đã lưu nháp.")}>Lưu nháp</button>
        <button type="button" className="btn btn-primary" disabled={action.busy || invalid} onClick={() => setModal({ kind: "close" })}>Chốt báo cáo tuần {report.number}</button></> : current && <button type="button" className="btn btn-primary" disabled={action.busy} onClick={() => void write(command("new"), "Đã tạo báo cáo tuần mới, kế thừa lũy kế đã chốt.")}>Tạo báo cáo tuần mới</button>}
      {!!report.import_issues.filter(issue => issue.status === "error").length && <button type="button" className="btn btn-o" disabled={action.busy} onClick={() => void download("issues")}>Tải danh sách dòng lỗi</button>}
    </div>
    <details className="card weekly-audit"><summary>Nhật ký thao tác ({view.log.length})</summary><ol>{view.log.map(entry => <li key={entry.version}><strong>{({ initialize: "Khởi tạo tuần đầu", save: "Lưu nháp", close: "Chốt tuần", new: "Tạo tuần mới", adjust: "Điều chỉnh kế hoạch", add: "Thêm hạng mục", delete: "Xóa hạng mục", import: "Nhập Excel" })[entry.action]}</strong> · {entry.actor} · <time dateTime={entry.at}>{new Date(entry.at).toLocaleString("vi-VN", { timeZone: "Asia/Ho_Chi_Minh" })}</time></li>)}</ol></details>
    {modal?.kind === "close" && <Dialog title={`Chốt báo cáo tuần ${report.number}`} onClose={() => !action.busy && setModal(null)} actions={<button type="button" className="btn btn-primary" disabled={action.busy} onClick={() => void write({ ...command("close"), rows: patches.rows, confirm: true }, "Đã chốt báo cáo tuần.")}>Xác nhận chốt</button>}><p>Sau khi chốt, số liệu và hạng mục của tuần này chỉ xem. Lũy kế sẽ là số đầu kỳ của tuần tiếp theo.</p>{action.view}</Dialog>}
    {modal?.kind === "discard" && <Dialog title="Hủy thay đổi chưa lưu" onClose={() => setModal(null)} actions={<button type="button" className="btn" onClick={() => { setDrafts({}); setModal(null); }}>Xác nhận hủy thay đổi</button>}><p>Các ô chưa lưu sẽ được đưa về số liệu đã lưu gần nhất.</p></Dialog>}
    {modal?.kind === "delete" && <Dialog title="Xóa hạng mục" onClose={() => !action.busy && setModal(null)} actions={<button type="button" className="btn" disabled={action.busy} onClick={() => void write({ ...command("delete"), item_id: modal.row.id, confirm: true }, "Đã xóa hạng mục và cập nhật lũy kế các tuần.")}>Xác nhận xóa</button>}><p>Xóa “{modal.row.name}”{modal.row.group && " và toàn bộ mục con"} sẽ làm thay đổi lũy kế và tổng kinh phí các tuần, kể cả tuần đã chốt. Bạn xác nhận xóa?</p>{action.view}</Dialog>}
    {modal?.kind === "adjust" && <AdjustDialog row={modal.row} busy={action.busy} feedback={action.view} onClose={() => !action.busy && setModal(null)} onSave={(plan, reason) => void write({ ...command("adjust"), item_id: modal.row.id, plan, reason }, "Đã điều chỉnh kế hoạch.")} />}
    {modal?.kind === "add" && <AddDialog parent={modal.parent} busy={action.busy} feedback={action.view} onClose={() => !action.busy && setModal(null)} onSave={item => void write({ ...command("add"), item }, "Đã thêm hạng mục.")} />}
    {modal?.kind === "import" && <ImportDialog api={api} program={program} view={view} busy={action.busy} feedback={action.view} run={action.run} onClose={() => !action.busy && setModal(null)} onImported={v => { setModal(null); onChanged(v, "Đã ghi các dòng hợp lệ từ Excel."); }} />}
  </>;
}

type Feedback = React.ReactNode;

function AdjustDialog({ row, busy, feedback, onClose, onSave }: { row: WeeklyRow; busy: boolean; feedback: Feedback; onClose: () => void; onSave: (plan: string, reason: string) => void }) {
  const [plan, setPlan] = useState(row.adjusted_plan ?? row.plan), [reason, setReason] = useState("");
  const valid = !!weeklyNumber(plan) && reason.trim().length > 0 && Array.from(reason).length <= 500;
  return <Dialog title="Điều chỉnh kế hoạch" onClose={onClose} actions={<button type="button" className="btn" disabled={busy || !valid} onClick={() => onSave(weeklyNumber(plan)!.toString(), reason)}>Lưu điều chỉnh</button>}><p>{row.name} · Quy mô theo quyết định: {displayWeekly(row.plan)}</p>
    <div className="field"><label htmlFor="weekly-adjust-plan">Quy mô điều chỉnh</label><input id="weekly-adjust-plan" className="input" inputMode="decimal" value={plan} disabled={busy} aria-invalid={!weeklyNumber(plan)} onChange={event => setPlan(event.target.value)} />{!weeklyNumber(plan) && <small className="err" role="alert">Nhập quy mô không âm, tối đa 15 chữ số và 4 số lẻ.</small>}</div>
    <div className="field"><label htmlFor="weekly-adjust-reason">Lý do điều chỉnh</label><textarea id="weekly-adjust-reason" className="input" value={reason} disabled={busy} required onChange={event => setReason(event.target.value)} /></div>{feedback}</Dialog>;
}

function AddDialog({ parent, busy, feedback, onClose, onSave }: { parent?: WeeklyRow; busy: boolean; feedback: Feedback; onClose: () => void; onSave: (item: WeeklyNewItem) => void }) {
  const [name, setName] = useState(""), [plan, setPlan] = useState("");
  const [group, setGroup] = useState(false), [children, setChildren] = useState([{ name: "", plan: "" }]);
  const named = (value: string) => value.trim().length > 0 && Array.from(value).length <= 200;
  const valid = named(name) && (group ? children.every(child => named(child.name) && !!weeklyNumber(child.plan)) : !!weeklyNumber(plan));
  return <Dialog title={parent ? "Thêm mục con" : "Thêm hạng mục"} onClose={onClose} actions={<button type="button" className="btn" disabled={busy || !valid} onClick={() => onSave({ name, parent_id: parent?.id, ...(group ? { children: children.map(child => ({ name: child.name, plan: weeklyNumber(child.plan)!.toString() })) } : { plan: weeklyNumber(plan)!.toString() }) })}>Thêm vào báo cáo</button>}>
    {parent && <p>Mục chính: {parent.name}{!parent.group && ". Mục chính chưa có số liệu sẽ chuyển thành nhóm."}</p>}
    <div className="field"><label htmlFor="weekly-new-name">Tên hạng mục (kèm đơn vị tính)</label><input id="weekly-new-name" className="input" value={name} disabled={busy} onChange={event => setName(event.target.value)} /></div>
    {!parent && <label className="row"><input type="checkbox" checked={group} disabled={busy} onChange={event => setGroup(event.target.checked)} /> Mục nhóm gồm các mục con</label>}
    {!group ? <div className="field"><label htmlFor="weekly-new-plan">Quy mô kế hoạch</label><input id="weekly-new-plan" className="input" inputMode="decimal" value={plan} disabled={busy} aria-invalid={plan !== "" && !weeklyNumber(plan)} onChange={event => setPlan(event.target.value)} /></div> : <fieldset disabled={busy}><legend>Các mục con</legend>{children.map((child, index) => <div className="weekly-new-child" key={index}><label>Tên mục con {index + 1}<input className="input" value={child.name} onChange={event => setChildren(previous => previous.map((value, at) => at === index ? { ...value, name: event.target.value } : value))} /></label><label>Quy mô mục con {index + 1}<input className="input" inputMode="decimal" value={child.plan} onChange={event => setChildren(previous => previous.map((value, at) => at === index ? { ...value, plan: event.target.value } : value))} /></label>{children.length > 1 && <button type="button" className="btn btn-o" onClick={() => setChildren(previous => previous.filter((_, at) => at !== index))}>Bỏ mục con {index + 1}</button>}</div>)}<button type="button" className="btn btn-o" onClick={() => setChildren(previous => [...previous, { name: "", plan: "" }])}>Thêm mục con vào nhóm</button></fieldset>}
    {feedback}</Dialog>;
}

function ImportDialog({ api, program, view, busy, feedback, run, onClose, onImported }: { api: WeeklyApi; program: Program; view: WeeklyView; busy: boolean; feedback: Feedback; run: ReturnType<typeof useAction>["run"]; onClose: () => void; onImported: (v: WeeklyView) => void }) {
  const [file, setFile] = useState<File | null>(null), [preview, setPreview] = useState<WeeklyPreview | null>(null), [overwrite, setOverwrite] = useState(false);
  const report = view.report!;
  const previewFile = async () => { if (!file) return; const result = await run(() => api.preview(program, { file, version: view.version, week_id: report.id })); if (result) { setPreview(result); setOverwrite(false); } };
  const commit = async () => { if (!file || !preview) return; const result = await run(() => api.commit(program, { file, version: preview.version, week_id: preview.week_id, sha256: preview.sha256, confirm_overwrite: overwrite })); if (result) onImported(result); };
  const errorsFile = () => {
    if (!preview) return;
    const quote = (value: string) => `"${(/^[=+@-]/.test(value) ? "'" + value : value).replaceAll('"', '""')}"`;
    const rows = [["Dòng", "Tên hạng mục", "Lỗi"], ...preview.rows.filter(row => row.status === "error").map(row => [String(row.line), row.name, row.errors.join("; ")])];
    saveBlob(new Blob(["\uFEFF", rows.map(row => row.map(quote).join(",")).join("\r\n")], { type: "text/csv;charset=utf-8" }), "dong-loi-nhap-tuan.csv");
  };
  return <Dialog title="Nhập từ Excel" onClose={onClose} actions={<><button type="button" className="btn btn-o" disabled={busy || !file} onClick={() => void previewFile()}>Kiểm tra tệp</button><button type="button" className="btn" disabled={busy || !preview || preview.valid_rows === 0 || (preview.overwrite_rows > 0 && !overwrite)} onClick={() => void commit()}>Ghi nhận dòng hợp lệ</button></>}>
    <p>Ghép theo tên hạng mục. Ô trống giữ số liệu đã nhập; cột Lũy kế được bỏ qua. Dòng lỗi không cản ghi các dòng hợp lệ.</p>
    <button type="button" className="btn btn-o" disabled={busy} onClick={() => void run(async () => { const result = await api.template(program, report.id); saveBlob(result.blob, result.name); })}>Tải tệp mẫu</button>
    <div className="field"><label htmlFor="weekly-import-file">Chọn tệp Excel hoặc CSV (tối đa 5 MB)</label><input id="weekly-import-file" type="file" accept=".xlsx,.csv" disabled={busy} onChange={event => { setFile(event.target.files?.[0] ?? null); setPreview(null); setOverwrite(false); }} /></div>
    {preview && <><p className="alert alert-info">{preview.source_columns}</p><p role="status">{preview.valid_rows} dòng hợp lệ · {preview.overwrite_rows} cảnh báo ghi đè · {preview.error_rows} dòng lỗi</p>{preview.overwrite_rows > 0 && <label className="row"><input type="checkbox" checked={overwrite} disabled={busy} onChange={event => setOverwrite(event.target.checked)} /> Xác nhận ghi đè các ô có số liệu khác</label>}
      {preview.error_rows > 0 && <button type="button" className="btn btn-o" onClick={errorsFile}>Tải danh sách dòng lỗi</button>}<div className="tbl-wrap"><table className="dt"><thead><tr><th scope="col">Dòng</th><th scope="col">Hạng mục</th><th scope="col">Kết quả</th></tr></thead><tbody>{preview.rows.map(row => <tr key={row.line}><td>{row.line}</td><td>{row.name}</td><td>{row.status === "error" ? row.errors.join("; ") : row.status === "overwrite" ? "Cảnh báo ghi đè" : "Hợp lệ"}</td></tr>)}</tbody></table></div></>}{feedback}
  </Dialog>;
}
