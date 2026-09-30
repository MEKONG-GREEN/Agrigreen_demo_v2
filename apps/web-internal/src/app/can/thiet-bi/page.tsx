"use client";

import { useState } from "react";
import { idem, unwrap, unwrapAs } from "@/api/client";
import type { ScaleDevice } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { ActionStatus, Chip, FacilitySelect, Provisional, Ref, Section, SelectField, SelfHint, TextField, useAction, useFacilities } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { validReason } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

const DEVICE_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$/;
const KEY_RE = /^[A-Za-z0-9+/]{43}=$/;
const SHA_RE = /^[0-9a-f]{64}$/;

export default function DeviceRegistryPage() {
  const { adminApi, principal } = useAuth();
  const facilities = useFacilities();
  const list = useAsync(async () => ((await unwrap(adminApi.GET("/v1/measurement/devices", { params: { query: { limit: 200 } } }))).items ?? []) as ScaleDevice[], [adminApi]);
  const [f, setF] = useState({ device_id: "", facility_ref: "", point: "HUB_IN", public_key: "", cert_sha256: "", scale_model: "" });
  const [reason, setReason] = useState("");
  const act = useAction();
  const canPropose = hasRole(principal, ROLES.scaleSupervisor);
  const canDecide = hasRole(principal, ROLES.deviceApprover);
  const canRevoke = hasRole(principal, ROLES.deviceApprover, ROLES.scaleSupervisor);
  const ok = DEVICE_RE.test(f.device_id) && !!f.facility_ref && KEY_RE.test(f.public_key.trim()) && SHA_RE.test(f.cert_sha256.trim()) && !!f.scale_model.trim();
  const facName = (id: string | undefined) => facilities.data?.find((x) => x.id === id)?.code ?? id;

  const propose = async () => {
    const r = await act.run(
      () =>
        unwrapAs<ScaleDevice>(
          adminApi.POST("/v1/measurement/devices", {
            params: { header: idem() },
            body: { device_id: f.device_id.trim(), facility_ref: f.facility_ref, point: f.point as "HUB_IN", public_key: f.public_key.trim(), cert_sha256: f.cert_sha256.trim(), scale_model: f.scale_model.trim() },
          }),
        ),
      (d) => `Đã đề xuất ${d.device_id} kỳ ${d.epoch} — chờ device_approver khác duyệt`,
    );
    if (r) list.reload();
  };
  const decide = async (d: ScaleDevice, decision: "approved" | "rejected") => {
    const r = await act.run(
      () =>
        unwrapAs<ScaleDevice>(
          adminApi.POST("/v1/measurement/devices/{device_id}/epochs/{epoch}/decision", {
            params: { path: { device_id: d.device_id!, epoch: d.epoch! }, header: idem() },
            body: decision === "rejected" ? { decision, reason: reason.trim() } : { decision },
          }),
        ),
      decision === "approved" ? `Đã duyệt ${d.device_id} kỳ ${d.epoch} — kỳ cũ đang hiệu lực chuyển thành bị thay` : `Đã từ chối ${d.device_id} kỳ ${d.epoch}`,
    );
    if (r) list.reload();
  };
  const revoke = async (d: ScaleDevice) => {
    const r = await act.run(
      () => unwrapAs<ScaleDevice>(adminApi.POST("/v1/measurement/devices/{device_id}/epochs/{epoch}/revoke", { params: { path: { device_id: d.device_id!, epoch: d.epoch! }, header: idem() }, body: { reason: reason.trim() } })),
      `Đã thu hồi ${d.device_id} kỳ ${d.epoch} — lô số đo mới bị ingest từ chối`,
    );
    if (r) list.reload();
  };

  return (
    <div className="stack">
      <PageHead
        title="Sổ thiết bị cân"
        subtitle="Mỗi kỳ đăng ký gồm khóa nguồn Ed25519 + băm chứng thư mTLS (ADR-0019); kỳ tăng dần, không dùng lại. Giám sát cân đề xuất, device_approver KHÁC người đề xuất duyệt; thu hồi không cần người thứ hai."
      />
      <ActionStatus a={act} />
      {canPropose ? (
        <Section title="Đăng ký kỳ mới" actions={<Provisional what="C-6" />}>
          <div className="stack">
            <div className="grid3">
              <TextField label="Mã thiết bị" value={f.device_id} onChange={(v) => setF({ ...f, device_id: v })} hint="Trùng CN/SAN chứng thư mTLS" />
              <FacilitySelect value={f.facility_ref} onChange={(v) => setF({ ...f, facility_ref: v })} facilities={facilities.data ?? []} />
              <SelectField label="Điểm cân" value={f.point} onChange={(v) => setF({ ...f, point: v })} options={[{ value: "HUB_IN", label: "HUB_IN — cân nhập hub" }, { value: "HUB_OUT", label: "HUB_OUT — cân xuất hub" }]} />
              <TextField label="Khóa công khai Ed25519 (base64)" value={f.public_key} onChange={(v) => setF({ ...f, public_key: v })} hint="32 byte, base64 chuẩn (44 ký tự)" />
              <TextField label="SHA-256 chứng thư mTLS" value={f.cert_sha256} onChange={(v) => setF({ ...f, cert_sha256: v })} hint="64 ký tự hex thường" />
              <TextField label="Mẫu cân" value={f.scale_model} onChange={(v) => setF({ ...f, scale_model: v })} />
            </div>
            <div className="row">
              <button type="button" className="btn btn-p btn-sm" disabled={!ok || act.busy} onClick={() => void propose()}>
                Đề xuất đăng ký
              </button>
            </div>
          </div>
        </Section>
      ) : null}
      <Section title="Các kỳ đăng ký">
        {list.loading ? <Loading /> : null}
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        {list.data ? (
          <div className="stack">
            <TextField label="Lý do (từ chối / thu hồi)" value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" />
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Kỳ đăng ký thiết bị">
                <thead>
                  <tr>
                    <th>Thiết bị</th>
                    <th>Kỳ</th>
                    <th>Cơ sở · điểm</th>
                    <th>Khóa</th>
                    <th>Trạng thái</th>
                    <th>Đề xuất</th>
                    <th>Thao tác</th>
                  </tr>
                </thead>
                <tbody>
                  {list.data.map((d) => (
                    <tr key={`${d.device_id}#${d.epoch}`}>
                      <td className="mono">{d.device_id}</td>
                      <td>{d.epoch}</td>
                      <td>
                        {facName(d.facility_ref)} · {d.point}
                      </td>
                      <td className="mono" title={d.public_key}>
                        {d.key_id}
                      </td>
                      <td>
                        <Chip status={d.status} />
                        {d.revoke_reason ? <div className="small mute">{d.revoke_reason}</div> : null}
                      </td>
                      <td className="small">
                        <Ref id={d.proposed_by} /> {formatDateTime(d.proposed_at)}
                      </td>
                      <td>
                        <div className="stack" style={{ gap: 6 }}>
                          {d.status === "pending" && canDecide ? (
                            <>
                              <SelfHint proposer={d.proposed_by} />
                              <div className="row">
                                <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void decide(d, "approved")} aria-label={`Duyệt ${d.device_id} kỳ ${d.epoch}`}>
                                  Duyệt
                                </button>
                                <button type="button" className="btn btn-d btn-sm" disabled={act.busy || !validReason(reason)} onClick={() => void decide(d, "rejected")} aria-label={`Từ chối ${d.device_id} kỳ ${d.epoch}`}>
                                  Từ chối
                                </button>
                              </div>
                            </>
                          ) : null}
                          {(d.status === "active" || d.status === "pending") && canRevoke ? (
                            <button type="button" className="btn btn-o btn-sm" disabled={act.busy || !validReason(reason)} onClick={() => void revoke(d)} aria-label={`Thu hồi ${d.device_id} kỳ ${d.epoch}`}>
                              Thu hồi
                            </button>
                          ) : null}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        ) : null}
      </Section>
    </div>
  );
}
