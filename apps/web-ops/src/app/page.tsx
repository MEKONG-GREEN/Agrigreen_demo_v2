"use client";

import Link from "next/link";
import { loadDashboard, progressPct } from "@/api/adapters/dashboard";
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

/** Bảng điều hành gộp — GET /v1/dashboard (G-02, US-DASH-01). */
export default function DashboardPage() {
  const { api, principal } = useAuth();
  const { data, error, loading, reload } = useAsync(() => loadDashboard(api), [api]);
  const total = data?.total;
  const unitName = data?.unit ? (principal?.unitName ?? data.unit) : null;

  const title = data
    ? data.level === "national"
      ? "Bảng điều hành toàn quốc"
      : `Bảng điều hành ${LEVEL_TITLE[data.level]} ${unitName ?? ""}`.trim()
    : "Bảng điều hành";
  const pct = total ? progressPct(total) : null;

  return (
    <>
      <PageHead
        title={title}
        subtitle={
          data ? (
            <>
              Kỳ báo cáo {data.period} · Cập nhật {formatDateTime(data.as_of)}
            </>
          ) : (
            "Đề án 1 triệu ha lúa chất lượng cao, phát thải thấp"
          )
        }
        actions={total?.provisional ? <ProvisionalBadge missing={total.missing_communes.length} total={total.expected_communes} /> : null}
      />
      {error ? <ProblemAlert problem={error} onRetry={reload} /> : null}
      {loading && !data ? <Loading /> : null}
      {data && total ? (
        <div className="stack">
          <section className="grid4" aria-label="Chỉ số chính">
            <KpiTile
              color="green"
              icon="area"
              label="Diện tích đã gieo sạ"
              value={formatDecimal(total.sown_area_ha, 0)}
              unit="ha"
              footer={
                total.target_area_ha === null
                  ? `${formatInt(total.reported_communes)}/${formatInt(total.expected_communes)} xã đã báo cáo · chưa đặt mục tiêu`
                  : `${pct === null ? "—" : formatDecimal(pct, 1)}% mục tiêu ${formatDecimal(total.target_area_ha, 0)} ha`
              }
            />
            <KpiTile
              color="blue"
              icon="coop"
              label="HTX đang hoạt động"
              value={formatInt(total.cooperatives)}
              footer={
                <Link href="/htx" style={{ color: "#fff" }}>
                  {`${formatInt(total.households)} hộ thành viên · ${formatInt(total.cooperatives_reporting)} HTX có báo cáo`}
                </Link>
              }
            />
            <KpiTile
              color="violet"
              icon="plot"
              label="Thửa đã số hoá"
              value={formatInt(total.digitized_plots)}
              footer={`${formatDecimal(total.digitized_area_ha, 0)} ha đã có ranh giới`}
            />
            <KpiTile
              color={total.overdue_tasks ? "red" : "orange"}
              icon="clock"
              label="Nhiệm vụ quá hạn"
              value={formatInt(total.overdue_tasks)}
              footer={
                <Link href="/nhiem-vu" style={{ color: "#fff" }}>
                  {`${formatInt(total.open_tasks)} nhiệm vụ đang mở`}
                </Link>
              }
            />
          </section>

          {data.children.length > 0 ? (
            <section className="grid2" style={{ alignItems: "start" }}>
              <div className="card sec">
                <div className="sec-h">
                  <h2>Diện tích gieo sạ theo {CHILD_LABEL[data.level]}</h2>
                  <span className="mute small">Đơn vị: ha</span>
                </div>
                <BarChart
                  title={`Diện tích đã gieo sạ theo ${CHILD_LABEL[data.level]}`}
                  unit="ha"
                  data={data.children.map((c) => ({
                    key: c.unit,
                    label: c.name ?? c.unit,
                    // Chỉ để vẽ độ dài thanh; số hiển thị đi qua formatDecimal từ chuỗi gốc.
                    value: Number(c.sown_area_ha),
                    muted: c.reported_communes === 0,
                    note: c.reported_communes === 0 ? "Chưa báo cáo" : undefined,
                  }))}
                />
              </div>
              <div className="card sec">
                <div className="sec-h">
                  <h2>Tiến độ so với mục tiêu</h2>
                </div>
                <div className="tbl-wrap">
                  <table className="tbl">
                    <thead>
                      <tr>
                        <th scope="col">Đơn vị</th>
                        <th scope="col" className="num">Gieo sạ / mục tiêu (ha)</th>
                        <th scope="col">Tiến độ</th>
                        <th scope="col" className="num">Thửa số hoá</th>
                        <th scope="col">Trạng thái</th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.children.map((c) => {
                        const p = progressPct(c);
                        return (
                          <tr key={c.unit}>
                            <td>{c.name ?? c.unit}</td>
                            <td className="num">
                              {formatDecimal(c.sown_area_ha, 0)} / {c.target_area_ha === null ? "—" : formatDecimal(c.target_area_ha, 0)}
                            </td>
                            <td>
                              {p === null ? (
                                <span className="mute small">Chưa đặt mục tiêu</span>
                              ) : (
                                <div className="row" style={{ gap: 6, flexWrap: "nowrap" }}>
                                  <div className="bar" role="img" aria-label={`${formatDecimal(p, 1)}% mục tiêu`}>
                                    <span style={{ width: `${Math.min(100, p)}%` }} />
                                  </div>
                                  <span className="small">{formatDecimal(p, 1)}%</span>
                                </div>
                              )}
                            </td>
                            <td className="num">{formatInt(c.digitized_plots)}</td>
                            <td>
                              {c.provisional ? (
                                <span className="chip c-warn">{c.provisional_label ?? "Số liệu tạm tính"}</span>
                              ) : (
                                <span className="chip c-ok">Đủ báo cáo</span>
                              )}
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
              </div>
            </section>
          ) : null}
          <p className="mute small">
            Nguồn số liệu: {data.sources.join(", ")}. Tổng = tổng các đơn vị con; mục tiêu diện tích là tham số đã phê duyệt của đơn vị.
          </p>
        </div>
      ) : null}
    </>
  );
}
