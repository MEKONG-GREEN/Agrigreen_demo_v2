"use client";

import { useState } from "react";
import { idem, unwrapAs } from "@/api/client";
import type { LossBand, LossNormValues, MeasurementParam, ToleranceList, ToleranceValues } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { ActionStatus, Chip, Provisional, Ref, Section, SelfHint, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Loading, PageHead } from "@/components/ui";
import { formatDate } from "@/lib/format";
import { isPct } from "@/lib/kg";
import { DEFAULT_LOSS_NORM, DEFAULT_TOLERANCE } from "@/lib/reconcile";
import { validReason } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

type Kind = "tolerance" | "loss_norm";
const PATH = { tolerance: "/v1/measurement/tolerance-versions", loss_norm: "/v1/measurement/loss-norm-versions" } as const;
const TOL_LABEL: Record<keyof ToleranceValues, string> = {
  commercial_flag_pct: "Thương mại — cờ (%)",
  commercial_critical_pct: "Thương mại — nghiêm trọng (%)",
  roll_flag_pct: "Cuộn ↔ khối lượng — cờ (%)",
  roll_critical_pct: "Cuộn ↔ khối lượng — nghiêm trọng (%)",
};

function valuesText(kind: Kind, v: MeasurementParam["values"]): string {
  if (!v) return "—";
  if (kind === "tolerance") {
    const t = v as ToleranceValues;
    return `Thương mại ${t.commercial_flag_pct}/${t.commercial_critical_pct} % · cuộn ${t.roll_flag_pct}/${t.roll_critical_pct} %`;
  }
  const n = v as LossNormValues;
  return `${n.bands.map((b) => `[${b.moisture_from}–${b.moisture_to}) ${b.loss_pct} %`).join(" · ")} · không đo ${n.default_loss_pct} % · cảnh báo > ${n.abnormal_alert_pct} %`;
}

function Versions({ kind }: { kind: Kind }) {
  const { adminApi, principal } = useAuth();
  const list = useAsync(() => unwrapAs<ToleranceList>(adminApi.GET(PATH[kind])), [adminApi, kind]);
  const act = useAction();
  const [rej, setRej] = useState("");
  const [tol, setTol] = useState<ToleranceValues>({ ...DEFAULT_TOLERANCE });
  const [bands, setBands] = useState<LossBand[]>(DEFAULT_LOSS_NORM.bands.map((b) => ({ ...b })));
  const [defLoss, setDefLoss] = useState(DEFAULT_LOSS_NORM.default_loss_pct);
  const [alertPct, setAlertPct] = useState(DEFAULT_LOSS_NORM.abnormal_alert_pct);
  const [eff, setEff] = useState(new Date().toISOString().slice(0, 10));
  const [reason, setReason] = useState("");

  if (list.loading) return <Loading />;
  if (list.error) return <ProblemAlert problem={list.error} onRetry={list.reload} />;
  const items = list.data?.items ?? [];
  const pending = items.find((v) => v.status === "pending");
  const active = items.find((v) => v.status === "approved");
  const effIso = new Date(`${eff}T00:00:00+07:00`).toISOString();
  const valuesOk = kind === "tolerance" ? Object.values(tol).every(isPct) : bands.every((b) => isPct(b.moisture_from) && isPct(b.moisture_to) && isPct(b.loss_pct)) && isPct(defLoss) && isPct(alertPct);

  const propose = async () => {
    const r = await act.run(
      () =>
        kind === "tolerance"
          ? unwrapAs<MeasurementParam>(adminApi.POST("/v1/measurement/tolerance-versions", { params: { header: idem() }, body: { ...tol, effective_from: effIso, provisional: true, reason: reason.trim() } }))
          : unwrapAs<MeasurementParam>(
              adminApi.POST("/v1/measurement/loss-norm-versions", { params: { header: idem() }, body: { bands, default_loss_pct: defLoss, abnormal_alert_pct: alertPct, effective_from: effIso, provisional: true, reason: reason.trim() } }),
            ),
      (v) => `Đã đề xuất phiên bản ${v.version} — chờ tolerance_approver khác duyệt`,
    );
    if (r) {
      setReason("");
      list.reload();
    }
  };
  const decide = async (v: MeasurementParam, decision: "approved" | "rejected") => {
    const body = decision === "rejected" ? { decision, reason: rej.trim() } : { decision };
    const r = await act.run(
      () =>
        kind === "tolerance"
          ? unwrapAs<MeasurementParam>(adminApi.POST("/v1/measurement/tolerance-versions/{seq}/decision", { params: { path: { seq: v.version! }, header: idem() }, body }))
          : unwrapAs<MeasurementParam>(adminApi.POST("/v1/measurement/loss-norm-versions/{seq}/decision", { params: { path: { seq: v.version! }, header: idem() }, body })),
      decision === "approved" ? `Đã duyệt phiên bản ${v.version} — áp theo THỜI ĐIỂM CÂN ≥ ngày hiệu lực` : `Đã từ chối phiên bản ${v.version}`,
    );
    if (r) list.reload();
  };

  return (
    <div className="stack">
      <div className="row">
        {active ? (
          <span>
            Đang áp dụng: <b>phiên bản {active.version}</b> — {valuesText(kind, active.values)}
          </span>
        ) : (
          <span>Chưa có phiên bản duyệt — dùng mặc định tạm {list.data?.provisional_default?.label}</span>
        )}
        <Provisional />
      </div>
      <ActionStatus a={act} />
      <div className="tbl-wrap">
        <table className="tbl" aria-label={kind === "tolerance" ? "Phiên bản dung sai" : "Phiên bản định mức hao hụt"}>
          <thead>
            <tr>
              <th>Phiên bản</th>
              <th>Trạng thái</th>
              <th>Giá trị</th>
              <th>Hiệu lực</th>
              <th>Lý do</th>
              <th>Người đặt</th>
              <th>Thao tác</th>
            </tr>
          </thead>
          <tbody>
            {items.map((v) => (
              <tr key={v.id}>
                <td>v{v.version}</td>
                <td>
                  <Chip status={v.status} /> {v.provisional ? <span className="chip c-warn">tạm</span> : null}
                </td>
                <td className="small">{valuesText(kind, v.values)}</td>
                <td>{formatDate(v.effective_from)}</td>
                <td>{v.reason}</td>
                <td>
                  <Ref id={v.proposed_by} />
                </td>
                <td>
                  {v.status === "pending" && hasRole(principal, ROLES.toleranceApprover) ? (
                    <div className="stack" style={{ gap: 6 }}>
                      <SelfHint proposer={v.proposed_by} />
                      <div className="row">
                        <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void decide(v, "approved")}>
                          Duyệt phiên bản {v.version}
                        </button>
                        <button type="button" className="btn btn-d btn-sm" disabled={act.busy || !validReason(rej)} onClick={() => void decide(v, "rejected")}>
                          Từ chối
                        </button>
                      </div>
                    </div>
                  ) : null}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {pending && hasRole(principal, ROLES.toleranceApprover) ? <TextField label="Lý do từ chối" value={rej} onChange={setRej} hint="Bắt buộc khi từ chối" /> : null}
      {!pending && hasRole(principal, ROLES.reconciliationOfficer) ? (
        <div className="panel stack">
          <b>Đề xuất phiên bản mới</b>
          {kind === "tolerance" ? (
            <div className="grid2">
              {(Object.keys(TOL_LABEL) as (keyof ToleranceValues)[]).map((k) => (
                <TextField key={k} label={TOL_LABEL[k]} value={tol[k]} onChange={(v) => setTol((t) => ({ ...t, [k]: v }))} inputMode="decimal" />
              ))}
            </div>
          ) : (
            <>
              {bands.map((b, i) => (
                <div className="grid3" key={i}>
                  <TextField label={`Khoảng ${i + 1} — ẩm từ (%)`} value={b.moisture_from} onChange={(v) => setBands((bs) => bs.map((x, j) => (j === i ? { ...x, moisture_from: v } : x)))} />
                  <TextField label={`Khoảng ${i + 1} — đến dưới (%)`} value={b.moisture_to} onChange={(v) => setBands((bs) => bs.map((x, j) => (j === i ? { ...x, moisture_to: v } : x)))} />
                  <TextField label={`Khoảng ${i + 1} — hao hụt (%)`} value={b.loss_pct} onChange={(v) => setBands((bs) => bs.map((x, j) => (j === i ? { ...x, loss_pct: v } : x)))} />
                </div>
              ))}
              <div className="grid2">
                <TextField label="Hao hụt khi không đo ẩm (%)" value={defLoss} onChange={setDefLoss} />
                <TextField label="Cảnh báo khi bất thường > (% HUB_IN)" value={alertPct} onChange={setAlertPct} />
              </div>
            </>
          )}
          <div className="grid2">
            <TextField label="Hiệu lực từ ngày" type="date" value={eff} onChange={setEff} />
            <TextField label="Lý do đề xuất" value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" />
          </div>
          <div className="row">
            <button type="button" className="btn btn-p btn-sm" disabled={!valuesOk || !validReason(reason) || act.busy} onClick={() => void propose()}>
              Gửi đề xuất
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

export default function MeasurementParamsPage() {
  const [tab, setTab] = useState<Kind>("tolerance");
  return (
    <div className="stack">
      <PageHead
        title="Dung sai & định mức"
        subtitle="Bảng dung sai (C-4) và định mức hao hụt sấy theo độ ẩm là tham số có phiên bản: cán bộ đối soát đề xuất, tolerance_approver KHÁC người đặt duyệt. Áp theo thời điểm cân."
      />
      <Section title="Phiên bản">
        <Tabs tabs={[{ id: "tolerance", label: "Dung sai" }, { id: "loss_norm", label: "Định mức hao hụt sấy" }]} value={tab} onChange={(v) => setTab(v as Kind)} label="Loại tham số">
          <Versions key={tab} kind={tab} />
        </Tabs>
      </Section>
    </div>
  );
}
