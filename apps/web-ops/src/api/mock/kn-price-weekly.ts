// Demo contract for FN-TI-02.13. Runtime starts with official catalog/plan values and zero results.
// Production parsing/rendering/transactions live in price/app/weekly*.go and are verified on native PostgreSQL.
import { uuidv7 } from "../idempotency";
import type { MockPersona } from "./personas";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { zipStored } from "./kn-khtc-archive-zip";
import { readStoredXlsx } from "./forms/xlsx";
import { Decimal } from "@/lib/exactDecimal";
import { weeklyBalances, weeklyNumber } from "@/kn/price/weekly/model";
import { PROGRAM_TITLE, type Program, type WeeklyCommand, type WeeklyNewItem, type WeeklyPatch, type WeeklyPreview, type WeeklyReport, type WeeklyRow, type WeeklyView } from "@/kn/price/weekly/api";
import catalog from "./fixtures/price-weekly-catalog.json";

interface Book { version: number; reports: WeeklyReport[]; deleted: Set<string>; log: WeeklyView["log"] }
const books = new Map<string, Book>();
const enc = new TextEncoder();
const fold = (s: string) => s.normalize("NFD").replace(/[\u0300-\u036f]/g, "").replace(/đ/gi, "d").toLowerCase().replace(/[^a-z0-9]+/g, "_").replace(/^_|_$/g, "");
const forbidden = () => problem(403, "forbidden", "Chỉ Cấp Tỉnh phòng TTTT và Admin trong ngữ cảnh hỗ trợ được thao tác");
const invalid = (message: string) => problem(422, "validation_failed", message);
const conflict = (message: string) => problem(409, "price.weekly_stale", message);
const validProgram = (program: string): program is Program => program === "information" || program === "training";

/** Same current-scope check as the native module, called before global mock replay. */
export function weeklyAuthorization(request: Request, persona: MockPersona): Response | null {
  const k = knContext(request.headers.get("X-AG-Context") ?? "");
  if (!k || k.read_only || k.department_code !== "thong_tin_truyen_thong" || (k.role !== "kn_tinh" && !(k.role === "admin" && k.support_session_id)) || (k.support_session_id && !persona.amr.includes("otp"))) return forbidden();
  return null;
}

function period(date: Date) {
  const d = new Date(date); d.setUTCHours(0, 0, 0, 0);
  const from = new Date(d); from.setUTCDate(d.getUTCDate() - ((d.getUTCDay() - 3 + 7) % 7));
  const to = new Date(from); to.setUTCDate(from.getUTCDate() + 6);
  const first = new Date(Date.UTC(to.getUTCFullYear(), 0, 1)); first.setUTCDate(first.getUTCDate() - ((first.getUTCDay() - 3 + 7) % 7));
  const number = Math.floor((from.getTime() - first.getTime()) / 604800000) + 1;
  return { number, key: `${to.getUTCFullYear()}-W${String(number).padStart(2, "0")}`, from: from.toISOString().slice(0, 10), to: to.toISOString().slice(0, 10) };
}

function row(name: string, plan: string, parent_id = "", group = false): WeeklyRow {
  return { id: uuidv7(), parent_id, name, plan, group, adjusted_plan: null, plan_reason: "", progress: "0", money: "0", progress_set: false, money_set: false, money_override: null, opening_progress: "0", opening_money: "0", evaluation: "", note: "", number: "", cumulative_progress: "0", cumulative_money: "0", rate: null, achieved: false };
}

function project(report: WeeklyReport, deleted: Set<string>): WeeklyReport {
  let n = 0, total = Decimal.ZERO, count = 0, achieved = 0;
  const rows = report.rows.filter(r => !deleted.has(r.id)).map(r => {
    const b = weeklyBalances(r), number = r.parent_id ? "" : String(++n);
    if (!r.group) { count++; total = total.add(b.money!); if (b.achieved) achieved++; }
    return { ...r, number, cumulative_progress: r.group ? "0" : b.progress!.toString(), cumulative_money: r.group ? "0" : b.money!.toString(), rate: r.group ? null : b.rate?.toString() ?? null, achieved: !r.group && b.achieved };
  });
  return { ...structuredClone(report), rows, item_count: count, achieved, total_money: total.toString() };
}

function view(program: Program, book: Book, selected?: string): WeeklyView | Response {
  const report = selected ? book.reports.find(r => r.id === selected) : book.reports.at(-1);
  if (!report) return problem(404, "price.weekly_not_found", "Không có báo cáo tuần này");
  return { program, title: program === "training" ? "Chương trình đào tạo huấn luyện và hợp tác" : PROGRAM_TITLE[program], plan_year: 2026, version: book.version,
    reports: book.reports.map(({ id, number, key, from, to, status }) => ({ id, number, key, from, to, status })), report: project(report, book.deleted), log: structuredClone(book.log) };
}

function load(ctx: Ctx): { program: Program; key: string; book: Book } | Response {
  const program = ctx.params[0] ?? "";
  if (!validProgram(program) || (ctx.url.searchParams.get("profile") ?? "kn") !== "kn") return problem(404, "price.weekly_not_found", "Không có chương trình này");
  const key = `${ctx.persona.me.tenant_id}:kn:${program}`;
  let book = books.get(key);
  if (!book) {
    const now = new Date(new Date().toLocaleDateString("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }) + "T00:00:00Z");
    const codes = new Map(catalog[program].map(c => [c.code, uuidv7()]));
    const rows = catalog[program].map(c => ({ ...row(c.name, c.plan || "0", c.parent ? codes.get(c.parent)! : "", c.plan === ""), id: codes.get(c.code)! }));
    const report: WeeklyReport = { id: uuidv7(), ...period(now), status: "draft", rows, item_count: 0, achieved: 0, total_money: "0", import_issues: [] };
    book = { version: 1, reports: [report], deleted: new Set(), log: [{ version: 1, week_id: report.id, action: "initialize", actor: ctx.persona.me.actor_ref, at: new Date().toISOString() }] };
    books.set(key, book);
  }
  return { program, key, book };
}

function hasData(r: WeeklyRow) { return r.progress_set || r.money_set || r.opening_progress !== "0" || r.opening_money !== "0" || r.progress !== "0" || r.money !== "0" || r.money_override !== null || !!r.evaluation || !!r.note; }

function applyRows(report: WeeklyReport, book: Book, program: Program, patches: WeeklyPatch[]): Response | null {
  if (!Array.isArray(patches) || patches.length > 200) return invalid("Tối đa 200 dòng mỗi lần lưu");
  const seen = new Set<string>();
  for (const patch of patches) {
    const r = report.rows.find(r => r.id === patch.id && !book.deleted.has(r.id));
    if (!r || seen.has(patch.id) || r.group) return invalid("Hạng mục không tồn tại, trùng dòng hoặc là mục nhóm");
    seen.add(patch.id);
    for (const field of ["progress", "money"] as const) {
      if (patch[field] === undefined) continue;
      if (typeof patch[field] !== "string") return invalid("Số liệu phải là chuỗi thập phân");
      const n = weeklyNumber(patch[field]!, field === "money"); if (!n) return invalid(`${r.name}: số liệu không hợp lệ`);
      r[field] = n.toString(); r[field === "progress" ? "progress_set" : "money_set"] = true;
    }
    if (patch.money_override !== undefined && patch.clear_override) return invalid("Không đồng thời ghi và xóa kinh phí lũy kế");
    if (patch.clear_override) r.money_override = null;
    if (patch.money_override !== undefined) { const n = typeof patch.money_override === "string" ? weeklyNumber(patch.money_override, true) : null; if (!n) return invalid("Kinh phí lũy kế phải là số đồng không âm"); r.money_override = n.toString(); }
    for (const field of ["evaluation", "note"] as const) if (patch[field] !== undefined) { if (typeof patch[field] !== "string" || Array.from(patch[field]!).length > 2000) return invalid("Đánh giá/ghi chú tối đa 2000 ký tự"); r[field] = patch[field]!.trim(); }
    if (program === "training" && r.note) return invalid("Chỉ Chương trình thông tin có cột Ghi chú");
    const b = weeklyBalances(r); if (!b.progress || !b.money || b.progress.gt(Decimal.parse("999999999999999.9999")!) || b.money.gt(Decimal.parse("999999999999999999")!)) return invalid("Lũy kế vượt giới hạn lưu số liệu");
  }
  return null;
}

function addItem(report: WeeklyReport, book: Book, item: WeeklyNewItem | undefined): Response | null {
  if (!item || report.rows.length + (item.children?.length ?? 0) + 1 > 200) return invalid("Cần hạng mục, tối đa 200 dòng");
  const children = item.children ?? [], names = new Set(report.rows.filter(r => !book.deleted.has(r.id)).map(r => fold(r.name)));
  for (const [index, i] of [item, ...children].entries()) {
    const name = i.name?.trim(); if (!name || Array.from(name).length > 200 || !fold(name) || names.has(fold(name))) return invalid("Tên hạng mục bắt buộc, không trùng, tối đa 200 ký tự");
    if (index > 0 && ((i.children?.length ?? 0) > 0 || i.parent_id)) return invalid("Chỉ có một cấp mục con");
    if ((index > 0 || children.length === 0) && (typeof i.plan !== "string" || !weeklyNumber(i.plan))) return invalid("Quy mô kế hoạch không hợp lệ");
    names.add(fold(name));
  }
  const parent = item.parent_id ? report.rows.find(r => r.id === item.parent_id && !book.deleted.has(r.id)) : null;
  if (item.parent_id && (!parent || parent.parent_id || children.length)) return invalid("Chỉ thêm mục con vào mục chính");
  if (parent && !parent.group) {
    if (book.reports.some(w => w.rows.some(r => r.id === parent.id && hasData(r)))) return invalid("Mục chính đã có số liệu, không thể chuyển thành nhóm");
    parent.group = true; parent.plan = "0"; parent.adjusted_plan = null; parent.plan_reason = "";
  }
  const r = row(item.name.trim(), children.length ? "0" : weeklyNumber(item.plan!)!.toString(), item.parent_id, children.length > 0);
  const added = [r, ...children.map(c => row(c.name.trim(), weeklyNumber(c.plan!)!.toString(), r.id))];
  if (!parent) report.rows.push(...added);
  else { let at = report.rows.indexOf(parent) + 1; while (report.rows[at]?.parent_id === parent.id) at++; report.rows.splice(at, 0, ...added); }
  return null;
}

function write(ctx: Ctx, command: WeeklyCommand, importIssues?: WeeklyPreview["rows"]): Response {
  const denied = weeklyAuthorization(ctx.request, ctx.persona); if (denied) return denied;
  const loaded = load(ctx); if (loaded instanceof Response) return loaded;
  const { program, key } = loaded, book = structuredClone(loaded.book);
  if (command.version !== book.version) return conflict("Báo cáo đã được cập nhật — tải lại số liệu mới");
  const report = book.reports.find(r => r.id === command.week_id);
  if (!report) return problem(404, "price.weekly_not_found", "Không có kỳ báo cáo này");
  let selected = report.id;
  const action = importIssues ? "import" : command.action;
  if (command.action === "new") {
    if (report.status !== "closed" || report !== book.reports.at(-1)) return problem(409, "price.weekly_closed", "Chỉ tạo tuần mới sau tuần đã chốt mới nhất");
    const date = new Date(`${report.to}T00:00:00Z`); date.setUTCDate(date.getUTCDate() + 1);
    const rows = project(report, book.deleted).rows.map(r => ({ ...r, opening_progress: r.cumulative_progress, opening_money: r.cumulative_money, progress: "0", money: "0", progress_set: false, money_set: false, money_override: null, evaluation: "", note: "" }));
    const next: WeeklyReport = { ...report, ...period(date), id: uuidv7(), status: "draft", rows, import_issues: [] }; book.reports.push(next); selected = next.id;
  } else {
    if (report.status !== "draft" || report !== book.reports.at(-1)) return problem(409, "price.weekly_closed", "Báo cáo đã chốt — chỉ xem");
    if (command.action === "save" || command.action === "close") {
      if (command.action === "close" && !command.confirm) return problem(409, "price.weekly_confirmation", "Cần xác nhận chốt tuần");
      const error = applyRows(report, book, program, command.rows ?? []); if (error) return error;
      if (command.action === "close") report.status = "closed";
      if (importIssues) report.import_issues = importIssues;
    } else if (command.action === "adjust") {
      const r = report.rows.find(r => r.id === command.item_id && !book.deleted.has(r.id));
      const plan = typeof command.plan === "string" ? weeklyNumber(command.plan) : null;
      if (!r || r.group || !plan || !command.reason?.trim() || Array.from(command.reason).length > 500) return invalid("Cần hạng mục, quy mô không âm và lý do điều chỉnh");
      r.adjusted_plan = plan.toString(); r.plan_reason = command.reason.trim();
    } else if (command.action === "add") {
      const error = addItem(report, book, command.item); if (error) return error;
    } else if (command.action === "delete") {
      const r = report.rows.find(r => r.id === command.item_id && !book.deleted.has(r.id)); if (!r) return problem(404, "price.weekly_not_found", "Không có hạng mục này");
      const remove = new Set([r.id, ...book.reports.flatMap(w => w.rows.filter(x => x.parent_id === r.id).map(x => x.id))]);
      if (!command.confirm && book.reports.some(w => w.rows.some(x => remove.has(x.id) && hasData(x)))) return problem(409, "price.weekly_confirmation", "Xóa làm thay đổi lũy kế và tổng các tuần, kể cả tuần đã chốt");
      for (const id of remove) book.deleted.add(id);
    } else return invalid("Thao tác không hợp lệ");
  }
  book.version++; book.log.unshift({ version: book.version, week_id: selected, action, actor: ctx.persona.me.actor_ref, at: new Date().toISOString() }); books.set(key, book);
  const result = view(program, book, selected); return result instanceof Response ? result : json(result);
}

const xml = (s: string) => s.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");
const letter = (n: number) => String.fromCharCode(65 + n);

function workbook(rows: string[][], merges: string[], name: string): Response {
  const sheet = `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="5" topLeftCell="A6" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols><col min="2" max="2" width="56" customWidth="1"/><col min="3" max="9" width="25" customWidth="1"/></cols><sheetData>${rows.map((cells, index) => `<row r="${index + 1}" ht="35" customHeight="1">${cells.map((cell, col) => `<c r="${letter(col)}${index + 1}" t="inlineStr" s="${index < 5 ? 1 : 2}"><is><t xml:space="preserve">${xml(cell)}</t></is></c>`).join("")}</row>`).join("")}</sheetData><mergeCells count="${merges.length}">${merges.map(range => `<mergeCell ref="${range}"/>`).join("")}</mergeCells></worksheet>`;
  const files: Record<string, string> = {
    "[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`,
    "_rels/.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
    "xl/workbook.xml": `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Bao cao tuan" sheetId="1" r:id="rId1"/></sheets></workbook>`,
    "xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`,
    "xl/styles.xml": `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="2"><border/><border><left style="thin"/><right style="thin"/><top style="thin"/><bottom style="thin"/></border></borders><cellStyleXfs count="1"><xf/></cellStyleXfs><cellXfs count="3"><xf/><xf fontId="1" borderId="1" applyBorder="1" applyAlignment="1"><alignment wrapText="1" vertical="center"/></xf><xf fontId="0" borderId="1" applyBorder="1" applyAlignment="1"><alignment wrapText="1" vertical="center"/></xf></cellXfs></styleSheet>`,
    "xl/worksheets/sheet1.xml": sheet,
  };
  const bytes = zipStored(Object.entries(files).map(([name, value]) => ({ name, data: enc.encode(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>${value}`) })));
  return new Response(bytes.slice().buffer, { headers: { "Content-Type": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "Content-Disposition": `attachment; filename*=UTF-8''${encodeURIComponent(name)}` } });
}

function exportFile(ctx: Ctx, kind: "export" | "template" | "issues"): Response {
  const denied = weeklyAuthorization(ctx.request, ctx.persona); if (denied) return denied;
  const loaded = load(ctx); if (loaded instanceof Response) return loaded;
  const selected = view(loaded.program, loaded.book, ctx.url.searchParams.get("week_id") ?? undefined); if (selected instanceof Response) return selected;
  const r = selected.report!, info = loaded.program === "information", last = info ? "I" : "H";
  if (kind === "issues") return workbook([["Dòng", "Tên hạng mục", "Lỗi"], ...r.import_issues.filter(i => i.status === "error").map(i => [String(i.line), i.name, i.errors.join("; ")])], [], "dong-loi-bao-cao-tuan.xlsx");
  const rows: string[][] = [[selected.title], [`Tuần ${r.number}, đến ngày ${r.to} — Quyết định 2855/QĐ-UBND ngày 19/6/2026`], [],
    ["TT", "TÊN CHƯƠNG TRÌNH KHUYẾN NÔNG", "QUY MÔ", `TIẾN ĐỘ THỰC HIỆN TUẦN ${r.number} (đến ngày ${r.to})`, "", `ĐÁNH GIÁ TUẦN ${r.number}`, "LŨY KẾ", "", ...(info ? ["GHI CHÚ"] : [])],
    ["", "", "", "Tiến độ thực hiện", "Kinh phí thực hiện (đồng)", "", "Tiến độ thực hiện", "Kinh phí thực hiện (đồng)"]];
  const merges = [`A1:${last}1`, `A2:${last}2`, "A4:A5", "B4:B5", "C4:C5", "F4:F5", "D4:E4", "G4:H4", ...(info ? ["I4:I5"] : [])];
  for (const row of r.rows) {
    const position = rows.length + 1;
    if (row.group) { rows.push([row.number, row.name]); merges.push(`B${position}:${last}${position}`); continue; }
    const plan = row.adjusted_plan ? `${row.plan} (điều chỉnh ${row.adjusted_plan})` : row.plan;
    const values = kind === "template" ? ["", "", "", "", ""] : [row.progress, row.money, row.evaluation, `${row.cumulative_progress} / ${row.adjusted_plan ?? row.plan}${row.rate === null ? "" : `; đạt ${row.rate}% KH`}`, row.cumulative_money];
    rows.push([row.number, row.parent_id ? `    ${row.name}` : row.name, plan, ...values, ...(info ? [kind === "template" ? "" : [row.note, row.plan_reason].filter(Boolean).join("; ")] : [])]);
  }
  return workbook(rows, merges, `${kind === "template" ? "mau-nhap" : "bao-cao"}-${loaded.program}-${r.key}.xlsx`);
}

// A synchronous SHA-256 is needed only because the shared mock dispatcher is synchronous.
const K = new Uint32Array([0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2]);
export function weeklyDigest(data: Uint8Array): string {
  const padded = new Uint8Array(Math.ceil((data.length + 9) / 64) * 64); padded.set(data); padded[data.length] = 128;
  new DataView(padded.buffer).setUint32(padded.length - 4, data.length * 8, false);
  const h = new Uint32Array([0x6a09e667,0xbb67ae85,0x3c6ef372,0xa54ff53a,0x510e527f,0x9b05688c,0x1f83d9ab,0x5be0cd19]);
  const rotate = (v: number, n: number) => (v >>> n) | (v << (32 - n));
  for (let offset = 0; offset < padded.length; offset += 64) {
    const w = new Uint32Array(64), bytes = new DataView(padded.buffer, offset, 64);
    for (let i = 0; i < 16; i++) w[i] = bytes.getUint32(i * 4, false);
    for (let i = 16; i < 64; i++) { const a = w[i - 15]!, b = w[i - 2]!; w[i] = (w[i - 16]! + (rotate(a,7)^rotate(a,18)^(a>>>3)) + w[i - 7]! + (rotate(b,17)^rotate(b,19)^(b>>>10))) >>> 0; }
    let [a,b,c,d,e,f,g,j] = Array.from(h) as [number,number,number,number,number,number,number,number];
    for (let i = 0; i < 64; i++) { const t1 = (j + (rotate(e,6)^rotate(e,11)^rotate(e,25)) + ((e&f)^(~e&g)) + K[i]! + w[i]!) >>> 0, t2 = ((rotate(a,2)^rotate(a,13)^rotate(a,22)) + ((a&b)^(a&c)^(b&c))) >>> 0; j=g;g=f;f=e;e=(d+t1)>>>0;d=c;c=b;b=a;a=(t1+t2)>>>0; }
    [a,b,c,d,e,f,g,j].forEach((value,index) => {h[index] = (h[index]! + value) >>> 0;});
  }
  return Array.from(h, value => value.toString(16).padStart(8,"0")).join("");
}

function csvRows(text: string): string[][] {
  const rows: string[][] = []; let cells: string[] = [], value = "", quoted = false;
  for (let i = 0; i < text.length; i++) { const c = text[i]!; if (c === '"') { if (quoted && text[i + 1] === '"') {value += '"';i++;} else quoted = !quoted; } else if (c === "," && !quoted) {cells.push(value.trim());value="";} else if ((c === "\n" || c === "\r") && !quoted) {if(c==="\r"&&text[i+1]==="\n")i++;cells.push(value.trim());rows.push(cells);cells=[];value="";} else value += c; }
  if (quoted) throw new Error("Tệp CSV không hợp lệ");
  if (value || cells.length) {cells.push(value.trim());rows.push(cells);} return rows;
}

function multipart(ctx: Ctx): { file: Uint8Array; parts: Record<string,string> } | Response {
  const boundary = /boundary=([^;]+)/.exec(ctx.request.headers.get("Content-Type") ?? "")?.[1]?.replace(/^"|"$/g, "");
  if (!boundary || !(ctx.body instanceof Uint8Array) || ctx.body.length > 5 * 1024 * 1024 + 131072) return invalid("Tệp Excel/CSV tối đa 5 MB");
  let raw = "";for(const b of ctx.body)raw+=String.fromCharCode(b);
  let file: Uint8Array | null = null; const parts: Record<string,string> = {};
  for (const part of raw.split(`--${boundary}`)) {const at=part.indexOf("\r\n\r\n");if(at<0)continue;const head=part.slice(0,at), name=/name="([^"]*)"/.exec(head)?.[1];if(!name)continue;const bytes=Uint8Array.from(part.slice(at+4).replace(/\r\n$/,""),c=>c.charCodeAt(0));if(name==="file")file=bytes;else parts[name]=new TextDecoder().decode(bytes);}
  return file && file.length > 0 && file.length <= 5 * 1024 * 1024 ? {file,parts} : invalid("Cần chọn tệp, tối đa 5 MB");
}

function previewFile(report: WeeklyReport, version: number, program: Program, data: Uint8Array): { preview: WeeklyPreview; patches: WeeklyPatch[] } | Response {
  let rows: string[][];
  if (data[0] === 80 && data[1] === 75) {
    const parsed = readStoredXlsx(data); if (!parsed) return invalid("Chế độ mô phỏng nhận CSV và tệp mẫu tải từ màn hình này; tệp Excel nén được xử lý trên máy chủ thật");
    rows=[];for(const [cell,value] of parsed.cells){const match=/^([A-I])(\d+)$/.exec(cell);if(!match)continue;const at=Number(match[2])-1;if(at>2000)return invalid("Tối đa 2000 dòng");while(rows.length<=at)rows.push([]);rows[at]![match[1]!.charCodeAt(0)-65]=value.formula ? "=FORMULA" : value.text??"";}
  } else {try {rows=csvRows(new TextDecoder("utf-8",{fatal:true}).decode(data));}catch{return invalid("CSV phải là UTF-8 hợp lệ");}}
  if(rows.length>2000||rows.some(r=>r.length>80))return invalid("Tệp vượt giới hạn dòng/cột");
  const header=rows.findIndex(r=>r.some(c=>fold(c??"").includes("ten_hang_muc")||fold(c??"").includes("ten_chuong_trinh")));if(header<0)return invalid("Thiếu cột tên hạng mục");
  const head=rows[header]!.map(c=>fold(c??"")), name=head.findIndex(c=>c.includes("ten_hang_muc")||c.includes("ten_chuong_trinh"));
  let progress=-1,money=-1,evaluation=-1,note=-1,selected=false;
  const isProgress=(value:string)=>value.includes("tien_do")&&value.includes("tuan")&&!value.includes("luy_ke");
  for(let j=0;j<head.length;j++){const h=head[j]??"",match=h.includes(`tuan_${report.number}_`)||h.endsWith(`tuan_${report.number}`);if(isProgress(h)&&(!selected||match)){progress=j;selected=match;}if(h.includes("ghi_chu"))note=j;}
  let periodEnd=head.length;for(let j=progress+1;j<head.length;j++){if(isProgress(head[j]??"")){periodEnd=j;break;}}
  if(progress>=0&&progress+1<periodEnd&&!(head[progress+1]??""))money=progress+1;
  for(let j=progress+1;progress>=0&&j<periodEnd;j++){const h=head[j]??"";if(h.includes("luy_ke"))continue;if(h.includes("kinh_phi"))money=j;if(h.includes("danh_gia"))evaluation=j;}
  if(progress<0||money<0)return invalid("Thiếu cột Tiến độ/Kinh phí tuần cùng kỳ");
  const columnLabel=(col:number)=>{if(col<0)return "không có";let n=col+1,name="";while(n){n--;name=String.fromCharCode(65+n%26)+name;n=Math.floor(n/26);}return `${name} ${(rows[header]![col]??"").replace(/\s+/g," ")}`.trim();};
  const source_columns=`${selected?`Số liệu tuần ${report.number}.`:`Không có cột tiến độ ghi rõ tuần ${report.number}; dùng cặp tuần cuối trong tệp.`} Tiến độ: ${columnLabel(progress)}; kinh phí: ${columnLabel(money)}; đánh giá: ${columnLabel(evaluation)}.`;
  const body=rows.slice(header+1).map((cells,index)=>({cells,line:header+index+2})).filter(({cells})=>!!cells[name]?.trim() && fold(cells[name]!)!==fold(program==="information"?"Chương trình thông tin":"Chương trình đào tạo huấn luyện và hợp tác"));
  const counts=new Map<string,number>();for(const r of body){const n=fold(r.cells[name]!);counts.set(n,(counts.get(n)??0)+1);}
  const preview: WeeklyPreview={version,week_id:report.id,sha256:weeklyDigest(data),source_columns,rows:[],valid_rows:0,overwrite_rows:0,error_rows:0},patches:WeeklyPatch[]=[];
  for(const {cells,line} of body){const label=cells[name]!.trim(),r=report.rows.find(r=>fold(r.name)===fold(label));const errors:string[]=[];let status:"valid"|"overwrite"|"error"="valid";const patch:WeeklyPatch={id:r?.id??""};
    if(!r)errors.push("Tên hạng mục không có trong danh mục");else if((counts.get(fold(label))??0)>1)errors.push("Hạng mục xuất hiện nhiều lần trong tệp");else if(r.group)errors.push("Mục nhóm không nhập số liệu");else{
      for(const [field,col] of [["progress",progress],["money",money],["evaluation",evaluation],["note",note]] as const){const value=cells[col]?.trim();if(!value)continue;if(field==="progress"||field==="money"){const n=weeklyNumber(value,field==="money");if(!n)errors.push("Tiến độ/kinh phí phải là số không âm đúng giới hạn");else patch[field]=n.toString();}else if(Array.from(value).length>2000)errors.push("Đánh giá/ghi chú tối đa 2000 ký tự");else patch[field]=value;}
      if(!errors.length){if((r.progress_set&&patch.progress!==undefined&&patch.progress!==r.progress)||(r.money_set&&patch.money!==undefined&&patch.money!==r.money)||(r.evaluation&&patch.evaluation!==undefined&&patch.evaluation!==r.evaluation)||(r.note&&patch.note!==undefined&&patch.note!==r.note)){status="overwrite";preview.overwrite_rows++;}patches.push(patch);preview.valid_rows++;}
    }
    if(errors.length){status="error";preview.error_rows++;}preview.rows.push({line,name:label,status,errors});
  }
  return {preview,patches};
}

function importFile(ctx: Ctx, confirm: boolean): Response {
  const denied=weeklyAuthorization(ctx.request,ctx.persona);if(denied)return denied;
  const loaded=load(ctx);if(loaded instanceof Response)return loaded;const uploaded=multipart(ctx);if(uploaded instanceof Response)return uploaded;
  const {book,program}=loaded,version=Number(uploaded.parts.version),report=book.reports.find(r=>r.id===uploaded.parts.week_id);
  if(!report)return problem(404,"price.weekly_not_found","Không có kỳ báo cáo");
  if(report.status!=="draft")return problem(409,"price.weekly_closed","Báo cáo đã chốt — chỉ xem");
  if(version!==book.version)return conflict("Báo cáo đã đổi — xem trước lại");
  const parsed=previewFile(project(report,book.deleted),version,program,uploaded.file);if(parsed instanceof Response)return parsed;
  if(!confirm)return json(parsed.preview);
  if(uploaded.parts.sha256!==parsed.preview.sha256)return problem(409,"price.weekly_import_changed","Tệp khác lần xem trước");
  if(parsed.preview.overwrite_rows>0&&uploaded.parts.confirm_overwrite!=="true")return problem(409,"price.weekly_confirmation","Cần xác nhận ghi đè");
  return write(ctx,{version,week_id:report.id,action:"save",rows:parsed.patches},parsed.preview.rows);
}

export function resetWeeklyPrograms() { books.clear(); }

export const priceWeeklyRoutes: Route[] = [
  {method:"GET",pattern:/^\/v1\/price\/weekly-programs\/([^/]+)$/,handler:ctx=>{const k=knContext(ctx.request.headers.get("X-AG-Context")??"");if(k?.role==="kn_xa"||k?.role==="kn_tw")return json({program:"",title:"",plan_year:0,version:0,reports:[],report:null,log:[]});const denied=weeklyAuthorization(ctx.request,ctx.persona);if(denied)return denied;const loaded=load(ctx);if(loaded instanceof Response)return loaded;const v=view(loaded.program,loaded.book,ctx.url.searchParams.get("week_id")??undefined);return v instanceof Response?v:json(v);}},
  {method:"POST",pattern:/^\/v1\/price\/weekly-programs\/([^/]+)\/actions$/,handler:ctx=>write(ctx,(ctx.body??{}) as WeeklyCommand)},
  ...(["export","template"] as const).map(kind=>({method:"GET",pattern:new RegExp(`^/v1/price/weekly-programs/([^/]+)/${kind}$`),handler:(ctx:Ctx)=>exportFile(ctx,kind)})),
  {method:"GET",pattern:/^\/v1\/price\/weekly-programs\/([^/]+)\/imports\/issues$/,handler:ctx=>exportFile(ctx,"issues")},
  {method:"POST",pattern:/^\/v1\/price\/weekly-programs\/([^/]+)\/imports\/preview$/,handler:ctx=>importFile(ctx,false)},
  {method:"POST",pattern:/^\/v1\/price\/weekly-programs\/([^/]+)\/imports\/confirm$/,handler:ctx=>importFile(ctx,true)},
];
