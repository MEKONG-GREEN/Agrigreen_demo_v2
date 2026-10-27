// Khai thác thủy sản tuần (BF-CN-CATCH #112 — UP-09): dựng 4 sheet của biểu gốc "ĐÁNH BẮT (nhập liệu theo xã).xlsx" (cá các
// loại, tôm các loại, mực, thủy sản khác) từ công việc BM-17 (/v1/forms/tasks, profile kn_cnts, mã CNTS-BM17). Mỗi sheet: dòng =
// xã/phường, cột = "Trong kỳ báo cáo (tuần …) (Tấn)", dòng TỔNG cộng các xã. Số liệu một tuần của một xã là MỘT bài nộp BM-17
// gồm cả 4 nhóm — nhập ở sheet nào cũng vào cùng bài. Hàm thuần để kiểm thử đơn vị.
import { Decimal } from "@/lib/exactDecimal";
import type { FormsTask } from "../periods/api";

export const CATCH_CODE = "CNTS-BM17";

export interface CatchSheet {
  /** Khóa trường của BM-17. */
  key: "ca" | "tom" | "muc" | "khac";
  /** Tên sheet trong tệp gốc. */
  tab: string;
  /** Tiêu đề sheet (ô A1 của tệp gốc). */
  heading: string;
}

export const SHEETS: CatchSheet[] = [
  { key: "ca", tab: "Đánh bắt cá các loại", heading: "SẢN LƯỢNG ĐÁNH BẮT CÁ CÁC LOẠI" },
  { key: "tom", tab: "Đánh bắt tôm các loại", heading: "SẢN LƯỢNG ĐÁNH BẮT TÔM CÁC LOẠI" },
  { key: "muc", tab: "Đánh bắt mực", heading: "SẢN LƯỢNG ĐÁNH BẮT MỰC" },
  { key: "khac", tab: "Đánh bắt thủy sản khác", heading: "SẢN LƯỢNG ĐÁNH BẮT THỦY SẢN KHÁC" },
];

export interface CatchWeek {
  periodId: string;
  label: string;
  from: string;
  dueAt: string;
}

export interface CatchMatrix {
  /** Tuần (kỳ BM-17), cũ → mới. */
  weeks: CatchWeek[];
  /** Mã xã/phường có công việc BM-17. */
  units: string[];
  /** Công việc theo `${periodId}|${unit}`. */
  cell: (periodId: string, unit: string) => FormsTask | undefined;
}

const k = (periodId: string, unit: string) => `${periodId}|${unit}`;

/** Gom công việc BM-17 thành ma trận xã × tuần; `limit` = số tuần gần nhất (0 = tất cả). */
export function catchMatrix(tasks: FormsTask[], limit = 0): CatchMatrix {
  const bm17 = tasks.filter((t) => t.template_code === CATCH_CODE && t.period_status !== "cancelled");
  const weeks = new Map<string, CatchWeek>();
  const units = new Set<string>();
  const cells = new Map<string, FormsTask>();
  for (const t of bm17) {
    if (!weeks.has(t.period_id)) weeks.set(t.period_id, { periodId: t.period_id, label: t.label, from: t.period_from ?? "", dueAt: t.due_at });
    units.add(t.unit);
    cells.set(k(t.period_id, t.unit), t);
  }
  let ws = [...weeks.values()].sort((a, b) => a.from.localeCompare(b.from) || a.label.localeCompare(b.label));
  if (limit > 0) ws = ws.slice(-limit);
  return { weeks: ws, units: [...units].sort(), cell: (p, u) => cells.get(k(p, u)) };
}

/** Ô của sheet: số đã nộp, "0" khi báo không phát sinh, null khi chưa nộp. */
export function sheetValue(t: FormsTask | undefined, key: CatchSheet["key"]): string | null {
  if (!t || t.version === 0) return null;
  if (t.nil_report) return "0";
  const v = t.values[key];
  return v === undefined || v === "" ? null : v;
}

/** Dòng TỔNG của một tuần trong sheet (cộng chính xác thập phân; null khi chưa xã nào nộp). */
export function sheetTotal(m: CatchMatrix, periodId: string, key: CatchSheet["key"]): string | null {
  let sum: Decimal | null = null;
  for (const u of m.units) {
    const v = sheetValue(m.cell(periodId, u), key);
    const d = v === null ? null : Decimal.parse(v);
    if (d) sum = (sum ?? Decimal.ZERO).add(d);
  }
  return sum ? sum.toString() : null;
}

/** Tổng 4 nhóm của một xã trong tuần (dòng "Sản lượng khai thác" của Phụ lục 2). */
export function unitTotal(t: FormsTask | undefined): string | null {
  let sum: Decimal | null = null;
  for (const s of SHEETS) {
    const v = sheetValue(t, s.key);
    const d = v === null ? null : Decimal.parse(v);
    if (d) sum = (sum ?? Decimal.ZERO).add(d);
  }
  return sum ? sum.toString() : null;
}

/** Tuần nhập được của Cấp Xã: kỳ đang nhận bài có ít nhất một xã, mới nhất trước. */
export function editableWeeks(m: CatchMatrix): CatchWeek[] {
  return m.weeks.filter((w) => m.units.some((u) => m.cell(w.periodId, u)?.open)).reverse();
}

/** Giá trị nháp ban đầu của một xã trong tuần: số đã nộp, chưa nộp thì 0 (ô số mặc định 0). */
export function draftOf(t: FormsTask): Record<CatchSheet["key"], string> {
  const out = {} as Record<CatchSheet["key"], string>;
  for (const s of SHEETS) out[s.key] = t.version > 0 && !t.nil_report ? (t.values[s.key] ?? "") : "0";
  return out;
}
