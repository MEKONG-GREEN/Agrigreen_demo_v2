// Phép toán đối soát lô (mục 13.1) — bản TypeScript CỦA QUY TẮC measurement.reconcile.v1
// (internal/mkg/measurement/domain.Reconcile). Dùng cho backend giả lập và kiểm thử; số liệu thật
// luôn do api-internal tính. Không float: khối lượng qua lib/kg (BigInt gam).
import type { LossBand, LossNormValues, ReconCommercial, ReconInternal, ReconRollCheck, ToleranceValues } from "@/api/types";
import { absPctGreater, applyPct, cmpKg, grams, mulKg, pctDiff, pctOfWhole, perUnit, subKg } from "./kg";

export type Route = "HUB" | "DIRECT";
export type PricingBasis = "per_tonne" | "per_bale";
export type Outcome = "within_tolerance" | "loss_alert" | "dispute_opened" | "direct_aggregate";

export interface ReconcileInput {
  route: Route;
  basis: PricingBasis;
  hubIn: string | null;
  hubOut: string | null;
  customerIn: string | null;
  hubInMoisture: string | null;
  /** Số cuộn đã xếp ghe/xe (RB-6) và kg/cuộn học được; null = chưa có số cuộn. */
  rolls: { rolls: number; kgPerRoll: string } | null;
  tolerance: ToleranceValues;
  lossNorm: LossNormValues;
}

export interface ReconcileOutput {
  internal: ReconInternal;
  commercial: ReconCommercial;
  roll_check: ReconRollCheck;
  yield_kg_per_roll: string | null;
  yield_aggregated: boolean;
  outcome: Outcome;
}

export class IncompleteError extends Error {
  constructor() {
    super("measurement: lô chưa đủ phiếu cân để đối soát");
  }
}

/** Mặc định TẠM (prototype plant-weighing: cờ > 5 %, nghiêm trọng > 15 %) — chờ khách C-4. */
export const DEFAULT_TOLERANCE: ToleranceValues = {
  commercial_flag_pct: "5.00",
  commercial_critical_pct: "15.00",
  roll_flag_pct: "5.00",
  roll_critical_pct: "15.00",
};

/** Định mức hao hụt sấy TẠM (không có trong prototype — chờ khách). */
export const DEFAULT_LOSS_NORM: LossNormValues = {
  bands: [
    { moisture_from: "0.00", moisture_to: "15.00", loss_pct: "1.00" },
    { moisture_from: "15.00", moisture_to: "20.00", loss_pct: "3.00" },
    { moisture_from: "20.00", moisture_to: "25.00", loss_pct: "6.00" },
    { moisture_from: "25.00", moisture_to: "100.00", loss_pct: "10.00" },
  ],
  default_loss_pct: "3.00",
  abnormal_alert_pct: "1.00",
};

export const DEFAULT_KG_PER_ROLL = "20";

/** Tỷ lệ hao hụt dự kiến theo độ ẩm HUB_IN: khoảng [from, to); khoảng cuối nhận cả biên trên. */
export function lossPctFor(norm: LossNormValues, moisture: string | null): string {
  if (moisture === null) return norm.default_loss_pct;
  const m = grams(moisture);
  const bands: LossBand[] = norm.bands;
  for (let i = 0; i < bands.length; i++) {
    const b = bands[i]!;
    const from = grams(b.moisture_from);
    const to = grams(b.moisture_to);
    const last = i === bands.length - 1;
    if (m >= from && (m < to || (last && m === to))) return b.loss_pct;
  }
  return norm.default_loss_pct;
}

/** within | flag | critical theo |pct| > ngưỡng. */
export function classify(pct: string, flag: string, crit: string): "within" | "flag" | "critical" {
  if (absPctGreater(pct, crit)) return "critical";
  if (absPctGreater(pct, flag)) return "flag";
  return "within";
}

export function reconcile(inp: ReconcileInput): ReconcileOutput {
  let internal: ReconInternal;
  let commercial: ReconCommercial;
  let outcome: Outcome;
  let basis: string;
  let aggregated = false;
  if (inp.route === "DIRECT") {
    if (!inp.customerIn) throw new IncompleteError();
    internal = { status: "not_applicable", loss_kg: null, loss_norm_pct: null, expected_loss_kg: null, abnormal_loss_kg: null };
    commercial = { status: "not_applicable", diff_kg: null, diff_pct: null };
    outcome = "direct_aggregate";
    aggregated = true;
    basis = inp.customerIn;
  } else {
    if (!inp.hubIn || !inp.hubOut || !inp.customerIn) throw new IncompleteError();
    if (grams(inp.hubIn) === 0n || grams(inp.hubOut) === 0n) throw new IncompleteError();
    const actual = subKg(inp.hubIn, inp.hubOut);
    const normPct = lossPctFor(inp.lossNorm, inp.hubInMoisture);
    const expected = applyPct(inp.hubIn, normPct);
    const abnormal = cmpKg(actual, expected) > 0 ? subKg(actual, expected) : "0.000";
    const abnormalPct = pctOfWhole(abnormal, inp.hubIn);
    const status: ReconInternal["status"] =
      grams(actual) < 0n ? "gain" : absPctGreater(abnormalPct, inp.lossNorm.abnormal_alert_pct) ? "abnormal" : "normal";
    internal = { status, loss_kg: actual, loss_norm_pct: normPct, expected_loss_kg: expected, abnormal_loss_kg: abnormal };
    const diffPct = pctDiff(inp.customerIn, inp.hubOut);
    const cStatus = classify(diffPct, inp.tolerance.commercial_flag_pct, inp.tolerance.commercial_critical_pct);
    commercial = { status: cStatus, diff_kg: subKg(inp.customerIn, inp.hubOut), diff_pct: diffPct };
    outcome = cStatus !== "within" ? "dispute_opened" : status !== "normal" ? "loss_alert" : "within_tolerance";
    basis = inp.hubIn;
  }

  let roll: ReconRollCheck = { status: "pending_rolls", rolls: null, kg_per_roll_estimate: null, estimate_kg: null, variance_pct: null };
  let yieldKg: string | null = null;
  if (inp.rolls && inp.rolls.rolls > 0) {
    const kpr = grams(inp.rolls.kgPerRoll) === 0n ? DEFAULT_KG_PER_ROLL : inp.rolls.kgPerRoll;
    const est = mulKg(kpr, inp.rolls.rolls);
    const variance = pctDiff(basis, est);
    yieldKg = perUnit(basis, inp.rolls.rolls);
    roll = {
      status: inp.basis === "per_bale" ? "informational" : classify(variance, inp.tolerance.roll_flag_pct, inp.tolerance.roll_critical_pct),
      rolls: inp.rolls.rolls,
      kg_per_roll_estimate: mulKg(kpr, 1),
      estimate_kg: est,
      variance_pct: variance,
    };
  }
  return { internal, commercial, roll_check: roll, yield_kg_per_roll: yieldKg, yield_aggregated: aggregated, outcome };
}
