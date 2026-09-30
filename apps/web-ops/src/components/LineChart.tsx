import { chartGeometry, type SeriesPoint } from "@/metrics/series";
import { formatDecimal } from "@/lib/decimal";
import { formatDate } from "@/lib/format";

const BOX = { width: 640, height: 220, left: 56, right: 14, top: 12, bottom: 28 };

function shortDate(d: string): string {
  return `${d.slice(8, 10)}/${d.slice(5, 7)}`;
}

/**
 * Biểu đồ đường SVG thuần theo ngày (lịch sử chỉ số đã chốt), có bảng dữ liệu ẩn cho trình đọc màn hình.
 * Điểm có viền cam = ngày đã được tính lại (bản ghi bù); vạch đứt = đổi phiên bản định nghĩa.
 */
export function LineChart({
  title,
  unit,
  points,
  from,
  to,
  versionChanges = [],
  digits = 1,
}: {
  title: string;
  unit: string;
  points: SeriesPoint[];
  from: string;
  to: string;
  versionChanges?: { date: string; from: number; to: number }[];
  digits?: number;
}) {
  const g = chartGeometry(points, from, to, BOX);
  return (
    <figure style={{ margin: 0 }} data-testid="line-chart" data-points={points.length}>
      <svg viewBox={`0 0 ${BOX.width} ${BOX.height}`} width="100%" role="img" aria-label={title} style={{ display: "block" }}>
        {g.yTicks.map((t) => (
          <g key={t.value}>
            <line x1={BOX.left} x2={BOX.width - BOX.right} y1={t.y} y2={t.y} stroke="#e4ece7" />
            <text x={BOX.left - 6} y={t.y + 4} fontSize={11} fill="#5b6f66" textAnchor="end">
              {formatDecimal(t.value, t.value % 1 === 0 ? 0 : digits)}
            </text>
          </g>
        ))}
        {g.xTicks.map((t) => (
          <text key={t.date} x={t.x} y={BOX.height - 8} fontSize={11} fill="#5b6f66" textAnchor="middle">
            {shortDate(t.date)}
          </text>
        ))}
        {versionChanges.map((v) => (
          <g key={v.date}>
            <line x1={g.xOf(v.date)} x2={g.xOf(v.date)} y1={BOX.top} y2={BOX.height - BOX.bottom} stroke="#8a5300" strokeDasharray="4 3" />
            <text x={g.xOf(v.date) + 4} y={BOX.top + 10} fontSize={10.5} fill="#8a5300">
              v{v.to}
            </text>
          </g>
        ))}
        <path d={g.path} fill="none" stroke="#1b8a54" strokeWidth={2} />
        {g.dots.map((d) => (
          <circle key={d.point.date} cx={d.x} cy={d.y} r={d.point.corrected ? 4 : 2.5} fill={d.point.corrected ? "#fff" : "#1b8a54"} stroke={d.point.corrected ? "#d9822b" : "#1b8a54"} strokeWidth={d.point.corrected ? 2 : 1}>
            <title>{`${formatDate(d.point.date)}: ${formatDecimal(d.point.raw, digits)} ${unit}${d.point.corrected ? " (đã tính lại)" : ""}`}</title>
          </circle>
        ))}
      </svg>
      <figcaption className="sr-only">
        <table>
          <caption>{title}</caption>
          <thead>
            <tr>
              <th>Ngày</th>
              <th>Giá trị ({unit})</th>
              <th>Phiên bản định nghĩa</th>
            </tr>
          </thead>
          <tbody>
            {points.map((p) => (
              <tr key={p.date}>
                <td>{formatDate(p.date)}</td>
                <td>
                  {formatDecimal(p.raw, digits)}
                  {p.corrected ? " (đã tính lại)" : ""}
                </td>
                <td>v{p.version}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </figcaption>
    </figure>
  );
}
