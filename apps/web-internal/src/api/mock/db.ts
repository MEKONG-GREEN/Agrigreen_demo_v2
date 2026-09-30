// ============================================================================
// DỮ LIỆU GIẢ LẬP TẤT ĐỊNH (R-95) của vùng MKG — không phải dữ liệu thật.
// Số mẫu theo prototype mekong-green (docs/dev/g3-conventions.md §1.1): một lần xếp hàng
// 5 800 cuộn (cuộn) → 5 700 (tập kết) → ghe 4 400 cuộn cân 96,8 t, bị cờ +10 % so với ước 20 kg/cuộn;
// một hợp đồng theo tấn và một theo cuộn. Trạng thái nằm trong bộ nhớ tab (tải lại trang = trạng thái đầu).
// ============================================================================
import type {
  CollectionOrder,
  CrewShift,
  Dispute,
  DisputeEvent,
  Facility,
  FacilityVersion,
  HtxConfirmation,
  LossNormValues,
  Lot,
  MeasurementParam,
  Movement,
  OrderEvent,
  Reconciliation,
  RollCount,
  ScaleDevice,
  Stocktake,
  ToleranceValues,
  Transfer,
  ValveExport,
  WeighingSession,
  WeighTicket,
  BuySideReconciliation,
  CustomerImport,
} from "../types";
import { remember } from "@/lib/recent";
import { DEFAULT_LOSS_NORM, DEFAULT_TOLERANCE, reconcile, type PricingBasis } from "@/lib/reconcile";
import { DEFAULT_RISK_V2 } from "@/lib/rules";
import { daysAgo, minutesAgo, opaque } from "./http";
import { ACTORS } from "./personas";

/** UUIDv7 giả lập tất định theo nhóm + số thứ tự. */
export function sid(group: number, n: number): string {
  return `0192f${group.toString(16).padStart(3, "0")}-0000-7000-8000-${n.toString(16).padStart(12, "0")}`;
}

let seq = 1000;
/** UUIDv7 giả lập cho bản ghi mới (tăng dần trong tab). */
export function newId(group = 0xfff): string {
  seq += 1;
  return sid(group, seq);
}

// ---------------------------------------------------------------------------
// Cơ sở (mkg/hub)
// ---------------------------------------------------------------------------
export const F = { tb: sid(1, 1), ct: sid(1, 2), lv: sid(1, 3) } as const;

export interface FacilityRec {
  id: string;
  current: Facility | null;
  versions: FacilityVersion[];
}

function fac(id: string, code: string, name: string, kind: "hub" | "warehouse" | "yard", prov: string, lat: number, lng: number, cap: string, days: number): FacilityRec {
  const vid = sid(2, Number.parseInt(id.slice(-2), 16) * 10 + 1);
  const f: Facility = {
    id,
    code,
    name,
    kind,
    parent_facility_id: null,
    location: { lat, lng },
    province_code: prov,
    status: "active",
    capacity_kg: cap,
    capacity_tons: (Number(cap) / 1000).toFixed(3),
    effective_from: daysAgo(days),
    version: 1,
    version_id: vid,
    proposed_by: ACTORS.dieuPhoi,
    approved_by: ACTORS.pheDuyet,
    approved_at: daysAgo(days),
  };
  const v: FacilityVersion = {
    id: vid,
    facility_id: id,
    version: 1,
    state: "approved",
    supersedes_id: null,
    change_reason: null,
    proposed_by: ACTORS.dieuPhoi,
    decided_by: ACTORS.pheDuyet,
    decided_at: daysAgo(days),
    code,
    name,
    kind,
    capacity_kg: cap,
    status: "active",
    province_code: prov,
    effective_from: daysAgo(days),
    location: { lat, lng },
    event_time: daysAgo(days + 1),
  };
  return { id, current: f, versions: [v] };
}

export const facilities: FacilityRec[] = [
  fac(F.tb, "HUB-TB", "Hub Thới Bình", "hub", "96", 9.35, 105.09, "500000.000", 120),
  fac(F.ct, "KHO-CT", "Kho trung chuyển Cần Thơ", "warehouse", "92", 10.03, 105.78, "300000.000", 90),
  fac(F.lv, "BAI-LV", "Bãi tập kết Lấp Vò", "yard", "82", 10.36, 105.52, "150000.000", 60),
];
// Phiên bản 2 chờ duyệt: mở rộng sức chứa Hub Thới Bình (người đề xuất = Điều phối viên A).
facilities[0]!.versions.unshift({
  id: sid(2, 902),
  facility_id: F.tb,
  version: 2,
  state: "pending",
  supersedes_id: facilities[0]!.versions[0]!.id ?? null,
  change_reason: "Mở rộng kho số 2 thêm 100 tấn sức chứa",
  proposed_by: ACTORS.dieuPhoi,
  decided_by: null,
  decided_at: null,
  code: "HUB-TB",
  name: "Hub Thới Bình",
  kind: "hub",
  capacity_kg: "600000.000",
  status: "active",
  province_code: "96",
  effective_from: daysAgo(-1),
  location: { lat: 9.35, lng: 105.09 },
  event_time: daysAgo(1),
});

// ---------------------------------------------------------------------------
// Lô, dịch chuyển (mkg/hub)
// ---------------------------------------------------------------------------
export const L = {
  proto: sid(3, 1), // ghe 4 400 cuộn, 96,8 t — đã xuất hết, đủ 3 điểm cân
  dispute: sid(3, 2),
  arriving: sid(3, 3),
  dried: sid(3, 4),
  compressed: sid(3, 5),
  ct: sid(3, 6),
  perBale: sid(3, 7),
  abnormal: sid(3, 8),
  critical: sid(3, 9),
  direct: sid(3, 10),
} as const;

export interface LotRec {
  lot: Lot;
  movements: Movement[];
}

export const lots: LotRec[] = [];

function mv(lotId: string, kind: Movement["kind"], qty: string, bal: string, at: string, ref?: [string, string]): Movement {
  return {
    id: newId(4),
    lot_id: lotId,
    kind,
    qty_kg: qty,
    balance_after_kg: bal,
    ref_type: ref?.[0] ?? null,
    ref_id: ref?.[1] ?? null,
    supersedes_id: null,
    correction_reason: null,
    event_time: at,
    recorded_at: at,
  };
}

function lot(id: string, facility: string, code: string, product: Lot["product"], initial: string, remaining: string, moisture: string | null, days: number, sources: Lot["sources"]): void {
  const at = daysAgo(days);
  const movements = [mv(id, "receive", initial, initial, at)];
  if (remaining !== initial) {
    const out = (Number(initial) - Number(remaining)).toFixed(3);
    movements.push(mv(id, "dispatch", `-${out}`, remaining, daysAgo(Math.max(days - 3, 0)), ["dispatch", newId(5)]));
  }
  lots.push({
    lot: { id, facility_id: facility, code, product, origin: "receipt", batch_id: null, initial_kg: initial, moisture_pct: moisture, sources, event_time: at, recorded_at: at },
    movements,
  });
  remember("lot", id, code);
}

export const O = { done: sid(6, 1), assigned: sid(6, 2), approved: sid(6, 3), planned: sid(6, 4), cancelled: sid(6, 5) } as const;
export const HTX = { vinhBinh: sid(7, 1), tanHiep: sid(7, 2), giongRieng: sid(7, 3) } as const;
export const PLOT = { a1: sid(8, 1), a2: sid(8, 2), b1: sid(8, 3), c1: sid(8, 4), c2: sid(8, 5), b2: sid(8, 6) } as const;
export const CREW = { t1: sid(9, 1), t2: sid(9, 2) } as const;

lot(L.proto, F.tb, "LO-TB-0921", "straw_bale", "96800.000", "0.000", "17.50", 9, [{ kind: "roll_count", ref: O.done, ratio: "1.000000", mass_kg: "96800.000" }]);
lot(L.dispute, F.tb, "LO-TB-0918", "straw_bale", "91500.000", "0.000", "16.00", 12, [{ kind: "cooperative", ref: HTX.giongRieng, ratio: "1.000000" }]);
lot(L.arriving, F.tb, "LO-TB-0927", "straw_bale", "84200.000", "64200.000", "21.00", 3, [
  { kind: "cooperative", ref: HTX.tanHiep, ratio: "0.600000" },
  { kind: "cooperative", ref: HTX.vinhBinh, ratio: "0.400000" },
]);
lot(L.dried, F.tb, "LO-TB-0912", "dried_straw", "30500.000", "30500.000", "13.00", 18, [{ kind: "lot", ref: L.dispute, ratio: "1.000000" }]);
lot(L.compressed, F.tb, "LO-TB-0901", "compressed_bale", "91500.000", "12000.000", "15.00", 29, [{ kind: "supplier", ref: sid(7, 9), ratio: "1.000000" }]);
lot(L.ct, F.ct, "LO-CT-0918", "straw_bale", "45000.000", "45000.000", "19.50", 12, [{ kind: "cooperative", ref: HTX.vinhBinh, ratio: "1.000000" }]);
lot(L.perBale, F.ct, "LO-CT-0905", "straw_bale", "50000.000", "0.000", "22.00", 25, [{ kind: "cooperative", ref: HTX.tanHiep, ratio: "1.000000" }]);
lot(L.abnormal, F.tb, "LO-TB-0908", "straw_bale", "60000.000", "0.000", "14.00", 21, [{ kind: "cooperative", ref: HTX.vinhBinh, ratio: "1.000000" }]);
lot(L.critical, F.ct, "LO-CT-0910", "straw_bale", "70500.000", "0.000", "18.50", 20, [{ kind: "cooperative", ref: HTX.giongRieng, ratio: "1.000000" }]);
remember("lot", L.direct, "LO-DIRECT-0925 (giao thẳng)");

export function findLot(id: string): LotRec | undefined {
  return lots.find((l) => l.lot.id === id);
}

export function remainingOf(r: LotRec): string {
  return r.movements[r.movements.length - 1]?.balance_after_kg ?? "0.000";
}

// ---------------------------------------------------------------------------
// Tham số hub (hub.risk.v2 / hub.weigh.v1) — phiên bản có duyệt
// ---------------------------------------------------------------------------
export interface HubParamRec {
  id: string;
  param_set: "risk" | "weigh";
  version: number;
  rule_version: "hub.risk.v2" | "hub.weigh.v1";
  params: Record<string, unknown>;
  reason: string;
  effective_from: string;
  proposed_by: string;
  state: "pending" | "approved" | "rejected";
  decided_by: string | null;
  decided_at: string | null;
  event_time: string;
}
export const DEFAULT_WEIGH_V1 = { tolerance_pct: "3", tolerance_min_kg: "0.000", ticket_required: false, kg_per_roll_default: "20.000" };
export const hubParams: HubParamRec[] = [
  {
    id: sid(10, 1),
    param_set: "risk",
    version: 1,
    rule_version: "hub.risk.v2",
    params: { ...DEFAULT_RISK_V2 },
    reason: "Áp hệ số prototype cho hub.risk.v2 (tạm, chờ khách C-4)",
    effective_from: daysAgo(30),
    proposed_by: ACTORS.dieuPhoi,
    state: "approved",
    decided_by: ACTORS.pheDuyet,
    decided_at: daysAgo(30),
    event_time: daysAgo(31),
  },
];

// ---------------------------------------------------------------------------
// Kiểm kê, chuyển kho, van công bố hub
// ---------------------------------------------------------------------------
export const stocktakes: Stocktake[] = [
  {
    id: sid(11, 1),
    facility_id: F.tb,
    status: "approved",
    lines: [{ product: "straw_bale", book_kg: "120000.000", counted_kg: "119850.000", variance_kg: "-150.000" }],
    book_kg: "120000.000",
    counted_kg: "119850.000",
    variance_kg: "-150.000",
    supersedes_id: null,
    counted_by: ACTORS.dieuPhoi,
    decided_by: ACTORS.pheDuyet,
    adjustments: [{ lot_id: L.compressed, movement_id: sid(4, 900), qty_kg: "-150.000" }],
  },
];

export const TR = { inTransit: sid(12, 1) } as const;
export const transfers: Transfer[] = [
  {
    id: TR.inTransit,
    from_facility_id: F.tb,
    to_facility_id: F.ct,
    dispatch_id: sid(5, 900),
    status: "in_transit",
    dispatched_kg: "20000.000",
    received_kg: null,
    loss_kg: null,
    source_lots: [{ lot_id: L.arriving, mass_kg: "20000.000" }],
    received_lots: [],
    dispatched_at: daysAgo(1, 4),
    received_at: null,
  },
];
remember("transfer", TR.inTransit, "Thới Bình → Cần Thơ, 20 t");
findLot(L.arriving)!.movements[1]!.kind = "dispatch";
findLot(L.arriving)!.movements[1]!.ref_type = "transfer";
findLot(L.arriving)!.movements[1]!.ref_id = TR.inTransit;

export const hubPublications: ValveExport[] = facilities.flatMap((f, i) => [
  {
    id: sid(13, i * 2 + 1),
    contract: "hub_facility",
    contract_version: 1,
    event_type: "mkg.hub.facility.published",
    schema_version: 1,
    subject_ref: f.id,
    seq: 1,
    payload: opaque({ facility_ref: f.id, code: f.current?.code, name: f.current?.name, kind: f.current?.kind, province_code: f.current?.province_code, status: "active" }),
    event_time: daysAgo(30 + i),
    recorded_at: daysAgo(30 + i),
  },
  {
    id: sid(13, i * 2 + 2),
    contract: "hub_capacity",
    contract_version: 1,
    event_type: "mkg.hub.capacity.published",
    schema_version: 1,
    subject_ref: f.id,
    seq: 2,
    payload: opaque({ facility_ref: f.id, capacity_tons: f.current?.capacity_tons, as_of: daysAgo(1) }),
    event_time: daysAgo(1),
    recorded_at: daysAgo(1),
  },
]);

// ---------------------------------------------------------------------------
// Thiết bị cân (mkg/measurement)
// ---------------------------------------------------------------------------
const KEY = "MCowBQYDK2VwAyEAq3kF0Vd9mJ2Zy7s8GQv1tCq0xL5bN4hP2aW6eR8uT0o=";
function dev(id: string, epoch: number, facility: string, point: "HUB_IN" | "HUB_OUT", status: ScaleDevice["status"], health: ScaleDevice["health"], proposer: string = ACTORS.dieuPhoi): ScaleDevice {
  return {
    device_id: id,
    epoch,
    facility_ref: facility,
    point,
    public_key: KEY,
    key_id: `${id}#${epoch}`,
    cert_sha256: (id.length.toString(16) + epoch).padEnd(64, "a"),
    scale_model: "Mettler Toledo IND570 (tạm, chờ khách C-6)",
    status,
    proposed_by: proposer,
    proposed_at: daysAgo(40 - epoch),
    decided_by: status === "pending" ? null : ACTORS.pheDuyet,
    decided_at: status === "pending" ? null : daysAgo(39 - epoch),
    revoked_at: status === "revoked" ? daysAgo(5) : null,
    revoke_reason: status === "revoked" ? "Mất ổ đĩa đệm, đăng ký kỳ mới (ADR-0019)" : null,
    health,
  };
}
const noHealth = { last_seen_at: null, buffer_pct: null, clock_skew_ms: null, last_counter: null };
export const devices: ScaleDevice[] = [
  dev("SCALE-TB-IN-01", 1, F.tb, "HUB_IN", "active", { last_seen_at: minutesAgo(2), buffer_pct: "3.50", clock_skew_ms: 120, last_counter: 1388 }),
  dev("SCALE-TB-OUT-01", 1, F.tb, "HUB_OUT", "active", { last_seen_at: minutesAgo(190), buffer_pct: "64.00", clock_skew_ms: 2400, last_counter: 902 }),
  dev("SCALE-CT-IN-01", 1, F.ct, "HUB_IN", "revoked", noHealth),
  dev("SCALE-CT-IN-01", 2, F.ct, "HUB_IN", "pending", noHealth),
];

// ---------------------------------------------------------------------------
// Tham số đo lường: dung sai (C-4) và định mức hao hụt sấy — phiên bản có duyệt
// ---------------------------------------------------------------------------
export const tolerances: MeasurementParam[] = [
  {
    id: sid(14, 1),
    kind: "tolerance",
    version: 1,
    status: "approved",
    effective_from: daysAgo(60),
    provisional: true,
    reason: "Mặc định prototype plant-weighing (tạm, chờ khách C-4)",
    proposed_by: ACTORS.dieuPhoi,
    decided_by: ACTORS.pheDuyet,
    decided_at: daysAgo(59),
    values: { ...DEFAULT_TOLERANCE },
  },
];
export const lossNorms: MeasurementParam[] = [
  {
    id: sid(15, 1),
    kind: "loss_norm",
    version: 1,
    status: "approved",
    effective_from: daysAgo(60),
    provisional: true,
    reason: "Định mức sấy theo độ ẩm tạm (chờ khách)",
    proposed_by: ACTORS.dieuPhoi,
    decided_by: ACTORS.pheDuyet,
    decided_at: daysAgo(59),
    values: { ...DEFAULT_LOSS_NORM, bands: DEFAULT_LOSS_NORM.bands.map((b) => ({ ...b })) },
  },
];

export function activeTolerance(): { label: string; values: ToleranceValues } {
  const v = tolerances.find((t) => t.status === "approved" && Date.parse(t.effective_from ?? "") <= Date.now());
  return v ? { label: `tolerance#${v.version}`, values: v.values as ToleranceValues } : { label: "provisional-default", values: DEFAULT_TOLERANCE };
}
export function activeLossNorm(): { label: string; values: LossNormValues } {
  const v = lossNorms.find((t) => t.status === "approved" && Date.parse(t.effective_from ?? "") <= Date.now());
  return v ? { label: `loss_norm#${v.version}`, values: v.values as LossNormValues } : { label: "provisional-default", values: DEFAULT_LOSS_NORM };
}

// ---------------------------------------------------------------------------
// Phiên cân, phiếu cân, điều khoản, đối soát, tranh chấp
// ---------------------------------------------------------------------------
export const sessions: WeighingSession[] = [
  {
    id: sid(16, 1),
    lot_ref: L.dried,
    point: "HUB_OUT",
    route: "HUB",
    facility_ref: F.tb,
    device_id: "SCALE-TB-OUT-01",
    epoch: 1,
    counter_from: 880,
    counter_to: null,
    status: "open",
    moisture_pct: null,
    tare_kg: null,
    opened_at: minutesAgo(35),
    closed_at: null,
  },
];
remember("session", sid(16, 1), "LO-TB-0912 · HUB_OUT · đang mở");

export const tickets: WeighTicket[] = [];
function ticket(lotRef: string, point: WeighTicket["point"], massKg: string, days: number, opts: Partial<WeighTicket> = {}): WeighTicket {
  const edge = point !== "CUSTOMER_IN";
  const t: WeighTicket = {
    id: newId(17),
    lot_ref: lotRef,
    point,
    route: opts.route ?? "HUB",
    source: edge ? "edge" : "manual_evidence",
    mass_kg: massKg,
    moisture_pct: opts.moisture_pct ?? null,
    moisture_state: opts.moisture_pct ? "as_received" : "not_measured",
    rule_version: edge ? "weigh.integrate.v1" : "measurement.ticket.v1",
    window: edge ? { device_id: point === "HUB_IN" ? "SCALE-TB-IN-01" : "SCALE-TB-OUT-01", epoch: 1, counter_from: 1200, counter_to: 1263, readings_used: 58, readings_total: 64 } : null,
    evidence_ref: edge ? null : newId(18),
    supersedes_id: null,
    effective: true,
    reason: edge ? null : "Nhập tay số cân nhà máy theo phiếu giấy (chưa tích hợp hệ cân khách — C-5)",
    event_time: daysAgo(days),
    recorded_at: daysAgo(days),
    seq: tickets.length + 1,
    ...opts,
  };
  tickets.push(t);
  return t;
}
ticket(L.proto, "HUB_IN", "96800.000", 9, { moisture_pct: "17.50" });
ticket(L.proto, "HUB_OUT", "93900.000", 6);
ticket(L.proto, "CUSTOMER_IN", "93520.000", 5);
ticket(L.dispute, "HUB_IN", "91500.000", 12, { moisture_pct: "16.00" });
ticket(L.dispute, "HUB_OUT", "88100.000", 9);
ticket(L.dispute, "CUSTOMER_IN", "82900.000", 8);
ticket(L.arriving, "HUB_IN", "84200.000", 3, { moisture_pct: "21.00" });
ticket(L.perBale, "HUB_IN", "50000.000", 25, { moisture_pct: "22.00" });
ticket(L.perBale, "HUB_OUT", "47000.000", 22);
ticket(L.perBale, "CUSTOMER_IN", "46800.000", 21);
ticket(L.abnormal, "HUB_IN", "60000.000", 21, { moisture_pct: "14.00" });
ticket(L.abnormal, "HUB_OUT", "58200.000", 18);
ticket(L.abnormal, "CUSTOMER_IN", "58100.000", 17);
ticket(L.critical, "HUB_IN", "70500.000", 20, { moisture_pct: "18.50" });
ticket(L.critical, "HUB_OUT", "70000.000", 17);
ticket(L.critical, "CUSTOMER_IN", "58000.000", 16);
ticket(L.direct, "CUSTOMER_IN", "45600.000", 4, { route: "DIRECT" });
// Lô gom của lệnh đang giao tổ: ghe đã qua cân HUB_IN nhưng chưa nhập hub (nhận theo phiếu cân, Q-MKG-04).
export const ARRIVING_TICKET = ticket(O.assigned, "HUB_IN", "86260.000", 0, { moisture_pct: "18.00" }).id!;
remember("lot", O.assigned, "LG-TH-0929 (ghe đã cân, chờ nhập hub)");

export interface TermsRec {
  lot_ref: string;
  pricing_basis: PricingBasis;
  contract_ref: string | null;
  customer_ref: string | null;
}
export const terms: TermsRec[] = [{ lot_ref: L.perBale, pricing_basis: "per_bale", contract_ref: sid(19, 2), customer_ref: sid(19, 10) }];
export function termsOf(lotRef: string): TermsRec {
  return terms.find((t) => t.lot_ref === lotRef) ?? { lot_ref: lotRef, pricing_basis: "per_tonne", contract_ref: null, customer_ref: null };
}

/** Số cuộn xếp ghe/xe và kg/cuộn học được của lô (từ mkg.fieldops.roll_count.recorded, RB-6). */
export const lotRolls: Record<string, { rolls: number; kgPerRoll: string }> = {
  [L.proto]: { rolls: 4400, kgPerRoll: "20" },
  [L.dispute]: { rolls: 4550, kgPerRoll: "20" },
  [L.perBale]: { rolls: 2300, kgPerRoll: "20" },
  [L.abnormal]: { rolls: 3000, kgPerRoll: "20" },
  [L.direct]: { rolls: 2280, kgPerRoll: "20" },
};

export function effectiveTicket(lotRef: string, point: WeighTicket["point"]): WeighTicket | undefined {
  const all = tickets.filter((t) => t.lot_ref === lotRef && t.point === point);
  const superseded = new Set(all.map((t) => t.supersedes_id).filter(Boolean));
  return all.filter((t) => !superseded.has(t.id)).pop();
}

export const reconciliations: Reconciliation[] = [];
export const disputes: Dispute[] = [];

/** Đối soát một lô theo measurement.reconcile.v1; null khi chưa đủ phiếu. */
export function runReconcile(lotRef: string, actor: string, at: string): Reconciliation | null {
  const hin = effectiveTicket(lotRef, "HUB_IN");
  const hout = effectiveTicket(lotRef, "HUB_OUT");
  const cin = effectiveTicket(lotRef, "CUSTOMER_IN");
  const route = cin?.route === "DIRECT" ? "DIRECT" : "HUB";
  const tol = activeTolerance();
  const norm = activeLossNorm();
  const basis = termsOf(lotRef).pricing_basis;
  let r;
  try {
    r = reconcile({
      route,
      basis,
      hubIn: hin?.mass_kg ?? null,
      hubOut: hout?.mass_kg ?? null,
      customerIn: cin?.mass_kg ?? null,
      hubInMoisture: hin?.moisture_pct ?? null,
      rolls: lotRolls[lotRef] ?? null,
      tolerance: tol.values,
      lossNorm: norm.values,
    });
  } catch {
    return null;
  }
  const prev = reconciliations.find((x) => x.lot_ref === lotRef);
  const rec: Reconciliation = {
    id: newId(20),
    lot_ref: lotRef,
    route,
    pricing_basis: basis,
    hub_in_kg: hin?.mass_kg ?? null,
    hub_out_kg: hout?.mass_kg ?? null,
    customer_in_kg: cin?.mass_kg ?? null,
    hub_in_moisture_pct: hin?.moisture_pct ?? null,
    internal: r.internal,
    commercial: r.commercial,
    roll_check: r.roll_check,
    yield_kg_per_roll: r.yield_kg_per_roll,
    yield_aggregated: r.yield_aggregated,
    outcome: r.outcome,
    tolerance_version: tol.label,
    loss_norm_version: route === "HUB" ? norm.label : null,
    dispute_id: null,
    supersedes_id: prev?.id ?? null,
    rule_version: "measurement.reconcile.v1",
    event_time: at,
    recorded_at: at,
  };
  if (r.outcome === "dispute_opened") {
    const open = disputes.find((d) => d.lot_ref === lotRef && (d.status === "open" || d.status === "under_review"));
    if (open) rec.dispute_id = open.id;
    else {
      const d: Dispute = {
        id: newId(21),
        lot_ref: lotRef,
        status: "open",
        severity: r.commercial.status === "critical" ? "critical" : "flag",
        reconciliation_id: rec.id!,
        hub_out_kg: hout?.mass_kg ?? null,
        customer_in_kg: cin?.mass_kg ?? null,
        diff_kg: r.commercial.diff_kg ?? "0.000",
        diff_pct: r.commercial.diff_pct,
        evidence_refs: [],
        resolution: null,
        agreed_kg: null,
        opened_at: at,
        opened_by: actor,
        events: [{ seq: 1, action: "opened", status_after: "open", evidence_refs: null, resolution: null, agreed_kg: null, note: null, event_time: at, actor_ref: actor }],
      };
      disputes.unshift(d);
      rec.dispute_id = d.id;
    }
  }
  reconciliations.unshift(rec);
  return rec;
}

for (const [lotRef, days] of [
  [L.proto, 5],
  [L.dispute, 8],
  [L.perBale, 21],
  [L.abnormal, 17],
  [L.critical, 16],
  [L.direct, 4],
] as const) {
  runReconcile(lotRef, ACTORS.system, daysAgo(days));
}
// Hồ sơ nghiêm trọng đang được xem xét.
{
  const d = disputes.find((x) => x.lot_ref === L.critical);
  if (d) {
    d.status = "under_review";
    const ev: DisputeEvent = {
      seq: 2,
      action: "start_review",
      status_after: "under_review",
      evidence_refs: null,
      resolution: null,
      agreed_kg: null,
      note: "Đề nghị nhà máy gửi ảnh phiếu cân và camera bàn cân",
      event_time: daysAgo(15),
      actor_ref: ACTORS.can,
    };
    d.events?.push(ev);
  }
}

export const imports: CustomerImport[] = [];
export const measurementPublications: Record<string, ValveExport[]> = {};

// ---------------------------------------------------------------------------
// Lệnh gom, đếm cuộn, xác nhận HTX (mkg/fieldops)
// ---------------------------------------------------------------------------
export const orders: CollectionOrder[] = [];
export const orderEvents: Record<string, OrderEvent[]> = {};
export const rollCounts: RollCount[] = [];
export const confirmations: HtxConfirmation[] = [];
export const shifts: CrewShift[] = [];
export const buySide: BuySideReconciliation[] = [];
export const fieldPublications: Record<string, ValveExport[]> = {};

/** Danh bạ thửa ↔ HTX (thay cho Application API của nền tảng — tài khoản dịch vụ MKG, bề mặt đối tác /v1/partner/*, ADR-0020). */
export const PLOT_HTX: Record<string, string> = {
  [PLOT.a1]: HTX.vinhBinh,
  [PLOT.a2]: HTX.vinhBinh,
  [PLOT.b1]: HTX.tanHiep,
  [PLOT.b2]: HTX.tanHiep,
  [PLOT.c1]: HTX.giongRieng,
  [PLOT.c2]: HTX.giongRieng,
};
export const HTX_COMMUNE: Record<string, string> = { [HTX.vinhBinh]: "31471", [HTX.tanHiep]: "31120", [HTX.giongRieng]: "31201" };
for (const h of Object.values(HTX)) remember("htx", h);

export function orderEvent(orderId: string, kind: OrderEvent["kind"], actor: string, at: string, extra: Partial<OrderEvent> = {}): OrderEvent {
  const list = (orderEvents[orderId] ??= []);
  const o = orders.find((x) => x.order_id === orderId);
  const e: OrderEvent = {
    id: newId(22),
    seq: list.length + 1,
    kind,
    revision: o?.revision ?? 1,
    crew_ref: null,
    reason: null,
    actor_ref: actor,
    event_time: at,
    recorded_at: at,
    ...extra,
  };
  list.push(e);
  return e;
}

function order(id: string, status: CollectionOrder["status"], htx: string, plots: string[], fromDays: number, creator: string, crew: string | null, reason: string | null = null): void {
  const at = daysAgo(fromDays + 2);
  const approved = status !== "planned" && status !== "cancelled";
  orders.push({
    order_id: id,
    status,
    revision: 1,
    htx_ref: htx,
    commune_code: HTX_COMMUNE[htx] ?? "31471",
    window_from: daysAgo(fromDays).slice(0, 10),
    window_to: daysAgo(fromDays - 7).slice(0, 10),
    plot_refs: plots,
    crew_ref: crew,
    reason,
    created_by: creator,
    approved_by: approved ? ACTORS.pheDuyet : null,
    approved_at: approved ? daysAgo(fromDays + 1) : null,
    published: approved,
    created_at: at,
    updated_at: at,
  });
  orderEvent(id, "planned", creator, at);
  if (approved) orderEvent(id, "approved", ACTORS.pheDuyet, daysAgo(fromDays + 1));
  if (crew) orderEvent(id, "assigned", ACTORS.dieuPhoi, daysAgo(fromDays), { crew_ref: crew });
  if (status === "closed") orderEvent(id, "closed", ACTORS.dieuPhoi, daysAgo(fromDays - 2));
  if (status === "cancelled") orderEvent(id, "cancelled", ACTORS.dieuPhoi, daysAgo(fromDays), { reason });
}

order(O.done, "closed", HTX.vinhBinh, [PLOT.a1, PLOT.a2], 10, ACTORS.dieuPhoi, CREW.t1);
order(O.assigned, "assigned", HTX.tanHiep, [PLOT.b1], 3, ACTORS.dieuPhoi, CREW.t2);
order(O.approved, "approved", HTX.giongRieng, [PLOT.c1, PLOT.c2], 1, ACTORS.pheDuyet, null);
order(O.planned, "planned", HTX.tanHiep, [PLOT.b2], -2, ACTORS.dieuPhoi, null);
order(O.cancelled, "cancelled", HTX.giongRieng, [PLOT.c2], 15, ACTORS.dieuPhoi, null, "HTX báo mưa lớn, ruộng ngập — hoãn thu gom");

export function recordCount(r: Omit<RollCount, "id" | "seq" | "rule_version" | "recorded_at"> & { id?: string }): RollCount {
  const rc: RollCount = { rule_version: "fieldops.roll_count.v1", recorded_at: r.event_time, ...r, id: r.id ?? newId(23), seq: rollCounts.length + 1 };
  rollCounts.push(rc);
  return rc;
}

function seedCount(orderId: string, plot: string, stage: RollCount["stage"], rolls: number, days: number, reason: string | null = null): RollCount {
  return recordCount({
    order_id: orderId,
    plot_ref: plot,
    htx_ref: PLOT_HTX[plot] ?? HTX.vinhBinh,
    stage,
    rolls,
    loose_kg: null,
    discrepancy_reason: reason,
    supersedes_id: null,
    correction_reason: null,
    device_id: "FIELD-APP-T1-07",
    event_time: daysAgo(days),
    actor_ref: ACTORS.hienTruong,
  });
}
// proto: 5 800 cuộn → 5 700 tập kết → 5 700 xếp ghe (ghe 1: 4 400 cuộn cân 96,8 t).
seedCount(O.done, PLOT.a1, "bale", 3000, 9);
seedCount(O.done, PLOT.a2, "bale", 2800, 9);
seedCount(O.done, PLOT.a1, "gather", 2950, 8, "50 cuộn bị ướt, để lại ruộng làm phân");
seedCount(O.done, PLOT.a2, "gather", 2750, 8, "50 cuộn rã dây khi bốc, gom thành rơm rời");
const boatA1 = seedCount(O.done, PLOT.a1, "boat_load", 2950, 8);
const boatA2 = seedCount(O.done, PLOT.a2, "boat_load", 2750, 8);
seedCount(O.assigned, PLOT.b1, "bale", 4300, 1);

export function buySideFor(orderId: string, plot: string): BuySideReconciliation | undefined {
  return buySide.find((b) => b.order_id === orderId && b.plot_ref === plot);
}

function seedRecon(rc: RollCount, statementRev: number, htxRolls: number | null, outcome: "confirmed" | "disputed" | null): void {
  const confirmId = outcome ? newId(24) : null;
  buySide.push({
    order_id: rc.order_id,
    plot_ref: rc.plot_ref,
    htx_ref: rc.htx_ref,
    statement_ref: sid(25, buySide.length + 1),
    statement_revision: statementRev,
    mkg_rolls: rc.rolls,
    mkg_roll_count_id: rc.id,
    htx_rolls: htxRolls,
    outcome,
    confirmation_id: confirmId,
    confirmed_revision: outcome ? statementRev : null,
    status: outcome === null ? "pending" : outcome === "confirmed" && htxRolls === rc.rolls ? "match" : "mismatch",
    updated_at: daysAgo(7),
  });
  if (outcome && confirmId) {
    confirmations.push({
      id: confirmId,
      seq: confirmations.length + 1,
      order_id: rc.order_id!,
      plot_ref: rc.plot_ref!,
      htx_ref: rc.htx_ref!,
      statement_ref: sid(25, buySide.length),
      statement_revision: statementRev,
      roll_count_id: rc.id!,
      mkg_rolls: rc.rolls!,
      outcome,
      htx_rolls: htxRolls ?? rc.rolls!,
      reason: outcome === "disputed" ? "HTX đếm 2 800 cuộn tại bờ, MKG ghi 2 750" : null,
      channel: "mkg_staff",
      evidence_ref: null,
      actor_ref: ACTORS.dieuPhoi,
      event_time: daysAgo(7),
      recorded_at: daysAgo(7),
    });
  }
}
seedRecon(boatA1, 1, 2950, "confirmed");
seedRecon(boatA2, 1, 2800, "disputed");

shifts.push({
  id: newId(26),
  crew_ref: CREW.t1,
  order_id: O.done,
  kind: "opened",
  headcount: 8,
  equipment_refs: ["MAY-CUON-01", "GHE-AG-12345"],
  actor_ref: ACTORS.hienTruong,
  device_id: "FIELD-APP-T1-07",
  event_time: daysAgo(9),
  recorded_at: daysAgo(9),
});

export function publish(bucket: Record<string, ValveExport[]>, subject: string, contract: ValveExport["contract"], eventType: string, payload: Record<string, unknown>, at: string): ValveExport {
  const list = (bucket[subject] ??= []);
  const x: ValveExport = {
    id: newId(27),
    contract,
    contract_version: 1,
    event_type: eventType,
    schema_version: 1,
    subject_ref: subject,
    seq: list.length + 1,
    payload: opaque(payload),
    event_time: at,
    recorded_at: at,
  };
  list.unshift(x);
  return x;
}
for (const o of orders.filter((x) => x.published)) {
  publish(fieldPublications, o.order_id!, "collection_schedule", "mkg.fieldops.collection_schedule.published", {
    schedule_ref: o.order_id,
    htx_ref: o.htx_ref,
    commune_code: o.commune_code,
    window_from: o.window_from,
    window_to: o.window_to,
    plot_refs: o.plot_refs,
    status: o.status,
  }, o.approved_at ?? daysAgo(1));
}
publish(fieldPublications, O.done, "htx_collection_statement", "mkg.fieldops.htx_collection_statement.published", { schedule_ref: O.done, htx_ref: HTX.vinhBinh, revision: 1, plots: [{ plot_ref: PLOT.a1, rolls: 2950 }, { plot_ref: PLOT.a2, rolls: 2750 }] }, daysAgo(8));

/** kg/cuộn học theo HTX (Σ kg cân / Σ cuộn của 30 lô gom đã cân gần nhất) — nội bộ, KHÔNG qua van. */
export const kgPerRoll: Record<string, { kg_per_roll: string; source: "htx" | "system" | "config" | "default"; samples: number }> = {
  [HTX.vinhBinh]: { kg_per_roll: "22.0", source: "htx", samples: 3 },
  [HTX.tanHiep]: { kg_per_roll: "19.8", source: "htx", samples: 5 },
};
export const SYSTEM_KG_PER_ROLL = { kg_per_roll: "20.9", source: "system" as const, samples: 12 };
