// Bản đồ lô ruộng cho Quản lý HTX (HTX US-PLOT-01): lô của HTX và lô Cán bộ KN vẽ trước (nháp, chưa gán HTX)
// trên CÙNG một bản ghi pf/geo — không có dữ liệu song song.
import type { AreaCollection } from "@/components/MapView";
import type { PlotFeature } from "@/api/types";
import { toAreaGeometry } from "@/lib/geojson";

/** Nhãn AC-1 cho lô KN vẽ trước chưa gán HTX. */
export const DRAFT_LABEL = "Nháp - KN vẽ trước";

export interface PlotStatusView {
  text: string;
  cls: string;
  fill: string;
}

/** Trạng thái hiển thị của một lô với HTX đang xem. */
export function plotStatus(p: PlotFeature, coopRef: string): PlotStatusView {
  const { status, cooperative_ref: coop, needs_review: review } = p.properties;
  if (status === "retired") return { text: "Ngừng dùng", cls: "c-mute", fill: "#9aa5ad" };
  if (status === "draft" && !coop) return { text: DRAFT_LABEL, cls: "c-warn", fill: "#f0a02c" };
  if (coop && coop !== coopRef) return { text: "Thuộc HTX khác", cls: "c-mute", fill: "#c3c9cf" };
  if (review) return { text: "Có chồng lấn chờ xem xét", cls: "c-bad", fill: "#d9534f" };
  return { text: status === "draft" ? "Nháp của HTX" : "Đang dùng", cls: "c-ok", fill: "#2e8b57" };
}

/** Gộp lô của HTX và lô nháp KN trong xã (bỏ trùng theo ref) → vùng cho MapView. */
export function plotAreas(own: readonly PlotFeature[], drafts: readonly PlotFeature[], coopRef: string): AreaCollection {
  const seen = new Set<string>();
  const features: AreaCollection["features"] = [];
  for (const p of [...own, ...drafts]) {
    if (seen.has(p.properties.ref)) continue;
    seen.add(p.properties.ref);
    const g = toAreaGeometry(p.geometry);
    if (!g) continue;
    const st = plotStatus(p, coopRef);
    features.push({
      type: "Feature",
      geometry: g,
      properties: { code: p.properties.ref, name: p.properties.code, fill: st.fill, status_label: st.text },
    });
  }
  return { type: "FeatureCollection", features };
}

/** Danh sách lô hiển thị kèm trạng thái, lô của HTX trước, nháp KN sau. */
export function plotRows(own: readonly PlotFeature[], drafts: readonly PlotFeature[], coopRef: string): { plot: PlotFeature; status: PlotStatusView; draft: boolean }[] {
  const ownRefs = new Set(own.map((p) => p.properties.ref));
  return [
    ...own.map((plot) => ({ plot, status: plotStatus(plot, coopRef), draft: false })),
    ...drafts.filter((p) => !ownRefs.has(p.properties.ref)).map((plot) => ({ plot, status: plotStatus(plot, coopRef), draft: true })),
  ];
}
