// Xem lại lớp GIS theo ngày (GIS US-EVL-01, US-SNAP-01, US-REPLAY-01) — hàm thuần, có Vitest.
// Máy chủ là nơi dựng trạng thái as-of; `foldLayerEvents` mô tả cùng quy tắc (dùng cho backend giả lập
// và để kiểm): áp các dòng nhật ký có recorded_at < Cutoff(D) theo thứ tự (recorded_at, id).
import type { GisLayerEvent, GisLayerState } from "@/api/types";
import type { AnyFeatureCollection } from "@/components/LayerMap";
import { cutoffOf, isIsoDate } from "@/lib/vnDate";

export const GIS_LAYERS = ["plot", "coop_boundary", "facility", "season_stage", "cgh_balance", "waterway"] as const;
export type GisLayer = (typeof GIS_LAYERS)[number];

export const LAYER_NAME: Record<string, string> = {
  plot: "Thửa ruộng",
  coop_boundary: "Ranh giới HTX",
  facility: "Kho / Hub",
  season_stage: "Giai đoạn mùa vụ",
  cgh_balance: "Cân đối máy CGH",
  waterway: "Đường thủy",
};

/** Màu vẽ lớp khi xem lại (không theo trạng thái nghiệp vụ — chỉ phân biệt lớp). */
export const REPLAY_COLOR: Record<string, string> = {
  plot: "#6aa84f",
  coop_boundary: "#0b6e4f",
  facility: "#f0871a",
  season_stage: "#b58900",
  cgh_balance: "#8e44ad",
  waterway: "#0057b8",
};

export const OP_LABEL: Record<GisLayerEvent["op"], string> = {
  create: "Tạo mới",
  update: "Sửa",
  retire: "Ngừng dùng",
  restore: "Khôi phục",
};

/** Kiểm ngày xem lại: đúng định dạng, không ở tương lai (máy chủ trả 422 metrics.future_date). */
export function validateReplayDate(date: string, today: string): string | null {
  if (!isIsoDate(date)) return "Chọn ngày hợp lệ (dd/mm/yyyy).";
  if (date > today) return "Không thể xem lại một ngày trong tương lai.";
  return null;
}

export interface FoldedFeature {
  attributes: Record<string, unknown> | null;
  retired: boolean;
  lastEventId: string;
}

export interface FoldResult {
  layers: Map<string, Map<string, FoldedFeature>>;
  watermark: { last_event_id: string; recorded_at: string } | null;
  applied: number;
}

/** Thứ tự áp của nhật ký: (recorded_at, id). */
export function compareEvents(a: Pick<GisLayerEvent, "recorded_at" | "id">, b: Pick<GisLayerEvent, "recorded_at" | "id">): number {
  return Date.parse(a.recorded_at) - Date.parse(b.recorded_at) || a.id.localeCompare(b.id);
}

/**
 * Trạng thái mọi lớp "như hệ biết lúc hết ngày `date`": áp mọi dòng recorded_at < Cutoff(date).
 * Có `base` (ảnh chụp) thì chỉ áp các dòng SAU watermark của ảnh chụp — kết quả phải bằng dựng thẳng từ nhật ký.
 */
export function foldLayerEvents(
  events: readonly GisLayerEvent[],
  date: string,
  base?: { layers: Map<string, Map<string, FoldedFeature>>; watermark: { last_event_id: string; recorded_at: string } | null } | null,
): FoldResult {
  const cutoff = Date.parse(cutoffOf(date));
  const layers = new Map<string, Map<string, FoldedFeature>>();
  for (const [layer, feats] of base?.layers ?? []) layers.set(layer, new Map(Array.from(feats, ([k, v]) => [k, { ...v }])));
  let watermark = base?.watermark ?? null;
  let applied = 0;
  const ordered = [...events].sort(compareEvents);
  for (const e of ordered) {
    if (Date.parse(e.recorded_at) >= cutoff) break;
    if (watermark && compareEvents(e, { recorded_at: watermark.recorded_at, id: watermark.last_event_id }) <= 0) continue;
    let feats = layers.get(e.layer);
    if (!feats) layers.set(e.layer, (feats = new Map()));
    const prev = feats.get(e.feature_ref);
    const attrs = (e.after ?? e.before ?? prev?.attributes ?? null) as Record<string, unknown> | null;
    if (e.op === "create" || e.op === "update") feats.set(e.feature_ref, { attributes: attrs, retired: false, lastEventId: e.id });
    else if (e.op === "retire") feats.set(e.feature_ref, { attributes: attrs, retired: true, lastEventId: e.id });
    else feats.set(e.feature_ref, { attributes: attrs, retired: false, lastEventId: e.id });
    watermark = { last_event_id: e.id, recorded_at: e.recorded_at };
    applied++;
  }
  return { layers, watermark, applied };
}

/** Số đối tượng đang dùng (không tính đã ngừng) của một lớp as-of. */
export function activeCount(state: Pick<GisLayerState, "features">): number {
  return state.features.filter((f) => !f.retired).length;
}

/** Tên hiển thị của đối tượng từ thuộc tính (name/code), không có → mã rút gọn. */
export function featureTitle(ref: string, attrs: Record<string, unknown> | null | undefined): string {
  const name = attrs?.name ?? attrs?.code;
  return typeof name === "string" && name ? name : `#${ref.slice(-6)}`;
}

/** Mọi lớp as-of → một FeatureCollection để vẽ (bỏ đối tượng đã ngừng dùng và đối tượng không có hình học). */
export function replayFeatureCollection(items: readonly GisLayerState[]): AnyFeatureCollection {
  const features: AnyFeatureCollection["features"] = [];
  for (const st of items) {
    for (const f of st.features) {
      if (f.retired || !f.geometry || typeof (f.geometry as { type?: unknown }).type !== "string") continue;
      features.push({
        type: "Feature",
        id: `${st.layer}:${f.feature_ref}`,
        geometry: f.geometry,
        properties: { layer: st.layer, layer_name: LAYER_NAME[st.layer] ?? st.layer, feature_ref: f.feature_ref, name: featureTitle(f.feature_ref, f.attributes), color: REPLAY_COLOR[st.layer] ?? "#555" },
      });
    }
  }
  return { type: "FeatureCollection", features };
}

export interface AttrDiff {
  key: string;
  before: string;
  after: string;
  changed: boolean;
}

function show(v: unknown): string {
  if (v === undefined || v === null || v === "") return "—";
  if (typeof v === "boolean") return v ? "Có" : "Không";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}

/** Bảng trước/sau của một dòng nhật ký (GIS US-EVL-01): hợp các khóa, đánh dấu khóa đổi. */
export function diffAttributes(before: Record<string, unknown> | null | undefined, after: Record<string, unknown> | null | undefined): AttrDiff[] {
  const keys = Array.from(new Set([...Object.keys(before ?? {}), ...Object.keys(after ?? {})])).sort();
  return keys.map((key) => {
    const b = show(before?.[key]);
    const a = show(after?.[key]);
    return { key, before: b, after: a, changed: b !== a };
  });
}

export type SnapshotBadge = { kind: "snapshot"; date: string; watermarkAt: string | null } | { kind: "replay"; baseDate: string | null } | { kind: "live" };

/**
 * Huy hiệu ảnh chụp (GIS US-SNAP-01): ngày có ảnh chụp đã lưu → "Ảnh chụp cuối ngày"; không → dựng từ ảnh
 * chụp gần nhất trước đó + nhật ký; hôm nay → trạng thái hiện tại.
 */
export function snapshotBadge(date: string, today: string, snapshots: readonly GisLayerState[] | null | undefined, replay: readonly GisLayerState[] | null | undefined): SnapshotBadge {
  if (date >= today) return { kind: "live" };
  const snap = (snapshots ?? []).find((s) => s.business_date === date);
  if (snap) return { kind: "snapshot", date, watermarkAt: snap.watermark?.recorded_at ?? null };
  const base = (replay ?? []).map((s) => s.base_snapshot_date).filter((d): d is string => !!d).sort().at(-1) ?? null;
  return { kind: "replay", baseDate: base };
}
