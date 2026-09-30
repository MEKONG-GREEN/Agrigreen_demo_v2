"use client";

import { useId, useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { LineChart } from "@/components/LineChart";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { formatDecimal } from "@/lib/decimal";
import { formatDate, formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";
import { rangeFor, shapeSeries, unitLabel } from "./series";

export interface MetricOption {
  key: string;
  label: string;
  digits?: number;
}

const RANGES = [
  { days: 30, label: "30 ngày" },
  { days: 90, label: "90 ngày" },
  { days: 365, label: "12 tháng" },
];

/**
 * Lịch sử chỉ số theo ngày đã chốt (GET /v1/metrics/{key}/values) — KN US-DASH-01/02, CGH FN-10/11.
 * Kỳ đã chốt không bị sửa: tính lại tạo dòng mới; bật "Xem giá trị đã bị thay" để thấy lịch sử (history=true).
 * Chỉ số theo vụ (grain = season) cần chọn vụ (`dimension`) từ danh sách vụ đã ghi.
 */
export function MetricHistory({
  title,
  metrics,
  units,
  defaultUnit = "",
  seasonal = false,
}: {
  title: string;
  metrics: MetricOption[];
  /** Đơn vị chọn được: "" = toàn phạm vi (chỉ khi người dùng có phạm vi TW). */
  units: { code: string; name: string }[];
  defaultUnit?: string;
  seasonal?: boolean;
}) {
  const { api } = useAuth();
  const id = useId();
  const [key, setKey] = useState(metrics[0]?.key ?? "");
  const [unit, setUnit] = useState(defaultUnit);
  const [days, setDays] = useState(30);
  const [dimension, setDimension] = useState("");
  const [history, setHistory] = useState(false);
  const range = useMemo(() => rangeFor(days), [days]);
  const opt = metrics.find((m) => m.key === key);
  const digits = opt?.digits ?? 1;

  const res = useAsync(
    () =>
      unwrap(
        api.GET("/v1/metrics/{key}/values", {
          params: { path: { key }, query: { from: range.from, to: range.to, unit: unit || undefined, dimension: dimension || undefined, history } },
        }),
      ),
    [api, key, unit, range.from, range.to, dimension, history],
  );
  const data = res.data;
  const dims = data?.dimensions ?? [];
  // Chỉ số theo vụ: tự chọn vụ mới nhất khi máy chủ trả danh sách vụ đã ghi.
  const effectiveDim = seasonal && !dimension && dims.length > 0 ? dims[dims.length - 1]! : dimension;
  if (effectiveDim !== dimension) setDimension(effectiveDim);
  const shaped = useMemo(() => shapeSeries(data?.items ?? []), [data]);
  const last = shaped.points.at(-1);
  const u = data ? unitLabel(data.unit) : "";

  return (
    <section className="card sec" aria-labelledby={`${id}-h`} data-testid="metric-history">
      <div className="sec-h" style={{ flexWrap: "wrap" }}>
        <h2 id={`${id}-h`}>{title}</h2>
        <span className="mute small">Giá trị cuối ngày đã chốt (như hệ biết lúc 00:00 hôm sau) · job chụp 00:45</span>
      </div>
      <div className="row" style={{ flexWrap: "wrap", gap: 12, marginBottom: 10, alignItems: "flex-end" }}>
        <div className="field">
          <label htmlFor={`${id}-key`}>Chỉ số</label>
          <select id={`${id}-key`} className="select" value={key} onChange={(e) => setKey(e.target.value)}>
            {metrics.map((m) => (
              <option key={m.key} value={m.key}>
                {m.label}
              </option>
            ))}
          </select>
        </div>
        {units.length > 1 ? (
          <div className="field">
            <label htmlFor={`${id}-unit`}>Đơn vị</label>
            <select id={`${id}-unit`} className="select" value={unit} onChange={(e) => setUnit(e.target.value)}>
              {units.map((x) => (
                <option key={x.code} value={x.code}>
                  {x.name}
                </option>
              ))}
            </select>
          </div>
        ) : null}
        {seasonal ? (
          <div className="field">
            <label htmlFor={`${id}-dim`}>Vụ</label>
            <select id={`${id}-dim`} className="select" value={dimension} onChange={(e) => setDimension(e.target.value)}>
              {dims.length === 0 ? <option value="">Chưa có vụ đã ghi</option> : null}
              {dims.map((d) => (
                <option key={d} value={d}>
                  {d}
                </option>
              ))}
            </select>
          </div>
        ) : null}
        <div className="field">
          <label htmlFor={`${id}-range`}>Khoảng thời gian</label>
          <select id={`${id}-range`} className="select" value={days} onChange={(e) => setDays(Number(e.target.value))}>
            {RANGES.map((r) => (
              <option key={r.days} value={r.days}>
                {r.label}
              </option>
            ))}
          </select>
        </div>
        <label className="row small" style={{ gap: 6 }}>
          <input type="checkbox" checked={history} onChange={(e) => setHistory(e.target.checked)} />
          Xem giá trị đã bị thay (kỳ đã chốt)
        </label>
      </div>
      {res.error ? <ProblemAlert problem={res.error} onRetry={res.reload} /> : null}
      {res.loading && !data ? <Loading /> : null}
      {data ? (
        <>
          <p className="small" data-testid="metric-latest">
            {last ? (
              <>
                <b style={{ display: "inline" }}>{data.title}</b> ngày {formatDate(last.date)}: <b style={{ display: "inline" }}>{formatDecimal(last.raw, digits)}</b> {u} · định nghĩa v
                {last.version}
              </>
            ) : seasonal && !effectiveDim ? (
              "Chưa có vụ nào được ghi cho chỉ số này."
            ) : (
              "Chưa có giá trị đã chốt trong khoảng đã chọn."
            )}
          </p>
          <LineChart title={`${data.title} — ${formatDate(data.from)} đến ${formatDate(data.to)}`} unit={u} points={shaped.points} from={data.from} to={data.to} versionChanges={shaped.versionChanges} digits={digits} />
          <p className="small mute" style={{ marginTop: 6 }}>
            Điểm viền cam = ngày đã tính lại bằng bản ghi bù (không sửa giá trị cũ). Vạch đứt = đổi phiên bản định nghĩa chỉ số.
          </p>
          {history ? (
            shaped.superseded.length === 0 ? (
              <p className="small mute" role="status">
                Không có giá trị nào bị thay trong khoảng đã chọn.
              </p>
            ) : (
              <div className="tbl-wrap" style={{ marginTop: 8 }}>
                <table className="tbl" aria-label="Giá trị kỳ đã chốt bị thay thế">
                  <thead>
                    <tr>
                      <th scope="col">Ngày</th>
                      <th scope="col" className="num">Giá trị cũ</th>
                      <th scope="col" className="num">Giá trị hiện hành</th>
                      <th scope="col">Lý do tính lại</th>
                      <th scope="col">Tính lúc (bản cũ)</th>
                    </tr>
                  </thead>
                  <tbody>
                    {shaped.superseded.map((r) => (
                      <tr key={r.id}>
                        <td>{formatDate(r.date)}</td>
                        <td className="num">
                          <s>{formatDecimal(r.oldValue, digits)}</s>
                        </td>
                        <td className="num">{r.currentValue === null ? "—" : formatDecimal(r.currentValue, digits)}</td>
                        <td className="small">{r.reason ?? "—"}</td>
                        <td className="small">{formatDateTime(r.computedAt)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )
          ) : null}
        </>
      ) : null}
    </section>
  );
}
