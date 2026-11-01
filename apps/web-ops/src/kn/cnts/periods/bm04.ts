// Số liệu cấp xã › Nhập liệu tuần — Cấp Tỉnh (BF-CN-XA-DATA #175, BRD CNTS v3.7 FN-09 BM-04): gom công việc BM-04
// (/v1/forms/tasks, profile kn_cnts, mã CNTS-BM04 — máy chủ lọc theo phạm vi người gọi) thành bảng xã × tuần; lũy kế
// BR-01 (tổng các kỳ có số liệu hợp lệ từ đầu năm/quý tới tuần đang xem, kỳ không nộp tính 0 và hiện "Thiếu kỳ");
// lịch sử phiên bản (cũ → mới) và cờ "Đã sửa sau xác thực" (AC-06). Hàm thuần để kiểm thử đơn vị.
import { Decimal } from "@/lib/exactDecimal";
import type { FormsField, FormsSubmission, FormsTask } from "./api";

export const BM04_CODE = "CNTS-BM04";

/**
 * Trường lũy kế (BR-01): số phát sinh trong tuần — tiêm phòng (CN-02), hóa chất (CN-04), KSGM (CN-06). Tổng đàn (CN-01)
 * và vịt chạy đồng (CN-08/09) là số hiện có tại thời điểm báo cáo, cộng dồn theo tuần không có nghĩa → chỉ hiện số tuần.
 */
export const FLOW_PREFIXES = ["tiem_", "hoa_chat_", "ksgm_"] as const;
export const isFlow = (key: string) => FLOW_PREFIXES.some((p) => key.startsWith(p));

export const isNumeric = (f: FormsField) => f.type === "integer" || f.type === "decimal";

export interface Bm04Week {
  /** Khóa tuần (period_key, vd "2026-W40") — các lô phát hành khác nhau của cùng tuần gộp một cột. */
  key: string;
  label: string;
  from: string;
  to: string;
  dueAt: string;
}

export interface Bm04Data {
  /** Tuần, cũ → mới. */
  weeks: Bm04Week[];
  /** Mã xã có công việc BM-04. */
  units: string[];
  /** Trường số của biểu (đúng thứ tự mẫu, AC-01). */
  fields: FormsField[];
  /**
   * Cột bảng tuần (chế độ Số liệu tuần): trường số và trường chữ của biểu theo đúng thứ tự mẫu — gồm "CN-10 Ghi chú" ở cột
   * cuối biểu (BRD CNTS v3.7 UR-XA-07, FN-09 AC-01). Trường chữ không cộng ở dòng TỔNG, không có lũy kế.
   */
  columns: FormsField[];
  /** Công việc theo `${weekKey}|${unit}`. */
  cell: (weekKey: string, unit: string) => FormsTask | undefined;
}

const k = (week: string, unit: string) => `${week}|${unit}`;

/** Gom công việc BM-04 (bỏ kỳ đã hủy). Cùng tuần cùng xã có hai kỳ (hai lô) → giữ kỳ có bài nộp mới hơn. */
export function bm04Data(tasks: FormsTask[]): Bm04Data {
  const ts = tasks.filter((t) => t.template_code === BM04_CODE && t.period_status !== "cancelled");
  const weeks = new Map<string, Bm04Week>();
  const units = new Set<string>();
  const cells = new Map<string, FormsTask>();
  for (const t of ts) {
    const wk = t.period_key || t.period_id;
    if (!weeks.has(wk)) weeks.set(wk, { key: wk, label: t.label, from: t.period_from ?? "", to: t.period_to ?? "", dueAt: t.due_at });
    units.add(t.unit);
    const cur = cells.get(k(wk, t.unit));
    if (!cur || (t.submitted_at ?? "") > (cur.submitted_at ?? "")) cells.set(k(wk, t.unit), t);
  }
  const all = ts.find((t) => t.fields.length)?.fields ?? [];
  const fields = all.filter(isNumeric);
  return {
    weeks: [...weeks.values()].sort((a, b) => a.from.localeCompare(b.from) || a.key.localeCompare(b.key)),
    units: [...units].sort(),
    fields,
    columns: all.filter((f) => isNumeric(f) || f.type === "text"),
    cell: (w, u) => cells.get(k(w, u)),
  };
}

/** Bài nộp của tuần dùng được cho số liệu (đã gửi, không bị trả lại chờ điều chỉnh). */
export function hasValid(t: FormsTask | undefined): t is FormsTask {
  return !!t && t.version > 0 && t.status !== "returned" && t.status !== "awaiting_reopen";
}

/** Giá trị một ô của tuần: số đã nộp, "0" khi báo không phát sinh, null khi chưa nộp. */
export function cellValue(t: FormsTask | undefined, key: string): string | null {
  if (!t || t.version === 0) return null;
  if (t.nil_report) return "0";
  const v = t.values[key];
  return v === undefined || v === "" ? null : v;
}

/** Giá trị trường chữ (vd CN-10 Ghi chú) của tuần: nguyên văn đã nộp, null khi chưa nộp/để trống — không quy "0" khi báo không phát sinh. */
export function cellText(t: FormsTask | undefined, key: string): string | null {
  if (!t || t.version === 0) return null;
  const v = t.values[key];
  return v === undefined || v.trim() === "" ? null : v;
}

export type CumBasis = "year" | "quarter";

/** Năm, quý của tuần (theo ngày cuối tuần — tuần vắt năm tính vào năm của khóa "YYYY-Www"). */
function yq(w: Bm04Week): [number, number] {
  const d = w.to || w.from;
  const dy = Number(d.slice(0, 4));
  const m = /^(\d{4})-W/.exec(w.key);
  const year = m ? Number(m[1]) : dy;
  if (year !== dy) return [year, year < dy ? 4 : 1];
  return [year, Math.ceil((Number(d.slice(5, 7)) || 1) / 3)];
}

export interface Cumulative {
  /** Lũy kế theo trường (null khi chưa kỳ nào có số). */
  values: Record<string, string | null>;
  /** Tuần có công việc nhưng chưa nộp (tính 0 — "Thiếu kỳ"). */
  missing: Bm04Week[];
  /** Tuần bị trả lại, chờ điều chỉnh (chưa cộng). */
  returned: Bm04Week[];
  /** Số tuần đã cộng. */
  counted: number;
}

/** Lũy kế BR-01 của một xã tới tuần `weekKey` (gồm tuần đó), từ đầu năm hoặc đầu quý của tuần. */
export function cumulative(d: Bm04Data, unit: string, weekKey: string, basis: CumBasis = "year"): Cumulative {
  const at = d.weeks.find((w) => w.key === weekKey);
  const out: Cumulative = { values: {}, missing: [], returned: [], counted: 0 };
  const flows = d.fields.filter((f) => isFlow(f.key));
  for (const f of flows) out.values[f.key] = null;
  if (!at) return out;
  const [y, q] = yq(at);
  const sums = new Map<string, Decimal>();
  for (const w of d.weeks) {
    if (w.from > at.from) break;
    const [wy, wq] = yq(w);
    if (wy !== y || (basis === "quarter" && wq !== q)) continue;
    const t = d.cell(w.key, unit);
    if (!t) continue; // xã không được giao tuần này
    if (t.version === 0) {
      out.missing.push(w);
      continue;
    }
    if (!hasValid(t)) {
      out.returned.push(w);
      continue;
    }
    out.counted++;
    for (const f of flows) {
      const v = cellValue(t, f.key);
      const n = v === null ? null : Decimal.parse(v);
      if (n) sums.set(f.key, (sums.get(f.key) ?? Decimal.ZERO).add(n));
    }
  }
  for (const f of flows) out.values[f.key] = sums.get(f.key)?.toString() ?? null;
  return out;
}

/** Dòng TỔNG của một cột (cộng chính xác; null khi chưa xã nào có số). */
export function columnTotal(values: (string | null)[]): string | null {
  let sum: Decimal | null = null;
  for (const v of values) {
    const n = v === null ? null : Decimal.parse(v);
    if (n) sum = (sum ?? Decimal.ZERO).add(n);
  }
  return sum ? sum.toString() : null;
}

/** Cờ AC-06: đã có phiên bản cũ được xác thực mà phiên bản hiện hành chưa được xác thực lại. `subs` mới trước (API). */
export function editedAfterAccept(subs: FormsSubmission[]): boolean {
  if (subs.length < 2) return false;
  const [cur, ...older] = subs;
  return cur!.status !== "accepted" && older.some((s) => s.status === "accepted" || s.review?.decision === "accepted");
}

export interface Change {
  key: string;
  label: string;
  old: string | null;
  new: string | null;
}

/** Thay đổi giữa hai phiên bản liên tiếp (cũ → mới) — "Lịch sử sửa hiện giá trị cũ → mới" (AC-05). */
export function changesBetween(fields: FormsField[], older: FormsSubmission | undefined, newer: FormsSubmission): Change[] {
  const val = (s: FormsSubmission | undefined, key: string) => {
    if (!s) return null;
    const v = s.nil_report && fields.find((f) => f.key === key && isNumeric(f)) ? "0" : s.values[key];
    return v === undefined || v === "" ? null : v;
  };
  return fields
    .map((f) => ({ key: f.key, label: f.group ? `${f.group} › ${f.label}` : f.label, old: val(older, f.key), new: val(newer, f.key) }))
    .filter((c) => c.old !== c.new);
}

/**
 * Trường dùng cho lịch sử phiên bản (AC-05): đủ định nghĩa trường của kỳ — số và chữ (gồm CN-10 Ghi chú) theo thứ tự mẫu —
 * tách khỏi `Bm04Data.fields` (chỉ trường số, dùng cho TỔNG/lũy kế). Kỳ không kèm định nghĩa thì dùng cột của biểu.
 */
export function historyFields(task: FormsTask, columns: FormsField[]): FormsField[] {
  const own = task.fields.filter((f) => isNumeric(f) || f.type === "text");
  return own.length ? own : columns;
}

/** Nhóm cột (giữ thứ tự xuất hiện) cho hàng tiêu đề gộp. */
export function columnGroups(fields: FormsField[]): { group: string; span: number }[] {
  const out: { group: string; span: number }[] = [];
  for (const f of fields) {
    const g = f.group ?? "";
    const last = out.at(-1);
    if (last && last.group === g) last.span++;
    else out.push({ group: g, span: 1 });
  }
  return out;
}
