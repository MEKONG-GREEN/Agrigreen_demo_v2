// Hiển thị yêu cầu hỗ trợ của nông dân cho quản lý HTX (HTX US-SUPPORT-01; module chủ HTX từ #313).
import type { CoopSupportRequest } from "@/api/types";

/** Nhãn của yêu cầu không có vị trí (nguyên văn AC-1 của HTX US-SUPPORT-01). */
export const LABEL_GPS_MISSING = "Thiếu vị trí GPS";

export const SUPPORT_URGENCY: Record<string, string> = { binh_thuong: "Bình thường", khan: "Khẩn" };

export function photoLabel(count: number): string {
  return count > 0 ? `${count} ảnh` : "Không có ảnh";
}

/** Trạng thái GPS: nhãn do máy chủ gắn nếu có, không thì theo gps_missing; kèm độ chính xác khi có vị trí. */
export function gpsLabel(r: Pick<CoopSupportRequest, "gps_missing" | "labels" | "gps_accuracy_m">): { text: string; missing: boolean } {
  if (r.gps_missing || r.labels.includes(LABEL_GPS_MISSING)) return { text: LABEL_GPS_MISSING, missing: true };
  return { text: r.gps_accuracy_m ? `Có vị trí GPS (±${r.gps_accuracy_m} m)` : "Có vị trí GPS", missing: false };
}

export interface SupportSummary {
  total: number;
  urgent: number;
  gpsMissing: number;
}

/** Tổng hợp trang hiện có: số yêu cầu, khẩn, thiếu GPS. */
export function summarizeSupport(items: readonly CoopSupportRequest[]): SupportSummary {
  let urgent = 0;
  let gpsMissing = 0;
  for (const r of items) {
    if (r.urgency === "khan") urgent++;
    if (r.gps_missing) gpsMissing++;
  }
  return { total: items.length, urgent, gpsMissing };
}
