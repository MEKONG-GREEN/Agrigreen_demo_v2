// GIẢ LẬP quy chuẩn hiển thị bản đồ (GIS US-CFG-01/02) — pf/masterdata:
//   App GET /v1/masterdata/display-config (mọi người dùng đã xác thực),
//   Admin POST /v1/masterdata/params/restore-default + kiểm HEX / bộ ngưỡng % khi đề xuất phiên bản.
// Giá trị hiệu lực = phiên bản ĐÃ DUYỆT mới nhất có effective_from ≤ now (R-56: áp dụng ngay khi được duyệt);
// khóa chưa có phiên bản duyệt → mặc định nhúng (source = default).
import type { AppGet, ParamVersion } from "../types";
import { PARAM_VERSIONS } from "./data";

export const HEX_ERROR = "Mã màu không đúng định dạng HEX (ví dụ: #1E90FF).";
export const PERCENT_TITLE = "Ngưỡng % phải tăng dần và nằm trong khoảng 0–100%.";
export const RESTORE_REASON = "Khôi phục giá trị mặc định (GIS US-CFG-02)";

const HEX = /^#[0-9A-Fa-f]{6}$/;
const DECIMAL = /^\d+(\.\d+)?$/;

/** Mặc định nhúng (seed pf/masterdata). */
export const DISPLAY_DEFAULTS: Readonly<Record<string, { value: string; kind: "color" | "decimal"; unit: string }>> = {
  "gis.stage_color:chuan_bi": { value: "#8B5A2B", kind: "color", unit: "" },
  "gis.stage_color:sinh_truong": { value: "#2E9E4F", kind: "color", unit: "" },
  "gis.stage_color:sap_thu": { value: "#E0B000", kind: "color", unit: "" },
  "gis.stage_color:chin_ro": { value: "#D6452F", kind: "color", unit: "" },
  "gis.stage_color:da_thu": { value: "#9AA5A0", kind: "color", unit: "" },
  "gis.stage_color:none": { value: "#CFD8D3", kind: "color", unit: "" },
  "gis.capacity_color:available": { value: "#1F7A38", kind: "color", unit: "" },
  "gis.capacity_color:near_full": { value: "#C27C0E", kind: "color", unit: "" },
  "gis.capacity_color:full": { value: "#B3261E", kind: "color", unit: "" },
  "gis.capacity_color:unknown": { value: "#7C8A83", kind: "color", unit: "" },
  "gis.ripening_outline_color": { value: "#B3261E", kind: "color", unit: "" },
  "geo.hub_capacity_warn_pct": { value: "80", kind: "decimal", unit: "%" },
  "geo.hub_capacity_full_pct": { value: "95", kind: "decimal", unit: "%" },
  "geo.plot_layer_min_zoom": { value: "13", kind: "decimal", unit: "" },
};

export const isColorKey = (key: string) => DISPLAY_DEFAULTS[key]?.kind === "color";
export const isPercentKey = (key: string) => key === "geo.hub_capacity_warn_pct" || key === "geo.hub_capacity_full_pct";

/** Bản đã duyệt đang hiệu lực của một khóa. */
export function effectiveVersion(key: string, now = Date.now()): ParamVersion | undefined {
  return PARAM_VERSIONS.filter((v) => v.key === key && v.status === "approved" && Date.parse(v.effective_from) <= now).sort(
    (a, b) => Date.parse(b.effective_from) - Date.parse(a.effective_from) || b.version - a.version,
  )[0];
}

type DisplayValue = AppGet<"/v1/masterdata/display-config">["ripening_outline_color"];

function displayValue(key: string): DisplayValue {
  const def = DISPLAY_DEFAULTS[key]!.value;
  const v = effectiveVersion(key);
  const value = v ? (v.text ?? v.value) : null;
  return value ? { key, value, default: def, version: v!.version, source: "approved" } : { key, value: def, default: def, version: 0, source: "default" };
}

export function displayConfig(): AppGet<"/v1/masterdata/display-config"> {
  const group = (prefix: string, codes: string[]) => Object.fromEntries(codes.map((c) => [c, displayValue(`${prefix}:${c}`)]));
  return {
    as_of: new Date().toISOString(),
    stage_colors: group("gis.stage_color", ["chuan_bi", "sinh_truong", "sap_thu", "chin_ro", "da_thu", "none"]),
    capacity_colors: group("gis.capacity_color", ["available", "near_full", "full", "unknown"]),
    ripening_outline_color: displayValue("gis.ripening_outline_color"),
    hub_capacity_warn_pct: displayValue("geo.hub_capacity_warn_pct"),
    hub_capacity_full_pct: displayValue("geo.hub_capacity_full_pct"),
    plot_layer_min_zoom: displayValue("geo.plot_layer_min_zoom"),
  };
}

/** Ngưỡng lấp đầy kho/Hub đang hiệu lực — dùng cho lớp kho/Hub giả lập (capacity-widgets). */
export function hubThresholds(): { warn_pct: string; full_pct: string } {
  return { warn_pct: displayValue("geo.hub_capacity_warn_pct").value, full_pct: displayValue("geo.hub_capacity_full_pct").value };
}

function scaled(s: string): number {
  const [i = "0", f = ""] = s.split(".");
  return Number(i) * 1_000_000 + Number((f + "000000").slice(0, 6));
}

/**
 * Kiểm giá trị đề xuất của khóa quy chuẩn hiển thị. Trả lỗi theo trường (422 validation_failed) hoặc
 * `percent` = vi phạm thứ tự bộ ngưỡng (422 masterdata.param_percent_thresholds), hoặc null khi hợp lệ.
 */
export function checkDisplayProposal(key: string, value: string, text: string): { errors?: { field: string; reason: string }[]; percent?: boolean } | null {
  if (isColorKey(key)) {
    if (!HEX.test(text)) return { errors: [{ field: "text", reason: HEX_ERROR }] };
    return null;
  }
  if (isPercentKey(key)) {
    if (!DECIMAL.test(value) || scaled(value) > 100_000_000) return { errors: [{ field: "value", reason: PERCENT_TITLE }] };
    const otherKey = key === "geo.hub_capacity_warn_pct" ? "geo.hub_capacity_full_pct" : "geo.hub_capacity_warn_pct";
    const other = displayValue(otherKey).value;
    const [warn, full] = key === "geo.hub_capacity_warn_pct" ? [value, other] : [other, value];
    if (scaled(warn) >= scaled(full)) return { percent: true };
  }
  return null;
}
