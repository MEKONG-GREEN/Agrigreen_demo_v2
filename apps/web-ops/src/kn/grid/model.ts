// Mô hình thuần của lưới mẫu báo cáo (không React): khung tiêu đề gộp nhiều tầng, độ rộng & cột cố định,
// vai trò ô (cố định / Tỉnh giao / nhập / kỳ mở / kỳ đóng / công thức), dòng theo xã × chỉ tiêu, dòng tổng
// Khu vực / Toàn tỉnh, dán khối từ Excel, chuẩn hóa nhập liệu và hiển thị vi-VN (không float).
import type { FormCellIssue, FormColumn, FormDefinition, FormEvalInput, FormEvalRow, FormTotalRow } from "@/kn/forms/types";
import { formatDateVi, formatDecimalVi, normalizeDateInput, normalizeNumberInput } from "./decimal";
import { columnIndex, parseRect } from "./letters";

export interface GridCommune {
  code: string;
  name: string;
  /** Mã khu vực (dòng tổng "Cộng khu vực"). */
  region?: string;
  regionName?: string;
}

export type GridRowKind = "data" | "total";

export interface GridRow {
  key: string;
  kind: GridRowKind;
  commune?: string;
  region?: string;
  indicator?: string;
  /** Dòng tổng: "region" | "province". */
  totalKind?: "region" | "province";
}

/** rowKey → chữ cái → giá trị chuỗi (decimal dạng máy chủ "38.5", ngày "YYYY-MM-DD", chữ). */
export type GridValues = Record<string, Record<string, string>>;

export interface CellChange {
  rowKey: string;
  letter: string;
  value: string;
}

export function rowKeyOf(commune: string, indicator?: string): string {
  return indicator ? `${commune}/${indicator}` : commune;
}

// ---------------------------------------------------------------------------
// Khung tiêu đề
// ---------------------------------------------------------------------------

export interface HeaderCellModel {
  /** Dòng tiêu đề (từ 1) và cột (từ 1) của ô góc trên trái. */
  row: number;
  col: number;
  text: string;
  colSpan: number;
  rowSpan: number;
}

/**
 * Ma trận tiêu đề: mỗi dòng một danh sách ô cần vẽ (ô bị vùng gộp che không có mặt). Vùng gộp lấy chữ ở
 * ô góc trên trái (như Excel); ô không có chữ trong khung tiêu đề vẫn được vẽ (rỗng).
 */
export function buildHeader(def: FormDefinition): HeaderCellModel[][] {
  const n = def.columns.length;
  const H = Math.max(1, def.header.rows);
  const covered = new Set<string>();
  const spans = new Map<string, { colSpan: number; rowSpan: number }>();
  for (const m of def.header.merges) {
    const r = parseRect(m);
    if (!r || r.r2 > H || r.c2 > n) continue;
    spans.set(`${r.r1}:${r.c1}`, { colSpan: r.c2 - r.c1 + 1, rowSpan: r.r2 - r.r1 + 1 });
    for (let rr = r.r1; rr <= r.r2; rr++) for (let cc = r.c1; cc <= r.c2; cc++) if (rr !== r.r1 || cc !== r.c1) covered.add(`${rr}:${cc}`);
  }
  const text = new Map<string, string>();
  for (const c of def.header.cells) text.set(`${c.row}:${columnIndex(c.letter)}`, c.text);
  const out: HeaderCellModel[][] = [];
  for (let r = 1; r <= H; r++) {
    const row: HeaderCellModel[] = [];
    for (let c = 1; c <= n; c++) {
      if (covered.has(`${r}:${c}`)) continue;
      const sp = spans.get(`${r}:${c}`) ?? { colSpan: 1, rowSpan: 1 };
      // Ô không gộp, không chữ ở dòng cuối → dùng tiêu đề cột (định nghĩa tạo bằng tay ít tiêu đề).
      let t = text.get(`${r}:${c}`) ?? "";
      if (!t && r === H && sp.rowSpan === 1 && !hasTextAbove(text, covered, c, r)) t = def.columns[c - 1]?.title ?? "";
      row.push({ row: r, col: c, text: t, ...sp });
    }
    out.push(row);
  }
  return out;
}

function hasTextAbove(text: Map<string, string>, covered: Set<string>, c: number, r: number): boolean {
  for (let rr = 1; rr < r; rr++) if (text.get(`${rr}:${c}`) && !covered.has(`${rr}:${c}`)) return true;
  return false;
}

// ---------------------------------------------------------------------------
// Độ rộng, cố định cột
// ---------------------------------------------------------------------------

export function columnWidth(col: FormColumn): number {
  if (col.kind === "fixed") {
    switch (col.fixed_source) {
      case "row_no":
        return 40;
      case "region_name":
        return 120;
      case "commune_name":
        return 130;
      case "indicator_label":
        return 170;
    }
  }
  if (col.dtype === "date") return 96;
  if (col.dtype === "text" || col.dtype === "catalog") return 120;
  return 72;
}

export interface FreezeModel {
  /** Số cột cố định (A…freeze_through). */
  count: number;
  /** left (px) của từng cột cố định. */
  lefts: number[];
  width: number;
}

export function freezeModel(def: FormDefinition, widths: number[]): FreezeModel {
  const count = def.freeze_through ? Math.min(columnIndex(def.freeze_through), def.columns.length) : 0;
  const lefts: number[] = [];
  let x = 0;
  for (let i = 0; i < count; i++) {
    lefts.push(x);
    x += widths[i] ?? 0;
  }
  return { count, lefts, width: x };
}

// ---------------------------------------------------------------------------
// Vai trò ô
// ---------------------------------------------------------------------------

export type CellRole = "fixed" | "provincial" | "input" | "open" | "closed" | "formula" | "inactive";

/** Khe kỳ của cột (cột theo kỳ hoặc công thức trong khối kỳ), 0 nếu không thuộc khối. */
export function slotOf(col: FormColumn): number {
  return col.period_block ? (col.period_slot ?? 0) : 0;
}

/**
 * Vai trò ô dữ liệu: openSlots = null/undefined → mọi cột theo kỳ coi như mở (xem trước của trình thiết kế);
 * mảng → chỉ khe có trong mảng nhập được ("Cột tuần đang mở được nhập; các cột tuần khác chỉ xem").
 */
export function cellRole(col: FormColumn, openSlots?: readonly number[] | null): CellRole {
  if (col.inactive) return "inactive";
  switch (col.kind) {
    case "fixed":
      return "fixed";
    case "provincial":
      return "provincial";
    case "input":
      return "input";
    case "formula":
      return "formula";
    case "per_period":
      return !openSlots || openSlots.includes(col.period_slot ?? 0) ? "open" : "closed";
  }
}

export function isEditableRole(role: CellRole): boolean {
  return role === "input" || role === "open";
}

// ---------------------------------------------------------------------------
// Dòng
// ---------------------------------------------------------------------------

/** Dòng nhập của một xã: một dòng, hoặc mỗi chỉ tiêu một dòng. */
export function communeRows(def: FormDefinition, c: GridCommune): GridRow[] {
  const inds = def.rows.indicators;
  if (!inds.length) return [{ key: rowKeyOf(c.code), kind: "data", commune: c.code, region: c.region }];
  return inds.map((i) => ({ key: rowKeyOf(c.code, i.code), kind: "data", commune: c.code, region: c.region, indicator: i.code }));
}

/**
 * Bảng tất cả các xã (chỉ xem): các xã theo khu vực, sau mỗi khu vực là dòng "Cộng khu vực" (mỗi chỉ tiêu
 * một dòng nếu mẫu có chỉ tiêu), cuối cùng "Toàn tỉnh" — theo định nghĩa totals của mẫu.
 */
export function allCommuneRows(def: FormDefinition, communes: readonly GridCommune[]): GridRow[] {
  const out: GridRow[] = [];
  const inds = def.rows.indicators.length ? def.rows.indicators.map((i) => i.code) : [""];
  const hasRegion = def.totals.some((t) => t.kind === "region");
  const hasProvince = def.totals.some((t) => t.kind === "province");
  const order: string[] = [];
  const byRegion = new Map<string, GridCommune[]>();
  for (const c of communes) {
    const r = c.region ?? "";
    if (!byRegion.has(r)) {
      byRegion.set(r, []);
      order.push(r);
    }
    byRegion.get(r)!.push(c);
  }
  for (const r of order) {
    for (const c of byRegion.get(r)!) out.push(...communeRows(def, c));
    if (hasRegion && r) {
      for (const ind of inds) out.push({ key: `region:${r}${ind ? `/${ind}` : ""}`, kind: "total", totalKind: "region", region: r, indicator: ind || undefined });
    }
  }
  if (hasProvince) for (const ind of inds) out.push({ key: `province${ind ? `/${ind}` : ""}`, kind: "total", totalKind: "province", indicator: ind || undefined });
  return out;
}

// ---------------------------------------------------------------------------
// Đầu vào tính (POST /v1/reports/form-versions/{id}/evaluate)
// ---------------------------------------------------------------------------

export interface EvalInputOptions {
  mode: "draft" | "submit";
  /** null = tính tạm, không kiểm kỳ (gửi mọi giá trị, kể cả kỳ đã đóng — để lũy kế đúng). */
  openSlots: number[] | null;
  /** Ô hệ thống thêm (Tỉnh giao, cố định khác) theo dòng. */
  system?: GridValues;
  xrefs?: Record<string, Record<string, Record<string, string>>>;
}

/**
 * Dựng đầu vào tính: ô nhập của người dùng → input; tên xã/khu vực → system (cột cố định) để máy chủ
 * điền đúng chữ. Khi kiểm để gửi (openSlots là mảng) bỏ giá trị của kỳ đã đóng (do kỳ trước đã nộp).
 */
export function buildEvalInput(def: FormDefinition, communes: readonly GridCommune[], values: GridValues, o: EvalInputOptions): FormEvalInput {
  const rows: FormEvalRow[] = [];
  const closed = new Set<string>();
  if (o.openSlots) for (const c of def.columns) if (c.kind === "per_period" && !o.openSlots.includes(c.period_slot ?? 0)) closed.add(c.letter);
  const editable = new Set(def.columns.filter((c) => c.kind === "input" || c.kind === "per_period").map((c) => c.letter));
  for (const c of communes) {
    for (const r of communeRows(def, c)) {
      const input: Record<string, string> = {};
      for (const [l, v] of Object.entries(values[r.key] ?? {})) {
        if (v === "" || !editable.has(l) || closed.has(l)) continue;
        input[l] = v;
      }
      const system: Record<string, string> = { ...(o.system?.[r.key] ?? o.system?.[c.code] ?? {}) };
      for (const col of def.columns) {
        if (col.kind !== "fixed" || system[col.letter]) continue;
        if (col.fixed_source === "commune_name") system[col.letter] = c.name;
        else if (col.fixed_source === "region_name" && (c.regionName || c.region)) system[col.letter] = c.regionName ?? c.region ?? "";
      }
      const row: FormEvalRow = { key: r.key, commune: c.code, input };
      if (c.region) row.region = c.region;
      if (r.indicator) row.indicator = r.indicator;
      if (Object.keys(system).length) row.system = system;
      const xr = o.xrefs?.[c.code];
      if (xr) row.xrefs = xr;
      rows.push(row);
    }
  }
  return { mode: o.mode, open_slots: o.openSlots, rows };
}

/** Ô hiển thị theo dòng: kết quả tính của máy chủ (dòng dữ liệu + dòng tổng), nhãn khu vực đổi sang tên. */
export function computedCells(rows: { key: string; cells: Record<string, string> }[], totals: FormTotalRow[], def: FormDefinition, communes: readonly GridCommune[]): GridValues {
  const out: GridValues = {};
  for (const r of rows) out[r.key] = r.cells;
  const regionName = new Map(communes.filter((c) => c.region).map((c) => [c.region!, c.regionName ?? c.region!]));
  for (const t of totals) {
    const cells = { ...t.cells };
    const def0 = def.totals.find((x) => x.kind === t.kind);
    if (def0 && t.region) {
      const l = def0.label_letter.toUpperCase();
      let label = def0.label.replaceAll("{region}", regionName.get(t.region) ?? t.region);
      if (t.indicator) {
        const ind = def.rows.indicators.find((i) => i.code === t.indicator);
        if (ind) label += ` – ${ind.label}`;
      }
      cells[l] = label;
    }
    out[t.key] = cells;
  }
  return out;
}

// ---------------------------------------------------------------------------
// Nhập liệu, hiển thị
// ---------------------------------------------------------------------------

/** Chuẩn hóa chuỗi người gõ/dán theo kiểu dữ liệu của cột (không float). */
export function normalizeCellInput(col: FormColumn, raw: string): string {
  const s = raw.replace(/\r/g, "").trim();
  if (s === "") return "";
  if (col.dtype === "number" || col.dtype === "integer") return normalizeNumberInput(s);
  if (col.dtype === "date") return normalizeDateInput(s);
  return s;
}

/** Chuỗi hiển thị vi-VN của một ô. Công thức/tổng: tối đa `decimals` (mặc định 2) chữ số thập phân. */
export function displayCell(col: FormColumn, value: string | undefined, computed: boolean): string {
  if (value === undefined || value === "") return "";
  if (col.dtype === "number" || col.dtype === "integer") {
    if (!/^[+-]?(\d+(\.\d*)?|\.\d+)$/.test(value)) return value; // #DIV/0!, chữ
    const max = col.decimals ?? (computed || col.kind === "formula" ? 2 : undefined);
    const s = formatDecimalVi(value, { maxDecimals: max, minDecimals: col.decimals ?? undefined });
    return col.unit === "%" ? `${s}%` : s;
  }
  if (col.dtype === "date") return formatDateVi(value);
  return value;
}

/** Giá trị đưa vào ô sửa: số dạng vi-VN không phân cách nghìn ("38,5"), ngày dd/mm/yyyy. */
export function editText(col: FormColumn, value: string | undefined): string {
  if (!value) return "";
  if (col.dtype === "number" || col.dtype === "integer") return formatDecimalVi(value, { group: false });
  if (col.dtype === "date") return formatDateVi(value);
  return value;
}

// ---------------------------------------------------------------------------
// Dán khối từ Excel
// ---------------------------------------------------------------------------

/** Văn bản clipboard của Excel (TSV, dòng CRLF, ô có xuống dòng trong nháy kép) → ma trận chuỗi. */
export function parseClipboard(textIn: string): string[][] {
  const text = textIn.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = "";
  let i = 0;
  let quoted = false;
  while (i < text.length) {
    const ch = text[i]!;
    if (quoted) {
      if (ch === '"') {
        if (text[i + 1] === '"') {
          cell += '"';
          i += 2;
          continue;
        }
        quoted = false;
        i++;
        continue;
      }
      cell += ch;
      i++;
      continue;
    }
    if (ch === '"' && cell === "") {
      quoted = true;
      i++;
    } else if (ch === "\t") {
      row.push(cell);
      cell = "";
      i++;
    } else if (ch === "\n") {
      row.push(cell);
      rows.push(row);
      row = [];
      cell = "";
      i++;
    } else {
      cell += ch;
      i++;
    }
  }
  if (cell !== "" || row.length) {
    row.push(cell);
    rows.push(row);
  }
  return rows;
}

export interface PasteResult {
  changes: CellChange[];
  /** Số ô bị bỏ qua vì không nhập được (cố định, Tỉnh giao, công thức, kỳ đóng) hoặc ra ngoài lưới. */
  skipped: number;
}

/** Dán ma trận bắt đầu tại (rowIdx, colIdx) lên các dòng dữ liệu nhập được; ô không nhập được bị bỏ qua. */
export function applyPaste(
  matrix: string[][],
  start: { row: number; col: number },
  rows: readonly GridRow[],
  columns: readonly FormColumn[],
  editable: (row: GridRow, col: FormColumn) => boolean,
): PasteResult {
  const changes: CellChange[] = [];
  let skipped = 0;
  matrix.forEach((line, dr) => {
    line.forEach((raw, dc) => {
      const row = rows[start.row + dr];
      const col = columns[start.col + dc];
      if (!row || !col || !editable(row, col)) {
        skipped++;
        return;
      }
      changes.push({ rowKey: row.key, letter: col.letter, value: normalizeCellInput(col, raw) });
    });
  });
  return { changes, skipped };
}

// ---------------------------------------------------------------------------
// Vấn đề tại ô
// ---------------------------------------------------------------------------

export function issueIndex(issues: readonly FormCellIssue[] | undefined): Map<string, FormCellIssue[]> {
  const m = new Map<string, FormCellIssue[]>();
  for (const it of issues ?? []) {
    const k = `${it.row_key}|${it.letter ?? ""}`;
    const list = m.get(k) ?? [];
    list.push(it);
    m.set(k, list);
  }
  return m;
}

export const ISSUE_LABEL: Readonly<Record<FormCellIssue["code"], string>> = {
  "reports.row_invalid": "Dòng không hợp lệ (thiếu mã xã, trùng dòng hoặc chỉ tiêu không có trong mẫu)",
  "reports.cell_unknown_column": "Cột không có trong mẫu",
  "reports.cell_not_editable": "Cột cố định, Tỉnh giao hoặc công thức — không nhập tay",
  "reports.cell_period_closed": "Chỉ cột của kỳ đang mở được nhập",
  "reports.cell_invalid_type": "Sai kiểu dữ liệu",
  "reports.cell_out_of_range": "Ngoài giới hạn của mẫu",
  "reports.cell_required": "Bắt buộc nhập",
  "reports.formula_error": "Công thức không tính được",
  "reports.rule_failed": "Không đạt ràng buộc của mẫu",
  "reports.rule_error": "Ràng buộc không tính được",
  "reports.form_invalid": "Định nghĩa mẫu không hợp lệ",
};
