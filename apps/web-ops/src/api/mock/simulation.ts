// GIẢ LẬP pf/simulation (CGH FN-02/FN-11/FN-14, mục 12) — hình dạng theo contracts/openapi/application.yaml.
// Kết quả là khối `result` rút gọn của contracts/optimizer/result.v1 với SỐ LIỆU TỔNG HỢP (không phải mô hình tối ưu thật).
// Việc mô phỏng: 202 → queued → running → completed sau ~3 giây (theo đồng hồ, không cần worker).
import type { SimCandidateHub, SimChange, SimCompareRow, SimScenario, SimScenarioHub, SimSimulation } from "../types";
import { uuidv7 } from "../idempotency";
import { haversineM, type LngLat } from "@/gis/geometry";
import { hasRole, iso, json, nowIso, opaque, pageSlice, problem, reasonOf, validation, type Route } from "./http";

const ANALYSTS = ["simulation_analyst", "platform_admin"];
const hex = (n: number, prefix: string) => `01927b3e-${prefix}-7000-8000-${n.toString(16).padStart(12, "0")}`;

const HUBS: SimCandidateHub[] = [
  { id: hex(1, "5b00"), code: "HUB-0001", name: "Hub Long Xuyên", location: { lat: 10.38, lng: 105.43 }, unit_code: "91", status: "active", retired_reason: null, created_at: iso(40) },
  { id: hex(2, "5b00"), code: "HUB-0002", name: "Hub Cao Lãnh", location: { lat: 10.46, lng: 105.63 }, unit_code: "82", status: "active", retired_reason: null, created_at: iso(38) },
  { id: hex(3, "5b00"), code: "HUB-0003", name: "Hub Thốt Nốt", location: { lat: 10.27, lng: 105.53 }, unit_code: "92", status: "active", retired_reason: null, created_at: iso(30) },
  { id: hex(4, "5b00"), code: "HUB-0004", name: "Hub Châu Thành (cũ)", location: { lat: 10.2, lng: 105.9 }, unit_code: "86", status: "retired", retired_reason: "Không còn quỹ đất thuê theo kế hoạch 2026", created_at: iso(60) },
];

interface ScenarioState {
  s: SimScenario;
  changes: SimChange[];
  allHubs: SimScenarioHub[];
}
interface SimState {
  sim: SimSimulation;
  approved: boolean;
  doneAt: number;
  result: Record<string, unknown> | null;
  failed: boolean;
}
const SCENARIOS: ScenarioState[] = [];
const SIMS: SimState[] = [];
const PLANT: LngLat = [105.78, 10.03];

function change(st: ScenarioState, kind: SimChange["kind"], actor: string, extra: Partial<SimChange> = {}): SimChange {
  const c: SimChange = { id: uuidv7(), seq: st.changes.length + 1, kind, hub_id: null, simulation_id: null, payload: {}, event_time: nowIso(), recorded_at: nowIso(), supersedes_id: null, correction_reason: null, actor_ref: actor, ...extra };
  st.changes.push(c);
  st.s.version = c.seq;
  st.s.updated_at = c.recorded_at;
  return c;
}

function newScenario(name: string, lifecycle: number, discounted: boolean, actor: string, at = nowIso()): ScenarioState {
  const st: ScenarioState = {
    s: {
      id: uuidv7(),
      code: `KB-${String(SCENARIOS.length + 1).padStart(4, "0")}`,
      name,
      plant: { plant_ref: null, location: { lat: PLANT[1], lng: PLANT[0] }, annual_demand_tons: "60000" },
      lifecycle_years: lifecycle,
      discounted,
      baseline_mode: "no_hub",
      baseline_manual_cost_per_ton: null,
      baseline_scenario_id: null,
      reference_year: 2026,
      version: 0,
      hubs: [],
      created_at: at,
      updated_at: at,
    },
    changes: [],
    allHubs: [],
  };
  change(st, "created", actor);
  SCENARIOS.push(st);
  return st;
}

function attach(st: ScenarioState, hub: SimCandidateHub, cfg: Partial<SimScenarioHub>, actor: string): void {
  const existing = st.s.hubs.find((h) => h.hub_id === hub.id);
  if (existing) {
    Object.assign(existing, Object.fromEntries(Object.entries(cfg).filter(([, v]) => v !== undefined)));
    change(st, "hub_updated", actor, { hub_id: hub.id, payload: opaque({ ...existing }) });
    return;
  }
  const sh: SimScenarioHub = {
    scenario_hub_id: uuidv7(),
    hub_id: hub.id,
    hub_code: hub.code,
    service_radius_km: cfg.service_radius_km ?? "25",
    design_capacity_tons: cfg.design_capacity_tons ?? "30000",
    barge_payload_tons: cfg.barge_payload_tons ?? null,
    mode_field_hub: cfg.mode_field_hub ?? "auto",
    mode_hub_plant: cfg.mode_hub_plant ?? "auto",
    land_mode: cfg.land_mode ?? "thue",
    sort_order: st.s.hubs.length + 1,
  };
  st.s.hubs.push(sh);
  st.allHubs.push(sh);
  change(st, "hub_attached", actor, { hub_id: hub.id, payload: opaque({ ...sh }) });
}

/** Kết quả tổng hợp theo số Hub và bán kính (đơn điệu, đủ để so sánh kịch bản trên giao diện). */
function makeResult(st: ScenarioState, simId: string, approved: boolean): Record<string, unknown> {
  const hubs = st.s.hubs;
  const radius = hubs.reduce((s, h) => s + Number(h.service_radius_km), 0) || 1;
  const delivered = Math.min(60000, 14000 * hubs.length + radius * 120);
  const capexPerHub = 4_800_000_000;
  const capex = capexPerHub * hubs.length;
  const collection = delivered * 95_000;
  const transport = hubs.reduce((s, h) => {
    const hub = HUBS.find((x) => x.id === h.hub_id);
    const km = hub ? haversineM([hub.location.lng, hub.location.lat], PLANT) / 1000 : 30;
    return s + delivered / hubs.length * km * (h.mode_hub_plant === "waterway" ? 1_100 : 2_300);
  }, 0);
  const warehouse = delivered * 28_000;
  const handling = delivered * 35_000;
  const total = collection + transport + warehouse + handling;
  const costPerTon = total / delivered;
  const baselineCost = 612_000;
  const years = st.s.lifecycle_years || 10;
  const opexYear = 1_450_000_000 * hubs.length;
  const tco = capex + (opexYear + total) * years;
  const savingsYear = (baselineCost - costPerTon) * delivered;
  const payback = savingsYear > 0 ? Math.ceil(capex / savingsYear) : null;
  const roi = ((savingsYear * years - capex) / capex) * 100;
  const r = (n: number) => Math.round(n);
  return {
    engine_version: "optimizer-1.4.0 (giả lập)",
    scenario_id: st.s.id,
    scenario_code: st.s.code,
    status: approved ? "chinh_thuc_du_dieu_kien" : "tham_khao",
    parameter_set_version: approved ? "PS-2026-09 (đã duyệt)" : "PS-2026-09-draft",
    hub_count: hubs.length,
    total_available_tons: r(delivered * 1.35),
    collectable_tons: r(delivered * 1.1),
    delivered_tons: r(delivered),
    plant_demand_coverage_pct: +((delivered / 60000) * 100).toFixed(1),
    plant_demand_warning: delivered < 60000 ? `Thiếu ${r(60000 - delivered).toLocaleString("vi-VN")} tấn so với nhu cầu nhà máy` : null,
    costs: {
      collection_cost: r(collection),
      warehouse_cost: r(warehouse),
      handling_cost: r(handling),
      transportation_cost: r(transport),
      total_delivered_logistics_cost: r(total),
      cost_per_ton: r(costPerTon),
      cost_per_ton_note: "Chi phí logistics giao tới nhà máy / tấn",
    },
    capex: { total: capex },
    financial: {
      lifecycle_years: years,
      discounted: st.s.discounted,
      discount_rate_pct: st.s.discounted ? 8 : 0,
      capex,
      tco: r(tco),
      lifetime_delivered_tons: r(delivered * years),
      tco_per_ton: r(tco / (delivered * years)),
      roi_pct: +roi.toFixed(1),
      payback_years: payback !== null && payback <= years ? payback : null,
      payback_status: payback === null ? "khong_hoan_von" : payback <= years ? "hoan_von" : "khong_hoan_von_trong_ky",
      baseline_cost_per_ton: baselineCost,
      baseline_source: "no_hub",
      baseline_source_label: "No-Hub Baseline: ruộng → nhà máy trực tiếp",
    },
    baseline: {
      available: true,
      reason: "",
      cooperative_count: 42,
      delivered_tons: r(delivered * 0.82),
      collection_cost: r(delivered * 0.82 * 95_000),
      transportation_cost: r(delivered * 0.82 * 420_000),
      total_delivered_logistics_cost: r(delivered * 0.82 * baselineCost),
      baseline_cost_per_ton: baselineCost,
      advisory_only: true,
    },
    hubs: hubs.map((h, i) => {
      const hub = HUBS.find((x) => x.id === h.hub_id);
      const share = delivered / hubs.length;
      return {
        hub_ref: h.hub_id,
        scenario_hub_id: h.scenario_hub_id,
        code: h.hub_code,
        service_radius_km: Number(h.service_radius_km),
        design_capacity_tons: Number(h.design_capacity_tons),
        cooperative_count: 9 + i * 4,
        delivered_tons: r(share),
        distance_to_plant_km: hub ? +(haversineM([hub.location.lng, hub.location.lat], PLANT) / 1000).toFixed(1) : null,
        distance_to_plant_source_label: h.mode_hub_plant === "waterway" ? "Đường thủy theo mạng" : "Ước tính đường bộ",
        flow: { peak_inventory_tons: r(share * 0.38), utilization_pct: +Math.min(100, (share * 0.38 / Number(h.design_capacity_tons)) * 100).toFixed(1) },
      };
    }),
    flow: {
      field_hub_tons: r(delivered * 0.7),
      hub_plant_tons: r(delivered * 0.68),
      direct_field_plant_tons: r(delivered * 0.3),
      uncollected_tons: r(delivered * 0.35),
      coverage_pct: +((delivered / 60000) * 100).toFixed(1),
      months: Array.from({ length: 12 }, (_, m) => ({ month: m + 1, label: `Th${m + 1}`, harvest_tons: r((delivered / 12) * (m % 4 === 2 ? 2.4 : 0.55)), plant_consumed_tons: r(delivered / 12) })),
    },
    unapproved_parameters: approved ? [] : [{ code: "sim.barge_cost_per_ton_km", number: 31, name: "Đơn giá sà lan (đ/tấn·km)" }],
    can_be_marked_official: approved,
    warnings: hubs.length === 1 ? ["Một Hub duy nhất — rủi ro gián đoạn cao"] : [],
    notes: [`Mô phỏng ${simId.slice(-6)} — số liệu tổng hợp chế độ giả lập`],
  };
}

function simOut(x: SimState): SimSimulation {
  const now = Date.now();
  if (!x.failed && x.sim.status !== "completed") {
    if (now >= x.doneAt) {
      x.sim.status = "completed";
      x.sim.completed_at = new Date(x.doneAt).toISOString();
      x.sim.has_result = true;
      const st = SCENARIOS.find((s) => s.s.id === x.sim.scenario_id)!;
      x.result = makeResult(st, x.sim.id, x.approved);
      change(st, "simulation_completed", "mock-worker", { simulation_id: x.sim.id });
    } else if (now >= x.doneAt - 2000) x.sim.status = "running";
  }
  return x.sim;
}

function submit(st: ScenarioState, actor: string, approved: boolean, at = Date.now(), delayMs = 3000, opts: { sensitivity?: boolean } = {}): SimState {
  const id = uuidv7();
  const x: SimState = {
    approved,
    doneAt: at + delayMs,
    result: null,
    failed: false,
    sim: {
      id,
      scenario_id: st.s.id,
      scenario_version: st.s.version,
      status: "queued",
      options: { mode: "evaluate", sensitivity_codes: opts.sensitivity ? ["sim.diesel_price", "sim.barge_cost_per_ton_km"] : [], max_hubs: null },
      requested_by: actor,
      requested_at: new Date(at).toISOString(),
      completed_at: null,
      error_code: null,
      error_detail: null,
      snapshot: {
        id: uuidv7(),
        sha256: id.replace(/-/g, "").padEnd(64, "0"),
        param_set_version: approved ? "PS-2026-09" : "PS-2026-09-draft",
        all_params_approved: approved,
        network_version: "2",
        algorithm_version: "hub-eval-1",
        cooperative_count: 42,
        route_count: 126,
        created_at: new Date(at).toISOString(),
      },
      has_result: false,
      official: false,
      official_eligible: approved,
      official_at: null,
      links: { self: `/v1/simulations/${id}`, job: `/v1/jobs/${id}`, result: `/v1/simulations/${id}/result` },
    },
  };
  SIMS.push(x);
  change(st, "simulation_requested", actor, { simulation_id: id });
  return x;
}

(function seed() {
  const a = newScenario("Phương án 2 Hub An Giang – Đồng Tháp", 10, false, "mock-admin-setter", iso(20));
  attach(a, HUBS[0]!, { service_radius_km: "25", design_capacity_tons: "30000", mode_hub_plant: "waterway" }, "mock-admin-setter");
  attach(a, HUBS[1]!, { service_radius_km: "20", design_capacity_tons: "20000" }, "mock-admin-setter");
  const b = newScenario("Phương án 1 Hub Thốt Nốt", 10, false, "mock-admin-setter", iso(10));
  attach(b, HUBS[2]!, { service_radius_km: "30", design_capacity_tons: "40000" }, "mock-admin-setter");
  newScenario("Phương án chiết khấu 15 năm (chưa gán Hub)", 15, true, "mock-admin-setter", iso(2));
  // Đã chạy: A đủ điều kiện Chính thức (mọi tham số đã duyệt); B chỉ tham khảo.
  simOut(submit(a, "mock-admin-setter", true, Date.now() - 86_400_000, 1000));
  simOut(submit(b, "mock-admin-setter", false, Date.now() - 43_200_000, 1000));
})();

const findScenario = (id: string | undefined) => SCENARIOS.find((s) => s.s.id === id);
const findSim = (id: string | undefined) => SIMS.find((x) => x.sim.id === id);

function compareRow(x: SimState): SimCompareRow {
  const st = findScenario(x.sim.scenario_id)!;
  const r = x.result as Record<string, Record<string, number | null> & { total?: number }>;
  const fin = r.financial!;
  const costs = r.costs!;
  return {
    simulation_id: x.sim.id,
    scenario_id: st.s.id,
    code: st.s.code,
    name: st.s.name,
    official: x.sim.official,
    is_baseline_column: false,
    parameter_set_version: String((x.result as { parameter_set_version?: string }).parameter_set_version ?? ""),
    lifecycle_years: st.s.lifecycle_years,
    discounted: st.s.discounted,
    hub_count: Number((x.result as { hub_count?: number }).hub_count ?? 0),
    delivered_tons: String((x.result as { delivered_tons?: number }).delivered_tons ?? 0),
    capex: String(fin.capex ?? 0),
    opex_year1: String(1_450_000_000 * st.s.hubs.length),
    collection_cost: String(costs.collection_cost ?? 0),
    transportation_cost: String(costs.transportation_cost ?? 0),
    warehouse_cost: String(costs.warehouse_cost ?? 0),
    handling_cost: String(costs.handling_cost ?? 0),
    total_delivered_logistics_cost: String(costs.total_delivered_logistics_cost ?? 0),
    cost_per_ton: costs.cost_per_ton === null || costs.cost_per_ton === undefined ? null : String(costs.cost_per_ton),
    tco: String(fin.tco ?? 0),
    tco_per_ton: fin.tco_per_ton === null || fin.tco_per_ton === undefined ? null : String(fin.tco_per_ton),
    lifecycle_cost_per_ton: fin.tco_per_ton === null || fin.tco_per_ton === undefined ? null : String(fin.tco_per_ton),
    roi_pct: fin.roi_pct === null || fin.roi_pct === undefined ? null : String(fin.roi_pct),
    payback_years: typeof fin.payback_years === "number" ? fin.payback_years : null,
    payback_status: String((fin as unknown as { payback_status: string }).payback_status),
    plant_demand_coverage_pct: String((x.result as { plant_demand_coverage_pct?: number }).plant_demand_coverage_pct ?? 0),
    shortfall_warnings: 0,
  };
}

export const simulationRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/candidate-hubs$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...ANALYSTS)) return problem(403, "forbidden", "Forbidden");
      const { slice, next } = pageSlice(HUBS, ctx.url, 50, 200);
      return json({ items: slice, next_cursor: next });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/candidate-hubs$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...ANALYSTS)) return problem(403, "forbidden", "Forbidden");
      const b = (ctx.body ?? {}) as { name?: string; location?: { lat?: number; lng?: number }; unit_code?: string };
      const name = (b.name ?? "").trim();
      if (name.length < 2 || name.length > 200) return validation([{ field: "name", reason: "Tên 2–200 ký tự" }]);
      const lat = Number(b.location?.lat);
      const lng = Number(b.location?.lng);
      if (!Number.isFinite(lat) || !Number.isFinite(lng) || Math.abs(lat) > 90 || Math.abs(lng) > 180) return validation([{ field: "location", reason: "Toạ độ WGS84 không hợp lệ" }]);
      const hub: SimCandidateHub = { id: uuidv7(), code: `HUB-${String(HUBS.length + 1).padStart(4, "0")}`, name, location: { lat, lng }, unit_code: b.unit_code ?? null, status: "active", retired_reason: null, created_at: nowIso() };
      HUBS.push(hub);
      return json(hub, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/candidate-hubs\/([\w-]+)\/retire$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...ANALYSTS)) return problem(403, "forbidden", "Forbidden");
      const hub = HUBS.find((h) => h.id === ctx.params[0]);
      if (!hub) return problem(404, "simulation.hub_not_found", "Not found");
      if (reasonOf(ctx.body).length < 10) return validation([{ field: "reason", reason: "Lý do ít nhất 10 ký tự" }]);
      hub.status = "retired";
      hub.retired_reason = reasonOf(ctx.body);
      return json(hub);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/scenarios$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...ANALYSTS)) return problem(403, "forbidden", "Forbidden");
      const { slice, next } = pageSlice(SCENARIOS.map((s) => s.s), ctx.url, 50, 200);
      return json({ items: slice, next_cursor: next });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/scenarios$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...ANALYSTS)) return problem(403, "forbidden", "Forbidden");
      const b = (ctx.body ?? {}) as { name?: string; lifecycle_years?: number; discounted?: boolean; reference_year?: number; plant?: { location?: { lat: number; lng: number }; annual_demand_tons?: string | null } };
      const name = (b.name ?? "").trim();
      if (name.length < 2 || name.length > 200) return validation([{ field: "name", reason: "Tên 2–200 ký tự" }]);
      if (!b.reference_year || b.reference_year < 2000 || b.reference_year > 2100) return validation([{ field: "reference_year", reason: "2000–2100" }]);
      const st = newScenario(name, b.lifecycle_years || 10, !!b.discounted, ctx.persona.me.actor_ref);
      st.s.reference_year = b.reference_year;
      if (b.plant?.location) st.s.plant.location = b.plant.location;
      if (b.plant?.annual_demand_tons) st.s.plant.annual_demand_tons = b.plant.annual_demand_tons;
      return json(st.s, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/scenarios:compare$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...ANALYSTS)) return problem(403, "forbidden", "Forbidden");
      const b = (ctx.body ?? {}) as { simulation_ids?: string[]; include_baseline?: boolean };
      const ids = b.simulation_ids ?? [];
      if (ids.length === 0 || ids.length > 20) return validation([{ field: "simulation_ids", reason: "1–20 lượt mô phỏng" }]);
      const sims = ids.map(findSim).filter((x): x is SimState => !!x && simOut(x).status === "completed" && !!x.result);
      const rows = sims.map(compareRow).sort((a, c) => Number(a.tco_per_ton ?? Infinity) - Number(c.tco_per_ton ?? Infinity));
      const lifecycles = new Set(sims.map((x) => `${findScenario(x.sim.scenario_id)!.s.lifecycle_years}|${findScenario(x.sim.scenario_id)!.s.discounted}`));
      const blocked = lifecycles.size > 1 ? "Các kịch bản khác vòng đời hoặc chế độ chiết khấu — không xếp hạng TCO/tấn được (BR-02)." : null;
      if (b.include_baseline !== false && sims[0]) {
        const base = sims[0].result as { baseline: { delivered_tons: number; total_delivered_logistics_cost: number; baseline_cost_per_ton: number; collection_cost: number; transportation_cost: number } };
        rows.push({
          simulation_id: null,
          scenario_id: null,
          code: "NO-HUB",
          name: "Không Hub (No-Hub Baseline)",
          official: false,
          is_baseline_column: true,
          lifecycle_years: rows[0]?.lifecycle_years ?? 10,
          discounted: rows[0]?.discounted ?? false,
          hub_count: 0,
          delivered_tons: String(base.baseline.delivered_tons),
          capex: "0",
          collection_cost: String(base.baseline.collection_cost),
          transportation_cost: String(base.baseline.transportation_cost),
          warehouse_cost: "0",
          handling_cost: "0",
          total_delivered_logistics_cost: String(base.baseline.total_delivered_logistics_cost),
          cost_per_ton: String(base.baseline.baseline_cost_per_ton),
          tco: String(base.baseline.total_delivered_logistics_cost * (rows[0]?.lifecycle_years ?? 10)),
          tco_per_ton: String(base.baseline.baseline_cost_per_ton),
          roi_pct: null,
          payback_years: null,
          payback_status: "thieu_baseline",
          plant_demand_coverage_pct: ((base.baseline.delivered_tons / 60000) * 100).toFixed(1),
          shortfall_warnings: 0,
        });
      }
      return json({ rows, sorted_by: "tco_per_ton", blocked, warnings: sims.length < ids.length ? ["Bỏ qua lượt mô phỏng chưa có kết quả"] : [] });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/scenarios\/([\w-]+)$/,
    handler: (ctx) => {
      const st = findScenario(ctx.params[0]);
      return st ? json(st.s) : problem(404, "simulation.scenario_not_found", "Not found");
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/scenarios\/([\w-]+)\/history$/,
    handler: (ctx) => {
      const st = findScenario(ctx.params[0]);
      if (!st) return problem(404, "simulation.scenario_not_found", "Not found");
      SIMS.filter((x) => x.sim.scenario_id === st.s.id).forEach(simOut);
      return json({ items: st.changes });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/scenarios\/([\w-]+)\/hubs$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...ANALYSTS)) return problem(403, "forbidden", "Forbidden");
      const st = findScenario(ctx.params[0]);
      if (!st) return problem(404, "simulation.scenario_not_found", "Not found");
      const b = (ctx.body ?? {}) as Partial<SimScenarioHub> & { hub_id?: string };
      const hub = HUBS.find((h) => h.id === b.hub_id);
      if (!hub) return problem(404, "simulation.hub_not_found", "Not found");
      if (hub.status === "retired") return problem(409, "simulation.hub_retired", "Retired");
      for (const f of ["service_radius_km", "design_capacity_tons", "barge_payload_tons"] as const) {
        const v = b[f];
        if (v !== undefined && v !== null && !(Number(v) > 0)) return validation([{ field: f, reason: "phải là số > 0" }]);
      }
      attach(st, hub, b, ctx.persona.me.actor_ref);
      return json(st.s);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/scenarios\/([\w-]+)\/hubs\/([\w-]+)\/detach$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...ANALYSTS)) return problem(403, "forbidden", "Forbidden");
      const st = findScenario(ctx.params[0]);
      if (!st) return problem(404, "simulation.scenario_not_found", "Not found");
      const idx = st.s.hubs.findIndex((h) => h.hub_id === ctx.params[1]);
      if (idx < 0) return problem(404, "simulation.hub_not_attached", "Not attached");
      if (reasonOf(ctx.body).length < 10) return validation([{ field: "reason", reason: "Lý do ít nhất 10 ký tự" }]);
      const prev = st.changes.filter((c) => c.hub_id === ctx.params[1]).at(-1);
      st.s.hubs.splice(idx, 1);
      change(st, "hub_detached", ctx.persona.me.actor_ref, { hub_id: ctx.params[1] ?? null, supersedes_id: prev?.id ?? null, correction_reason: reasonOf(ctx.body) });
      return json(st.s);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/simulations$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...ANALYSTS)) return problem(403, "forbidden", "Forbidden");
      const b = (ctx.body ?? {}) as { scenario_id?: string; sensitivity?: boolean };
      const st = findScenario(b.scenario_id);
      if (!st) return problem(404, "simulation.scenario_not_found", "Not found");
      if (st.s.hubs.length === 0) return problem(422, "simulation.scenario_without_hubs", "No hubs");
      // Chế độ giả lập: bộ tham số hiện hành còn một tham số chưa duyệt → kết quả mới chỉ "Tham khảo".
      const x = submit(st, ctx.persona.me.actor_ref, false, Date.now(), 3000, { sensitivity: b.sensitivity });
      return json(x.sim, 202, { Location: `/v1/jobs/${x.sim.id}` });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/simulations\/([\w-]+)$/,
    handler: (ctx) => {
      const x = findSim(ctx.params[0]);
      return x ? json(simOut(x)) : problem(404, "simulation.simulation_not_found", "Not found");
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/simulations\/([\w-]+)\/result$/,
    handler: (ctx) => {
      const x = findSim(ctx.params[0]);
      if (!x) return problem(404, "simulation.simulation_not_found", "Not found");
      const sim = simOut(x);
      if (sim.status !== "completed" || !x.result) return problem(409, "simulation.result_not_ready", "Not ready");
      return json({
        simulation_id: sim.id,
        scenario_id: sim.scenario_id,
        status: sim.official ? "chinh_thuc" : "tham_khao",
        official: sim.official,
        official_eligible: sim.official_eligible,
        snapshot: sim.snapshot ?? undefined,
        engine_version: "optimizer-1.4.0 (giả lập)",
        algorithm_version: "hub-eval-1",
        result_sha256: sim.id.replace(/-/g, "").padEnd(64, "f"),
        received_at: sim.completed_at ?? nowIso(),
        result: x.result,
        sensitivity: null,
      });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/simulations\/([\w-]+)\/mark-official$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...ANALYSTS)) return problem(403, "forbidden", "Forbidden");
      const x = findSim(ctx.params[0]);
      if (!x) return problem(404, "simulation.simulation_not_found", "Not found");
      const sim = simOut(x);
      if (sim.status !== "completed") return problem(409, "simulation.result_not_ready", "Not ready");
      if (!x.approved)
        return problem(409, "simulation.params_unapproved", "Parameters unapproved", { unapproved_parameters: [{ code: "sim.barge_cost_per_ton_km", number: 31, name: "Đơn giá sà lan (đ/tấn·km)" }] });
      sim.official = true;
      sim.official_at = nowIso();
      change(findScenario(sim.scenario_id)!, "marked_official", ctx.persona.me.actor_ref, { simulation_id: sim.id });
      return json(sim);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/jobs\/([\w-]+)$/,
    handler: (ctx) => {
      const x = findSim(ctx.params[0]);
      if (!x) return problem(404, "simulation.simulation_not_found", "Not found");
      const sim = simOut(x);
      return json({ id: sim.id, kind: "simulation", status: sim.status, error_code: sim.error_code ?? null, requested_at: sim.requested_at, completed_at: sim.completed_at ?? null, links: sim.links });
    },
  },
];
