"use client";

import Link from "next/link";
import { unwrap, unwrapAs } from "@/api/client";
import type { DisputePage } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { Chip } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { KpiTile, Loading, PageHead } from "@/components/ui";
import { formatTons } from "@/lib/kg";
import { useAsync } from "@/lib/useAsync";

export default function OverviewPage() {
  const { api, principal } = useAuth();
  const seeHub = hasRole(principal, ROLES.hubOperator, ROLES.hubApprover);
  const seeMeas = hasRole(principal, ROLES.disputeOfficer, ROLES.reconciliationOfficer);
  const seeField = hasRole(principal, ROLES.fieldSupervisor, ROLES.fieldCollector);
  const data = useAsync(async () => {
    const [hubs, disputes, planned] = await Promise.all([
      seeHub
        ? unwrap(api.GET("/v1/hub/facilities")).then((r) =>
            Promise.all((r.items ?? []).map((f) => unwrap(api.GET("/v1/hub/facilities/{id}/inventory", { params: { path: { id: f.id! } } })))),
          )
        : Promise.resolve([]),
      seeMeas ? unwrapAs<DisputePage>(api.GET("/v1/measurement/disputes", { params: { query: { status: "open" } } })) : Promise.resolve(null),
      seeField ? unwrap(api.GET("/v1/fieldops/orders", { params: { query: { status: "planned" } } })) : Promise.resolve(null),
    ]);
    return { hubs, disputes, planned };
  }, [api, seeHub, seeMeas, seeField]);

  const used = data.data?.hubs.reduce((s, h) => s + Number(h.capacity?.used_kg ?? 0), 0) ?? 0;
  const cap = data.data?.hubs.reduce((s, h) => s + Number(h.capacity?.capacity_kg ?? 0), 0) ?? 0;
  return (
    <div className="stack">
      <PageHead title="Tổng quan vận hành" subtitle="Số liệu nội bộ vùng MKG — không bao giờ qua van công bố (khối lượng chính xác, tồn theo lô, tranh chấp)." />
      {data.error ? <ProblemAlert problem={data.error} onRetry={data.reload} /> : null}
      {data.loading ? <Loading /> : null}
      {data.data ? (
        <div className="grid4">
          {seeHub ? <KpiTile label="Tồn theo lô toàn hệ thống" value={formatTons(used.toFixed(3)).replace(" t", "")} unit="t" color="green" icon="warehouse" footer={`Sức chứa ${formatTons(cap.toFixed(3))}`} /> : null}
          {seeHub ? <KpiTile label="Cơ sở đang hoạt động" value={String(data.data.hubs.length)} color="blue" icon="layers" /> : null}
          {seeMeas ? <KpiTile label="Tranh chấp đang mở" value={String(data.data.disputes?.items.length ?? 0)} color="red" icon="conflict" /> : null}
          {seeField ? <KpiTile label="Lệnh gom chờ duyệt" value={String(data.data.planned?.items?.length ?? 0)} color="orange" icon="tractor" /> : null}
        </div>
      ) : null}
      {data.data && seeHub ? (
        <section className="card sec" aria-label="Cơ sở">
          <div className="sec-h">
            <h2>Cơ sở và lô rủi ro cao nhất</h2>
            <Link className="btn btn-o btn-sm" href="/hub/ton-kho">
              Xem tồn kho
            </Link>
          </div>
          <div className="tbl-wrap">
            <table className="tbl">
              <thead>
                <tr>
                  <th>Cơ sở</th>
                  <th className="num">Đang dùng</th>
                  <th className="num">Còn trống</th>
                  <th>Lô rủi ro cao nhất</th>
                  <th>Quy tắc</th>
                </tr>
              </thead>
              <tbody>
                {data.data.hubs.map((h) => (
                  <tr key={h.facility?.id}>
                    <td>
                      {h.facility?.code} · {h.facility?.name}
                    </td>
                    <td className="num">{formatTons(h.capacity?.used_kg)}</td>
                    <td className="num">{formatTons(h.capacity?.available_kg)}</td>
                    <td>{h.lots?.[0] ? `${h.lots[0].code} (điểm ${h.lots[0].risk_score})` : "—"}</td>
                    <td className="mono">{h.risk_rule}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      ) : null}
      {data.data?.disputes?.items.length ? (
        <section className="card sec" aria-label="Tranh chấp đang mở">
          <div className="sec-h">
            <h2>Tranh chấp đang mở</h2>
            <Link className="btn btn-o btn-sm" href="/can/tranh-chap">
              Xử lý
            </Link>
          </div>
          <ul className="list-plain">
            {data.data.disputes.items.map((d) => (
              <li key={d.id} className="row">
                <Chip status={d.severity} /> Lệch {d.diff_pct} % ({d.diff_kg} kg)
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </div>
  );
}
