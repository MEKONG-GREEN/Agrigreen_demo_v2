// Hiển thị yêu cầu hỗ trợ của nông dân cho cán bộ (HTX US-SUPPORT-01, KN US-TASK-01/03).
import type { SupportRequestStaff } from "@/api/types";

/** Nhãn của yêu cầu không có vị trí (nguyên văn AC-1 của HTX US-SUPPORT-01). */
export const LABEL_GPS_MISSING = "Thiếu vị trí GPS";

export const SUPPORT_URGENCY: Record<string, string> = { binh_thuong: "Bình thường", khan: "Khẩn" };

/** Quy tắc giao nhiệm vụ tự sinh (KN US-TASK-01 AC-2). */
export const ASSIGNMENT_RULE: Record<string, string> = {
  commune_on_duty: "Cán bộ xã đang trực",
  province_on_duty: "Cán bộ tỉnh đang trực",
  province_officer: "Cán bộ TTKN tỉnh",
  admin_queue: "Chờ quản trị tỉnh gán",
};

export function photoLabel(count: number): string {
  return count > 0 ? `${count} ảnh` : "Không có ảnh";
}

/** Trạng thái GPS: nhãn do máy chủ gắn nếu có, không thì theo gps_missing; kèm độ chính xác khi có vị trí. */
export function gpsLabel(r: Pick<SupportRequestStaff, "gps_missing" | "labels" | "gps_accuracy_m">): { text: string; missing: boolean } {
  if (r.gps_missing || r.labels.includes(LABEL_GPS_MISSING)) return { text: LABEL_GPS_MISSING, missing: true };
  return { text: r.gps_accuracy_m ? `Có vị trí GPS (±${r.gps_accuracy_m} m)` : "Có vị trí GPS", missing: false };
}

export interface SupportSummary {
  total: number;
  open: number;
  urgentOpen: number;
  gpsMissing: number;
  unassigned: number;
}

/** Tổng hợp trang hiện có: chưa xử lý xong = nhiệm vụ chưa "resolved". */
export function summarizeSupport(items: readonly SupportRequestStaff[]): SupportSummary {
  let open = 0;
  let urgentOpen = 0;
  let gpsMissing = 0;
  let unassigned = 0;
  for (const r of items) {
    const isOpen = r.task.status !== "resolved";
    if (isOpen) open++;
    if (isOpen && r.urgency === "khan") urgentOpen++;
    if (r.gps_missing) gpsMissing++;
    if (isOpen && !r.task.assignee_ref) unassigned++;
  }
  return { total: items.length, open, urgentOpen, gpsMissing, unassigned };
}
