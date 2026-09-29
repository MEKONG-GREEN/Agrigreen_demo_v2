// Quy chuẩn hiển thị dùng chung của web GIS: màu giai đoạn mùa vụ (GIS FN-13 B2 / BR-11: nâu → xanh → vàng → đỏ → xám),
// màu mức lấp đầy kho/Hub (US-HUB-01, ngưỡng geo.hub_capacity_warn_pct / full_pct), nhãn phân loại tuyến.
// Bảng màu và ngưỡng % là THAM SỐ CÓ PHÊ DUYỆT (GIS US-CFG-01/02) đọc từ GET /v1/masterdata/display-config;
// DEFAULT_PALETTE (= giá trị mặc định nhúng của backend) chỉ dùng khi không tải được cấu hình.
import type { AppGet } from "@/api/types";
import { toScaled } from "@/lib/decimal";

export type CropStage = "chuan_bi" | "sinh_truong" | "sap_thu" | "chin_ro" | "da_thu";
export type CapacityClass = "available" | "near_full" | "full" | "unknown";
/** Mã giai đoạn trong bảng màu: 5 giai đoạn + "none" (chưa có vụ). */
export type StageKey = CropStage | "none";

export type DisplayConfig = AppGet<"/v1/masterdata/display-config">;
export type DisplayValue = DisplayConfig["ripening_outline_color"];

export interface DisplayPalette {
  stage: Record<StageKey, string>;
  capacity: Record<CapacityClass, string>;
  ripeningOutline: string;
  /** Ngưỡng % dạng chuỗi thập phân (R-10). */
  warnPct: string;
  fullPct: string;
  plotMinZoom: string;
}

export const STAGE_LABEL: Record<CropStage, string> = {
  chuan_bi: "Chuẩn bị đất",
  sinh_truong: "Sinh trưởng",
  sap_thu: "Sắp thu hoạch",
  chin_ro: "Chín rộ / đang thu hoạch",
  da_thu: "Đã thu hoạch",
};
export const STAGE_CODES: readonly CropStage[] = ["chuan_bi", "sinh_truong", "sap_thu", "chin_ro", "da_thu"];
export const NO_STAGE_LABEL = "Chưa có vụ";

export const CAPACITY_LABEL: Record<CapacityClass, string> = {
  available: "Còn chỗ",
  near_full: "Sắp đầy",
  full: "Đã đầy",
  unknown: "Chưa có số liệu",
};
export const CAPACITY_CLASSES: readonly CapacityClass[] = ["available", "near_full", "full", "unknown"];

/** Giá trị mặc định nhúng (seed pf/masterdata) — chỉ là dự phòng khi không đọc được display-config. */
export const DEFAULT_PALETTE: DisplayPalette = {
  stage: { chuan_bi: "#8B5A2B", sinh_truong: "#2E9E4F", sap_thu: "#E0B000", chin_ro: "#D6452F", da_thu: "#9AA5A0", none: "#CFD8D3" },
  capacity: { available: "#1F7A38", near_full: "#C27C0E", full: "#B3261E", unknown: "#7C8A83" },
  ripeningOutline: "#B3261E",
  warnPct: "80",
  fullPct: "95",
  plotMinZoom: "13",
};

export const HEX_COLOR_ERROR = "Mã màu không đúng định dạng HEX (ví dụ: #1E90FF).";
export const PERCENT_ERROR = "Ngưỡng % phải tăng dần và nằm trong khoảng 0–100%.";

const HEX = /^#[0-9A-Fa-f]{6}$/;
const DECIMAL = /^\d+(\.\d+)?$/;

export function isHexColor(v: string | null | undefined): boolean {
  return typeof v === "string" && HEX.test(v.trim());
}

/**
 * Kiểm bộ ngưỡng % (GIS US-CFG-01 AC-2): chuỗi thập phân không âm, 0–100, cảnh báo < đầy (tăng dần nghiêm ngặt).
 * Trả thông điệp AC hoặc null khi hợp lệ. Không qua float (R-10).
 */
export function validateThresholds(warn: string, full: string): string | null {
  const w = warn.trim();
  const f = full.trim();
  if (!DECIMAL.test(w) || !DECIMAL.test(f)) return PERCENT_ERROR;
  const sw = toScaled(w);
  const sf = toScaled(f);
  const hundred = toScaled("100")!;
  if (sw === null || sf === null) return PERCENT_ERROR;
  if (sw > hundred || sf > hundred) return PERCENT_ERROR;
  if (sw >= sf) return PERCENT_ERROR;
  return null;
}

function pick(v: DisplayValue | undefined, fallback: string, ok: (s: string) => boolean): string {
  const norm = (x: string) => (x.trim().startsWith("#") ? x.trim().toUpperCase() : x.trim());
  if (v && typeof v.value === "string" && ok(v.value)) return norm(v.value);
  if (v && typeof v.default === "string" && ok(v.default)) return norm(v.default);
  return fallback;
}

const isDecimal = (s: string) => DECIMAL.test(s.trim());

/** display-config → bảng màu; từng khóa sai định dạng/thiếu → giá trị mặc định của khóa đó. */
export function paletteFromConfig(cfg: DisplayConfig | null | undefined): DisplayPalette {
  if (!cfg) return DEFAULT_PALETTE;
  const stage = { ...DEFAULT_PALETTE.stage };
  for (const k of Object.keys(stage) as StageKey[]) stage[k] = pick(cfg.stage_colors?.[k], DEFAULT_PALETTE.stage[k], isHexColor);
  const capacity = { ...DEFAULT_PALETTE.capacity };
  for (const k of CAPACITY_CLASSES) capacity[k] = pick(cfg.capacity_colors?.[k], DEFAULT_PALETTE.capacity[k], isHexColor);
  let warnPct = pick(cfg.hub_capacity_warn_pct, DEFAULT_PALETTE.warnPct, isDecimal);
  let fullPct = pick(cfg.hub_capacity_full_pct, DEFAULT_PALETTE.fullPct, isDecimal);
  if (validateThresholds(warnPct, fullPct)) {
    warnPct = DEFAULT_PALETTE.warnPct;
    fullPct = DEFAULT_PALETTE.fullPct;
  }
  return {
    stage,
    capacity,
    ripeningOutline: pick(cfg.ripening_outline_color, DEFAULT_PALETTE.ripeningOutline, isHexColor),
    warnPct,
    fullPct,
    plotMinZoom: pick(cfg.plot_layer_min_zoom, DEFAULT_PALETTE.plotMinZoom, isDecimal),
  };
}

/** 5 giai đoạn theo thứ tự chú giải, kèm màu của bảng màu đang hiệu lực. */
export function stages(palette: DisplayPalette = DEFAULT_PALETTE): { code: CropStage; label: string; color: string }[] {
  return STAGE_CODES.map((code) => ({ code, label: STAGE_LABEL[code], color: palette.stage[code] }));
}

export function stageColor(code: string | null | undefined, palette: DisplayPalette = DEFAULT_PALETTE): string {
  return code && Object.hasOwn(STAGE_LABEL, code) ? palette.stage[code as CropStage] : palette.stage.none;
}

export function stageLabel(code: string | null | undefined): string {
  return code && Object.hasOwn(STAGE_LABEL, code) ? STAGE_LABEL[code as CropStage] : NO_STAGE_LABEL;
}

/** Nhãn + màu một mức lấp đầy (mã lạ → "Chưa có số liệu"). */
export function capacityStyle(cls: string | null | undefined, palette: DisplayPalette = DEFAULT_PALETTE): { label: string; color: string } {
  const k: CapacityClass = cls && Object.hasOwn(CAPACITY_LABEL, cls) ? (cls as CapacityClass) : "unknown";
  return { label: CAPACITY_LABEL[k], color: palette.capacity[k] };
}

/** Chú giải mức lấp đầy kèm ngưỡng % (ưu tiên ngưỡng máy chủ trả cùng widget, không có thì của display-config). */
export function capacityLegend(
  palette: DisplayPalette = DEFAULT_PALETTE,
  thresholds?: { warn_pct?: string | null; full_pct?: string | null } | null,
): { code: CapacityClass; label: string; color: string; note: string | null }[] {
  const warn = thresholds?.warn_pct ?? palette.warnPct;
  const full = thresholds?.full_pct ?? palette.fullPct;
  const note: Record<CapacityClass, string | null> = { available: `< ${warn}%`, near_full: `≥ ${warn}%`, full: `≥ ${full}%`, unknown: null };
  return CAPACITY_CLASSES.map((code) => ({ code, label: CAPACITY_LABEL[code], color: palette.capacity[code], note: note[code] }));
}

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
