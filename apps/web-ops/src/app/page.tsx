"use client";

import Link from "next/link";
import { unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { LEVEL_LABEL } from "@/auth/principal";
import { BarChart } from "@/components/BarChart";
import { ProblemAlert } from "@/components/ProblemAlert";
import { ProvisionalBadge } from "@/components/ProvisionalBadge";
import { KpiTile, Loading, PageHead } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

export default function DashboardPage() {
  const { api, principal } = useAuth();
  const { data, error, loading, reload } = useAsync(() => unwrap(api.GET("/v1/dashboard/kpis")), [api]);

  const scopeTitle =
    principal?.level === "tw" || !principal?.level
      ? "Bảng điều hành toàn quốc"
      : `Bảng điều hành ${LEVEL_LABEL[principal.level].toLowerCase()} ${principal.unitName ?? ""}`.trim();

  return (
    <>
      <PageHead
        title={scopeTitle}
        subtitle={
          data ? (
            <>
              Vụ {data.season} · Cập nhật {formatDateTime(data.as_of)}
            </>
          ) : (
            "Đề án 1 triệu ha lúa chất lượng cao, phát thải thấp"
          )
        }
        actions={data && !data.complete ? <ProvisionalBadge missing={data.missing_units} total={data.total_units} /> : null}
      />
      {error ? <ProblemAlert problem={error} onRetry={reload} /> : null}
      {loading && !data ? <Loading /> : null}
      {data ? (
        <div className="stack">
          <section className="grid4" aria-label="Chỉ số chính">
            <KpiTile
              color="green"
              icon="area"
              label="Diện tích đã triển khai"
              value={data.kpis.deployed_area_ha === null ? null : formatDecimal(data.kpis.deployed_area_ha, 0)}
              unit="ha"
              footer={`${data.total_units - data.missing_units}/${data.total_units} đơn vị đã báo cáo`}
            />
            <KpiTile
              color="blue"
              icon="coop"
              label="Số HTX tham gia"
              value={data.kpis.cooperatives === null ? null : formatInt(data.kpis.cooperatives)}
              footer={<Link href="/htx" style={{ color: "#fff" }}>Xem danh sách HTX</Link>}
            />
            <KpiTile
              color="violet"
              icon="plot"
              label="Thửa đã số hoá ranh giới"
              value={data.kpis.digitized_plots === null ? null : formatInt(data.kpis.digitized_plots)}
              footer={<Link href="/ban-do" style={{ color: "#fff" }}>Mở bản đồ</Link>}
            />
            <KpiTile
              color={data.kpis.overdue_tasks ? "red" : "orange"}
              icon="clock"
              label="Nhiệm vụ quá hạn"
              value={data.kpis.overdue_tasks === null ? null : formatInt(data.kpis.overdue_tasks)}
              footer={<Link href="/nhiem-vu" style={{ color: "#fff" }}>Xử lý nhiệm vụ</Link>}
            />
          </section>

          <section className="grid2" style={{ alignItems: "start" }}>
            <div className="card sec">
              <div className="sec-h">
                <h2>Tiến độ diện tích theo {data.scope.level === "tw" ? "tỉnh" : "xã"}</h2>
                <span className="mute small">Vạch nâu: mục tiêu</span>
              </div>
              <BarChart
                title="Diện tích đã triển khai so với mục tiêu"
                unit="ha"
                data={data.breakdown.map((b) => ({
                  key: b.unit_code,
                  label: b.unit_name,
                  value: Number(b.deployed_area_ha),
                  target: Number(b.target_area_ha),
                  muted: !b.reported,
                  note: b.reported ? undefined : "Chưa báo cáo",
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
                      <th scope="col" className="num">Đã triển khai (ha)</th>
                      <th scope="col" className="num">Đạt</th>
                      <th scope="col">Báo cáo</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.breakdown.map((b) => {
                      const ratio = Number(b.target_area_ha) > 0 ? (Number(b.deployed_area_ha) / Number(b.target_area_ha)) * 100 : 0;
                      return (
                        <tr key={b.unit_code}>
                          <td>{b.unit_name}</td>
                          <td className="num">{b.reported ? formatDecimal(b.deployed_area_ha, 0) : "—"}</td>
                          <td className="num">{b.reported ? `${formatDecimal(ratio, 1)}%` : "—"}</td>
                          <td>
                            {b.reported ? <span className="chip c-ok">Đã gửi</span> : <span className="chip c-warn">Chưa gửi</span>}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </div>
          </section>
        </div>
      ) : null}
    </>
  );
}
