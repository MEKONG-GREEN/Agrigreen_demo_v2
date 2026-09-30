// Backend giả lập mkg/costing — theo contracts/openapi/mkg-internal.yaml (tag costing) và costing/domain:
// kỳ tháng open → closing → closed → posted (đóng kỳ 202 + job, mẫu mục 12; không mở lại), bút toán điều chỉnh ở kỳ
// đang mở (supersedes_id), người đóng kỳ ≠ người lập điều chỉnh của kỳ (R-56), phân bổ + tính lại (kết quả mới
// supersedes_id), quyết toán HTX proto PM-01..04, chia carbon 45 % / 70-15-15 (VVB nhận phần dư).
import type { CostEntry, CostingAllocation, CostingCloseJob, CostingParam, CostingParamVersion, CostingPeriod, CostingSettlement } from "../types";
import { grams, kgOf } from "@/lib/kg";
import { amountForCount, amountForKg, cents, divRound, isPrice, moneyOf, percentHundredths, priceOf, priceUnits, splitCarbon } from "@/lib/money";
import { sha256Text } from "@/lib/sha256";
import { HTX, L, O, F, newId, sid } from "./db";
import { monthVn, reasonOf, todayVn } from "./g4common";
import { created, daysAgo, forbidden, hasRole, json, nowIso, problem, route, selfApproval, str, validation, UUID_RE, type Route } from "./http";
import { contractFor } from "./orders";
import { ACTORS } from "./personas";

const COST = "cost_accountant";
const CLOSER = "period_closer";
const CHIEF = "chief_accountant";
const READ = [COST, CLOSER, CHIEF];

export const CO = {
  periodPrev3: sid(0x2d, 1),
  periodPrev2: sid(0x2d, 2),
  periodPrev1: sid(0x2d, 3),
  periodCurrent: sid(0x2d, 4),
  settlementPerBale: sid(0x30, 1),
  settlementPerTonne: sid(0x30, 2),
  settlementNoContract: sid(0x30, 3),
  allocationTrip: sid(0x2f, 1),
  entryStorage: sid(0x2e, 4),
} as const;

// ---------------------------------------------------------------------------
// Tham số giá thành có phiên bản (bề mặt quản trị)
// ---------------------------------------------------------------------------
const PARAM_DEFS: { key: string; def: string; source: string; kind: "alloc" | "moisture" | "pct" | "rate" | "days" }[] = [
  { key: "costing.allocation.key", def: "mass", source: "đề xuất G4-0 (Q-2)", kind: "alloc" },
  { key: "costing.moisture.base_pct", def: "none", source: "C-4 (chưa có công thức)", kind: "moisture" },
  { key: "costing.loss.expected_pct", def: "5", source: "proto #19", kind: "pct" },
  { key: "costing.loss.hub_processing_pct", def: "2", source: "proto #56", kind: "pct" },
  { key: "costing.rate.hub_processing_vnd_per_t", def: "120000", source: "proto #55", kind: "rate" },
  { key: "costing.settlement.payment_term_days", def: "15", source: "proto PAYMENT_TERM_DAYS", kind: "days" },
  { key: "costing.carbon.retained_pct", def: "45", source: "proto CARBON-SHARE", kind: "pct" },
  { key: "costing.carbon.split.farmers_pct", def: "70", source: "proto CARBON-SHARE", kind: "pct" },
  { key: "costing.carbon.split.authority_pct", def: "15", source: "proto CARBON-SHARE", kind: "pct" },
  { key: "costing.carbon.split.verifier_pct", def: "15", source: "proto CARBON-SHARE (nhận phần dư làm tròn)", kind: "pct" },
];
const paramVersions: Record<string, CostingParamVersion[]> = Object.fromEntries(PARAM_DEFS.map((d) => [d.key, []]));

function paramCheck(kind: string, v: string): string | null {
  if (kind === "alloc") return v === "mass" ? null : "Chỉ 'mass' tới khi khách chốt khoá phân bổ (Q-2)";
  if (kind === "moisture") return v === "none" ? null : "Chỉ 'none' tới khi có công thức quy đổi ẩm (C-4)";
  if (kind === "pct") return /^\d{1,3}(\.\d{1,2})?$/.test(v) && percentHundredths(v) <= 10_000n ? null : "0..100, tối đa 2 số lẻ";
  if (kind === "days") return /^\d{1,3}$/.test(v) && Number(v) >= 1 && Number(v) <= 365 ? null : "1..365 ngày";
  return isPrice(v) ? null : "Đơn giá > 0, tối đa 4 số lẻ";
}

export function costParam(key: string): { value: string; seq: number } {
  const v = (paramVersions[key] ?? []).filter((x) => x.decision === "approved" && Date.parse(x.effective_from!) <= Date.now()).sort((a, b) => b.seq! - a.seq!)[0];
  return v ? { value: v.value!, seq: v.seq! } : { value: PARAM_DEFS.find((d) => d.key === key)!.def, seq: 0 };
}

function paramView(key: string): CostingParam {
  const d = PARAM_DEFS.find((x) => x.key === key)!;
  const a = costParam(key);
  return {
    key,
    default: d.def,
    default_source: d.source,
    default_label: "tạm, chờ khách",
    active: { value: a.value, seq: a.seq, pinned: `${key}#${a.seq}`, label: a.seq === 0 ? "tạm, chờ khách" : "đã duyệt" },
    versions: [...(paramVersions[key] ?? [])].sort((x, y) => y.seq! - x.seq!),
  };
}

// ---------------------------------------------------------------------------
// Kỳ, bút toán, phân bổ
// ---------------------------------------------------------------------------
const periods: CostingPeriod[] = [];
const entries: CostEntry[] = [];
const allocations: CostingAllocation[] = [];
const jobs: (CostingCloseJob & { polls: number })[] = [];

function monthBounds(code: string): { from: string; to: string } {
  const [y, m] = code.split("-").map(Number) as [number, number];
  const from = new Date(Date.UTC(y, m - 1, 1) - 7 * 3_600_000).toISOString();
  const to = new Date(Date.UTC(y, m, 1) - 7 * 3_600_000).toISOString();
  return { from, to };
}

function period(id: string, code: string, status: CostingPeriod["status"]): CostingPeriod {
  const b = monthBounds(code);
  const p: CostingPeriod = { id, code, ...b, status, opened_at: b.from, close_run_id: null, closed_by: null, closed_at: null, journal_ref: null, posted_at: null, summary: null };
  periods.push(p);
  return p;
}

function periodOf(code: string): CostingPeriod | undefined {
  return periods.find((p) => p.code === code);
}

function ensurePeriod(code: string): CostingPeriod {
  return periodOf(code) ?? period(newId(0x2d), code, "open");
}

function entry(id: string, lot: string, code: string, category: CostEntry["category"], amount: string, massKg: string | null, dim: CostEntry["dimension_kind"], dimRef: string | null, source: CostEntry["source_kind"], sourceRef: string | null, actor: string, daysBack: number, extra: Partial<CostEntry> = {}): CostEntry {
  const seq = entries.filter((e) => e.lot_ref === lot).length + 1;
  const e: CostEntry = { id, lot_ref: lot, period_code: code, late_from: null, category, amount, delta_amount: amount, mass_kg: massKg, delta_mass_kg: massKg, dimension_kind: dim, dimension_ref: dimRef, source_kind: source, source_ref: sourceRef, allocation_ref: null, entry_kind: "normal", note: null, event_time: daysAgo(daysBack), recorded_at: daysAgo(daysBack), supersedes_id: null, correction_reason: null, actor_ref: actor, rule_version: "costing.entry.v1", seq, ...extra };
  entries.push(e);
  return e;
}

/** Dòng tổng hợp tất định của kỳ: Σ delta theo (nhóm, chiều) — sắp xếp cố định. */
function summarize(code: string) {
  const map = new Map<string, { category: CostEntry["category"]; dimension_kind: string; dimension_ref: string | null; amount: bigint; mass: bigint | null; entries: number }>();
  for (const e of entries.filter((x) => x.period_code === code)) {
    const k = `${e.category}|${e.dimension_kind}|${e.dimension_ref ?? ""}`;
    const cur = map.get(k) ?? { category: e.category, dimension_kind: e.dimension_kind ?? "none", dimension_ref: e.dimension_ref ?? null, amount: 0n, mass: null, entries: 0 };
    cur.amount += cents(e.delta_amount ?? "0");
    if (e.delta_mass_kg) cur.mass = (cur.mass ?? 0n) + grams(e.delta_mass_kg);
    cur.entries += 1;
    map.set(k, cur);
  }
  return [...map.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([, v]) => ({ category: v.category, dimension_kind: v.dimension_kind, dimension_ref: v.dimension_ref, amount: moneyOf(v.amount), mass_kg: v.mass === null ? null : kgOf(v.mass), entries: v.entries }));
}

type ClosedHook = (p: CostingPeriod) => void;
let onClosed: ClosedHook = () => undefined;
/** acctbridge đăng ký: kỳ đóng → job dựng bút toán (người dựng = người đóng kỳ). */
export function onPeriodClosed(fn: ClosedHook): void {
  onClosed = fn;
}

function closeNow(p: CostingPeriod, closer: string, runId: string): void {
  const lines = summarize(p.code!);
  const snapshot = sha256Text(JSON.stringify(entries.filter((e) => e.period_code === p.code).map((e) => [e.id, e.delta_amount, e.category])));
  const result = sha256Text(JSON.stringify(lines));
  p.status = "closed";
  p.close_run_id = runId;
  p.closed_by = closer;
  p.closed_at = nowIso();
  p.summary = { close_run_id: runId, lines, line_count: lines.length, input_snapshot_sha256: snapshot, result_set_sha256: result, closed_by: closer, closed_at: p.closed_at, rule_version: "costing.close.v1" };
}

/** acctbridge: bút toán đã đẩy → kỳ posted (consumer mkg.costing.journal_pushed). */
export function markPosted(periodId: string, journalId: string): void {
  const p = periods.find((x) => x.id === periodId);
  if (p && p.status === "closed") {
    p.status = "posted";
    p.journal_ref = journalId;
    p.posted_at = nowIso();
  }
}

export function periodById(id: string): CostingPeriod | undefined {
  return periods.find((p) => p.id === id);
}
export const allPeriods = periods;

const M3 = monthVn(3);
const M2 = monthVn(2);
const M1 = monthVn(1);
const M0 = monthVn(0);
period(CO.periodPrev3, M3, "open");
period(CO.periodPrev2, M2, "open");
period(CO.periodPrev1, M1, "open");
period(CO.periodCurrent, M0, "open");

// Kỳ M3, M2: đã đóng (M2 đã ghi sổ kế toán; M3 bút toán đẩy lỗi — xem acctbridge).
entry(sid(0x2e, 20), L.critical, M3, "collection_labour", "10575000.00", "70500.000", "htx", HTX.giongRieng, "manual", null, ACTORS.keToan, 80);
entry(sid(0x2e, 21), L.critical, M3, "transport_waterway", "6345000.00", "70500.000", "mode", null, "trip", sid(0x20, 90), ACTORS.keToan, 80);
entry(sid(0x2e, 22), L.critical, M3, "straw_purchase", "45825000.00", "70500.000", "htx", HTX.giongRieng, "manual", null, ACTORS.keToan, 80);
entry(sid(0x2e, 30), L.abnormal, M2, "collection_labour", "9000000.00", "60000.000", "htx", HTX.vinhBinh, "manual", null, ACTORS.keToan, 50);
entry(sid(0x2e, 31), L.abnormal, M2, "hub_processing", "7200000.00", "60000.000", "facility", F.tb, "hub_processing", null, ACTORS.keToan, 50);
entry(sid(0x2e, 32), L.abnormal, M2, "sales_revenue", "81000000.00", "60000.000", "customer", sid(0x29, 20), "delivery", sid(0x2a, 90), ACTORS.keToan, 45);
closeNow(periods[0]!, ACTORS.keToanTruong, sid(0x31, 1));
closeNow(periods[1]!, ACTORS.keToanTruong, sid(0x31, 2));

// Kỳ M1: đang mở, đã hết tháng → đóng được; có bút toán điều chỉnh do Kế toán giá thành L lập (R-56 khi đóng).
entry(sid(0x2e, 1), L.proto, M1, "collection_labour", "8712000.00", "96800.000", "htx", HTX.tanHiep, "manual", null, ACTORS.keToan, 25);
entry(sid(0x2e, 2), L.proto, M1, "transport_waterway", "5204250.00", "90000.000", "mode", null, "trip", sid(0x20, 2), ACTORS.system, 24, { allocation_ref: CO.allocationTrip });
entry(sid(0x2e, 3), L.proto, M1, "hub_processing", "11616000.00", "96800.000", "facility", F.tb, "hub_processing", null, ACTORS.system, 22);
const storage = entry(CO.entryStorage, L.proto, M1, "storage", "1500000.00", null, "facility", F.tb, "manual", null, ACTORS.keToan, 20);
entry(sid(0x2e, 5), L.proto, M1, "handling", "2420000.00", null, "none", null, "manual", null, ACTORS.keToan, 18);
entry(sid(0x2e, 6), L.proto, M1, "sales_revenue", "174240000.00", "96800.000", "customer", sid(0x29, 20), "delivery", sid(0x2a, 91), ACTORS.system, 15);
entry(sid(0x2e, 7), L.proto, M1, "storage", "1800000.00", null, "facility", F.tb, "adjusting", null, ACTORS.keToan, 12, { supersedes_id: storage.id, delta_amount: "300000.00", entry_kind: "adjusting", correction_reason: "Phí lưu kho tháng tính thiếu 3 ngày cuối kỳ" });
entry(sid(0x2e, 8), L.compressed, M1, "transport_road", "1250304.00", "14800.000", "mode", null, "trip", sid(0x20, 3), ACTORS.system, 10);
entry(sid(0x2e, 9), L.ct, M1, "collection_labour", "4050000.00", "45000.000", "htx", HTX.vinhBinh, "manual", null, ACTORS.keToan, 9);
entry(sid(0x2e, 10), L.ct, M0, "storage", "600000.00", null, "facility", F.ct, "manual", null, ACTORS.keToan, 2);

allocations.push({
  id: CO.allocationTrip,
  period_ref: CO.periodPrev1,
  kind: "trip_cost",
  source_ref: sid(0x20, 2),
  source_event_time: daysAgo(24),
  basis: "mass",
  total_amount: "10408500.00",
  total_kg: "90000.000",
  lines: [
    { lot_ref: L.proto, mass_kg: "45000.000", amount: "5204250.00" },
    { lot_ref: L.ct, mass_kg: "45000.000", amount: "5204250.00" },
  ] as unknown as Record<string, never>[],
  inputs: { trip: sid(0x20, 2), actual_cost: "10408500.00", cargo: 2 } as unknown as Record<string, never>,
  inputs_sha256: sha256Text("alloc-trip-inputs"),
  result_sha256: sha256Text("alloc-trip-result"),
  param_pins: { "costing.allocation.key": 0 },
  params_as_of: daysAgo(24),
  rule_version: "costing.alloc.v1",
  supersedes_id: null,
  superseded_by: null,
  correction_reason: null,
  recorded_at: daysAgo(24),
});

function lotCost(lot: string) {
  const list = entries.filter((e) => e.lot_ref === lot);
  if (!list.length) return null;
  const cats = new Map<string, { amount: bigint; mass: bigint | null; entries: number }>();
  let revenue = 0n;
  let delivered = 0n;
  for (const e of list) {
    if (e.category === "sales_revenue") {
      revenue += cents(e.delta_amount ?? "0");
      if (e.delta_mass_kg) delivered += grams(e.delta_mass_kg);
      continue;
    }
    if (e.category === "straw_purchase" || e.category === "carbon_revenue" || e.category === "carbon_share") continue;
    const c = cats.get(e.category!) ?? { amount: 0n, mass: null, entries: 0 };
    c.amount += cents(e.delta_amount ?? "0");
    if (e.delta_mass_kg) c.mass = (c.mass ?? 0n) + grams(e.delta_mass_kg);
    c.entries += 1;
    cats.set(e.category!, c);
  }
  const total = [...cats.values()].reduce((s, c) => s + c.amount, 0n);
  // đ/t = tổng / tấn giao; tấn giao = 0 → không xác định (proto buildCostChain).
  const perTonne = delivered > 0n ? moneyOf(divRound(total * 1_000_000n, delivered)) : null;
  return {
    lot_ref: lot,
    categories: [...cats.entries()].map(([category, c]) => ({ category: category as CostEntry["category"], amount: moneyOf(c.amount), mass_kg: c.mass === null ? null : kgOf(c.mass), entries: c.entries })),
    total_cost: moneyOf(total),
    revenue: moneyOf(revenue),
    delivered_kg: kgOf(delivered),
    cost_per_tonne: perTonne,
    cost_per_tonne_undetermined: perTonne === null,
    note: "Không gồm giá mua rơm (quyết toán HTX, tính riêng — proto D03), doanh thu, carbon, khấu hao.",
  };
}

// ---------------------------------------------------------------------------
// Quyết toán HTX (PM-01..04)
// ---------------------------------------------------------------------------
const settlements: CostingSettlement[] = [];

/** Số cuộn / khối lượng cân của lệnh gom (đọc measurement/fieldops qua port — giả lập). */
const ORDER_QTY: Record<string, { rolls: number; weighedKg: string | null; allBoatsWeighed: boolean }> = {
  [O.done]: { rolls: 4400, weighedKg: "96800.000", allBoatsWeighed: true },
  [O.assigned]: { rolls: 4300, weighedKg: "86260.000", allBoatsWeighed: false },
  [O.approved]: { rolls: 0, weighedKg: "91500.000", allBoatsWeighed: true },
  [O.planned]: { rolls: 1200, weighedKg: null, allBoatsWeighed: false },
};

function compute(s: CostingSettlement): void {
  const q = ORDER_QTY[s.collection_order_ref!] ?? { rolls: 0, weighedKg: null, allBoatsWeighed: false };
  s.rolls = q.rolls || null;
  s.mass_kg = q.weighedKg;
  if (!s.basis || !s.unit_price) {
    s.status = s.status === "cancelled" ? s.status : "pending_confirmation";
    s.blocked_reason = "no_contract";
    s.gross_amount = null;
    s.amount = null;
    return;
  }
  if (s.basis === "per_bale") {
    if (!q.rolls) {
      s.blocked_reason = "no_rolls";
      s.gross_amount = null;
      s.amount = null;
      return;
    }
    s.gross_amount = amountForCount(q.rolls, s.unit_price);
  } else {
    if (!q.allBoatsWeighed || !q.weighedKg) {
      s.status = "awaiting_weighing";
      s.blocked_reason = "awaiting_weighing";
      s.gross_amount = q.weighedKg ? amountForKg(q.weighedKg, s.unit_price) : null;
      s.amount = null;
      return;
    }
    s.gross_amount = amountForKg(q.weighedKg, s.unit_price);
  }
  s.blocked_reason = null;
  s.status = "pending_confirmation";
  s.amount = moneyOf(cents(s.gross_amount) - cents(s.deductions ?? "0.00"));
}

function settlement(id: string, order: string, htx: string, harvest: string, by: string): CostingSettlement {
  const c = contractFor(htx, harvest);
  const s: CostingSettlement = { id, kind: "original", status: "pending_confirmation", collection_order_ref: order, htx_ref: htx, harvest_date: harvest, contract_ref: c?.contract_id ?? null, contract_version: c?.version ?? null, basis: c?.basis ?? null, price_source: c ? "contract" : null, unit_price: c?.unit_price ?? null, rolls: null, mass_kg: null, gross_amount: null, deductions: "0.00", amount: null, blocked_reason: null, due_on: null, period_ref: periodOf(M0)?.id ?? null, prepared_by: by, approved_by: null, approved_at: null, supersedes_id: null, rule_version: "costing.settlement.v1" };
  compute(s);
  settlements.push(s);
  return s;
}
settlement(CO.settlementPerBale, O.done, HTX.vinhBinh, todayVn(-12), ACTORS.keToan);
settlement(CO.settlementPerTonne, O.assigned, HTX.tanHiep, todayVn(-6), ACTORS.keToan);
settlement(CO.settlementNoContract, O.approved, HTX.giongRieng, todayVn(-20), ACTORS.keToan);

function settlementOr404(id: string) {
  return settlements.find((s) => s.id === id) ?? problem(404, "costing.settlement_not_found", "Settlement not found");
}

function openPeriodForNow(): CostingPeriod | Response {
  const p = ensurePeriod(M0);
  return p.status === "open" ? p : problem(409, "costing.period_not_open", "Period not open");
}

export const costingRoutes: Route[] = [
  route("GET", "/v1/costing/lots/:id/cost", ({ params: p, persona }) => {
    if (!hasRole(persona, ...READ)) return forbidden();
    const c = lotCost(p[0]!);
    return c ? json(c) : problem(404, "costing.lot_not_found", "Lot has no cost entries");
  }),
  route("GET", "/v1/costing/lots/:id/entries", ({ params: p, url, persona }) => {
    if (!hasRole(persona, ...READ)) return forbidden();
    const after = Number(url.searchParams.get("after_seq") ?? "0");
    const list = entries.filter((e) => e.lot_ref === p[0] && (e.seq ?? 0) > after).sort((a, b) => a.seq! - b.seq!);
    return json({ items: list, next_after_seq: null });
  }),
  route("POST", "/v1/costing/entries", ({ body, persona }) => {
    if (!hasRole(persona, COST)) return forbidden();
    const errs: { field: string; reason: string }[] = [];
    const lot = str(body, "lot_ref");
    if (!UUID_RE.test(lot)) errs.push({ field: "lot_ref", reason: "UUID lô" });
    const cat = str(body, "category");
    if (!["collection_labour", "consumables", "transport_road", "transport_waterway", "hub_processing", "storage", "handling", "loss"].includes(cat)) errs.push({ field: "category", reason: "Nhóm chi phí không ghi tay được" });
    const amount = str(body, "amount");
    if (!/^\d+(\.\d{1,2})?$/.test(amount) || cents(amount) <= 0n) errs.push({ field: "amount", reason: "Tiền VND > 0, tối đa 2 số lẻ" });
    const mass = str(body, "mass_kg");
    if (mass && !/^\d+(\.\d{1,3})?$/.test(mass)) errs.push({ field: "mass_kg", reason: "kg, tối đa 3 số lẻ" });
    if (errs.length) return validation(errs);
    const at = str(body, "event_time") || nowIso();
    const code = new Date(Date.parse(at) + 7 * 3_600_000).toISOString().slice(0, 7);
    const per = ensurePeriod(code);
    if (per.status !== "open") return problem(409, "costing.period_not_open", "Period not open");
    const e = entry(newId(0x2e), lot, code, cat as CostEntry["category"], moneyOf(cents(amount)), mass ? kgOf(grams(mass)) : null, (str(body, "dimension_kind") || "none") as CostEntry["dimension_kind"], str(body, "dimension_ref") || null, "manual", null, persona.actor, 0, { note: str(body, "note") || null, event_time: at, recorded_at: nowIso() });
    return created(e);
  }),
  route("POST", "/v1/costing/entries/:id/adjust", ({ params: p, body, persona }) => {
    if (!hasRole(persona, COST)) return forbidden();
    const orig = entries.find((e) => e.id === p[0]);
    if (!orig) return problem(404, "costing.entry_not_found", "Entry not found");
    if (entries.some((e) => e.supersedes_id === orig.id)) return problem(409, "costing.entry_superseded", "Entry superseded");
    const amount = str(body, "amount");
    if (!/^\d+(\.\d{1,2})?$/.test(amount)) return validation([{ field: "amount", reason: "Tiền VND, tối đa 2 số lẻ" }]);
    const reason = reasonOf(body);
    if (reason instanceof Response) return reason;
    const open = openPeriodForNow();
    if (open instanceof Response) return open;
    const origPeriod = periodOf(orig.period_code!);
    const e = entry(newId(0x2e), orig.lot_ref!, open.code!, orig.category, moneyOf(cents(amount)), orig.mass_kg ?? null, orig.dimension_kind, orig.dimension_ref ?? null, "adjusting", orig.source_ref ?? null, persona.actor, 0, {
      supersedes_id: orig.id,
      delta_amount: moneyOf(cents(amount) - cents(orig.amount!)),
      entry_kind: "adjusting",
      correction_reason: reason,
      late_from: origPeriod && origPeriod.status !== "open" ? orig.period_code : null,
      event_time: nowIso(),
      recorded_at: nowIso(),
    });
    return created(e);
  }),
  route("GET", "/v1/costing/allocations/:id", ({ params: p, persona }) => {
    if (!hasRole(persona, ...READ)) return forbidden();
    const a = allocations.find((x) => x.id === p[0]);
    return a ? json(a) : problem(404, "costing.allocation_not_found", "Allocation not found");
  }),
  route("POST", "/v1/costing/allocations/:id/recompute", ({ params: p, body, persona }) => {
    if (!hasRole(persona, COST)) return forbidden();
    const a = allocations.find((x) => x.id === p[0]);
    if (!a) return problem(404, "costing.allocation_not_found", "Allocation not found");
    if (a.superseded_by) return problem(409, "costing.allocation_superseded", "Allocation superseded");
    const reason = reasonOf(body);
    if (reason instanceof Response) return reason;
    const asOf = str(body, "as_of");
    // Tính lại với tham số theo quyết định đã ghi đến as_of: trước khi kết quả được tính → không đổi (cùng băm).
    if (asOf && Date.parse(asOf) <= Date.parse(a.recorded_at!)) return json({ ...a, changed: false });
    const pin = costParam("costing.allocation.key");
    const n: CostingAllocation = { ...a, id: newId(0x2f), param_pins: { "costing.allocation.key": pin.seq }, params_as_of: asOf || nowIso(), supersedes_id: a.id, superseded_by: null, correction_reason: reason, result_sha256: sha256Text(`${a.result_sha256}|${nowIso()}`), recorded_at: nowIso() };
    a.superseded_by = n.id;
    allocations.push(n);
    return created({ ...n, changed: true });
  }),
  route("GET", "/v1/costing/sources/:id/allocations", ({ params: p, persona }) => {
    if (!hasRole(persona, ...READ)) return forbidden();
    return json({ items: allocations.filter((a) => a.source_ref === p[0]) });
  }),
  route("GET", "/v1/costing/collections/:id/conversion", ({ params: p, persona }) => {
    if (!hasRole(persona, ...READ)) return forbidden();
    const q = ORDER_QTY[p[0]!];
    if (!q?.rolls || !q.weighedKg) return problem(404, "costing.conversion_not_found", "No conversion");
    return json({ id: sid(0x32, 1), collection_order_ref: p[0], rolls: q.rolls, weighed_kg: q.weighedKg, weigh_point: "HUB_IN", yield_kg_per_roll: kgOf(divRound(grams(q.weighedKg), BigInt(q.rolls))), moisture_rule: "none", converted_kg: q.weighedKg, inputs_sha256: sha256Text(`${p[0]}|${q.rolls}|${q.weighedKg}`), param_pins: { "costing.moisture.base_pct": 0 }, rule_version: "costing.conversion.v1", supersedes_id: null });
  }),
  route("GET", "/v1/costing/periods", ({ persona }) => {
    if (!hasRole(persona, ...READ)) return forbidden();
    return json({ items: [...periods].sort((a, b) => b.code!.localeCompare(a.code!)).map((p) => ({ ...p, summary: null })), next_before: null });
  }),
  route("POST", "/v1/costing/periods", ({ body, persona }) => {
    if (!hasRole(persona, COST, CLOSER)) return forbidden();
    const code = str(body, "code");
    if (!/^\d{4}-(0[1-9]|1[0-2])$/.test(code)) return validation([{ field: "code", reason: "YYYY-MM" }]);
    return created(ensurePeriod(code));
  }),
  route("GET", "/v1/costing/periods/:id", ({ params: p, persona }) => {
    if (!hasRole(persona, ...READ)) return forbidden();
    const per = periods.find((x) => x.id === p[0]);
    return per ? json(per) : problem(404, "costing.period_not_found", "Period not found");
  }),
  route("POST", "/v1/costing/periods/:id/close", ({ params: p, persona }) => {
    if (!hasRole(persona, CLOSER)) return forbidden();
    const per = periods.find((x) => x.id === p[0]);
    if (!per) return problem(404, "costing.period_not_found", "Period not found");
    if (per.status !== "open") return problem(409, "costing.period_not_open", "Period not open");
    if (Date.parse(per.to!) > Date.now()) return problem(409, "costing.period_not_ended", "Period not ended");
    // R-56: người đóng kỳ ≠ người lập bút toán điều chỉnh của kỳ.
    if (entries.some((e) => e.period_code === per.code && e.entry_kind === "adjusting" && e.actor_ref === persona.actor)) return selfApproval();
    per.status = "closing";
    const list = entries.filter((e) => e.period_code === per.code);
    const job = { job_id: newId(0x31), kind: "costing.period_close" as const, period_id: per.id, period_code: per.code, status: "queued" as const, requested_by: persona.actor, requested_at: nowIso(), as_of: nowIso(), input_snapshot_sha256: sha256Text(JSON.stringify(list.map((e) => [e.id, e.delta_amount]))), snapshot_counts: { entries: list.length, settlements: settlements.filter((s) => s.period_ref === per.id).length, carbon: 0 }, error_code: null, completed_at: null, polls: 0 };
    jobs.push(job);
    const { polls: _polls, ...out } = job;
    void _polls;
    return json(out, 202);
  }),
  route("GET", "/v1/costing/period-close-jobs/:id", ({ params: p, persona }) => {
    if (!hasRole(persona, ...READ)) return forbidden();
    const j = jobs.find((x) => x.job_id === p[0]);
    if (!j) return problem(404, "costing.job_not_found", "Job not found");
    // worker-internal giả lập: job hoàn tất ở lần hỏi thứ hai.
    j.polls += 1;
    if (j.status === "queued" && j.polls >= 2) {
      const per = periods.find((x) => x.id === j.period_id)!;
      closeNow(per, j.requested_by!, newId(0x31));
      j.status = "completed";
      j.completed_at = nowIso();
      onClosed(per);
    }
    const { polls: _polls, ...out } = j;
    void _polls;
    return json(out);
  }),
  route("GET", "/v1/costing/settlements", ({ url, persona }) => {
    if (!hasRole(persona, ...READ)) return forbidden();
    const st = url.searchParams.get("status");
    const ord = url.searchParams.get("collection_order_ref");
    return json({ items: settlements.filter((s) => (!st || s.status === st) && (!ord || s.collection_order_ref === ord)).reverse() });
  }),
  route("POST", "/v1/costing/settlements", ({ body, persona }) => {
    if (!hasRole(persona, COST)) return forbidden();
    const order = str(body, "collection_order_ref");
    const htx = str(body, "htx_ref");
    const harvest = str(body, "harvest_date");
    const errs: { field: string; reason: string }[] = [];
    if (!UUID_RE.test(order)) errs.push({ field: "collection_order_ref", reason: "UUID lệnh gom" });
    if (!UUID_RE.test(htx)) errs.push({ field: "htx_ref", reason: "UUID HTX" });
    if (!/^\d{4}-\d{2}-\d{2}$/.test(harvest)) errs.push({ field: "harvest_date", reason: "YYYY-MM-DD" });
    const ded = str(body, "deductions");
    if (ded && !/^\d+(\.\d{1,2})?$/.test(ded)) errs.push({ field: "deductions", reason: "Tiền VND ≥ 0" });
    if (ded && cents(ded) > 0n && str(body, "reason").length < 10) errs.push({ field: "reason", reason: "Có khấu trừ cần lý do ≥ 10 ký tự" });
    if (errs.length) return validation(errs);
    if (settlements.some((s) => s.kind === "original" && s.collection_order_ref === order && s.status !== "cancelled")) return problem(409, "costing.settlement_exists", "Settlement exists");
    const open = openPeriodForNow();
    if (open instanceof Response) return open;
    const s = settlement(newId(0x30), order, htx, harvest, persona.actor);
    if (ded) {
      s.deductions = moneyOf(cents(ded));
      compute(s);
    }
    return created(s);
  }),
  route("GET", "/v1/costing/settlements/:id", ({ params: p, persona }) => {
    if (!hasRole(persona, ...READ)) return forbidden();
    const s = settlementOr404(p[0]!);
    return s instanceof Response ? s : json(s);
  }),
  route("POST", "/v1/costing/settlements/:id/price", ({ params: p, body, persona }) => {
    if (!hasRole(persona, COST)) return forbidden();
    const s = settlementOr404(p[0]!);
    if (s instanceof Response) return s;
    if (s.blocked_reason !== "no_contract" || s.status !== "pending_confirmation") return problem(409, "costing.settlement_transition", "Price only for no-contract settlements");
    const basis = str(body, "basis");
    const price = str(body, "unit_price");
    const errs: { field: string; reason: string }[] = [];
    if (basis !== "per_tonne" && basis !== "per_bale") errs.push({ field: "basis", reason: "per_tonne | per_bale" });
    if (!isPrice(price)) errs.push({ field: "unit_price", reason: "Giá > 0, tối đa 4 số lẻ" });
    if (errs.length) return validation(errs);
    const reason = reasonOf(body);
    if (reason instanceof Response) return reason;
    s.basis = basis as "per_tonne";
    s.unit_price = priceOf(priceUnits(price));
    s.price_source = "manual";
    s.prepared_by = persona.actor; // người nhập giá trở thành người lập (PM-03)
    compute(s);
    return created(s);
  }),
  route("POST", "/v1/costing/settlements/:id/pay", ({ params: p, persona }) => {
    if (!hasRole(persona, COST, CHIEF)) return forbidden();
    const s = settlementOr404(p[0]!);
    if (s instanceof Response) return s;
    if (s.status !== "approved") return problem(409, "costing.settlement_transition", "Only approved settlements can be paid");
    s.status = "paid";
    return created(s);
  }),
  route("POST", "/v1/costing/settlements/:id/cancel", ({ params: p, body, persona }) => {
    if (!hasRole(persona, COST)) return forbidden();
    const s = settlementOr404(p[0]!);
    if (s instanceof Response) return s;
    const reason = reasonOf(body);
    if (reason instanceof Response) return reason;
    if (s.status === "approved") return problem(409, "costing.settlement_frozen", "Settlement frozen");
    if (s.status === "paid") return problem(409, "costing.settlement_paid", "Settlement paid");
    if (s.status === "cancelled") return problem(409, "costing.settlement_transition", "Already cancelled");
    s.status = "cancelled";
    return created(s);
  }),
  route("POST", "/v1/costing/settlements/:id/adjust", ({ params: p, body, persona }) => {
    if (!hasRole(persona, COST)) return forbidden();
    const s = settlementOr404(p[0]!);
    if (s instanceof Response) return s;
    if (s.status !== "approved" && s.status !== "paid") return problem(409, "costing.settlement_transition", "Adjust only approved/paid");
    const amount = str(body, "amount");
    if (!/^-?\d+(\.\d{1,2})?$/.test(amount) || cents(amount) === 0n) return validation([{ field: "amount", reason: "Tiền VND ≠ 0 (âm = đảo)" }]);
    const reason = reasonOf(body);
    if (reason instanceof Response) return reason;
    const a: CostingSettlement = { ...s, id: newId(0x30), kind: "adjustment", status: "pending_confirmation", gross_amount: moneyOf(cents(amount)), deductions: "0.00", amount: moneyOf(cents(amount)), blocked_reason: null, due_on: null, prepared_by: persona.actor, approved_by: null, approved_at: null, supersedes_id: s.id };
    settlements.push(a);
    return created(a);
  }),
  route("POST", "/v1/costing/carbon-revenue", ({ body, persona }) => {
    if (!hasRole(persona, COST)) return forbidden();
    const amount = str(body, "amount");
    if (!/^\d+(\.\d{1,2})?$/.test(amount) || cents(amount) <= 0n) return validation([{ field: "amount", reason: "Tiền VND > 0" }]);
    const f = costParam("costing.carbon.split.farmers_pct");
    const a = costParam("costing.carbon.split.authority_pct");
    const v = costParam("costing.carbon.split.verifier_pct");
    if (percentHundredths(f.value) + percentHundredths(a.value) + percentHundredths(v.value) !== 10_000n) return problem(422, "costing.carbon_split_invalid", "Split does not sum to 100");
    const open = openPeriodForNow();
    if (open instanceof Response) return open;
    const r = costParam("costing.carbon.retained_pct");
    const s = splitCarbon(moneyOf(cents(amount)), r.value, f.value, a.value);
    return created({ id: newId(0x33), period_code: open.code, revenue_amount: moneyOf(cents(amount)), retained_amount: s.retained, farmers_amount: s.farmers, authority_amount: s.authority, verifier_amount: s.verifier, param_pins: { "costing.carbon.retained_pct": r.seq, "costing.carbon.split.farmers_pct": f.seq, "costing.carbon.split.authority_pct": a.seq, "costing.carbon.split.verifier_pct": v.seq }, label: r.seq + f.seq + a.seq + v.seq === 0 ? "tạm, chờ khách" : "đã duyệt" });
  }),
  route("GET", "/v1/costing/params", ({ persona }) => (hasRole(persona, ...READ) ? json({ items: PARAM_DEFS.map((d) => paramView(d.key)) }) : forbidden())),
];

export const costingAdminRoutes: Route[] = [
  route("GET", "/v1/costing/params", ({ persona }) => (hasRole(persona, ...READ) ? json({ items: PARAM_DEFS.map((d) => paramView(d.key)) }) : forbidden())),
  route("POST", "/v1/costing/params", ({ body, persona }) => {
    if (!hasRole(persona, COST)) return forbidden();
    const key = str(body, "key");
    const d = PARAM_DEFS.find((x) => x.key === key);
    if (!d) return problem(404, "costing.param_not_found", "Param not found");
    const value = str(body, "value");
    const bad = paramCheck(d.kind, value);
    if (bad) return validation([{ field: "value", reason: bad }]);
    const reason = reasonOf(body);
    if (reason instanceof Response) return reason;
    const list = paramVersions[key]!;
    if (list.some((x) => x.decision === null)) return problem(409, "costing.param_pending_exists", "Pending exists");
    const v: CostingParamVersion = { key, seq: list.length + 1, value, reason, effective_from: str(body, "effective_from") || nowIso(), proposed_by: persona.actor, recorded_at: nowIso(), decision: null, decided_by: null, decided_at: null, decision_reason: null };
    list.push(v);
    return created(v);
  }),
  route("POST", "/v1/costing/params/:key/versions/:seq/decision", ({ params: p, body, persona }) => {
    if (!hasRole(persona, CHIEF)) return forbidden();
    const list = paramVersions[p[0]!];
    if (!list) return problem(404, "costing.param_not_found", "Param not found");
    const v = list.find((x) => x.seq === Number(p[1]));
    if (!v) return problem(404, "costing.param_version_not_found", "Version not found");
    if (v.decision !== null) return problem(409, "costing.param_not_pending", "Not pending");
    if (v.proposed_by === persona.actor) return selfApproval();
    const d = str(body, "decision");
    if (d !== "approved" && d !== "rejected") return validation([{ field: "decision", reason: "approved | rejected" }]);
    if (d === "rejected") {
      const r = reasonOf(body);
      if (r instanceof Response) return r;
      v.decision_reason = r;
    }
    v.decision = d;
    v.decided_by = persona.actor;
    v.decided_at = nowIso();
    return created(v);
  }),
  route("GET", "/v1/costing/settlements/:id", ({ params: p, persona }) => {
    if (!hasRole(persona, ...READ)) return forbidden();
    const s = settlementOr404(p[0]!);
    return s instanceof Response ? s : json(s);
  }),
  route("POST", "/v1/costing/settlements/:id/approve", ({ params: p, persona }) => {
    if (!hasRole(persona, CHIEF)) return forbidden();
    const s = settlementOr404(p[0]!);
    if (s instanceof Response) return s;
    if (s.status === "approved" || s.status === "paid") return problem(409, "costing.settlement_frozen", "Settlement frozen");
    if (s.status === "cancelled") return problem(409, "costing.settlement_transition", "Cancelled");
    if (s.prepared_by === persona.actor) return selfApproval();
    if (s.blocked_reason || !s.amount || cents(s.amount) === 0n || (s.kind === "original" && cents(s.amount) < 0n)) return problem(409, "costing.settlement_not_ready", "Not ready");
    const term = costParam("costing.settlement.payment_term_days");
    s.status = "approved";
    s.approved_by = persona.actor;
    s.approved_at = nowIso();
    s.due_on = todayVn(Number(term.value));
    return created(s);
  }),
  route("POST", "/v1/costing/projections/rebuild", ({ persona }) => (hasRole(persona, CHIEF, COST) ? json({ lot_cost_current: new Set(entries.map((e) => e.lot_ref)).size, settlement_current: settlements.length }) : forbidden())),
];
