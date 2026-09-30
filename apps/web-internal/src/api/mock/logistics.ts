// Backend giả lập mkg/logistics — theo contracts/openapi/mkg-internal.yaml (tag logistics) và quy tắc
// g4-conventions §1.1 (proto FM-05, GH-01..03, decideMode, plant weighing). Dữ liệu tất định, trong bộ nhớ tab.
//
// Quy ước giả lập định tuyến (thay cho /v1/partner/waterway/route của nền tảng): chặng đường thuỷ tới
// `customer_site` → không có đường thuỷ (`no_waterway_route`, dự phòng Haversine); còn lại → định tuyến thành công,
// phản hồi NGUYÊN VĂN + SHA-256 lưu vào ảnh chụp (L-03). Đường bộ = Haversine × 1,3 (#10).
import type { LogisticsLeg, LogisticsTrip, LogisticsTripEvent, LogisticsVessel } from "../types";
import { grams, kgOf } from "@/lib/kg";
import { normalizeVesselCode } from "@/lib/g4labels";
import { cents, divRound, moneyOf, priceUnits } from "@/lib/money";
import { sha256Text, textToBase64 } from "@/lib/sha256";
import { F, L, O, newId, sid } from "./db";
import { decimalCheck, paramStore, reasonOf, todayVn } from "./g4common";
import { created, daysAgo, forbidden, hasRole, json, nowIso, pageSlice, problem, route, str, validation, UUID_RE, type Route } from "./http";
import { ACTORS } from "./personas";

const DISPATCHER = "dispatcher";
const DRIVER = "driver";
const CHIEF = "chief_accountant";

export const LG = {
  tripFm05: sid(0x20, 1),
  tripDeparted: sid(0x20, 2),
  tripCompleted: sid(0x20, 3),
  tripCancelled: sid(0x20, 4),
  tripArrived: sid(0x20, 5),
  vesselOk: sid(0x21, 1),
  vesselExpiring: sid(0x21, 2),
  vesselExpired: sid(0x21, 3),
  truck: sid(0x21, 4),
  vesselInactive: sid(0x21, 5),
} as const;

export const params = paramStore({
  prefix: "/v1/logistics",
  module: "logistics",
  shape: "logistics",
  proposerRoles: [DISPATCHER],
  approverRoles: [CHIEF],
  readRoles: [DISPATCHER, CHIEF],
  defaults: {
    "logistics.rate.road_vnd_per_t_km": { value: "2200", note: "proto #22 (1 800–2 900) — tạm, chờ khách", check: decimalCheck(4, 100_000) },
    "logistics.rate.waterway_vnd_per_t_km": { value: "900", note: "proto #23 (650–1 250) — tạm, chờ khách", check: decimalCheck(4, 100_000) },
    "logistics.mode.road_feasible_km": { value: "120", note: "proto #27 — chờ khách", check: decimalCheck(3, 5_000) },
    "logistics.vessel.straw_payload_tons": { value: "90", note: "proto #51 (50–140) — tạm, chờ khách", check: decimalCheck(3, 5_000) },
    "logistics.vessel.payload_warn_factor": { value: "1.1", note: "proto FM-05", check: decimalCheck(3, 10) },
    "logistics.vessel.expiry_warn_days": { value: "30", note: "proto GH-02", check: decimalCheck(0, 365, true) },
    "logistics.trip.unweighed_alert_hours": { value: "48", note: "proto nhắc ghe chưa cân", check: decimalCheck(0, 720, true) },
    "logistics.route.road_haversine_factor": { value: "1.3", note: "proto #10", check: decimalCheck(3, 10) },
  },
});

const pv = (key: string) => params.effective(key);

// ---------------------------------------------------------------------------
// Danh mục phương tiện (GH-01/02)
// ---------------------------------------------------------------------------
interface VesselRec {
  v: LogisticsVessel;
}
const vessels: VesselRec[] = [];

function regStatus(expiry: string | null | undefined): Pick<LogisticsVessel, "registration_status" | "registration_days_left" | "warning" | "warn_param_version"> {
  const warn = pv("logistics.vessel.expiry_warn_days");
  if (!expiry) return { registration_status: "unknown", registration_days_left: null, warning: null, warn_param_version: warn.version };
  const days = Math.round((Date.parse(expiry) - Date.parse(todayVn())) / 86_400_000);
  if (days < 0) return { registration_status: "expired", registration_days_left: days, warning: "registration_expired", warn_param_version: warn.version };
  if (days <= Number(warn.value)) return { registration_status: "expiring", registration_days_left: days, warning: "registration_expiring", warn_param_version: warn.version };
  return { registration_status: "valid", registration_days_left: days, warning: null, warn_param_version: warn.version };
}

function vessel(id: string, code: string, kind: LogisticsVessel["kind"], name: string, reg: string | null, payload: string | null, expiryDays: number | null, rateType: string | null, rate: string | null, status: "active" | "inactive" = "active") {
  const expiry = expiryDays === null ? null : todayVn(expiryDays);
  vessels.push({
    v: { vessel_id: id, version: 1, code, kind, name, owner_ref: sid(0x29, vessels.length + 1), registered_tons: reg, straw_payload_tons: payload, registration_expiry: expiry, rate_type: rateType, rate_vnd: rate, status, ...regStatus(expiry), updated_at: daysAgo(40) },
  });
}
vessel(LG.vesselOk, "TG-12345", "ghe", "Ghe Tân Hiệp 1", "120", null, 200, "per_ton", "85000", "active");
vessel(LG.vesselExpiring, "CT-06789", "sa_lan", "Sà lan Cần Thơ 6", "180", "140", 10, "per_ton_km", "800", "active");
vessel(LG.vesselExpired, "VL-00111", "ghe", "Ghe Vĩnh Long 11", "80", "60", -5, null, null, "active");
vessel(LG.truck, "65C-12345", "truck", "Xe tải 15 t", "15", "15", null, null, null, "active");
vessel(LG.vesselInactive, "KG-09999", "ghe", "Ghe Kiên Giang 99 (ngừng)", "90", null, 90, null, null, "inactive");

function currentVessel(id: string | null | undefined): LogisticsVessel | undefined {
  const r = vessels.find((x) => x.v.vessel_id === id);
  if (!r) return undefined;
  Object.assign(r.v, regStatus(r.v.registration_expiry));
  return r.v;
}

function vesselInput(body: Record<string, unknown>, revising: boolean): Response | Partial<LogisticsVessel> {
  const code = normalizeVesselCode(str(body, "code"));
  const kind = str(body, "kind");
  const errs: { field: string; reason: string }[] = [];
  if (!code) errs.push({ field: "code", reason: "Bắt buộc" });
  if (!["ghe", "sa_lan", "truck"].includes(kind)) errs.push({ field: "kind", reason: "ghe | sa_lan | truck" });
  const dec = (f: string) => {
    const v = str(body, f);
    if (!v) return null;
    if (!/^\d+(\.\d{1,3})?$/.test(v) || /^0+(\.0*)?$/.test(v)) errs.push({ field: f, reason: "> 0, tối đa 3 số lẻ" });
    return v;
  };
  const reg = dec("registered_tons");
  const payload = dec("straw_payload_tons");
  const rateType = str(body, "rate_type");
  const rate = str(body, "rate_vnd") || null;
  if (rateType && !["per_ton", "per_trip", "per_ton_km"].includes(rateType)) errs.push({ field: "rate_type", reason: "per_ton | per_trip | per_ton_km" });
  if (rateType && (!rate || !/^\d+(\.\d{1,4})?$/.test(rate) || priceUnits(rate) <= 0n)) errs.push({ field: "rate_vnd", reason: "> 0 khi có loại đơn giá (GH-01)" });
  const expiry = str(body, "registration_expiry") || null;
  if (expiry && !/^\d{4}-\d{2}-\d{2}$/.test(expiry)) errs.push({ field: "registration_expiry", reason: "YYYY-MM-DD" });
  if (revising && str(body, "reason").length < 10) errs.push({ field: "reason", reason: "Sửa phương tiện cần lý do ≥ 10 ký tự" });
  if (errs.length) return validation(errs);
  const status = str(body, "status") === "inactive" ? "inactive" : "active";
  return { code, kind: kind as LogisticsVessel["kind"], name: str(body, "name") || null, owner_ref: str(body, "owner_ref") || null, registered_tons: reg, straw_payload_tons: payload, registration_expiry: expiry, rate_type: rateType || null, rate_vnd: rateType ? rate : null, status };
}

// ---------------------------------------------------------------------------
// Chuyến, chặng, ảnh chụp định tuyến
// ---------------------------------------------------------------------------
interface SnapshotRec {
  snapshot_id: string;
  trip_id: string;
  leg_id: string;
  provider: "pf_partner_waterway" | "road_haversine";
  request: Record<string, never>;
  raw_base64: string;
  raw_sha256: string;
  grantor_tenant: string | null;
  network_version: number | null;
  fetched_at: string;
  recorded_at: string;
  rule_version: string;
}
interface TripRec {
  trip: LogisticsTrip;
  events: LogisticsTripEvent[];
}
const trips: TripRec[] = [];
const snapshots: SnapshotRec[] = [];
let tripSeq = 100;

const GRANTOR = "0192f000-0000-7000-8000-00000000e001";

function ev(t: LogisticsTrip, kind: LogisticsTripEvent["kind"], actor: string, at: string, extra: Partial<LogisticsTripEvent> = {}): LogisticsTripEvent {
  const rec = trips.find((x) => x.trip.trip_id === t.trip_id);
  const e: LogisticsTripEvent = { event_id: newId(0x22), kind, status: t.status, seq: (rec?.events.length ?? 0) + 1, event_time: at, recorded_at: at, actor_ref: actor, supersedes_id: null, correction_reason: null, reason: null, actual_kg: null, actual_cost: null, cost_source: null, param_version: null, lat: null, lng: null, source_ref: null, flags: [...(t.flags ?? [])], rule_version: "logistics.trip.v1", ...extra };
  rec?.events.push(e);
  return e;
}

/** Haversine (km) — số đo khoảng cách, không phải tiền; kết quả làm tròn 3 số lẻ thành chuỗi. */
function haversineKm(a: { lat: string; lng: string }, b: { lat: string; lng: string }, factor: string): string {
  const R = 6371;
  const rad = (d: string) => (Number(d) * Math.PI) / 180;
  const dLat = rad(b.lat) - rad(a.lat);
  const dLng = rad(b.lng) - rad(a.lng);
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(rad(a.lat)) * Math.cos(rad(b.lat)) * Math.sin(dLng / 2) ** 2;
  return (2 * R * Math.asin(Math.sqrt(h)) * Number(factor)).toFixed(3);
}

/** Cước = tấn × km × đơn giá đ/t·km, làm tròn 2 số (BigInt: gam × mét × đơn vị giá / 10^11 xu). */
export function legCost(kg: string, km: string, rate: string): string {
  const meters = grams(km); // km 3 số lẻ → "mét"
  return moneyOf(divRound(grams(kg) * meters * priceUnits(rate), 100_000_000_000n));
}

interface EndpointIn {
  kind: string;
  ref?: string | null;
  lat: string;
  lng: string;
}

function planLeg(t: LogisticsTrip, seq: number, revision: number, mode: "road" | "waterway", from: EndpointIn, to: EndpointIn, manualKm: string | null, manualReason: string | null): LogisticsLeg {
  const legId = newId(0x23);
  const flags: string[] = [];
  const factor = pv("logistics.route.road_haversine_factor");
  let km: string;
  let source: LogisticsLeg["route_source"];
  let snapshot: string | null = null;
  let network: number | null = null;
  if (manualKm) {
    km = kgOf(grams(manualKm));
    source = "manual";
  } else if (mode === "waterway" && to.kind !== "customer_site") {
    const raw = JSON.stringify({ distance_km: haversineKm(from, to, "1.15"), network_version: 7, min_load_tons: "0", path: [[from.lng, from.lat], [to.lng, to.lat]], edges: 14 });
    km = haversineKm(from, to, "1.15");
    source = "routed";
    network = 7;
    const s: SnapshotRec = {
      snapshot_id: newId(0x24),
      trip_id: t.trip_id,
      leg_id: legId,
      provider: "pf_partner_waterway",
      request: { from: `${from.lat},${from.lng}`, to: `${to.lat},${to.lng}`, grantor: GRANTOR } as unknown as Record<string, never>,
      raw_base64: textToBase64(raw),
      raw_sha256: sha256Text(raw),
      grantor_tenant: GRANTOR,
      network_version: 7,
      fetched_at: nowIso(),
      recorded_at: nowIso(),
      rule_version: "logistics.route.v1",
    };
    snapshots.push(s);
    snapshot = s.snapshot_id;
  } else {
    km = haversineKm(from, to, factor.value);
    source = "haversine";
    if (mode === "waterway") flags.push("no_waterway_route");
    if (mode === "road" && grams(km) > grams(pv("logistics.mode.road_feasible_km").value)) flags.push("waterway_suggested");
  }
  const rateKey = mode === "road" ? "logistics.rate.road_vnd_per_t_km" : "logistics.rate.waterway_vnd_per_t_km";
  const rate = pv(rateKey);
  const kg = t.planned_kg ?? "0.000";
  return {
    leg_id: legId,
    seq,
    plan_revision: revision,
    mode,
    from: { kind: from.kind, ref: from.ref ?? null, lat: from.lat, lng: from.lng },
    to: { kind: to.kind, ref: to.ref ?? null, lat: to.lat, lng: to.lng },
    distance_km: km,
    route_source: source,
    route_snapshot_ref: snapshot,
    network_version: network,
    manual_reason: manualReason,
    mass_kg: kg,
    rate_vnd_per_t_km: rate.value,
    planned_cost: legCost(kg, km, rate.value),
    param_version: rate.version,
    flags,
    rule_version: "logistics.leg.v1",
  };
}

function tripFlags(t: LogisticsTrip): string[] {
  const legFlags = (t.legs ?? []).flatMap((l) => l.flags ?? []);
  const base = (t.flags ?? []).filter((f) => !["no_waterway_route", "waterway_suggested", "routing_unavailable", "no_route"].includes(f));
  if (!t.legs?.length && !base.includes("no_route") && t.status !== "completed" && t.status !== "cancelled") base.push("no_route");
  return Array.from(new Set([...base, ...legFlags]));
}

function vehicleWarnings(t: LogisticsTrip, vehicleRef: string): string[] {
  const v = currentVessel(vehicleRef);
  if (!v) return ["vessel_unregistered"];
  const w: string[] = [];
  if (v.status === "inactive") w.push("vessel_inactive");
  if (v.registration_status === "expired") w.push("registration_expired");
  if (v.registration_status === "expiring") w.push("registration_expiring");
  const payloadT = v.straw_payload_tons ?? (v.kind === "truck" ? null : pv("logistics.vessel.straw_payload_tons").value);
  if (payloadT && t.planned_kg) {
    // tấn ước > hệ số × tải: so bằng gam (kg × 1000 so với tấn × 1000 × 1000 × hệ số)
    const limitG = divRound(grams(payloadT) * 1000n * grams(pv("logistics.vessel.payload_warn_factor").value), 1000n);
    if (grams(t.planned_kg) > limitG) w.push("payload_exceeded");
  }
  return w;
}

function seedTrip(id: string, status: LogisticsTrip["status"], mode: "road" | "waterway", plannedKg: string, hoursAgo: number, extra: Partial<LogisticsTrip> = {}, legs: [("road" | "waterway"), EndpointIn, EndpointIn, string | null][] = []) {
  tripSeq += 1;
  const at = daysAgo(0, hoursAgo);
  const t: LogisticsTrip = {
    trip_id: id,
    code: `CH${String(tripSeq).padStart(6, "0")}`,
    status: "planned",
    mode,
    vehicle_ref: null,
    driver_ref: null,
    collection_order_ref: null,
    delivery_ref: null,
    cargo: [],
    planned_kg: plannedKg,
    actual_kg: null,
    planned_cost: null,
    actual_cost: null,
    cost_source: null,
    plan_revision: legs.length ? 1 : 0,
    planned_at: at,
    departed_at: null,
    arrived_at: null,
    completed_at: null,
    cancelled_at: null,
    cancel_reason: null,
    ticket_ref: null,
    source_ref: null,
    flags: [],
    rule_version: "logistics.trip.v1",
    recorded_at: at,
    legs: [],
    ...extra,
  };
  trips.push({ trip: t, events: [] });
  ev(t, "planned", extra.source_ref ? ACTORS.system : ACTORS.vanTai, at, { source_ref: extra.source_ref ?? null });
  t.legs = legs.map(([m, a, b, manual], i) => planLeg(t, i + 1, 1, m, a, b, manual, manual ? "Kênh cạn mùa khô, đo theo đồng hồ ghe" : null));
  t.planned_cost = t.legs.length ? addLegCosts(t.legs) : null;
  if (t.vehicle_ref) ev(t, "vehicle_assigned", ACTORS.vanTai, at);
  t.status = status;
  t.flags = tripFlags(t);
}

function addLegCosts(legs: LogisticsLeg[]): string {
  return moneyOf(legs.reduce((s, l) => s + cents(l.planned_cost), 0n));
}

const P = {
  bai: { kind: "collection_point", ref: null, lat: "10.0452", lng: "105.2134" },
  hubTb: { kind: "facility", ref: F.tb, lat: "10.2710", lng: "105.3561" },
  hubCt: { kind: "facility", ref: F.ct, lat: "10.0341", lng: "105.7880" },
  plant: { kind: "customer_site", ref: sid(0x29, 20), lat: "10.3802", lng: "106.1025" },
} satisfies Record<string, EndpointIn>;

// proto FM-05: lượt xuống ghe 4 400 cuộn → chuyến đường thuỷ planned, tấn ước 88 t, chưa gán ghe, xuống ghe 60 giờ trước.
seedTrip(LG.tripFm05, "planned", "waterway", "88000.000", 60, { collection_order_ref: O.assigned, source_ref: sid(0x0a, 3), cargo: [{ lot_ref: L.arriving, mass_kg: "88000.000", ticket_ref: null }], flags: ["no_vehicle"] });
seedTrip(LG.tripDeparted, "departed", "waterway", "90000.000", 30, { vehicle_ref: LG.vesselOk, driver_ref: sid(0x29, 11), departed_at: daysAgo(0, 20), cargo: [{ lot_ref: L.ct, mass_kg: "45000.000", ticket_ref: null }, { lot_ref: L.perBale, mass_kg: "45000.000", ticket_ref: null }] }, [["waterway", P.bai, P.hubCt, null]]);
seedTrip(LG.tripCompleted, "completed", "road", "15000.000", 200, { vehicle_ref: LG.truck, delivery_ref: sid(0x28, 1), departed_at: daysAgo(8), arrived_at: daysAgo(8), completed_at: daysAgo(8), actual_kg: "14800.000", cost_source: "param_rate", ticket_ref: sid(0x0b, 90), cargo: [{ lot_ref: L.compressed, mass_kg: "15000.000", ticket_ref: sid(0x0b, 90) }] }, [["road", P.hubTb, P.plant, null]]);
seedTrip(LG.tripCancelled, "cancelled", "road", "12000.000", 120, { cancelled_at: daysAgo(4), cancel_reason: "Khách lùi lịch nhận hàng sang tuần sau" });
seedTrip(LG.tripArrived, "arrived", "waterway", "130000.000", 34, { vehicle_ref: LG.vesselExpiring, departed_at: daysAgo(0, 30), arrived_at: daysAgo(0, 2), cargo: [{ lot_ref: L.dried, mass_kg: "130000.000", ticket_ref: null }], flags: ["registration_expiring"] }, [["road", P.bai, P.hubTb, "5.200"], ["waterway", P.hubTb, P.hubCt, null]]);
{
  const done = trips.find((x) => x.trip.trip_id === LG.tripCompleted)!.trip;
  done.actual_cost = legCost("14800.000", done.legs![0]!.distance_km, "2200");
}

function tripOr404(id: string): TripRec | Response {
  return trips.find((x) => x.trip.trip_id === id) ?? problem(404, "logistics.trip_not_found", "Trip not found");
}

function result(t: LogisticsTrip, warnings: string[], event?: LogisticsTripEvent) {
  return created({ trip: t, warnings, ...(event ? { event } : {}) });
}

function closed(t: LogisticsTrip): Response | null {
  return t.status === "completed" || t.status === "cancelled" ? problem(409, "logistics.trip_closed", "Trip closed") : null;
}

function isCoord(s: string, max: number): boolean {
  return /^-?\d{1,3}(\.\d{1,7})?$/.test(s) && Math.abs(Number(s)) <= max;
}

function endpoint(v: unknown, field: string, errs: { field: string; reason: string }[]): EndpointIn {
  const o = (typeof v === "object" && v !== null ? v : {}) as Record<string, unknown>;
  const kind = typeof o.kind === "string" ? o.kind : "";
  const lat = typeof o.lat === "string" ? o.lat.trim() : "";
  const lng = typeof o.lng === "string" ? o.lng.trim() : "";
  if (!["plot", "collection_point", "facility", "customer_site", "point"].includes(kind)) errs.push({ field: `${field}.kind`, reason: "Loại điểm không hợp lệ" });
  if (!isCoord(lat, 90)) errs.push({ field: `${field}.lat`, reason: "Vĩ độ WGS84 (chuỗi thập phân)" });
  if (!isCoord(lng, 180)) errs.push({ field: `${field}.lng`, reason: "Kinh độ WGS84 (chuỗi thập phân)" });
  return { kind, ref: typeof o.ref === "string" && o.ref ? o.ref : null, lat, lng };
}

/** Cước thực khi hoàn tất (proto completeTrip, GH-03). */
function completionCost(t: LogisticsTrip, actualKg: string): { cost: string; source: "param_rate" | "vessel_rate"; version: string | null } {
  const v = currentVessel(t.vehicle_ref);
  const km = (t.legs ?? []).reduce((s, l) => s + grams(l.distance_km), 0n);
  if (v?.rate_type && v.rate_vnd) {
    if (v.rate_type === "per_trip") return { cost: moneyOf(priceUnits(v.rate_vnd) / 100n), source: "vessel_rate", version: null };
    if (v.rate_type === "per_ton") return { cost: moneyOf(divRound(grams(actualKg) * priceUnits(v.rate_vnd), 100_000_000n)), source: "vessel_rate", version: null };
    return { cost: legCost(actualKg, kgOf(km), v.rate_vnd), source: "vessel_rate", version: null };
  }
  const total = (t.legs ?? []).reduce((s, l) => s + cents(legCost(actualKg, l.distance_km, l.rate_vnd_per_t_km)), 0n);
  return { cost: moneyOf(total), source: "param_rate", version: t.legs?.[0]?.param_version ?? null };
}

export const logisticsRoutes: Route[] = [
  route("GET", "/v1/logistics/trips", ({ url, persona }) => {
    if (!hasRole(persona, DISPATCHER, DRIVER)) return forbidden();
    const st = url.searchParams.get("status");
    if (st && !["planned", "departed", "arrived", "completed", "cancelled"].includes(st)) return validation([{ field: "status", reason: "Trạng thái lạ" }]);
    const list = trips.map((x) => x.trip).filter((t) => !st || t.status === st).sort((a, b) => b.planned_at.localeCompare(a.planned_at));
    return json(pageSlice(list, url));
  }),
  route("POST", "/v1/logistics/trips", ({ body, persona }) => {
    if (!hasRole(persona, DISPATCHER)) return forbidden();
    const mode = str(body, "mode");
    if (mode !== "road" && mode !== "waterway") return validation([{ field: "mode", reason: "road | waterway" }]);
    const cargoIn = Array.isArray(body.cargo) ? (body.cargo as { lot_ref?: string; mass_kg?: string }[]) : [];
    const errs: { field: string; reason: string }[] = [];
    const cargo = cargoIn.map((c, i) => {
      if (!UUID_RE.test(c.lot_ref ?? "")) errs.push({ field: `cargo[${i}].lot_ref`, reason: "UUID lô" });
      if (!/^\d+(\.\d{1,3})?$/.test(c.mass_kg ?? "") || grams(c.mass_kg ?? "0") <= 0n) errs.push({ field: `cargo[${i}].mass_kg`, reason: "kg > 0" });
      return { lot_ref: c.lot_ref ?? "", mass_kg: /^\d+(\.\d{1,3})?$/.test(c.mass_kg ?? "") ? kgOf(grams(c.mass_kg!)) : "0.000", ticket_ref: null };
    });
    const planned = str(body, "planned_kg");
    if (planned && (!/^\d+(\.\d{1,3})?$/.test(planned) || grams(planned) <= 0n)) errs.push({ field: "planned_kg", reason: "kg > 0" });
    if (!planned && !cargo.length) errs.push({ field: "planned_kg", reason: "Nhập tấn kế hoạch hoặc lô trên chuyến" });
    if (errs.length) return validation(errs);
    const at = str(body, "event_time") || nowIso();
    tripSeq += 1;
    const t: LogisticsTrip = {
      trip_id: newId(0x20),
      code: `CH${String(tripSeq).padStart(6, "0")}`,
      status: "planned",
      mode,
      vehicle_ref: str(body, "vehicle_ref") || null,
      driver_ref: str(body, "driver_ref") || null,
      collection_order_ref: str(body, "collection_order_ref") || null,
      delivery_ref: str(body, "delivery_ref") || null,
      cargo,
      planned_kg: planned ? kgOf(grams(planned)) : kgOf(cargo.reduce((s, c) => s + grams(c.mass_kg), 0n)),
      actual_kg: null,
      planned_cost: null,
      actual_cost: null,
      cost_source: null,
      plan_revision: 0,
      planned_at: at,
      departed_at: null,
      arrived_at: null,
      completed_at: null,
      cancelled_at: null,
      cancel_reason: null,
      ticket_ref: null,
      source_ref: null,
      flags: [],
      rule_version: "logistics.trip.v1",
      recorded_at: nowIso(),
      legs: [],
    };
    trips.push({ trip: t, events: [] });
    const warnings = t.vehicle_ref ? vehicleWarnings(t, t.vehicle_ref) : ["no_vehicle"];
    t.flags = tripFlags({ ...t, flags: warnings });
    return result(t, warnings, ev(t, "planned", persona.actor, at));
  }),
  route("GET", "/v1/logistics/trips/:id", ({ params: p, persona }) => {
    if (!hasRole(persona, DISPATCHER, DRIVER)) return forbidden();
    const r = tripOr404(p[0]!);
    return r instanceof Response ? r : json({ trip: r.trip });
  }),
  route("GET", "/v1/logistics/trips/:id/events", ({ params: p, persona }) => {
    if (!hasRole(persona, DISPATCHER)) return forbidden();
    const r = tripOr404(p[0]!);
    return r instanceof Response ? r : json({ items: r.events });
  }),
  route("POST", "/v1/logistics/trips/:id/vehicle", ({ params: p, body, persona }) => {
    if (!hasRole(persona, DISPATCHER)) return forbidden();
    const r = tripOr404(p[0]!);
    if (r instanceof Response) return r;
    const c = closed(r.trip);
    if (c) return c;
    if (r.trip.status !== "planned" && r.trip.status !== "departed") return problem(409, "logistics.trip_transition", "Only planned/departed");
    const vehicle = str(body, "vehicle_ref");
    if (!UUID_RE.test(vehicle)) return validation([{ field: "vehicle_ref", reason: "UUID phương tiện" }]);
    r.trip.vehicle_ref = vehicle;
    r.trip.driver_ref = str(body, "driver_ref") || r.trip.driver_ref || null;
    const warnings = vehicleWarnings(r.trip, vehicle);
    r.trip.flags = tripFlags({ ...r.trip, flags: [...(r.trip.flags ?? []).filter((f) => !["no_vehicle", "vessel_unregistered", "vessel_inactive", "registration_expired", "registration_expiring", "payload_exceeded"].includes(f)), ...warnings] });
    return result(r.trip, warnings, ev(r.trip, "vehicle_assigned", persona.actor, nowIso()));
  }),
  route("POST", "/v1/logistics/trips/:id/legs", ({ params: p, body, persona }) => {
    if (!hasRole(persona, DISPATCHER)) return forbidden();
    const r = tripOr404(p[0]!);
    if (r instanceof Response) return r;
    const c = closed(r.trip);
    if (c) return c;
    const legsIn = Array.isArray(body.legs) ? (body.legs as Record<string, unknown>[]) : [];
    if (!legsIn.length || legsIn.length > 24) return validation([{ field: "legs", reason: "1..24 chặng" }]);
    const errs: { field: string; reason: string }[] = [];
    const parsed = legsIn.map((l, i) => {
      const mode: "road" | "waterway" | null = l.mode === "road" || l.mode === "waterway" ? l.mode : null;
      if (!mode) errs.push({ field: `legs[${i}].mode`, reason: "road | waterway" });
      const from = endpoint(l.from, `legs[${i}].from`, errs);
      const to = endpoint(l.to, `legs[${i}].to`, errs);
      const manual = typeof l.manual_km === "string" && l.manual_km.trim() ? l.manual_km.trim() : null;
      if (manual && !/^\d+(\.\d{1,3})?$/.test(manual)) errs.push({ field: `legs[${i}].manual_km`, reason: "km, tối đa 3 số lẻ" });
      const reason = typeof l.manual_reason === "string" ? l.manual_reason.trim() : "";
      return { mode: mode ?? "road", from, to, manual, reason };
    });
    if (errs.length) return validation(errs);
    if (parsed.some((x) => x.manual && (x.reason.length < 10 || x.reason.length > 500))) return problem(422, "logistics.manual_km_reason_required", "Manual km requires reason");
    const rev = (r.trip.plan_revision ?? 0) + 1;
    r.trip.plan_revision = rev;
    r.trip.legs = parsed.map((x, i) => planLeg(r.trip, i + 1, rev, x.mode, x.from, x.to, x.manual, x.manual ? x.reason : null));
    r.trip.planned_cost = addLegCosts(r.trip.legs);
    r.trip.flags = tripFlags(r.trip);
    const warnings = r.trip.legs.flatMap((l) => l.flags ?? []);
    return result(r.trip, Array.from(new Set(warnings)), ev(r.trip, "revised", persona.actor, nowIso()));
  }),
  route("GET", "/v1/logistics/trips/:id/legs/:leg/route-snapshot", ({ params: p, persona }) => {
    if (!hasRole(persona, DISPATCHER, DRIVER)) return forbidden();
    const r = tripOr404(p[0]!);
    if (r instanceof Response) return r;
    const leg = r.trip.legs?.find((l) => l.leg_id === p[1]);
    const s = leg?.route_snapshot_ref ? snapshots.find((x) => x.snapshot_id === leg.route_snapshot_ref) : undefined;
    return s ? json(s) : problem(404, "logistics.route_snapshot_not_found", "No route snapshot");
  }),
  route("POST", "/v1/logistics/trips/:id/milestones", ({ params: p, body, persona }) => {
    if (!hasRole(persona, DRIVER, DISPATCHER)) return forbidden();
    const r = tripOr404(p[0]!);
    if (r instanceof Response) return r;
    const c = closed(r.trip);
    if (c) return c;
    const kind = str(body, "kind");
    if (kind !== "departed" && kind !== "arrived") return validation([{ field: "kind", reason: "departed | arrived" }]);
    const lat = str(body, "lat");
    const lng = str(body, "lng");
    const errs: { field: string; reason: string }[] = [];
    if (!isCoord(lat, 90)) errs.push({ field: "lat", reason: "Vĩ độ WGS84" });
    if (!isCoord(lng, 180)) errs.push({ field: "lng", reason: "Kinh độ WGS84" });
    const at = str(body, "event_time");
    if (!at) errs.push({ field: "event_time", reason: "Bắt buộc" });
    else if (Date.parse(at) > Date.now() + 60_000) errs.push({ field: "event_time", reason: "Không ở tương lai" });
    if (errs.length) return validation(errs);
    const want = r.trip.status === "planned" ? "departed" : r.trip.status === "departed" ? "arrived" : null;
    if (want !== kind) return problem(409, "logistics.trip_transition", "Milestone out of order");
    r.trip.status = kind;
    if (kind === "departed") r.trip.departed_at = at;
    else r.trip.arrived_at = at;
    return result(r.trip, [], ev(r.trip, kind, persona.actor, at, { lat, lng }));
  }),
  route("POST", "/v1/logistics/trips/:id/completion", ({ params: p, body, persona }) => {
    if (!hasRole(persona, DISPATCHER)) return forbidden();
    const r = tripOr404(p[0]!);
    if (r instanceof Response) return r;
    const t = r.trip;
    const actual = str(body, "actual_kg");
    if (!/^\d+(\.\d{1,3})?$/.test(actual) || grams(actual) <= 0n) return validation([{ field: "actual_kg", reason: "phải lớn hơn 0" }]);
    if (t.status === "cancelled") return problem(409, "logistics.trip_closed", "Trip cancelled");
    if (t.status === "planned") return problem(409, "logistics.trip_transition", "Trip not departed");
    const correction = str(body, "correction_reason");
    const prev = r.events.filter((e) => e.kind === "completed").at(-1);
    if (t.status === "completed" && (correction.length < 10 || correction.length > 500)) return problem(422, "logistics.correction_reason_required", "Correction reason required");
    const manualCost = str(body, "actual_cost");
    let cost: string;
    let source: "param_rate" | "vessel_rate" | "manual";
    let version: string | null = null;
    if (manualCost) {
      if (!/^\d+(\.\d{1,2})?$/.test(manualCost)) return validation([{ field: "actual_cost", reason: "Tiền VND, tối đa 2 số lẻ" }]);
      const reason = reasonOf(body);
      if (reason instanceof Response) return validation([{ field: "reason", reason: "Cước nhập tay cần lý do / số chứng từ ≥ 10 ký tự" }]);
      cost = moneyOf(cents(manualCost));
      source = "manual";
    } else {
      const c2 = completionCost(t, actual);
      cost = c2.cost;
      source = c2.source;
      version = c2.version;
    }
    const kg = kgOf(grams(actual));
    const flags = (t.flags ?? []).filter((f) => f !== "weigh_variance");
    if (t.planned_kg && grams(t.planned_kg) > 0n) {
      const diff = grams(kg) - grams(t.planned_kg);
      if ((diff < 0n ? -diff : diff) * 100n > grams(t.planned_kg) * 5n) flags.push("weigh_variance");
    }
    const at = str(body, "event_time") || nowIso();
    t.status = "completed";
    t.actual_kg = kg;
    t.actual_cost = cost;
    t.cost_source = source;
    t.completed_at = at;
    t.ticket_ref = str(body, "ticket_ref") || t.ticket_ref || null;
    t.flags = flags;
    const e = ev(t, "completed", persona.actor, at, { actual_kg: kg, actual_cost: cost, cost_source: source, param_version: version, supersedes_id: prev ? prev.event_id : null, correction_reason: prev ? correction : null, reason: manualCost ? str(body, "reason") : null });
    return result(t, flags.includes("weigh_variance") ? ["weigh_variance"] : [], e);
  }),
  route("POST", "/v1/logistics/trips/:id/cancel", ({ params: p, body, persona }) => {
    if (!hasRole(persona, DISPATCHER)) return forbidden();
    const r = tripOr404(p[0]!);
    if (r instanceof Response) return r;
    const reason = reasonOf(body);
    if (reason instanceof Response) return reason;
    if (r.trip.status !== "planned" && r.trip.status !== "departed") return problem(409, "logistics.trip_transition", "Only planned/departed trips can be cancelled");
    r.trip.status = "cancelled";
    r.trip.cancelled_at = nowIso();
    r.trip.cancel_reason = reason;
    return result(r.trip, [], ev(r.trip, "cancelled", persona.actor, nowIso(), { reason }));
  }),
  route("GET", "/v1/logistics/vessels", ({ url, persona }) => {
    if (!hasRole(persona, DISPATCHER, DRIVER)) return forbidden();
    const st = url.searchParams.get("status");
    return json({ items: vessels.map((x) => currentVessel(x.v.vessel_id)!).filter((v) => !st || v.status === st) });
  }),
  route("POST", "/v1/logistics/vessels", ({ body, persona }) => {
    if (!hasRole(persona, DISPATCHER)) return forbidden();
    const inp = vesselInput(body, false);
    if (inp instanceof Response) return inp;
    if (vessels.some((x) => x.v.code === inp.code)) return problem(409, "logistics.vessel_code_taken", "Vessel code taken");
    const v = { vessel_id: newId(0x21), version: 1, ...inp, ...regStatus(inp.registration_expiry), updated_at: nowIso() } as LogisticsVessel;
    vessels.push({ v });
    return created({ vessel: v });
  }),
  route("GET", "/v1/logistics/vessels/:id", ({ params: p, persona }) => {
    if (!hasRole(persona, DISPATCHER, DRIVER)) return forbidden();
    const v = currentVessel(p[0]);
    return v ? json({ vessel: v }) : problem(404, "logistics.vessel_not_found", "Vessel not found");
  }),
  route("POST", "/v1/logistics/vessels/:id/versions", ({ params: p, body, persona }) => {
    if (!hasRole(persona, DISPATCHER)) return forbidden();
    const r = vessels.find((x) => x.v.vessel_id === p[0]);
    if (!r) return problem(404, "logistics.vessel_not_found", "Vessel not found");
    const inp = vesselInput(body, true);
    if (inp instanceof Response) return inp;
    if (typeof body.expected_version === "number" && body.expected_version !== r.v.version) return problem(409, "logistics.vessel_version_stale", "Stale version");
    if (vessels.some((x) => x !== r && x.v.code === inp.code)) return problem(409, "logistics.vessel_code_taken", "Vessel code taken");
    r.v = { ...r.v, ...inp, version: r.v.version + 1, ...regStatus(inp.registration_expiry), updated_at: nowIso() } as LogisticsVessel;
    return created({ vessel: r.v });
  }),
  route("GET", "/v1/logistics/alerts", ({ persona }) => {
    if (!hasRole(persona, DISPATCHER)) return forbidden();
    const hours = pv("logistics.trip.unweighed_alert_hours");
    const limit = Date.now() - Number(hours.value) * 3_600_000;
    const unweighed = trips.map((x) => x.trip).filter((t) => t.mode === "waterway" && ["planned", "departed", "arrived"].includes(t.status) && Date.parse(t.planned_at) < limit);
    const vs = vessels.map((x) => currentVessel(x.v.vessel_id)!).filter((v) => v.status === "active" && (v.registration_status === "expired" || v.registration_status === "expiring"));
    return json({ unweighed_hours: Number(hours.value), unweighed_param_version: hours.version, unweighed_trips: unweighed, vessels: vs });
  }),
  ...params.routes,
];

export const logisticsAdminRoutes: Route[] = [
  route("POST", "/v1/logistics/projections/rebuild", ({ persona }) => (hasRole(persona, DISPATCHER) ? json({ trip_current: trips.length }) : forbidden())),
];
