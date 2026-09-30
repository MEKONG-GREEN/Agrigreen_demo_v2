"use client";

import { useState } from "react";
import { idem, unwrap } from "@/api/client";
import type { LogisticsEndpointInput, LogisticsTrip, LogisticsVessel, RouteSnapshot } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { Flags, KV, Money, When } from "@/components/g4kit";
import { ActionStatus, Chip, Ref, Section, SelectField, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, KpiTile, Loading, PageHead } from "@/components/ui";
import { COST_SOURCE_LABEL, ENDPOINT_KIND_LABEL, flagLabel, MODE_LABEL, nextMilestone, ROUTE_SOURCE_LABEL, TRIP_EVENT_LABEL, TRIP_STATUS_LABEL } from "@/lib/g4labels";
import { formatKg, formatTons, isKg } from "@/lib/kg";
import { formatDecimalString, formatPrice, isMoney, shortHash } from "@/lib/money";
import { validReason } from "@/lib/rules";
import { base64ToBytes, sha256Hex } from "@/lib/sha256";
import { useAsync } from "@/lib/useAsync";

type Mode = "road" | "waterway";
const STATUSES = ["planned", "departed", "arrived", "completed", "cancelled"] as const;
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Điểm hay dùng (toạ độ WGS84 dạng chuỗi — R-10). Chọn "Điểm toạ độ" để nhập tay. */
const PRESETS: { id: string; label: string; p: LogisticsEndpointInput }[] = [
  { id: "bai", label: "Bãi tập kết Tân Hiệp", p: { kind: "collection_point", lat: "10.0452", lng: "105.2134" } },
  { id: "hub-tb", label: "Hub Thoại Bình (HUB-TB)", p: { kind: "facility", ref: "0192f001-0000-7000-8000-000000000001", lat: "10.2710", lng: "105.3561" } },
  { id: "hub-ct", label: "Hub Cần Thơ (HUB-CT)", p: { kind: "facility", ref: "0192f001-0000-7000-8000-000000000002", lat: "10.0341", lng: "105.7880" } },
  { id: "plant", label: "Nhà máy khách (Long An)", p: { kind: "customer_site", lat: "10.3802", lng: "106.1025" } },
  { id: "point", label: "Điểm toạ độ (nhập tay)", p: { kind: "point", lat: "", lng: "" } },
];

interface LegForm {
  mode: Mode;
  from: string;
  to: string;
  fromLat: string;
  fromLng: string;
  toLat: string;
  toLng: string;
  manualKm: string;
  manualReason: string;
}
const newLeg = (mode: Mode): LegForm => ({ mode, from: "bai", to: "hub-ct", fromLat: "", fromLng: "", toLat: "", toLng: "", manualKm: "", manualReason: "" });

function endpoint(preset: string, lat: string, lng: string): LogisticsEndpointInput {
  const p = PRESETS.find((x) => x.id === preset)!.p;
  return preset === "point" ? { kind: "point", lat: lat.trim(), lng: lng.trim() } : p;
}

function legError(l: LegForm): string | null {
  const coord = /^-?\d{1,3}(\.\d{1,7})?$/;
  if (l.from === "point" && (!coord.test(l.fromLat) || !coord.test(l.fromLng))) return "Điểm đi: nhập vĩ độ / kinh độ";
  if (l.to === "point" && (!coord.test(l.toLat) || !coord.test(l.toLng))) return "Điểm đến: nhập vĩ độ / kinh độ";
  if (l.manualKm && !/^\d+(\.\d{1,3})?$/.test(l.manualKm.trim())) return "Km tay: số, tối đa 3 số lẻ";
  if (l.manualKm && !validReason(l.manualReason)) return "Km tay cần lý do ≥ 10 ký tự (logistics.manual_km_reason_required)";
  return null;
}

function SnapshotView({ tripId, legId }: { tripId: string; legId: string }) {
  const { api } = useAuth();
  const s = useAsync(() => unwrap(api.GET("/v1/logistics/trips/{id}/legs/{leg}/route-snapshot", { params: { path: { id: tripId, leg: legId } } })), [api, tripId, legId]);
  if (s.loading && !s.data) return <Loading label="Đang đọc ảnh chụp đã lưu…" />;
  if (s.error) return <ProblemAlert problem={s.error} />;
  const snap = s.data as RouteSnapshot;
  const bytes = base64ToBytes(snap.raw_base64);
  const ok = sha256Hex(bytes) === snap.raw_sha256;
  return (
    <div className="panel stack small" aria-label="Ảnh chụp định tuyến">
      <KV
        items={[
          ["Nguồn", snap.provider === "pf_partner_waterway" ? "Bề mặt đối tác nền tảng (/v1/partner/waterway/route, cấp quyền waterway_routes)" : snap.provider],
          ["Phiên bản mạng", snap.network_version ?? "—"],
          ["SHA-256 nguyên văn", <span key="h" className="mono">{snap.raw_sha256}</span>],
          ["Kiểm băm", ok ? <span key="k" className="chip c-ok">Băm khớp nguyên văn</span> : <span key="k" className="chip c-bad">Băm KHÔNG khớp</span>],
          ["Lấy lúc", <When key="w" at={snap.fetched_at} />],
        ]}
      />
      <pre className="mono" style={{ whiteSpace: "pre-wrap", wordBreak: "break-all", maxHeight: 160, overflow: "auto" }}>
        {new TextDecoder().decode(bytes)}
      </pre>
      <span className="mute">Đọc chặng cũ không gọi lại nền tảng — dùng đúng bản đã lưu (mục 16, L-03).</span>
    </div>
  );
}

function TripPanel({ id, vessels, onChanged }: { id: string; vessels: LogisticsVessel[]; onChanged: () => void }) {
  const { api, principal } = useAuth();
  const disp = hasRole(principal, ROLES.dispatcher);
  const drv = hasRole(principal, ROLES.driver, ROLES.dispatcher);
  const d = useAsync(() => unwrap(api.GET("/v1/logistics/trips/{id}", { params: { path: { id } } })), [api, id]);
  const events = useAsync(async () => (disp ? (await unwrap(api.GET("/v1/logistics/trips/{id}/events", { params: { path: { id } } }))).items : []), [api, id, disp]);
  const act = useAction();
  const [warnings, setWarnings] = useState<string[]>([]);
  const [vehicle, setVehicle] = useState("");
  const [driver, setDriver] = useState("");
  const [legs, setLegs] = useState<LegForm[] | null>(null);
  const [snap, setSnap] = useState<string | null>(null);
  const [actualKg, setActualKg] = useState("");
  const [ticket, setTicket] = useState("");
  const [cost, setCost] = useState("");
  const [costReason, setCostReason] = useState("");
  const [correction, setCorrection] = useState("");
  const [cancelReason, setCancelReason] = useState("");
  if (d.loading && !d.data) return <Loading />;
  if (d.error) return <ProblemAlert problem={d.error} />;
  const t = d.data!.trip;
  const vcode = (ref: string | null | undefined) => vessels.find((v) => v.vessel_id === ref)?.code;
  const open = t.status !== "completed" && t.status !== "cancelled";
  const run = async (fn: () => Promise<{ warnings: string[] }>, msg: string) => {
    const r = await act.run(fn, msg);
    if (r) {
      setWarnings(r.warnings ?? []);
      d.reload();
      events.reload();
      onChanged();
    }
    return r;
  };
  const p = () => ({ path: { id }, header: idem() });
  const ms = nextMilestone(t.status);
  const legErr = legs ? (legs.map(legError).find(Boolean) ?? null) : null;
  const firstLeg = t.legs?.[0];
  return (
    <div className="stack">
      <KV
        label="Thông tin chuyến"
        items={[
          ["Mã chuyến", <b key="c" className="mono">{t.code}</b>],
          ["Trạng thái", <Chip key="s" status={t.status} label={TRIP_STATUS_LABEL[t.status]} />],
          ["Phương thức", MODE_LABEL[t.mode]],
          ["Phương tiện / tài xế", <span key="v">{vcode(t.vehicle_ref) ?? <Ref id={t.vehicle_ref} />} · <Ref id={t.driver_ref} /></span>],
          ["Nguồn", t.source_ref ? <span key="src">Lượt xuống ghe (FM-05) <Ref id={t.source_ref} /> · lệnh gom <Ref id={t.collection_order_ref} /></span> : t.delivery_ref ? <span key="src">Giao nhận <Ref id={t.delivery_ref} /></span> : "Điều phối lập"],
          ["Hàng trên chuyến", <span key="cg">{(t.cargo ?? []).map((c) => <span key={c.lot_ref} className="chip c-mute">lô <Ref id={c.lot_ref} /> {formatKg(c.mass_kg)}</span>)}</span>],
          ["Tấn kế hoạch / thật", `${formatTons(t.planned_kg)} / ${formatTons(t.actual_kg)}`],
          ["Cước kế hoạch / thực", <span key="cost"><Money v={t.planned_cost} /> / <Money v={t.actual_cost} /> {t.cost_source ? <span className="small mute">({COST_SOURCE_LABEL[t.cost_source]})</span> : null}</span>],
          ["Mốc", <span key="m" className="small">lập <When at={t.planned_at} /> · xuất phát <When at={t.departed_at} /> · đến <When at={t.arrived_at} /> · hoàn tất <When at={t.completed_at} /></span>],
          ["Cờ cảnh báo (không chặn)", <Flags key="f" flags={t.flags} />],
          ...(t.cancel_reason ? ([["Lý do huỷ", t.cancel_reason]] as [string, string][]) : []),
        ]}
      />
      <ActionStatus a={act} />
      {warnings.length ? (
        <div className="alert alert-warn" role="status" aria-label="Cảnh báo thao tác">
          <span>Cảnh báo (không chặn): {warnings.map(flagLabel).join(" · ")}</span>
        </div>
      ) : null}

      <h3>Chặng (kế hoạch lần {t.plan_revision ?? 0})</h3>
      {t.legs?.length ? (
        <div className="tbl-wrap">
          <table className="tbl" aria-label="Chặng của chuyến">
            <thead>
              <tr>
                <th>#</th>
                <th>Phương thức</th>
                <th>Từ → đến</th>
                <th className="num">Km</th>
                <th>Nguồn km</th>
                <th>Ảnh chụp định tuyến</th>
                <th className="num">Đơn giá</th>
                <th className="num">Cước kế hoạch</th>
                <th>Cờ</th>
              </tr>
            </thead>
            <tbody>
              {t.legs.map((l) => (
                <tr key={l.leg_id}>
                  <td>{l.seq}</td>
                  <td>{MODE_LABEL[l.mode]}</td>
                  <td className="small">
                    {ENDPOINT_KIND_LABEL[l.from.kind ?? ""] ?? l.from.kind} ({l.from.lat}, {l.from.lng}) → {ENDPOINT_KIND_LABEL[l.to.kind ?? ""] ?? l.to.kind} ({l.to.lat}, {l.to.lng})
                  </td>
                  <td className="num">{formatDecimalString(l.distance_km)}</td>
                  <td className="small">
                    {ROUTE_SOURCE_LABEL[l.route_source]}
                    {l.manual_reason ? ` — ${l.manual_reason}` : ""}
                  </td>
                  <td>
                    {l.route_snapshot_ref ? (
                      <button type="button" className="btn btn-o btn-sm" onClick={() => setSnap(snap === l.leg_id ? null : l.leg_id)} aria-label={`Ảnh chụp chặng ${l.seq}`}>
                        mạng v{l.network_version} · {shortHash(l.route_snapshot_ref)}
                      </button>
                    ) : (
                      <span className="mute small">Không có (km {l.route_source === "manual" ? "tay" : "Haversine"})</span>
                    )}
                  </td>
                  <td className="num">{formatPrice(l.rate_vnd_per_t_km, "t·km")}</td>
                  <td className="num">
                    <Money v={l.planned_cost} />
                    <div className="small mute mono">{l.param_version}</div>
                  </td>
                  <td>
                    <Flags flags={l.flags} label={`Cờ chặng ${l.seq}`} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <Empty>Chưa lập chặng — chưa định tuyến.</Empty>
      )}
      {snap ? <SnapshotView tripId={id} legId={snap} /> : null}

      {disp && open ? (
        <div className="panel stack">
          <b>Gán phương tiện / tài xế</b>
          <div className="grid3">
            <SelectField label="Phương tiện" value={vehicle} onChange={setVehicle} options={[{ value: "", label: "— Chọn ghe / xe —" }, ...vessels.map((v) => ({ value: v.vessel_id, label: `${v.code} · ${v.name ?? ""}${v.status === "inactive" ? " (ngừng)" : ""}` }))]} />
            <TextField label="Mã tài xế (vault)" value={driver} onChange={setDriver} hint="UUID tài xế ở vault MKG (RB-3) — không nhập tên" />
          </div>
          <div className="row">
            <button type="button" className="btn btn-p btn-sm" disabled={act.busy || !vehicle || (!!driver && !UUID.test(driver.trim()))} onClick={() => void run(() => unwrap(api.POST("/v1/logistics/trips/{id}/vehicle", { params: p(), body: { vehicle_ref: vehicle, ...(driver.trim() ? { driver_ref: driver.trim() } : {}) } })), "Đã gán phương tiện")}>
              Gán phương tiện
            </button>
          </div>
        </div>
      ) : null}

      {disp && open ? (
        <div className="panel stack">
          <div className="row">
            <b>Lập (lại) chặng — mỗi lần là một kế hoạch mới</b>
            {!legs ? (
              <button type="button" className="btn btn-o btn-sm" onClick={() => setLegs([newLeg(t.mode)])}>
                Lập chặng
              </button>
            ) : null}
          </div>
          {legs ? (
            <>
              {legs.map((l, i) => {
                const set = (x: Partial<LegForm>) => setLegs(legs.map((y, j) => (j === i ? { ...y, ...x } : y)));
                return (
                  <fieldset key={i} className="stack" style={{ border: "1px solid var(--line, #ddd)", padding: 8, borderRadius: 8 }}>
                    <legend className="small">Chặng {i + 1}</legend>
                    <div className="grid3">
                      <SelectField label={`Phương thức chặng ${i + 1}`} value={l.mode} onChange={(mode) => set({ mode: mode as Mode })} options={[{ value: "road", label: "Đường bộ" }, { value: "waterway", label: "Đường thuỷ" }]} />
                      <SelectField label={`Điểm đi chặng ${i + 1}`} value={l.from} onChange={(from) => set({ from })} options={PRESETS.map((x) => ({ value: x.id, label: x.label }))} />
                      <SelectField label={`Điểm đến chặng ${i + 1}`} value={l.to} onChange={(to) => set({ to })} options={PRESETS.map((x) => ({ value: x.id, label: x.label }))} />
                    </div>
                    {l.from === "point" || l.to === "point" ? (
                      <div className="grid4">
                        {l.from === "point" ? (
                          <>
                            <TextField label={`Vĩ độ đi ${i + 1}`} value={l.fromLat} onChange={(fromLat) => set({ fromLat })} inputMode="decimal" />
                            <TextField label={`Kinh độ đi ${i + 1}`} value={l.fromLng} onChange={(fromLng) => set({ fromLng })} inputMode="decimal" />
                          </>
                        ) : null}
                        {l.to === "point" ? (
                          <>
                            <TextField label={`Vĩ độ đến ${i + 1}`} value={l.toLat} onChange={(toLat) => set({ toLat })} inputMode="decimal" />
                            <TextField label={`Kinh độ đến ${i + 1}`} value={l.toLng} onChange={(toLng) => set({ toLng })} inputMode="decimal" />
                          </>
                        ) : null}
                      </div>
                    ) : null}
                    <div className="grid2">
                      <TextField label={`Km nhập tay chặng ${i + 1}`} value={l.manualKm} onChange={(manualKm) => set({ manualKm })} inputMode="decimal" hint="Bỏ trống = định tuyến (đường thuỷ qua nền tảng) / Haversine × 1,3 (đường bộ)" />
                      <TextField label={`Lý do km tay chặng ${i + 1}`} value={l.manualReason} onChange={(manualReason) => set({ manualReason })} hint="Bắt buộc khi nhập km tay (≥ 10 ký tự)" />
                    </div>
                  </fieldset>
                );
              })}
              {legErr ? <p className="pill-bad">{legErr}</p> : null}
              <div className="row">
                <button type="button" className="btn btn-o btn-sm" disabled={legs.length >= 24} onClick={() => setLegs([...legs, newLeg(t.mode)])}>
                  Thêm chặng
                </button>
                <button
                  type="button"
                  className="btn btn-p btn-sm"
                  disabled={act.busy || !!legErr}
                  onClick={() =>
                    void run(
                      () =>
                        unwrap(
                          api.POST("/v1/logistics/trips/{id}/legs", {
                            params: p(),
                            body: {
                              legs: legs.map((l) => ({
                                mode: l.mode,
                                from: endpoint(l.from, l.fromLat, l.fromLng),
                                to: endpoint(l.to, l.toLat, l.toLng),
                                ...(l.manualKm.trim() ? { manual_km: l.manualKm.trim(), manual_reason: l.manualReason.trim() } : {}),
                              })),
                            },
                          }),
                        ),
                      "Đã lập chặng — lưu kết quả định tuyến nguyên văn + SHA-256",
                    ).then((r) => {
                      if (r) setLegs(null);
                    })
                  }
                >
                  Lưu chặng
                </button>
                <button type="button" className="btn btn-o btn-sm" onClick={() => setLegs(null)}>
                  Huỷ lập chặng
                </button>
              </div>
            </>
          ) : null}
        </div>
      ) : null}

      {drv && ms ? (
        <div className="panel row">
          <span className="small">Mốc kế tiếp (tài xế, từng bậc — L-02): {ms === "departed" ? "xuất phát" : "đến nơi"}</span>
          <button
            type="button"
            className="btn btn-p btn-sm"
            disabled={act.busy}
            onClick={() => {
              const at = ms === "departed" ? firstLeg?.from : t.legs?.at(-1)?.to;
              void run(() => unwrap(api.POST("/v1/logistics/trips/{id}/milestones", { params: p(), body: { kind: ms, event_time: new Date().toISOString(), lat: at?.lat ?? "10.0452", lng: at?.lng ?? "105.2134" } })), ms === "departed" ? "Đã ghi mốc xuất phát" : "Đã ghi mốc đến nơi");
            }}
          >
            {ms === "departed" ? "Ghi mốc xuất phát" : "Ghi mốc đến nơi"}
          </button>
        </div>
      ) : null}

      {disp && (t.status === "departed" || t.status === "arrived" || t.status === "completed") ? (
        <div className="panel stack">
          <b>{t.status === "completed" ? "Bản bù hoàn tất (RB-1 — phát lại trip.completed với supersedes_id)" : "Hoàn tất chuyến (tấn thật + cước thực)"}</b>
          <p className="small mute">Đường chính là phiếu cân nhà máy (consumer mkg.logistics.weigh_ticket). Cước: nhập tay có chứng từ → «manual»; bỏ trống → đơn giá thuê ghe (GH-03) hoặc đơn giá tham số.</p>
          <div className="grid3">
            <TextField label="Khối lượng thật (kg)" value={actualKg} onChange={setActualKg} inputMode="decimal" />
            <TextField label="Mã phiếu cân" value={ticket} onChange={setTicket} hint="UUID phiếu cân (tuỳ chọn)" />
            <TextField label="Cước thực nhập tay (VND)" value={cost} onChange={setCost} inputMode="decimal" hint="Tuỳ chọn — cần lý do / số chứng từ" />
          </div>
          <div className="grid2">
            {cost ? <TextField label="Lý do / chứng từ cước tay" value={costReason} onChange={setCostReason} /> : null}
            {t.status === "completed" ? <TextField label="Lý do bù" value={correction} onChange={setCorrection} hint="Bắt buộc với bản bù (≥ 10 ký tự)" /> : null}
          </div>
          <div className="row">
            <button
              type="button"
              className="btn btn-p btn-sm"
              disabled={act.busy || !isKg(actualKg.trim()) || (!!cost && (!isMoney(cost.trim()) || !validReason(costReason))) || (t.status === "completed" && !validReason(correction)) || (!!ticket && !UUID.test(ticket.trim()))}
              onClick={() =>
                void run(
                  () =>
                    unwrap(
                      api.POST("/v1/logistics/trips/{id}/completion", {
                        params: p(),
                        body: {
                          actual_kg: actualKg.trim(),
                          ...(ticket.trim() ? { ticket_ref: ticket.trim() } : {}),
                          ...(cost.trim() ? { actual_cost: cost.trim(), reason: costReason.trim() } : {}),
                          ...(t.status === "completed" ? { correction_reason: correction.trim() } : {}),
                        },
                      }),
                    ),
                  t.status === "completed" ? "Đã ghi bản bù hoàn tất — bản gốc giữ nguyên" : "Đã hoàn tất chuyến — phát mkg.logistics.trip.completed",
                )
              }
            >
              {t.status === "completed" ? "Ghi bản bù" : "Hoàn tất chuyến"}
            </button>
          </div>
        </div>
      ) : null}

      {disp && (t.status === "planned" || t.status === "departed") ? (
        <div className="panel row">
          <TextField label="Lý do huỷ chuyến" value={cancelReason} onChange={setCancelReason} hint="Ít nhất 10 ký tự — huỷ là sự kiện, không xoá" />
          <button type="button" className="btn btn-d btn-sm" disabled={act.busy || !validReason(cancelReason)} onClick={() => void run(() => unwrap(api.POST("/v1/logistics/trips/{id}/cancel", { params: p(), body: { reason: cancelReason.trim() } })), "Đã huỷ chuyến (sự kiện cancelled, có lý do)")}>
            Huỷ chuyến
          </button>
        </div>
      ) : null}

      {disp && events.data?.length ? (
        <>
          <h3>Sổ chuyến (kiểm toán)</h3>
          <ol className="list-plain small" aria-label="Sổ chuyến">
            {events.data.map((e) => (
              <li key={e.event_id}>
                #{e.seq} <When at={e.event_time} /> · <b>{TRIP_EVENT_LABEL[e.kind] ?? e.kind}</b> · <Ref id={e.actor_ref} />
                {e.actual_kg ? ` · ${formatKg(e.actual_kg)}` : ""}
                {e.actual_cost ? <> · <Money v={e.actual_cost} /></> : null}
                {e.supersedes_id ? <> · bù cho <Ref id={e.supersedes_id} /> — {e.correction_reason}</> : null}
                {e.reason ? ` — ${e.reason}` : ""}
              </li>
            ))}
          </ol>
        </>
      ) : null}
    </div>
  );
}

function CreateTrip({ vessels, onCreated }: { vessels: LogisticsVessel[]; onCreated: (id: string) => void }) {
  const { api } = useAuth();
  const act = useAction();
  const [mode, setMode] = useState<Mode>("road");
  const [lot, setLot] = useState("");
  const [kg, setKg] = useState("");
  const [vehicle, setVehicle] = useState("");
  const err = !UUID.test(lot.trim()) ? "Nhập mã lô (UUID)" : !isKg(kg.trim()) || /^0+(\.0*)?$/.test(kg.trim()) ? "Khối lượng lô (kg) > 0" : null;
  return (
    <div className="stack">
      <div className="grid4">
        <SelectField label="Phương thức" value={mode} onChange={(m) => setMode(m as Mode)} options={[{ value: "road", label: "Đường bộ (mặc định — decideMode)" }, { value: "waterway", label: "Đường thuỷ" }]} />
        <TextField label="Mã lô trên chuyến" value={lot} onChange={setLot} hint="UUID lô (hub)" />
        <TextField label="Khối lượng lô (kg)" value={kg} onChange={setKg} inputMode="decimal" />
        <SelectField label="Phương tiện (tuỳ chọn)" value={vehicle} onChange={setVehicle} options={[{ value: "", label: "— Gán sau —" }, ...vessels.map((v) => ({ value: v.vessel_id, label: v.code }))]} />
      </div>
      {err ? <p className="small mute">{err}</p> : null}
      <ActionStatus a={act} />
      <div className="row">
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={!!err || act.busy}
          onClick={() =>
            void act
              .run(() => unwrap(api.POST("/v1/logistics/trips", { params: { header: idem() }, body: { mode, cargo: [{ lot_ref: lot.trim(), mass_kg: kg.trim() }], ...(vehicle ? { vehicle_ref: vehicle } : {}) } })), (r) => `Đã lập chuyến ${r.trip.code} (kế hoạch)${r.warnings.length ? ` — cảnh báo: ${r.warnings.map(flagLabel).join(", ")}` : ""}`)
              .then((r) => {
                if (r) onCreated(r.trip.trip_id);
              })
          }
        >
          Lập chuyến
        </button>
      </div>
    </div>
  );
}

export default function TripsPage() {
  const { api, principal } = useAuth();
  const disp = hasRole(principal, ROLES.dispatcher);
  const [status, setStatus] = useState("");
  const list = useAsync(async () => (await unwrap(api.GET("/v1/logistics/trips", { params: { query: { limit: 200, ...(status ? { status: status as (typeof STATUSES)[number] } : {}) } } }))).items, [api, status]);
  const vessels = useAsync(async () => (await unwrap(api.GET("/v1/logistics/vessels"))).items, [api]);
  const [sel, setSel] = useState("");
  const [creating, setCreating] = useState(false);
  const items: LogisticsTrip[] = list.data ?? [];
  const vs = vessels.data ?? [];
  const count = (s: string) => items.filter((t) => t.status === s).length;
  return (
    <div className="stack">
      <PageHead
        title="Chuyến & chặng"
        subtitle="Chuyến chỉ đi tiến (kế hoạch → xuất phát → đến → hoàn tất; huỷ có lý do). Cảnh báo ghe/đăng kiểm/quá tải không chặn. Kết quả định tuyến lưu nguyên văn + SHA-256."
        actions={
          disp ? (
            <button type="button" className="btn btn-p btn-sm" onClick={() => setCreating((v) => !v)}>
              Lập chuyến mới
            </button>
          ) : null
        }
      />
      {!status && list.data ? (
        <div className="grid4">
          <KpiTile label="Kế hoạch" value={String(count("planned"))} color="orange" icon="clock" />
          <KpiTile label="Đang chạy" value={String(count("departed") + count("arrived"))} color="blue" icon="sync" />
          <KpiTile label="Hoàn tất" value={String(count("completed"))} color="green" icon="check" />
          <KpiTile label="Có cờ cảnh báo" value={String(items.filter((t) => t.flags?.length).length)} color="red" icon="alert" />
        </div>
      ) : null}
      {creating && disp ? (
        <Section title="Lập chuyến (điều phối)">
          <CreateTrip
            vessels={vs}
            onCreated={(id) => {
              setCreating(false);
              setSel(id);
              list.reload();
            }}
          />
        </Section>
      ) : null}
      <Section title="Danh sách chuyến">
        <div className="stack">
          <SelectField label="Lọc trạng thái" value={status} onChange={setStatus} options={[{ value: "", label: "Tất cả" }, ...STATUSES.map((s) => ({ value: s, label: TRIP_STATUS_LABEL[s]! }))]} />
          {list.loading ? <Loading /> : null}
          {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
          {list.data && !items.length ? <Empty>Không có chuyến.</Empty> : null}
          {items.length ? (
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Chuyến">
                <thead>
                  <tr>
                    <th>Mã</th>
                    <th>Phương thức</th>
                    <th>Trạng thái</th>
                    <th>Phương tiện</th>
                    <th className="num">Tấn KH / thật</th>
                    <th className="num">Cước KH / thực</th>
                    <th>Cờ</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((t) => (
                    <tr key={t.trip_id} aria-selected={t.trip_id === sel}>
                      <td className="mono">{t.code}</td>
                      <td>{MODE_LABEL[t.mode]}</td>
                      <td>
                        <Chip status={t.status} label={TRIP_STATUS_LABEL[t.status]} />
                      </td>
                      <td>{vs.find((v) => v.vessel_id === t.vehicle_ref)?.code ?? <Ref id={t.vehicle_ref} />}</td>
                      <td className="num">
                        {formatTons(t.planned_kg)} / {formatTons(t.actual_kg)}
                      </td>
                      <td className="num">
                        <Money v={t.planned_cost} /> / <Money v={t.actual_cost} />
                      </td>
                      <td>
                        <Flags flags={t.flags} label={`Cờ ${t.code}`} />
                      </td>
                      <td>
                        <button type="button" className="btn btn-o btn-sm" onClick={() => setSel(t.trip_id)} aria-label={`Mở chuyến ${t.code}`}>
                          Mở
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
      {sel ? (
        <Section title="Chi tiết chuyến">
          <TripPanel key={sel} id={sel} vessels={vs} onChanged={list.reload} />
        </Section>
      ) : null}
    </div>
  );
}
