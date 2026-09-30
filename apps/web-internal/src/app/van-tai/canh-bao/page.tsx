"use client";

import { unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { Flags, When } from "@/components/g4kit";
import { Chip, Section } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, KpiTile, Loading, PageHead } from "@/components/ui";
import { formatDate } from "@/lib/format";
import { MODE_LABEL, REGISTRATION_LABEL, TRIP_STATUS_LABEL, VESSEL_KIND_LABEL } from "@/lib/g4labels";
import { formatTons } from "@/lib/kg";
import { useAsync } from "@/lib/useAsync";

export default function LogisticsAlertsPage() {
  const { api } = useAuth();
  const a = useAsync(() => unwrap(api.GET("/v1/logistics/alerts")), [api]);
  const d = a.data;
  return (
    <div className="stack">
      <PageHead title="Cảnh báo vận tải" subtitle="Ghe xuống hàng quá N giờ chưa cân (tham số #48 giờ) và phương tiện hết / sắp hết hạn đăng kiểm (GH-02). Chỉ cảnh báo vận hành — không chặn." />
      {a.loading ? <Loading /> : null}
      {a.error ? <ProblemAlert problem={a.error} onRetry={a.reload} /> : null}
      {d ? (
        <>
          <div className="grid4">
            <KpiTile label={`Chưa cân > ${d.unweighed_hours} giờ`} value={String(d.unweighed_trips.length)} color={d.unweighed_trips.length ? "red" : "green"} icon="clock" footer={<span className="mono small">{d.unweighed_param_version}</span>} />
            <KpiTile label="Đăng kiểm hết hạn" value={String(d.vessels.filter((v) => v.registration_status === "expired").length)} color="red" icon="alert" />
            <KpiTile label="Đăng kiểm sắp hết hạn" value={String(d.vessels.filter((v) => v.registration_status === "expiring").length)} color="orange" icon="bell" />
          </div>
          <Section title={`Chuyến xuống ghe quá ${d.unweighed_hours} giờ chưa cân`}>
            {d.unweighed_trips.length ? (
              <div className="tbl-wrap">
                <table className="tbl" aria-label="Chuyến chưa cân">
                  <thead>
                    <tr>
                      <th>Mã</th>
                      <th>Phương thức</th>
                      <th>Trạng thái</th>
                      <th>Lập lúc</th>
                      <th className="num">Tấn ước</th>
                      <th>Cờ</th>
                    </tr>
                  </thead>
                  <tbody>
                    {d.unweighed_trips.map((t) => (
                      <tr key={t.trip_id}>
                        <td className="mono">{t.code}</td>
                        <td>{MODE_LABEL[t.mode]}</td>
                        <td>
                          <Chip status={t.status} label={TRIP_STATUS_LABEL[t.status]} />
                        </td>
                        <td>
                          <When at={t.planned_at} />
                        </td>
                        <td className="num">{formatTons(t.planned_kg)}</td>
                        <td>
                          <Flags flags={t.flags} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <Empty>Không có ghe nào chờ cân quá hạn.</Empty>
            )}
          </Section>
          <Section title="Phương tiện cần gia hạn đăng kiểm">
            {d.vessels.length ? (
              <div className="tbl-wrap">
                <table className="tbl" aria-label="Phương tiện cần gia hạn">
                  <thead>
                    <tr>
                      <th>Số hiệu</th>
                      <th>Loại</th>
                      <th>Hạn đăng kiểm</th>
                      <th>Tình trạng</th>
                    </tr>
                  </thead>
                  <tbody>
                    {d.vessels.map((v) => (
                      <tr key={v.vessel_id}>
                        <td className="mono">{v.code}</td>
                        <td>{VESSEL_KIND_LABEL[v.kind]}</td>
                        <td>
                          {formatDate(v.registration_expiry)} ({v.registration_days_left} ngày)
                        </td>
                        <td>
                          <Chip status={v.registration_status} label={REGISTRATION_LABEL[v.registration_status]} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <Empty>Mọi phương tiện còn hạn đăng kiểm.</Empty>
            )}
          </Section>
        </>
      ) : null}
    </div>
  );
}
