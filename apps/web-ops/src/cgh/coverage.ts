import type { MechCoverageStatus, MechRun } from "@/api/types";
import { compareDecimal } from "@/lib/decimal";

/**
 * Phân loại mức đáp ứng CGH (BRD Bản đồ CGH v1.3, QT-02 / FN-02 / FN-09):
 *   Thừa ≥ 120% · Đủ ≥ 85% · Cần chú ý 60–<85% · Thiếu < 60% · Chưa có dữ liệu (xám).
 * Backend pf/mechanization đã phân loại và LƯU kết quả theo vụ (status trên MechGroup) — giao diện
 * dùng status đó. `coverageStatus` chỉ còn cho chú giải/kiểm thử ngưỡng, không tính lại số liệu.
 */
export type CoverageStatus = "thua" | "du" | "chu_y" | "thieu" | "khong_du_lieu" | "chua_cau_hinh" | "chua_dinh_muc";

/** Ngưỡng (%) dạng chuỗi thập phân (R-10). */
export interface CoverageThresholds {
  sufficient_pct: string;
  attention_pct: string;
  surplus_pct: string;
}

export const DEFAULT_THRESHOLDS: CoverageThresholds = {
  sufficient_pct: "85",
  attention_pct: "60",
  surplus_pct: "120",
};

export function coverageStatus(
  pct: string | number | null | undefined,
  thresholds: CoverageThresholds | null = DEFAULT_THRESHOLDS,
): CoverageStatus {
  if (pct === null || pct === undefined || pct === "") return "khong_du_lieu";
  if (!thresholds) return "chua_cau_hinh";
  if (compareDecimal(pct, thresholds.surplus_pct) >= 0) return "thua";
  if (compareDecimal(pct, thresholds.sufficient_pct) >= 0) return "du";
  if (compareDecimal(pct, thresholds.attention_pct) >= 0) return "chu_y";
  return "thieu";
}

const FROM_MECH: Record<MechCoverageStatus, CoverageStatus> = {
  sufficient: "du",
  attention: "chu_y",
  shortage: "thieu",
  surplus: "thua",
  no_data: "khong_du_lieu",
  no_norm: "chua_dinh_muc",
  unclassified: "chua_cau_hinh",
};

/** Mã mức đáp ứng của backend (MechCoverageStatus) → trạng thái hiển thị. */
export function fromMechStatus(s: MechCoverageStatus): CoverageStatus {
  return FROM_MECH[s] ?? "khong_du_lieu";
}

/** Ngưỡng đã dùng của một lần cân đối; thiếu một ngưỡng → null ("Chưa cấu hình ngưỡng"). */
export function thresholdsFromRun(run: Pick<MechRun, "thresholds"> | null | undefined): CoverageThresholds | null {
  const t = run?.thresholds;
  if (!t?.sufficient || !t.attention || !t.surplus) return null;
  return { sufficient_pct: t.sufficient.value, attention_pct: t.attention.value, surplus_pct: t.surplus.value };
}

export interface CoverageStyle {
  label: string;
  /** Màu tô vùng trên bản đồ. */
  fill: string;
  /** Màu chữ/viền đạt tương phản AA trên nền sáng. */
  ink: string;
  bg: string;
}

/**
 * `fill` trùng màu tô bản đồ của backend (pf/mechanization domain.CoverageColors, FN-08 BR-01) để chú giải,
 * chấm màu trên nhãn và vùng tô trên bản đồ là MỘT màu. Chú giải ưu tiên `legend[].color` do máy chủ trả.
 */
export const COVERAGE_STYLES: Record<CoverageStatus, CoverageStyle> = {
  du: { label: "Đủ", fill: "#1F7A38", ink: "#11663A", bg: "#DFF7E8" },
  chu_y: { label: "Cần chú ý", fill: "#9C6414", ink: "#8A5300", bg: "#FFF3D6" },
  thieu: { label: "Thiếu", fill: "#A3372A", ink: "#A11C22", bg: "#FDE8E8" },
  thua: { label: "Thừa", fill: "#2A78D6", ink: "#1454A8", bg: "#E3EFFD" },
  khong_du_lieu: { label: "Chưa có dữ liệu", fill: "#9AA5A0", ink: "#4B5563", bg: "#EEF1EF" },
  chua_cau_hinh: { label: "Chưa cấu hình ngưỡng", fill: "#9AA5A0", ink: "#4B5563", bg: "#F3F4F6" },
  chua_dinh_muc: { label: "Chưa cấu hình định mức", fill: "#9AA5A0", ink: "#4B5563", bg: "#F3F4F6" },
};

/** Chú giải theo thứ tự hiển thị, kèm mô tả ngưỡng. */
export function legendEntries(t: CoverageThresholds | null): { status: CoverageStatus; text: string }[] {
  const th = t ?? DEFAULT_THRESHOLDS;
  return [
    { status: "du", text: `≥ ${th.sufficient_pct}%` },
    { status: "chu_y", text: `${th.attention_pct}–<${th.sufficient_pct}%` },
    { status: "thieu", text: `< ${th.attention_pct}%` },
    { status: "thua", text: `≥ ${th.surplus_pct}%` },
    { status: "khong_du_lieu", text: "không tính vào mức đáp ứng" },
  ];
}
