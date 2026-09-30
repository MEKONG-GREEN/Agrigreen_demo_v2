// Dựng dữ liệu biểu đồ lịch sử chỉ số (GET /v1/metrics/{key}/values) — hàm thuần, có Vitest.
// Giá trị là số thập phân dạng chuỗi (R-10): chỉ đổi sang số để VẼ, số hiển thị dùng chuỗi gốc.
import type { MetricDefinition, MetricValue } from "@/api/types";
import { addDays, vnToday } from "@/lib/vnDate";

export interface SeriesPoint {
  date: string;
  /** Chỉ để vẽ. */
  y: number;
  /** Chuỗi gốc từ máy chủ (hiển thị). */
  raw: string;
  version: number;
  /** Ngày này đã được tính lại (có dòng bị thay). */
  corrected: boolean;
}

export interface SupersededRow {
  date: string;
  id: string;
  oldValue: string;
  currentValue: string | null;
  reason: string | null;
  computedAt: string;
  version: number;
}

export interface ShapedSeries {
  points: SeriesPoint[];
  superseded: SupersededRow[];
  /** Ngày đổi phiên bản định nghĩa (so với điểm trước) — vẽ vạch mốc. */
  versionChanges: { date: string; from: number; to: number }[];
}

/**
 * KN US-DASH-02: kỳ đã chốt không bị sửa — tính lại tạo dòng mới (`supersedes_id`, `correction_reason`).
 * Mỗi ngày lấy dòng `current`; dòng đã bị thay gom vào `superseded` (kèm giá trị hiện hành cùng ngày).
 */
export function shapeSeries(items: readonly MetricValue[]): ShapedSeries {
  const current = new Map<string, MetricValue>();
  const old: MetricValue[] = [];
  for (const v of items) {
    if (v.current) {
      const prev = current.get(v.business_date);
      // Phòng thủ: hai dòng hiện hành cùng ngày → lấy dòng tính sau.
      if (!prev || Date.parse(v.computed_at) > Date.parse(prev.computed_at)) current.set(v.business_date, v);
    } else old.push(v);
  }
  const correctedDays = new Set(old.map((v) => v.business_date));
  for (const v of current.values()) if (v.supersedes_id) correctedDays.add(v.business_date);

  const points = Array.from(current.values())
    .sort((a, b) => a.business_date.localeCompare(b.business_date))
    .map((v) => ({ date: v.business_date, y: Number(v.value), raw: v.value, version: v.definition_version, corrected: correctedDays.has(v.business_date) }))
    .filter((p) => Number.isFinite(p.y));

  const byId = new Map(items.map((v) => [v.id, v]));
  // Lý do sửa nằm ở dòng MỚI (dòng thay) → tìm dòng có supersedes_id = id cũ.
  const replacedBy = new Map<string, MetricValue>();
  for (const v of items) if (v.supersedes_id) replacedBy.set(v.supersedes_id, v);
  const superseded = old
    .map((v) => ({
      date: v.business_date,
      id: v.id,
      oldValue: v.value,
      currentValue: current.get(v.business_date)?.value ?? null,
      reason: replacedBy.get(v.id)?.correction_reason ?? (v.supersedes_id ? (byId.get(v.supersedes_id)?.correction_reason ?? null) : null),
      computedAt: v.computed_at,
      version: v.definition_version,
    }))
    .sort((a, b) => b.date.localeCompare(a.date) || b.computedAt.localeCompare(a.computedAt));

  const versionChanges: ShapedSeries["versionChanges"] = [];
  for (let i = 1; i < points.length; i++) {
    const a = points[i - 1]!;
    const b = points[i]!;
    if (a.version !== b.version) versionChanges.push({ date: b.date, from: a.version, to: b.version });
  }
  return { points, superseded, versionChanges };
}

/** Khoảng mặc định như máy chủ: `to` = hôm qua (ngày đã chốt gần nhất, giờ VN), `from` = to − (days − 1). */
export function rangeFor(days: number, now: Date = new Date()): { from: string; to: string } {
  const to = addDays(vnToday(now), -1);
  return { from: addDays(to, -(Math.max(1, Math.min(days, 400)) - 1)), to };
}

export interface ChartBox {
  width: number;
  height: number;
  left: number;
  right: number;
  top: number;
  bottom: number;
}

export interface ChartGeometry {
  path: string;
  dots: { x: number; y: number; point: SeriesPoint }[];
  yTicks: { y: number; value: number }[];
  xTicks: { x: number; date: string }[];
  min: number;
  max: number;
  xOf: (date: string) => number;
}

function niceStep(span: number, count: number): number {
  const raw = span / Math.max(1, count);
  const mag = 10 ** Math.floor(Math.log10(raw || 1));
  const n = raw / mag;
  return (n <= 1 ? 1 : n <= 2 ? 2 : n <= 5 ? 5 : 10) * mag;
}

/**
 * Toạ độ đường gấp khúc theo ngày (trục x theo lịch, ngày thiếu giá trị để trống khoảng — không nối qua ngày
 * không có dòng). Trục y bắt đầu từ 0 khi mọi giá trị ≥ 0.
 */
export function chartGeometry(points: readonly SeriesPoint[], from: string, to: string, box: ChartBox): ChartGeometry {
  const t0 = Date.parse(`${from}T00:00:00Z`);
  const t1 = Math.max(t0 + 86_400_000, Date.parse(`${to}T00:00:00Z`));
  const plotW = box.width - box.left - box.right;
  const plotH = box.height - box.top - box.bottom;
  const ys = points.map((p) => p.y);
  let min = ys.length ? Math.min(...ys) : 0;
  let max = ys.length ? Math.max(...ys) : 1;
  if (min >= 0) min = 0;
  if (max === min) max = min + 1;
  const step = niceStep(max - min, 4);
  max = Math.ceil(max / step) * step;
  min = Math.floor(min / step) * step;
  const xOf = (date: string) => box.left + ((Date.parse(`${date}T00:00:00Z`) - t0) / (t1 - t0)) * plotW;
  const yOf = (v: number) => box.top + plotH - ((v - min) / (max - min)) * plotH;
  let path = "";
  let prev: string | null = null;
  const dots = points.map((p) => ({ x: xOf(p.date), y: yOf(p.y), point: p }));
  for (const d of dots) {
    const contiguous = prev !== null && addDays(prev, 1) === d.point.date;
    path += `${contiguous ? "L" : "M"}${d.x.toFixed(1)},${d.y.toFixed(1)}`;
    prev = d.point.date;
  }
  const yTicks: ChartGeometry["yTicks"] = [];
  for (let v = min; v <= max + step / 2; v += step) yTicks.push({ y: yOf(v), value: Number(v.toFixed(6)) });
  const days = Math.round((t1 - t0) / 86_400_000);
  const every = Math.max(1, Math.ceil(days / 6));
  const xTicks: ChartGeometry["xTicks"] = [];
  for (let i = 0; i <= days; i += every) {
    const date = addDays(from, i);
    xTicks.push({ x: xOf(date), date });
  }
  return { path, dots, yTicks, xTicks, min, max, xOf };
}

export interface DefinitionGroup {
  key: string;
  /** Phiên bản mới nhất có hiệu lực vào `on` (không có → phiên bản sớm nhất). */
  effective: MetricDefinition;
  /** Mọi phiên bản, mới trước. */
  versions: MetricDefinition[];
}

/** Gom định nghĩa theo khóa (trang /quan-tri/chi-so): mỗi khóa một dòng, các phiên bản xếp mới trước. */
export function groupDefinitions(defs: readonly MetricDefinition[], on: string = vnToday()): DefinitionGroup[] {
  const by = new Map<string, MetricDefinition[]>();
  for (const d of defs) by.set(d.key, [...(by.get(d.key) ?? []), d]);
  return Array.from(by.entries())
    .map(([key, list]) => {
      const versions = [...list].sort((a, b) => b.version - a.version);
      const effective = versions.find((v) => v.effective_from <= on) ?? versions[versions.length - 1]!;
      return { key, effective, versions };
    })
    .sort((a, b) => a.key.localeCompare(b.key));
}

/** Nhãn đơn vị đo của chỉ số. */
export function unitLabel(unit: string): string {
  return unit === "count" ? "số lượng" : unit;
}
