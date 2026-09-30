"use client";

import { useState } from "react";
import { idem, unwrapAs } from "@/api/client";
import type { LotYield, Reconciliation, ReconciliationHistory } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { ActionStatus, Chip, RefPicker, Section, SelectField, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { ReconView } from "@/components/ReconView";
import { Empty, Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { formatKg } from "@/lib/kg";
import { validReason } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export default function ReconciliationPage() {
  const { api, principal } = useAuth();
  const [lot, setLot] = useState("");
  const hist = useAsync(async () => (UUID.test(lot) ? unwrapAs<ReconciliationHistory>(api.GET("/v1/measurement/lots/{id}/reconciliations", { params: { path: { id: lot } } })) : null), [api, lot]);
  const yieldD = useAsync(async () => (UUID.test(lot) ? unwrapAs<LotYield>(api.GET("/v1/measurement/lots/{id}/yield", { params: { path: { id: lot } } })).catch(() => null) : null), [api, lot]);
  const [basis, setBasis] = useState<"per_tonne" | "per_bale">("per_tonne");
  const [reason, setReason] = useState("");
  const act = useAction();
  const canWrite = hasRole(principal, ROLES.reconciliationOfficer);
  const latest = hist.data?.latest;

  const reconcile = async () => {
    const r = await act.run(
      () => unwrapAs<Reconciliation>(api.POST("/v1/measurement/lots/{id}/reconciliations", { params: { path: { id: lot }, header: idem() } })),
      (x) => (x.id === latest?.id ? "Đầu vào không đổi — giữ kết quả đối soát trước" : "Đã đối soát lại lô (phát sự kiện nội bộ mkg.measurement.reconciliation.completed)"),
    );
    if (r) hist.reload();
  };
  const setTerms = async () => {
    const r = await act.run(
      () => unwrapAs<unknown>(api.POST("/v1/measurement/lots/{id}/terms", { params: { path: { id: lot }, header: idem() }, body: { pricing_basis: basis, ...(reason.trim() ? { reason: reason.trim() } : {}) } })),
      `Đã đặt cách tính giá ${basis === "per_bale" ? "theo cuộn (kiểm cuộn chỉ tham khảo)" : "theo tấn (kiểm chéo cuộn ↔ khối lượng RB-6)"} — đối soát lại để áp dụng`,
    );
    if (r !== undefined) setReason("");
  };
  const mrv = () =>
    act.run(() => unwrapAs<unknown>(api.POST("/v1/measurement/lots/{id}/mrv-evidence", { params: { path: { id: lot }, header: idem() }, body: {} })), "Đã công bố bằng chứng thu gom qua van mrv_collection_evidence (khối lượng theo bậc)");

  return (
    <div className="stack">
      <PageHead
        title="Đối soát lô"
        subtitle="Nội bộ: hao hụt dự kiến theo định mức sấy vs bất thường. Thương mại: CUSTOMER_IN − HUB_OUT so dung sai. Theo tấn: kiểm chéo số cuộn ↔ khối lượng (RB-6); theo cuộn: chỉ tham khảo."
      />
      <Section title="Chọn lô">
        <RefPicker label="Mã lô" kind="lot" value={lot} onChange={setLot} />
      </Section>
      {hist.loading && lot ? <Loading /> : null}
      {hist.error ? <ProblemAlert problem={hist.error} /> : null}
      {hist.data ? (
        <Section
          title="Đối soát hiện hành"
          actions={
            canWrite ? (
              <>
                <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void reconcile()}>
                  Đối soát lại
                </button>
                <button type="button" className="btn btn-o btn-sm" disabled={act.busy} onClick={() => void mrv()}>
                  Công bố bằng chứng MRV
                </button>
              </>
            ) : null
          }
        >
          <div className="stack">
            <ActionStatus a={act} />
            {latest ? <ReconView r={latest} /> : <Empty>Lô chưa được đối soát (chưa đủ phiếu cân).</Empty>}
            {yieldD.data ? (
              <p className="small">
                Sản lượng quy đổi: <b>{formatKg(yieldD.data.yield_kg_per_roll, 2)}/cuộn</b> = {formatKg(yieldD.data.basis_kg)} ({yieldD.data.basis_point}) / {yieldD.data.rolls ?? "—"} cuộn
                {yieldD.data.aggregated ? " — giao thẳng, gộp" : ""}. Nội bộ MKG, không bao giờ qua van.
              </p>
            ) : null}
          </div>
        </Section>
      ) : null}
      {hist.data && canWrite ? (
        <Section title="Cách tính giá của lô (tạm tới G4 orders)">
          <div className="grid3">
            <SelectField label="Cách tính giá" value={basis} onChange={(v) => setBasis(v as typeof basis)} options={[{ value: "per_tonne", label: "Theo tấn nhà máy cân" }, { value: "per_bale", label: "Theo cuộn" }]} />
            <TextField label="Lý do đổi" value={reason} onChange={setReason} hint="Bắt buộc khi đổi cách tính" />
          </div>
          <div className="row" style={{ marginTop: 10 }}>
            <button type="button" className="btn btn-o btn-sm" disabled={act.busy || (!!reason && !validReason(reason))} onClick={() => void setTerms()}>
              Đặt cách tính giá
            </button>
          </div>
        </Section>
      ) : null}
      {hist.data?.items.length ? (
        <Section title="Lịch sử đối soát">
          <div className="tbl-wrap">
            <table className="tbl" aria-label="Lịch sử đối soát">
              <thead>
                <tr>
                  <th>Thời điểm</th>
                  <th>Kết quả</th>
                  <th>Nội bộ</th>
                  <th>Thương mại</th>
                  <th>Cuộn</th>
                  <th>Dung sai</th>
                </tr>
              </thead>
              <tbody>
                {hist.data.items.map((r) => (
                  <tr key={r.id}>
                    <td>{formatDateTime(r.event_time)}</td>
                    <td>
                      <Chip status={r.outcome} />
                    </td>
                    <td>
                      <Chip status={r.internal?.status} />
                    </td>
                    <td>
                      <Chip status={r.commercial?.status} /> {r.commercial?.diff_pct ?? ""}
                    </td>
                    <td>
                      <Chip status={r.roll_check?.status} /> {r.roll_check?.variance_pct ?? ""}
                    </td>
                    <td className="mono">{r.tolerance_version}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      ) : null}
    </div>
  );
}
