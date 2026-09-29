// Đọc khối `result` (contracts/optimizer/result.v1 — scenarioResult) để hiển thị. Khối là object tự do trong OpenAPI
// nên đọc phòng thủ: thiếu trường → null, không ném lỗi. Số là number JSON (optimizer) hoặc chuỗi decimal.

export interface SimSummary {
  deliveredTons: number | null;
  coveragePct: number | null;
  costPerTon: number | null;
  totalLogisticsCost: number | null;
  capex: number | null;
  tco: number | null;
  tcoPerTon: number | null;
  roiPct: number | null;
  paybackYears: number | null;
  paybackStatus: string | null;
  lifecycleYears: number | null;
  baselineCostPerTon: number | null;
  baselineLabel: string | null;
  baselineAvailable: boolean;
  baselineDeliveredTons: number | null;
  flows: { fieldHub: number | null; hubPlant: number | null; direct: number | null; uncollected: number | null };
  hubs: { code: string; deliveredTons: number | null; radiusKm: number | null; distanceKm: number | null; distanceSource: string | null; peakInventory: number | null; utilizationPct: number | null }[];
  unapproved: { code: string; number: number | null; name: string }[];
  warnings: string[];
  plantWarning: string | null;
  parameterSet: string | null;
}

type Obj = Record<string, unknown>;
const obj = (v: unknown): Obj => (v && typeof v === "object" && !Array.isArray(v) ? (v as Obj) : {});
const arr = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);
export function num(v: unknown): number | null {
  if (typeof v === "number") return Number.isFinite(v) ? v : null;
  if (typeof v === "string" && v.trim() !== "") {
    const n = Number(v);
    return Number.isFinite(n) ? n : null;
  }
  return null;
}
const str = (v: unknown): string | null => (typeof v === "string" ? v : null);

export function summarize(result: unknown): SimSummary {
  const r = obj(result);
  const costs = obj(r.costs);
  const fin = obj(r.financial);
  const base = obj(r.baseline);
  const flow = obj(r.flow);
  return {
    deliveredTons: num(r.delivered_tons),
    coveragePct: num(r.plant_demand_coverage_pct),
    costPerTon: num(costs.cost_per_ton),
    totalLogisticsCost: num(costs.total_delivered_logistics_cost),
    capex: num(fin.capex) ?? num(obj(r.capex).total),
    tco: num(fin.tco),
    tcoPerTon: num(fin.tco_per_ton),
    roiPct: num(fin.roi_pct),
    paybackYears: num(fin.payback_years),
    paybackStatus: str(fin.payback_status),
    lifecycleYears: num(fin.lifecycle_years),
    baselineCostPerTon: num(fin.baseline_cost_per_ton) ?? num(base.baseline_cost_per_ton),
    baselineLabel: str(fin.baseline_source_label),
    baselineAvailable: base.available !== false && Object.keys(base).length > 0,
    baselineDeliveredTons: num(base.delivered_tons),
    flows: { fieldHub: num(flow.field_hub_tons), hubPlant: num(flow.hub_plant_tons), direct: num(flow.direct_field_plant_tons), uncollected: num(flow.uncollected_tons) },
    hubs: arr(r.hubs).map((h) => {
      const x = obj(h);
      const f = obj(x.flow);
      return {
        code: str(x.code) ?? "—",
        deliveredTons: num(x.delivered_tons),
        radiusKm: num(x.service_radius_km),
        distanceKm: num(x.distance_to_plant_km),
        distanceSource: str(x.distance_to_plant_source_label),
        peakInventory: num(f.peak_inventory_tons),
        utilizationPct: num(f.utilization_pct),
      };
    }),
    unapproved: arr(r.unapproved_parameters).map((p) => {
      const x = obj(p);
      return { code: str(x.code) ?? "", number: num(x.number), name: str(x.name) ?? str(x.code) ?? "" };
    }),
    warnings: arr(r.warnings).filter((w): w is string => typeof w === "string"),
    plantWarning: str(r.plant_demand_warning),
    parameterSet: str(r.parameter_set_version),
  };
}

export const PAYBACK_STATUS: Record<string, string> = {
  hoan_von: "Hoàn vốn",
  khong_hoan_von: "Không hoàn vốn",
  khong_hoan_von_trong_ky: "Không hoàn vốn trong vòng đời",
  thieu_baseline: "Thiếu baseline",
};

const vnd = new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 0 });

/** Tiền đồng: ≥ 1 tỷ hiển thị "12,5 tỷ đ", còn lại "612.000 đ". */
export function formatVnd(v: number | string | null | undefined): string {
  const n = typeof v === "string" ? num(v) : v;
  if (n === null || n === undefined) return "—";
  if (Math.abs(n) >= 1e9) return `${new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 2 }).format(n / 1e9)} tỷ đ`;
  if (Math.abs(n) >= 1e6) return `${new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 1 }).format(n / 1e6)} triệu đ`;
  return `${vnd.format(n)} đ`;
}

/** Chênh lệch chi phí/tấn so với No-Hub (âm = tiết kiệm). */
export function savingVsBaseline(costPerTon: number | null, baseline: number | null): { pct: number; saving: boolean } | null {
  if (costPerTon === null || baseline === null || baseline === 0) return null;
  const pct = ((costPerTon - baseline) / baseline) * 100;
  return { pct, saving: pct < 0 };
}
