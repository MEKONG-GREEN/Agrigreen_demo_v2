// BF-TT-FORMS (#107, UP-04) "Nhập từ Excel": ghép các dòng máy chủ đọc từ tệp (chữ cái cột → giá trị, POST
// /v1/extension/report-tasks/{id}/grid-import) vào lưới nhập của biểu. Dòng xác định xã bằng cột tên xã (fixed_source
// commune_name) và chỉ tiêu bằng cột nhãn chỉ tiêu (indicator_label) nếu mẫu có chỉ tiêu. CHỈ điền ô nhập được
// (`editable` — cột nhập / cột kỳ đang mở); ô cố định, Tỉnh giao, công thức bỏ qua. Không ghi gì: người dùng xem lại rồi Lưu.
import type { FormDefinition } from "@/kn/forms/types";
import { communeRows, type GridCommune, type GridValues } from "./model";

export interface ImportedRow {
  line: number;
  cells: Record<string, string>;
  /** Chữ cái cột của ô SỐ trong .xlsx — giá trị đã là thập phân dấu chấm chuẩn ("1.234" = 1,234), không đọc kiểu VN. */
  numeric?: readonly string[];
}

export interface GridImportResult {
  values: GridValues;
  applied: number;
  /** Dòng tệp không ghép được (số dòng + lý do). */
  skipped: { line: number; reason: string }[];
}

/** Chuẩn hóa tên để so: bỏ dấu, đ → d, chữ thường, bỏ tiền tố "xã/phường/đặc khu", gộp khoảng trắng. */
export function normName(s: string): string {
  return s
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .replace(/đ/gi, "d")
    .toLowerCase()
    .replace(/^(xa|phuong|dac khu|thi tran)\s+/, "")
    .replace(/[^a-z0-9]+/g, " ")
    .trim();
}

/** Ô CHỮ (CSV, ô chữ .xlsx): số kiểu Việt Nam "1.234,5" → "1234.5"; chuỗi khác giữ nguyên (máy chủ kiểm khi tính/nộp).
 * Không dùng cho ô số .xlsx — "1.234" ở đó là một phẩy hai ba bốn, đọc kiểu VN sẽ thành 1234 (Codex P1 #107). */
function normValue(v: string): string {
  const t = v.trim().replace(/\s/g, "");
  if (/^-?\d{1,3}(\.\d{3})+(,\d+)?$/.test(t)) return t.replace(/\./g, "").replace(",", ".");
  if (/^-?\d+,\d+$/.test(t)) return t.replace(",", ".");
  return v.trim();
}

export function mergeGridImport(def: FormDefinition, communes: readonly GridCommune[], current: GridValues, rows: readonly ImportedRow[], editable: (letter: string) => boolean): GridImportResult {
  const communeCol = def.columns.find((c) => c.fixed_source === "commune_name")?.letter;
  const indicatorCol = def.columns.find((c) => c.fixed_source === "indicator_label")?.letter;
  const byName = new Map(communes.map((c) => [normName(c.name), c]));
  const inputCols = new Set(def.columns.filter((c) => c.kind === "input" || c.kind === "per_period").map((c) => c.letter));
  const values: GridValues = { ...current };
  const skipped: GridImportResult["skipped"] = [];
  let applied = 0;
  let lastCommune: GridCommune | undefined;
  for (const r of rows) {
    const rawCommune = communeCol ? (r.cells[communeCol] ?? "") : "";
    // Ô tên xã gộp dọc qua các dòng chỉ tiêu → dòng sau trống tên xã thì dùng xã của dòng trên.
    const c = rawCommune ? byName.get(normName(rawCommune)) : def.rows.indicators.length ? lastCommune : communes.length === 1 ? communes[0] : undefined;
    if (rawCommune) lastCommune = c;
    if (!c) {
      skipped.push({ line: r.line, reason: rawCommune ? "xã không thuộc công việc này" : "không có tên xã" });
      continue;
    }
    const gridRows = communeRows(def, c);
    let key = gridRows[0]!.key;
    if (def.rows.indicators.length) {
      const label = indicatorCol ? (r.cells[indicatorCol] ?? "") : "";
      const ind = def.rows.indicators.find((i) => normName(i.label) === normName(label) || i.code === label.trim());
      const row = ind ? gridRows.find((g) => g.indicator === ind.code) : undefined;
      if (!row) {
        skipped.push({ line: r.line, reason: label ? "chỉ tiêu không có trong biểu" : "không có nhãn chỉ tiêu" });
        continue;
      }
      key = row.key;
    }
    let n = 0;
    const numeric = new Set(r.numeric ?? []);
    for (const [letter, v] of Object.entries(r.cells)) {
      if (!inputCols.has(letter) || !editable(letter)) continue;
      values[key] = { ...(values[key] ?? {}), [letter]: numeric.has(letter) ? v.trim() : normValue(v) };
      n++;
    }
    if (n) applied++;
    else skipped.push({ line: r.line, reason: "không có ô nhập được" });
  }
  return { values, applied, skipped };
}
