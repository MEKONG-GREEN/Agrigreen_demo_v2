export interface BarDatum {
  key: string;
  label: string;
  value: number;
  /** Mục tiêu (vẽ vạch mốc). */
  target?: number;
  muted?: boolean;
  note?: string;
}

/** Biểu đồ thanh ngang SVG thuần, có bảng dữ liệu ẩn cho trình đọc màn hình. */
export function BarChart({ data, unit, title }: { data: BarDatum[]; unit: string; title: string }) {
  const max = Math.max(1, ...data.map((d) => Math.max(d.value, d.target ?? 0)));
  const rowH = 34;
  const labelW = 130;
  const width = 560;
  const barW = width - labelW - 90;
  const fmt = new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 0 });
  return (
    <figure style={{ margin: 0 }}>
      <svg
        viewBox={`0 0 ${width} ${data.length * rowH + 8}`}
        width="100%"
        role="img"
        aria-label={title}
        style={{ display: "block" }}
      >
        {data.map((d, i) => {
          const y = i * rowH + 6;
          const w = (d.value / max) * barW;
          const tx = d.target ? labelW + (d.target / max) * barW : null;
          return (
            <g key={d.key}>
              <text x={0} y={y + 16} fontSize={12.5} fill="#33473f">
                {d.label.length > 18 ? `${d.label.slice(0, 17)}…` : d.label}
              </text>
              <rect x={labelW} y={y + 4} width={barW} height={16} rx={8} fill="#edf3ef" />
              <rect x={labelW} y={y + 4} width={Math.max(w, 2)} height={16} rx={8} fill={d.muted ? "#b8c2bd" : "#1b8a54"} />
              {tx !== null ? <line x1={tx} x2={tx} y1={y} y2={y + 24} stroke="#8a5300" strokeWidth={2} strokeDasharray="3 2" /> : null}
              <text x={labelW + barW + 8} y={y + 16} fontSize={12} fill="#0f1f1a" fontWeight={600}>
                {d.note ?? fmt.format(d.value)}
              </text>
            </g>
          );
        })}
      </svg>
      <figcaption className="sr-only">
        <table>
          <caption>{title}</caption>
          <thead>
            <tr>
              <th>Đơn vị</th>
              <th>Giá trị ({unit})</th>
              <th>Mục tiêu</th>
            </tr>
          </thead>
          <tbody>
            {data.map((d) => (
              <tr key={d.key}>
                <td>{d.label}</td>
                <td>{d.note ?? fmt.format(d.value)}</td>
                <td>{d.target ? fmt.format(d.target) : "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </figcaption>
    </figure>
  );
}
