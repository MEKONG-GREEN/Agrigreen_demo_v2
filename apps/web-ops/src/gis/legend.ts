// Quy chuẩn hiển thị dùng chung của web GIS: màu giai đoạn mùa vụ (GIS FN-13 B2 / BR-11: nâu → xanh → vàng → đỏ → xám),
// màu mức lấp đầy kho/Hub (US-HUB-01, ngưỡng geo.hub_capacity_warn_pct / full_pct), nhãn phân loại tuyến.

export type CropStage = "chuan_bi" | "sinh_truong" | "sap_thu" | "chin_ro" | "da_thu";

export const STAGES: readonly { code: CropStage; label: string; color: string }[] = [
  { code: "chuan_bi", label: "Chuẩn bị đất", color: "#8B5A2B" },
  { code: "sinh_truong", label: "Sinh trưởng", color: "#2E9E4F" },
  { code: "sap_thu", label: "Sắp thu hoạch", color: "#E0B000" },
  { code: "chin_ro", label: "Chín rộ / đang thu hoạch", color: "#D6452F" },
  { code: "da_thu", label: "Đã thu hoạch", color: "#9AA5A0" },
];

export function stageColor(code: string | null | undefined): string {
  return STAGES.find((s) => s.code === code)?.color ?? "#cfd8d3";
}

export function stageLabel(code: string | null | undefined): string {
  return STAGES.find((s) => s.code === code)?.label ?? "Chưa có vụ";
}

/** Viền nổi bật vùng chín rộ 1–3 ngày tới (GIS US-YIELD-01). */
export const RIPENING_OUTLINE = "#B3261E";

export type CapacityClass = "available" | "near_full" | "full" | "unknown";

export const CAPACITY: Record<CapacityClass, { label: string; color: string }> = {
  available: { label: "Còn chỗ", color: "#1F7A38" },
  near_full: { label: "Sắp đầy", color: "#C27C0E" },
  full: { label: "Đã đầy", color: "#B3261E" },
  unknown: { label: "Chưa có số liệu", color: "#7C8A83" },
};

export const FACILITY_KIND: Record<string, string> = { kho: "Kho", hub: "Hub", nha_may: "Nhà máy" };
export const CAPACITY_UNIT: Record<string, string> = { tan: "tấn", m3: "m³", ke: "kệ" };
export const CAPACITY_SOURCE: Record<string, string> = { manual: "Admin nhập tay", import: "Nhập tệp", valve: "Đồng bộ (van công bố)" };

export const ROAD_CLASS: Record<string, string> = {
  quoc_lo: "Quốc lộ",
  tinh_lo: "Tỉnh lộ",
  huyen_lo: "Huyện lộ",
  truc_xa: "Trục xã",
  bo_ruong: "Bờ ruộng",
};

export const ROAD_COLOR: Record<string, string> = {
  quoc_lo: "#C0392B",
  tinh_lo: "#E67E22",
  huyen_lo: "#B7950B",
  truc_xa: "#7D6608",
  bo_ruong: "#A0826D",
};

export const WATERWAY_CLASS: Record<string, string> = {
  song: "Sông",
  kenh_chinh: "Kênh chính",
  noi_dong: "Kênh nội đồng",
  muong: "Mương",
};

export const WATERWAY_STATUS: Record<string, string> = { draft: "Nháp", confirmed: "Đã xác nhận", retired: "Ngừng dùng" };

export const LIMITING: Record<string, string> = {
  width: "chiều rộng lòng kênh",
  depth: "độ sâu",
  clearance: "tĩnh không công trình",
  declared: "tải trọng khai báo",
  none: "không có ràng buộc",
};

/** Nhãn ràng buộc bất lợi nhất (limiting) của tuyến thủy: "unknown:<thông số>" → "thiếu số liệu …". */
export function limitingLabel(code: string | null | undefined): string {
  if (!code) return "—";
  if (code.startsWith("unknown:")) return `thiếu số liệu ${LIMITING[code.slice(8)] ?? code.slice(8)}`;
  return LIMITING[code] ?? code;
}

/** Nhãn % lấp đầy trên marker (chuỗi decimal → "72%"). */
export function fillLabel(pct: string | null | undefined): string {
  if (pct === null || pct === undefined || pct === "") return "?";
  const n = Number(pct);
  return Number.isFinite(n) ? `${Math.round(n)}%` : "?";
}

/** Tóm tắt giai đoạn theo HTX từ lớp giai đoạn (stage-map): cooperative_ref → mã giai đoạn trội. */
export function stageByCoop(rows: readonly { cooperative_ref: string; dominant_stage: string }[]): Map<string, string> {
  return new Map(rows.map((r) => [r.cooperative_ref, r.dominant_stage]));
}
