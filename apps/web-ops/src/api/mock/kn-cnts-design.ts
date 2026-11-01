// GIẢ LẬP FN-07 phần xem/kiểm tệp. XLSX chỉ đọc ZIP stored do giả lập sinh; backend thật đọc XLSX nén.
import type { DesignImport, DesignField } from "@/kn/cnts/design/api";
import { DESIGN_HEADERS } from "@/kn/cnts/design/api";
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
export const cntsDesignRoutes: Route[] = [
  {    
method: "GET", pattern: /^\/v1\/extension\/cnts\/form-designs$/, handler: c => {
      if (!allowed(c)) return problem(403, "forbidden", "Chỉ Cấp Tỉnh CNTS và Admin được xem thiết kế biểu");
      return json({ items: cntsDesignMockForms().map(f => ({ code: f.code, bm: f.bm, title: f.title, period_type: f.period_type, due: f.due, template_id: f.template_id, version: f.template_version, fields: f.fields })) });
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
