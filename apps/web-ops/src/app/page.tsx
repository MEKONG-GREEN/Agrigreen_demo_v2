"use client";

import Link from "next/link";
import { loadDashboard } from "@/api/adapters/dashboard";
import { useAuth } from "@/auth/AuthProvider";
import { BarChart } from "@/components/BarChart";
import { ProblemAlert } from "@/components/ProblemAlert";
import { ProvisionalBadge } from "@/components/ProvisionalBadge";
import { KpiTile, Loading, PageHead } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

const LEVEL_TITLE = { national: "toàn quốc", province: "tỉnh", commune: "xã" } as const;
const CHILD_LABEL = { national: "tỉnh", province: "xã", commune: "xã" } as const;

/** Tỷ lệ 0..1 dạng chuỗi → "%" để hiển thị (không dùng để tính). */
function pct(ratio: string | null): string | null {
  if (ratio === null) return null;
  const n = Number(ratio);
  return Number.isFinite(n) ? formatDecimal(n * 100, 1) : null;
}

/** Tổng quan 4 KPI theo phạm vi — GET /v1/extension/dashboard (US-DASH-01). */
export default function DashboardPage() {
  const { api } = useAuth();
  const { data, error, loading, reload } = useAsync(() => loadDashboard(api), [api]);
  const raw = data?.raw;
  const total = raw?.total;

  const title = raw
    ? raw.level === "national"
      ? "Bảng điều hành toàn quốc"
      : `Bảng điều hành ${LEVEL_TITLE[raw.level]} ${data?.unitName ?? raw.unit ?? ""}`.trim()
    : "Bảng điều hành";

  return (
    <>
      <PageHead
        title={title}
        subtitle={
          raw ? (
            <>
              Kỳ báo cáo {raw.period} · Cập nhật {formatDateTime(raw.as_of)}
              {raw.period_closed ? " · Kỳ đã chốt" : ""}
            </>
          ) : (
            "Đề án 1 triệu ha lúa chất lượng cao, phát thải thấp"
          )
        }
        actions={
          total?.provisional ? (
            <ProvisionalBadge missing={total.missing_communes.length} total={total.expected_communes} />
          ) : null
        }
      />
      {error ? <ProblemAlert problem={error} onRetry={reload} /> : null}
      {loading && !data ? <Loading /> : null}
      {raw && total && data ? (
        <div className="stack">
          <section className="grid4" aria-label="Chỉ số chính">
            <KpiTile
              color="green"
              icon="area"
              label="Diện tích đã gieo sạ"
              value={formatDecimal(total.sown_area_ha, 0)}
              unit="ha"
              footer={`${formatInt(total.reported_communes)}/${formatInt(total.expected_communes)} xã đã báo cáo`}
            />
            <KpiTile
              color="blue"
              icon="coop"
              label="HTX có báo cáo"
              value={formatInt(total.cooperatives_reporting)}
              footer={<Link href="/htx" style={{ color: "#fff" }}>Xem danh sách HTX</Link>}
            />
            <KpiTile
              color={total.overdue_tasks ? "red" : "orange"}
              icon="clock"
              label="Nhiệm vụ quá hạn"
              value={formatInt(total.overdue_tasks)}
              footer={<Link href="/nhiem-vu" style={{ color: "#fff" }}>{`${formatInt(total.open_tasks)} nhiệm vụ đang mở`}</Link>}
            />
            <KpiTile
              color="violet"
              icon="task"
              label="Tỷ lệ xác nhận chỉ đạo"
              value={pct(total.directive_ack_rate)}
              unit="%"
              missingHint="Chưa ban hành chỉ đạo nào trong kỳ"
              footer={`${formatInt(total.directive_acks)}/${formatInt(total.directive_targets)} lượt đã xác nhận`}
            />
          </section>

          {data.children.length > 0 ? (
            <section className="grid2" style={{ alignItems: "start" }}>
              <div className="card sec">
                <div className="sec-h">
                  <h2>Diện tích gieo sạ theo {CHILD_LABEL[raw.level]}</h2>
                  <span className="mute small">Đơn vị: ha</span>
                </div>
                <BarChart
                  title={`Diện tích đã gieo sạ theo ${CHILD_LABEL[raw.level]}`}
                  unit="ha"
                  data={data.children.map((c) => ({
                    key: c.unit,
                    label: c.name,
                    // Chỉ để vẽ độ dài thanh; số hiển thị đi qua formatDecimal từ chuỗi gốc.
                    value: Number(c.kpi.sown_area_ha),
                    muted: c.kpi.reported_communes === 0,
                    note: c.kpi.reported_communes === 0 ? "Chưa báo cáo" : undefined,
                  }))}
                />
              </div>
              <div className="card sec">
                <div className="sec-h">
                  <h2>Tình trạng báo cáo</h2>
                </div>
                <div className="tbl-wrap">
                  <table className="tbl">
                    <thead>
                      <tr>
                        <th scope="col">Đơn vị</th>
                        <th scope="col" className="num">Gieo sạ (ha)</th>
                        <th scope="col" className="num">Xã đã báo cáo</th>
                        <th scope="col" className="num">Quá hạn</th>
                        <th scope="col">Trạng thái</th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.children.map((c) => (
                        <tr key={c.unit}>
                          <td>
                            {c.name}
                            {raw.closed_provinces.includes(c.unit) ? <div className="mute small">Kỳ đã chốt</div> : null}
                          </td>
                          <td className="num">{formatDecimal(c.kpi.sown_area_ha, 0)}</td>
                          <td className="num">
                            {formatInt(c.kpi.reported_communes)}/{formatInt(c.kpi.expected_communes)}
                          </td>
                          <td className="num">{formatInt(c.kpi.overdue_tasks)}</td>
                          <td>
                            {c.kpi.provisional ? (
                              <span className="chip c-warn">{c.kpi.provisional_label ?? "Số liệu tạm tính"}</span>
                            ) : (
                              <span className="chip c-ok">Đủ báo cáo</span>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            </section>
          ) : null}
          <p className="mute small">
            Chưa có trên backend: số thửa đã số hoá và mục tiêu diện tích theo đơn vị trong cùng bảng điều hành (xem
            docs/dev/web-backend-gaps.md, G-02).
          </p>
        </div>
      ) : null}
    </>
  );
}
