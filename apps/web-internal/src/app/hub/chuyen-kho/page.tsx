"use client";

import { useState } from "react";
import { idem, unwrapAs } from "@/api/client";
import type { Transfer } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { ActionStatus, Chip, FacilitySelect, Ref, RefPicker, Section, TextField, useAction, useFacilities } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { formatKg, isKg } from "@/lib/kg";
import { useAsync } from "@/lib/useAsync";

export default function TransferPage() {
  const { api, principal } = useAuth();
  const canWrite = hasRole(principal, ROLES.hubOperator);
  const facilities = useFacilities();
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [mass, setMass] = useState("");
  const [tid, setTid] = useState("");
  const [recvMass, setRecvMass] = useState("");
  const [recvTickets, setRecvTickets] = useState("");
  const start = useAction();
  const recv = useAction();
  const t = useAsync(async () => (/^[0-9a-f-]{36}$/i.test(tid) ? (unwrapAs<Transfer>(api.GET("/v1/hub/transfers/{id}", { params: { path: { id: tid } } }))) : null), [api, tid]);
  const name = (id: string | undefined) => facilities.data?.find((f) => f.id === id)?.name ?? id ?? "—";

  const doStart = async () => {
    const r = await start.run(
      () => unwrapAs<Transfer>(api.POST("/v1/hub/facilities/{id}/transfers", { params: { path: { id: from }, header: idem() }, body: { to_facility_id: to, mass_kg: mass.trim() } })),
      (x) => `Đã xuất chuyển ${formatKg(x.dispatched_kg)} — hàng đang đi đường`,
    );
    if (r?.id) setTid(r.id);
  };
  const doReceive = async (tr: Transfer) => {
    const refs = recvTickets
      .split(/[\s,]+/)
      .map((s) => s.trim())
      .filter(Boolean);
    const r = await recv.run(
      () =>
        unwrapAs<Transfer>(
          api.POST("/v1/hub/facilities/{id}/transfers/{transfer}/receipt", {
            params: { path: { id: tr.to_facility_id!, transfer: tr.id! }, header: idem() },
            body: refs.length ? { weigh_ticket_refs: refs } : { mass_kg: recvMass.trim() },
          }),
        ),
      (x) => `Đã nhận ${formatKg(x.received_kg)} — hao hụt đường ${formatKg(x.loss_kg, 3)}`,
    );
    if (r) t.reload();
  };

  return (
    <div className="stack">
      <PageHead title="Chuyển kho" subtitle="Xuất chuyển sang cơ sở khác (Q-MKG-10): phân bổ theo hub.risk.v2; nhận ở đích bằng khối lượng tay hoặc phiếu cân HUB_IN; hao hụt đường = xuất − nhận." />
      {canWrite ? (
        <Section title="Xuất chuyển kho">
          <div className="stack">
            <div className="grid3">
              <FacilitySelect label="Từ cơ sở" value={from} onChange={setFrom} facilities={facilities.data ?? []} />
              <FacilitySelect label="Đến cơ sở" value={to} onChange={setTo} facilities={facilities.data ?? []} exclude={from} />
              <TextField label="Khối lượng chuyển (kg)" value={mass} onChange={setMass} inputMode="decimal" />
            </div>
            <div className="row">
              <button type="button" className="btn btn-p btn-sm" disabled={!from || !to || !isKg(mass) || start.busy} onClick={() => void doStart()}>
                Xuất chuyển kho
              </button>
            </div>
            <ActionStatus a={start} />
          </div>
        </Section>
      ) : null}
      <Section title="Phiếu chuyển">
        <div className="stack">
          <RefPicker label="Mã phiếu chuyển" kind="transfer" value={tid} onChange={setTid} />
          {t.loading && tid ? <Loading /> : null}
          {t.error ? <ProblemAlert problem={t.error} /> : null}
          {t.data ? (
            <>
              <dl className="kv">
                <dt>Trạng thái</dt>
                <dd>
                  <Chip status={t.data.status} />
                </dd>
                <dt>Tuyến</dt>
                <dd>
                  {name(t.data.from_facility_id)} → {name(t.data.to_facility_id)}
                </dd>
                <dt>Đã xuất</dt>
                <dd>
                  {formatKg(t.data.dispatched_kg, 3)} lúc {formatDateTime(t.data.dispatched_at)}
                </dd>
                <dt>Đã nhận</dt>
                <dd>{t.data.received_kg ? `${formatKg(t.data.received_kg, 3)} lúc ${formatDateTime(t.data.received_at)}` : "—"}</dd>
                <dt>Hao hụt đường</dt>
                <dd>{formatKg(t.data.loss_kg, 3)}</dd>
                <dt>Lô nguồn</dt>
                <dd>
                  {t.data.source_lots?.map((l) => (
                    <span key={l.lot_id} style={{ marginRight: 8 }}>
                      <Ref id={l.lot_id} /> {formatKg(l.mass_kg)}
                    </span>
                  ))}
                </dd>
                {t.data.received_lots?.length ? (
                  <>
                    <dt>Lô mới ở đích</dt>
                    <dd>
                      {t.data.received_lots.map((l) => (
                        <span key={l.lot_id} style={{ marginRight: 8 }}>
                          <Ref id={l.lot_id} /> {formatKg(l.mass_kg)}
                        </span>
                      ))}
                    </dd>
                  </>
                ) : null}
              </dl>
              {t.data.status === "in_transit" && canWrite ? (
                <div className="panel stack">
                  <b>Nhận hàng ở {name(t.data.to_facility_id)}</b>
                  <div className="grid2">
                    <TextField label="Khối lượng nhận (kg)" value={recvMass} onChange={setRecvMass} inputMode="decimal" />
                    <TextField label="Hoặc phiếu cân HUB_IN (mã, cách nhau dấu phẩy)" value={recvTickets} onChange={setRecvTickets} />
                  </div>
                  <div className="row">
                    <button type="button" className="btn btn-p btn-sm" disabled={recv.busy || (!isKg(recvMass) && !recvTickets.trim())} onClick={() => void doReceive(t.data!)}>
                      Xác nhận nhận hàng
                    </button>
                  </div>
                </div>
              ) : null}
              <ActionStatus a={recv} />
            </>
          ) : null}
        </div>
      </Section>
    </div>
  );
}
