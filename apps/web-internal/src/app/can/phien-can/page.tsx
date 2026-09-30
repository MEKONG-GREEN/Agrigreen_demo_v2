"use client";

import { useState } from "react";
import { idem, unwrap, unwrapAs } from "@/api/client";
import type { ScaleDevice, WeighingSession } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { ActionStatus, Chip, Ref, RefPicker, Section, SelectField, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { isKg, isPct } from "@/lib/kg";
import { labelOf, remember } from "@/lib/recent";
import { useAsync } from "@/lib/useAsync";

export default function SessionsPage() {
  const { api } = useAuth();
  const devices = useAsync(async () => ((await unwrap(api.GET("/v1/measurement/devices", { params: { query: { limit: 200 } } }))).items ?? []) as ScaleDevice[], [api]);
  const active = (devices.data ?? []).filter((d) => d.status === "active");
  const [dev, setDev] = useState("");
  const [lot, setLot] = useState("");
  const [from, setFrom] = useState("");
  const [sid, setSid] = useState("");
  const [to, setTo] = useState("");
  const [tare, setTare] = useState("");
  const [moist, setMoist] = useState("");
  const open = useAction();
  const close = useAction();
  const session = useAsync(async () => (/^[0-9a-f-]{36}$/i.test(sid) ? unwrapAs<WeighingSession>(api.GET("/v1/measurement/sessions/{id}", { params: { path: { id: sid } } })) : null), [api, sid]);
  const chosen = active.find((d) => `${d.device_id}#${d.epoch}` === dev) ?? active[0];

  const doOpen = async () => {
    if (!chosen) return;
    const r = await open.run(
      () => unwrapAs<WeighingSession>(api.POST("/v1/measurement/sessions", { params: { header: idem() }, body: { lot_ref: lot, device_id: chosen.device_id!, epoch: chosen.epoch!, counter_from: Number(from) } })),
      (s) => `Đã mở phiên cân ${s.point} trên ${s.device_id} từ bộ đếm ${s.counter_from}`,
    );
    if (r?.id) {
      remember("session", r.id, `${labelOf("lot", lot) ?? lot.slice(-6)} · ${r.point}`);
      setSid(r.id);
    }
  };
  const doClose = async () => {
    const r = await close.run(
      () =>
        unwrapAs<WeighingSession>(
          api.POST("/v1/measurement/sessions/{id}/close", {
            params: { path: { id: sid }, header: idem() },
            body: { counter_to: Number(to), ...(tare.trim() ? { tare_kg: tare.trim() } : {}), ...(moist.trim() ? { moisture_pct: moist.trim() } : {}) },
          }),
        ),
      "Đã đóng phiên cân — ingest tích phân cửa sổ bộ đếm thành phiếu cân (R-70, weigh.integrate.v1 tạm C-6)",
    );
    if (r) session.reload();
  };
  const s = session.data;
  return (
    <div className="stack">
      <PageHead title="Phiên cân" subtitle="Mở phiên cân của lô trên một thiết bị: cửa sổ lô tính theo BỘ ĐẾM (không theo giờ thiết bị). Điểm cân và cơ sở lấy từ kỳ đăng ký đang hiệu lực." />
      <Section title="Mở phiên cân">
        <div className="stack">
          {devices.error ? <ProblemAlert problem={devices.error} /> : null}
          <div className="grid3">
            <SelectField label="Thiết bị cân (kỳ đang hiệu lực)" value={dev || (chosen ? `${chosen.device_id}#${chosen.epoch}` : "")} onChange={setDev} options={active.map((d) => ({ value: `${d.device_id}#${d.epoch}`, label: `${d.device_id} · kỳ ${d.epoch} · ${d.point}` }))} />
            <TextField label="Bộ đếm bắt đầu" value={from} onChange={setFrom} inputMode="numeric" hint={chosen?.health?.last_counter != null ? `Bộ đếm cuối ingest thấy: ${chosen.health.last_counter}` : undefined} />
          </div>
          <RefPicker label="Mã lô cân" kind="lot" value={lot} onChange={setLot} />
          <div className="row">
            <button type="button" className="btn btn-p btn-sm" disabled={!chosen || !/^\d+$/.test(from) || !/^[0-9a-f-]{36}$/i.test(lot) || open.busy} onClick={() => void doOpen()}>
              Mở phiên cân
            </button>
          </div>
          <ActionStatus a={open} />
        </div>
      </Section>
      <Section title="Phiên cân đang theo dõi">
        <div className="stack">
          <RefPicker label="Mã phiên cân" kind="session" value={sid} onChange={setSid} />
          {session.loading && sid ? <Loading /> : null}
          {session.error ? <ProblemAlert problem={session.error} /> : null}
          {s ? (
            <>
              <dl className="kv">
                <dt>Trạng thái</dt>
                <dd>
                  <Chip status={s.status === "open" ? "open" : "closed"} label={s.status === "open" ? "Đang mở" : "Đã đóng"} />
                </dd>
                <dt>Lô</dt>
                <dd>
                  {labelOf("lot", s.lot_ref ?? "") ?? ""} <Ref id={s.lot_ref} />
                </dd>
                <dt>Điểm cân · thiết bị</dt>
                <dd>
                  {s.point} · {s.device_id} (kỳ {s.epoch})
                </dd>
                <dt>Cửa sổ bộ đếm</dt>
                <dd>
                  {s.counter_from} → {s.counter_to ?? "…"}
                </dd>
                <dt>Mở lúc</dt>
                <dd>{formatDateTime(s.opened_at)}</dd>
                <dt>Bì / độ ẩm</dt>
                <dd>
                  {s.tare_kg ?? "—"} kg / {s.moisture_pct ?? "—"} %
                </dd>
              </dl>
              {s.status === "open" ? (
                <div className="panel stack">
                  <b>Đóng phiên cân</b>
                  <div className="grid3">
                    <TextField label="Bộ đếm kết thúc" value={to} onChange={setTo} inputMode="numeric" />
                    <TextField label="Bì (kg)" value={tare} onChange={setTare} inputMode="decimal" />
                    <TextField label="Độ ẩm (%)" value={moist} onChange={setMoist} inputMode="decimal" />
                  </div>
                  <div className="row">
                    <button
                      type="button"
                      className="btn btn-p btn-sm"
                      disabled={!/^\d+$/.test(to) || (!!tare && !isKg(tare)) || (!!moist && !isPct(moist)) || close.busy}
                      onClick={() => void doClose()}
                    >
                      Đóng phiên cân
                    </button>
                  </div>
                </div>
              ) : null}
              <ActionStatus a={close} />
            </>
          ) : null}
        </div>
      </Section>
    </div>
  );
}
