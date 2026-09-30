"use client";

import { useState } from "react";
import { idem, unwrapAs } from "@/api/client";
import type { HubParams, ParamVersion } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { ActionStatus, Chip, Provisional, Ref, Section, SelfHint, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Loading, PageHead } from "@/components/ui";
import { formatDate } from "@/lib/format";
import { RISK_KEYS, validateRisk, validReason, type RiskParamsV2 } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

type Set = "risk" | "weigh";

const LABEL: Record<string, string> = {
  age_horizon_days: "Thời hạn tuổi (ngày)",
  age_weight: "Trọng số tuổi",
  moisture_warn_pct: "Ngưỡng ẩm lô (%)",
  moisture_span_pct: "Khoảng ẩm bão hoà (%)",
  moisture_weight: "Trọng số ẩm",
  alert_saturation: "Số cảnh báo bão hoà",
  alert_weight: "Trọng số cảnh báo",
  humidity_warn_pct: "Độ ẩm kho cảnh báo (%)",
  humidity_crit_pct: "Độ ẩm kho nguy hiểm (%)",
  temp_warn_c: "Nhiệt độ cảnh báo (°C)",
  temp_crit_c: "Nhiệt độ nguy hiểm (°C)",
  crit_alert_weight: "Hệ số cảnh báo nguy hiểm",
  tolerance_pct: "Dung sai phiếu cân (%)",
  tolerance_min_kg: "Sàn dung sai (kg)",
  ticket_required: "Bắt buộc phiếu cân khi nhận (true/false)",
  kg_per_roll_default: "kg/cuộn mặc định",
};
const WEIGH_KEYS = ["tolerance_pct", "tolerance_min_kg", "ticket_required", "kg_per_roll_default"];

function ParamSet({ set }: { set: Set }) {
  const { adminApi, principal } = useAuth();
  const data = useAsync(() => unwrapAs<HubParams>(adminApi.GET("/v1/hub/params/{set}", { params: { path: { set } } })), [adminApi, set]);
  const [form, setForm] = useState<Record<string, string> | null>(null);
  const [reason, setReason] = useState("");
  const [rejectReason, setRejectReason] = useState("");
  const act = useAction();
  const keys = set === "risk" ? RISK_KEYS : WEIGH_KEYS;

  if (data.loading) return <Loading />;
  if (data.error) return <ProblemAlert problem={data.error} onRetry={data.reload} />;
  const active = data.data?.active ?? {};
  const items = (data.data?.items ?? []) as ParamVersion[];
  const pending = items.find((v) => v.state === "pending");
  const f = form ?? Object.fromEntries(keys.map((k) => [k, String(active[k] ?? "")]));

  const parsed = (): Record<string, unknown> =>
    Object.fromEntries(keys.map((k) => [k, k === "crit_alert_weight" ? Number(f[k]) : k === "ticket_required" ? f[k] === "true" : f[k]!.trim()]));
  const clientErr = set === "risk" ? validateRisk(parsed() as unknown as RiskParamsV2) : null;

  const propose = async () => {
    const r = await act.run(
      () => unwrapAs<ParamVersion>(adminApi.POST("/v1/hub/params/{set}/versions", { params: { path: { set }, header: idem() }, body: { params: parsed() as Record<string, never>, reason: reason.trim() } })),
      (v) => `Đã đề xuất phiên bản ${v.version} — chờ hub_approver khác duyệt`,
    );
    if (r) {
      setForm(null);
      setReason("");
      data.reload();
    }
  };
  const decide = async (v: ParamVersion, decision: "approved" | "rejected") => {
    const r = await act.run(
      () =>
        unwrapAs<ParamVersion>(
          adminApi.POST("/v1/hub/params/{set}/versions/{version}/decision", {
            params: { path: { set, version: v.version! }, header: idem() },
            body: decision === "rejected" ? { decision, reason: rejectReason.trim() } : { decision },
          }),
        ),
      decision === "approved" ? `Đã duyệt phiên bản ${v.version} — áp dụng từ ngày hiệu lực` : `Đã từ chối phiên bản ${v.version}`,
    );
    if (r) data.reload();
  };

  return (
    <div className="stack">
      <div className="row">
        <span>
          Đang áp dụng: <b className="mono">{data.data?.active_version}</b>
        </span>
        {data.data?.active_version?.endsWith("#0") ? <Provisional /> : <Chip status="approved" label="Phiên bản đã duyệt" />}
        <Provisional what="C-4" />
      </div>
      <ActionStatus a={act} />
      <div className="tbl-wrap">
        <table className="tbl" aria-label={`Lịch sử ${set}`}>
          <thead>
            <tr>
              <th>Phiên bản</th>
              <th>Trạng thái</th>
              <th>Hiệu lực</th>
              <th>Lý do</th>
              <th>Người đặt</th>
              <th>Thao tác</th>
            </tr>
          </thead>
          <tbody>
            {items.map((v) => (
              <tr key={v.id}>
                <td className="mono">
                  {v.rule_version}#{v.version}
                </td>
                <td>
                  <Chip status={v.state} />
                </td>
                <td>{formatDate(v.effective_from)}</td>
                <td>{v.reason}</td>
                <td>
                  <Ref id={v.proposed_by} />
                </td>
                <td>
                  {v.state === "pending" && hasRole(principal, ROLES.hubApprover) ? (
                    <div className="stack" style={{ gap: 6 }}>
                      <SelfHint proposer={v.proposed_by} />
                      <div className="row">
                        <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void decide(v, "approved")}>
                          Duyệt tham số
                        </button>
                        <button type="button" className="btn btn-d btn-sm" disabled={act.busy || !validReason(rejectReason)} onClick={() => void decide(v, "rejected")}>
                          Từ chối tham số
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
      {pending && hasRole(principal, ROLES.hubApprover) ? <TextField label="Lý do từ chối tham số" value={rejectReason} onChange={setRejectReason} hint="Bắt buộc khi từ chối" /> : null}
      {hasRole(principal, ROLES.hubOperator) && !pending ? (
        <div className="panel stack">
          <b>Đề xuất phiên bản mới</b>
          <div className="grid3">
            {keys.map((k) => (
              <TextField key={k} label={LABEL[k] ?? k} value={f[k] ?? ""} onChange={(v) => setForm({ ...f, [k]: v })} />
            ))}
          </div>
          <TextField label="Lý do đề xuất" value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" />
          {clientErr ? (
            <p role="alert" className="pill-bad">
              {clientErr}
            </p>
          ) : null}
          <div className="row">
            <button type="button" className="btn btn-p btn-sm" disabled={act.busy || !!clientErr || !validReason(reason)} onClick={() => void propose()}>
              Gửi đề xuất tham số
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

export default function HubParamsPage() {
  const [tab, setTab] = useState<Set>("risk");
  return (
    <div className="stack">
      <PageHead title="Tham số rủi ro v2" subtitle="hub.risk.v2 (xếp xuất theo rủi ro, cảnh báo môi trường kho) và hub.weigh.v1 (dung sai phiếu cân, bắt buộc phiếu). Người đặt ≠ người duyệt (R-56); #0 = mặc định tạm." />
      <Section title="Bộ tham số">
        <Tabs tabs={[{ id: "risk", label: "hub.risk.v2" }, { id: "weigh", label: "hub.weigh.v1" }]} value={tab} onChange={(v) => setTab(v as Set)} label="Bộ tham số hub">
          <ParamSet key={tab} set={tab} />
        </Tabs>
      </Section>
    </div>
  );
}
