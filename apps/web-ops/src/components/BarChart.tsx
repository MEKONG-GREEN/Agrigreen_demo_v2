import { Decimal, formatDecimalVi } from "@/lib/exactDecimal";

export interface BarDatum {
  key: string;
  label: string;
  value: number;
  /** Mục tiêu (vẽ vạch mốc). */
  target?: number;
  muted?: boolean;
  note?: string;
}

export interface ColumnSeries {
  key: string;
  label: string;
  color: string;
}

export interface ColumnDatum {
  key: string;
  label: string;
  note: string;
  values: Record<string, string>;
}

/** Cột chồng/cột đôi; số tiền giữ nguyên decimal, chỉ tỷ lệ hình học SVG đổi sang Number. */
export function ColumnChart({ title, unit, data, series, stacked = false }: {
  title: string;
  unit: string;
  data: ColumnDatum[];
  series: ColumnSeries[];
  stacked?: boolean;
}) {
  const parsed = data.map((d) => series.map((s) => {
    const raw = d.values[s.key];
    // Giới hạn trước parse: không exponent, không chuỗi số quá dài từ phản hồi API.
    return raw && raw.length <= 37 && /^\d{1,30}(?:\.\d{1,6})?$/.test(raw) ? Decimal.parse(raw) : null;
  }));
  const invalid = parsed.some((row) => row.some((v) => v === null));
  const maxima = parsed.map((row) => stacked
    ? row.reduce<Decimal>((sum, v) => sum.add(v ?? Decimal.ZERO), Decimal.ZERO)
    : row.reduce<Decimal>((max, v) => v && v.gt(max) ? v : max, Decimal.ZERO));
  const max = maxima.reduce((a, b) => b.gt(a) ? b : a, Decimal.ZERO);
  const width = Math.max(640, 100 + data.length * 160);
  const left = 100, bottom = 240, height = 180;
  const slot = (width - left - 24) / Math.max(1, data.length);
  const pixels = (v: Decimal) => max.isZero() ? 0 : Number(v.divRound(max, 8).toString()) * height;
  return (
    <figure className="card" style={{ margin: "0 0 16px", padding: 16 }}>
      <figcaption>
        <h2 style={{ fontSize: 17, margin: "0 0 8px" }}>{title}</h2>
        <p className="muted" style={{ margin: "0 0 12px" }}>Đơn vị: {unit}. Số liệu và nhãn Mẫu nhỏ xem tại bảng cùng bộ lọc.</p>
      </figcaption>
      <div className="row" style={{ flexWrap: "wrap", gap: 16, marginBottom: 8 }}>
        {series.map((s) => <span key={s.key}><span aria-hidden="true" style={{ display: "inline-block", width: 12, height: 12, background: s.color, marginRight: 6 }} />{s.label}</span>)}
      </div>
      {!data.length ? <p role="status">Không có phiếu đã duyệt phù hợp để vẽ biểu đồ.</p> : invalid ? (
        <p role="alert">Không thể vẽ biểu đồ do dữ liệu số không hợp lệ. Xem bảng dữ liệu hoặc thử tải lại.</p>
      ) : (
        <div className="tbl-wrap" tabIndex={0} role="region" aria-label={`Cuộn biểu đồ ${title}`}>
          <svg viewBox={`0 0 ${width} 320`} width="100%" role="img" aria-label={title} style={{ display: "block", minWidth: width }}>
            <desc>{stacked ? "Mỗi cột gồm các khoản chi phí bình quân của một mặt hàng." : "Mỗi cặp cột so sánh giá thành và giá bán bình quân của một mặt hàng trong một vùng."} Bảng dữ liệu bên cạnh biểu đồ cung cấp toàn bộ giá trị.</desc>
            {[0, 1, 2, 3, 4].filter((i) => !max.isZero() || i === 0).map((i) => {
              const y = bottom - height * i / 4;
              const tick = max.mul(Decimal.fromInt(i)).divRound(Decimal.fromInt(4), 0).toString();
              return <g key={i}>
                <line x1={left} x2={width - 24} y1={y} y2={y} stroke="#e4ece7" />
                <text x={left - 8} y={y + 4} textAnchor="end" fontSize={11} fill="#5b6f66">{formatDecimalVi(tick, { maxDecimals: 0, group: true })}</text>
              </g>;
            })}
            {data.map((d, i) => {
              let offset = 0;
              const groupWidth = Math.min(80, slot - 24);
              const x = left + i * slot + (slot - groupWidth) / 2;
              return <g key={d.key}>
                {series.map((s, j) => {
                  const h = pixels(parsed[i]![j]!);
                  const y = bottom - h - (stacked ? offset : 0);
                  offset += h;
                  const barWidth = stacked ? groupWidth : groupWidth / series.length;
                  return <rect key={s.key} x={x + (stacked ? 0 : j * barWidth)} y={y} width={barWidth} height={h} fill={s.color} stroke="white" strokeWidth={1}>
                    <title>{`${d.label} · ${s.label}: ${formatDecimalVi(d.values[s.key]!, { maxDecimals: 0, group: true })} ${unit} · ${d.note}`}</title>
                  </rect>;
                })}
                <text x={left + (i + 0.5) * slot} y={bottom + 22} textAnchor="middle" fontSize={12} fill="#33473f">
                  <title>{d.label}</title>{d.label.length > 22 ? `${d.label.slice(0, 21)}…` : d.label}
                </text>
                <text x={left + (i + 0.5) * slot} y={bottom + 42} textAnchor="middle" fontSize={11} fill="#5b6f66">{d.note}</text>
              </g>;
            })}
          </svg>
        </div>
      )}
    </figure>
  );
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
