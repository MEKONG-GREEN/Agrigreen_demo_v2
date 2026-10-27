// Báo cáo công việc tuần — CNTS Cấp Xã (BF-CN-WEEKLY, PRD UP-06, BRD CNTS UR-XA-09 / BM-05). Quy tắc thuần để kiểm thử
// đơn vị: bảng Thứ Hai–Chủ Nhật, kiểm trước khi gửi (máy chủ kiểm lại — reportcycle/domain.ValidateCNTS), chọn kỳ mặc
// định, gợi ý điền Phần I từ KSGM (BM-01), Nhập liệu tuần (BM-04) và Lịch trực KSGM của tôi.
import type { components } from "@/api/generated/application";
import type { FormsTask } from "../periods/api";

type S = components["schemas"];
export type MyDuty = S["CNTSMyDuty"];

export const PART_I = "CNTS-BM05-I";
export const PART_II = "CNTS-BM05-II";
export const KSGM = "CNTS-BM01";
export const INTAKE = "CNTS-BM04";
export type Part = "I" | "II";
export const codeOf = (p: Part) => (p === "I" ? PART_I : PART_II);

export const WORK_TYPES = ["Làm việc chuyên môn thường xuyên tại xã", "Công tác khác"] as const;
export const WORK_OTHER = WORK_TYPES[1];
/** Bảng T2–CN; 5 ngày đầu bắt buộc. */
export const DAYS: readonly (readonly [string, string])[] = [
  ["t2", "Thứ Hai"],
  ["t3", "Thứ Ba"],
  ["t4", "Thứ Tư"],
  ["t5", "Thứ Năm"],
  ["t6", "Thứ Sáu"],
  ["t7", "Thứ Bảy"],
  ["cn", "Chủ Nhật"],
];
export const WORKDAYS = 5;
export const MAX_CELL = 500;

/** Cột văn bản của từng phần (khóa hậu tố). */
export const TEXT_COLS: Record<Part, readonly { suffix: string; label: string }[]> = {
  I: [{ suffix: "ket_qua", label: "Kết quả thực hiện" }],
  II: [
    { suffix: "ke_hoach", label: "Kế hoạch công việc" },
    { suffix: "ket_qua_du_kien", label: "Kết quả dự kiến" },
  ],
};

/** Mọi khóa trường của một phần (khớp CNTSForms phía máy chủ). */
export function partKeys(part: Part): string[] {
  const extra = part === "II" ? ["dia_diem"] : [];
  return DAYS.flatMap(([k]) => ["loai", "cong_tac_khac", ...TEXT_COLS[part].map((c) => c.suffix), ...extra].map((s) => `${k}_${s}`));
}

export interface DayRow {
  key: string;
  label: string;
  /** yyyy-mm-dd */
  date: string;
  required: boolean;
}

const D = 86_400_000;
const ymd = (t: number) => new Date(t).toISOString().slice(0, 10);

/** Bảy ngày của tuần bắt đầu Thứ Hai `from` (yyyy-mm-dd). */
export function weekDays(from: string): DayRow[] {
  const a = Date.parse(from + "T00:00:00Z");
  return DAYS.map(([key, label], i) => ({ key, label, date: ymd(a + i * D), required: i < WORKDAYS }));
}

/** "21/09" */
export const ddmm = (date: string) => `${date.slice(8, 10)}/${date.slice(5, 7)}`;

/** Lỗi theo trường trước khi gửi (UP-06): bắt buộc T2–T6; T7/CN đã chọn loại thì phải ghi nội dung; "Công tác khác" phải ghi rõ;
 * Phần II đi công tác bắt buộc Địa điểm; mỗi ô ≤ 500 ký tự; sau hạn phải ghi lý do. */
export function validateWeekly(part: Part, values: Record<string, string>, reasonRequired: boolean, reason: string): Record<string, string> {
  const errs: Record<string, string> = {};
  const v = (k: string) => (values[k] ?? "").trim();
  DAYS.forEach(([k], i) => {
    const loai = v(`${k}_loai`);
    const need = i < WORKDAYS || loai !== "";
    if (i < WORKDAYS && !loai) errs[`${k}_loai`] = "Chọn loại công việc.";
    if (loai && !(WORK_TYPES as readonly string[]).includes(loai)) errs[`${k}_loai`] = "Chọn trong danh sách.";
    if (need) for (const c of TEXT_COLS[part]) if (!v(`${k}_${c.suffix}`)) errs[`${k}_${c.suffix}`] = "Bắt buộc nhập.";
    if (loai === WORK_OTHER && !v(`${k}_cong_tac_khac`)) errs[`${k}_cong_tac_khac`] = "Ghi rõ công tác khác.";
    if (part === "II" && loai === WORK_OTHER && !v(`${k}_dia_diem`)) errs[`${k}_dia_diem`] = "Đi công tác: bắt buộc Địa điểm.";
  });
  for (const k of partKeys(part)) if (v(k).length > MAX_CELL) errs[k] = `Tối đa ${MAX_CELL} ký tự.`;
  if (reasonRequired && !reason.trim()) errs.explanation = "Sửa hoặc gửi sau hạn: bắt buộc ghi lý do.";
  return errs;
}

/** Giá trị gửi: chỉ khóa của phần, bỏ ô trống, cắt khoảng trắng. */
export function weeklyPayload(part: Part, values: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const k of partKeys(part)) {
    const x = (values[k] ?? "").trim();
    if (x) out[k] = x;
  }
  return out;
}

/** Quá hạn nộp của kỳ (giờ máy) — lý do bắt buộc (profile kn_cnts RequireLateReason). */
export const pastDue = (t: Pick<FormsTask, "due_at">, now = Date.now()) => now > Date.parse(t.due_at);

/**
 * Kỳ mặc định: Phần I — tuần gần nhất đã kết thúc (kết quả tuần trước), chưa có thì tuần đang chạy; Phần II — tuần
 * gần nhất chưa bắt đầu (kế hoạch tuần sau), chưa có thì tuần mới nhất. `today` yyyy-mm-dd.
 */
export function pickTask(tasks: FormsTask[], part: Part, today: string): FormsTask | null {
  const mine = tasks.filter((t) => t.template_code === codeOf(part) && t.period_from).sort((a, b) => (a.period_from! < b.period_from! ? 1 : -1));
  if (!mine.length) return null;
  if (part === "I") return mine.find((t) => (t.period_to ?? t.period_from!) < today) ?? mine.find((t) => t.period_from! <= today) ?? mine[mine.length - 1]!;
  const future = mine.filter((t) => t.period_from! > today);
  return future[future.length - 1] ?? mine[0]!;
}

const SHIFT: Record<MyDuty["shift"], string> = { N: "ca ngày", D: "ca đêm", ND: "cả ngày đêm" };
const KSGM_FIELDS: [string, string][] = [
  ["trau_bo", "trâu bò"],
  ["heo", "heo"],
  ["gia_cam", "gia cầm"],
  ["de", "dê"],
];

/** Ngày giờ địa phương (yyyy-mm-dd) của một thời điểm ISO. */
function localDay(iso: string): string {
  const d = new Date(iso);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/**
 * Gợi ý Phần I theo ngày (CV-03 "gợi ý từ BM-01, BM-04, lịch trực đã nhập trong ngày"):
 *  - KSGM: bài BM-01 đã nộp của cơ sở thuộc xã, kỳ ngày = ngày đó;
 *  - Nhập liệu tuần: bài BM-04 của xã gửi trong ngày đó;
 *  - Lịch trực: ca trực KSGM của tôi trong ngày.
 */
export function buildSuggestions(
  days: DayRow[],
  unit: string,
  ksgm: FormsTask[],
  intake: FormsTask[],
  duties: MyDuty[],
  facilityName: (t: FormsTask) => string,
): Record<string, string[]> {
  const out: Record<string, string[]> = {};
  for (const d of days) {
    const lines: string[] = [];
    for (const du of duties.filter((x) => x.date === d.date)) lines.push(`Trực KSGM ${SHIFT[du.shift]} tại ${du.facility.name ?? du.facility.code ?? "cơ sở giết mổ"}`);
    for (const t of ksgm.filter((x) => x.template_code === KSGM && x.parent === unit && x.period_from === d.date && x.version > 0)) {
      const counts = t.nil_report ? "không hoạt động" : KSGM_FIELDS.filter(([k]) => (t.values[k] ?? "") !== "").map(([k, l]) => `${l} ${t.values[k]}`).join(", ");
      lines.push(`KSGM ${facilityName(t)}: ${counts || "đã báo cáo"}`);
    }
    for (const t of intake.filter((x) => x.template_code === INTAKE && x.unit === unit && x.submitted_at && localDay(x.submitted_at) === d.date))
      lines.push(`Gửi Nhập liệu tuần chăn nuôi (${t.label})`);
    if (lines.length) out[d.key] = lines;
  }
  return out;
}

/** Điền gợi ý vào ô Kết quả còn trống (không ghi đè nội dung đã nhập); ngày chưa chọn loại → "Làm việc chuyên môn…". */
export function applySuggestions(values: Record<string, string>, sugg: Record<string, string[]>): { values: Record<string, string>; filled: number } {
  const next = { ...values };
  let filled = 0;
  for (const [k, lines] of Object.entries(sugg)) {
    if ((next[`${k}_ket_qua`] ?? "").trim()) continue;
    next[`${k}_ket_qua`] = lines.join("; ").slice(0, MAX_CELL);
    if (!(next[`${k}_loai`] ?? "").trim()) next[`${k}_loai`] = WORK_TYPES[0];
    filled++;
  }
  return { values: next, filled };
}
