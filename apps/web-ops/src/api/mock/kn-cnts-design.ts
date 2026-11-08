// GIẢ LẬP FN-07 (#174 xem/kiểm tệp; #192 lưu phiên bản, lịch sử, sao chép/tạo biểu điều tra, khóa mẫu KNQG). XLSX chỉ đọc
// ZIP stored do giả lập sinh; backend thật đọc XLSX nén. Phiên bản giả lập không đổi kỳ đã phát hành của giả lập biểu kỳ.
import type { DesignImport, DesignField } from "@/kn/cnts/design/api";
import { DESIGN_HEADERS, designFieldErrors } from "@/kn/cnts/design/api";
import { uuidv7 } from "../idempotency";
import { parseCsv } from "./admin-ops";
import { rowsOfXlsx } from "./kn-cnts-cal-xlsx";
import { cntsDesignMockForms } from "./kn-cnts-periods";
import { knContext } from "./kn";
import { json, problem, validation, type Ctx, type Route } from "./http";

function allowed(c: Ctx): boolean {
  const k = knContext(c.request.headers.get("X-AG-Context") ?? "");
  return !!k && (k.role === "admin" || (k.role === "kn_tinh" && k.department_code === "chan_nuoi_thuy_san"));
}
export function previewDesignRows(rows: string[][]): DesignImport {
  const out: DesignImport = { fields: [], issues: [], valid: false };
  const add = (line: number, field: string, message: string) => out.issues.push({ line, field, message });
  if (rows[0]?.map(x => x.trim().toLowerCase()).join(",") !== DESIGN_HEADERS.join(",")) { add(1, "header", "Cần đúng thứ tự cột: " + DESIGN_HEADERS.join(", ")); return out; }
  const data = rows.slice(1).filter(r => r.some(x => x.trim()));
  if (!data.length || data.length > 500) { add(1, "rows", "Cần 1–500 dòng chỉ tiêu"); return out; }
  const seen = new Set<string>();
  data.forEach((raw, i) => {
    const r = raw.map(x => x.trim()); const at = (n: number) => r[n] ?? ""; const f: DesignField = { key: at(0), label: at(1), type: at(2) as DesignField["type"], required: at(3) === "true", unit: at(4), group: at(5), non_negative: at(6) === "true" };
    if (raw.length > 8) add(i + 2, "row", "Dòng có cột thừa");
    if (!/^[a-z][a-z0-9_]{0,63}$/.test(f.key) || seen.has(f.key)) add(i + 2, "key", "Mã không hợp lệ hoặc trùng"); seen.add(f.key);
    if (!f.label || [...f.label].length > 300) add(i + 2, "label", "Tên cần 1–300 ký tự");
    if ([...at(4)].length > 80) add(i + 2, "unit", "Đơn vị tối đa 80 ký tự");
    if ([...at(5)].length > 200) add(i + 2, "group", "Nhóm tối đa 200 ký tự");
    for (const [n, name] of [[3, "required"], [6, "non_negative"]] as const) if (at(n) && !["true", "false"].includes(at(n))) add(i + 2, name, "Chỉ nhận true hoặc false");
    if (!["text", "integer", "decimal", "date", "choice"].includes(f.type)) add(i + 2, "type", "Kiểu không hợp lệ");
    const opts = at(7) ? at(7).split("|").map(x => x.trim()) : [];
    if (f.type === "choice") { if (!opts.length || opts.length > 100 || opts.some(x => !x || [...x].length > 100) || new Set(opts).size !== opts.length) add(i + 2, "options", "choice cần 1–100 lựa chọn, không rỗng/trùng"); f.options = opts; }
    else if (opts.length) add(i + 2, "options", "Chỉ kiểu choice có lựa chọn");
    if (at(7).length > 10000) add(i + 2, "options", "Danh sách lựa chọn quá dài");
    if (f.non_negative && !["integer", "decimal"].includes(f.type)) add(i + 2, "non_negative", "Chỉ dùng cho kiểu số");
    out.fields.push(f);
  });
  out.valid = out.issues.length === 0; if (!out.valid) out.fields = []; return out;
}
const LOCKED = ["CNTS-BM01", "CNTS-BM04", "CNTS-BM05-I", "CNTS-BM05-II"];
interface Ver { template_id: string; version: number; title: string; fields: DesignField[] }
const SAVED = new Map<string, Ver[]>(); // mã → phiên bản (cũ trước)
/** Xóa trạng thái (kiểm thử). */
const OPERATIONS = new Map<string, { fingerprint: string; code: string; version: Ver }>();
export function resetCntsDesign() { SAVED.clear(); OPERATIONS.clear(); }
function writer(c: Ctx, code: string): boolean {
  const k = knContext(c.request.headers.get("X-AG-Context") ?? "");
  if (!k || k.read_only) return false;
  if (k.role === "admin") return true;
  return k.role === "kn_tinh" && k.department_code === "chan_nuoi_thuy_san" && (!k.province_group || k.province_group === "tong_hop") &&
    (code === "CNTS-BM17" || code.startsWith("CNTS-DT-"));
}
function versions(code: string): Ver[] {
  const saved = SAVED.get(code) ?? [];
  const base = cntsDesignMockForms().find(f => f.code === code);
  const installed: Ver[] = base?.template_id && base.template_version ? [{ template_id: base.template_id, version: base.template_version, title: `${base.bm} — ${base.title}`, fields: base.fields }] : [];
  return [...installed, ...saved];
}
function view(c: Ctx, code: string) {
  const base = cntsDesignMockForms().find(f => f.code === code); const vs = versions(code); const last = vs.at(-1); const locked = LOCKED.includes(code);
  return { code, bm: base?.bm ?? code, title: last?.title ?? base?.title ?? code, kind: base ? "report" : "survey", period_type: base?.period_type ?? "adhoc",
    due: base?.due ?? "Theo đợt điều tra", locked, can_edit: !locked && writer(c, code), template_id: last?.template_id ?? null, version: last?.version ?? null,
    fields: last?.fields ?? base?.fields ?? [] };
}
export function cntsDesignVersion(code: string, version?: number): Ver | undefined {
  const vs = versions(code);
  const v = version === undefined ? vs.at(-1) : vs.find(v => v.version === version);
  return v ? structuredClone(v) : undefined;
}
function append(c: Ctx, code: string, base: number, title: string, fields: DesignField[]) {
  const key = c.request.headers.get("Idempotency-Key") ?? "";
  const fingerprint = JSON.stringify([c.persona.me.actor_ref, c.request.url, c.body]);
  const prior = OPERATIONS.get(key);
  if (prior) return prior.fingerprint === fingerprint ? json({ code: prior.code, version: prior.version }, 201) : problem(422, "idempotency_key_reused", "Khóa đã dùng cho thao tác khác");
  const vs = versions(code); const cur = vs.at(-1)?.version ?? 0;
  if (base !== cur) return problem(409, "conflict", `phiên bản hiện hành là ${cur}; đọc lại trước khi lưu`);
  const v: Ver = { template_id: uuidv7(), version: cur + 1, title, fields: structuredClone(fields) };
  OPERATIONS.set(key, { fingerprint, code, version: structuredClone(v) });
  SAVED.set(code, [...(SAVED.get(code) ?? []), v]);
  return json({ code, version: v }, 201);
}
export const cntsDesignRoutes: Route[] = [
  {    
method: "GET", pattern: /^\/v1\/extension\/cnts\/form-designs$/, handler: c => {
      if (!allowed(c)) return problem(403, "forbidden", "Chỉ Cấp Tỉnh CNTS và Admin được xem thiết kế biểu");
      const codes = [...cntsDesignMockForms().map(f => f.code), ...[...SAVED.keys()].filter(x => x.startsWith("CNTS-DT-"))];
      return json({ items: codes.map(code => view(c, code)) });
    }  
},
  {    
method: "POST", pattern: /^\/v1\/extension\/cnts\/form-designs$/, handler: c => {
      if (!allowed(c)) return problem(403, "forbidden", "Chỉ Cấp Tỉnh CNTS và Admin");
      const b = (c.body ?? {}) as { code_suffix?: string; title?: string; source_code?: string; source_version?: number; fields?: DesignField[] };
      const suffix = (b.code_suffix ?? "").trim().toUpperCase();
      if (!/^[A-Z0-9][A-Z0-9-]{0,39}$/.test(suffix)) return validation([{ field: "code_suffix", reason: "Chữ in hoa, số, gạch ngang; 1–40 ký tự" }]);
      const code = "CNTS-DT-" + suffix;
      if (!writer(c, code)) return problem(403, "forbidden", "Chỉ cán bộ tổng hợp Cấp Tỉnh CNTS hoặc Admin");
      if (!b.title?.trim()) return validation([{ field: "title", reason: "Tên biểu cần 1–200 ký tự" }]);
      let fields = b.fields ?? [];
      if (b.source_code) {
        if (!b.source_version || b.source_version < 1) return validation([{ field: "source_version", reason: "Chọn phiên bản nguồn bất biến" }]);
        const vs = versions(b.source_code); const src = vs.find(v => v.version === b.source_version);
        if (!src) return problem(404, "forms.template_not_found", "Không tìm thấy biểu nguồn"); fields = src.fields;
      }
      const errs = designFieldErrors(fields); if (errs.length) return validation([{ field: "fields", reason: errs[0]! }]);
      return append(c, code, 0, b.title.trim(), fields);
    }  
},
  {    
method: "GET", pattern: /^\/v1\/extension\/cnts\/form-designs\/([^/]+)\/versions$/, handler: c => {
      if (!allowed(c)) return problem(403, "forbidden", "Chỉ Cấp Tỉnh CNTS và Admin");
      return json({ items: [...versions(decodeURIComponent(c.params[0]!))].reverse() });
    }  
},
  {    
method: "POST", pattern: /^\/v1\/extension\/cnts\/form-designs\/([^/]+)\/versions$/, handler: c => {
      if (!allowed(c)) return problem(403, "forbidden", "Chỉ Cấp Tỉnh CNTS và Admin");
      const code = decodeURIComponent(c.params[0]!);
      if (LOCKED.includes(code)) return problem(403, "forbidden", "Mẫu cố định theo TTKN Quốc gia — không sửa cấu trúc (FN-07 BR-02)");
      if (!writer(c, code)) return problem(403, "forbidden", "Chỉ cán bộ tổng hợp Cấp Tỉnh CNTS hoặc Admin được lưu phiên bản");
      const b = (c.body ?? {}) as { base_version?: number; title?: string; fields?: DesignField[] };
      if (!b.title?.trim()) return validation([{ field: "title", reason: "Tên biểu cần 1–200 ký tự" }]);
      const errs = designFieldErrors(b.fields ?? [], versions(code).find(v => v.version === b.base_version)?.fields ?? []);
      if (errs.length) return validation([{ field: "fields", reason: errs[0]! }]);
      return append(c, code, b.base_version ?? -1, b.title.trim(), b.fields ?? []);
    }  
},
  {    
method: "POST", pattern: /^\/v1\/extension\/cnts\/form-designs\/import-preview$/, handler: c => {
      if (!allowed(c)) return problem(403, "forbidden", "Chỉ Cấp Tỉnh CNTS và Admin được kiểm tệp");
      const b = typeof c.body === "string" ? new TextEncoder().encode(c.body) : c.body;
      if (!(b instanceof Uint8Array) || !b.length || b.length > 2 * 1024 * 1024) return validation([{ field: "file", reason: "Tệp không hợp lệ hoặc vượt 2 MB" }]);
      const zip = b[0] === 0x50 && b[1] === 0x4b;
      const rows = zip ? rowsOfXlsx(b)?.rows : parseCsv(new TextDecoder().decode(b));
      if (!rows) return validation([{ field: "file", reason: "Giả lập chỉ đọc XLSX ZIP stored do giả lập sinh. Máy chủ thật đọc XLSX theo mẫu." }]);
      return json(previewDesignRows(rows));
    }  
},
];
