"use client";

import { useState } from "react";
import { unwrap } from "@/api/client";
import type { BuySideReconciliation } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Chip, Ref, Section, SelectField } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, KpiTile, Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { shortRef } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

export default function BuySidePage() {
  const { api } = useAuth();
  const orders = useAsync(async () => (await unwrap(api.GET("/v1/fieldops/orders", { params: { query: { limit: 200 } } }))).items ?? [], [api]);
  const [order, setOrder] = useState("");
  const [status, setStatus] = useState<"" | "pending" | "match" | "mismatch">("");
  const data = useAsync(
    () => unwrap(api.GET("/v1/fieldops/reconciliation", { params: { query: { ...(order ? { order_id: order } : {}), ...(status ? { status } : {}), limit: 500 } } })),
    [api, order, status],
  );
  const counts = (data.data?.page_counts ?? {}) as Record<string, number>;
  const items = (data.data?.items ?? []) as BuySideReconciliation[];
  return (
    <div className="stack">
      <PageHead title="Đối soát chiều mua" subtitle="MKG ↔ HTX theo thửa (RB-6): số cuộn MKG xếp ghe/xe so với số HTX xác nhận trên bảng kê htx_collection_statement." />
      <Section title="Bộ lọc">
        <div className="grid2">
          <SelectField label="Lệnh gom" value={order} onChange={setOrder} options={[{ value: "", label: "Tất cả lệnh" }, ...(orders.data ?? []).map((o) => ({ value: o.order_id ?? "", label: `${shortRef(o.order_id)} · HTX ${shortRef(o.htx_ref)}` }))]} />
          <SelectField
            label="Trạng thái đối soát"
            value={status}
            onChange={(v) => setStatus(v as typeof status)}
            options={[
              { value: "", label: "Tất cả" },
              { value: "pending", label: "Chờ HTX" },
              { value: "match", label: "Khớp" },
              { value: "mismatch", label: "Lệch" },
            ]}
          />
        </div>
      </Section>
      {data.loading ? <Loading /> : null}
      {data.error ? <ProblemAlert problem={data.error} onRetry={data.reload} /> : null}
      {data.data ? (
        <>
          <div className="grid3">
            <KpiTile label="Chờ HTX xác nhận" value={String(counts.pending ?? 0)} color="orange" icon="clock" />
            <KpiTile label="Khớp" value={String(counts.match ?? 0)} color="green" icon="check" />
            <KpiTile label="Lệch" value={String(counts.mismatch ?? 0)} color="red" icon="conflict" />
          </div>
          <Section title="Theo thửa">
            {!items.length ? (
              <Empty>Không có dòng đối soát.</Empty>
            ) : (
              <div className="tbl-wrap">
                <table className="tbl" aria-label="Đối soát chiều mua theo thửa">
                  <thead>
                    <tr>
                      <th>Lệnh</th>
                      <th>Thửa</th>
                      <th>HTX</th>
                      <th>Bảng kê</th>
                      <th className="num">MKG</th>
                      <th className="num">HTX</th>
                      <th>Trạng thái</th>
                      <th>Cập nhật</th>
                    </tr>
                  </thead>
                  <tbody>
                    {items.map((r) => (
                      <tr key={`${r.order_id}-${r.plot_ref}`}>
                        <td>
                          <Ref id={r.order_id} />
                        </td>
                        <td>
                          <Ref id={r.plot_ref} />
                        </td>
                        <td>
                          <Ref id={r.htx_ref} />
                        </td>
                        <td>
                          v{r.statement_revision}
                          {r.confirmed_revision && r.confirmed_revision !== r.statement_revision ? ` (HTX xem v${r.confirmed_revision})` : ""}
                        </td>
                        <td className="num">{r.mkg_rolls?.toLocaleString("vi-VN")}</td>
                        <td className="num">{r.htx_rolls?.toLocaleString("vi-VN") ?? "—"}</td>
                        <td>
                          <Chip status={r.status} label={r.status === "pending" ? "Chờ HTX" : undefined} />
                        </td>
                        <td>{formatDateTime(r.updated_at)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Section>
        </>
      ) : null}
    </div>
  );
}
