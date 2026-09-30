// BACKEND GIẢ LẬP — pf/metrics (G3): định nghĩa chỉ số có phiên bản, lịch sử giá trị ngày đã chốt
// (KN US-DASH-01/02, CGH FN-10/11), nhật ký lớp GIS + ảnh chụp cuối ngày + phát lại (GIS US-EVL-01,
// US-SNAP-01, US-REPLAY-01). Dữ liệu tổng hợp, ổn định theo hạt giống; hình dạng đúng contracts/openapi.
import type { GisLayerEvent, GisLayerFeature, GisLayerState, MetricDefinition, MetricValue } from "../types";
import { cutoffOf, addDays, isIsoDate, vnToday, daysBetween } from "@/lib/vnDate";
import { foldLayerEvents, GIS_LAYERS, type FoldedFeature } from "@/gis/replay";
import { ADMIN_UNITS, COOPERATIVES, hexagon, mulberry32, plotsOf } from "./data";
import { hasRole, json, pageSlice, problem, scopeUnits, validation, type Route } from "./http";
import type { MockPersona } from "./personas";

/** uuid v7-dạng ổn định từ một chuỗi (FNV-1a). */
export function mockUuid(seed: string): string {
  let h1 = 0x811c9dc5;
  let h2 = 0x01000193;
  for (let i = 0; i < seed.length; i++) {
    h1 = Math.imul(h1 ^ seed.charCodeAt(i), 16777619) >>> 0;
    h2 = Math.imul(h2 ^ seed.charCodeAt(i), 2246822519) >>> 0;
  }
  const a = h1.toString(16).padStart(8, "0");
  const b = h2.toString(16).padStart(8, "0");
  return `0192${a.slice(0, 4)}-${a.slice(4, 8)}-7${b.slice(0, 3)}-8${b.slice(3, 6)}-${(a + b).slice(4, 16)}`;
}

function seedOf(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619) >>> 0;
  return h;
}

// --------------------------------------------------------------------------
// Định nghĩa chỉ số (bản sao internal/platform/metrics/defs/*.yaml + một phiên bản 2 minh hoạ)
// --------------------------------------------------------------------------
interface MockDef extends MetricDefinition {
  viewer_roles: string[];
}

const KN_VIEW = ["kn_national_admin", "kn_province_admin", "kn_officer"];
const CC_VIEW = ["coop_manager", "kn_national_admin", "kn_province_admin", "cgh_viewer"];
const CGH_VIEW = ["cgh_viewer", "cgh_admin"];
const PENDING = "tich-hop-g3 (tạm, chờ khách)";

function def(d: Omit<MockDef, "proposed_by" | "approved_by" | "file" | "effective_from"> & { effective_from?: string; proposed_by?: string }): MockDef {
  return {
    effective_from: "2026-01-01",
    proposed_by: "pf-metrics (G3-A5)",
    approved_by: PENDING,
    file: `internal/platform/metrics/defs/${d.key}.v${d.version}.yaml`,
    ...d,
  };
}

/** Ngày hiệu lực phiên bản 2 của kn.sown_area_ha (tương đối, để luôn nằm trong 30 ngày gần nhất). */
export const SOWN_V2_FROM = addDays(vnToday(), -12);

export const METRIC_DEFS: MockDef[] = [
  def({ key: "kn.sown_area_ha", version: 1, title: "Diện tích đã sạ báo cáo (kỳ tháng)", description: "Tổng sown_area_ha của phiên bản báo cáo gieo sạ mới nhất theo từng khóa phiên bản, của kỳ tháng chứa ngày D, chỉ tính bản ghi có recorded_at < Cutoff(D).", unit: "ha", grain: "day", aggregation: "sum", source_module: "pf/extension", source: "pf_extension.field_report (sổ, phiên bản mới nhất theo stream_key)", rule_version: "kn.dash.v1", viewer_roles: KN_VIEW }),
  def({ key: "kn.sown_area_ha", version: 2, effective_from: SOWN_V2_FROM, title: "Diện tích đã sạ báo cáo (kỳ tháng)", description: "Như phiên bản 1, loại báo cáo của xã đã sáp nhập (mã hết hiệu lực) — minh hoạ phiên bản mới, không sửa phiên bản cũ.", unit: "ha", grain: "day", aggregation: "sum", source_module: "pf/extension", source: "pf_extension.field_report (sổ, phiên bản mới nhất theo stream_key)", rule_version: "kn.dash.v2", viewer_roles: KN_VIEW }),
  def({ key: "kn.cooperatives_reported", version: 1, title: "Số HTX có báo cáo gieo sạ (kỳ tháng)", description: "Số cooperative_ref khác nhau trong các phiên bản báo cáo gieo sạ mới nhất của kỳ tháng chứa ngày D (recorded_at < Cutoff(D)).", unit: "count", grain: "day", aggregation: "count", source_module: "pf/extension", source: "pf_extension.field_report (sổ)", rule_version: "kn.dash.v1", viewer_roles: KN_VIEW }),
  def({ key: "kn.open_tasks", version: 1, title: "Nhiệm vụ hỗ trợ đang mở (cuối ngày)", description: "Số nhiệm vụ mà sự việc mới nhất (theo seq) có recorded_at < Cutoff(D) mang status_after khác resolved.", unit: "count", grain: "day", aggregation: "last", source_module: "pf/extension", source: "pf_extension.task_event (sổ)", rule_version: "kn.dash.v1", viewer_roles: KN_VIEW }),
  def({ key: "kn.directive_ack_rate_pct", version: 1, title: "Tỷ lệ xác nhận đọc chỉ đạo (kỳ tháng)", description: "100 × số lượt xác nhận ÷ số lượt nhận của chỉ đạo ban hành trong kỳ tháng chứa D. Không có lượt nhận → 0.", unit: "%", grain: "day", aggregation: "avg", source_module: "pf/extension", source: "pf_extension.directive, directive_target, directive_ack", rule_version: "kn.dash.v1", viewer_roles: KN_VIEW }),
  def({ key: "geo.plots_digitised", version: 1, title: "Số thửa đã số hoá", description: "Số thửa có ít nhất một phiên bản ranh giới hiện hành ghi nhận trước Cutoff(D) và chưa ngừng dùng.", unit: "count", grain: "day", aggregation: "count", source_module: "pf/geo", source: "pf_geo.plot_event, plot_boundary (sổ)", rule_version: "geo.plot.v1", viewer_roles: ["geo_admin", "plot_viewer", "plot_editor", "kn_national_admin", "kn_province_admin"] }),
  def({ key: "cc.season_area_ha", version: 1, title: "Diện tích vụ (theo vụ)", description: "Tổng area_ha của sự việc mới nhất của mỗi vụ thửa thuộc vụ Scope.Dimension, trừ vụ đã hủy.", unit: "ha", grain: "season", aggregation: "sum", source_module: "pf/cropcycle", source: "pf_cropcycle.season_event (sổ)", rule_version: "cc.season.v1", viewer_roles: CC_VIEW }),
  def({ key: "cc.yield_t_per_ha", version: 1, title: "Năng suất bình quân (theo vụ)", description: "Σ mass_kg ÷ 1000 ÷ Σ area_ha của khai báo sản lượng mới nhất của mỗi vụ thửa thuộc vụ Scope.Dimension.", unit: "t/ha", grain: "season", aggregation: "avg", source_module: "pf/cropcycle", source: "pf_cropcycle.harvest_declaration (sổ)", rule_version: "cc.harvest.v1", viewer_roles: CC_VIEW }),
  def({ key: "cgh.coop_sufficiency_pct", version: 1, title: "Tỷ lệ HTX đủ máy (theo vụ)", description: "Lần cân đối mới nhất của vụ có recorded_at < Cutoff(D); 100 × số HTX Đủ hoặc Thừa ÷ số HTX trong lần cân đối.", unit: "%", grain: "season", aggregation: "last", source_module: "pf/mechanization", source: "pf_mechanization.balance_run, balance_result (sổ)", rule_version: "cgh.balance.v1", viewer_roles: CGH_VIEW }),
  def({ key: "cgh.coops_shortage", version: 1, title: "Số HTX thiếu máy (theo vụ)", description: "Lần cân đối mới nhất của vụ có recorded_at < Cutoff(D); số HTX có mức đáp ứng tổng thể Thiếu.", unit: "count", grain: "season", aggregation: "last", source_module: "pf/mechanization", source: "pf_mechanization.balance_run, balance_result (sổ)", rule_version: "cgh.balance.v1", viewer_roles: CGH_VIEW }),
];

const SEASON_DIMENSIONS = ["DX-2025", "HT-2025", "DX-2026", "HT-2026"];

function canSeeDef(p: MockPersona, d: MockDef): boolean {
  return hasRole(p, "platform_admin", "metrics_viewer", ...d.viewer_roles);
}

function strip(d: MockDef): MetricDefinition {
  const copy: Partial<MockDef> = { ...d };
  delete copy.viewer_roles;
  return copy as MetricDefinition;
}

/** Ngày D đã bị tính lại (có dòng bù) trong mock: 9 và 3 ngày trước hôm qua. */
export function correctedDays(to: string): Set<string> {
  return new Set([addDays(to, -9), addDays(to, -3)]);
}

export const CORRECTION_REASON = "Tính lại sau khi xã bổ sung báo cáo gieo sạ muộn (bản ghi bù, kỳ đã chốt không sửa)";

function valueOf(key: string, unit: string, dimension: string, date: string, r: () => number): string {
  const scale = unit === "" ? 12 : unit.length <= 2 ? 3 : 0.4;
  const dom = Number(date.slice(8, 10));
  const noise = r();
  switch (key) {
    case "kn.sown_area_ha":
      return (scale * 10_000 * (0.25 + (0.75 * dom) / 31) * (0.98 + noise * 0.04)).toFixed(2);
    case "kn.cooperatives_reported":
      return String(Math.round(scale * 12 * (0.3 + (0.7 * dom) / 31)));
    case "kn.open_tasks":
      return String(Math.max(0, Math.round(scale * 6 + (noise - 0.5) * 6 * Math.max(1, scale / 3))));
    case "kn.directive_ack_rate_pct":
      return (62 + (30 * dom) / 31 + (noise - 0.5) * 6).toFixed(1);
    case "geo.plots_digitised":
      return String(Math.round(scale * 900 + daysBetween("2026-01-01", date) * scale * 1.5));
    case "cc.season_area_ha":
      return (scale * 8_000 * (dimension.startsWith("DX") ? 1.05 : 0.95)).toFixed(2);
    case "cc.yield_t_per_ha":
      return ((dimension.startsWith("DX") ? 6.8 : 5.9) + (noise - 0.5) * 0.2).toFixed(4);
    case "cgh.coop_sufficiency_pct": {
      // Đổi bậc khi có lần cân đối mới (mỗi ~10 ngày).
      const step = Math.floor(daysBetween("2026-01-01", date) / 10);
      return (54 + (step % 5) * 4.5 + (dimension.startsWith("DX") ? 3 : 0)).toFixed(1);
    }
    case "cgh.coops_shortage": {
      const step = Math.floor(daysBetween("2026-01-01", date) / 10);
      return String(Math.max(0, Math.round(scale * 4 - (step % 5))));
    }
    default:
      return "0";
  }
}

function metricValues(d: MockDef, versions: MockDef[], unit: string, dimension: string, from: string, to: string, history: boolean): MetricValue[] {
  const last = addDays(vnToday(), -1);
  const end = to < last ? to : last;
  const fixed = correctedDays(last);
  const out: MetricValue[] = [];
  for (let date = from; date <= end; date = addDays(date, 1)) {
    const r = mulberry32(seedOf(`${d.key}|${unit}|${dimension}|${date}`));
    const version = [...versions].sort((a, b) => b.version - a.version).find((v) => v.effective_from <= date)?.version ?? 1;
    const cutoff = cutoffOf(date);
    const computedAt = new Date(Date.parse(cutoff) + 45 * 60_000).toISOString();
    const value = valueOf(d.key, unit, dimension, date, r);
    const base = { business_date: date, definition_version: version, cutoff, watermark: { last_event_id: mockUuid(`wm|${d.key}|${date}`), recorded_at: new Date(Date.parse(cutoff) - 3_600_000).toISOString() } };
    const id = mockUuid(`${d.key}|${unit}|${dimension}|${date}`);
    if (fixed.has(date)) {
      // Kỳ đã chốt: dòng gốc giữ nguyên, dòng bù mang supersedes_id + lý do (KN US-DASH-02).
      const scaledOld = d.unit === "count" ? String(Math.max(0, Math.round(Number(value) * 0.94))) : (Number(value) * 0.94).toFixed(value.includes(".") ? value.split(".")[1]!.length : 0);
      const oldId = mockUuid(`${d.key}|${unit}|${dimension}|${date}|orig`);
      if (history) out.push({ id: oldId, ...base, value: scaledOld, computed_at: computedAt, supersedes_id: null, correction_reason: null, current: false });
      out.push({ id, ...base, value, computed_at: new Date(Date.parse(computedAt) + 2 * 86_400_000).toISOString(), supersedes_id: oldId, correction_reason: CORRECTION_REASON, current: true });
    } else {
      out.push({ id, ...base, value, computed_at: computedAt, supersedes_id: null, correction_reason: null, current: true });
    }
  }
  return out;
}

// --------------------------------------------------------------------------
// Nhật ký lớp GIS (dựng một lần, thời điểm tương đối so với lúc tải trang)
// --------------------------------------------------------------------------
type Geometry = { type: string; coordinates: unknown };
const GEOMETRY = new Map<string, Geometry>();

interface LogBuild {
  events: GisLayerEvent[];
}

let LOG: LogBuild | null = null;

/** recorded_at = 00:00 giờ VN của (hôm nay − daysAgo) + hours giờ. */
function at(daysAgo: number, hours: number): string {
  const day = addDays(vnToday(), -daysAgo);
  return new Date(Date.parse(`${day}T00:00:00Z`) - 7 * 3_600_000 + hours * 3_600_000).toISOString();
}

const SOURCE: Record<string, { module: string; type: string }> = {
  plot: { module: "pf/geo", type: "pf.geo.plot" },
  coop_boundary: { module: "pf/geo", type: "pf.geo.cooperative_boundary" },
  facility: { module: "pf/geo", type: "pf.geo.facility" },
  season_stage: { module: "pf/cropcycle", type: "pf.cropcycle.season" },
  cgh_balance: { module: "pf/mechanization", type: "pf.mechanization.balance" },
  waterway: { module: "pf/waterway", type: "pf.waterway.network" },
};

function buildLog(): LogBuild {
  const events: GisLayerEvent[] = [];
  let n = 0;
  const push = (layer: string, ref: string, op: GisLayerEvent["op"], before: Record<string, unknown> | null, after: Record<string, unknown> | null, recorded: string) => {
    n++;
    const src = SOURCE[layer]!;
    events.push({
      id: mockUuid(`gis-ev|${n}`),
      layer,
      feature_ref: ref,
      op,
      before,
      after,
      source_event_id: mockUuid(`gis-src|${n}`),
      source_type: `${src.type}.${op === "create" ? "created" : op === "retire" ? "retired" : op === "restore" ? "restored" : "updated"}`,
      source_module: src.module,
      event_time: new Date(Date.parse(recorded) - 5 * 60_000).toISOString(),
      recorded_at: recorded,
    });
  };
  const geom = (ref: string, g: Geometry) => {
    GEOMETRY.set(ref, g);
    return ref;
  };

  const dt = COOPERATIVES.filter((c) => c.province_code === "82").slice(0, 6);
  const ag = COOPERATIVES.filter((c) => c.province_code === "91").slice(0, 2);
  // Ranh giới HTX.
  [...dt, ...ag].forEach((c, i) => {
    const ref = c.api.ref;
    const g1 = geom(mockUuid(`geom|coop|${ref}|1`), { type: "Polygon", coordinates: hexagon(c.lnglat, 0.018) });
    const attrs = { name: c.api.name, code: c.api.code, commune_code: c.api.admin_unit_code, province_code: c.province_code, area_ha: c.api.area_ha, geometry_ref: g1 };
    push("coop_boundary", ref, "create", null, attrs, at(40 - i, 9));
    if (i === 0) {
      const g2 = geom(mockUuid(`geom|coop|${ref}|2`), { type: "Polygon", coordinates: hexagon(c.lnglat, 0.026) });
      push("coop_boundary", ref, "update", attrs, { ...attrs, area_ha: (Number(c.api.area_ha) * 1.9).toFixed(4), geometry_ref: g2 }, at(5, 10));
    }
    if (i === 1) push("coop_boundary", ref, "update", attrs, { ...attrs, name: `${c.api.name} (đổi tên)` }, at(2, 14));
  });
  // Thửa ruộng của HTX đầu tiên.
  const coop = dt[0]!;
  const plots = plotsOf(coop).filter((p) => p.properties.status !== "retired").slice(0, 12);
  plots.forEach((p, i) => {
    const ref = p.properties.ref;
    const g = geom(mockUuid(`geom|plot|${ref}|1`), p.geometry as Geometry);
    const attrs = { code: p.properties.code, name: p.properties.name, commune_code: p.properties.commune_code, cooperative_ref: p.properties.cooperative_ref, area_ha: p.properties.area_ha, status: "active", geometry_ref: g };
    push("plot", ref, "create", null, attrs, at(35, 8 + (i % 8)));
    if (i === 3) push("plot", ref, "update", attrs, { ...attrs, area_ha: (Number(p.properties.area_ha) + 0.35).toFixed(4) }, at(6, 15));
    if (i === 5) push("plot", ref, "retire", attrs, { ...attrs, status: "retired", status_reason: "Chuyển mục đích sử dụng đất (giả lập)" }, at(3, 9));
  });
  {
    const ref = mockUuid("plot|new|1");
    const g = geom(mockUuid(`geom|plot|${ref}|1`), { type: "Polygon", coordinates: hexagon([coop.lnglat[0] + 0.012, coop.lnglat[1] - 0.01], 0.0014) });
    push("plot", ref, "create", null, { code: "TH9990001", name: "Thửa mới số hoá", commune_code: coop.api.admin_unit_code, cooperative_ref: coop.api.ref, area_ha: "1.2500", status: "active", geometry_ref: g }, at(1, 10));
  }
  // Giai đoạn mùa vụ (không có hình học riêng).
  plots.slice(0, 6).forEach((p, i) => {
    const before = { plot_ref: p.properties.ref, commune_code: p.properties.commune_code, stage: "sowing", season_code: "HT-2026" };
    push("season_stage", p.properties.ref, "create", null, before, at(30, 7));
    if (i < 4) push("season_stage", p.properties.ref, "update", before, { ...before, stage: "tillering" }, at(8, 16));
  });
  // Kho / Hub.
  [0, 1, 2].forEach((k) => {
    const c = dt[k]!;
    const ref = mockUuid(`facility|${k}`);
    const g = geom(mockUuid(`geom|facility|${k}`), { type: "Point", coordinates: [c.lnglat[0] + 0.01, c.lnglat[1] + 0.008] });
    const attrs = { name: `Kho lúa ${c.api.name.replace(/^HTX /, "")}`, commune_code: c.api.admin_unit_code, province_code: c.province_code, capacity_t: "1200", used_t: String(300 + k * 150), geometry_ref: g };
    push("facility", ref, "create", null, attrs, at(30, 11));
    push("facility", ref, "update", attrs, { ...attrs, used_t: String(700 + k * 120) }, at(4, 13));
    if (k === 0) push("facility", ref, "update", { ...attrs, used_t: "700" }, { ...attrs, used_t: "1150" }, at(1, 8));
  });
  // Đường thủy (lớp nền, không có địa bàn).
  [0, 1].forEach((k) => {
    const ref = mockUuid(`waterway|${k}`);
    const y = 10.4 - k * 0.12;
    const g = geom(mockUuid(`geom|waterway|${k}`), { type: "LineString", coordinates: [[105.6, y], [105.85, y + 0.05], [106.1, y + 0.02]] });
    const attrs = { name: k === 0 ? "Sông Tiền (đoạn giả lập)" : "Kênh Nguyễn Văn Tiếp (giả lập)", waterway_class: k === 0 ? "song" : "kenh_chinh", state: "passable", geometry_ref: g };
    push("waterway", ref, "create", null, attrs, at(30, 6));
    if (k === 1) {
      push("waterway", ref, "update", attrs, { ...attrs, state: "blocked", note: "Nạo vét luồng" }, at(7, 9));
      push("waterway", ref, "update", { ...attrs, state: "blocked", note: "Nạo vét luồng" }, attrs, at(2, 9));
    }
  });
  return { events };
}

function log(): LogBuild {
  LOG ??= buildLog();
  return LOG;
}

const HISTORY_ROLES = ["metrics_viewer", "platform_admin", "geo_admin", "plot_viewer", "plot_editor", "kn_national_admin", "kn_province_admin", "cgh_viewer"];

/** Đối tượng trong phạm vi: lớp nền không có địa bàn luôn thấy (GIS US-EVL-01). */
function eventVisible(p: MockPersona, attrs: Record<string, unknown> | null | undefined): boolean {
  const s = scopeUnits(p);
  if (!s) return true;
  const commune = typeof attrs?.commune_code === "string" ? attrs.commune_code : null;
  const province = typeof attrs?.province_code === "string" ? attrs.province_code : commune ? commune.slice(0, 2) : null;
  if (!commune && !province) return true;
  return (!!commune && s.has(commune)) || (!!province && s.has(province));
}

/** Ảnh chụp đã lưu: job 00:45 chụp hôm qua; mock giữ 14 ngày gần nhất (cũ hơn đã chuyển tầng lưu trữ — RB-1). */
export const SNAPSHOT_DAYS = 14;

function hasSnapshot(date: string, today: string): boolean {
  const d = daysBetween(date, today);
  return d >= 1 && d <= SNAPSHOT_DAYS;
}

function latestSnapshotOnOrBefore(date: string, today: string): string | null {
  for (let d = date; daysBetween(d, today) <= SNAPSHOT_DAYS; d = addDays(d, -1)) if (hasSnapshot(d, today)) return d;
  return null;
}

function toStates(
  fold: { layers: Map<string, Map<string, FoldedFeature>> },
  layers: readonly string[],
  p: MockPersona,
  meta: { date: string; basis: GisLayerState["basis"]; base: string | null; recordedAt?: string; geometry: boolean },
  events: readonly GisLayerEvent[],
): GisLayerState[] {
  const cutoff = Date.parse(cutoffOf(meta.date));
  return layers.map((layer) => {
    const feats = Array.from(fold.layers.get(layer) ?? new Map<string, FoldedFeature>(), ([ref, f]) => ({ ref, ...f })).filter((f) => eventVisible(p, f.attributes));
    const last = events.filter((e) => e.layer === layer && Date.parse(e.recorded_at) < cutoff).at(-1);
    const features: GisLayerFeature[] = feats.map((f) => {
      const g = meta.geometry && typeof f.attributes?.geometry_ref === "string" ? GEOMETRY.get(f.attributes.geometry_ref) : undefined;
      return { feature_ref: f.ref, attributes: f.attributes, retired: f.retired, ...(g ? { geometry: g as unknown as Record<string, unknown> } : {}) };
    });
    return {
      layer,
      business_date: meta.date,
      watermark: last ? { last_event_id: last.id, recorded_at: last.recorded_at } : null,
      basis: meta.basis,
      base_snapshot_date: meta.base,
      feature_count: features.length,
      features,
      ...(meta.recordedAt ? { recorded_at: meta.recordedAt } : {}),
    };
  });
}

function parseLayers(v: string | null): string[] | Response {
  if (!v) return [...GIS_LAYERS];
  const list = v.split(",").map((s) => s.trim()).filter(Boolean);
  const bad = list.find((l) => !(GIS_LAYERS as readonly string[]).includes(l));
  return bad ? problem(404, "metrics.unknown_layer", "Unknown layer", { detail: bad }) : list;
}

export const metricsRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/metrics\/definitions$/,
    handler: ({ persona }) => {
      const items = METRIC_DEFS.filter((d) => canSeeDef(persona, d)).map(strip);
      return items.length ? json({ items }) : problem(403, "forbidden", "Forbidden");
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/metrics\/([a-z0-9_.]+)\/values$/,
    handler: ({ persona, params, url }) => {
      const key = params[0] ?? "";
      const versions = METRIC_DEFS.filter((d) => d.key === key);
      const d = versions[0];
      if (!d) return problem(404, "metrics.unknown_metric", "Unknown metric");
      if (!canSeeDef(persona, d)) return problem(403, "forbidden", "Forbidden");
      const unit = url.searchParams.get("unit") ?? "";
      const s = scopeUnits(persona);
      if (unit === "" ? !!s : (s && !s.has(unit)) || !ADMIN_UNITS.some((u) => u.code === unit)) return problem(403, "metrics.out_of_scope", "Out of scope");
      const yesterday = addDays(vnToday(), -1);
      const to = url.searchParams.get("to") ?? yesterday;
      const from = url.searchParams.get("from") ?? addDays(to, -30);
      if (!isIsoDate(from) || !isIsoDate(to)) return validation([{ field: isIsoDate(from) ? "to" : "from", reason: "Ngày YYYY-MM-DD" }]);
      if (from > to || daysBetween(from, to) > 400) return problem(422, "metrics.range_too_large", "Range too large");
      const dimension = url.searchParams.get("dimension") ?? "";
      const history = url.searchParams.get("history") === "true";
      const seasonal = d.grain === "season";
      const items = seasonal && !dimension ? [] : metricValues(d, versions, unit, seasonal ? dimension : "", from, to, history);
      const latest = [...versions].sort((a, b) => b.version - a.version)[0]!;
      return json({ key, title: latest.title, unit: d.unit, grain: d.grain, scope_unit: unit, dimension: seasonal ? dimension : "", from, to, items, dimensions: seasonal ? SEASON_DIMENSIONS : [] });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/gis\/events$/,
    handler: ({ persona, url }) => {
      if (!hasRole(persona, ...HISTORY_ROLES)) return problem(403, "forbidden", "Forbidden");
      const layer = url.searchParams.get("layer");
      if (layer && !(GIS_LAYERS as readonly string[]).includes(layer)) return problem(404, "metrics.unknown_layer", "Unknown layer");
      const today = vnToday();
      const to = url.searchParams.get("to") ?? today;
      const from = url.searchParams.get("from") ?? addDays(to, -6);
      if (!isIsoDate(from) || !isIsoDate(to)) return validation([{ field: "from", reason: "Ngày YYYY-MM-DD" }]);
      if (from > to || daysBetween(from, to) > 400) return problem(422, "metrics.range_too_large", "Range too large");
      const start = Date.parse(cutoffOf(addDays(from, -1)));
      const end = Date.parse(cutoffOf(to));
      const ref = url.searchParams.get("feature_ref");
      const list = log()
        .events.filter((e) => {
          const t = Date.parse(e.recorded_at);
          return t >= start && t < end && (!layer || e.layer === layer) && (!ref || e.feature_ref === ref) && eventVisible(persona, (e.after ?? e.before) as Record<string, unknown> | null);
        })
        .sort((a, b) => Date.parse(a.recorded_at) - Date.parse(b.recorded_at) || a.id.localeCompare(b.id));
      const { slice, next } = pageSlice(list, url, 100, 500);
      return json({ items: slice, next_cursor: next, from, to });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/gis\/snapshots\/(\d{4}-\d{2}-\d{2})$/,
    handler: ({ persona, params, url }) => {
      if (!hasRole(persona, ...HISTORY_ROLES)) return problem(403, "forbidden", "Forbidden");
      const date = params[0] ?? "";
      if (!isIsoDate(date)) return validation([{ field: "date", reason: "Ngày YYYY-MM-DD" }]);
      const layers = parseLayers(url.searchParams.get("layers"));
      if (layers instanceof Response) return layers;
      const today = vnToday();
      if (!hasSnapshot(date, today)) return json({ business_date: date, items: [] });
      const events = log().events;
      const fold = foldLayerEvents(events, date);
      const recordedAt = new Date(Date.parse(cutoffOf(date)) + 45 * 60_000).toISOString();
      return json({ business_date: date, items: toStates(fold, layers, persona, { date, basis: "snapshot", base: date, recordedAt, geometry: false }, events) });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/gis\/replay$/,
    handler: ({ persona, url }) => {
      if (!hasRole(persona, ...HISTORY_ROLES)) return problem(403, "forbidden", "Forbidden");
      const date = url.searchParams.get("date") ?? "";
      if (!isIsoDate(date)) return validation([{ field: "date", reason: "Ngày YYYY-MM-DD" }]);
      const today = vnToday();
      if (date > today) return problem(422, "metrics.future_date", "Future date");
      const layers = parseLayers(url.searchParams.get("layers"));
      if (layers instanceof Response) return layers;
      const events = log().events;
      // Ảnh chụp mới nhất ≤ date + các dòng sau watermark (bằng đúng dựng thẳng từ nhật ký).
      const base = date < today ? latestSnapshotOnOrBefore(date, today) : latestSnapshotOnOrBefore(addDays(today, -1), today);
      const baseFold = base ? foldLayerEvents(events, base) : null;
      const fold = foldLayerEvents(events, date, baseFold);
      const basis: GisLayerState["basis"] = base === date ? "snapshot" : "replay";
      return json({
        business_date: date,
        cutoff: cutoffOf(date),
        items: toStates(fold, layers, persona, { date, basis, base, geometry: url.searchParams.get("geometry") === "true" }, events),
      });
    },
  },
];

/** Cho kiểm thử: nhật ký lớp giả lập. */
export const mockLayerEvents = () => log().events;
