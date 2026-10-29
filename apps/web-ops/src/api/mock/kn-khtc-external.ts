// GIẢ LẬP W11.67 — Số liệu nguồn ngoài KHTC (FN-TI-02.11). Dữ liệu chỉ ở bộ nhớ trình duyệt; kỳ lấy từ kho kỳ KHTC giả lập.
// Quy tắc như máy chủ: nguồn bắt buộc, trùng chỉ tiêu × kỳ → hỏi ghi đè, sửa cần lý do ≥ 10 ký tự, kỳ đã đóng/hủy → chỉ xem,
// nạp tệp hai bước (kiểm → Hoàn tất). Tệp .xlsx không đọc được ở giả lập: dùng bộ dòng mẫu tuần 39 của prototype.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { khtcMockStore, type Stored } from "./kn-khtc-periods";
type S = components["schemas"];
type Indicator = S["KHTCExternalIndicator"];
type Figure = S["KHTCExternalFigure"];
type Entry = { id: string; seq: number; period: string; indicator: string; action: "set" | "withdraw"; value: string | null; unit: string; agency: string; document: string; date: string; evidence: string[]; reason: string; import_id: string | null; prev: string | null; at: string; actor: string };
type Imp = S["KHTCExternalImport"] & { raw: S["KHTCExternalImportRow"][] };
const KHTC = "ke_hoach_tai_chinh";
const INDICATORS: Indicator[] = [];
const ENTRIES: Entry[] = [];
const IMPORTS = new Map<string, Imp>();
const EVIDENCE = new Map<string, S["KHTCExternalEvidence"]>();
const err = (status: number, name: string, title: string, extra: Record<string, unknown> = {}) => problem(status, `extension.khtc_ext_${name}`, title, extra);
const now = () => new Date().toISOString();
let seeded = false;
function seed() {
    if (seeded)
        return;
    seeded = true;
    for (const [code, name, unit, group] of [["ng-gtsx", "Giá trị sản xuất nông nghiệp lũy kế", "tỷ đồng", "Kinh tế"], ["ng-tom-nl", "Diện tích nuôi tôm nước lợ", "ha", "Thủy sản"], ["ng-ho-giong", "Số hộ tham gia chương trình hỗ trợ giống", "hộ", "Chương trình"], ["ng-kp-kn", "Kinh phí giải ngân CT khuyến nông", "triệu đồng", "Tài chính"], ["ng-lua-td", "Diện tích lúa Thu Đông", "ha", "Trồng trọt"], ["ng-tom-cx", "Số hộ nuôi tôm càng xanh", "hộ", "Thủy sản"], ["ng-htx-lua", "Số HTX tham gia chuỗi lúa CLC", "HTX", "Kinh tế hợp tác"], ["ng-xk-gao", "Giá trị xuất khẩu gạo", "triệu USD", "Kinh tế"]] as const)
        INDICATORS.push({ id: uuidv7(), code, name, unit, group, created_at: now() });
    const week = khtcMockStore().find(s => s.p.report_type === "week" && s.p.year === new Date().getFullYear() && s.terminal !== "cancelled");
    if (!week)
        return;
    const ev = { id: uuidv7(), file_name: "email-ChiCuc-29-9.png", content_type: "image/png", size: 48213, sha256: "0".repeat(64), created_at: now() };
    EVIDENCE.set(ev.id, ev);
    const first = put(week, INDICATORS[1]!, "26100", "ha", "Chi cục Thủy sản", "Email 29/9", "2026-09-29", [ev.id], "", null, "00000000-0000-7000-8000-000000000001");
    put(week, INDICATORS[1]!, "26400", "ha", "Chi cục Thủy sản", "Email 30/9", "2026-09-30", [ev.id], "Chi cục cập nhật số liệu", null, first.actor);
    put(week, INDICATORS[0]!, "48215", "tỷ đồng", "Cục Thống kê An Giang", "285/BC-CTK", "2026-09-28", [], "", null, first.actor);
}
function latest(period: string, indicator: string) {
    return ENTRIES.filter(e => e.period === period && e.indicator === indicator).at(-1);
}
function put(s: Stored, ind: Indicator, value: string | null, unit: string, agency: string, document: string, date: string, evidence: string[], reason: string, importId: string | null, actor: string, action: "set" | "withdraw" = "set"): Entry {
    const cur = latest(s.p.id, ind.id);
    const e: Entry = { id: uuidv7(), seq: (cur?.seq ?? 0) + 1, period: s.p.id, indicator: ind.id, action, value, unit: unit || ind.unit, agency, document, date, evidence, reason, import_id: importId, prev: cur?.id ?? null, at: now(), actor };
    ENTRIES.push(e);
    return e;
}
const shortName = (name: string) => name.replace(/\s*\(.*\)\s*$/, "").trim();
const label = (s: string) => shortName(s).toLowerCase().replace(/\s+/g, " ").replace(/^(.*\S)\s*\/\s*(\d{4})$/, "$1 năm $2").replace(/\b0+(\d)/g, "$1").trim();
const used = (t: string) => ({ week: "Mẫu 1 — Báo cáo tuần", month: "Mẫu 2 — Báo cáo tháng", quarter: "Mẫu 3 — Báo cáo quý", year: "Mẫu 4 — Báo cáo năm", program: "Mẫu 5 — Tiến độ chương trình", adhoc: "Báo cáo phát sinh" } as Record<string, string>)[t] ?? "";
function view(e: Entry, writable: boolean): Figure | null {
    const s = khtcMockStore().find(x => x.p.id === e.period), ind = INDICATORS.find(i => i.id === e.indicator);
    if (!s || !ind || e.action !== "set" || e.value === null)
        return null;
    const closed = s.terminal !== "";
    return { entry_id: e.id, version: e.seq, period_id: s.p.id, period_name: shortName(s.p.name), period_type: s.p.report_type, year: s.p.year, closed, indicator_id: ind.id, code: ind.code, name: ind.name, group: ind.group, value: e.value, unit: e.unit, agency: e.agency, document: e.document, source_date: e.date, evidence: e.evidence.map(id => EVIDENCE.get(id)).filter((x): x is S["KHTCExternalEvidence"] => !!x).map(x => ({ id: x.id, file_name: x.file_name, content_type: x.content_type, size: x.size })), used_in: used(s.p.report_type), actor_ref: e.actor, actor_name: "Cán bộ KHTC (giả lập)", at: e.at, editable: writable && !closed };
}
function current(periodIds?: Set<string>): Entry[] {
    const seen = new Map<string, Entry>();
    for (const e of ENTRIES)
        if (!periodIds || periodIds.has(e.period))
            seen.set(`${e.period}:${e.indicator}`, e);
    return [...seen.values()];
}
/** Ô Nhập ngoài của một kỳ cho bản soạn giả lập (kn-khtc-reports.ts). */
export function khtcExternalForPeriod(periodId: string): S["KHTCReportExternalFigure"][] {
    seed();
    return current(new Set([periodId])).map(e => view(e, false)).filter((x): x is Figure => !!x).sort((a, b) => a.name.localeCompare(b.name, "vi")).map(f => ({ entry_id: f.entry_id, version: f.version, indicator_id: f.indicator_id, code: f.code, name: f.name, group: f.group, value: f.value, unit: f.unit, agency: f.agency, document: f.document, source_date: f.source_date, evidence: f.evidence.map(x => ({ id: x.id, file_name: x.file_name })), at: f.at }));
}
function parseValue(raw: string): string | null {
    let s = raw.replace(/[\s ]/g, "");
    if (!s)
        return null;
    if (s.includes(","))
        s = s.replaceAll(".", "").replace(",", ".");
    else if ((s.match(/\./g) ?? []).length > 1)
        s = s.replaceAll(".", "");
    if (!/^-?\d+(\.\d{1,4})?$/.test(s))
        return null;
    return s.replace(/^(-?)0+(\d)/, "$1$2").replace(/(\.\d*?)0+$/, "$1").replace(/\.$/, "");
}
function parseDate(raw: string): string | null {
    const s = raw.trim();
    if (/^\d{4}-\d{2}-\d{2}$/.test(s))
        return s;
    const m = /^(\d{1,2})\/(\d{1,2})\/(\d{4})$/.exec(s);
    return m ? `${m[3]}-${m[2]!.padStart(2, "0")}-${m[1]!.padStart(2, "0")}` : null;
}
function check(year: number, raw: S["KHTCExternalImportRow"][]): Imp["result"] {
    const periods = khtcMockStore().filter(s => s.p.year === year && s.terminal !== "cancelled");
    const rows: S["KHTCExternalImportRow"][] = [];
    const seen = new Map<string, number>();
    let valid = 0, overwrites = 0, errors = 0;
    for (const r0 of raw) {
        const r: S["KHTCExternalImportRow"] = { ...r0, errors: [], indicator_id: null, indicator_name: "", period_id: null, period_name: "", normalized_value: "", current_value: null, status: "ok" };
        const ind = INDICATORS.find(i => i.code === r.indicator.trim().toLowerCase() || i.name.toLowerCase() === r.indicator.trim().toLowerCase());
        if (!r.indicator.trim())
            r.errors.push("thiếu chỉ tiêu");
        else if (!ind)
            r.errors.push(`chỉ tiêu không có trong danh mục: ${r.indicator}`);
        else {
            r.indicator_id = ind.id;
            r.indicator_name = ind.name;
            r.unit ||= ind.unit;
        }
        const cand = periods.filter(s => label(s.p.name) === label(r.period) || (s.p.period_key ?? "").toLowerCase() === r.period.trim().toLowerCase());
        if (!r.period.trim())
            r.errors.push("thiếu kỳ");
        else if (!cand.length)
            r.errors.push(`kỳ không có hoặc chưa mở: ${r.period}`);
        else if (cand[0]!.terminal)
            r.errors.push(`kỳ đã đóng — chỉ xem: ${shortName(cand[0]!.p.name)}`);
        else {
            r.period_id = cand[0]!.p.id;
            r.period_name = shortName(cand[0]!.p.name);
        }
        const v = parseValue(r.value);
        if (v === null)
            r.errors.push("giá trị: không phải số");
        else
            r.normalized_value = v;
        if (!r.agency.trim())
            r.errors.push("thiếu nguồn: cơ quan cung cấp");
        if (!r.document.trim())
            r.errors.push("thiếu nguồn: văn bản hoặc kênh");
        if (!r.source_date.trim())
            r.errors.push("thiếu nguồn: ngày");
        else if (!parseDate(r.source_date))
            r.errors.push("ngày YYYY-MM-DD hoặc DD/MM/YYYY");
        if (r.indicator_id && r.period_id) {
            const k = `${r.period_id}:${r.indicator_id}`;
            if (seen.has(k))
                r.errors.push(`trùng chỉ tiêu và kỳ với dòng ${seen.get(k)}`);
            else
                seen.set(k, r.line);
            const cur = latest(r.period_id, r.indicator_id);
            if (cur?.action === "set")
                r.current_value = cur.value;
        }
        r.status = r.errors.length ? "error" : r.current_value !== null ? "overwrite" : "ok";
        if (r.status === "error")
            errors++;
        else if (r.status === "overwrite")
            overwrites++;
        else
            valid++;
        rows.push(r);
    }
    return { rows, valid, overwrites, errors };
}
const blank = (line: number, cells: string[]): S["KHTCExternalImportRow"] => ({ line, indicator: cells[0] ?? "", period: cells[1] ?? "", value: cells[2] ?? "", unit: cells[3] ?? "", agency: cells[4] ?? "", document: cells[5] ?? "", source_date: cells[6] ?? "", status: "ok", errors: [], indicator_id: null, indicator_name: "", period_id: null, period_name: "", normalized_value: "", current_value: null });
function rowsOf(body: unknown, year: number): S["KHTCExternalImportRow"][] {
    if (typeof body !== "string") {
        // Bộ dòng mẫu tuần 39 của prototype (4 chỉ tiêu, 1 dòng thiếu nguồn).
        const week = khtcMockStore().find(s => s.p.year === year && s.p.report_type === "week" && !s.terminal);
        const k = week ? shortName(week.p.name) : `Tuần 39/${year}`;
        return [["Diện tích lúa Thu Đông", k, "12450", "ha", "Cục Thống kê", "File Excel tổng hợp tuần 39", "2026-09-29"], ["Số hộ nuôi tôm càng xanh", k, "812", "hộ", "Chi cục Thủy sản", "File Excel tổng hợp tuần 39", "2026-09-29"], ["Số HTX tham gia chuỗi lúa CLC", k, "34", "HTX", "Sở NN&MT", "File Excel tổng hợp tuần 39", "2026-09-29"], ["Giá trị xuất khẩu gạo", k, "210", "triệu USD", "", "", ""]].map((c, i) => blank(i + 2, c));
    }
    const lines = body.replace(/^﻿/, "").split(/\r?\n/).filter(l => l.trim());
    const head = (lines[0] ?? "").split(",").map(h => h.trim().toLowerCase());
    const col = (names: string[]) => head.findIndex(h => names.some(n => h.startsWith(n)));
    const idx = [col(["mã chỉ tiêu", "chỉ tiêu"]), col(["kỳ"]), col(["giá trị"]), col(["đvt"]), col(["cơ quan", "nguồn"]), col(["văn bản", "kênh"]), col(["ngày"])];
    return lines.slice(1).map((l, i) => { const cells = l.split(","); return blank(i + 2, idx.map(j => (j >= 0 ? cells[j] ?? "" : "").trim())); });
}
export const khtcExternalRoutes: Route[] = [
    { method: "GET", pattern: /^\/v1\/extension\/khtc\/external\/(indicators|figures|history|template|imports\/([^/]+)|evidence\/([^/]+))$/, handler: handle },
    { method: "POST", pattern: /^\/v1\/extension\/khtc\/external\/(indicators|figures|figures\/withdraw|imports|imports\/([^/]+)\/commit|evidence)$/, handler: handle },
];
function handle(ctx: Ctx): Response {
    seed();
    const [op = "", importId, evidenceId] = ctx.params;
    const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
    if (kc && kc.role !== "admin" && !(kc.department_code === KHTC && (kc.role === "kn_tinh" || kc.role === "kn_tw")))
        return problem(403, "forbidden", "Số liệu nguồn ngoài dành cho Phòng Kế hoạch – Tài chính");
    const writable = !kc || (kc.role === "kn_tinh" && kc.department_code === KHTC && !kc.read_only);
    const q = ctx.url.searchParams, year = Number(q.get("year") ?? new Date().getFullYear()), actor = ctx.persona.me.actor_ref;
    if (ctx.request.method === "GET") {
        if (op === "indicators")
            return json({ items: [...INDICATORS].sort((a, b) => a.name.localeCompare(b.name, "vi")) });
        const ids = new Set(khtcMockStore().filter(s => s.p.year === year && (!q.get("period_id") || s.p.id === q.get("period_id"))).map(s => s.p.id));
        if (op === "figures") {
            const agency = (q.get("agency") ?? "").toLowerCase(), search = (q.get("q") ?? "").toLowerCase();
            return json({ items: current(ids).map(e => view(e, writable)).filter((f): f is Figure => !!f && (!agency || f.agency.toLowerCase().includes(agency)) && (!search || f.name.toLowerCase().includes(search) || f.code.includes(search))) });
        }
        if (op === "history")
            return json({ items: ENTRIES.filter(e => ids.has(e.period)).reverse().map(e => { const s = khtcMockStore().find(x => x.p.id === e.period)!; const prev = ENTRIES.find(x => x.id === e.prev); return { entry_id: e.id, version: e.seq, action: e.action, period_id: e.period, period_name: shortName(s.p.name), indicator_id: e.indicator, indicator_name: INDICATORS.find(i => i.id === e.indicator)?.name ?? "", old_value: prev?.value ?? null, new_value: e.value, unit: e.unit, reason: e.reason, import_id: e.import_id, actor_ref: e.actor, actor_name: "Cán bộ KHTC (giả lập)", at: e.at }; }) });
        if (op === "template")
            return new Response("﻿Chỉ tiêu,Kỳ,Giá trị,ĐVT,Cơ quan cung cấp,Văn bản / kênh,Ngày (YYYY-MM-DD)\r\n", { headers: { "Content-Type": "text/csv; charset=utf-8", "Content-Disposition": `attachment; filename="gia-lap-mau-so-lieu-nguon-ngoai-${year}.csv"` } });
        if (importId) {
            const imp = IMPORTS.get(importId);
            return imp ? json(imp) : err(404, "not_found", "Không có lần nạp");
        }
        const ev = evidenceId ? EVIDENCE.get(evidenceId) : undefined;
        return ev ? new Response(`GIẢ LẬP minh chứng ${ev.file_name}`, { headers: { "Content-Type": "text/plain; charset=utf-8", "Content-Disposition": `attachment; filename="gia-lap-${encodeURIComponent(ev.file_name)}.txt"` } }) : err(404, "not_found", "Không có tệp");
    }
    if (!writable)
        return problem(403, "forbidden", "Chỉ cán bộ Cấp Tỉnh Phòng KHTC nhập số liệu nguồn ngoài");
    const body = (ctx.body ?? {}) as Record<string, unknown>;
    if (op === "indicators") {
        const name = String(body.name ?? "").trim().replace(/\s+/g, " ");
        if (!name)
            return err(422, "invalid", "Tên chỉ tiêu bắt buộc");
        if (INDICATORS.some(i => i.name.toLowerCase() === name.toLowerCase() || (body.code && i.code === body.code)))
            return err(409, "indicator_duplicate", "Mã hoặc tên chỉ tiêu đã có trong danh mục");
        const ind: Indicator = { id: uuidv7(), code: String(body.code || `ng-${uuidv7().slice(-10)}`), name, unit: String(body.unit ?? ""), group: String(body.group ?? ""), created_at: now() };
        INDICATORS.push(ind);
        return json(ind, 201);
    }
    if (op === "evidence") {
        const name = q.get("file_name") ?? "";
        if (!/\.(pdf|png|jpe?g|docx?|xlsx?|eml|txt)$/i.test(name))
            return err(422, "evidence_invalid", "Tệp minh chứng không hợp lệ", { detail: "chỉ nhận PDF, ảnh JPEG/PNG, Word, Excel, email (.eml) hoặc văn bản" });
        const size = ctx.body instanceof Uint8Array ? ctx.body.length : String(ctx.body ?? "").length;
        const ev = { id: uuidv7(), file_name: name, content_type: "application/octet-stream", size, sha256: "0".repeat(64), created_at: now() };
        EVIDENCE.set(ev.id, ev);
        return json(ev, 201);
    }
    if (op === "figures" || op === "figures/withdraw") {
        const s = khtcMockStore().find(x => x.p.id === body.period_id), ind = INDICATORS.find(i => i.id === body.indicator_id);
        if (!s || !ind)
            return err(404, "not_found", "Không có kỳ hoặc chỉ tiêu");
        if (s.terminal)
            return err(409, "period_closed", "Kỳ đã đóng hoặc đã hủy — số liệu nguồn ngoài chỉ xem");
        const cur = latest(s.p.id, ind.id), reason = String(body.reason ?? "").trim();
        if (op === "figures/withdraw") {
            if (!cur || cur.action !== "set")
                return err(404, "not_found", "Không có số liệu để rút");
            if (cur.seq !== body.base_version)
                return err(409, "conflict", "Số liệu vừa được người khác thay đổi");
            if (reason.length < 10)
                return err(422, "invalid", "Lý do 10–1000 ký tự", { errors: [{ field: "reason", reason: "lý do 10–1000 ký tự" }] });
            put(s, ind, null, cur.unit, "", "", "", [], reason, null, actor, "withdraw");
            return new Response(null, { status: 204 });
        }
        const value = parseValue(String(body.value ?? "")), date = parseDate(String(body.source_date ?? ""));
        const errors = [...(value === null ? [{ field: "value", reason: "không phải số" }] : []), ...(!String(body.agency ?? "").trim() ? [{ field: "agency", reason: "thiếu nguồn: cơ quan cung cấp" }] : []), ...(!String(body.document ?? "").trim() ? [{ field: "document", reason: "thiếu nguồn: văn bản hoặc kênh" }] : []), ...(!date ? [{ field: "source_date", reason: "thiếu nguồn: ngày" }] : [])];
        if (errors.length)
            return err(422, "invalid", "Số liệu nguồn ngoài không hợp lệ", { errors });
        const base = body.base_version as number | null | undefined;
        if (cur) {
            if (base != null && base !== cur.seq)
                return err(409, "conflict", "Số liệu vừa được người khác thay đổi, tải lại trước khi sửa");
            if (cur.action === "set" && base == null && !body.overwrite)
                return err(409, "duplicate", "Chỉ tiêu đã có giá trị trong kỳ: xác nhận ghi đè và nhập lý do", { detail: `Giá trị hiện có: ${cur.value} ${cur.unit}`, current_version: cur.seq });
            if (reason.length < 10)
                return err(422, "invalid", "Số liệu nguồn ngoài không hợp lệ", { errors: [{ field: "reason", reason: "sửa hoặc ghi đè bắt buộc lý do (tối thiểu 10 ký tự)" }] });
        }
        const e = put(s, ind, value, String(body.unit ?? ""), String(body.agency), String(body.document), date!, (body.evidence_ids as string[] | undefined) ?? [], cur ? reason : "", null, actor);
        return json(view(e, true), 201);
    }
    if (op === "imports") {
        const raw = rowsOf(ctx.body, year);
        const imp: Imp = { import_id: uuidv7(), year, file_name: q.get("file_name") || "so-lieu-nguon-ngoai.xlsx", status: "staged", result: check(year, raw), recorded: 0, skipped: 0, overwrite: false, raw };
        IMPORTS.set(imp.import_id, imp);
        return json(imp, 201);
    }
    const imp = importId ? IMPORTS.get(importId) : undefined;
    if (!imp)
        return err(404, "not_found", "Không có lần nạp");
    if (imp.status === "committed")
        return err(409, "import_committed", "Lần nạp đã hoàn tất");
    const overwrite = !!body.overwrite, reason = String(body.reason ?? "").trim();
    if (overwrite && reason.length < 10)
        return err(422, "invalid", "Ghi đè bắt buộc lý do 10–1000 ký tự", { errors: [{ field: "reason", reason: "ghi đè bắt buộc lý do" }] });
    const result = check(imp.year, imp.raw);
    let recorded = 0, skipped = 0;
    for (const r of result.rows) {
        if (r.status === "error")
            continue;
        if (r.status === "overwrite" && !overwrite) {
            skipped++;
            continue;
        }
        const s = khtcMockStore().find(x => x.p.id === r.period_id)!, ind = INDICATORS.find(i => i.id === r.indicator_id)!;
        put(s, ind, r.normalized_value, r.unit, r.agency, r.document, parseDate(r.source_date)!, [], r.status === "overwrite" ? reason : "", imp.import_id, actor);
        recorded++;
    }
    Object.assign(imp, { status: "committed", result, recorded, skipped, overwrite });
    return json(imp, 201);
}
