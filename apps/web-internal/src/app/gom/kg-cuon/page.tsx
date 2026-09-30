"use client";

import { useState } from "react";
import { idem, unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { ActionStatus, Ref, RefPicker, Section, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { formatDecimal } from "@/lib/decimal";
import { useAsync } from "@/lib/useAsync";

const SOURCE_LABEL: Record<string, string> = {
  htx: "Học theo HTX (30 lô gần nhất)",
  system: "Toàn tenant",
  config: "Cấu hình AG_FIELDOPS_KG_PER_ROLL",
  default: "Mặc định 20 (tạm, chờ khách)",
};

export default function KgPerRollPage() {
  const { api } = useAuth();
  const [extra, setExtra] = useState("");
  const [tick, setTick] = useState(0);
  const data = useAsync(async () => {
    const orders = (await unwrap(api.GET("/v1/fieldops/orders", { params: { query: { limit: 200 } } }))).items ?? [];
    const htx = Array.from(new Set([...orders.map((o) => o.htx_ref!), ...(/^[0-9a-f-]{36}$/i.test(extra) ? [extra] : [])]));
    return Promise.all(htx.map((h) => unwrap(api.GET("/v1/fieldops/htx/{id}/kg-per-roll", { params: { path: { id: h } } }))));
  }, [api, extra, tick]);
  const act = useAction();
  const refresh = async () => {
    const r = await act.run(() => unwrap(api.POST("/v1/fieldops/kg-per-roll/refresh", { params: { header: idem() } })), (x) => `Đã kéo khối lượng cân từ mkg/measurement — ${x.weighed_lots_added ?? 0} lô mới có khối lượng`);
    if (r) setTick((t) => t + 1);
  };
  return (
    <div className="stack">
      <PageHead
        title="kg/cuộn theo HTX"
        subtitle="Σ kg cân / Σ cuộn của 30 lô gom đã cân gần nhất của HTX → toàn tenant → cấu hình → 20 (tạm, chờ khách). Dùng ước lượng hàng về và kiểm chéo RB-6 — nội bộ MKG, KHÔNG BAO GIỜ qua van."
        actions={
          <button type="button" className="btn btn-o btn-sm" disabled={act.busy} onClick={() => void refresh()}>
            Kéo khối lượng cân mới
          </button>
        }
      />
      <ActionStatus a={act} />
      <Section title="Theo HTX">
        <div className="stack">
          <RefPicker label="Thêm HTX khác" kind="htx" value={extra} onChange={setExtra} />
          {data.loading ? <Loading /> : null}
          {data.error ? <ProblemAlert problem={data.error} onRetry={data.reload} /> : null}
          {data.data ? (
            <div className="tbl-wrap">
              <table className="tbl" aria-label="kg mỗi cuộn theo HTX">
                <thead>
                  <tr>
                    <th>HTX</th>
                    <th className="num">kg/cuộn</th>
                    <th>Nguồn số</th>
                    <th className="num">Số mẫu</th>
                    <th>Quy tắc</th>
                  </tr>
                </thead>
                <tbody>
                  {data.data.map((k) => (
                    <tr key={k.htx_ref}>
                      <td>
                        <Ref id={k.htx_ref} />
                      </td>
                      <td className="num">
                        <b>{formatDecimal(k.kg_per_roll, 1)}</b>
                      </td>
                      <td>{SOURCE_LABEL[k.source ?? ""] ?? k.source}</td>
                      <td className="num">{k.samples}</td>
                      <td className="mono">{k.rule_version}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </div>
      </Section>
    </div>
  );
}
