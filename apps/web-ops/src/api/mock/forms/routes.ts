// ============================================================================
// BACKEND GIẢ LẬP mẫu báo cáo dạng lưới KN v7.7 (FN-AD-04.06) — đúng hợp đồng admin.yaml / application.yaml
// mục pf/reports: /v1/reports/forms*, /form-versions/{id}/evaluate, /form-assignments*, /forms/available,
// /layout, /template.xlsx. Quy tắc như reports/app/forms.go: nháp → đề nghị (khóa) → phát hành bởi NGƯỜI
// KHÁC người đề nghị và người sửa cuối (R-56, 403 self_approval_forbidden) → bất biến → ngừng dùng; mẫu
// chưa gán loại báo cáo không đề nghị/phát hành được; mỗi mẫu một phiên bản đang soạn.
// Dữ liệu khởi đầu (cố định): T01 và T04 lấy NGUYÊN định nghĩa seed của máy chủ (fixtures/seed.json, sinh
// từ reports/form.TrongTrotSeeds) đã phát hành v1 "Tuần 37/2026"; CC01 (Cấp Tỉnh, Báo cáo gửi Chi cục)
// là nháp như Hình 27. "Khởi tạo 14 mẫu" thêm 12 mẫu còn lại thành nháp v1 + gán loại báo cáo.
// ============================================================================
import { uuidv7 } from "../../idempotency";
import { hasRole, isZip, json, problem, reasonOf, type Ctx, type Route } from "../http";
import { KN_ACCOUNTS } from "../kn-accounts";
import { MOCK_PERSONAS, type MockPersona } from "../personas";
import type { FormAssignment, FormDefinition, FormEvalInput, FormImportReport, FormLogEntry, FormOp, FormReportType, FormTemplate, FormVersion } from "@/kn/forms/types";
import { columnLetter } from "@/kn/grid/letters";
import { ValidationError, validate, validCode } from "./compile";
import { evaluateGrid } from "./evaluate";
import seedJson from "./fixtures/seed.json";
import { layout } from "./layout";
import { applyOps, cloneDef, OpError } from "./ops";
import { exportXlsx, readStoredXlsx } from "./xlsx";

interface SeedRow {
  code: string;
  name: string;
  entry_level: "commune" | "province";
  report_type: FormReportType;
  definition: FormDefinition;
}
export const FORM_SEEDS = seedJson as unknown as SeedRow[];

const ADMIN_ROLES = ["reports_admin", "platform_admin"];
const READER_ROLES = ["kn_officer", "kn_province_admin", "kn_national_admin", "reports_admin", "platform_admin", "kn_user"];
const REPORT_TYPES: FormReportType[] = ["weekly", "monthly", "seasonal", "annual", "adhoc", "dispatch"];

type StoredVersion = FormVersion & { definition: FormDefinition };

interface FormsState {
  templates: FormTemplate[];
  versions: Map<string, StoredVersion[]>; // template id → phiên bản (v tăng dần)
  assignments: FormAssignment[];
  log: Map<string, FormLogEntry[]>; // mới nhất cuối
  seq: number;
}

// Tác nhân cố định của dữ liệu khởi đầu (tài khoản KN giả lập).
const SEEDER = "mock-kn-an";
const PUBLISHER = "mock-kn-admin";
const T0 = "2026-09-01T02:00:00.000Z";
const T1 = "2026-09-05T03:30:00.000Z";

function actorName(ref: string): string | null {
  const kn = KN_ACCOUNTS.find((a) => `mock-${a.id}` === ref);
  if (kn) return kn.displayName;
  return MOCK_PERSONAS.find((p) => p.me.actor_ref === ref)?.displayName ?? null;
}

function sha(def: FormDefinition): string {
  const s = JSON.stringify(def);
  let out = "";
  for (let k = 0; k < 8; k++) {
    let h = 2166136261 ^ k;
    for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619) >>> 0;
    out += h.toString(16).padStart(8, "0");
  }
  return out;
}

function blankDefinition(code: string, title: string): FormDefinition {
  return {
    schema: "form-template.v1",
    code,
    title,
    sheet_name: "",
    requirements: "",
    completeness: "complete",
    columns: [
      { letter: "A", title: "STT", kind: "fixed", dtype: "integer", fixed_source: "row_no" },
      { letter: "B", title: "Khu vực", kind: "fixed", dtype: "text", fixed_source: "region_name" },
      { letter: "C", title: "Xã, phường", kind: "fixed", dtype: "text", fixed_source: "commune_name" },
      { letter: "D", title: "Chỉ tiêu 1", kind: "input", dtype: "number" },
    ],
    header: {
      rows: 1,
      merges: [],
      cells: [
        { row: 1, letter: "A", text: "STT" },
        { row: 1, letter: "B", text: "Khu vực" },
        { row: 1, letter: "C", text: "Xã, phường" },
        { row: 1, letter: "D", text: "Chỉ tiêu 1" },
      ],
    },
    rows: { kind: "per_commune", indicators: [] },
    totals: [
      { kind: "region", label: "Cộng {region}", label_letter: "B" },
      { kind: "province", label: "Toàn tỉnh", label_letter: "B" },
    ],
    period_blocks: [],
    groups: [],
    rules: [],
    pending: [],
    freeze_through: "",
    source: { kind: "designer" },
  };
}

function newVersion(tpl: FormTemplate, n: number, def: FormDefinition, origin: FormVersion["origin"], actor: string, at: string, extra: Partial<FormVersion> = {}): StoredVersion {
  return {
    id: uuidv7(Date.parse(at)),
    template_id: tpl.id,
    version: n,
    status: "draft",
    effective_from: null,
    effective_label: null,
    origin,
    origin_version_id: null,
    import_id: null,
    definition_sha256: sha(def),
    completeness: def.completeness,
    pending_count: def.pending.length,
    column_count: def.columns.length,
    created_by: actor,
    created_at: at,
    updated_by: actor,
    updated_at: at,
    proposed_by: null,
    proposed_at: null,
    published_by: null,
    published_at: null,
    retired_by: null,
    retired_at: null,
    retire_reason: null,
    definition: def,
    ...extra,
  };
}

function initialState(): FormsState {
  const st: FormsState = { templates: [], versions: new Map(), assignments: [], log: new Map(), seq: 0 };
  const mk = (id: string, code: string, name: string, level: FormTemplate["entry_level"], def: FormDefinition, rt: FormReportType, published: boolean) => {
    const tpl: FormTemplate = { id, code, name, department_code: "trong_trot", entry_level: level, status: "active", retired_reason: null, created_by: SEEDER, created_at: T0, updated_at: T1 };
    st.templates.push(tpl);
    const v = newVersion(tpl, 1, def, code === "CC01" ? "blank" : "seed", SEEDER, T0, { id: id.replace("-f001-", "-f0a1-") });
    if (published) {
      Object.assign(v, {
        status: "published",
        effective_from: "2026-09-07",
        effective_label: "Tuần 37/2026",
        proposed_by: SEEDER,
        proposed_at: T0,
        published_by: PUBLISHER,
        published_at: T1,
      } satisfies Partial<FormVersion>);
    }
    st.versions.set(id, [v]);
    st.assignments.push({ id: id.replace("-f001-", "-f0b1-"), template_id: id, department_code: "trong_trot", entry_level: level, report_type: rt, status: "active", assigned_by: SEEDER, assigned_at: T0, revoked_by: null, revoked_at: null, revoke_reason: null });
    const ev = (event: FormLogEntry["event"], actor: string, at: string, changes: FormLogEntry["changes"], withV = true): FormLogEntry => ({
      id: uuidv7(Date.parse(at)),
      event,
      actor_ref: actor,
      actor_display: actorName(actor),
      at,
      version_id: withV ? v.id : null,
      version: withV ? 1 : null,
      changes,
      reason: null,
      seq: ++st.seq,
    });
    const log = [ev(code === "CC01" ? "template_created" : "seeded", SEEDER, T0, [{ field: "code", old: null, new: code }], code !== "CC01"), ev("assigned", SEEDER, T0, [{ field: "report_type", old: null, new: rt }], false)];
    if (published) {
      log.push(ev("proposed", SEEDER, T0, [{ field: "status", old: "draft", new: "proposed" }, { field: "effective_from", old: null, new: "2026-09-07" }]));
      log.push(ev("published", PUBLISHER, T1, [{ field: "status", old: "proposed", new: "published" }]));
    }
    st.log.set(id, log);
  };
  const seed = (code: string) => {
    const s = FORM_SEEDS.find((x) => x.code === code)!;
    return { name: s.name, def: cloneDef(s.definition), rt: s.report_type };
  };
  const t01 = seed("T01");
  const t04 = seed("T04");
  mk("01927b3e-f001-7000-8000-000000000001", "T01", t01.name, "commune", t01.def, t01.rt, true);
  mk("01927b3e-f001-7000-8000-000000000004", "T04", t04.name, "commune", t04.def, t04.rt, true);
  const cc = blankDefinition("CC01", "BÁO CÁO GỬI CHI CỤC (mẫu mới)");
  cc.columns[3]!.title = "Diện tích xuống giống lúa";
  cc.columns[3]!.unit = "ha";
  cc.header.cells[3]!.text = "Diện tích xuống giống lúa (ha)";
  mk("01927b3e-f001-7000-8000-0000000000cc", "CC01", "Báo cáo gửi Chi cục (mẫu mới)", "province", cc, "dispatch", false);
  return st;
}

let S: FormsState = initialState();

/** Cho kiểm thử: về dữ liệu khởi đầu. */
export function resetMockForms(): void {
  S = initialState();
}

// ---------------------------------------------------------------------------
// Tiện ích
// ---------------------------------------------------------------------------

const now = () => new Date().toISOString();
const UUID = "([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})";

function isAdmin(p: MockPersona): boolean {
  return hasRole(p, ...ADMIN_ROLES);
}

function strip(v: StoredVersion, withDef: boolean): FormVersion {
  const { definition, ...rest } = v;
  return withDef ? { ...rest, definition: cloneDef(definition) } : rest;
}

function tplOf(id: string | undefined): FormTemplate | undefined {
  return S.templates.find((t) => t.id === id);
}

function versionsOf(id: string): StoredVersion[] {
  return S.versions.get(id) ?? [];
}

function openVersion(id: string): StoredVersion | undefined {
  return versionsOf(id).find((v) => v.status === "draft" || v.status === "proposed");
}

function activeAssignments(tplId: string): FormAssignment[] {
  return S.assignments.filter((a) => a.template_id === tplId && a.status === "active");
}

function logEvent(tplId: string, actor: string, event: FormLogEntry["event"], changes: FormLogEntry["changes"], v?: StoredVersion, reason: string | null = null): void {
  const list = S.log.get(tplId) ?? [];
  list.push({ id: uuidv7(), event, actor_ref: actor, actor_display: actorName(actor), at: now(), version_id: v?.id ?? null, version: v?.version ?? null, changes, reason, seq: ++S.seq });
  S.log.set(tplId, list);
}

const forbidden = () => problem(403, "forbidden", "Forbidden");
const notFound = () => problem(404, "reports.form_not_found", "Không tìm thấy mẫu báo cáo");
const versionNotFound = () => problem(404, "reports.form_version_not_found", "Không tìm thấy phiên bản mẫu");
const fieldProblem = (status: number, code: string, title: string, errors: { field: string; reason: string }[], detail?: string) =>
  problem(status, code, title, { errors, ...(detail ? { detail } : {}) });

function defProblem(e: unknown): Response {
  if (e instanceof ValidationError) {
    return fieldProblem(422, e.code, "Định nghĩa mẫu không hợp lệ", e.issues.slice(0, 50).map((i) => ({ field: `definition.${i.field}`, reason: i.msg })));
  }
  if (e instanceof OpError) return fieldProblem(422, e.code, "Thao tác lưới không hợp lệ", [{ field: `ops[${e.index}]`, reason: e.msg }]);
  throw e;
}

function reason(body: unknown, required: boolean): string | null | Response {
  const r = reasonOf(body);
  if (r === "" && !required) return null;
  const n = Array.from(r).length;
  if (n < 10 || n > 500) return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "reason", reason: "bắt buộc, 10–500 ký tự" }]);
  return r;
}

const deptOK = (s: unknown): s is string => typeof s === "string" && /^[a-z][a-z0-9_]{1,63}$/.test(s);

function loadVersion(ctx: Ctx): { tpl: FormTemplate; v: StoredVersion } | Response {
  const tpl = tplOf(ctx.params[0]);
  if (!tpl) return notFound();
  const n = Number(ctx.params[1]);
  const v = versionsOf(tpl.id).find((x) => x.version === n);
  if (!v) return versionNotFound();
  return { tpl, v };
}

function summary(tpl: FormTemplate) {
  const vs = versionsOf(tpl.id);
  const open = vs.find((v) => v.status === "draft" || v.status === "proposed");
  let pub: StoredVersion | undefined;
  for (const v of vs) if (v.status === "published" && (!pub || (v.effective_from ?? "") > (pub.effective_from ?? ""))) pub = v;
  return {
    template: tpl,
    open_version: open ? strip(open, false) : null,
    published_version: pub ? strip(pub, false) : null,
    report_types: activeAssignments(tpl.id).map((a) => a.report_type),
  };
}

function checkPublishable(tpl: FormTemplate, v: StoredVersion): Response | null {
  if (!activeAssignments(tpl.id).length) return problem(409, "reports.form_not_assigned", "Mẫu chưa gán loại báo cáo — không phát hành được");
  if (!v.effective_from) {
    return fieldProblem(422, "reports.form_effective_invalid", "“Áp dụng từ kỳ” không hợp lệ", [{ field: "effective_from", reason: "bắt buộc: ngày bắt đầu kỳ áp dụng (vd 2026-09-07 cho Tuần 37/2026)" }]);
  }
  const last = versionsOf(tpl.id)
    .filter((x) => x.status === "published" && x.effective_from)
    .map((x) => x.effective_from!)
    .sort()
    .at(-1);
  if (last && v.effective_from < last) {
    return problem(422, "reports.form_effective_invalid", "“Áp dụng từ kỳ” không hợp lệ", { detail: `kỳ đã có phiên bản ${last} — phiên bản mới chỉ áp dụng từ kỳ sau` });
  }
  return null;
}

function mutateDraft(ctx: Ctx, event: FormLogEntry["event"], fn: (v: StoredVersion) => FormLogEntry["changes"] | Response): Response {
  if (!isAdmin(ctx.persona)) return forbidden();
  const lv = loadVersion(ctx);
  if (lv instanceof Response) return lv;
  const { tpl, v } = lv;
  if (tpl.status !== "active") return problem(409, "reports.form_retired", "Mẫu đã ngừng dùng");
  if (v.status !== "draft") return problem(409, "reports.form_version_locked", "Chỉ sửa được phiên bản nháp; phiên bản đã phát hành là bất biến", { detail: `phiên bản v${v.version} đang ở trạng thái ${v.status}` });
  const ch = fn(v);
  if (ch instanceof Response) return ch;
  v.updated_by = ctx.persona.me.actor_ref;
  v.updated_at = now();
  v.definition_sha256 = sha(v.definition);
  v.completeness = v.definition.completeness;
  v.pending_count = v.definition.pending.length;
  v.column_count = v.definition.columns.length;
  logEvent(tpl.id, ctx.persona.me.actor_ref, event, ch, v);
  return json(strip(v, true));
}

function diffDef(a: FormDefinition, b: FormDefinition): FormLogEntry["changes"] {
  const out: FormLogEntry["changes"] = [];
  const add = (field: string, o: unknown, n: unknown) => {
    if (out.length < 200) out.push({ field, old: o, new: n });
  };
  if (a.title !== b.title) add("title", a.title, b.title);
  if (a.sheet_name !== b.sheet_name) add("sheet_name", a.sheet_name, b.sheet_name);
  if (a.requirements !== b.requirements) add("requirements", a.requirements, b.requirements);
  if (a.columns.length !== b.columns.length) add("columns.count", a.columns.length, b.columns.length);
  for (let i = 0; i < Math.max(a.columns.length, b.columns.length); i++) {
    const ca = a.columns[i];
    const cb = b.columns[i];
    if (!ca && cb) add(`columns.${cb.letter}`, null, cb.title);
    else if (ca && !cb) add(`columns.${ca.letter}`, ca.title, null);
    else if (ca && cb) {
      for (const f of ["title", "kind", "dtype", "formula", "unit"] as const) {
        if ((ca[f] ?? "") !== (cb[f] ?? "")) add(`columns.${ca.letter}.${f}`, ca[f] ?? "", cb[f] ?? "");
      }
    }
  }
  if (a.header.rows !== b.header.rows) add("header.rows", a.header.rows, b.header.rows);
  if (JSON.stringify(a.header.merges) !== JSON.stringify(b.header.merges)) add("header.merges", a.header.merges, b.header.merges);
  if (a.rows.indicators.length !== b.rows.indicators.length) add("rows.indicators.count", a.rows.indicators.length, b.rows.indicators.length);
  if (a.rules.length !== b.rules.length) add("rules.count", a.rules.length, b.rules.length);
  return out;
}

function validDate(s: unknown): s is string {
  return typeof s === "string" && /^\d{4}-\d{2}-\d{2}$/.test(s) && !Number.isNaN(Date.parse(`${s}T00:00:00Z`));
}

// ---------------------------------------------------------------------------
// Nạp Excel (giả lập): đọc lại tệp ZIP không nén do "Tải mẫu Excel" sinh; tệp nén → lưới mẫu + cảnh báo.
// ---------------------------------------------------------------------------

function importDefinition(bytes: Uint8Array, code: string, title: string, fileName: string): { def: FormDefinition; report: FormImportReport } {
  const parsed = readStoredXlsx(bytes);
  const warnings: string[] = [];
  if (!parsed) {
    const def = blankDefinition(code, title);
    def.source = { kind: "import", file: fileName };
    warnings.push("Chế độ giả lập không giải nén được tệp Excel nén — đã dựng lưới tối thiểu A–D; máy chủ thật phân tích đủ tiêu đề gộp, chữ cái cột, công thức.");
    return { def, report: { sheet: "Sheet1", sheets: ["Sheet1"], header_rows: 1, data_row: 2, columns: 4, merges: 0, formulas: 0, kind_row: false, warnings } };
  }
  // Bố cục do exportXlsx sinh: dòng 1…H tiêu đề, H+1 hàng chữ cái, H+2 dòng loại cột, H+3 dữ liệu.
  let letterRow = 0;
  for (let r = 1; r <= 25 && !letterRow; r++) if (parsed.cells.get(`A${r}`)?.text === "A" && parsed.cells.get(`B${r}`)?.text === "B") letterRow = r;
  if (!letterRow) {
    const def = blankDefinition(code, title);
    def.source = { kind: "import", file: fileName, sheet: parsed.sheet };
    warnings.push("Không nhận ra hàng chữ cái A, B, C… — dựng lưới tối thiểu (giả lập).");
    return { def, report: { sheet: parsed.sheet, sheets: [parsed.sheet], header_rows: 1, data_row: 2, columns: 4, merges: 0, formulas: 0, kind_row: false, warnings } };
  }
  const H = letterRow - 1;
  let n = 0;
  while (parsed.cells.get(`${columnLetter(n + 1)}${letterRow}`)?.text === columnLetter(n + 1)) n++;
  const KIND: Record<string, FormDefinition["columns"][number]["kind"]> = { "Cố định": "fixed", "Tỉnh giao": "provincial", Nhập: "input", Kỳ: "input", "ƒx": "formula" };
  const kindRow = letterRow + 1;
  const dataRow = letterRow + 2;
  const def = blankDefinition(code, title);
  def.columns = [];
  def.header = { rows: Math.max(1, H), cells: [], merges: [] };
  let formulas = 0;
  for (let i = 1; i <= n; i++) {
    const L = columnLetter(i);
    let kind = KIND[parsed.cells.get(`${L}${kindRow}`)?.text ?? ""] ?? "input";
    const f = parsed.cells.get(`${L}${dataRow}`)?.formula;
    let title2 = "";
    for (let r = H; r >= 1 && !title2; r--) title2 = parsed.cells.get(`${L}${r}`)?.text ?? "";
    const col: FormDefinition["columns"][number] = { letter: L, title: title2 || L, kind, dtype: kind === "fixed" && i <= 3 ? (i === 1 ? "integer" : "text") : "number" };
    if (kind === "fixed") col.fixed_source = i === 1 ? "row_no" : i === 2 ? "region_name" : i === 3 ? "commune_name" : "none";
    if (kind === "formula") {
      if (f) {
        col.formula = f.replace(new RegExp(`([A-Z]{1,3})${dataRow}(?![0-9])`, "g"), "$1#");
        formulas++;
      } else {
        kind = "input";
        col.kind = "input";
        warnings.push(`Cột ${L}: dòng loại cột ghi ƒx nhưng không có công thức — nạp thành cột nhập.`);
      }
    }
    if (parsed.cells.get(`${L}${kindRow}`)?.text === "Kỳ") warnings.push(`Cột ${L}: cột theo kỳ nạp thành cột nhập — đặt lại khối kỳ trong trình thiết kế.`);
    def.columns.push(col);
    for (let r = 1; r <= H; r++) {
      const t = parsed.cells.get(`${L}${r}`)?.text;
      if (t) def.header.cells.push({ row: r, letter: L, text: t });
    }
  }
  def.header.merges = parsed.merges;
  def.sheet_name = parsed.sheet;
  def.source = { kind: "import", file: fileName, sheet: parsed.sheet, data_row: dataRow };
  return { def, report: { sheet: parsed.sheet, sheets: [parsed.sheet], header_rows: H, data_row: dataRow, columns: n, merges: parsed.merges.length, formulas, kind_row: true, warnings } };
}

// ---------------------------------------------------------------------------
// Admin API
// ---------------------------------------------------------------------------

export const formsAdminRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/reports\/forms$/,
    handler: ({ persona, url }) => {
      if (!isAdmin(persona)) return forbidden();
      const dept = url.searchParams.get("department");
      const level = url.searchParams.get("level");
      const status = url.searchParams.get("status");
      const rt = url.searchParams.get("report_type");
      const items = S.templates
        .filter((t) => (!dept || t.department_code === dept) && (!level || t.entry_level === level) && (!status || t.status === status))
        .map(summary)
        .filter((s) => !rt || s.report_types.includes(rt as FormReportType))
        .sort((a, b) => a.template.code.localeCompare(b.template.code));
      return json({ items });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/reports\/forms$/,
    handler: ({ persona, body }) => {
      if (!isAdmin(persona)) return forbidden();
      const b = (body ?? {}) as { code?: string; name?: string; department_code?: string; entry_level?: string; definition?: FormDefinition; clone_from_version_id?: string };
      const code = (b.code ?? "").trim();
      const name = (b.name ?? "").trim();
      if (!validCode(code)) return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "code", reason: "chữ hoa, số, . _ -; 2–32 ký tự (vd T01)" }]);
      if (!name || Array.from(name).length > 300) return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "name", reason: "bắt buộc, tối đa 300 ký tự" }]);
      if (!deptOK(b.department_code)) return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "department_code", reason: "mã phòng ban chữ thường, số, gạch dưới" }]);
      if (b.entry_level !== "commune" && b.entry_level !== "province") return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "entry_level", reason: "commune | province" }]);
      if (S.templates.some((t) => t.department_code === b.department_code && t.code === code)) return problem(409, "reports.form_code_taken", "Mã mẫu đã có trong phòng ban");
      const at = now();
      const actor = persona.me.actor_ref;
      const tpl: FormTemplate = { id: uuidv7(), code, name, department_code: b.department_code, entry_level: b.entry_level, status: "active", retired_reason: null, created_by: actor, created_at: at, updated_at: at };
      let def: FormDefinition;
      let origin: FormVersion["origin"] = "blank";
      let originId: string | null = null;
      if (b.clone_from_version_id) {
        const src = [...S.versions.values()].flat().find((v) => v.id === b.clone_from_version_id);
        if (!src) return versionNotFound();
        def = cloneDef(src.definition);
        origin = "clone";
        originId = src.id;
      } else if (b.definition) def = cloneDef(b.definition);
      else def = blankDefinition(code, name);
      def.code = code;
      if (!def.title) def.title = name;
      try {
        validate(def);
      } catch (e) {
        return defProblem(e);
      }
      const v = newVersion(tpl, 1, def, origin, actor, at, { origin_version_id: originId });
      S.templates.push(tpl);
      S.versions.set(tpl.id, [v]);
      logEvent(tpl.id, actor, "template_created", [
        { field: "code", old: null, new: code },
        { field: "department_code", old: null, new: tpl.department_code },
        { field: "entry_level", old: null, new: tpl.entry_level },
      ]);
      logEvent(tpl.id, actor, "draft_created", [{ field: "origin", old: null, new: origin }], v);
      return json({ template: tpl, version: strip(v, true) }, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/reports\/forms\/import$/,
    handler: ({ persona, body, url, request }) => {
      if (!isAdmin(persona)) return forbidden();
      const type = (request.headers.get("Content-Type") ?? "").split(";")[0]?.trim();
      if (type !== "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" || !isZip(body)) {
        return problem(422, "reports.form_import_invalid", "Tệp Excel không phải mẫu báo cáo hợp lệ", { detail: "Content-Type phải là application/vnd.openxmlformats-officedocument.spreadsheetml.sheet và tệp .xlsx" });
      }
      const q = url.searchParams;
      const actor = persona.me.actor_ref;
      const fileName = q.get("file_name") || "mau.xlsx";
      let tpl: FormTemplate | undefined;
      if (q.get("template_id")) {
        tpl = tplOf(q.get("template_id") ?? "");
        if (!tpl) return notFound();
        if (tpl.status !== "active") return problem(409, "reports.form_retired", "Mẫu đã ngừng dùng");
        const open = openVersion(tpl.id);
        if (open) return problem(409, "reports.form_draft_exists", "Mẫu đang có phiên bản nháp hoặc chờ phát hành", { detail: `phiên bản v${open.version} (${open.status})` });
      } else {
        const code = (q.get("code") ?? "").trim();
        const name = (q.get("name") ?? "").trim();
        const dept = q.get("department_code");
        const level = q.get("entry_level") || "commune";
        if (!validCode(code)) return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "code", reason: "chữ hoa, số, . _ -; 2–32 ký tự" }]);
        if (!name) return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "name", reason: "bắt buộc, tối đa 300 ký tự" }]);
        if (!deptOK(dept)) return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "department_code", reason: "mã phòng ban chữ thường, số, gạch dưới" }]);
        if (level !== "commune" && level !== "province") return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "entry_level", reason: "commune | province" }]);
        if (S.templates.some((t) => t.department_code === dept && t.code === code)) return problem(409, "reports.form_code_taken", "Mã mẫu đã có trong phòng ban");
        const at = now();
        tpl = { id: uuidv7(), code, name, department_code: dept, entry_level: level, status: "active", retired_reason: null, created_by: actor, created_at: at, updated_at: at };
      }
      const { def, report } = importDefinition(body, tpl.code, tpl.name, fileName);
      try {
        validate(def);
      } catch (e) {
        return defProblem(e);
      }
      const isNew = !S.templates.includes(tpl);
      if (isNew) {
        S.templates.push(tpl);
        logEvent(tpl.id, actor, "template_created", [
          { field: "code", old: null, new: tpl.code },
          { field: "origin", old: null, new: "import" },
        ]);
      }
      const importId = uuidv7();
      const list = versionsOf(tpl.id);
      const v = newVersion(tpl, list.length + 1, def, "import", actor, now(), { import_id: importId });
      S.versions.set(tpl.id, [...list, v]);
      logEvent(tpl.id, actor, "imported", [
        { field: "sheet", old: null, new: report.sheet },
        { field: "columns", old: null, new: report.columns },
        { field: "warnings", old: null, new: report.warnings?.length ?? 0 },
      ], v);
      return json({ import_id: importId, template: tpl, version: strip(v, true), report }, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/reports\/forms\/seed$/,
    handler: ({ persona, body }) => {
      if (!isAdmin(persona)) return forbidden();
      const dept = ((body ?? {}) as { department_code?: string }).department_code || "trong_trot";
      if (!deptOK(dept)) return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "department_code", reason: "mã phòng ban chữ thường, số, gạch dưới" }]);
      const created: string[] = [];
      const skipped: string[] = [];
      const actor = persona.me.actor_ref;
      for (const s of FORM_SEEDS) {
        if (S.templates.some((t) => t.department_code === dept && t.code === s.code)) {
          skipped.push(s.code);
          continue;
        }
        const at = now();
        const tpl: FormTemplate = { id: uuidv7(), code: s.code, name: s.name, department_code: dept, entry_level: s.entry_level, status: "active", retired_reason: null, created_by: actor, created_at: at, updated_at: at };
        const v = newVersion(tpl, 1, cloneDef(s.definition), "seed", actor, at);
        S.templates.push(tpl);
        S.versions.set(tpl.id, [v]);
        logEvent(tpl.id, actor, "seeded", [
          { field: "seed", old: null, new: "kn-v7.7-seed-2026-10-01" },
          { field: "completeness", old: null, new: s.definition.completeness },
          { field: "pending", old: null, new: s.definition.pending.length },
        ], v);
        S.assignments.push({ id: uuidv7(), template_id: tpl.id, department_code: dept, entry_level: s.entry_level, report_type: s.report_type, status: "active", assigned_by: actor, assigned_at: at, revoked_by: null, revoked_at: null, revoke_reason: null });
        logEvent(tpl.id, actor, "assigned", [{ field: "report_type", old: null, new: s.report_type }]);
        created.push(s.code);
      }
      return json({ created, skipped });
    },
  },
  {
    method: "GET",
    pattern: new RegExp(`^/v1/reports/forms/${UUID}$`),
    handler: ({ persona, params }) => {
      if (!isAdmin(persona)) return forbidden();
      const tpl = tplOf(params[0]);
      if (!tpl) return notFound();
      return json({ template: tpl, versions: [...versionsOf(tpl.id)].reverse().map((v) => strip(v, false)), assignments: S.assignments.filter((a) => a.template_id === tpl.id) });
    },
  },
  {
    method: "POST",
    pattern: new RegExp(`^/v1/reports/forms/${UUID}/retire$`),
    handler: ({ persona, params, body }) => {
      if (!isAdmin(persona)) return forbidden();
      const r = reason(body, true);
      if (r instanceof Response) return r;
      const tpl = tplOf(params[0]);
      if (!tpl) return notFound();
      if (tpl.status !== "active") return problem(409, "reports.form_retired", "Mẫu đã ngừng dùng");
      tpl.status = "retired";
      tpl.retired_reason = r;
      tpl.updated_at = now();
      logEvent(tpl.id, persona.me.actor_ref, "template_retired", [{ field: "status", old: "active", new: "retired" }], undefined, r);
      return json(tpl);
    },
  },
  {
    method: "GET",
    pattern: new RegExp(`^/v1/reports/forms/${UUID}/log$`),
    handler: ({ persona, params, url }) => {
      if (!isAdmin(persona)) return forbidden();
      const tpl = tplOf(params[0]);
      if (!tpl) return notFound();
      const all = [...(S.log.get(tpl.id) ?? [])].reverse();
      const limit = Math.min(Math.max(Number(url.searchParams.get("limit") ?? 50) || 50, 1), 200);
      const cursor = url.searchParams.get("cursor");
      const start = cursor ? Math.max(0, all.findIndex((e) => e.id === atob(cursor)) + 1) : 0;
      const items = all.slice(start, start + limit);
      const next = start + limit < all.length ? btoa(items.at(-1)!.id) : null;
      return json({ items, next_cursor: next });
    },
  },
  {
    method: "POST",
    pattern: new RegExp(`^/v1/reports/forms/${UUID}/versions$`),
    handler: ({ persona, params, body }) => {
      if (!isAdmin(persona)) return forbidden();
      const b = (body ?? {}) as { from_version?: number; restore?: boolean };
      const tpl = tplOf(params[0]);
      if (!tpl) return notFound();
      if (tpl.status !== "active") return problem(409, "reports.form_retired", "Mẫu đã ngừng dùng");
      const open = openVersion(tpl.id);
      if (open) return problem(409, "reports.form_draft_exists", "Mẫu đang có phiên bản nháp hoặc chờ phát hành", { detail: `phiên bản v${open.version} (${open.status})` });
      const list = versionsOf(tpl.id);
      const src = b.from_version ? list.find((v) => v.version === b.from_version) : list.at(-1);
      if (!src) return versionNotFound();
      const def = cloneDef(src.definition);
      def.source = { kind: "designer" };
      const actor = persona.me.actor_ref;
      const v = newVersion(tpl, list.length + 1, def, b.restore ? "restore" : "clone", actor, now(), { origin_version_id: src.id });
      S.versions.set(tpl.id, [...list, v]);
      logEvent(tpl.id, actor, "draft_created", [
        { field: "origin", old: null, new: v.origin },
        { field: "from_version", old: null, new: src.version },
      ], v);
      return json(strip(v, true), 201);
    },
  },
  {
    method: "GET",
    pattern: new RegExp(`^/v1/reports/forms/${UUID}/versions/(\\d+)$`),
    handler: (ctx) => {
      if (!isAdmin(ctx.persona)) return forbidden();
      const lv = loadVersion(ctx);
      if (lv instanceof Response) return lv;
      return json({ template: lv.tpl, version: strip(lv.v, true) });
    },
  },
  {
    method: "POST",
    pattern: new RegExp(`^/v1/reports/forms/${UUID}/versions/(\\d+)/draft$`),
    handler: (ctx) => {
      const b = (ctx.body ?? {}) as { definition?: FormDefinition; effective_from?: string; effective_label?: string };
      if (b.effective_from !== undefined && b.effective_from !== "" && !validDate(b.effective_from)) {
        return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "effective_from", reason: "ngày dạng YYYY-MM-DD" }]);
      }
      return mutateDraft(ctx, "draft_saved", (v) => {
        let ch: FormLogEntry["changes"] = [];
        if (b.definition) {
          const d = cloneDef(b.definition);
          d.code = v.definition.code;
          try {
            validate(d);
          } catch (e) {
            return defProblem(e);
          }
          ch = diffDef(v.definition, d);
          v.definition = d;
        }
        if (b.effective_from) {
          ch.push({ field: "effective_from", old: v.effective_from, new: b.effective_from });
          v.effective_from = b.effective_from;
        }
        if (b.effective_label !== undefined) {
          const l = b.effective_label.trim();
          const n = Array.from(l).length;
          if (n < 1 || n > 60) return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "effective_label", reason: "1–60 ký tự, vd “Tuần 37/2026”" }]);
          v.effective_label = l;
        }
        return ch;
      });
    },
  },
  {
    method: "POST",
    pattern: new RegExp(`^/v1/reports/forms/${UUID}/versions/(\\d+)/ops$`),
    handler: (ctx) => {
      const ops = ((ctx.body ?? {}) as { ops?: FormOp[] }).ops ?? [];
      if (!ops.length || ops.length > 500) return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "ops", reason: "1–500 thao tác" }]);
      return mutateDraft(ctx, "draft_ops_applied", (v) => {
        let d: FormDefinition;
        try {
          d = applyOps(v.definition, ops);
        } catch (e) {
          return defProblem(e);
        }
        const ch = [...diffDef(v.definition, d), { field: "ops", old: null, new: ops }];
        v.definition = d;
        return ch;
      });
    },
  },
  {
    method: "POST",
    pattern: new RegExp(`^/v1/reports/forms/${UUID}/versions/(\\d+)/propose$`),
    handler: (ctx) => {
      if (!isAdmin(ctx.persona)) return forbidden();
      const b = (ctx.body ?? {}) as { effective_from?: string; effective_label?: string };
      if (b.effective_from && !validDate(b.effective_from)) return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "effective_from", reason: "ngày dạng YYYY-MM-DD" }]);
      const lv = loadVersion(ctx);
      if (lv instanceof Response) return lv;
      const { tpl, v } = lv;
      if (tpl.status !== "active") return problem(409, "reports.form_retired", "Mẫu đã ngừng dùng");
      if (v.status !== "draft") return problem(409, "reports.form_version_locked", "Chỉ sửa được phiên bản nháp; phiên bản đã phát hành là bất biến");
      try {
        validate(v.definition);
      } catch (e) {
        return defProblem(e);
      }
      const prevFrom = v.effective_from;
      const prevLabel = v.effective_label;
      if (b.effective_from) v.effective_from = b.effective_from;
      if (b.effective_label !== undefined) v.effective_label = b.effective_label.trim();
      const bad = checkPublishable(tpl, v);
      if (bad) {
        v.effective_from = prevFrom;
        v.effective_label = prevLabel;
        return bad;
      }
      const at = now();
      v.updated_by = ctx.persona.me.actor_ref;
      v.updated_at = at;
      v.status = "proposed";
      v.proposed_by = ctx.persona.me.actor_ref;
      v.proposed_at = at;
      logEvent(tpl.id, ctx.persona.me.actor_ref, "proposed", [
        { field: "status", old: "draft", new: "proposed" },
        { field: "effective_from", old: null, new: v.effective_from },
      ], v);
      return json(strip(v, true));
    },
  },
  {
    method: "POST",
    pattern: new RegExp(`^/v1/reports/forms/${UUID}/versions/(\\d+)/withdraw$`),
    handler: (ctx) => {
      if (!isAdmin(ctx.persona)) return forbidden();
      const r = reason(ctx.body, false);
      if (r instanceof Response) return r;
      const lv = loadVersion(ctx);
      if (lv instanceof Response) return lv;
      const { tpl, v } = lv;
      if (v.status !== "proposed") return problem(409, "reports.form_not_proposed", "Phiên bản chưa được đề nghị phát hành");
      const prev = v.proposed_by;
      v.status = "draft";
      v.proposed_by = null;
      v.proposed_at = null;
      logEvent(tpl.id, ctx.persona.me.actor_ref, "proposal_withdrawn", [
        { field: "status", old: "proposed", new: "draft" },
        { field: "proposed_by", old: prev, new: null },
      ], v, r);
      return json(strip(v, true));
    },
  },
  {
    method: "POST",
    pattern: new RegExp(`^/v1/reports/forms/${UUID}/versions/(\\d+)/publish$`),
    handler: (ctx) => {
      if (!isAdmin(ctx.persona)) return forbidden();
      const lv = loadVersion(ctx);
      if (lv instanceof Response) return lv;
      const { tpl, v } = lv;
      if (v.status !== "proposed") return problem(409, "reports.form_not_proposed", "Phiên bản chưa được đề nghị phát hành");
      const me = ctx.persona.me.actor_ref;
      if (v.proposed_by === me || v.updated_by === me) {
        return problem(403, "self_approval_forbidden", "Self approval forbidden", { detail: "người phát hành phải khác người đề nghị và người sửa cuối (R-56)" });
      }
      const bad = checkPublishable(tpl, v);
      if (bad) return bad;
      v.status = "published";
      v.published_by = me;
      v.published_at = now();
      logEvent(tpl.id, me, "published", [{ field: "status", old: "proposed", new: "published" }], v);
      return json(strip(v, true));
    },
  },
  {
    method: "POST",
    pattern: new RegExp(`^/v1/reports/forms/${UUID}/versions/(\\d+)/retire$`),
    handler: (ctx) => {
      if (!isAdmin(ctx.persona)) return forbidden();
      const r = reason(ctx.body, true);
      if (r instanceof Response) return r;
      const lv = loadVersion(ctx);
      if (lv instanceof Response) return lv;
      const { tpl, v } = lv;
      if (v.status !== "published") return problem(409, "reports.form_not_published", "Phiên bản chưa phát hành");
      v.status = "retired";
      v.retired_by = ctx.persona.me.actor_ref;
      v.retired_at = now();
      v.retire_reason = r;
      logEvent(tpl.id, ctx.persona.me.actor_ref, "version_retired", [{ field: "status", old: "published", new: "retired" }], v, r);
      return json(strip(v, true));
    },
  },
  {
    method: "POST",
    pattern: new RegExp(`^/v1/reports/form-versions/${UUID}/evaluate$`),
    handler: (ctx) => evaluateRoute(ctx),
  },
  {
    method: "GET",
    pattern: /^\/v1\/reports\/form-assignments$/,
    handler: ({ persona, url }) => {
      if (!isAdmin(persona)) return forbidden();
      const q = url.searchParams;
      const activeOnly = q.get("active") !== "false";
      const items = S.assignments.filter(
        (a) =>
          (!q.get("template_id") || a.template_id === q.get("template_id")) &&
          (!q.get("department") || a.department_code === q.get("department")) &&
          (!q.get("level") || a.entry_level === q.get("level")) &&
          (!q.get("report_type") || a.report_type === q.get("report_type")) &&
          (!activeOnly || a.status === "active"),
      );
      return json({ items });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/reports\/form-assignments$/,
    handler: ({ persona, body }) => {
      if (!isAdmin(persona)) return forbidden();
      const b = (body ?? {}) as { template_id?: string; report_type?: FormReportType };
      if (!b.report_type || !REPORT_TYPES.includes(b.report_type)) return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "report_type", reason: REPORT_TYPES.join(" | ") }]);
      const tpl = tplOf(b.template_id);
      if (!tpl) return notFound();
      if (tpl.status !== "active") return problem(409, "reports.form_retired", "Mẫu đã ngừng dùng");
      if (activeAssignments(tpl.id).some((a) => a.report_type === b.report_type)) return problem(409, "reports.form_already_assigned", "Mẫu đã gán loại báo cáo này");
      const a: FormAssignment = { id: uuidv7(), template_id: tpl.id, department_code: tpl.department_code, entry_level: tpl.entry_level, report_type: b.report_type, status: "active", assigned_by: persona.me.actor_ref, assigned_at: now(), revoked_by: null, revoked_at: null, revoke_reason: null };
      S.assignments.push(a);
      logEvent(tpl.id, persona.me.actor_ref, "assigned", [{ field: "report_type", old: null, new: b.report_type }]);
      return json(a, 201);
    },
  },
  {
    method: "POST",
    pattern: new RegExp(`^/v1/reports/form-assignments/${UUID}/revoke$`),
    handler: ({ persona, params, body }) => {
      if (!isAdmin(persona)) return forbidden();
      const r = reason(body, true);
      if (r instanceof Response) return r;
      const a = S.assignments.find((x) => x.id === params[0]);
      if (!a || a.status !== "active") return problem(404, "reports.form_assignment_not_found", "Không tìm thấy gán mẫu");
      a.status = "revoked";
      a.revoked_by = persona.me.actor_ref;
      a.revoked_at = now();
      a.revoke_reason = r;
      logEvent(a.template_id, persona.me.actor_ref, "assignment_revoked", [{ field: "report_type", old: a.report_type, new: null }], undefined, r);
      return json(a);
    },
  },
];

// ---------------------------------------------------------------------------
// Application API
// ---------------------------------------------------------------------------

function evaluateRoute(ctx: Ctx): Response {
  const p = ctx.persona;
  if (!hasRole(p, ...READER_ROLES)) return forbidden();
  const input = (ctx.body ?? {}) as FormEvalInput;
  if (!Array.isArray(input.rows)) return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "rows", reason: "bắt buộc" }]);
  if (input.rows.length > 5000) return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "rows", reason: "tối đa 5000 dòng" }]);
  if (input.mode !== "draft" && input.mode !== "submit") return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "mode", reason: "draft | submit" }]);
  const v = [...S.versions.values()].flat().find((x) => x.id === ctx.params[0]);
  if (!v || (!isAdmin(p) && v.status !== "published" && v.status !== "retired")) return versionNotFound();
  try {
    validate(v.definition);
  } catch (e) {
    return defProblem(e);
  }
  return json(evaluateGrid(v.definition, input));
}

export const formsAppRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/reports\/forms\/available$/,
    handler: ({ persona, url }) => {
      if (!hasRole(persona, ...READER_ROLES)) return forbidden();
      const dept = url.searchParams.get("department");
      if (!deptOK(dept)) return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "department", reason: "bắt buộc" }]);
      const level = url.searchParams.get("level");
      const rt = url.searchParams.get("report_type");
      const at = url.searchParams.get("at") ?? new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
      const items = [];
      for (const a of S.assignments) {
        if (a.status !== "active" || a.department_code !== dept || (level && a.entry_level !== level) || (rt && a.report_type !== rt)) continue;
        const tpl = tplOf(a.template_id);
        if (!tpl || tpl.status !== "active") continue;
        const v = versionsOf(tpl.id)
          .filter((x) => x.status === "published" && x.effective_from && x.effective_from <= at)
          .sort((x, y) => (x.effective_from! < y.effective_from! ? -1 : 1))
          .at(-1);
        if (!v) continue;
        items.push({ template: tpl, report_type: a.report_type, version: strip(v, false) });
      }
      items.sort((x, y) => x.template.code.localeCompare(y.template.code));
      return json({ items });
    },
  },
  {
    method: "GET",
    pattern: new RegExp(`^/v1/reports/forms/${UUID}/versions/(\\d+)/layout$`),
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...READER_ROLES)) return forbidden();
      const lv = loadVersion(ctx);
      if (lv instanceof Response) return lv;
      if (!isAdmin(ctx.persona) && lv.v.status !== "published" && lv.v.status !== "retired") return versionNotFound();
      const labels = ctx.url.searchParams.getAll("period_label").map((s) => s.trim()).filter(Boolean);
      if (labels.length > 200) return fieldProblem(422, "validation_failed", "Validation failed", [{ field: "period_labels", reason: "tối đa 200 kỳ" }]);
      const def = layout(lv.v.definition, { periodLabels: labels, season: ctx.url.searchParams.get("season") ?? "" });
      return json({ template: lv.tpl, version: strip(lv.v, false), definition: def });
    },
  },
  {
    method: "GET",
    pattern: new RegExp(`^/v1/reports/forms/${UUID}/versions/(\\d+)/template\\.xlsx$`),
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...READER_ROLES)) return forbidden();
      const lv = loadVersion(ctx);
      if (lv instanceof Response) return lv;
      if (!isAdmin(ctx.persona) && lv.v.status !== "published" && lv.v.status !== "retired") return versionNotFound();
      const def = layout(lv.v.definition, { periodLabels: [], season: ctx.url.searchParams.get("season") ?? "" });
      const bytes = exportXlsx(def);
      return new Response(new Blob([bytes as BlobPart]), {
        status: 200,
        headers: {
          "Content-Type": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
          "Content-Disposition": `attachment; filename="${lv.tpl.code}-v${lv.v.version}.xlsx"`,
        },
      });
    },
  },
  {
    method: "POST",
    pattern: new RegExp(`^/v1/reports/form-versions/${UUID}/evaluate$`),
    handler: (ctx) => evaluateRoute(ctx),
  },
];
