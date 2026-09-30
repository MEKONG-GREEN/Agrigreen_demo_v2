"use client";

import { useState } from "react";
import { idem, unwrap } from "@/api/client";
import type { Movement } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { ActionStatus, Chip, FacilitySelect, Ref, RefPicker, Section, TextField, useAction, useFacilities } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, KpiTile, Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { formatKg, formatTons } from "@/lib/kg";
import { remember } from "@/lib/recent";
import { PRODUCT_LABEL, validReason } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";


const MOVE_LABEL: Record<string, string> = { receive: "Nhận", dispatch: "Xuất", correction: "Bù sai", transform_in: "Vào mẻ", transform_out: "Ra mẻ" };

function LotDetail({ lotId }: { lotId: string }) {
  const { api, principal } = useAuth();
  const canCorrect = hasRole(principal, ROLES.hubOperator);
  const lot = useAsync(() => unwrap(api.GET("/v1/hub/lots/{id}", { params: { path: { id: lotId } } })), [api, lotId]);
  const meas = useAsync(() => unwrap(api.GET("/v1/hub/lots/{id}/measurements", { params: { path: { id: lotId } } })).catch(() => null), [api, lotId]);
  const [target, setTarget] = useState<Movement | null>(null);
  const [reason, setReason] = useState("");
  const act = useAction();
  if (lot.loading) return <Loading />;
  if (lot.error) return <ProblemAlert problem={lot.error} onRetry={lot.reload} />;
  const l = lot.data!;
  const corrected = new Set(l.movements?.map((m) => m.supersedes_id).filter(Boolean));
  const correct = async () => {
    if (!target) return;
    const r = await act.run(
      () => unwrap(api.POST("/v1/hub/movements/{id}/corrections", { params: { path: { id: target.id! }, header: idem() }, body: { reason: reason.trim() } })),
      "Đã ghi dòng bù ngược dấu (không sửa dòng gốc — RB-1)",
    );
    if (r) {
      setTarget(null);
      setReason("");
      lot.reload();
    }
  };
  return (
    <div className="stack">
      <dl className="kv">
        <dt>Mã lô</dt>
        <dd>
          {l.lot?.code} <Ref id={l.lot?.id} />
        </dd>
        <dt>Dạng hàng</dt>
        <dd>{PRODUCT_LABEL[l.lot?.product ?? ""] ?? l.lot?.product}</dd>
        <dt>Nhận ban đầu</dt>
        <dd>{formatKg(l.lot?.initial_kg, 3)}</dd>
        <dt>Còn lại</dt>
        <dd>{formatKg(l.remaining_kg, 3)}</dd>
        <dt>Độ ẩm khi nhận</dt>
        <dd>{l.lot?.moisture_pct ? `${l.lot.moisture_pct} %` : "Không đo"}</dd>
        <dt>Nguồn gốc</dt>
        <dd>
          {l.lot?.sources?.map((s, i) => (
            <span key={i} className="chip c-info" style={{ marginRight: 6 }}>
              {s.kind} <Ref id={s.ref} /> × {s.ratio}
            </span>
          ))}
        </dd>
        {meas.data ? (
          <>
            <dt>Phiếu cân HUB_IN</dt>
            <dd>{meas.data.hub_in ? `${formatKg((meas.data.hub_in as { measured_kg?: string }).measured_kg, 3)} (${(meas.data.hub_in as { status?: string }).status})` : "Chưa có"}</dd>
            <dt>Tham số cân</dt>
            <dd className="mono">{meas.data.weigh_param_version}</dd>
          </>
        ) : null}
      </dl>
      <ActionStatus a={act} />
      <div className="tbl-wrap">
        <table className="tbl" aria-label="Dịch chuyển của lô">
          <thead>
            <tr>
              <th>Thời điểm</th>
              <th>Loại</th>
              <th className="num">Lượng</th>
              <th className="num">Tồn sau</th>
              <th>Tham chiếu</th>
              <th>Ghi chú</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {l.movements?.map((m) => (
              <tr key={m.id}>
                <td>{formatDateTime(m.event_time)}</td>
                <td>{MOVE_LABEL[m.kind ?? ""] ?? m.kind}</td>
                <td className="num">{formatKg(m.qty_kg, 3)}</td>
                <td className="num">{formatKg(m.balance_after_kg, 3)}</td>
                <td>
                  {m.ref_type ? `${m.ref_type} ` : ""}
                  <Ref id={m.ref_id} />
                </td>
                <td>{m.correction_reason ?? (corrected.has(m.id) ? "Đã bù" : "")}</td>
                <td>
                  {canCorrect && (m.kind === "receive" || m.kind === "dispatch") && !corrected.has(m.id) ? (
                    <button type="button" className="btn btn-o btn-sm" onClick={() => setTarget(m)}>
                      Bù sai
                    </button>
                  ) : null}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {target ? (
        <div className="panel stack">
          <b>
            Bù sai dòng {MOVE_LABEL[target.kind ?? ""]} {formatKg(target.qty_kg, 3)}
          </b>
          <TextField label="Lý do bù sai" value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" />
          <div className="row">
            <button type="button" className="btn btn-p btn-sm" disabled={!validReason(reason) || act.busy} onClick={() => void correct()}>
              Ghi dòng bù
            </button>
            <button type="button" className="btn btn-o btn-sm" onClick={() => setTarget(null)}>
              Huỷ
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

export default function InventoryPage() {
  const { api, principal } = useAuth();
  const facilities = useFacilities();
  const [fid, setFid] = useState("");
  const facility = fid || facilities.data?.[0]?.id || "";
  const inv = useAsync(async () => (facility ? unwrap(api.GET("/v1/hub/facilities/{id}/inventory", { params: { path: { id: facility } } })) : null), [api, facility]);
  const [lotId, setLotId] = useState("");
  const snap = useAction();
  const cap = inv.data?.capacity;
  return (
    <div className="stack">
      <PageHead title="Lô & tồn kho" subtitle="Lô là nguồn sự thật duy nhất của tồn. Thứ tự theo điểm rủi ro xuống cấp hub.risk.v2 (tuổi + độ ẩm + cảnh báo môi trường) — không FEFO." />
      <Section
        title="Tồn theo lô"
        actions={
          hasRole(principal, ROLES.hubOperator) && facility ? (
            <button
              type="button"
              className="btn btn-o btn-sm"
              disabled={snap.busy}
              onClick={() => void snap.run(() => unwrap(api.POST("/v1/hub/facilities/{id}/capacity-snapshots", { params: { path: { id: facility }, header: idem() } })), "Đã chụp sức chứa và xuất van hub_capacity v1")}
            >
              Chụp sức chứa (van)
            </button>
          ) : null
        }
      >
        <div className="stack">
          <FacilitySelect value={facility} onChange={setFid} facilities={facilities.data ?? []} />
          <ActionStatus a={snap} />
          {inv.error ? <ProblemAlert problem={inv.error} onRetry={inv.reload} /> : null}
          {inv.loading ? <Loading /> : null}
          {cap ? (
            <div className="grid4">
              <KpiTile label="Sức chứa" value={formatTons(cap.capacity_kg).replace(" t", "")} unit="t" color="blue" icon="warehouse" />
              <KpiTile label="Đang dùng" value={formatTons(cap.used_kg).replace(" t", "")} unit="t" color="green" icon="layers" />
              <KpiTile label="Còn trống" value={formatTons(cap.available_kg).replace(" t", "")} unit="t" color={Number(cap.available_kg) < 0 ? "red" : "violet"} icon="area" />
              <KpiTile label="Quy tắc rủi ro" value={inv.data?.risk_rule ?? "—"} color="orange" icon="param" />
            </div>
          ) : null}
          {inv.data && !inv.data.lots?.length ? <Empty>Cơ sở chưa có lô còn tồn.</Empty> : null}
          {inv.data?.lots?.length ? (
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Lô theo rủi ro">
                <thead>
                  <tr>
                    <th>#</th>
                    <th>Mã lô</th>
                    <th>Dạng hàng</th>
                    <th className="num">Còn lại</th>
                    <th className="num">Độ ẩm</th>
                    <th className="num">Tuổi (ngày)</th>
                    <th className="num">Điểm rủi ro</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {inv.data.lots.map((l, i) => (
                    <tr key={l.lot_id}>
                      <td>{i + 1}</td>
                      <td className="mono">{l.code}</td>
                      <td>{PRODUCT_LABEL[l.product ?? ""] ?? l.product}</td>
                      <td className="num">{formatKg(l.remaining_kg)}</td>
                      <td className="num">{l.moisture_pct ? `${l.moisture_pct} %` : "—"}</td>
                      <td className="num">{l.age_days}</td>
                      <td className="num">
                        <b className={Number(l.risk_score) >= 50 ? "pill-bad" : undefined}>{l.risk_score}</b>
                      </td>
                      <td>
                        <button
                          type="button"
                          className="btn btn-o btn-sm"
                          aria-label={`Chi tiết lô ${l.code}`}
                          onClick={() => {
                            remember("lot", l.lot_id!, l.code);
                            setLotId(l.lot_id!);
                          }}
                        >
                          Chi tiết
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </div>
      </Section>
      <Section title="Tra cứu lô">
        <div className="stack">
          <RefPicker label="Mã lô" kind="lot" value={lotId} onChange={setLotId} hint="Cả lô đã xuất hết (tồn 0) — dùng mã UUID" />
          {lotId ? <LotDetail key={lotId} lotId={lotId} /> : <Chip status="pending_rolls" label="Chọn một lô" />}
        </div>
      </Section>
    </div>
  );
}
