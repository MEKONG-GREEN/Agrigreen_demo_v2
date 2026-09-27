import type { CoverageThresholds } from "@/api/pending/cgh";
import { compareDecimal } from "@/lib/decimal";

/**
 * Phân loại mức đáp ứng CGH (BRD Bản đồ CGH v1.3, QT-02 / FN-02 / FN-09):
 *   Thừa ≥ 120% · Đủ ≥ 85% · Cần chú ý 60–<85% · Thiếu < 60% · Chưa có dữ liệu (xám).
 * Ngưỡng là tham số có hiệu lực theo vụ; không có ngưỡng → không tô màu phân loại.
 */
export type CoverageStatus = "thua" | "du" | "chu_y" | "thieu" | "khong_du_lieu" | "chua_cau_hinh";

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

export interface CoverageStyle {
  label: string;
  /** Màu tô vùng trên bản đồ. */
  fill: string;
  /** Màu chữ/viền đạt tương phản AA trên nền sáng. */
  ink: string;
  bg: string;
}

export const COVERAGE_STYLES: Record<CoverageStatus, CoverageStyle> = {
  du: { label: "Đủ", fill: "#22A45D", ink: "#11663A", bg: "#DFF7E8" },
  chu_y: { label: "Cần chú ý", fill: "#F0A020", ink: "#8A5300", bg: "#FFF3D6" },
  thieu: { label: "Thiếu", fill: "#E5484D", ink: "#A11C22", bg: "#FDE8E8" },
  thua: { label: "Thừa", fill: "#2F80ED", ink: "#1454A8", bg: "#E3EFFD" },
  khong_du_lieu: { label: "Chưa có dữ liệu", fill: "#B8C2BD", ink: "#4B5563", bg: "#EEF1EF" },
  chua_cau_hinh: { label: "Chưa cấu hình ngưỡng", fill: "#D5DBD7", ink: "#4B5563", bg: "#F3F4F6" },
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
