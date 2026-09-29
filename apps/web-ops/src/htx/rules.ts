// Quy tắc kiểm tra phía client cho màn hình App HTX (pf/cropcycle, pf/accounts).
// CHỈ để báo lỗi sớm — backend vẫn kiểm lại mọi quy tắc và là nguồn sự thật (R-55).
import type { CropActivity, CropSOPStep } from "@/api/types";

export const MSG_STEP_ORDER = "Thứ tự bước không hợp lệ, vui lòng kiểm tra lại.";
export const MSG_HARVEST_STEP = "Quy trình phải có bước thu hoạch để kế hoạch sản xuất có điểm kết thúc.";
export const MSG_SOWING_WINDOW = "Ngày gieo sạ phải trong khoảng 15 ngày trước đến 30 ngày sau ngày hiện tại.";

export const ACTIVITY_LABEL: Record<CropActivity, string> = {
  lam_dat: "Làm đất",
  gieo_sa: "Gieo sạ",
  tuoi_nuoc: "Tưới nước",
  bon_phan: "Bón phân",
  phun_thuoc: "Phun thuốc",
  kiem_tra: "Kiểm tra đồng ruộng",
  thu_hoach: "Thu hoạch",
};
export const ACTIVITIES = Object.keys(ACTIVITY_LABEL) as CropActivity[];

export function activityLabel(code: string | null | undefined): string {
  return code ? (ACTIVITY_LABEL[code as CropActivity] ?? code) : "—";
}

/** Nhãn vị trí (HTX US-GPS-01/02, US-LOG-05 AC-1). */
export const LOCATION_LABEL: Record<string, { text: string; cls: string }> = {
  match: { text: "Vị trí khớp", cls: "c-ok" },
  mismatch: { text: "Vị trí không khớp", cls: "c-bad" },
  missing: { text: "Thiếu GPS", cls: "c-warn" },
};

export function locationLabel(status: string | null | undefined): { text: string; cls: string } {
  return LOCATION_LABEL[status ?? ""] ?? { text: status ?? "—", cls: "c-mute" };
}

export const SEASON_STATUS: Record<string, { text: string; cls: string }> = {
  open: { text: "Đang canh tác", cls: "c-info" },
  completed: { text: "Đã hoàn thành vụ", cls: "c-ok" },
  cancelled: { text: "Đã hủy", cls: "c-mute" },
};

export const LOG_STATUS: Record<string, { text: string; cls: string }> = {
  pending: { text: "Chờ duyệt", cls: "c-warn" },
  approved: { text: "Đã duyệt", cls: "c-ok" },
  needs_supplement: { text: "Cần bổ sung", cls: "c-bad" },
  superseded: { text: "Đã thay thế", cls: "c-mute" },
};

export const TASK_STATUS: Record<string, { text: string; cls: string }> = {
  new: { text: "Mới — chờ phân công", cls: "c-warn" },
  assigned: { text: "Đã giao cán bộ", cls: "c-info" },
  accepted: { text: "Cán bộ đã nhận", cls: "c-info" },
  in_progress: { text: "Đang xử lý", cls: "c-violet" },
  resolved: { text: "Đã xử lý", cls: "c-ok" },
};

export const SUPPORT_CATEGORY: Record<string, string> = {
  dich_hai: "Dịch hại",
  benh_lua: "Bệnh lúa",
  thoi_tiet: "Thời tiết bất thường",
  ky_thuat: "Vướng mắc kỹ thuật",
  may_moc: "Máy móc",
  khac: "Khác",
  sau_benh: "Sâu bệnh",
};

export const GRANT_STATUS: Record<string, { text: string; cls: string }> = {
  active: { text: "Chờ kích hoạt", cls: "c-warn" },
  used: { text: "Đã kích hoạt", cls: "c-ok" },
  revoked: { text: "Đã thu hồi", cls: "c-mute" },
  expired: { text: "Hết hạn", cls: "c-bad" },
};

/** Lý do sửa/hủy/yêu cầu bổ sung: bỏ khoảng trắng hai đầu, tối thiểu `min` ký tự. null = hợp lệ. */
export function reasonError(value: string, min = 10): string | null {
  const n = value.trim().length;
  if (n === 0) return "Bắt buộc nhập lý do.";
  if (n < min) return `Lý do tối thiểu ${min} ký tự (hiện ${n}).`;
  return null;
}

export interface StepIssue {
  index: number;
  field: keyof CropSOPStep | "steps";
  message: string;
}

/**
 * Kiểm bước SOP trước khi lưu (HTX US-CAT-01 AC-6): seq liên tục 1..n theo thứ tự hiển thị,
 * tên bắt buộc, khoảng ngày trong giới hạn hợp đồng. `forPublish` thêm yêu cầu có bước thu hoạch.
 */
export function validateSteps(steps: readonly CropSOPStep[], forPublish = false): StepIssue[] {
  const out: StepIssue[] = [];
  if (steps.length === 0) out.push({ index: -1, field: "steps", message: "Quy trình cần ít nhất một bước." });
  if (steps.length > 100) out.push({ index: -1, field: "steps", message: "Tối đa 100 bước." });
  steps.forEach((s, i) => {
    if (s.seq !== i + 1) out.push({ index: i, field: "seq", message: MSG_STEP_ORDER });
    if (!s.name.trim()) out.push({ index: i, field: "name", message: "Nhập tên bước." });
    if (s.name.length > 200) out.push({ index: i, field: "name", message: "Tên bước tối đa 200 ký tự." });
    if (!Number.isInteger(s.day_offset) || s.day_offset < -60 || s.day_offset > 365)
      out.push({ index: i, field: "day_offset", message: "Ngày so với gieo sạ phải trong −60 … 365." });
    if (!Number.isInteger(s.window_days) || s.window_days < 0 || s.window_days > 60)
      out.push({ index: i, field: "window_days", message: "Cửa sổ thực hiện phải trong 0 … 60 ngày." });
    if (s.phi_days !== undefined && (!Number.isInteger(s.phi_days) || s.phi_days < 0 || s.phi_days > 120))
      out.push({ index: i, field: "phi_days", message: "Thời gian cách ly phải trong 0 … 120 ngày." });
  });
  if (forPublish && steps.length > 0 && !steps.some((s) => s.activity_code === "thu_hoach"))
    out.push({ index: -1, field: "steps", message: MSG_HARVEST_STEP });
  return out;
}

/** Đánh số lại 1..n theo thứ tự hiện tại (sau khi thêm/xoá/đổi chỗ bước). */
export function renumber(steps: readonly CropSOPStep[]): CropSOPStep[] {
  return steps.map((s, i) => ({ ...s, seq: i + 1 }));
}

/** Đổi chỗ bước i với bước i+delta rồi đánh số lại. Ngoài biên → giữ nguyên. */
export function moveStep(steps: readonly CropSOPStep[], i: number, delta: -1 | 1): CropSOPStep[] {
  const j = i + delta;
  if (i < 0 || j < 0 || i >= steps.length || j >= steps.length) return [...steps];
  const next = [...steps];
  [next[i], next[j]] = [next[j]!, next[i]!];
  return renumber(next);
}

/**
 * SĐT chủ hộ → dạng chuẩn 0xxxxxxxxx (tên đăng nhập tài khoản nông dân). Nhận +84…, 84…, có dấu cách/chấm/gạch.
 * Sai định dạng → null.
 */
export function normalizePhone(raw: string): string | null {
  const digits = raw.replace(/[\s.\-()]/g, "");
  let s = digits;
  if (s.startsWith("+84")) s = `0${s.slice(3)}`;
  else if (/^84\d{9}$/.test(s)) s = `0${s.slice(2)}`;
  return /^0\d{9}$/.test(s) ? s : null;
}

/**
 * Chuỗi số nhập tay (chấp nhận dấu phẩy thập phân kiểu Việt) → chuỗi thập phân chuẩn "123.45".
 * Không qua float (R-10). Quá `maxFrac` chữ số lẻ, âm (khi !allowNegative) hoặc sai dạng → null.
 */
export function parseDecimalInput(raw: string, maxFrac = 3, allowNegative = false): string | null {
  const s = raw.trim().replace(/\s/g, "").replace(",", ".");
  const m = /^(-?)(\d+)(?:\.(\d*))?$/.exec(s);
  if (!m) return null;
  if (m[1] === "-" && !allowNegative) return null;
  const frac = (m[3] ?? "").replace(/0+$/, "");
  if (frac.length > maxFrac) return null;
  const int = m[2]!.replace(/^0+(?=\d)/, "");
  return `${m[1]}${int}${frac ? `.${frac}` : ""}`;
}

/** Ngày YYYY-MM-DD → số ngày kể từ 1970-01-01 (không phụ thuộc múi giờ). */
function dayNumber(iso: string): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
  if (!m) return null;
  const t = Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
  const d = new Date(t);
  if (d.getUTCMonth() !== Number(m[2]) - 1 || d.getUTCDate() !== Number(m[3])) return null;
  return Math.round(t / 86_400_000);
}

/** Ngày nghiệp vụ hôm nay theo giờ Việt Nam (R-12) dạng YYYY-MM-DD. */
export function todayVN(now: Date = new Date()): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh", year: "numeric", month: "2-digit", day: "2-digit" }).format(now);
}

/** Cộng n ngày vào ngày YYYY-MM-DD. */
export function addDays(iso: string, n: number): string {
  const d = dayNumber(iso);
  if (d === null) return iso;
  return new Date((d + n) * 86_400_000).toISOString().slice(0, 10);
}

/** Ngày sạ trong [hôm nay − 15, hôm nay + 30] (HTX US-SEASON-01 AC-2). null = hợp lệ. */
export function sowingDateError(date: string, today: string = todayVN()): string | null {
  const d = dayNumber(date);
  const t = dayNumber(today);
  if (d === null || t === null) return "Ngày không hợp lệ.";
  return d < t - 15 || d > t + 30 ? MSG_SOWING_WINDOW : null;
}

export interface HarvestDraft {
  mass: string;
  date: string;
}

/** Một dòng bảng nhập nhanh sản lượng (HTX US-YIELD-02). Trống cả hai = bỏ qua dòng (null, null). */
export function harvestRowError(row: HarvestDraft, today: string = todayVN()): string | null {
  if (!row.mass.trim()) return "Nhập sản lượng (kg).";
  const kg = parseDecimalInput(row.mass, 3, true);
  if (kg === null) return "Sản lượng phải là số, tối đa 3 chữ số lẻ.";
  if (kg.startsWith("-")) return "Sản lượng không được âm.";
  if (row.date) {
    const d = dayNumber(row.date);
    const t = dayNumber(today);
    if (d === null) return "Ngày thu hoạch không hợp lệ.";
    if (t !== null && d > t) return "Ngày thu hoạch không được ở tương lai.";
  }
  return null;
}

/** Năng suất t/ha chỉ để HIỂN THỊ trước khi lưu (backend tính số chính thức). */
export function previewYield(massKg: string, areaHa: string): string | null {
  const kg = Number(massKg);
  const ha = Number(areaHa);
  if (!Number.isFinite(kg) || !Number.isFinite(ha) || ha <= 0) return null;
  return (kg / 1000 / ha).toFixed(2);
}

/** Ô bắt đầu bằng = + - @ bị Excel hiểu là công thức (chèn công thức CSV) → thêm dấu nháy. */
function csvCell(v: string | number | null | undefined): string {
  let s = v === null || v === undefined ? "" : String(v);
  if (/^[=+\-@\t\r]/.test(s) && !/^-?\d+([.,]\d+)?$/.test(s)) s = `'${s}`;
  return /[",\r\n;]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

/** CSV UTF-8 có BOM (Excel mở đúng tiếng Việt), phân cách dấu phẩy, xuống dòng CRLF. */
export function toCsv(rows: readonly (readonly (string | number | null | undefined)[])[]): string {
  return `﻿${rows.map((r) => r.map(csvCell).join(",")).join("\r\n")}\r\n`;
}
