import type { MechCoverageStatus } from "@/api/types";
import { COVERAGE_STYLES, fromMechStatus, legendEntries, type CoverageStatus, type CoverageThresholds } from "@/cgh/coverage";

/**
 * Chú giải mức đáp ứng. `legend` là chú giải máy chủ trả kèm kết quả cân đối (GET /v1/mechanization/coverage):
 * có thì dùng đúng màu đó (cùng màu tô bản đồ, FN-08 AC-01); không có thì dùng màu mặc định.
 */
export function CoverageLegend({
  thresholds,
  legend,
}: {
  thresholds: CoverageThresholds | null;
  legend?: readonly { status: MechCoverageStatus; color: string }[] | null;
}) {
  const colors = new Map<CoverageStatus, string>();
  for (const l of legend ?? []) if (!colors.has(fromMechStatus(l.status))) colors.set(fromMechStatus(l.status), l.color);
  return (
    <div>
      <h3 style={{ fontSize: 13.5, marginBottom: 8 }} id="legend-title">
        Mức đáp ứng máy
      </h3>
      <ul className="legend" aria-labelledby="legend-title">
        {legendEntries(thresholds).map((e) => (
          <li key={e.status} data-status={e.status}>
            <span className="sw" style={{ background: colors.get(e.status) ?? COVERAGE_STYLES[e.status].fill }} aria-hidden="true" data-color={colors.get(e.status) ?? COVERAGE_STYLES[e.status].fill} />
            <b style={{ minWidth: 104 }}>{COVERAGE_STYLES[e.status].label}</b>
            <span className="mute small">{e.text}</span>
          </li>
        ))}
      </ul>
      {!thresholds ? (
        <p className="small" style={{ marginTop: 8, color: "var(--amber)" }}>
          Chưa cấu hình ngưỡng cảnh báo — số liệu không được tô màu phân loại.
        </p>
      ) : null}
    </div>
  );
}

export function CoverageChip({ status }: { status: CoverageStatus }) {
  const s = COVERAGE_STYLES[status];
  return (
    <span className="chip" style={{ background: s.bg, color: s.ink }} data-status={status}>
      <span aria-hidden="true" style={{ width: 8, height: 8, borderRadius: 4, background: s.fill, display: "inline-block" }} />
      {s.label}
    </span>
  );
}

/** Nhãn nguồn dữ liệu theo QT-03: ưu tiên nhãn backend trả (G-10 `area_source_label`), không có thì suy từ mã nguồn. */
export function SourceLabel({ source, label }: { source: string | null | undefined; label?: string | null }) {
  const text = label ?? (source === "app_htx" ? "[App HTX]" : source === "manual" ? "[Nhập tay]" : "[Chưa có dữ liệu]");
  return <span className="mute small">{text}</span>;
}
