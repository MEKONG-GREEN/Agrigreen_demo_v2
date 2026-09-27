import type { CoverageThresholds, DataSource } from "@/api/pending/cgh";
import { COVERAGE_STYLES, legendEntries, type CoverageStatus } from "@/cgh/coverage";

export function CoverageLegend({ thresholds }: { thresholds: CoverageThresholds | null }) {
  return (
    <div>
      <h3 style={{ fontSize: 13.5, marginBottom: 8 }} id="legend-title">
        Mức đáp ứng máy
      </h3>
      <ul className="legend" aria-labelledby="legend-title">
        {legendEntries(thresholds).map((e) => (
          <li key={e.status}>
            <span className="sw" style={{ background: COVERAGE_STYLES[e.status].fill }} aria-hidden="true" />
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

const SOURCE: Record<DataSource, string> = { app_htx: "[App HTX]", manual: "[Nhập tay]", none: "[Chưa có dữ liệu]" };

export function SourceLabel({ source }: { source: DataSource }) {
  return <span className="mute small">{SOURCE[source]}</span>;
}
