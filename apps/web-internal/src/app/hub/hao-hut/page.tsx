"use client";

import { useState } from "react";
import { unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { Chip, RefPicker, Section, useFacilities } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { formatKg, formatTons } from "@/lib/kg";
import { useAsync } from "@/lib/useAsync";

interface LossBlock {
  reconciliation_id?: string;
  hub_in_kg?: string | null;
  hub_out_kg?: string | null;
  expected_loss_kg?: string | null;
  abnormal_loss_kg?: string | null;
  outcome?: string;
}

export default function LossesPage() {
  const { api } = useAuth();
  const facilities = useFacilities();
  const losses = useAsync(
    async () =>
      Promise.all((facilities.data ?? []).map(async (f) => ({ f, l: await unwrap(api.GET("/v1/hub/facilities/{id}/losses", { params: { path: { id: f.id! } } })) }))),
    [api, facilities.data],
  );
  const [lot, setLot] = useState("");
  const m = useAsync(async () => (/^[0-9a-f-]{36}$/i.test(lot) ? unwrap(api.GET("/v1/hub/lots/{id}/measurements", { params: { path: { id: lot } } })) : null), [api, lot]);
  const loss = (m.data?.loss ?? null) as LossBlock | null;
  return (
    <div className="stack">
      <PageHead title="Hao hụt" subtitle="Hao hụt đã quy kết theo đối soát hiện hành của mỗi lô: dự kiến theo định mức sấy (độ ẩm HUB_IN) và bất thường. Chỉ bất thường vượt ngưỡng mới cảnh báo." />
      <Section title="Theo cơ sở">
        {losses.loading ? <Loading /> : null}
        {losses.error ? <ProblemAlert problem={losses.error} onRetry={losses.reload} /> : null}
        {losses.data ? (
          <div className="tbl-wrap">
            <table className="tbl" aria-label="Hao hụt theo cơ sở">
              <thead>
                <tr>
                  <th>Cơ sở</th>
                  <th className="num">Lô đã đối soát</th>
                  <th className="num">Cảnh báo</th>
                  <th className="num">HUB_IN</th>
                  <th className="num">HUB_OUT</th>
                  <th className="num">Hao hụt dự kiến</th>
                  <th className="num">Hao hụt bất thường</th>
                </tr>
              </thead>
              <tbody>
                {losses.data.map(({ f, l }) => (
                  <tr key={f.id}>
                    <td>
                      {f.code} · {f.name}
                    </td>
                    <td className="num">{l.lots}</td>
                    <td className="num">{l.loss_alerts ? <Chip status="abnormal" label={String(l.loss_alerts)} /> : 0}</td>
                    <td className="num">{formatTons(l.hub_in_kg)}</td>
                    <td className="num">{formatTons(l.hub_out_kg)}</td>
                    <td className="num">{formatKg(l.expected_loss_kg)}</td>
                    <td className="num">{formatKg(l.abnormal_loss_kg)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </Section>
      <Section title="Đo lường của một lô">
        <div className="stack">
          <RefPicker label="Mã lô" kind="lot" value={lot} onChange={setLot} />
          {m.error ? <ProblemAlert problem={m.error} /> : null}
          {m.data ? (
            <dl className="kv">
              <dt>Tham số cân</dt>
              <dd className="mono">{m.data.weigh_param_version}</dd>
              <dt>Số cuộn dự kiến</dt>
              <dd>{Object.entries(m.data.expected_rolls ?? {}).map(([k, v]) => `${k}: ${v}`).join(", ") || "—"}</dd>
              <dt>Khối lượng dự kiến</dt>
              <dd>{formatKg(m.data.expected_kg)}</dd>
              <dt>HUB_IN → HUB_OUT</dt>
              <dd>{loss ? `${formatKg(loss.hub_in_kg, 3)} → ${formatKg(loss.hub_out_kg, 3)}` : "Chưa đối soát"}</dd>
              <dt>Hao hụt dự kiến / bất thường</dt>
              <dd>{loss ? `${formatKg(loss.expected_loss_kg, 3)} / ${formatKg(loss.abnormal_loss_kg, 3)}` : "—"}</dd>
              <dt>Kết quả</dt>
              <dd>{loss ? <Chip status={loss.outcome} /> : "—"}</dd>
            </dl>
          ) : null}
        </div>
      </Section>
    </div>
  );
}
