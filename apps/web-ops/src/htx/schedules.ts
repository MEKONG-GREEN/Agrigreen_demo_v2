// Lịch gửi báo cáo của Quản lý HTX (HTX US-DASH-03): kiểm biểu mẫu phía client và thông báo AC-2.
import type { ReportSchedule, ReportScheduleInput, ReportScheduleRun } from "@/api/types";

export interface ScheduleDraft {
  format: "xlsx" | "pdf";
  frequency: "weekly" | "monthly";
  weekday: string;
  day: string;
  hour: string;
  seasonCode: string;
  seasonYear: string;
  recipients: string[];
}

/** Thông báo AC-2 khi email đăng ký không hợp lệ hoặc gửi thất bại. */
export const DELIVERY_FAILED = "Email đăng ký nhận báo cáo không hợp lệ hoặc gửi thất bại, vui lòng kiểm tra lại.";

const intIn = (s: string, lo: number, hi: number): number | null => {
  if (!/^\d{1,4}$/.test(s.trim())) return null;
  const n = Number(s.trim());
  return n >= lo && n <= hi ? n : null;
};

/** Kiểm lịch; hợp lệ → thân POST /v1/reports/schedules (mẫu htx.production). */
export function validateSchedule(d: ScheduleDraft): { ok: true; body: ReportScheduleInput } | { ok: false; errors: string[] } {
  const errors: string[] = [];
  const hour = intIn(d.hour, 0, 23);
  if (hour === null) errors.push("Giờ gửi phải từ 0 đến 23.");
  const weekday = d.frequency === "weekly" ? intIn(d.weekday, 1, 7) : null;
  const day = d.frequency === "monthly" ? intIn(d.day, 1, 28) : null;
  if (d.frequency === "weekly" && weekday === null) errors.push("Chọn thứ trong tuần.");
  if (d.frequency === "monthly" && day === null) errors.push("Ngày trong tháng phải từ 1 đến 28.");
  let seasonYear: number | undefined;
  if (d.seasonCode) {
    const y = intIn(d.seasonYear, 2000, 2100);
    if (y === null) errors.push("Nhập năm của vụ (2000–2100).");
    else seasonYear = y;
  }
  if (d.recipients.length === 0) errors.push("Chọn ít nhất một người nhận.");
  if (errors.length > 0) return { ok: false, errors };
  return {
    ok: true,
    body: {
      template_key: "htx.production",
      format: d.format,
      frequency: d.frequency,
      weekday: weekday ?? undefined,
      day_of_month: day ?? undefined,
      hour: hour!,
      recipients: d.recipients,
      params: d.seasonCode ? { season_code: d.seasonCode, season_year: seasonYear } : {},
    },
  };
}

const WEEKDAY = ["", "thứ Hai", "thứ Ba", "thứ Tư", "thứ Năm", "thứ Sáu", "thứ Bảy", "Chủ nhật"];

/** Mô tả lịch: "Hằng tháng, ngày 1 lúc 6 giờ". */
export function scheduleWhen(s: Pick<ReportSchedule, "frequency" | "weekday" | "day_of_month" | "hour">): string {
  const when = s.frequency === "weekly" ? `Hằng tuần, ${WEEKDAY[s.weekday ?? 1] ?? ""}` : `Hằng tháng, ngày ${s.day_of_month ?? 1}`;
  return `${when} lúc ${s.hour} giờ`;
}

/** Có lần gửi bị bỏ, lỗi hoặc thiếu email → thông báo cho người lập (AC-2). */
export function deliveryWarning(runs: readonly Pick<ReportScheduleRun, "delivery_status" | "delivery_note">[]): string | null {
  const bad = runs.find((r) => r.delivery_status === "skipped" || r.delivery_status === "failed" || (r.delivery_note ?? "").includes("chưa có email"));
  return bad ? `${DELIVERY_FAILED}${bad.delivery_note ? ` (${bad.delivery_note})` : ""}` : null;
}
