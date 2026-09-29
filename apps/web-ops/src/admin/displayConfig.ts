// Quy chuẩn hiển thị bản đồ (GIS US-CFG-01/02) — màn quản trị /quan-tri/hien-thi.
// Mỗi màu / ngưỡng là MỘT khóa tham số có phê duyệt (pf/masterdata). Logic thuần để kiểm thử Vitest.
import type { ParamVersion } from "@/api/types";
import { compareDecimal } from "@/lib/decimal";
import { CAPACITY_LABEL, STAGE_LABEL, type DisplayConfig, type DisplayValue } from "@/gis/legend";

export type ColorSection = "stage" | "capacity" | "ripening";

export interface ColorRowDef {
  section: ColorSection;
  key: string;
  label: string;
}

export const WARN_KEY = "geo.hub_capacity_warn_pct";
export const FULL_KEY = "geo.hub_capacity_full_pct";
export const MIN_ZOOM_KEY = "geo.plot_layer_min_zoom";

export const COLOR_ROWS: readonly ColorRowDef[] = [
  ...(Object.keys(STAGE_LABEL) as (keyof typeof STAGE_LABEL)[]).map((c) => ({ section: "stage" as const, key: `gis.stage_color:${c}`, label: STAGE_LABEL[c] })),
  { section: "stage", key: "gis.stage_color:none", label: "Chưa có vụ" },
  ...(Object.keys(CAPACITY_LABEL) as (keyof typeof CAPACITY_LABEL)[]).map((c) => ({ section: "capacity" as const, key: `gis.capacity_color:${c}`, label: CAPACITY_LABEL[c] })),
  { section: "ripening", key: "gis.ripening_outline_color", label: "Viền vùng chín rộ 1–3 ngày tới" },
];

export const SECTION_TITLE: Record<ColorSection, string> = {
  stage: "Màu giai đoạn mùa vụ (bản đồ nhiệt)",
  capacity: "Màu mức lấp đầy kho/Hub",
  ripening: "Viền nổi bật vùng chín rộ",
};

/** Khóa thuộc quy chuẩn hiển thị bản đồ (để lọc danh sách chờ duyệt). */
export function isDisplayKey(key: string): boolean {
  return key.startsWith("gis.") || key === WARN_KEY || key === FULL_KEY || key === MIN_ZOOM_KEY;
}

/** Mọi DisplayValue của display-config theo khóa tham số. */
export function displayValuesByKey(cfg: DisplayConfig | null | undefined): Map<string, DisplayValue> {
  const out = new Map<string, DisplayValue>();
  if (!cfg) return out;
  for (const v of [
    ...Object.values(cfg.stage_colors ?? {}),
    ...Object.values(cfg.capacity_colors ?? {}),
    cfg.ripening_outline_color,
    cfg.hub_capacity_warn_pct,
    cfg.hub_capacity_full_pct,
    cfg.plot_layer_min_zoom,
  ]) {
    if (v?.key) out.set(v.key, v);
  }
  return out;
}

/** "Đã duyệt v.N" / "Mặc định". */
export function sourceLabel(v: Pick<DisplayValue, "source" | "version"> | null | undefined): string {
  if (!v) return "—";
  return v.source === "approved" ? `Đã duyệt v.${v.version}` : "Mặc định";
}

/** Phiên bản đã gửi duyệt của các khóa hiển thị, gom theo khóa (cũ → mới). */
export function pendingByKey(versions: readonly ParamVersion[]): Map<string, ParamVersion[]> {
  const out = new Map<string, ParamVersion[]>();
  for (const v of versions) {
    if (v.status !== "submitted" || !isDisplayKey(v.key)) continue;
    out.set(v.key, [...(out.get(v.key) ?? []), v]);
  }
  for (const list of out.values()) list.sort((a, b) => a.version - b.version);
  return out;
}

/** Giá trị hiển thị của một phiên bản (màu ở `text`, số ở `value`). */
export function versionValue(v: Pick<ParamVersion, "value" | "text">): string {
  return v.text ?? v.value ?? "—";
}

/**
 * Thứ tự đề xuất bộ ngưỡng % (chỉ khóa đổi giá trị). Backend kiểm mỗi đề xuất với mốc còn lại ĐÃ DUYỆT,
 * nên khi nâng cả hai thì đề xuất "Đã đầy" trước, khi hạ thì "Sắp đầy" trước.
 */
export function thresholdPlan(current: { warn: string; full: string }, next: { warn: string; full: string }): { key: string; value: string }[] {
  const same = (a: string, b: string) => {
    try {
      return compareDecimal(a, b) === 0;
    } catch {
      return a === b;
    }
  };
  const warn = same(current.warn, next.warn) ? null : { key: WARN_KEY, value: next.warn };
  const full = same(current.full, next.full) ? null : { key: FULL_KEY, value: next.full };
  if (warn && full) {
    let raising = false;
    try {
      raising = compareDecimal(next.full, current.full) > 0;
    } catch {
      raising = false;
    }
    return raising ? [full, warn] : [warn, full];
  }
  return [warn, full].filter((x): x is { key: string; value: string } => !!x);
}
